"""Outils typés de l'assistant et du serveur MCP (CLAUDE.md §6).

Chaque outil :
- a un schéma JSON strict (validation Pydantic avant exécution) ;
- appelle une route publique de l'API Kairn avec le jeton de l'utilisateur,
  donc avec ses droits (aucun SQL, aucun accès direct aux bases) ;
- renvoie un JSON compact et un identifiant de source citable.
"""

from __future__ import annotations

import json
from collections.abc import Callable
from datetime import UTC, datetime, timedelta
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field, ValidationError

from .kairn import KairnError, UserAPI

MAX_ROWS = 60


class _In(BaseModel):
    model_config = ConfigDict(extra="forbid")


class Period(_In):
    start: str | None = Field(default=None, description="Début inclus, AAAA-MM-JJ (défaut : J-30)")
    end: str | None = Field(default=None, description="Fin exclue, AAAA-MM-JJ (défaut : demain)")


class GetCostSummary(_In):
    pass


class GetCosts(Period):
    group_by: list[str] = Field(
        default_factory=list,
        description=(
            "Dimensions : provider, resource_type, region, cost_type, allocation_node_id, resource_id, connector_id, source, sku, label:<clé>"
        ),
    )
    filters: list[str] = Field(default_factory=list, description="Filtres dimension:valeur, ex. provider:openstack, label:team:shop")
    granularity: Literal["day", "week", "month", "total"] = "total"


class GetUsage(Period):
    resource_ids: list[str] = Field(description="Identifiants de ressources (voir search_resources)")
    metrics: list[str] = Field(default_factory=lambda: ["cpu.utilization", "mem.utilization"])
    step: str = "1d"
    agg: Literal["avg", "max", "min", "p95", "sum", "last"] = "avg"


class GetEfficiency(_In):
    level: Literal["resource", "workload", "node"] = "resource"


class ListRecommendations(_In):
    status: Literal["open", "accepted", "postponed", "dismissed", "applied"] = "open"
    type: str | None = None
    limit: int = Field(default=10, ge=1, le=50)


class GetAnomalies(Period):
    status: Literal["open", "acknowledged", "resolved"] | None = None


class GetEvents(Period):
    kinds: list[str] = Field(default_factory=list, description="deployment, hpa_scale, incident, inventory_change, k8s_event")


class GetBudgetStatus(_In):
    node_id: str | None = None


class GetForecast(_In):
    node_id: str | None = None


class SearchResources(_In):
    query: str | None = None
    type: str | None = Field(default=None, description="compute.instance, storage.volume, k8s.workload, k8s.pod…")
    limit: int = Field(default=20, ge=1, le=100)


class ListAllocationNodes(_In):
    pass


class ChartSeries(_In):
    name: str
    values: list[float]


class RenderChart(_In):
    kind: Literal["bar", "line", "donut"]
    title: str
    categories: list[str]
    series: list[ChartSeries]
    currency: str = "EUR"


def _day(s: str | None, default: datetime) -> str:
    if not s:
        return default.strftime("%Y-%m-%dT00:00:00Z")
    return s[:10] + "T00:00:00Z"


def _window(p: Period, days: int = 30) -> tuple[str, str]:
    now = datetime.now(tz=UTC).replace(hour=0, minute=0, second=0, microsecond=0)
    return _day(p.start, now - timedelta(days=days - 1)), _day(p.end, now + timedelta(days=1))


class ToolSpec:
    def __init__(self, name: str, description: str, model: type[_In], run: Callable[[UserAPI, Any], Any]) -> None:
        self.name = name
        self.description = description
        self.model = model
        self.run = run

    def schema(self) -> dict[str, Any]:
        s = self.model.model_json_schema()
        s.pop("title", None)
        s.setdefault("properties", {})
        s["additionalProperties"] = False
        return s


def _nodes(api: UserAPI) -> dict[str, str]:
    try:
        res = api.get("/orgs/{org}/allocation/nodes", {"limit": 500})
    except KairnError:
        return {}
    return {n["id"]: n["name"] for n in res.get("items") or []}


def _run_summary(api: UserAPI, _: GetCostSummary) -> Any:
    s = api.get("/orgs/{org}/costs/summary")
    names = _nodes(api)
    for r in s.get("by_node") or []:
        r["node_name"] = names.get(r["keys"].get("allocation_node_id", ""), "non alloué")
    s["daily"] = (s.get("daily") or [])[-14:]
    return s


def _run_costs(api: UserAPI, a: GetCosts) -> Any:
    start, end = _window(a)
    res = api.get("/orgs/{org}/costs", {"from": start, "to": end, "granularity": a.granularity, "group_by": a.group_by, "filter": a.filters})
    rows = res.get("rows") or []
    if "allocation_node_id" in a.group_by:
        names = _nodes(api)
        for r in rows:
            r["node_name"] = names.get(r["keys"].get("allocation_node_id", ""), "non alloué")
    res["rows"] = rows[:MAX_ROWS]
    res["truncated"] = len(rows) > MAX_ROWS
    return res


def _run_usage(api: UserAPI, a: GetUsage) -> Any:
    start, end = _window(a, 14)
    series = api.get(
        "/orgs/{org}/usage", {"resource_id": a.resource_ids[:20], "metric": a.metrics, "from": start, "to": end, "step": a.step, "agg": a.agg}
    )
    for s in series:
        pts = s.get("points") or []
        vals = [p["value"] for p in pts]
        s["summary"] = {"min": min(vals, default=0), "max": max(vals, default=0), "avg": round(sum(vals) / len(vals), 4) if vals else 0}
        s["points"] = pts[-31:]
    return series


def _run_efficiency(api: UserAPI, a: GetEfficiency) -> Any:
    return (api.get("/orgs/{org}/efficiency", {"level": a.level}) or [])[:MAX_ROWS]


def _run_recos(api: UserAPI, a: ListRecommendations) -> Any:
    res = api.get("/orgs/{org}/recommendations", {"status": [a.status], "type": [a.type] if a.type else None, "limit": a.limit})
    items = res.get("items") or []
    for r in items:
        rem = r.get("remediation") or {}
        r["remediation"] = {"steps": rem.get("steps", []), "cli": rem.get("cli", "")}
    return {"items": items, "summary": api.get("/orgs/{org}/recommendations/summary")}


def _run_anomalies(api: UserAPI, a: GetAnomalies) -> Any:
    start, _ = _window(a, 90)
    items = api.get("/orgs/{org}/anomalies", {"from": start, "status": a.status}) or []
    return items[:20]


def _run_events(api: UserAPI, a: GetEvents) -> Any:
    start, end = _window(a, 7)
    evs = api.get("/orgs/{org}/events", {"from": start, "to": end, "kind": a.kinds, "limit": 200}) or []
    return evs[-80:]


def _run_budgets(api: UserAPI, a: GetBudgetStatus) -> Any:
    items = api.get("/orgs/{org}/budgets-status") or []
    if a.node_id:
        items = [b for b in items if (b.get("budget") or {}).get("node_id") == a.node_id]
    return items


def _run_forecast(api: UserAPI, a: GetForecast) -> Any:
    res = api.get("/orgs/{org}/forecast", {"node_id": a.node_id})
    res["history"] = (res.get("history") or [])[-14:]
    return res


def _run_search(api: UserAPI, a: SearchResources) -> Any:
    res = api.get("/orgs/{org}/resources", {"q": a.query, "type": [a.type] if a.type else None, "limit": a.limit})
    return [
        {k: r.get(k) for k in ("id", "name", "type", "provider", "region", "labels", "cost_30d", "currency")}
        | {"flavor": (r.get("attributes") or {}).get("flavor")}
        for r in res.get("items") or []
    ]


def _run_nodes(api: UserAPI, _: ListAllocationNodes) -> Any:
    res = api.get("/orgs/{org}/allocation/nodes", {"limit": 500})
    return [{k: n.get(k) for k in ("id", "parent_id", "kind", "name")} for n in res.get("items") or []]


def _run_chart(_: UserAPI, a: RenderChart) -> Any:
    return {"rendered": True, "title": a.title}


TOOLS: list[ToolSpec] = [
    ToolSpec(
        "get_cost_summary",
        "Vue d'ensemble du mois : dépense à date, comparaison au mois précédent, prévision de fin de mois, "
        "répartition par fournisseur, équipe et type de coût, principales variations, économies potentielles.",
        GetCostSummary,
        _run_summary,
    ),
    ToolSpec(
        "get_costs",
        "Coûts agrégés sur une période, regroupés par dimensions et filtrés. Montants en chaînes décimales dans la devise de l'organisation.",
        GetCosts,
        _run_costs,
    ),
    ToolSpec("get_usage", "Séries d'utilisation (CPU, mémoire, IOPS, réseau, requêtes/s) de ressources, avec min/max/moyenne.", GetUsage, _run_usage),
    ToolSpec(
        "get_efficiency",
        "Coût × utilisation sur 30 jours : efficience et gaspillage estimé par VM, workload Kubernetes ou équipe.",
        GetEfficiency,
        _run_efficiency,
    ),
    ToolSpec(
        "list_recommendations",
        "Recommandations d'optimisation avec économie mensuelle, risque, preuves et commande d'application.",
        ListRecommendations,
        _run_recos,
    ),
    ToolSpec(
        "get_anomalies",
        "Anomalies de coût détectées, avec valeurs attendue et observée, événements corrélés et explication.",
        GetAnomalies,
        _run_anomalies,
    ),
    ToolSpec(
        "get_events", "Événements d'une fenêtre : déploiements, mises à l'échelle HPA, incidents, changements d'inventaire.", GetEvents, _run_events
    ),
    ToolSpec("get_budget_status", "État des budgets : montant, réel, prévision et pourcentages.", GetBudgetStatus, _run_budgets),
    ToolSpec(
        "get_forecast", "Prévision de fin de mois avec intervalle de confiance (organisation ou nœud d'allocation).", GetForecast, _run_forecast
    ),
    ToolSpec("search_resources", "Recherche dans l'inventaire (nom, type) avec le coût des 30 derniers jours.", SearchResources, _run_search),
    ToolSpec(
        "list_allocation_nodes",
        "Arbre d'allocation : organisation, business units, équipes, services, environnements.",
        ListAllocationNodes,
        _run_nodes,
    ),
    ToolSpec(
        "render_chart",
        "Affiche un graphique dans la réponse. Les valeurs doivent provenir de résultats d'outils déjà obtenus.",
        RenderChart,
        _run_chart,
    ),
]

BY_NAME = {t.name: t for t in TOOLS}


def anthropic_tools() -> list[dict[str, Any]]:
    """Définitions au format de l'API Anthropic (ordre stable pour le cache de prompt)."""
    return [{"name": t.name, "description": t.description, "input_schema": t.schema(), "eager_input_streaming": True} for t in TOOLS]


def openai_tools() -> list[dict[str, Any]]:
    """Définitions au format « function calling » (Mistral, modèles locaux compatibles OpenAI)."""
    return [{"type": "function", "function": {"name": t.name, "description": t.description, "parameters": t.schema()}} for t in TOOLS]


class ToolOutcome(BaseModel):
    name: str
    ok: bool
    content: str  # JSON transmis au modèle
    data: Any = None
    source: str = ""


def execute(api: UserAPI, name: str, raw_input: Any, call_index: int) -> ToolOutcome:
    """Valide l'entrée puis exécute l'outil ; les erreurs deviennent des résultats d'erreur lisibles par le modèle."""
    spec = BY_NAME.get(name)
    if spec is None:
        return ToolOutcome(name=name, ok=False, content=json.dumps({"error": f"outil inconnu : {name}"}))
    try:
        args = spec.model.model_validate(raw_input or {})
    except ValidationError as exc:
        return ToolOutcome(name=name, ok=False, content=json.dumps({"error": "INVALID_INPUT", "details": exc.errors(include_url=False)}, default=str))
    try:
        data = spec.run(api, args)
    except KairnError as exc:
        msg = "accès refusé ou fonctionnalité non incluse dans le plan" if exc.status in (402, 403) else exc.detail
        return ToolOutcome(name=name, ok=False, content=json.dumps({"error": msg, "status": exc.status}))
    source = f"tool:{name}#{call_index}"
    return ToolOutcome(name=name, ok=True, content=json.dumps(data, ensure_ascii=False, default=str), data=data, source=source)
