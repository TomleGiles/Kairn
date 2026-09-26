"""Corrélation automatique d'une anomalie avec les événements (M-07).

Dans la fenêtre [début - 36 h, fin], les déploiements, changements
d'inventaire, événements HPA et incidents sont notés selon leur proximité
temporelle et leur rapport avec le périmètre de l'anomalie.
"""

from __future__ import annotations

from datetime import datetime, timedelta

from .anomalies import Finding
from .models import CorrelatedEvent, Event

KIND_WEIGHT = {
    "deployment": 1.0,
    "inventory_change": 0.9,
    "hpa_scale": 0.5,
    "incident": 0.7,
    "k8s_event": 0.6,
    "metric_spike": 0.6,
}


def correlate(f: Finding, events: list[Event], scope_resources: set[str], limit: int = 5) -> list[CorrelatedEvent]:
    """Classe les événements susceptibles d'expliquer l'anomalie."""
    start = f.start - timedelta(hours=36)
    end = f.end
    out: list[CorrelatedEvent] = []
    for e in events:
        ts = e.ts
        if ts < start or ts >= end:
            continue
        # Les mises à l'échelle quotidiennes sont du bruit, sauf si le périmètre les concerne.
        if e.kind == "hpa_scale" and f.end - f.start <= timedelta(days=1) and e.resource_id not in scope_resources:
            continue
        weight = KIND_WEIGHT.get(e.kind, 0.3)
        # Proximité : maximale le jour même ou la veille du début.
        hours = abs((ts - f.start).total_seconds()) / 3600
        proximity = 1.0 if ts >= f.start - timedelta(hours=12) and ts < f.start + timedelta(hours=24) else max(0.2, 1 - hours / 72)
        in_scope = bool(scope_resources) and e.resource_id in scope_resources
        scope = 1.0 if in_scope else (0.5 if not scope_resources else 0.25)
        action = str(e.payload.get("action", ""))
        if e.kind == "inventory_change" and action == "created" and f.actual > f.expected:
            weight += 0.2
        if e.kind == "inventory_change" and action == "deleted" and f.actual < f.expected:
            weight += 0.2
        score = round(weight * proximity * scope, 3)
        if score < 0.1:
            continue
        why = []
        if in_scope:
            why.append("concerne une ressource du périmètre")
        why.append(f"{_kind_label(e.kind)} le {ts.strftime('%d/%m %H:%M')} UTC")
        out.append(CorrelatedEvent(event=e, score=score, why=", ".join(why)))
    out.sort(key=lambda c: (-c.score, c.event.ts))
    return _dedupe(out)[:limit]


def _dedupe(items: list[CorrelatedEvent]) -> list[CorrelatedEvent]:
    seen: set[tuple[str, str]] = set()
    out = []
    for c in items:
        key = (c.event.kind, c.event.title)
        if key in seen:
            continue
        seen.add(key)
        out.append(c)
    return out


def _kind_label(kind: str) -> str:
    return {
        "deployment": "déploiement",
        "inventory_change": "changement d'inventaire",
        "hpa_scale": "mise à l'échelle HPA",
        "incident": "incident",
        "k8s_event": "événement Kubernetes",
    }.get(kind, kind)


def window_bounds(f: Finding) -> tuple[datetime, datetime]:
    return f.start - timedelta(hours=36), f.end
