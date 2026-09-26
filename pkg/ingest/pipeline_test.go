package ingest_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/connectors/demo"
	"github.com/kairn-io/kairn/pkg/costrun"
	"github.com/kairn-io/kairn/pkg/ingest"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/pricing/catalogs"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/store/memstore"
	"github.com/kairn-io/kairn/pkg/tenancy"
	"github.com/kairn-io/kairn/pkg/tsdb"
	"github.com/kairn-io/kairn/pkg/tsdb/memtsdb"
)

// Pipeline complet : connecteurs démo → inventaire historisé → métriques →
// facture → cost-engine → rapprochement estimé/facturé.
func TestDemoPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("pipeline test")
	}
	now := time.Date(2026, 9, 20, 6, 0, 0, 0, time.UTC)
	epoch := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	st, db := memstore.New(), memtsdb.New()
	ctx := tenancy.WithOrg(context.Background(), "org-demo")
	if err := st.Orgs().Create(ctx, &model.Organization{ID: "org-demo", Name: "Démo", Slug: "demo", Plan: model.PlanTeam, Currency: "EUR"}); err != nil {
		t.Fatal(err)
	}
	if _, err := catalogs.InstallSamples(ctx, st); err != nil {
		t.Fatal(err)
	}
	settings := map[string]string{"epoch": epoch.Format(time.RFC3339), "seed": "test"}
	os := model.Connector{ID: "c-os", Type: demo.TypeOpenStack, Name: "OVH GRA", Settings: settings, Enabled: true, IntervalSeconds: 3600}
	k8s := model.Connector{ID: "c-k8s", Type: demo.TypeKubernetes, Name: "prod-gra", Settings: settings, Enabled: true, IntervalSeconds: 900}
	for _, c := range []*model.Connector{&os, &k8s} {
		if err := st.Connectors().Create(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	sy := &ingest.Syncer{Store: st, TSDB: db, Now: func() time.Time { return now }}
	from := epoch.AddDate(0, 0, -5)
	for _, c := range []model.Connector{os, k8s} {
		rep, err := sy.Backfill(ctx, c, from)
		if err != nil {
			t.Fatalf("backfill %s: %v", c.Type, err)
		}
		if rep.Inventory.Created == 0 || rep.Metrics == 0 {
			t.Fatalf("backfill %s produced nothing: %+v", c.Type, rep)
		}
	}
	// Le node K8s est relié à sa VM.
	edges, _ := st.Resources().Edges(ctx, time.Time{}, time.Time{})
	backs := 0
	for _, e := range edges {
		if e.Relation == model.RelBacks {
			backs++
		}
	}
	if backs != 6 {
		t.Fatalf("want 6 VM→node links, got %d", backs)
	}
	// Coûts sur 3 jours complets.
	runner := &costrun.Runner{Store: st, TSDB: db}
	sums, err := runner.ComputeRange(ctx, epoch.AddDate(0, 0, -3), epoch)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sums {
		if s.Lines == 0 || !s.Total.IsPositive() {
			t.Fatalf("empty day: %+v", s)
		}
		for _, w := range s.Warnings {
			t.Errorf("unexpected warning on %s: %s", s.Day.Format("2006-01-02"), w)
		}
	}
	// Les pods portent le coût des nodes : présence de lignes k8s_workload et idle.
	rows, err := db.QueryCosts(ctx, tsdb.CostQuery{From: epoch.AddDate(0, 0, -3), To: epoch, Granularity: tsdb.GranTotal, GroupBy: []string{"cost_type"}})
	if err != nil {
		t.Fatal(err)
	}
	byType := map[string]decimal.Decimal{}
	for _, r := range rows {
		byType[r.Keys["cost_type"]] = r.Amount
	}
	if !byType[model.CostK8sWork].IsPositive() || !byType[model.CostK8sIdle].IsPositive() || !byType[model.CostCompute].IsPositive() {
		t.Fatalf("cost types: %v", byType)
	}
	// Rapprochement : l'écart estimé/facturé reste sous 2 % (critère R1).
	recs, err := runner.Reconcile(ctx, epoch.AddDate(0, 0, -3))
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("want 1 reconciliation, got %+v", recs)
	}
	delta := recs[0].DeltaPercent().Abs()
	t.Logf("rapprochement : estimé %s, facturé %s, écart %s %%", recs[0].Estimated.StringFixed(2), recs[0].Billed.StringFixed(2), recs[0].DeltaPercent())
	for _, s := range sums {
		t.Logf("jour %s : %d lignes, total %s EUR", s.Day.Format("2006-01-02"), s.Lines, s.Total.StringFixed(2))
	}
	if delta.GreaterThan(decimal.NewFromInt(2)) {
		t.Fatalf("estimated vs billed delta %s%% > 2%% (est %s, billed %s)", recs[0].DeltaPercent(), recs[0].Estimated, recs[0].Billed)
	}
	// Idempotence : un second backfill ne duplique rien.
	before, _ := st.Resources().Count(ctx)
	if _, err := sy.Backfill(ctx, os, from); err != nil {
		t.Fatal(err)
	}
	after, _ := st.Resources().Count(ctx)
	if before != after {
		t.Fatalf("backfill must be idempotent: %d → %d", before, after)
	}
	cur, _ := st.Resources().Current(ctx, store.ResourceFilter{Types: []string{model.TypeK8sPod}})
	if len(cur) == 0 {
		t.Fatal("no pods")
	}
}
