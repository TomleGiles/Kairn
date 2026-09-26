"""Tests de la boucle de l'assistant, des fournisseurs, des rapports et du MCP (sans appel réseau réel)."""

from __future__ import annotations

import json
from collections.abc import Callable
from datetime import date
from decimal import Decimal
from types import SimpleNamespace
from typing import Any

import httpx
import pytest

from kairn_ai import reports
from kairn_ai.assistant import Assistant, ChatMessage
from kairn_ai.config import Settings
from kairn_ai.kairn import ServiceAPI, UserAPI
from kairn_ai.mcp_server import OrgError, _resolve_org
from kairn_ai.providers import (
    AnthropicProvider,
    OpenAICompatibleProvider,
    ProviderUnavailable,
    ToolCall,
    TurnResult,
    Usage,
    echo_content,
    for_org,
)
from kairn_ai.tools import ToolOutcome

ORG = "0190f5a0-0000-7000-8000-000000000001"
CFG = Settings(api_url="http://kairn.test", service_token="svc", public_url="http://app.test")


EXTRA_ORGS: list[dict[str, object]] = []


def fake_api(request: httpx.Request) -> httpx.Response:
    path = request.url.path
    if path.endswith("/costs/summary"):
        return httpx.Response(
            200, json={"currency": "EUR", "month_to_date": "12345.67", "forecast_month_end": "15210.40", "by_node": [], "daily": []}
        )
    if path.endswith("/costs"):
        group = request.url.params.get_list("group_by")
        start = request.url.params.get("from", "")
        if request.url.params.get("granularity") == "day":
            first = PREVIOUS_DATA_FROM.get("day") or start[:10]
            return httpx.Response(
                200, json={"currency": "EUR", "total": "1", "rows": [{"period": f"{first}T00:00:00Z", "keys": {}, "amount": "10.00"}]}
            )
        if group == ["provider"]:
            rows = [{"keys": {"provider": "openstack"}, "amount": "8000.00"}, {"keys": {"provider": "kubernetes"}, "amount": "4345.67"}]
        elif group == ["allocation_node_id"]:
            base = Decimal("5000") if start.startswith("2026-08") else Decimal("4000")
            rows = [
                {"keys": {"allocation_node_id": "n1"}, "amount": str(base)},
                {"keys": {"allocation_node_id": "n2"}, "amount": "1100.50"},
                {"keys": {"allocation_node_id": "env-prod"}, "amount": "1000.00"},
            ]
        else:
            rows = []
        total = "7100.50" if start.startswith("2026-08") else "6100.50"
        return httpx.Response(200, json={"currency": "EUR", "total": total, "rows": rows})
    if path.endswith("/allocation/nodes"):
        return httpx.Response(
            200,
            json={
                "items": [
                    {"id": "n1", "name": "Data", "kind": "team"},
                    {"id": "n2", "name": "Shop", "kind": "team"},
                    {"id": "svc", "name": "Boutique", "kind": "service", "parent_id": "n2"},
                    {"id": "env-prod", "name": "production", "kind": "environment", "parent_id": "svc"},
                ]
            },
        )
    if path.endswith("/anomalies"):
        return httpx.Response(
            200,
            json=[
                {
                    "kind": "cost",
                    "title": "Hausse Data",
                    "expected": "120.00",
                    "actual": "320.00",
                    "window_start": "2026-08-12T00:00:00Z",
                    "window_end": "2026-08-14T00:00:00Z",
                    "explanation": "Le déploiement spark-worker v4 coïncide avec la hausse.",
                }
            ],
        )
    if path.endswith("/recommendations/summary"):
        return httpx.Response(
            200, json={"open_count": 4, "potential_monthly": "1225.00", "accepted_monthly": "150.00", "realized_monthly": "310.20", "currency": "EUR"}
        )
    if path.endswith("/recommendations"):
        return httpx.Response(
            200,
            json={
                "items": [
                    {"title": "Réduire vm-batch-01", "savings_monthly": "420.00", "risk": "low", "remediation": {}},
                    {"title": "Supprimer le volume orphelin", "savings_monthly": "35.10", "risk": "low", "remediation": {}},
                ]
            },
        )
    if path.endswith("/budgets-status"):
        return httpx.Response(200, json=[])
    if path == "/internal/v1/orgs":
        return httpx.Response(200, json=[{"id": ORG, "name": "Démo", "currency": "EUR", "locale": "fr", "settings": {}}, *EXTRA_ORGS])
    if path.startswith("/internal/v1/orgs/") and request.method in ("PUT", "POST"):
        CAPTURED.append((path, json.loads(request.content)))
        return httpx.Response(200, json={})
    return httpx.Response(404, json={"detail": "not found"})


CAPTURED: list[tuple[str, Any]] = []
PREVIOUS_DATA_FROM: dict[str, str] = {}


class ScriptedProvider:
    """Fournisseur factice : rejoue une suite de tours."""

    name = "fake"
    model = "fake-1"

    def __init__(self, turns: list[Callable[[list[dict[str, Any]]], TurnResult]]) -> None:
        self.turns = turns
        self.seen: list[list[dict[str, Any]]] = []

    def turn(self, system: str, messages: list[dict[str, Any]], on_text: Callable[[str], None], use_tools: bool = True) -> TurnResult:
        self.seen.append(list(messages))
        res = self.turns.pop(0)(messages)
        if res.text:
            on_text(res.text)
        return res

    def tool_results(self, results: list[tuple[ToolCall, ToolOutcome]]) -> list[dict[str, Any]]:
        return [
            {
                "role": "user",
                "content": [{"type": "tool_result", "tool_use_id": c.id, "content": o.content, "is_error": not o.ok} for c, o in results],
            }
        ]


def _usage() -> Usage:
    return Usage("fake", "fake-1", 100, 20, Decimal("0.001"))


def tool_turn(*calls: ToolCall) -> Callable[[list[dict[str, Any]]], TurnResult]:
    return lambda _m: TurnResult(
        "", list(calls), "tool_use", {"role": "assistant", "content": [{"type": "tool_use", "id": c.id} for c in calls]}, _usage()
    )


def text_turn(text: str, stop: str = "end") -> Callable[[list[dict[str, Any]]], TurnResult]:
    return lambda _m: TurnResult(text, [], stop, {"role": "assistant", "content": text}, _usage())


def run(provider: ScriptedProvider, question: str = "Combien ce mois-ci ?") -> list[dict[str, Any]]:
    events: list[dict[str, Any]] = []
    api = UserAPI(CFG, "tok", ORG, transport=httpx.MockTransport(fake_api))
    svc = ServiceAPI(CFG, transport=httpx.MockTransport(fake_api))
    Assistant(provider, api, svc, {"id": ORG, "name": "Démo", "currency": "EUR"}, "fr").run(
        [ChatMessage(role="user", content=question)], events.append
    )
    return events


def types(events: list[dict[str, Any]]) -> list[str]:
    return [e["type"] for e in events]


def test_tool_loop_grounded_answer() -> None:
    CAPTURED.clear()
    p = ScriptedProvider([tool_turn(ToolCall("t1", "get_cost_summary", {})), text_turn("Vous avez dépensé **12 345,67 €** ce mois-ci.")])
    ev = run(p)
    assert types(ev) == ["tool_call", "text", "sources", "done"]
    assert ev[2]["sources"] == ["#1 get_cost_summary"]
    # Le résultat d'outil est réinjecté avant le second tour.
    assert p.seen[1][-1]["content"][0]["tool_use_id"] == "t1"
    # Consommation enregistrée pour l'organisation, coût en chaîne décimale.
    usage = [body for path, body in CAPTURED if path.endswith("/llm-usage")]
    assert usage and usage[0]["cost"] == "0.002" and usage[0]["feature"] == "assistant"


def test_ungrounded_answer_is_repaired_then_warned() -> None:
    p = ScriptedProvider(
        [
            tool_turn(ToolCall("t1", "get_cost_summary", {})),
            text_turn("Vous avez dépensé 99 999 € ce mois-ci."),
            text_turn("Environ 88 888 € ce mois-ci."),
        ]
    )
    ev = run(p)
    assert "reset" in types(ev)
    warning = next(e for e in ev if e["type"] == "warning")
    assert "88 888" in warning["message"]
    # La consigne de correction cite les chiffres non vérifiés.
    assert "99 999" in p.seen[2][-1]["content"]


def test_repair_succeeds_without_warning() -> None:
    p = ScriptedProvider(
        [
            tool_turn(ToolCall("t1", "get_cost_summary", {})),
            text_turn("Environ 99 999 €."),
            text_turn("La prévision de fin de mois est de 15 210,40 €."),
        ]
    )
    ev = run(p)
    assert "warning" not in types(ev)
    assert types(ev)[-2:] == ["sources", "done"]


def test_chart_values_must_come_from_tools() -> None:
    bad = ToolCall("c1", "render_chart", {"kind": "bar", "title": "x", "categories": ["a"], "series": [{"name": "s", "values": [4242.42]}]})
    good = ToolCall(
        "c2", "render_chart", {"kind": "bar", "title": "x", "categories": ["a", "b"], "series": [{"name": "s", "values": [12345.67, 15210.4]}]}
    )
    p = ScriptedProvider([tool_turn(ToolCall("t1", "get_cost_summary", {})), tool_turn(bad), tool_turn(good), text_turn("Voici le graphique.")])
    ev = run(p)
    charts = [e for e in ev if e["type"] == "chart"]
    assert len(charts) == 1 and charts[0]["spec"]["series"][0]["values"] == [12345.67, 15210.4]
    rejected = json.loads(p.seen[2][-1]["content"][0]["content"])
    assert rejected["values"] == ["4242.42"]


def test_invalid_tool_input_returns_error_result() -> None:
    p = ScriptedProvider([tool_turn(ToolCall("t1", "get_costs", {"granularity": "hourly", "oops": 1})), text_turn("Je ne peux pas répondre.")])
    run(p)
    result = p.seen[1][-1]["content"][0]
    assert result["is_error"] is True and "INVALID_INPUT" in result["content"]


def test_max_tokens_does_not_run_tools() -> None:
    p = ScriptedProvider([text_turn("Réponse partielle", stop="max_tokens")])
    ev = run(p)
    assert "tool_call" not in types(ev)
    assert any(e["type"] == "warning" for e in ev)


def test_provider_error_is_reported() -> None:
    p = ScriptedProvider([lambda _m: TurnResult("", [], "error", {}, Usage("fake", "fake-1"), error="Fournisseur injoignable.")])
    ev = run(p)
    assert ev[-1] == {"type": "error", "message": "Fournisseur injoignable."}


def test_echo_content_drops_declined_internal_blocks() -> None:
    def b(t: str, **kw: str) -> SimpleNamespace:
        return SimpleNamespace(type=t, **kw)

    content = [b("text", text="Début"), b("thinking"), b("tool_use", id="x"), b("fallback"), b("text", text="Suite"), b("tool_use", id="y")]
    kept = echo_content(content)
    assert [(k.type, getattr(k, "id", getattr(k, "text", ""))) for k in kept] == [("text", "Début"), ("text", "Suite"), ("tool_use", "y")]
    assert echo_content(content[:3]) == content[:3]


def test_anthropic_usage_cost_is_decimal() -> None:
    prov = AnthropicProvider(CFG, client=SimpleNamespace())  # type: ignore[arg-type]
    msg = SimpleNamespace(
        model="claude-opus-5",
        usage=SimpleNamespace(input_tokens=1000, output_tokens=2000, cache_read_input_tokens=10000, cache_creation_input_tokens=0),
    )
    u = prov._usage(msg)
    # 1000×5 + 10000×0,5 + 2000×25 = 60 000 / 1 M = 0,06 $
    assert u.cost == Decimal("0.060000") and u.input_tokens == 11000


def test_openai_compatible_provider_parses_tool_calls() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        body = json.loads(request.content)
        assert body["messages"][0]["role"] == "system" and body["tools"]
        return httpx.Response(
            200,
            json={
                "model": "mistral-large",
                "choices": [
                    {
                        "finish_reason": "tool_calls",
                        "message": {
                            "content": "",
                            "tool_calls": [{"id": "c1", "function": {"name": "get_costs", "arguments": '{"group_by": ["provider"]}'}}],
                        },
                    }
                ],
                "usage": {"prompt_tokens": 1000, "completion_tokens": 100},
            },
        )

    prov = OpenAICompatibleProvider(
        "mistral", "http://llm.test/v1", "k", "mistral-large", Decimal(2), Decimal(6), "EUR", httpx.MockTransport(handler)
    )
    res = prov.turn("sys", [{"role": "user", "content": "q"}], lambda _t: None)
    assert res.stop == "tool_use" and res.tool_calls[0].input == {"group_by": ["provider"]}
    assert res.usage.cost == Decimal("0.002600") and res.usage.currency == "EUR"


def test_provider_policy() -> None:
    with pytest.raises(ProviderUnavailable):
        for_org(CFG, "anthropic", allow_external=False)
    with pytest.raises(ProviderUnavailable):
        for_org(CFG, "none", allow_external=True)
    with pytest.raises(ProviderUnavailable):
        for_org(Settings(mistral_key=""), "mistral", allow_external=False)
    assert for_org(CFG, "local", allow_external=False).name == "local"


def test_report_facts_template_and_pdf() -> None:
    CAPTURED.clear()
    api = ServiceAPI(CFG, transport=httpx.MockTransport(fake_api))
    org = api.org(ORG)
    facts = reports.collect(api, org, "2026-08", today=date(2026, 9, 25))
    assert not facts.partial
    assert facts.total == Decimal("7100.50") and facts.previous_total == Decimal("6100.50")
    assert facts.change_percent == Decimal("16.4")
    # Les coûts d'un environnement remontent à son équipe.
    assert [(t["name"], t["amount"]) for t in facts.by_team] == [("Data", Decimal("5000.00")), ("Shop", Decimal("2100.50"))]
    assert facts.variations[0]["name"] == "Data" and facts.variations[0]["delta"] == Decimal("1000.00")
    assert facts.actions[0]["title"] == "Réduire vm-batch-01"
    n = reports.template(facts, "fr")
    assert "7 100,50 €" in n.headline and "+16,4" in n.headline
    pdf = reports.render_pdf(CFG, facts, n, "fr", "template")
    assert pdf.startswith(b"%PDF") and len(pdf) > 1000


def test_report_without_complete_previous_month_has_no_comparison() -> None:
    PREVIOUS_DATA_FROM["day"] = "2026-07-27"
    try:
        api = ServiceAPI(CFG, transport=httpx.MockTransport(fake_api))
        facts = reports.collect(api, api.org(ORG), "2026-08", today=date(2026, 9, 25))
    finally:
        PREVIOUS_DATA_FROM.clear()
    assert not facts.previous_complete and facts.change_percent is None
    assert facts.variations == [] and all(t["change_percent"] is None for t in facts.by_team)
    assert "pas de comparaison" in reports.template(facts, "fr").headline


def test_report_generate_uploads_with_ai_narrative() -> None:
    CAPTURED.clear()
    api = ServiceAPI(CFG, transport=httpx.MockTransport(fake_api))
    narrative = {
        "headline": "Dépense de 7 100,50 € en août, en hausse de 16,4 %.",
        "summary": "La hausse vient surtout de l'équipe Data (5 000 €).",
        "variations": ["Data : +1 000 €."],
        "actions": ["Réduire vm-batch-01 : 420 € par mois.", "Supprimer le volume orphelin : 35,10 € par mois.", "Revoir les budgets."],
    }
    p = ScriptedProvider([text_turn(json.dumps(narrative, ensure_ascii=False))])
    summary = reports.generate(CFG, api, ORG, "2026-08", provider=p, today=date(2026, 9, 25))
    assert summary["generated_by"] == "fake" and summary["headline"] == narrative["headline"]
    upload = next(body for path, body in CAPTURED if path.endswith("/reports/2026-08"))
    assert upload["status"] == "ready" and upload["pdf_base64"]


def test_report_rejects_invented_numbers() -> None:
    api = ServiceAPI(CFG, transport=httpx.MockTransport(fake_api))
    facts = reports.collect(api, api.org(ORG), "2026-08", today=date(2026, 9, 25))
    bad = {"headline": "Dépense de 9 999 €.", "summary": "…", "variations": [], "actions": []}
    n, _ = reports.narrate(ScriptedProvider([text_turn(json.dumps(bad))]), facts, "fr")
    assert n is None


def test_mcp_org_resolution() -> None:
    from mcp.server.auth.provider import AccessToken

    one = AccessToken(token="t", client_id="kairn", scopes=[], claims={"orgs": [ORG]})
    assert _resolve_org(one, None) == ORG
    with pytest.raises(OrgError):
        _resolve_org(one, "0190f5a0-0000-7000-8000-000000000099")
    many = AccessToken(token="t", client_id="kairn", scopes=[], claims={"orgs": [ORG, "other"]})
    with pytest.raises(OrgError):
        _resolve_org(many, None)


def test_anthropic_without_credentials_is_unavailable(monkeypatch: pytest.MonkeyPatch) -> None:
    for var in ("ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"):
        monkeypatch.delenv(var, raising=False)
    with pytest.raises(ProviderUnavailable):
        AnthropicProvider(CFG)


def test_report_white_label_from_msp_parent() -> None:
    msp = "0190f5a0-0000-7000-8000-0000000000aa"
    EXTRA_ORGS.append(
        {
            "id": msp,
            "name": "Infogérance SA",
            "settings": {
                "white_label": {"company_name": "Infogérance\r\nBcc: x@y", "primary_color": "#0F766E", "support_email": "support@infogerance.example"}
            },
        }
    )
    try:
        api = ServiceAPI(CFG, transport=httpx.MockTransport(fake_api))
        client = {"id": "client", "name": "Client", "parent_org_id": msp, "settings": {}}
        brand = reports.brand_for(api, client)
        # La marque du MSP parent s'applique ; aucun caractère de contrôle (injection d'en-têtes e-mail).
        assert brand.white_label and brand.name == "InfogéranceBcc: x@y" and brand.color == (15, 118, 110)
        assert brand.support_email == "support@infogerance.example"
        # L'organisation cliente peut porter sa propre marque, prioritaire sur celle du MSP.
        own = reports.brand_for(api, {**client, "settings": {"white_label": {"company_name": "Ma marque", "primary_color": "pas une couleur"}}})
        assert own.name == "Ma marque" and own.color == reports.Brand().color
        assert reports.brand_for(api, {"id": ORG, "settings": {}}) == reports.Brand()
        facts = reports.collect(api, api.org(ORG), "2026-08", today=date(2026, 9, 25))
        pdf = reports.render_pdf(CFG, facts, reports.template(facts, "fr"), "fr", "template", brand)
        assert pdf.startswith(b"%PDF")
    finally:
        EXTRA_ORGS.clear()
