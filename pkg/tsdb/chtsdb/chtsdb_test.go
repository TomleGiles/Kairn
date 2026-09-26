package chtsdb

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/ids"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/tenancy"
	"github.com/kairn-io/kairn/pkg/tsdb"
)

type captured struct {
	query  string
	params url.Values
	body   string
}

// fakeCH enregistre les requêtes et renvoie une réponse JSONEachRow prédéfinie aux SELECT.
func fakeCH(t *testing.T, selectReply string) (*DB, *[]captured) {
	t.Helper()
	var mu sync.Mutex
	var got []captured
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		q := r.URL.Query()
		c := captured{params: q}
		if qq := q.Get("query"); qq != "" {
			c.query, c.body = qq, string(b)
		} else {
			c.query = string(b)
		}
		mu.Lock()
		got = append(got, c)
		mu.Unlock()
		if strings.HasPrefix(strings.TrimSpace(c.query), "SELECT") {
			_, _ = io.WriteString(w, selectReply)
		}
	}))
	t.Cleanup(srv.Close)
	return New(&Client{URL: srv.URL, Database: "kairn"}), &got
}

func TestReplaceCostLinesIsExactAndScoped(t *testing.T) {
	db, got := fakeCH(t, "")
	org := ids.New()
	ctx := tenancy.WithOrg(context.Background(), org)
	day := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	err := db.ReplaceCostLines(ctx, day, []model.CostLine{{ResourceID: "r1", CostType: model.CostCompute, Quantity: decimal.RequireFromString("24"),
		Amount: decimal.RequireFromString("1.634400"), Currency: "EUR", Source: model.SourceEstimate, Labels: map[string]string{"team": "shop"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(*got) != 2 {
		t.Fatalf("requests: %d", len(*got))
	}
	del, ins := (*got)[0], (*got)[1]
	if !strings.Contains(del.query, "org_id = {org:UUID}") || del.params.Get("param_org") != org || del.params.Get("param_day") != "2026-09-20" {
		t.Fatalf("delete not scoped: %+v", del)
	}
	if !strings.HasPrefix(ins.query, "INSERT INTO cost_lines") || !strings.Contains(ins.body, `"amount":1.6344`) || !strings.Contains(ins.body, `"org_id":"`+org+`"`) {
		t.Fatalf("insert body: %s", ins.body)
	}
	if strings.Contains(ins.body, "e+") || strings.Contains(ins.body, `"amount":"`) {
		t.Fatalf("amounts must be exact numeric literals: %s", ins.body)
	}
	// Ligne d'une autre organisation refusée.
	if err := db.ReplaceCostLines(ctx, day, []model.CostLine{{OrgID: ids.New(), Amount: decimal.Zero}}); err == nil {
		t.Fatal("cross-org line accepted")
	}
}

func TestQueryCostsParsesDecimalsAndUsesParameters(t *testing.T) {
	db, got := fakeCH(t, `{"period":"1970-01-01","currency":"EUR","g0":"shop","total":"2.034400"}`+"\n"+`{"period":"1970-01-01","currency":"EUR","g0":"","total":"0.2"}`+"\n")
	org := ids.New()
	ctx := tenancy.WithOrg(context.Background(), org)
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	rows, err := db.QueryCosts(ctx, tsdb.CostQuery{From: from, To: from.AddDate(0, 1, 0), Granularity: tsdb.GranTotal, GroupBy: []string{"label:team"},
		Filters: map[string][]string{"provider": {"openstack", "o'hara"}}, NodeIDs: []string{"n1"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || !rows[0].Amount.Equal(decimal.RequireFromString("2.0344")) || rows[0].Keys["label:team"] != "shop" || !rows[0].Period.IsZero() {
		t.Fatalf("rows: %+v", rows)
	}
	q := (*got)[0]
	for _, want := range []string{"org_id = {org:UUID}", "labels[{lk0:String}]", "IN {f0:Array(String)}", "allocation_node_id IN {nodes:Array(String)}", "FINAL"} {
		if !strings.Contains(q.query, want) {
			t.Fatalf("query missing %q: %s", want, q.query)
		}
	}
	if q.params.Get("param_org") != org || q.params.Get("param_lk0") != "team" || q.params.Get("param_f0") != `['openstack','o\'hara']` {
		t.Fatalf("params: %v", q.params)
	}
	if strings.Contains(q.query, "openstack") || strings.Contains(q.query, "team") {
		t.Fatalf("values must never be inlined: %s", q.query)
	}
}

func TestQueriesRequireOrganization(t *testing.T) {
	db, got := fakeCH(t, "")
	ctx := context.Background()
	if _, err := db.QueryCosts(ctx, tsdb.CostQuery{From: time.Now(), To: time.Now().Add(time.Hour)}); err == nil {
		t.Fatal("query without org accepted")
	}
	if err := db.PurgeOrg(ctx); err == nil {
		t.Fatal("purge without org accepted")
	}
	if err := db.Rollup(tenancy.WithSystem(ctx), time.Now().Add(-time.Hour), time.Now()); err == nil {
		t.Fatal("rollup without org accepted")
	}
	if len(*got) != 0 {
		t.Fatalf("no request must reach ClickHouse: %d", len(*got))
	}
	if err := db.WriteMetrics(tenancy.WithOrg(ctx, ids.New()), []model.MetricPoint{{ResourceID: "not-a-uuid"}}); err == nil {
		t.Fatal("invalid resource id accepted")
	}
}

func TestQueryMetricsResolutionAndDecoding(t *testing.T) {
	rid := ids.New()
	db, got := fakeCH(t, `{"rid":"`+rid+`","metric":"cpu.utilization","b":"2026-09-20 10:00:00.000","v":0.5}`+"\n"+
		`{"rid":"`+rid+`","metric":"cpu.utilization","b":"2026-09-20 11:00:00.000","v":0.7}`+"\n")
	now := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	db.Now = func() time.Time { return now }
	ctx := tenancy.WithOrg(context.Background(), ids.New())
	s, err := db.QueryMetrics(ctx, tsdb.MetricQuery{ResourceIDs: []string{rid}, From: now.AddDate(0, 0, -7), To: now, Step: time.Hour, Agg: tsdb.AggP95})
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 1 || len(s[0].Points) != 2 || s[0].Points[1].Value != 0.7 {
		t.Fatalf("series: %+v", s)
	}
	if q := (*got)[0].query; !strings.Contains(q, "FROM metrics_raw") || !strings.Contains(q, "quantileExactInclusive(0.95)(value)") {
		t.Fatalf("recent window must use raw data: %s", q)
	}
	_, _ = db.QueryMetrics(ctx, tsdb.MetricQuery{From: now.AddDate(0, -2, 0), To: now, Step: 24 * time.Hour, Agg: tsdb.AggAvg})
	if q := (*got)[1].query; !strings.Contains(q, "FROM metrics_5m") || !strings.Contains(q, "sum(avg * samples) / sum(samples)") {
		t.Fatalf("60-day window must use 5-minute aggregates: %s", q)
	}
	_, _ = db.QueryMetrics(ctx, tsdb.MetricQuery{From: now.AddDate(0, -6, 0), To: now, Step: 24 * time.Hour, Agg: tsdb.AggMax})
	if q := (*got)[2].query; !strings.Contains(q, "FROM metrics_1h") || !strings.Contains(q, "max(max)") {
		t.Fatalf("6-month window must use hourly aggregates: %s", q)
	}
}

func TestEventIDIsDeterministic(t *testing.T) {
	e := model.Event{TS: time.Date(2026, 9, 20, 10, 12, 0, 0, time.UTC), Kind: model.EventDeployment, Source: "gitlab", Title: "v2.3.0"}
	again := e
	if eventID(e) != eventID(again) || len(eventID(e)) != 32 {
		t.Fatal("event id")
	}
	e2 := e
	e2.Title = "v2.3.1"
	if eventID(e) == eventID(e2) {
		t.Fatal("distinct events share an id")
	}
}
