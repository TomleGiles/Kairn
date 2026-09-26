package prometheus

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kairn-io/kairn/pkg/connector"
	"github.com/kairn-io/kairn/pkg/model"
)

func fakeProm(t *testing.T, seen *[]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer prom-token" || r.Header.Get("X-Scope-OrgID") != "acme" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		q := r.URL.Query().Get("query")
		*seen = append(*seen, q)
		resp := map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": []any{}}}
		series := func(labels map[string]string, values ...[2]any) {
			resp["data"].(map[string]any)["result"] = []any{map[string]any{"metric": labels, "values": values}}
		}
		switch {
		case r.URL.Path == "/api/v1/query":
			resp["data"] = map[string]any{"resultType": "vector", "result": []any{map[string]any{"metric": map[string]any{}, "value": []any{1758800000, "42"}}}}
		case strings.Contains(q, "container_cpu_usage_seconds_total"):
			series(map[string]string{"namespace": "shop", "pod": "web-abc"}, [2]any{1758794400, "0.25"}, [2]any{1758794700, "NaN"}, [2]any{1758795000, "0.5"})
		case strings.Contains(q, `resource="memory"`) && strings.Contains(q, "kube_pod_container_resource_requests"):
			series(map[string]string{"namespace": "shop", "pod": "web-abc"}, [2]any{1758794400, "536870912"})
		case strings.Contains(q, "kube_node_status_capacity") && strings.Contains(q, `resource="cpu"`):
			series(map[string]string{"node": "node-1"}, [2]any{1758794400, "4"})
		case strings.Contains(q, "kube_deployment_status_replicas"):
			series(map[string]string{"namespace": "shop", "deployment": "web"}, [2]any{1758794400, "3"})
		case strings.Contains(q, "node_cpu_seconds_total"):
			series(map[string]string{"instance_id": "vm-1"}, [2]any{1758794400, "0.37"})
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func conn(t *testing.T, srv *httptest.Server, settings map[string]string) *Conn {
	t.Helper()
	s := map[string]string{"url": srv.URL, "cluster_name": "prod-gra", "matchers": `cluster="prod-gra"`, "tenant_id": "acme"}
	for k, v := range settings {
		s[k] = v
	}
	c, err := New(connector.Config{Settings: s, Secrets: map[string]string{"token": "prom-token"}})
	if err != nil {
		t.Fatal(err)
	}
	return c.(*Conn)
}

func TestKubernetesMode(t *testing.T) {
	var seen []string
	srv := fakeProm(t, &seen)
	c := conn(t, srv, nil)
	if err := c.Validate(context.Background(), connector.Config{}); err != nil {
		t.Fatal(err)
	}
	from := time.Unix(1758794400, 0).UTC()
	ctx, sink := connector.WithErrorSink(context.Background())
	ch, err := c.SyncMetrics(ctx, connector.TimeWindow{From: from, To: from.Add(2 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	pts := connector.Collect(ch)
	if err := sink.Err(); err != nil {
		t.Fatal(err)
	}
	got := map[string][]float64{}
	for _, p := range pts {
		got[p.ResourceType+"|"+p.ResourceExternalID+"|"+p.Metric] = append(got[p.ResourceType+"|"+p.ResourceExternalID+"|"+p.Metric], p.Value)
	}
	if v := got[model.TypeK8sPod+"|prod-gra/shop/web-abc|"+model.MetricCPUUsageCores]; len(v) != 2 || v[1] != 0.5 {
		t.Fatalf("cpu usage (NaN dropped): %v", got)
	}
	if v := got[model.TypeK8sPod+"|prod-gra/shop/web-abc|"+model.MetricMemRequestBytes]; len(v) != 1 || v[0] != 536870912 {
		t.Fatalf("memory requests: %v", got)
	}
	if got[model.TypeK8sNode+"|node-1|"+model.MetricCPUCapacityCores][0] != 4 || got[model.TypeK8sWorkload+"|prod-gra/shop/Deployment/web|"+model.MetricReplicas][0] != 3 {
		t.Fatalf("node/workload: %v", got)
	}
	for _, q := range seen[1:] {
		if !strings.Contains(q, `cluster="prod-gra"`) {
			t.Fatalf("matchers must be injected in every selector: %s", q)
		}
	}
}

func TestNodeModeAndValidation(t *testing.T) {
	var seen []string
	srv := fakeProm(t, &seen)
	c := conn(t, srv, map[string]string{"mode": "node", "cluster_name": ""})
	from := time.Unix(1758794400, 0).UTC()
	ctx, sink := connector.WithErrorSink(context.Background())
	ch, err := c.SyncMetrics(ctx, connector.TimeWindow{From: from, To: from.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	pts := connector.Collect(ch)
	if err := sink.Err(); err != nil {
		t.Fatal(err)
	}
	if len(pts) != 1 || pts[0].ResourceType != model.TypeInstance || pts[0].ResourceExternalID != "vm-1" || pts[0].Metric != model.MetricCPUUtil {
		t.Fatalf("node mode: %+v", pts)
	}
	// Aucune injection PromQL via les sélecteurs.
	for _, bad := range []string{`cluster="a"}) or vector(1`, `cluster=a`, `x`, `1abc="v"`} {
		if _, err := parseMatchers(bad); err == nil {
			t.Fatalf("matcher %q accepted", bad)
		}
	}
	if m, err := parseMatchers(`cluster="prod", env=~"prod|staging"`); err != nil || m != `cluster="prod",env=~"prod|staging"` {
		t.Fatalf("valid matchers: %q %v", m, err)
	}
	if _, err := New(connector.Config{Settings: map[string]string{"url": srv.URL}}); !errors.Is(err, connector.ErrMissingConfig) {
		t.Fatalf("kubernetes mode requires a cluster name: %v", err)
	}
	if _, err := c.SyncInventory(context.Background(), time.Time{}); !errors.Is(err, connector.ErrNotSupported) {
		t.Fatal("prometheus has no inventory")
	}
	bad := conn(t, srv, map[string]string{"tenant_id": "other"})
	if err := bad.Validate(context.Background(), connector.Config{}); !errors.Is(err, connector.ErrPermission) {
		t.Fatalf("expected permission error: %v", err)
	}
}
