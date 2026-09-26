package openstack

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kairn-io/kairn/pkg/connector"
	"github.com/kairn-io/kairn/pkg/model"
)

const (
	projectID = "a1b2c3d4e5f6"
	token     = "tok-123"
)

// fakeCloud simule les API OpenStack à partir de réponses enregistrées (format réel des services).
func fakeCloud(t *testing.T) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		write := func(v any) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(v)
		}
		if r.URL.Path == "/identity/v3/auth/tokens" {
			var body map[string]any
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &body)
			if !strings.Contains(string(b), `"secret":"s3cret"`) {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			ep := func(typ, path string) map[string]any {
				return map[string]any{"type": typ, "endpoints": []map[string]any{
					{"interface": "public", "region": "GRA11", "url": srv.URL + path},
					{"interface": "internal", "region": "GRA11", "url": "http://internal.invalid" + path},
				}}
			}
			w.Header().Set("X-Subject-Token", token)
			w.WriteHeader(http.StatusCreated)
			write(map[string]any{"token": map[string]any{
				"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
				"project":    map[string]any{"id": projectID, "name": "shop-prod"},
				"roles":      []map[string]any{{"name": "reader"}},
				"catalog": []any{ep("compute", "/compute/v2.1"), ep("volumev3", "/volume/v3/"+projectID), ep("network", "/network"),
					ep("load-balancer", "/lb"), ep("object-store", "/swift/v1/AUTH_"+projectID), ep("metric", "/gnocchi")},
			}})
			return
		}
		if r.Header.Get("X-Auth-Token") != token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/compute/v2.1/servers/detail":
			if r.Header.Get("OpenStack-API-Version") != "compute 2.47" {
				t.Errorf("missing compute microversion")
			}
			if r.URL.Query().Get("marker") == "" {
				write(map[string]any{
					"servers": []any{map[string]any{"id": "vm-1", "name": "shop-web-1", "status": "ACTIVE", "created": "2026-06-01T08:00:00Z",
						"flavor":   map[string]any{"original_name": "b2-7", "vcpus": 2, "ram": 7000, "disk": 50},
						"metadata": map[string]any{"team": "shop", "env": "prod"}, "OS-EXT-AZ:availability_zone": "nova"}},
					"servers_links": []any{map[string]any{"rel": "next", "href": srv.URL + "/compute/v2.1/servers/detail?limit=500&marker=vm-1"}},
				})
				return
			}
			write(map[string]any{"servers": []any{map[string]any{"id": "vm-2", "name": "batch-1", "status": "SHUTOFF", "created": "2026-07-01T08:00:00Z",
				"flavor": map[string]any{"original_name": "b2-15", "vcpus": 4, "ram": 15000, "disk": 100}, "metadata": map[string]any{}}}})
		case "/compute/v2.1/servers":
			write(map[string]any{"servers": []any{}})
		case "/volume/v3/" + projectID + "/volumes/detail":
			write(map[string]any{"volumes": []any{
				map[string]any{"id": "vol-1", "name": "data", "size": 100, "volume_type": "high-speed", "status": "in-use", "created_at": "2026-06-01T08:00:00.000000",
					"attachments": []any{map[string]any{"server_id": "vm-1"}}, "metadata": map[string]any{}},
				map[string]any{"id": "vol-2", "name": "orphan", "size": 50, "volume_type": "classic", "status": "available", "created_at": "2026-05-01T08:00:00.000000",
					"attachments": []any{}},
			}})
		case "/volume/v3/" + projectID + "/snapshots/detail":
			write(map[string]any{"snapshots": []any{map[string]any{"id": "snap-1", "name": "before-upgrade", "size": 100, "volume_id": "vol-1",
				"status": "available", "created_at": "2026-01-10T08:00:00.000000"}}})
		case "/network/v2.0/floatingips":
			if r.URL.Query().Get("project_id") != projectID {
				t.Errorf("floating IPs must be scoped to the project")
			}
			write(map[string]any{"floatingips": []any{
				map[string]any{"id": "fip-1", "floating_ip_address": "51.68.1.10", "status": "ACTIVE", "port_details": map[string]any{"device_id": "vm-1"}},
				map[string]any{"id": "fip-2", "floating_ip_address": "51.68.1.11", "status": "DOWN"},
			}})
		case "/lb/v2/lbaas/flavors":
			write(map[string]any{"flavors": []any{map[string]any{"id": "f-small", "name": "small"}}})
		case "/lb/v2/lbaas/loadbalancers":
			write(map[string]any{"loadbalancers": []any{map[string]any{"id": "lb-1", "name": "shop-lb", "flavor_id": "f-small",
				"provisioning_status": "ACTIVE", "operating_status": "ONLINE", "vip_address": "10.0.0.5"}}})
		case "/swift/v1/AUTH_" + projectID:
			if r.URL.Query().Get("format") != "json" {
				t.Errorf("swift listing must request json")
			}
			write([]any{map[string]any{"name": "shop-media", "count": 1200, "bytes": 850000000000}})
		case "/gnocchi/v1/resource/instance/vm-1":
			write(map[string]any{"metrics": map[string]any{"cpu": "m-cpu", "memory.usage": "m-mem"}})
		case "/gnocchi/v1/resource/instance/vm-2":
			w.WriteHeader(http.StatusNotFound)
		case "/gnocchi/v1/metric/m-cpu/measures":
			if r.URL.Query().Get("aggregation") != "rate:mean" {
				t.Errorf("cpu must use rate:mean aggregation")
			}
			// 150 s CPU par période de 300 s = 0,5 cœur.
			write([]any{[]any{"2026-09-25T10:00:00+00:00", 300.0, 150e9}, []any{"2026-09-25T10:05:00+00:00", 300.0, 300e9}})
		case "/gnocchi/v1/metric/m-mem/measures":
			write([]any{[]any{"2026-09-25T10:00:00+00:00", 300.0, 3500.0}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newConn(t *testing.T, srv *httptest.Server, secret string) *Conn {
	t.Helper()
	c, err := New(connector.Config{
		Settings: map[string]string{"auth_url": srv.URL + "/identity", "region": "GRA11", "application_credential_id": "app-1"},
		Secrets:  map[string]string{"application_credential_secret": secret},
	})
	if err != nil {
		t.Fatal(err)
	}
	conn := c.(*Conn)
	conn.now = func() time.Time { return time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC) }
	return conn
}

func TestInventory(t *testing.T) {
	srv := fakeCloud(t)
	c := newConn(t, srv, "s3cret")
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
	byID := map[string]connector.Resource{}
	counts := map[string]int{}
	for _, r := range res {
		byID[r.ExternalID] = r
		counts[r.Type]++
	}
	want := map[string]int{model.TypeProject: 1, model.TypeInstance: 2, model.TypeVolume: 2, model.TypeSnapshot: 1, model.TypeIP: 2,
		model.TypeLoadBalancer: 1, model.TypeBucket: 1}
	for typ, n := range want {
		if counts[typ] != n {
			t.Fatalf("%s: got %d, want %d (%v)", typ, counts[typ], n, counts)
		}
	}
	vm := byID["vm-1"]
	if vm.Attributes["flavor"] != "b2-7" || vm.Attributes["vcpus"] != 2.0 || vm.Attributes["billing_state"] != "running" || vm.Labels["team"] != "shop" ||
		vm.Region != "GRA11" || vm.CreatedAt == nil || vm.Parents[0].ParentExternalID != projectID {
		t.Fatalf("vm-1: %+v", vm)
	}
	if byID["vm-2"].Attributes["billing_state"] != "stopped_billed" {
		t.Fatalf("SHUTOFF instance must stay billed by default: %+v", byID["vm-2"].Attributes)
	}
	if byID["vol-1"].Attributes["attached_to"] != "vm-1" || len(byID["vol-1"].Parents) != 2 || byID["vol-2"].Attributes["attached_to"] != nil {
		t.Fatalf("volume attachments: %+v / %+v", byID["vol-1"], byID["vol-2"])
	}
	if byID["fip-1"].Attributes["attached_to"] != "vm-1" || byID["fip-2"].Attributes["attached_to"] != nil {
		t.Fatalf("floating ips: %+v", byID["fip-1"])
	}
	if byID["lb-1"].Attributes["flavor"] != "small" || byID["shop-media"].Attributes["size_gb"] != 850.0 {
		t.Fatalf("lb/bucket: %+v %+v", byID["lb-1"].Attributes, byID["shop-media"].Attributes)
	}
	if byID["snap-1"].Attributes["created_at"] != "2026-01-10T08:00:00Z" {
		t.Fatalf("snapshot: %+v", byID["snap-1"].Attributes)
	}
}

func TestMetrics(t *testing.T) {
	c := newConn(t, fakeCloud(t), "s3cret")
	ctx, sink := connector.WithErrorSink(context.Background())
	w := connector.TimeWindow{From: time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC), To: time.Date(2026, 9, 25, 13, 0, 0, 0, time.UTC)}
	ch, err := c.SyncMetrics(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	pts := connector.Collect(ch)
	if err := sink.Err(); err != nil {
		t.Fatal(err)
	}
	got := map[string][]float64{}
	for _, p := range pts {
		got[p.ResourceExternalID+"|"+p.Metric] = append(got[p.ResourceExternalID+"|"+p.Metric], p.Value)
	}
	if v := got["vm-1|"+model.MetricCPUUsageCores]; len(v) != 2 || v[0] != 0.5 || v[1] != 1 {
		t.Fatalf("cpu cores: %v", v)
	}
	if v := got["vm-1|"+model.MetricCPUUtil]; len(v) != 2 || v[0] != 0.25 {
		t.Fatalf("cpu utilization: %v", v)
	}
	if v := got["vm-1|"+model.MetricMemUtil]; len(v) != 1 || v[0] != 0.5 {
		t.Fatalf("memory utilization: %v", v)
	}
	if v := got["shop-media|"+model.MetricStorageBytes]; len(v) != 1 || v[0] != 850e9 {
		t.Fatalf("bucket size: %v", v)
	}
}

func TestPermissionAndConfigErrors(t *testing.T) {
	c := newConn(t, fakeCloud(t), "wrong")
	err := c.Validate(context.Background(), connector.Config{})
	if !errors.Is(err, connector.ErrPermission) {
		t.Fatalf("expected permission error, got %v", err)
	}
	if strings.Contains(err.Error(), "wrong") {
		t.Fatal("error message leaks the secret")
	}
	if h := c.Health(context.Background()); h.Status != connector.HealthDown {
		t.Fatalf("health: %+v", h)
	}
	if _, err := New(connector.Config{Settings: map[string]string{"auth_url": "https://x"}}); !errors.Is(err, connector.ErrMissingConfig) {
		t.Fatalf("missing credentials: %v", err)
	}
	if _, err := c.SyncBilling(context.Background(), connector.Period{}); !errors.Is(err, connector.ErrNotSupported) {
		t.Fatal("billing must be unsupported")
	}
}
