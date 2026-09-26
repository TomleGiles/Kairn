"""Serveur MCP Kairn (M-10 / M-12).

Expose aux agents IA des clients (Claude, etc.) les mêmes outils typés que
l'assistant. Authentification : jeton d'API Kairn (`Authorization: Bearer
kairn_…`) ou session ; chaque appel est rejoué sur l'API publique avec ce
jeton, donc avec exactement les permissions, scopes, limites de plan et
l'isolation de l'utilisateur. Aucun appel LLM n'est fait côté Kairn.
"""

from __future__ import annotations

import json
from functools import partial
from typing import Any

import anyio
import httpx
from mcp.server.auth.middleware.auth_context import get_access_token
from mcp.server.auth.provider import AccessToken
from mcp.server.auth.settings import AuthSettings
from mcp.server.mcpserver import MCPServer
from mcp.types import ToolAnnotations

from . import __version__
from .config import Settings
from .kairn import UserAPI
from .tools import BY_NAME, execute

INSTRUCTIONS = """Kairn : observabilité des coûts cloud (OpenStack, Kubernetes, OVHcloud, Scaleway, Outscale).
Outils en lecture seule sur les coûts, l'usage, l'efficience, les recommandations, les anomalies, les événements, les budgets et les prévisions.
Les montants sont des chaînes décimales dans la devise de l'organisation. Si l'utilisateur a accès à plusieurs organisations,
appelez list_organizations puis passez org_id aux autres outils."""


class KairnTokenVerifier:
    """Valide le jeton auprès de l'API Kairn (GET /api/v1/me)."""

    def __init__(self, cfg: Settings, transport: httpx.AsyncBaseTransport | None = None) -> None:
        self._cfg = cfg
        self._transport = transport

    async def verify_token(self, token: str) -> AccessToken | None:
        async with httpx.AsyncClient(base_url=self._cfg.api_url, timeout=15, transport=self._transport) as c:
            try:
                r = await c.get("/api/v1/me", headers={"Authorization": f"Bearer {token}"})
            except httpx.HTTPError:
                return None
        if r.status_code != 200:
            return None
        me = r.json()
        orgs = sorted((me.get("permissions") or {}).keys())
        return AccessToken(
            token=token,
            client_id="kairn",
            scopes=[],
            subject=str((me.get("user") or {}).get("id") or ""),
            claims={
                "orgs": orgs,
                "memberships": [{"org_id": m.get("org_id"), "name": m.get("org_name"), "role": m.get("role")} for m in me.get("memberships") or []],
            },
        )


class OrgError(ValueError):
    pass


def _resolve_org(tok: AccessToken, org_id: str | None) -> str:
    orgs: list[str] = list((tok.claims or {}).get("orgs") or [])
    if org_id:
        if org_id not in orgs:
            raise OrgError("organisation inconnue ou inaccessible avec ce jeton")
        return org_id
    if len(orgs) == 1:
        return orgs[0]
    raise OrgError("plusieurs organisations accessibles : appelez list_organizations puis précisez org_id")


def build(cfg: Settings, *, transport: httpx.BaseTransport | None = None, verifier: KairnTokenVerifier | None = None) -> MCPServer:
    base = cfg.public_url
    server: MCPServer = MCPServer(
        name="kairn",
        title="Kairn",
        version=__version__,
        instructions=INSTRUCTIONS,
        website_url=base,
        token_verifier=verifier or KairnTokenVerifier(cfg),
        auth=AuthSettings(issuer_url=base, resource_server_url=f"{base}/mcp", validate_token_resource=False),
    )
    ro = ToolAnnotations(readOnlyHint=True, openWorldHint=False)

    def run_tool(token: str, org: str, name: str, args: dict[str, Any]) -> str:
        api = UserAPI(cfg, token, org, transport=transport)
        try:
            return execute(api, name, {k: v for k, v in args.items() if v is not None}, 1).content
        finally:
            api.close()

    async def call(name: str, org_id: str | None, args: dict[str, Any]) -> str:
        tok = get_access_token()  # variable de contexte : lue avant de passer dans un thread
        if tok is None:
            return json.dumps({"error": "authentification requise"})
        try:
            org = _resolve_org(tok, org_id)
        except OrgError as exc:
            return json.dumps({"error": str(exc)})
        return await anyio.to_thread.run_sync(partial(run_tool, tok.token, org, name, args))

    @server.tool(annotations=ro, description="Organisations accessibles avec ce jeton (identifiant, nom, rôle).")
    def list_organizations() -> str:
        tok = get_access_token()
        if tok is None:
            return json.dumps({"error": "authentification requise"})
        return json.dumps((tok.claims or {}).get("memberships") or [{"org_id": o} for o in (tok.claims or {}).get("orgs") or []], ensure_ascii=False)

    @server.tool(annotations=ro, description=BY_NAME["get_cost_summary"].description)
    async def get_cost_summary(org_id: str | None = None) -> str:
        return await call("get_cost_summary", org_id, {})

    @server.tool(annotations=ro, description=BY_NAME["get_costs"].description)
    async def get_costs(
        org_id: str | None = None,
        start: str | None = None,
        end: str | None = None,
        group_by: list[str] | None = None,
        filters: list[str] | None = None,
        granularity: str = "total",
    ) -> str:
        return await call("get_costs", org_id, {"start": start, "end": end, "group_by": group_by, "filters": filters, "granularity": granularity})

    @server.tool(annotations=ro, description=BY_NAME["get_usage"].description)
    async def get_usage(
        resource_ids: list[str],
        org_id: str | None = None,
        metrics: list[str] | None = None,
        start: str | None = None,
        end: str | None = None,
        step: str = "1d",
        agg: str = "avg",
    ) -> str:
        return await call(
            "get_usage", org_id, {"resource_ids": resource_ids, "metrics": metrics, "start": start, "end": end, "step": step, "agg": agg}
        )

    @server.tool(annotations=ro, description=BY_NAME["get_efficiency"].description)
    async def get_efficiency(org_id: str | None = None, level: str = "resource") -> str:
        return await call("get_efficiency", org_id, {"level": level})

    @server.tool(annotations=ro, description=BY_NAME["list_recommendations"].description)
    async def list_recommendations(org_id: str | None = None, status: str = "open", type: str | None = None, limit: int = 10) -> str:
        return await call("list_recommendations", org_id, {"status": status, "type": type, "limit": limit})

    @server.tool(annotations=ro, description=BY_NAME["get_anomalies"].description)
    async def get_anomalies(org_id: str | None = None, start: str | None = None, end: str | None = None, status: str | None = None) -> str:
        return await call("get_anomalies", org_id, {"start": start, "end": end, "status": status})

    @server.tool(annotations=ro, description=BY_NAME["get_events"].description)
    async def get_events(org_id: str | None = None, start: str | None = None, end: str | None = None, kinds: list[str] | None = None) -> str:
        return await call("get_events", org_id, {"start": start, "end": end, "kinds": kinds})

    @server.tool(annotations=ro, description=BY_NAME["get_budget_status"].description)
    async def get_budget_status(org_id: str | None = None, node_id: str | None = None) -> str:
        return await call("get_budget_status", org_id, {"node_id": node_id})

    @server.tool(annotations=ro, description=BY_NAME["get_forecast"].description)
    async def get_forecast(org_id: str | None = None, node_id: str | None = None) -> str:
        return await call("get_forecast", org_id, {"node_id": node_id})

    @server.tool(annotations=ro, description=BY_NAME["search_resources"].description)
    async def search_resources(org_id: str | None = None, query: str | None = None, type: str | None = None, limit: int = 20) -> str:
        return await call("search_resources", org_id, {"query": query, "type": type, "limit": limit})

    @server.tool(annotations=ro, description=BY_NAME["list_allocation_nodes"].description)
    async def list_allocation_nodes(org_id: str | None = None) -> str:
        return await call("list_allocation_nodes", org_id, {})

    return server
