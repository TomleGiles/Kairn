"""Détection d'anomalies de coût (M-07).

Chaque série journalière (total, nœud d'allocation, fournisseur, ressource)
est comparée à une base robuste saisonnière. Les jours anormaux consécutifs
sont fusionnés en une anomalie (fenêtre), avec sévérité et score.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime, timedelta
from decimal import Decimal

from .money import dec, micros
from .stats import normalized_baseline, weekday_factors


@dataclass
class DailySeries:
    key: str  # ex. cost:total, cost:node:<id>, cost:resource:<id>
    label: str  # libellé lisible
    scope: dict[str, str] = field(default_factory=dict)  # node_id, resource_id, provider…
    days: list[datetime] = field(default_factory=list)
    values: list[float] = field(default_factory=list)
    min_delta: float = 1.0  # écart absolu minimal (devise/jour)


@dataclass
class Finding:
    series: DailySeries
    start: datetime
    end: datetime  # exclu
    expected: float
    actual: float
    score: float
    severity: str
    sustained: bool = False  # rupture de niveau durable


def _severity(rel: float, z: float) -> str:
    if abs(rel) >= 0.5 or abs(z) >= 10:
        return "critical"
    if abs(rel) >= 0.2 or abs(z) >= 6:
        return "warning"
    return "info"


def detect(series: DailySeries, lookback_days: int = 21, z_threshold: float = 4.0, rel_threshold: float = 0.15,
           window: int = 28, shift_days: int = 3) -> list[Finding]:
    """Détecte les jours anormaux des `lookback_days` derniers jours complets.

    Une rupture de niveau (au moins `shift_days` jours consécutifs anormaux dans
    le même sens) est signalée une seule fois : les jours suivants sont comparés
    au nouveau régime, qui devient la base.
    """
    n = len(series.values)
    if n < 14:
        return []
    weekdays = [d.weekday() for d in series.days]
    flagged: list[tuple[int, float, float, float]] = []  # index, attendu, réel, z
    start_idx = max(14, n - lookback_days)
    regime = 0  # début du régime courant
    run: list[int] = []  # jours anormaux consécutifs en cours
    for i in range(start_idx, n):
        lo = max(regime, i - window)
        if i - lo < 3:
            continue  # nouveau régime trop court pour servir de base
        # Facteurs hebdomadaires estimés sur un historique long (tous régimes confondus).
        long_lo = max(0, i - 2 * window)
        factors = weekday_factors(series.values[long_lo:i], weekdays[long_lo:i])
        base = normalized_baseline(series.values[lo:i], weekdays[lo:i], weekdays[i], factors)
        actual = series.values[i]
        delta = actual - base.expected
        z = delta / base.scale
        rel = delta / base.expected if base.expected > 0 else (1.0 if actual > 0 else 0.0)
        if abs(z) >= z_threshold and abs(rel) >= rel_threshold and abs(delta) >= series.min_delta:
            if run and (i != run[-1] + 1 or (z > 0) != (flagged[-1][3] > 0)):
                run = []
            flagged.append((i, base.expected, actual, z))
            run.append(i)
            if len(run) >= shift_days:
                regime = run[0]  # rupture durable : nouveau régime
        else:
            run = []
    findings: list[Finding] = []
    group: list[tuple[int, float, float, float]] = []

    def flush() -> None:
        if not group:
            return
        exp = sum(g[1] for g in group) / len(group)
        act = sum(g[2] for g in group) / len(group)
        z = max((g[3] for g in group), key=abs)
        rel = (act - exp) / exp if exp > 0 else 1.0
        findings.append(Finding(series, series.days[group[0][0]], series.days[group[-1][0]] + timedelta(days=1),
                                exp, act, float(z), _severity(rel, z), sustained=len(group) >= shift_days))

    for item in flagged:
        if group and (item[0] != group[-1][0] + 1 or (item[3] > 0) != (group[-1][3] > 0)):
            flush()
            group = []
        group.append(item)
    flush()
    return findings


def title_for(f: Finding, currency: str) -> str:
    direction = "Hausse" if f.actual > f.expected else "Baisse"
    rel = (f.actual - f.expected) / f.expected * 100 if f.expected > 0 else 100.0
    return f"{direction} de coût : {f.series.label} ({rel:+.0f} %)"


def to_decimal(v: float) -> Decimal:
    return micros(dec(v))
