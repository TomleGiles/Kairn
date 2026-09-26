"""Outils statistiques : base robuste saisonnière et Holt-Winters additif amorti."""

from __future__ import annotations

import math
from dataclasses import dataclass

import numpy as np


def percentile(values: list[float], p: float) -> float:
    """Percentile par interpolation linéaire (0..100)."""
    if not values:
        return 0.0
    return float(np.percentile(np.asarray(values, dtype=float), p))


def mad(values: np.ndarray) -> float:
    """Écart absolu médian."""
    if values.size == 0:
        return 0.0
    med = float(np.median(values))
    return float(np.median(np.abs(values - med)))


@dataclass(frozen=True)
class Baseline:
    expected: float
    scale: float  # dispersion robuste (1,4826 × MAD)
    samples: int


def seasonal_baseline(history: list[float], weekdays: list[int], target_weekday: int, window: int = 28) -> Baseline:
    """Valeur attendue d'un jour à partir des `window` jours précédents.

    La base est la médiane des jours précédents, corrigée d'un facteur de jour
    de semaine (rapport entre la médiane du même jour de semaine et la médiane
    globale) quand l'historique le permet. Robuste aux valeurs aberrantes.
    """
    h = np.asarray(history[-window:], dtype=float)
    wd = weekdays[-window:]
    if h.size == 0:
        return Baseline(0.0, 0.0, 0)
    med = float(np.median(h))
    same = np.asarray([v for v, d in zip(h, wd, strict=False) if d == target_weekday], dtype=float)
    factor = 1.0
    if same.size >= 3 and med > 0:
        factor = float(np.median(same)) / med
        factor = min(max(factor, 0.5), 2.0)
    expected = med * factor
    scale = 1.4826 * mad(h)
    # Plancher de dispersion : 2 % de la valeur attendue, pour les séries très stables.
    scale = max(scale, abs(expected) * 0.02, 1e-9)
    return Baseline(expected, scale, int(h.size))


def weekday_factors(values: list[float], weekdays: list[int]) -> dict[int, float]:
    """Facteur multiplicatif par jour de semaine (médiane du jour / médiane globale)."""
    arr = np.asarray(values, dtype=float)
    med = float(np.median(arr)) if arr.size else 0.0
    out = {d: 1.0 for d in range(7)}
    if med <= 0:
        return out
    for d in range(7):
        same = [v for v, w in zip(values, weekdays, strict=False) if w == d]
        if len(same) >= 3:
            out[d] = min(max(float(np.median(same)) / med, 0.5), 2.0)
    return out


def normalized_baseline(history: list[float], weekdays: list[int], target_weekday: int, factors: dict[int, float]) -> Baseline:
    """Base robuste d'un jour : médiane des valeurs désaisonnalisées × facteur du jour visé."""
    if not history:
        return Baseline(0.0, 0.0, 0)
    norm = np.asarray([v / factors.get(w, 1.0) for v, w in zip(history, weekdays, strict=False)], dtype=float)
    f = factors.get(target_weekday, 1.0)
    expected = float(np.median(norm)) * f
    scale = max(1.4826 * mad(norm) * f, abs(expected) * 0.02, 1e-9)
    return Baseline(expected, scale, int(norm.size))


@dataclass(frozen=True)
class HWResult:
    forecast: list[float]
    sigma: float
    alpha: float
    beta: float
    gamma: float
    phi: float
    sse: float


def _hw_run(y: np.ndarray, m: int, alpha: float, beta: float, gamma: float, phi: float, horizon: int) -> tuple[float, list[float]]:
    n = y.size
    level = float(np.mean(y[:m]))
    trend = float((np.mean(y[m : 2 * m]) - np.mean(y[:m])) / m) if n >= 2 * m else 0.0
    season = [float(y[i] - level) for i in range(m)]
    sse = 0.0
    for t in range(n):
        s = season[t % m]
        pred = level + phi * trend + s
        if t >= m:
            err = float(y[t]) - pred
            sse += err * err
        prev_level = level
        level = alpha * (float(y[t]) - s) + (1 - alpha) * (prev_level + phi * trend)
        trend = beta * (level - prev_level) + (1 - beta) * phi * trend
        season[t % m] = gamma * (float(y[t]) - level) + (1 - gamma) * s
    out: list[float] = []
    damp = 0.0
    for h in range(1, horizon + 1):
        damp += phi**h
        out.append(level + damp * trend + season[(n + h - 1) % m])
    return sse, out


def holt_winters(values: list[float], horizon: int, season: int = 7) -> HWResult:
    """Holt-Winters additif à tendance amortie, paramètres choisis par grille.

    Repli sur une moyenne saisonnière naïve si l'historique est trop court.
    """
    y = np.asarray(values, dtype=float)
    if y.size < 2 * season + 2:
        mean = float(np.mean(y)) if y.size else 0.0
        std = float(np.std(y)) if y.size > 1 else abs(mean) * 0.1
        return HWResult([mean] * horizon, std, 0, 0, 0, 1, 0)
    best: HWResult | None = None
    for alpha in (0.1, 0.2, 0.3, 0.5, 0.7):
        for beta in (0.0, 0.05, 0.1, 0.2):
            for gamma in (0.05, 0.1, 0.3):
                for phi in (0.8, 0.9, 0.98):
                    sse, fc = _hw_run(y, season, alpha, beta, gamma, phi, horizon)
                    if best is None or sse < best.sse:
                        n_err = max(y.size - season, 1)
                        best = HWResult(fc, math.sqrt(sse / n_err), alpha, beta, gamma, phi, sse)
    assert best is not None
    return best
