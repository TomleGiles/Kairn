package ovh

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/connector"
	"github.com/kairn-io/kairn/pkg/model"
)

const proj = "0123456789abcdef"

func fakeOVH(t *testing.T) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		write := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		if r.URL.Path == "/1.0/auth/time" {
			write(time.Date(2026, 9, 25, 12, 0, 10, 0, time.UTC).Unix())
			return
		}
		tsStr := r.Header.Get("X-Ovh-Timestamp")
		tsv, _ := strconv.ParseInt(tsStr, 10, 64)
		want := sign("as", "ck", http.MethodGet, srv.URL+r.URL.RequestURI(), "", tsv)
		if r.Header.Get("X-Ovh-Application") != "ak" || r.Header.Get("X-Ovh-Consumer") != "ck" || r.Header.Get("X-Ovh-Signature") != want {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		base := "/1.0/cloud/project/" + proj
		switch r.URL.Path {
		case "/1.0/cloud/project":
			write([]string{proj})
		case base:
			write(map[string]any{"description": "shop-prod", "status": "ok", "creationDate": "2025-01-01T00:00:00Z"})
		case base + "/flavor":
			write([]any{map[string]any{"id": "f1", "name": "b2-7", "vcpus": 2, "ram": 7, "disk": 50}})
		case base + "/instance":
			write([]any{
				map[string]any{"id": "i1", "name": "web-1", "flavorId": "f1", "region": "GRA11", "status": "ACTIVE", "created": "2026-06-01T08:00:00Z"},
				map[string]any{"id": "i2", "name": "batch", "flavorId": "f1", "region": "GRA11", "status": "SHUTOFF", "created": "2026-06-01T08:00:00Z",
					"monthlyBilling": map[string]any{"status": "ok"}},
			})
		case base + "/volume":
			write([]any{map[string]any{"id": "v1", "name": "data", "size": 100, "type": "high-speed", "region": "GRA11", "status": "in-use", "attachedTo": []string{"i1"}}})
		case base + "/volume/snapshot":
			write([]any{})
		case base + "/region":
			write([]string{"GRA11"})
		case base + "/region/GRA11/floatingip":
			write([]any{map[string]any{"id": "fip1", "ip": "51.68.1.1", "status": "active", "associatedEntity": map[string]any{"id": "i1", "type": "instance"}}})
		case base + "/region/GRA11/loadbalancing/flavor":
			write([]any{map[string]any{"id": "lbf", "name": "small"}})
		case base + "/region/GRA11/loadbalancing/loadbalancer":
			write([]any{map[string]any{"id": "lb1", "name": "shop-lb", "flavorId": "lbf", "provisioningStatus": "active"}})
		case base + "/region/GRA11/storage":
			write([]any{map[string]any{"name": "media", "objectsCount": 12, "objectsSize": 5e9}})
		case base + "/kube":
			write([]string{"k1"})
		case base + "/kube/k1":
			write(map[string]any{"id": "k1", "name": "prod", "region": "GRA7", "version": "1.31", "status": "READY", "plan": "free"})
		case base + "/usage/history":
			write([]any{})
		case base + "/usage/current":
			write(map[string]any{
				"period": map[string]any{"from": "2026-09-01T00:00:00Z", "to": "2026-10-01T00:00:00Z"}, "lastUpdate": "2026-09-04T00:00:00Z",
				"hourlyUsage": map[string]any{
					"instance": []any{map[string]any{"reference": "b2-7", "region": "GRA11", "details": []any{
						map[string]any{"instanceId": "i1", "quantity": map[string]any{"unit": "Hour", "value": 72}, "totalPrice": 4.91}}}},
					"volume": []any{map[string]any{"type": "high-speed", "region": "GRA11", "details": []any{
						map[string]any{"volumeId": "v1", "quantity": map[string]any{"unit": "GiBh", "value": 7200}, "totalPrice": 0.86}}}},
				},
				"monthlyUsage":   map[string]any{"instance": []any{}},
				"resourcesUsage": []any{map[string]any{"type": "octavia-loadbalancer", "totalPrice": 1.0}},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newConn(t *testing.T, srv *httptest.Server) *Conn {
	t.Helper()
	c, err := New(connector.Config{Settings: map[string]string{"application_key": "ak"},
		Secrets: map[string]string{"application_secret": "as", "consumer_key": "ck"}})
	if err != nil {
		t.Fatal(err)
	}
	conn := c.(*Conn)
	conn.c.base = srv.URL + "/1.0" // l'API réelle impose HTTPS ; le faux serveur de test est en HTTP
	now := func() time.Time { return time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC) }
	conn.now, conn.c.now = now, now
	return conn
}

func TestInventoryAndSignature(t *testing.T) {
	c := newConn(t, fakeOVH(t))
	if err := c.Validate(context.Background(), connector.Config{}); err != nil {
		t.Fatal(err)
	}
	ctx, sink := connector.WithErrorSink(context.Background())
	ch, err := c.SyncInventory(ctx, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	res := connector.Collect(ch)
	if err := sink.Err(); err != nil {
		t.Fatal(err)
	}
	by := map[string]connector.Resource{}
	for _, r := range res {
		by[r.ExternalID] = r
	}
	if len(res) != 8 { // projet, 2 instances, volume, IP, répartiteur, bucket, cluster
		t.Fatalf("resources: %d", len(res))
	}
	if by["i1"].Attributes["ram_gb"] != 7.0 || by["i2"].Attributes["billing_state"] != "stopped_billed" || by["i2"].Attributes["billing_mode"] != "monthly" {
		t.Fatalf("instances: %+v / %+v", by["i1"].Attributes, by["i2"].Attributes)
	}
	if by["v1"].Attributes["attached_to"] != "i1" || by["fip1"].Attributes["attached_to"] != "i1" || by["lb1"].Attributes["flavor"] != "small" {
		t.Fatalf("attachments: %+v", by)
	}
	if by["GRA11/media"].Attributes["size_gb"] != 5.0 || by["k1"].Attributes["control_plane_tier"] != "free" {
		t.Fatalf("bucket/kube: %+v %+v", by["GRA11/media"].Attributes, by["k1"].Attributes)
	}
}

func TestBillingSpreadIsExact(t *testing.T) {
	c := newConn(t, fakeOVH(t))
	ctx, sink := connector.WithErrorSink(context.Background())
	period := connector.Period{From: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	ch, err := c.SyncBilling(ctx, period)
	if err != nil {
		t.Fatal(err)
	}
	lines := connector.Collect(ch)
	if err := sink.Err(); err != nil {
		t.Fatal(err)
	}
	total := decimal.Zero
	perResource := map[string]decimal.Decimal{}
	days := map[string]int{}
	for _, l := range lines {
		total = total.Add(l.Amount)
		perResource[l.ResourceExternalID+"|"+l.SKU] = perResource[l.ResourceExternalID+"|"+l.SKU].Add(l.Amount)
		days[l.ResourceExternalID]++
		if l.Currency != "EUR" || l.InvoiceID == "" {
			t.Fatalf("line: %+v", l)
		}
	}
	// 4,91 € sur 3 jours (1er → 4 septembre, dernière mise à jour) : 1,63 + 1,63 + 1,65.
	if !perResource["i1|compute.flavor.b2-7"].Equal(decimal.RequireFromString("4.91")) || days["i1"] != 3 {
		t.Fatalf("instance: %v over %d days", perResource["i1|compute.flavor.b2-7"], days["i1"])
	}
	if !total.Equal(decimal.RequireFromString("6.77")) {
		t.Fatalf("total must be exact: %s", total)
	}
	for _, l := range lines {
		if l.ResourceExternalID == "" && l.SKU == "octavia-loadbalancer" && l.CostType != model.CostNetwork {
			t.Fatalf("cost type: %+v", l)
		}
	}
}

func TestSpread(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	period := connector.Period{From: from, To: from.AddDate(0, 1, 0)}
	s := spread(decimal.RequireFromString("10"), from, from.AddDate(0, 0, 3), period)
	sum := decimal.Zero
	for _, v := range s {
		sum = sum.Add(v)
	}
	if len(s) != 3 || !sum.Equal(decimal.NewFromInt(10)) || !s[from].Equal(decimal.RequireFromString("3.33")) {
		t.Fatalf("spread: %v", s)
	}
	partial := spread(decimal.RequireFromString("10"), from, from.AddDate(0, 0, 3), connector.Period{From: from.AddDate(0, 0, 1), To: from.AddDate(0, 0, 2)})
	if len(partial) != 1 {
		t.Fatalf("period clipping: %v", partial)
	}
}
