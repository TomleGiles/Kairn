// Package tsdbtest est la suite de contrat de tsdb.TSDB (mémoire et ClickHouse).
package tsdbtest

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/ids"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/tenancy"
	"github.com/kairn-io/kairn/pkg/tsdb"
)

// Run exécute la suite ; now doit être proche de l'horloge de l'implémentation (rétention).
func Run(t *testing.T, db tsdb.TSDB, now time.Time) {
	f := &fixture{db: db, orgA: ids.New(), orgB: ids.New(), now: now.UTC().Truncate(time.Hour)}
	f.ctxA = tenancy.WithOrg(context.Background(), f.orgA)
	f.ctxB = tenancy.WithOrg(context.Background(), f.orgB)
	t.Run("metrics", f.metrics)
	t.Run("costs", f.costs)
	t.Run("billing", f.billing)
	t.Run("events_uptime_units", f.eventsUptimeUnits)
	t.Run("purge", f.purge)
}

type fixture struct {
	db         tsdb.TSDB
	orgA, orgB string
	ctxA, ctxB context.Context
	now        time.Time
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func (f *fixture) metrics(t *testing.T) {
	r1, r2 := ids.New(), ids.New()
	base := f.now.Add(-6 * time.Hour)
	var pts []model.MetricPoint
	for i := 0; i < 12; i++ {
		ts := base.Add(time.Duration(i) * 30 * time.Minute)
		pts = append(pts, model.MetricPoint{ResourceID: r1, Metric: model.MetricCPUUtil, TS: ts, Value: float64(i) / 10},
			model.MetricPoint{ResourceID: r2, Metric: model.MetricCPUUtil, TS: ts, Value: 0.5})
	}
	must(t, f.db.WriteMetrics(f.ctxA, pts))
	// Réécriture d'un point : le dernier écrit gagne (ingestion idempotente).
	must(t, f.db.WriteMetrics(f.ctxA, []model.MetricPoint{{ResourceID: r1, Metric: model.MetricCPUUtil, TS: base, Value: 0.05}}))
	s, err := f.db.QueryMetrics(f.ctxA, tsdb.MetricQuery{ResourceIDs: []string{r1}, Metrics: []string{model.MetricCPUUtil}, From: base, To: f.now})
	must(t, err)
	if len(s) != 1 || len(s[0].Points) != 12 || s[0].Points[0].Value != 0.05 || !s[0].Points[0].TS.Equal(base) {
		t.Fatalf("raw series: %+v", s)
	}
	hourly, err := f.db.QueryMetrics(f.ctxA, tsdb.MetricQuery{ResourceIDs: []string{r1}, From: base, To: f.now, Step: time.Hour, Agg: tsdb.AggMax})
	must(t, err)
	if len(hourly) != 1 || len(hourly[0].Points) != 6 || hourly[0].Points[5].Value != 1.1 || !hourly[0].Points[1].TS.Equal(base.Add(time.Hour)) {
		t.Fatalf("hourly max: %+v", hourly)
	}
	all, err := f.db.QueryMetrics(f.ctxA, tsdb.MetricQuery{From: base, To: f.now, Step: 24 * time.Hour, Agg: tsdb.AggAvg})
	must(t, err)
	if len(all) != 2 {
		t.Fatalf("series count: %d", len(all))
	}
	if leak, _ := f.db.QueryMetrics(f.ctxB, tsdb.MetricQuery{From: base, To: f.now}); len(leak) != 0 {
		t.Fatalf("metrics leak")
	}
	if _, err := f.db.QueryMetrics(context.Background(), tsdb.MetricQuery{From: base, To: f.now}); err == nil {
		t.Fatalf("query without org must fail")
	}
	must(t, f.db.Rollup(f.ctxA, base, f.now))
}

func (f *fixture) costs(t *testing.T) {
	d1 := tsdb.TruncDay(f.now).AddDate(0, 0, -2)
	d2 := d1.AddDate(0, 0, 1)
	node := ids.New()
	line := func(res, typ, amount string, labels map[string]string) model.CostLine {
		return model.CostLine{ResourceID: res, ConnectorID: "c1", Provider: "openstack", ResourceType: model.TypeInstance, Region: "GRA", CostType: typ,
			SKU: "b2-7", Quantity: dec("24"), Unit: model.UnitHour, Amount: dec(amount), Currency: "EUR", Source: model.SourceEstimate,
			CatalogVersion: "v1", AllocationNodeID: node, Labels: labels}
	}
	must(t, f.db.ReplaceCostLines(f.ctxA, d1, []model.CostLine{
		line("r1", model.CostCompute, "1.634400", map[string]string{"team": "shop"}),
		line("r2", model.CostStorage, "0.100000", map[string]string{"team": "data"}),
	}))
	must(t, f.db.ReplaceCostLines(f.ctxA, d2, []model.CostLine{line("r1", model.CostCompute, "1.634400", map[string]string{"team": "shop"})}))
	// Remplacement : le jour d2 est recalculé avec d'autres lignes.
	must(t, f.db.ReplaceCostLines(f.ctxA, d2, []model.CostLine{
		line("r1", model.CostCompute, "0.1", map[string]string{"team": "shop"}), line("r3", model.CostCompute, "0.2", nil),
	}))
	to := d2.AddDate(0, 0, 1)
	total, err := f.db.QueryCosts(f.ctxA, tsdb.CostQuery{From: d1, To: to, Granularity: tsdb.GranTotal})
	must(t, err)
	if len(total) != 1 || !total[0].Amount.Equal(dec("2.0344")) || total[0].Currency != "EUR" {
		t.Fatalf("total: %+v", total)
	}
	daily, err := f.db.QueryCosts(f.ctxA, tsdb.CostQuery{From: d1, To: to, Granularity: tsdb.GranDay, GroupBy: []string{"cost_type"}})
	must(t, err)
	if len(daily) != 3 || !daily[0].Period.Equal(d1) || daily[0].Keys["cost_type"] != model.CostCompute || !daily[2].Amount.Equal(dec("0.3")) {
		t.Fatalf("daily by cost type: %+v", daily)
	}
	byLabel, err := f.db.QueryCosts(f.ctxA, tsdb.CostQuery{From: d1, To: to, Granularity: tsdb.GranTotal, GroupBy: []string{"label:team"},
		Filters: map[string][]string{"cost_type": {model.CostCompute}}})
	must(t, err)
	if len(byLabel) != 2 || byLabel[0].Keys["label:team"] != "shop" || !byLabel[0].Amount.Equal(dec("1.7344")) || byLabel[1].Keys["label:team"] != "" {
		t.Fatalf("by label: %+v", byLabel)
	}
	// Fin de fenêtre en cours de journée : le jour entamé est inclus.
	partial, _ := f.db.QueryCosts(f.ctxA, tsdb.CostQuery{From: d1, To: d2.Add(time.Hour), Granularity: tsdb.GranTotal})
	if len(partial) != 1 || !partial[0].Amount.Equal(dec("2.0344")) {
		t.Fatalf("partial window: %+v", partial)
	}
	onlyD1, _ := f.db.QueryCosts(f.ctxA, tsdb.CostQuery{From: d1, To: d2, Granularity: tsdb.GranTotal})
	if len(onlyD1) != 1 || !onlyD1[0].Amount.Equal(dec("1.7344")) {
		t.Fatalf("window end exclusive: %+v", onlyD1)
	}
	scoped, _ := f.db.QueryCosts(f.ctxA, tsdb.CostQuery{From: d1, To: to, Granularity: tsdb.GranTotal, NodeIDs: []string{"other"}})
	if len(scoped) != 0 {
		t.Fatalf("node restriction: %+v", scoped)
	}
	limited, _ := f.db.QueryCosts(f.ctxA, tsdb.CostQuery{From: d1, To: to, Granularity: tsdb.GranTotal, GroupBy: []string{"resource_id"}, Limit: 1})
	if len(limited) != 1 || limited[0].Keys["resource_id"] != "r1" {
		t.Fatalf("limit/order: %+v", limited)
	}
	lines, err := f.db.CostLines(f.ctxA, tsdb.CostQuery{From: d1, To: to, Filters: map[string][]string{"resource_id": {"r1"}}})
	must(t, err)
	if len(lines) != 2 || !lines[0].Day.Equal(d1) || !lines[0].Quantity.Equal(dec("24")) || lines[0].Labels["team"] != "shop" || lines[0].OrgID != f.orgA {
		t.Fatalf("cost lines: %+v", lines)
	}
	if leak, _ := f.db.QueryCosts(f.ctxB, tsdb.CostQuery{From: d1, To: to, Granularity: tsdb.GranTotal}); len(leak) != 0 {
		t.Fatalf("costs leak")
	}
	if _, err := f.db.QueryCosts(f.ctxA, tsdb.CostQuery{From: d1, To: to, GroupBy: []string{"password"}}); err == nil {
		t.Fatalf("unknown dimension must fail")
	}
}

func (f *fixture) billing(t *testing.T) {
	d := tsdb.TruncDay(f.now).AddDate(0, -1, 0)
	bl := func(day time.Time, amount string) model.BillingLine {
		return model.BillingLine{Provider: "ovh", Day: day, ResourceID: "r1", Service: "compute", SKU: "b2-7", CostType: model.CostCompute,
			Quantity: dec("1"), Unit: model.UnitMonth, Amount: dec(amount), Currency: "EUR", InvoiceID: "FR123"}
	}
	must(t, f.db.ReplaceBillingLines(f.ctxA, "conn", d, d.AddDate(0, 0, 2), []model.BillingLine{bl(d, "10.5"), bl(d.AddDate(0, 0, 1), "11")}))
	must(t, f.db.ReplaceBillingLines(f.ctxA, "conn", d.AddDate(0, 0, 1), d.AddDate(0, 0, 2), []model.BillingLine{bl(d.AddDate(0, 0, 1), "12.25")}))
	got, err := f.db.BillingLines(f.ctxA, "conn", d, d.AddDate(0, 0, 3))
	must(t, err)
	if len(got) != 2 || !got[0].Amount.Equal(dec("10.5")) || !got[1].Amount.Equal(dec("12.25")) || got[1].InvoiceID != "FR123" {
		t.Fatalf("billing: %+v", got)
	}
	if leak, _ := f.db.BillingLines(f.ctxB, "", d, d.AddDate(0, 0, 3)); len(leak) != 0 {
		t.Fatalf("billing leak")
	}
}

func (f *fixture) eventsUptimeUnits(t *testing.T) {
	base := f.now.Add(-3 * time.Hour)
	evs := []model.Event{
		{TS: base, Kind: model.EventDeployment, Source: "gitlab", ResourceID: "r1", Title: "v1", Payload: map[string]any{"version": "1"}},
		{TS: base.Add(time.Hour), Kind: model.EventHPAScale, Source: "k8s", Title: "scale"},
		{TS: base.Add(2 * time.Hour), Kind: model.EventDeployment, Source: "gitlab", Title: "v2"},
	}
	must(t, f.db.WriteEvents(f.ctxA, evs))
	must(t, f.db.WriteEvents(f.ctxA, evs[:1])) // réémission idempotente
	got, err := f.db.QueryEvents(f.ctxA, tsdb.EventQuery{From: base, To: f.now})
	must(t, err)
	if len(got) != 3 || got[0].Title != "v1" || got[0].Payload["version"] != "1" {
		t.Fatalf("events: %+v", got)
	}
	last, _ := f.db.QueryEvents(f.ctxA, tsdb.EventQuery{Kinds: []string{model.EventDeployment}, Limit: 1})
	if len(last) != 1 || last[0].Title != "v2" {
		t.Fatalf("events limit keeps most recent: %+v", last)
	}
	if leak, _ := f.db.QueryEvents(f.ctxB, tsdb.EventQuery{}); len(leak) != 0 {
		t.Fatalf("events leak")
	}

	check := ids.New()
	must(t, f.db.WriteUptime(f.ctxA, []model.UptimeResult{
		{CheckID: check, Region: "eu-west", TS: base, Up: true, LatencyMS: 120.5, StatusCode: 200},
		{CheckID: check, Region: "eu-west", TS: base.Add(time.Minute), Up: false, StatusCode: 503, Error: "unavailable"},
	}))
	up, err := f.db.QueryUptime(f.ctxA, tsdb.UptimeQuery{CheckIDs: []string{check}, Limit: 10})
	must(t, err)
	if len(up) != 2 || !up[0].Up || up[1].Up || up[1].StatusCode != 503 || up[0].LatencyMS != 120.5 {
		t.Fatalf("uptime: %+v", up)
	}

	metric := ids.New()
	d := tsdb.TruncDay(f.now).AddDate(0, 0, -1)
	must(t, f.db.WriteUnitValues(f.ctxA, metric, map[time.Time]decimal.Decimal{d: dec("1250000")}))
	must(t, f.db.WriteUnitValues(f.ctxA, metric, map[time.Time]decimal.Decimal{d: dec("1300000.5")}))
	vals, err := f.db.UnitValues(f.ctxA, metric, d, d.AddDate(0, 0, 1))
	must(t, err)
	if len(vals) != 1 || !vals[d].Equal(dec("1300000.5")) {
		t.Fatalf("unit values: %+v", vals)
	}
}

func (f *fixture) purge(t *testing.T) {
	ctx := tenancy.WithOrg(context.Background(), ids.New())
	d := tsdb.TruncDay(f.now)
	must(t, f.db.ReplaceCostLines(ctx, d, []model.CostLine{{ResourceID: "r", CostType: model.CostCompute, Amount: dec("1"), Currency: "EUR",
		Quantity: dec("1"), Source: model.SourceEstimate}}))
	must(t, f.db.WriteEvents(ctx, []model.Event{{TS: f.now, Kind: model.EventIncident, Title: "x"}}))
	must(t, f.db.PurgeOrg(ctx))
	if rows, _ := f.db.QueryCosts(ctx, tsdb.CostQuery{From: d, To: d.AddDate(0, 0, 1), Granularity: tsdb.GranTotal}); len(rows) != 0 {
		t.Fatalf("costs survived purge")
	}
	if evs, _ := f.db.QueryEvents(ctx, tsdb.EventQuery{}); len(evs) != 0 {
		t.Fatalf("events survived purge")
	}
	// Les autres organisations sont intactes.
	if rows, _ := f.db.QueryEvents(f.ctxA, tsdb.EventQuery{}); len(rows) == 0 {
		t.Fatalf("purge affected another organization")
	}
}
