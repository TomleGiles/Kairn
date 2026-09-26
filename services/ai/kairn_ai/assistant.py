"""Boucle de l'assistant conversationnel (M-10).

Déroulé d'une question :
1. le modèle répond ou demande des outils ; les outils appellent l'API Kairn
   avec le jeton de l'utilisateur (mêmes droits que lui) ;
2. les résultats sont réinjectés jusqu'à la réponse finale (nombre de tours borné) ;
3. contrôle de grounding : chaque chiffre de la réponse doit figurer dans un
   résultat d'outil ; sinon un tour de correction est demandé, puis un
   avertissement est affiché si des chiffres restent invérifiables ;
4. la consommation (tokens, coût Decimal) est enregistrée pour l'organisation.

Les événements émis suivent le protocole SSE attendu par l'interface web :
text, reset, tool_call, chart, sources, warning, error, done.
"""

from __future__ import annotations

import json
import logging
from collections.abc import Callable
from datetime import UTC, datetime
from decimal import Decimal, InvalidOperation
from typing import Any, Literal

from pydantic import BaseModel, Field

from . import grounding, prompts
from .kairn import ServiceAPI, UserAPI
from .providers import Provider, ToolCall, Usage
from .tools import RenderChart, ToolOutcome, execute

log = logging.getLogger(__name__)

Emit = Callable[[dict[str, Any]], None]


class ChatMessage(BaseModel):
    role: Literal["user", "assistant"]
    content: str = Field(max_length=20000)


def _label(call: ToolCall, index: int) -> str:
    """Libellé lisible d'une source : outil et paramètres principaux."""
    args = call.input if isinstance(call.input, dict) else {}
    parts = []
    for k, v in args.items():
        if v in (None, "", [], {}) or k == "series":
            continue
        if isinstance(v, list):
            v = ",".join(str(x) for x in v[:4])
        parts.append(f"{k}={v}")
    return f"#{index} {call.name}" + (f" ({'; '.join(parts)[:160]})" if parts else "")


def _chart_grounded(spec: RenderChart, ref: set[Decimal]) -> list[str]:
    missing: list[str] = []
    for s in spec.series:
        for v in s.values:
            try:
                d = Decimal(repr(v))
            except InvalidOperation:
                missing.append(str(v))
                continue
            if not ({x.normalize() for x in grounding.value_variants(d)} & ref):
                missing.append(str(v))
    return missing


class Assistant:
    def __init__(self, provider: Provider, api: UserAPI, service: ServiceAPI | None, org: dict[str, Any], locale: str, max_turns: int = 8) -> None:
        self.provider = provider
        self.api = api
        self.service = service
        self.org = org
        self.locale = "en" if locale == "en" else "fr"
        self.max_turns = max_turns
        self.usage = Usage(provider.name, provider.model)
        self.sources: list[str] = []
        self.results: list[str] = []  # contenus JSON des résultats d'outils réussis

    def _system(self) -> str:
        return prompts.assistant(self.locale, str(self.org.get("name") or ""), str(self.org.get("currency") or "EUR"))

    def _run_tools(self, calls: list[ToolCall], emit: Emit) -> list[tuple[ToolCall, ToolOutcome]]:
        out: list[tuple[ToolCall, ToolOutcome]] = []
        for call in calls:
            index = len(self.sources) + 1
            emit({"type": "tool_call", "name": call.name})
            if call.name == "render_chart":
                outcome = self._chart(call, emit)
            elif isinstance(call.input, dict) and "__invalid_json__" in call.input:
                outcome = ToolOutcome(name=call.name, ok=False, content=json.dumps({"error": "INVALID_JSON"}))
            else:
                outcome = execute(self.api, call.name, call.input, index)
            if outcome.ok and call.name != "render_chart":
                self.sources.append(_label(call, index))
                self.results.append(outcome.content)
            out.append((call, outcome))
        return out

    def _chart(self, call: ToolCall, emit: Emit) -> ToolOutcome:
        try:
            spec = RenderChart.model_validate(call.input or {})
        except ValueError as exc:
            return ToolOutcome(name=call.name, ok=False, content=json.dumps({"error": "INVALID_INPUT", "details": str(exc)[:500]}))
        missing = _chart_grounded(spec, grounding.reference_values(self.results))
        if missing:
            return ToolOutcome(
                name=call.name,
                ok=False,
                content=json.dumps({"error": "values not found in tool results; call a data tool first", "values": missing[:10]}),
            )
        emit({"type": "chart", "spec": spec.model_dump()})
        return ToolOutcome(name=call.name, ok=True, content=json.dumps({"rendered": True}))

    def run(self, history: list[ChatMessage], emit: Emit) -> None:
        question = next((m.content for m in reversed(history) if m.role == "user"), "")
        messages: list[dict[str, Any]] = [{"role": m.role, "content": m.content} for m in history if m.content.strip()]
        system = self._system()
        answer_parts: list[str] = []
        repaired = False
        pending_break = False

        def on_text(t: str) -> None:
            nonlocal pending_break
            # Paragraphe distinct entre le préambule d'un appel d'outil et la suite de la réponse.
            if pending_break and answer_parts and not answer_parts[-1].endswith("\n"):
                answer_parts.append("\n\n")
                emit({"type": "text", "text": "\n\n"})
            pending_break = False
            answer_parts.append(t)
            emit({"type": "text", "text": t})

        try:
            for _ in range(self.max_turns):
                turn = self.provider.turn(system, messages, on_text)
                self.usage.add(turn.usage)
                if turn.stop == "error":
                    emit({"type": "error", "message": turn.error or "erreur du fournisseur"})
                    return
                if turn.stop == "refusal":
                    emit({"type": "error", "message": turn.error})
                    return
                messages.append(turn.assistant_message)
                if turn.stop == "tool_use":
                    messages.extend(self.provider.tool_results(self._run_tools(turn.tool_calls, emit)))
                    pending_break = True
                    continue
                if turn.stop == "max_tokens":
                    emit(
                        {
                            "type": "warning",
                            "message": "Réponse tronquée (longueur maximale atteinte)."
                            if self.locale == "fr"
                            else "Answer truncated (maximum length reached).",
                        }
                    )
                report = grounding.check("".join(answer_parts), self.results, question)
                if report.ok:
                    break
                if repaired:
                    emit(
                        {
                            "type": "warning",
                            "numbers": report.ungrounded,
                            "message": (
                                "Chiffres non vérifiés dans les données : " if self.locale == "fr" else "Numbers not verified against the data: "
                            )
                            + ", ".join(report.ungrounded[:10]),
                        }
                    )
                    break
                # Un tour de correction : la réponse affichée est remplacée.
                log.info("ungrounded numbers in answer, asking for a repair", extra={"count": len(report.ungrounded)})
                repaired = True
                answer_parts.clear()
                emit({"type": "reset"})
                messages.append({"role": "user", "content": prompts.repair(self.locale, report.ungrounded)})
            else:
                emit(
                    {"type": "warning", "message": "Nombre maximal d'étapes atteint." if self.locale == "fr" else "Maximum number of steps reached."}
                )
            emit({"type": "sources", "sources": self.sources})
            emit(
                {
                    "type": "done",
                    "usage": {"input_tokens": self.usage.input_tokens, "output_tokens": self.usage.output_tokens, "model": self.usage.model},
                }
            )
        finally:
            self.record()

    def record(self, feature: str = "assistant") -> None:
        if self.service is None or (self.usage.input_tokens == 0 and self.usage.output_tokens == 0):
            return
        self.service.record_usage(str(self.org["id"]), usage_payload(self.usage, feature))


def usage_payload(u: Usage, feature: str) -> dict[str, Any]:
    return {
        "at": datetime.now(tz=UTC).isoformat(),
        "feature": feature,
        "provider": u.provider,
        "model": u.model,
        "input_tokens": u.input_tokens,
        "output_tokens": u.output_tokens,
        "cost": str(u.cost),
        "currency": u.currency,
    }
