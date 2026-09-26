package scaleway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/connector"
	"github.com/kairn-io/kairn/pkg/model"
)

func fake(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Auth-Token") != "scw-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		write := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		switch r.URL.Path {
		case "/instance/v1/zones/fr-par-1/servers":
			write(map[string]any{"total_count": 2, "servers": []any{
				map[string]any{"id": "s1", "name": "web", "commercial_type": "PRO2-XS", "state": "running", "creation_date": "2026-06-01T08:00:00Z", "tags": []string{"team=shop", "prod"}},
				map[string]any{"id": "s2", "name": "batch", "commercial_type": "DEV1-S", "state": "stopped", "creation_date": "2026-06-01T08:00:00Z"},
			}})
		case "/instance/v1/zones/fr-par-1/volumes":
			write(map[string]any{"total_count": 2, "volumes": []any{map[string]any{"id": "v1", "name": "data", "size": 50000000000, "volume_type": "b_ssd",
				"state": "in_use", "server": map[string]any{"id": "s1"}},
				map[string]any{"id": "sbs1", "name": "db", "size": 20000000000, "volume_type": "sbs_volume", "state": "in_use", "server": map[string]any{"id": "s1"}}}})
		case "/block/v1alpha1/zones/fr-par-1/volumes":
			if r.URL.Query().Get("page_size") != "100" {
				t.Errorf("Block API pagination uses page_size")
			}
			write(map[string]any{"total_count": 1, "volumes": []any{map[string]any{"id": "sbs1", "name": "db", "size": 20000000000, "status": "in_use",
				"specs": map[string]any{"perf_iops": 15000, "class": "sbs"}, "tags": []string{"team=data"},
				"references": []any{map[string]any{"product_resource_type": "instance_server", "product_resource_id": "s1"}}}}})
		case "/instance/v1/zones/fr-par-1/snapshots":
			write(map[string]any{"total_count": 0, "snapshots": []any{}})
		case "/instance/v1/zones/fr-par-1/ips":
			write(map[string]any{"total_count": 1, "ips": []any{map[string]any{"id": "ip1", "address": "51.15.1.1", "server": map[string]any{"id": "s1"}}}})
		case "/lb/v1/zones/fr-par-1/lbs":
			write(map[string]any{"total_count": 1, "lbs": []any{map[string]any{"id": "lb1", "name": "front", "type": "LB-S", "status": "ready"}}})
		case "/k8s/v1/regions/fr-par/clusters":
			write(map[string]any{"total_count": 1, "clusters": []any{map[string]any{"id": "k1", "name": "prod", "version": "1.31.2", "type": "kapsule", "status": "ready"}}})
		case "/billing/v2beta1/consumptions":
			if r.URL.Query().Get("organization_id") != "org-1" {
				t.Errorf("billing must be scoped to the organization")
			}
			write(map[string]any{"consumptions": []any{
				map[string]any{"value": map[string]any{"currency_code": "EUR", "units": 10, "nanos": 500000000}, "product_name": "Instances PRO2-XS",
					"sku": "/compute/pro2_xs/run_par1", "category_name": "Compute", "billed_quantity": "72"},
			}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newConn(t *testing.T, srv *httptest.Server) *Conn {
	t.Helper()
	c, err := New(connector.Config{Settings: map[string]string{"organization_id": "org-1", "zones": "fr-par-1"}, Secrets: map[string]string{"secret_key": "scw-secret"}})
	if err != nil {
		t.Fatal(err)
	}
	conn := c.(*Conn)
	conn.base = srv.URL
	conn.now = func() time.Time { return time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC) }
	return conn
}

func TestScalewayInventoryAndBilling(t *testing.T) {
	c := newConn(t, fake(t))
	if err := c.Validate(context.Background(), connector.Config{}); err != nil {
		t.Fatal(err)
	}
	ctx, sink := connector.WithErrorSink(context.Background())
	ch, err := c.SyncInventory(ctx, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]connector.Resource{}
	for _, r := range connector.Collect(ch) {
		by[r.ExternalID] = r
	}
	if err := sink.Err(); err != nil {
		t.Fatal(err)
	}
	if len(by) != 7 || by["s1"].Attributes["flavor"] != "PRO2-XS" || by["s1"].Labels["team"] != "shop" || by["s1"].Labels["prod"] != "true" ||
		by["s2"].Attributes["billing_state"] != "stopped_unbilled" {
		t.Fatalf("instances: %+v", by)
	}
	if by["v1"].Attributes["size_gb"] != 50.0 || by["v1"].Attributes["attached_to"] != "s1" || by["ip1"].Attributes["ip_kind"] != "floating" ||
		by["k1"].Attributes["control_plane_tier"] != "free" || by["lb1"].Attributes["flavor"] != "LB-S" ||
		by["sbs1"].Attributes["volume_type"] != "sbs_15k" || by["sbs1"].Attributes["attached_to"] != "s1" || by["sbs1"].Labels["team"] != "data" {
		t.Fatalf("resources: %+v", by)
	}
	bctx, bsink := connector.WithErrorSink(context.Background())
	bch, err := c.SyncBilling(bctx, connector.Period{From: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	lines := connector.Collect(bch)
	if err := bsink.Err(); err != nil {
		t.Fatal(err)
	}
	total := decimal.Zero
	for _, l := range lines {
		total = total.Add(l.Amount)
		if l.CostType != model.CostCompute || l.Currency != "EUR" {
			t.Fatalf("line: %+v", l)
		}
	}
	// Mois en cours connu jusqu'au 3 septembre inclus : 3 jours, total exact 10,50 €.
	if len(lines) != 3 || !total.Equal(decimal.RequireFromString("10.5")) {
		t.Fatalf("billing: %d lines, total %s", len(lines), total)
	}
}
