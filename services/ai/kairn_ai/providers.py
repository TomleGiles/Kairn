"""Fournisseurs LLM abstraits : Anthropic (défaut SaaS), Mistral (souverain, UE)
et modèle local via une API compatible OpenAI (édition self-hosted).

Chaque fournisseur exécute un « tour » de conversation : il diffuse le texte
au fil de l'eau, puis renvoie les appels d'outils demandés, le message
assistant à réinjecter et la consommation (tokens et coût, en Decimal).
"""

from __future__ import annotations

import json
import logging
import os
from collections.abc import Callable
from dataclasses import dataclass, field
from decimal import Decimal
from typing import Any, Protocol

import anthropic
import httpx

from .config import Settings
from .tools import ToolOutcome, anthropic_tools, openai_tools

log = logging.getLogger(__name__)

MTOK = Decimal(1_000_000)

# Prix publics par million de tokens (USD) — utilisés pour la comptabilisation par organisation.
ANTHROPIC_PRICES: dict[str, tuple[Decimal, Decimal]] = {
    "claude-opus-5": (Decimal(5), Decimal(25)),
    "claude-opus-4-8": (Decimal(5), Decimal(25)),
    "claude-sonnet-5": (Decimal(2), Decimal(10)),
    "claude-haiku-4-5": (Decimal(1), Decimal(5)),
    "claude-fable-5-1": (Decimal(10), Decimal(50)),
}


@dataclass
class ToolCall:
    id: str
    name: str
    input: Any


@dataclass
class Usage:
    provider: str
    model: str
    input_tokens: int = 0
    output_tokens: int = 0
    cost: Decimal = Decimal(0)
    currency: str = "USD"

    def add(self, other: Usage) -> None:
        self.input_tokens += other.input_tokens
        self.output_tokens += other.output_tokens
        self.cost += other.cost
        self.model = other.model or self.model


@dataclass
class TurnResult:
    text: str
    tool_calls: list[ToolCall]
    stop: str  # end | tool_use | max_tokens | refusal | error
    assistant_message: dict[str, Any]
    usage: Usage
    error: str = ""
    served_by: str = ""
    notes: list[str] = field(default_factory=list)


class Provider(Protocol):
    name: str
    model: str

    def turn(self, system: str, messages: list[dict[str, Any]], on_text: Callable[[str], None], use_tools: bool = True) -> TurnResult: ...

    def tool_results(self, results: list[tuple[ToolCall, ToolOutcome]]) -> list[dict[str, Any]]: ...


class ProviderUnavailable(RuntimeError):
    pass


_MODEL_INTERNAL = {"thinking", "redacted_thinking", "tool_use"}


def echo_content(content: list[Any]) -> list[Any]:
    """Blocs à réinjecter dans l'historique.

    Après un repli en cours de génération, les blocs internes (thinking,
    tool_use) produits avant le dernier bloc `fallback` appartiennent au modèle
    qui a décliné : ils ne sont ni réinjectés ni exécutés.
    """
    last = max((i for i, b in enumerate(content) if getattr(b, "type", "") == "fallback"), default=-1)
    if last < 0:
        return list(content)
    kept = [b for b in content[:last] if getattr(b, "type", "") not in _MODEL_INTERNAL and getattr(b, "type", "") != "server_tool_use"]
    return kept + list(content[last + 1 :])


def served_by_fallback(msg: Any) -> bool:
    iterations = getattr(getattr(msg, "usage", None), "iterations", None) or []
    return any(getattr(it, "type", "") == "fallback_message" for it in iterations)


class AnthropicProvider:
    """Claude via le SDK officiel (flux, outils, cache de prompt, repli serveur sur refus)."""

    name = "anthropic"

    def __init__(self, cfg: Settings, client: anthropic.Anthropic | None = None) -> None:
        self.model = cfg.anthropic_model
        self.effort = cfg.anthropic_effort
        self._client = client or anthropic.Anthropic()
        if client is None and not any(getattr(self._client, a, None) for a in ("api_key", "auth_token", "credentials")):
            raise ProviderUnavailable("Le fournisseur Anthropic n'est pas configuré (ANTHROPIC_API_KEY absente).")

    def _usage(self, msg: Any) -> Usage:
        u = msg.usage
        inp = int(getattr(u, "input_tokens", 0) or 0)
        out = int(getattr(u, "output_tokens", 0) or 0)
        cache_read = int(getattr(u, "cache_read_input_tokens", 0) or 0)
        cache_write = int(getattr(u, "cache_creation_input_tokens", 0) or 0)
        model = str(getattr(msg, "model", self.model) or self.model)
        pin, pout = ANTHROPIC_PRICES.get(model, ANTHROPIC_PRICES["claude-opus-5"])
        cost = (Decimal(inp) * pin + Decimal(cache_read) * pin / 10 + Decimal(cache_write) * pin * Decimal("1.25") + Decimal(out) * pout) / MTOK
        return Usage(self.name, model, inp + cache_read + cache_write, out, cost.quantize(Decimal("0.000001")))

    def turn(self, system: str, messages: list[dict[str, Any]], on_text: Callable[[str], None], use_tools: bool = True) -> TurnResult:
        params: dict[str, Any] = {
            "model": self.model,
            "max_tokens": 64000,
            "system": [{"type": "text", "text": system}],
            "messages": messages,
            "thinking": {"type": "adaptive"},
            "cache_control": {"type": "ephemeral"},
            # Repli côté serveur si le modèle décline la requête (routage par catégorie de refus).
            "betas": ["server-side-fallback-2026-07-01"],
            "fallbacks": "default",
        }
        if use_tools:
            params["tools"] = anthropic_tools()
        if self.effort:
            params["output_config"] = {"effort": self.effort}
        empty = Usage(self.name, self.model)
        for attempt in range(2):
            streamed: list[str] = []
            try:
                with self._client.beta.messages.stream(**params) as stream:
                    for event in stream:
                        if event.type == "content_block_delta" and event.delta.type == "text_delta":
                            streamed.append(event.delta.text)
                            on_text(event.delta.text)
                    final = stream.get_final_message()
            except ValueError as exc:
                # Entrée d'outil JSON illisible en flux continu : on relance le tour une fois.
                log.warning("invalid streamed tool input (attempt %d): %s", attempt + 1, exc)
                if attempt == 0 and not streamed:
                    continue
                return TurnResult("".join(streamed), [], "error", {}, empty, error="INVALID_JSON")
            except anthropic.AuthenticationError:
                return TurnResult("", [], "error", {}, empty, error="Le fournisseur Anthropic n'est pas configuré (clé API absente ou invalide).")
            except anthropic.RateLimitError:
                return TurnResult("", [], "error", {}, empty, error="Limite de débit du fournisseur atteinte, réessayez dans un instant.")
            except anthropic.APIStatusError as exc:
                return TurnResult("", [], "error", {}, empty, error=f"Erreur du fournisseur ({exc.status_code}).")
            except anthropic.APIConnectionError:
                return TurnResult("", [], "error", {}, empty, error="Fournisseur injoignable.")
            usage = self._usage(final)
            content = echo_content(final.content)
            text = "".join(b.text for b in content if b.type == "text")
            calls = [ToolCall(b.id, b.name, b.input) for b in content if b.type == "tool_use"]
            assistant = {"role": "assistant", "content": content}
            stop = final.stop_reason
            notes = ["fallback"] if served_by_fallback(final) else []
            if stop == "refusal":
                return TurnResult(
                    text, [], "refusal", assistant, usage, error="Le modèle a décliné cette demande.", served_by=usage.model, notes=notes
                )
            if stop == "max_tokens":
                # Un appel d'outil tronqué ne doit jamais être exécuté.
                return TurnResult(text, [], "max_tokens", assistant, usage, served_by=usage.model, notes=notes)
            if stop == "tool_use" and calls:
                return TurnResult(text, calls, "tool_use", assistant, usage, served_by=usage.model, notes=notes)
            return TurnResult(text, [], "end", assistant, usage, served_by=usage.model, notes=notes)
        return TurnResult("", [], "error", {}, empty, error="INVALID_JSON")

    def tool_results(self, results: list[tuple[ToolCall, ToolOutcome]]) -> list[dict[str, Any]]:
        # Tous les résultats d'un tour dans un seul message utilisateur (appels parallèles).
        return [
            {
                "role": "user",
                "content": [{"type": "tool_result", "tool_use_id": c.id, "content": o.content, "is_error": not o.ok} for c, o in results],
            }
        ]


class OpenAICompatibleProvider:
    """Mistral (API hébergée dans l'UE) ou modèle local servi par une API compatible OpenAI."""

    def __init__(
        self,
        name: str,
        base_url: str,
        api_key: str,
        model: str,
        price_in: Decimal,
        price_out: Decimal,
        currency: str,
        transport: httpx.BaseTransport | None = None,
    ) -> None:
        self.name = name
        self.model = model
        self._price = (price_in, price_out, currency)
        headers = {"Authorization": f"Bearer {api_key}"} if api_key else {}
        self._http = httpx.Client(base_url=base_url.rstrip("/"), headers=headers, timeout=180, transport=transport)

    def turn(self, system: str, messages: list[dict[str, Any]], on_text: Callable[[str], None], use_tools: bool = True) -> TurnResult:
        body: dict[str, Any] = {"model": self.model, "messages": [{"role": "system", "content": system}, *messages], "temperature": 0.2}
        if use_tools:
            body["tools"] = openai_tools()
            body["tool_choice"] = "auto"
        empty = Usage(self.name, self.model, currency=self._price[2])
        try:
            r = self._http.post("/chat/completions", json=body)
        except httpx.HTTPError:
            return TurnResult("", [], "error", {}, empty, error=f"Fournisseur {self.name} injoignable.")
        if r.status_code >= 400:
            return TurnResult("", [], "error", {}, empty, error=f"Erreur du fournisseur {self.name} ({r.status_code}).")
        data = r.json()
        choice = (data.get("choices") or [{}])[0]
        msg = choice.get("message") or {}
        text = msg.get("content") or ""
        if text:
            on_text(text)
        calls: list[ToolCall] = []
        for tc in msg.get("tool_calls") or []:
            fn = tc.get("function") or {}
            try:
                args = json.loads(fn.get("arguments") or "{}")
            except ValueError:
                args = {"__invalid_json__": fn.get("arguments")}
            calls.append(ToolCall(tc.get("id", ""), fn.get("name", ""), args))
        u = data.get("usage") or {}
        inp, out = int(u.get("prompt_tokens", 0)), int(u.get("completion_tokens", 0))
        cost = (Decimal(inp) * self._price[0] + Decimal(out) * self._price[1]) / MTOK
        usage = Usage(self.name, str(data.get("model") or self.model), inp, out, cost.quantize(Decimal("0.000001")), self._price[2])
        assistant = {"role": "assistant", "content": text, **({"tool_calls": msg["tool_calls"]} if msg.get("tool_calls") else {})}
        finish = choice.get("finish_reason")
        if finish == "length":
            return TurnResult(text, [], "max_tokens", assistant, usage)
        if calls:
            return TurnResult(text, calls, "tool_use", assistant, usage)
        return TurnResult(text, [], "end", assistant, usage)

    def tool_results(self, results: list[tuple[ToolCall, ToolOutcome]]) -> list[dict[str, Any]]:
        return [{"role": "tool", "tool_call_id": c.id, "name": c.name, "content": o.content} for c, o in results]


def for_org(cfg: Settings, llm_provider: str, allow_external: bool) -> Provider:
    """Choisit le fournisseur autorisé par l'organisation (souveraineté : aucun appel hors UE sans accord explicite)."""
    p = llm_provider or "anthropic"
    if p == "none":
        raise ProviderUnavailable("Les fonctionnalités IA sont désactivées pour cette organisation.")
    if p == "anthropic":
        if not allow_external:
            raise ProviderUnavailable("L'organisation n'a pas autorisé les appels LLM hors UE.")
        return AnthropicProvider(cfg)
    if p == "mistral":
        if not cfg.mistral_key:
            raise ProviderUnavailable("Le fournisseur Mistral n'est pas configuré (MISTRAL_API_KEY).")
        return OpenAICompatibleProvider(
            "mistral",
            cfg.mistral_url,
            cfg.mistral_key,
            cfg.mistral_model,
            Decimal(os.environ.get("KAIRN_MISTRAL_PRICE_IN", "2")),
            Decimal(os.environ.get("KAIRN_MISTRAL_PRICE_OUT", "6")),
            "EUR",
        )
    if p == "local":
        return OpenAICompatibleProvider("local", cfg.local_url, cfg.local_key, cfg.local_model, Decimal(0), Decimal(0), "EUR")
    raise ProviderUnavailable(f"Fournisseur LLM inconnu : {p}")
