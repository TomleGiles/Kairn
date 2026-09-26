package api

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/seed"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/tenancy"
)

// seededEnv sème l'organisation de démonstration (14 jours) et ouvre une session propriétaire.
func seededEnv(t *testing.T) (*testEnv, string) {
	t.Helper()
	e := newEnv(t)
	if _, err := seed.Run(context.Background(), e.st, e.db, seed.Options{Days: 14, Now: e.now}); err != nil {
		t.Fatal(err)
	}
	return e, e.login(seed.UserEmail)
}

type costsResp struct {
	Total string `json:"total"`
	Rows  []struct {
		Keys   map[string]string `json:"keys"`
		Amount string            `json:"amount"`
	} `json:"rows"`
}

func dec(t *testing.T, s string) decimal.Decimal {
	t.Helper()
	d, err := decimal.NewFromString(s)
	if err != nil {
		t.Fatalf("decimal %q: %v", s, err)
	}
	return d
}

func TestCostQueriesAndFilters(t *testing.T) {
	e, tok := seededEnv(t)
	base := "/api/v1/orgs/" + seed.OrgID + "/costs?granularity=total"
	get := func(q string) costsResp {
		t.Helper()
		r := e.do(http.MethodGet, base+q, tok, nil)
		expect(t, r, http.StatusOK)
		var out costsResp
		r.JSON(t, &out)
		return out
	}
	all := get("")
	ns := get("&filter=" + url.QueryEscape("label:k8s.namespace:search"))
	both := get("&filter=" + url.QueryEscape("label:k8s.namespace:search") + "&filter=" + url.QueryEscape("label:k8s.workload:search-api"))
	if !dec(t, all.Total).IsPositive() || !dec(t, ns.Total).IsPositive() {
		t.Fatalf("totals: %s / %s", all.Total, ns.Total)
	}
	if !dec(t, both.Total).LessThan(dec(t, ns.Total)) {
		t.Fatalf("repeated filters must all apply: namespace=%s, namespace+workload=%s", ns.Total, both.Total)
	}
	// Somme des groupes = total (aucune perte au regroupement).
	grouped := get("&group_by=cost_type")
	sum := decimal.Zero
	for _, r := range grouped.Rows {
		sum = sum.Add(dec(t, r.Amount))
	}
	if !sum.Equal(dec(t, grouped.Total)) || !sum.Equal(dec(t, all.Total)) {
		t.Fatalf("group sum %s vs total %s / %s", sum, grouped.Total, all.Total)
	}
	// Dimension inconnue → 422.
	expect(t, e.do(http.MethodGet, base+"&group_by=password", tok, nil), http.StatusUnprocessableEntity)
}

func TestScopedMemberSeesOnlyItsSubtree(t *testing.T) {
	e, owner := seededEnv(t)
	ctx := tenancy.WithOrg(context.Background(), seed.OrgID)
	nodes, _ := e.st.AllocationNodes().List(ctx, store.ListQuery{Limit: 500})
	var search string
	for _, n := range nodes {
		if n.Name == "Équipe Search" {
			search = n.ID
		}
	}
	if search == "" {
		t.Fatal("search node not found")
	}
	r := e.do(http.MethodPost, "/api/v1/orgs/"+seed.OrgID+"/members", owner, map[string]any{"email": "lead@search.example", "role": "viewer", "scopes": []string{search}})
	expect(t, r, http.StatusCreated)
	lead := e.login("lead@search.example")
	var scoped, full costsResp
	e.do(http.MethodGet, "/api/v1/orgs/"+seed.OrgID+"/costs?granularity=total&group_by=allocation_node_id", lead, nil).JSON(t, &scoped)
	e.do(http.MethodGet, "/api/v1/orgs/"+seed.OrgID+"/costs?granularity=total", owner, nil).JSON(t, &full)
	for _, row := range scoped.Rows {
		if row.Keys["allocation_node_id"] != search {
			t.Fatalf("scoped member sees node %s", row.Keys["allocation_node_id"])
		}
	}
	if !dec(t, scoped.Total).LessThan(dec(t, full.Total)) {
		t.Fatalf("scoped total %s must be below full %s", scoped.Total, full.Total)
	}
}

func TestSummaryAndWhatIf(t *testing.T) {
	e, tok := seededEnv(t)
	var s summaryOut
	r := e.do(http.MethodGet, "/api/v1/orgs/"+seed.OrgID+"/costs/summary", tok, nil)
	expect(t, r, http.StatusOK)
	r.JSON(t, &s)
	if !s.MonthToDate.IsPositive() || s.ResourceCount == 0 || !s.ForecastMonthEnd.GreaterThanOrEqual(s.MonthToDate) {
		t.Fatalf("summary: %+v", s)
	}
	r = e.do(http.MethodPost, "/api/v1/orgs/"+seed.OrgID+"/whatif", tok, map[string]any{
		"scenarios": []map[string]any{{"kind": "add_nodes", "provider": "openstack", "flavor": "b2-15", "count": 2}},
	})
	expect(t, r, http.StatusOK)
	var w struct {
		DeltaMonthly string `json:"delta_monthly"`
	}
	r.JSON(t, &w)
	// 2 × 0,1331 € × 730 h = 194,33 €
	if w.DeltaMonthly != "194.33" {
		t.Fatalf("what-if delta: %s", w.DeltaMonthly)
	}
}
