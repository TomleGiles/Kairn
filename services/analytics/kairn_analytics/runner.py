"""Exécution des analyses pour une organisation."""

from __future__ import annotations

import logging
from collections import defaultdict
from datetime import UTC, datetime, timedelta
from decimal import Decimal
from typing import Any
from zoneinfo import ZoneInfo

from . import recommendations as reco
from .anomalies import DailySeries, detect, title_for, to_decimal
from .client import KairnClient
from .correlation import correlate
from .explain import ai_explanation, template
from .forecast import forecast_month
from .models import AllocationNode, Anomaly, CostRow, Organization, Resource
from .money import cents

log = logging.getLogger(__name__)

HISTORY_DAYS = 63


def _day(dt: datetime) -> datetime:
    return dt.astimezone(UTC).replace(hour=0, minute=0, second=0, microsecond=0)


def _is_off_hours(ts: datetime, tz: ZoneInfo) -> bool:
    local = ts.astimezone(tz)
    return local.weekday() >= 5 or local.hour < 8 or local.hour >= 20


def run_org(client: KairnClient, org: Organization, now: datetime | None = None) -> dict[str, Any]:
    """Recommandations, anomalies et prévisions d'une organisation."""
    now = now or datetime.now(tz=UTC)
    summary: dict[str, Any] = {"org_id": org.id}
    resources = client.resources(org.id)
    edges = client.edges(org.id)
    if org.allows("recommendations"):
        summary["recommendations"] = _recommendations(client, org, now, resources, edges)
    if org.allows("anomalies"):
        summary["anomalies"] = _anomalies(client, org, now, resources, edges)
    if org.allows("forecast"):
        summary["forecasts"] = _forecasts(client, org, now)
    return summary


# ------------------------------------------------------------------ recommandations

def _recommendations(client: KairnClient, org: Organization, now: datetime, resources: list[Resource], edges: list[Any]) -> dict[str, int]:
    window = org.settings.rightsizing_window_days or 14
    pct = org.settings.rightsizing_percentile or 95
    start = now - timedelta(days=window)
    try:
        tz = ZoneInfo(org.timezone)
    except Exception:
        tz = ZoneInfo("Europe/Paris")
    vms = [r.id for r in resources if r.type == "compute.instance"]
    vols = [r.id for r in resources if r.type == "storage.volume"]
    pods = [r.id for r in resources if r.type == "k8s.pod"]
    series: dict[tuple[str, str], list[float]] = defaultdict(list)
    night: dict[tuple[str, str], list[float]] = defaultdict(list)
    for s in client.metrics(org.id, vms, ["cpu.utilization", "mem.utilization", "net.rx_bytes_per_sec"], start, now):
        for p in s.points:
            series[(s.resource_id, s.metric)].append(p.value)
            if _is_off_hours(p.ts, tz):
                night[(s.resource_id, s.metric)].append(p.value)
    for s in client.metrics(org.id, vols, ["disk.iops"], start, now):
        series[(s.resource_id, s.metric)] = [p.value for p in s.points]
    if pods:
        for s in client.metrics(org.id, pods, ["cpu.usage_cores", "mem.usage_bytes"], start, now):
            series[(s.resource_id, s.metric)] = [p.value for p in s.points]
    history: dict[str, list[Resource]] = {}
    for r in resources:
        if r.type == "storage.volume" and not r.attr("attached_to"):
            history[r.id] = client.history(org.id, r.id)
    ctx = reco.Context(
        now=now, currency=org.currency, locale=org.locale, window_days=window, percentile=pct, resources=resources,
        edges=edges, prices=reco.PriceBook.build(client.catalog(org.id)), series=dict(series), night_series=dict(night),
        workload_pods=reco.build_workload_pods(resources, edges), history=history,
    )
    recos = reco.generate(ctx)
    measured = _measured_savings(client, org, now)
    res = client.publish_recommendations(org.id, recos, reco.ALL_TYPES, measured)
    log.info("org %s: %d recommendations (%s)", org.id, len(recos), res)
    return res


def _measured_savings(client: KairnClient, org: Organization, now: datetime) -> dict[str, str]:
    """Économie mesurée : coût 7 jours avant l'application vs 7 jours après."""
    out: dict[str, str] = {}
    for r in client.recommendations(org.id):
        if r.status != "applied" or r.applied_at is None or now - r.applied_at < timedelta(days=7):
            continue
        applied = _day(r.applied_at)
        before = client.costs(org.id, applied - timedelta(days=7), applied, "total", filters={"resource_id": [r.resource_id]})
        after = client.costs(org.id, applied + timedelta(days=1), applied + timedelta(days=8), "total", filters={"resource_id": [r.resource_id]})
        b = sum((x.amount for x in before), Decimal(0))
        a = sum((x.amount for x in after), Decimal(0))
        out[r.id] = str(cents((b - a) * Decimal(30) / Decimal(7)))
    return out


# ------------------------------------------------------------------ anomalies

def _series_from_rows(rows: list[CostRow], key_dim: str | None, days: list[datetime]) -> dict[str, list[float]]:
    idx = {d: i for i, d in enumerate(days)}
    out: dict[str, list[float]] = defaultdict(lambda: [0.0] * len(days))
    for r in rows:
        d = _day(r.period)
        if d not in idx:
            continue
        k = r.keys.get(key_dim, "") if key_dim else "total"
        out[k][idx[d]] += float(r.amount)
    return dict(out)


def _anomalies(client: KairnClient, org: Organization, now: datetime, resources: list[Resource], edges: list[Any]) -> dict[str, int]:
    today = _day(now)
    start = today - timedelta(days=HISTORY_DAYS)
    days = [start + timedelta(days=i) for i in range(HISTORY_DAYS)]
    nodes = {n.id: n.name for n in _nodes(client, org)}
    by_node_res = client.costs(org.id, today - timedelta(days=30), today, "total", group_by=["allocation_node_id", "resource_id"])
    node_members: dict[str, set[str]] = defaultdict(set)
    for row in by_node_res:
        node_members[row.keys.get("allocation_node_id", "")].add(row.keys.get("resource_id", ""))
    parents: dict[str, set[str]] = defaultdict(set)
    for e in edges:
        parents[e.child_id].add(e.parent_id)

    candidates: list[DailySeries] = []
    total = _series_from_rows(client.costs(org.id, start, today, "day"), None, days)
    for vals in total.values():
        candidates.append(DailySeries("cost:total", "l'organisation", {}, days, vals, min_delta=10.0))
    for node, vals in _series_from_rows(client.costs(org.id, start, today, "day", ["allocation_node_id"]), "allocation_node_id", days).items():
        label = nodes.get(node, "non alloué" if node == "unallocated" else node)
        candidates.append(DailySeries(f"cost:node:{node}", label, {"node_id": node}, days, vals, min_delta=0.5))
    # Workloads Kubernetes : rattachés à leur ressource pour la corrélation avec les déploiements.
    workloads = {(r.attr("k8s.namespace"), r.name): r.id for r in resources if r.type == "k8s.workload"}
    wl_rows = client.costs(org.id, start, today, "day", ["label:k8s.namespace", "label:k8s.workload"], {"cost_type": ["k8s_workload"]})
    by_wl: dict[tuple[str, str], list[float]] = defaultdict(lambda: [0.0] * len(days))
    day_idx = {d: i for i, d in enumerate(days)}
    for row in wl_rows:
        key = (row.keys.get("label:k8s.namespace", ""), row.keys.get("label:k8s.workload", ""))
        if key[1] and _day(row.period) in day_idx:
            by_wl[key][day_idx[_day(row.period)]] += float(row.amount)
    for (ns, wl), vals in by_wl.items():
        rid = workloads.get((ns, wl), "")
        wl_scope = {"resource_id": rid} if rid else {}
        candidates.append(DailySeries(f"cost:workload:{ns}/{wl}", f"{ns}/{wl}", wl_scope, days, vals, min_delta=0.3))
    providers = _series_from_rows(client.costs(org.id, start, today, "day", ["provider"]), "provider", days)
    active = {p: v for p, v in providers.items() if sum(v) > 0}
    if len(active) > 1:  # avec un seul fournisseur, la série double le total
        for prov, vals in active.items():
            candidates.append(DailySeries(f"cost:provider:{prov}", f"{prov}", {"provider": prov}, days, vals, min_delta=5.0))

    anomalies: list[Anomaly] = []
    events = client.events(org.id, today - timedelta(days=30), now)
    for s in candidates:
        for f in detect(s):
            scope: set[str] = set()
            if "node_id" in s.scope:
                scope = set(node_members.get(s.scope["node_id"], set()))
            elif "resource_id" in s.scope:
                scope = {s.scope["resource_id"]}
            for rid in list(scope):
                scope |= parents.get(rid, set())
            corr = correlate(f, events, scope)
            text, sources = template(f, corr, org.currency, org.locale)
            ai = ai_explanation(org, f, corr, text)
            anomalies.append(Anomaly(
                series_key=s.key, kind="cost", title=title_for(f, org.currency), window_start=f.start, window_end=f.end,
                severity=f.severity, expected=to_decimal(f.expected), actual=to_decimal(f.actual), currency=org.currency,
                score=round(f.score, 3), correlated_events=corr, explanation=ai or text, explanation_sources=sources,
            ))
    res = client.publish_anomalies(org.id, anomalies) if anomalies else {"created": 0, "updated": 0}
    log.info("org %s: %d anomalies (%s)", org.id, len(anomalies), res)
    return res


def _nodes(client: KairnClient, org: Organization) -> list[AllocationNode]:
    return client.allocation_nodes(org.id)


# ------------------------------------------------------------------ prévisions

def _forecasts(client: KairnClient, org: Organization, now: datetime) -> dict[str, int]:
    today = _day(now)
    start = today - timedelta(days=90)
    days = [start + timedelta(days=i) for i in range(90)]
    out = []
    total = _series_from_rows(client.costs(org.id, start, today, "day"), None, days).get("total", [0.0] * 90)
    out.append(forecast_month(days, [Decimal(str(round(v, 6))) for v in total], today, org.currency, ""))
    nodes = _nodes(client, org)
    by_node = _series_from_rows(client.costs(org.id, start, today, "day", ["allocation_node_id"]), "allocation_node_id", days)
    # Prévision par nœud : chaque nœud agrège son sous-arbre.
    children: dict[str, list[str]] = defaultdict(list)
    for n in nodes:
        if n.parent_id:
            children[n.parent_id].append(n.id)

    def subtree(nid: str) -> list[str]:
        acc = [nid]
        for c in children.get(nid, []):
            acc += subtree(c)
        return acc

    for n in nodes:
        vals = [0.0] * 90
        for sub in subtree(n.id):
            for i, v in enumerate(by_node.get(sub, [])):
                vals[i] += v
        if sum(vals) <= 0:
            continue
        out.append(forecast_month(days, [Decimal(str(round(v, 6))) for v in vals], today, org.currency, n.id))
    res = client.publish_forecasts(org.id, out)
    log.info("org %s: %d forecasts", org.id, len(out))
    return res


def run_all(client: KairnClient) -> list[dict[str, Any]]:
    results = []
    for org in client.orgs():
        try:
            results.append(run_org(client, org))
        except Exception as exc:
            log.exception("analytics failed for org %s", org.id)
            results.append({"org_id": org.id, "error": str(exc)})
    return results

