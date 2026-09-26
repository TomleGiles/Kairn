"""Exécute le jeu d'évaluation de l'assistant contre une instance Kairn.

Usage (instance de démo : `make demo`) :
    KAIRN_API_URL=http://localhost:8080 KAIRN_EVAL_EMAIL=demo@kairn.local ANTHROPIC_API_KEY=… python evals/run.py

Sans clé de fournisseur LLM, l'évaluation est ignorée (code 0) ; en CI, la
variable KAIRN_EVAL_REQUIRED=1 la rend obligatoire. Chaque exécution consomme
des tokens : elle est déclenchée sur les branches principales, pas à chaque commit.
"""

from __future__ import annotations

import os
import sys
from decimal import Decimal
from pathlib import Path
from typing import Any

import httpx
import yaml

from kairn_ai import grounding
from kairn_ai.assistant import Assistant, ChatMessage
from kairn_ai.config import settings
from kairn_ai.kairn import UserAPI
from kairn_ai.providers import ProviderUnavailable, for_org


def login(api_url: str, email: str) -> str:
    r = httpx.post(f"{api_url}/api/v1/auth/login", json={"email": email}, timeout=30)
    r.raise_for_status()
    return str(r.json()["token"])


def reference(api: UserAPI, spec: dict[str, Any]) -> set[Decimal]:
    data = api.get(spec["path"], spec.get("params"))
    values: list[Any] = []
    if "fields" in spec:
        values = [data[f] for f in spec["fields"]]
    elif "items_field" in spec:
        values = [it[spec["items_field"]] for it in data.get("items") or []]
    return {Decimal(str(v)) for v in values}


def found(answer: str, expected: Decimal) -> bool:
    ref = grounding.value_variants(expected)
    return any({c.normalize() for c in cands} & ref for _, cands in grounding.numbers_in_text(answer))


def run_case(case: dict[str, Any], provider_name: str, allow_external: bool, api: UserAPI, org: dict[str, Any]) -> list[str]:
    cfg = settings()
    provider = for_org(cfg, provider_name, allow_external)
    events: list[dict[str, Any]] = []
    Assistant(provider, api, None, org, case.get("locale", "fr"), cfg.max_tool_turns).run(
        [ChatMessage(role="user", content=case["question"])], events.append
    )
    answer = ""
    for e in events:
        if e["type"] == "text":
            answer += e["text"]
        elif e["type"] == "reset":  # réponse remplacée après un tour de correction
            answer = ""
    tools = [e["name"] for e in events if e["type"] == "tool_call"]
    failures: list[str] = []
    if any(e["type"] == "error" for e in events):
        failures.append("erreur : " + next(e["message"] for e in events if e["type"] == "error"))
    if any(e["type"] == "warning" and e.get("numbers") for e in events):
        failures.append("chiffres non vérifiés : " + ", ".join(next(e["numbers"] for e in events if e.get("numbers"))))
    for t in case.get("expect_tools", []):
        if t not in tools:
            failures.append(f"outil attendu non appelé : {t}")
    if case.get("expect_tools_any") and not set(case["expect_tools_any"]) & set(tools):
        failures.append(f"aucun des outils attendus : {case['expect_tools_any']}")
    if case.get("expect_no_tools") and tools:
        failures.append(f"outils appelés hors périmètre : {tools}")
    if case.get("expect_no_numbers") and grounding.numbers_in_text(answer):
        failures.append("la réponse hors périmètre contient des chiffres")
    if case.get("expect_text_any") and not any(s.lower() in answer.lower() for s in case["expect_text_any"]):
        failures.append(f"aucun des textes attendus : {case['expect_text_any']}")
    for spec in case.get("expect_numbers", []):
        expected = reference(api, spec)
        if expected and not any(found(answer, v) for v in expected):
            failures.append(f"chiffre attendu absent ({spec['path']}) : {sorted(str(v) for v in expected)[:3]}")
    return failures


def main() -> int:
    if hasattr(sys.stdout, "reconfigure"):
        sys.stdout.reconfigure(encoding="utf-8")
    cfg = settings()
    provider_name = os.environ.get("KAIRN_EVAL_PROVIDER", "anthropic")
    token = os.environ.get("KAIRN_EVAL_TOKEN") or login(cfg.api_url, os.environ.get("KAIRN_EVAL_EMAIL", "demo@kairn.local"))
    me = httpx.get(f"{cfg.api_url}/api/v1/me", headers={"Authorization": f"Bearer {token}"}, timeout=30).json()
    membership = me["memberships"][0]
    org_id = os.environ.get("KAIRN_EVAL_ORG") or membership["org_id"]
    api = UserAPI(cfg, token, org_id)
    org = api.get("/orgs/{org}")
    try:
        for_org(cfg, provider_name, True)
    except ProviderUnavailable as exc:
        print(f"évaluation ignorée : {exc}")
        return 1 if os.environ.get("KAIRN_EVAL_REQUIRED") == "1" else 0
    cases = yaml.safe_load(Path(__file__).with_name("cases.yaml").read_text(encoding="utf-8"))["cases"]
    failed = 0
    for case in cases:
        failures = run_case(case, provider_name, True, api, org)
        status = "OK  " if not failures else "ÉCHEC"
        print(f"{status} {case['id']}")
        for f in failures:
            print(f"      - {f}")
        failed += bool(failures)
    print(f"\n{len(cases) - failed}/{len(cases)} cas réussis")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
