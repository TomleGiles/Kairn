"""Explications d'anomalies.

L'explication par défaut est un gabarit déterministe : chaque chiffre cité
provient des données (série de coût, événements). Si le service IA est
configuré et autorisé pour l'organisation, il reformule l'explication à
partir des mêmes données et de sa propre vérification des chiffres.
"""

from __future__ import annotations

import logging
import os
from decimal import Decimal

import httpx

from .anomalies import Finding
from .models import CorrelatedEvent, Organization
from .money import dec, fmt_eur

log = logging.getLogger(__name__)


def template(f: Finding, correlated: list[CorrelatedEvent], currency: str, locale: str = "fr") -> tuple[str, list[str]]:
    exp, act = dec(round(f.expected, 2)), dec(round(f.actual, 2))
    rel = (f.actual - f.expected) / f.expected * 100 if f.expected > 0 else 100.0
    start = f.start.strftime("%d/%m/%Y")
    days = (f.end - f.start).days
    sources = [f"series:{f.series.key}"]
    if locale == "en":
        text = (f"Daily cost of {f.series.label} went from {fmt_eur(exp, currency, 'en')} (expected) to "
                f"{fmt_eur(act, currency, 'en')} ({rel:+.0f}%) starting {f.start:%Y-%m-%d}")
        text += " (lasting change)." if f.sustained else (f", for {days} day(s)." if days > 1 else ".")
    else:
        text = (f"Le coût journalier de {f.series.label} est passé de {fmt_eur(exp, currency)} (attendu) à "
                f"{fmt_eur(act, currency)} ({rel:+.0f} %) à partir du {start}")
        text += " (changement durable)." if f.sustained else (f", pendant {days} jours." if days > 1 else ".")
    if correlated:
        top = correlated[0]
        for c in correlated:
            ref = f"event:{c.event.kind}:{c.event.ts.isoformat()}:{c.event.resource_id or c.event.title}"
            if ref not in sources:
                sources.append(ref)
        if locale == "en":
            text += f" Most likely related event: « {top.event.title} » ({top.event.ts:%Y-%m-%d %H:%M} UTC)."
        else:
            text += f" Événement le plus probablement lié : « {top.event.title} » ({top.event.ts:%d/%m %H:%M} UTC)."
        if len(correlated) > 1:
            others = ", ".join(f"« {c.event.title} »" for c in correlated[1:3])
            text += (" Other nearby events: " if locale == "en" else " Autres événements proches : ") + others + "."
    else:
        text += (" No deployment, inventory change or incident was found in the window; check usage metrics."
                 if locale == "en" else
                 " Aucun déploiement, changement d'inventaire ni incident n'a été trouvé dans la fenêtre ; vérifier les métriques d'usage.")
    return text, sources


def ai_explanation(org: Organization, f: Finding, correlated: list[CorrelatedEvent], fallback: str) -> str | None:
    """Demande une reformulation au service IA ; None si indisponible ou non autorisé."""
    url = os.environ.get("KAIRN_AI_URL", "")
    provider = org.settings.llm_provider or "anthropic"
    if not url or not org.allows("assistant") or provider == "none" or (provider == "anthropic" and not org.settings.allow_external_llm):
        return None
    payload = {
        "org_id": org.id, "locale": org.locale, "series": f.series.label,
        "expected": str(Decimal(str(round(f.expected, 2)))), "actual": str(Decimal(str(round(f.actual, 2)))),
        "currency": org.currency, "start": f.start.isoformat(), "end": f.end.isoformat(),
        "events": [{"title": c.event.title, "kind": c.event.kind, "ts": c.event.ts.isoformat(), "score": c.score} for c in correlated],
        "draft": fallback,
    }
    try:
        r = httpx.post(url.rstrip("/") + "/v1/explain", json=payload, timeout=60,
                       headers={"X-Kairn-Service-Token": os.environ.get("KAIRN_SERVICE_TOKEN", "")})
        r.raise_for_status()
        text = r.json().get("explanation")
        return text if isinstance(text, str) and text else None
    except (httpx.HTTPError, ValueError) as exc:
        log.warning("AI explanation unavailable: %s", exc)
        return None
