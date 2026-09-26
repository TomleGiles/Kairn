"""Reformulation des explications d'anomalies (M-07) demandée par analytics.

Le brouillon déterministe d'analytics fait foi : la reformulation n'est
retenue que si tous ses chiffres figurent dans les données fournies.
"""

from __future__ import annotations

import json
from typing import Any

from pydantic import BaseModel, Field

from . import grounding, prompts
from .providers import Provider, Usage


class ExplainEvent(BaseModel):
    title: str
    kind: str
    ts: str
    score: float


class ExplainRequest(BaseModel):
    org_id: str
    locale: str = "fr"
    series: str
    expected: str
    actual: str
    currency: str = "EUR"
    start: str
    end: str
    events: list[ExplainEvent] = Field(default_factory=list, max_length=20)
    draft: str = Field(max_length=4000)


def explain(provider: Provider, req: ExplainRequest) -> tuple[str | None, Usage]:
    payload: dict[str, Any] = req.model_dump(exclude={"org_id", "locale"})
    turn = provider.turn(
        prompts.explain(req.locale), [{"role": "user", "content": json.dumps(payload, ensure_ascii=False)}], lambda _t: None, use_tools=False
    )
    text = turn.text.strip()
    if turn.stop != "end" or not text or len(text) > 1500:
        return None, turn.usage
    if not grounding.check(text, [payload]).ok:
        return None, turn.usage
    return text, turn.usage
