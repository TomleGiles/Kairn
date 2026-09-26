from __future__ import annotations

import math
from datetime import UTC, datetime, timedelta
from decimal import Decimal

from kairn_analytics import recommendations as reco
from kairn_analytics.anomalies import DailySeries, detect
from kairn_analytics.correlation import correlate
from kairn_analytics.explain import template
from kairn_analytics.forecast import forecast_month
from kairn_analytics.models import CatalogItem, Edge, Event, Resource
from kairn_analytics.money import cents, fmt_eur
from kairn_analytics.stats import holt_winters, seasonal_baseline

T0 = datetime(2026, 7, 1, tzinfo=UTC)


def days(n: int) -> list[datetime]:
    return [T0 + timedelta(days=i) for i in range(n)]


def weekly(n: int, base: float = 100.0) -> list[float]:
    # Coût plus faible le week-end, légère variation déterministe.
    return [base * (0.8 if (T0 + timedelta(days=i)).weekday() >= 5 else 1.0) + (i % 3) for i in range(n)]


def test_baseline_weekday_factor() -> None:
    vals = weekly(28)
    wds = [d.weekday() for d in days(28)]
    sat = seasonal_baseline(vals, wds, 5)
    mon = seasonal_baseline(vals, wds, 0)
    assert sat.expected < mon.expected
    assert abs(mon.expected - 101) < 3


def test_detects_step_increase_once() -> None:
    vals = weekly(60)
    for i in range(50, 60):  # +40 % à partir du jour 50
        vals[i] *= 1.4
    s = DailySeries("cost:total", "l'organisation", {}, days(60), vals, min_delta=5)
    found = detect(s)
    assert len(found) == 1, "a sustained step is reported once"
    assert found[0].start == T0 + timedelta(days=50)
    assert found[0].end == T0 + timedelta(days=53)
    assert found[0].actual > found[0].expected
    assert found[0].severity in ("warning", "critical")


def test_no_false_positive_on_stable_series() -> None:
    s = DailySeries("cost:total", "org", {}, days(60), weekly(60), min_delta=5)
    assert detect(s) == []


def test_correlation_prefers_in_scope_deployment() -> None:
    vals = weekly(40)
    for i in range(35, 40):
        vals[i] *= 1.6
    f = detect(DailySeries("cost:node:n1", "Équipe Search", {"node_id": "n1"}, days(40), vals, min_delta=5))[0]
    evs = [
        Event(ts=f.start + timedelta(hours=10), kind="deployment", resource_id="wl-search", title="search-api v2.3.0 déployé"),
        Event(ts=f.start + timedelta(hours=11), kind="deployment", resource_id="other", title="shop v1.4 déployé"),
        Event(ts=f.start - timedelta(days=10), kind="deployment", resource_id="wl-search", title="trop ancien"),
    ]
    corr = correlate(f, evs, {"wl-search", "pod-1"})
    assert corr[0].event.title == "search-api v2.3.0 déployé"
    assert all(c.event.title != "trop ancien" for c in corr)
    text, sources = template(f, corr, "EUR")
    assert "search-api v2.3.0" in text and sources[0] == "series:cost:node:n1"


def test_holt_winters_tracks_weekly_pattern() -> None:
    hw = holt_winters(weekly(56), 14)
    assert len(hw.forecast) == 14
    sat = hw.forecast[(5 - (T0 + timedelta(days=56)).weekday()) % 7]
    assert sat < max(hw.forecast)
    assert all(v > 0 for v in hw.forecast)


def test_forecast_month_total_includes_actuals() -> None:
    ds = days(75)  # jusqu'au 13 septembre inclus
    vals = [Decimal(100)] * 75
    today = T0 + timedelta(days=75)  # 14 septembre
    fc = forecast_month(ds, vals, today, "EUR")
    # 13 jours réels à 100 + 17 jours prévus ≈ 100 → ≈ 3000
    assert abs(fc.total - Decimal(3000)) < Decimal(60)
    assert fc.lower <= fc.total <= fc.upper
    assert fc.period_end == datetime(2026, 10, 1, tzinfo=UTC)
    assert len(fc.points) == 17


def _catalog() -> list[CatalogItem]:
    def f(name: str, vcpus: int, ram: int, price: str, fam: str = "general") -> CatalogItem:
        return CatalogItem(provider="openstack", sku=f"compute.flavor.{name}", unit="hour", price=Decimal(price),
                           attributes={"vcpus": str(vcpus), "ram_gb": str(ram), "family": fam})
    return [
        f("b2-7", 2, 7, "0.0681"), f("b2-15", 4, 15, "0.1331"), f("b2-60", 16, 60, "0.5271"),
        CatalogItem(provider="openstack", sku="storage.volume.classic", unit="gb_month", price=Decimal("0.04")),
        CatalogItem(provider="openstack", sku="storage.volume.high-speed", unit="gb_month", price=Decimal("0.08")),
        CatalogItem(provider="openstack", sku="network.ip.floating", unit="hour", price=Decimal("0.0025")),
    ]


def _ctx(resources: list[Resource], series: dict[tuple[str, str], list[float]], night: dict[tuple[str, str], list[float]] | None = None,
         edges: list[Edge] | None = None) -> reco.Context:
    return reco.Context(now=T0 + timedelta(days=60), currency="EUR", locale="fr", window_days=14, percentile=95,
                        resources=resources, edges=edges or [], prices=reco.PriceBook.build(_catalog()), series=series,
                        night_series=night or {}, workload_pods={})


def vm(rid: str, flavor: str, vcpus: int, ram: int, **labels: str) -> Resource:
    return Resource(id=rid, provider="openstack", type="compute.instance", external_id=rid, name=rid,
                    attributes={"flavor": flavor, "vcpus": vcpus, "ram_gb": ram}, labels=labels, valid_from=T0)


def test_rightsizing_oversized_vm() -> None:
    r = vm("jupyter", "b2-60", 16, 60)
    hours = 14 * 24
    cpu = [0.04 + 0.02 * math.sin(i / 5) for i in range(hours)]
    mem = [0.12] * hours
    out = reco.rightsize_vms(_ctx([r], {("jupyter", "cpu.utilization"): cpu, ("jupyter", "mem.utilization"): mem}))
    assert len(out) == 1
    rec = out[0]
    assert rec.evidence["target_flavor"] == "b2-15"
    # (0,5271 − 0,1331) × 730 = 287,62
    assert rec.savings_monthly == Decimal("287.62")
    assert "openstack server resize --flavor b2-15 jupyter" in rec.remediation.cli
    assert rec.risk == "low"


def test_no_rightsizing_for_busy_vm_or_k8s_node() -> None:
    busy = vm("api", "b2-15", 4, 15)
    node_vm = vm("k8s-1", "b2-60", 16, 60)
    hours = 14 * 24
    series = {("api", "cpu.utilization"): [0.7] * hours, ("api", "mem.utilization"): [0.8] * hours,
              ("k8s-1", "cpu.utilization"): [0.05] * hours, ("k8s-1", "mem.utilization"): [0.1] * hours}
    edges = [Edge(parent_id="k8s-1", child_id="node-1", relation="backs", valid_from=T0)]
    assert reco.rightsize_vms(_ctx([busy, node_vm], series, edges=edges)) == []


def test_orphans_and_off_hours() -> None:
    vol = Resource(id="v1", provider="openstack", type="storage.volume", external_id="v1", name="old", valid_from=T0,
                   attributes={"size_gb": 250, "volume_type": "classic", "status": "available"})
    ip = Resource(id="ip1", provider="openstack", type="network.ip", external_id="ip1", name="1.2.3.4", valid_from=T0, attributes={})
    stg = vm("stg-web", "b2-7", 2, 7, env="staging")
    ctx = _ctx([vol, ip, stg], {}, night={("stg-web", "cpu.utilization"): [0.02] * 200})
    types = {r.type: r for r in reco.generate(ctx)}
    assert types["orphan_volume"].savings_monthly == Decimal("10.00")  # 250 × 0,04
    assert types["orphan_ip"].savings_monthly == cents(Decimal("0.0025") * 730)
    # 0,0681 × 470 h × 0,9
    assert types["off_hours_schedule"].savings_monthly == cents(Decimal("0.0681") * 470 * Decimal("0.9"))


def test_fmt_eur() -> None:
    assert fmt_eur(Decimal("1234.5")) == "1 234,50 €"
    assert fmt_eur(Decimal("1234.5"), "EUR", "en") == "€1,234.50"
