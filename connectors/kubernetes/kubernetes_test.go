package kubernetes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kairn-io/kairn/pkg/connector"
	"github.com/kairn-io/kairn/pkg/model"
)

func list(items ...any) map[string]any {
	return map[string]any{"kind": "List", "metadata": map[string]any{}, "items": items}
}

func obj(name, ns string, extra map[string]any) map[string]any {
	m := map[string]any{"metadata": map[string]any{"name": name, "namespace": ns, "creationTimestamp": "2026-09-01T10:00:00Z", "labels": map[string]any{"team": "shop"}}}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func podTemplate(cpu, mem, image string) map[string]any {
	return map[string]any{"containers": []any{map[string]any{"image": image, "resources": map[string]any{"requests": map[string]any{"cpu": cpu, "memory": mem}}}}}
}

// fakeAPI simule l'API server Kubernetes (réponses au format réel).
func fakeAPI(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sa-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		write := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		switch r.URL.Path {
		case "/version":
			write(map[string]any{"gitVersion": "v1.31.4"})
		case "/api/v1/nodes":
			write(list(map[string]any{
				"metadata": map[string]any{"name": "node-1", "creationTimestamp": "2026-08-01T00:00:00Z",
					"labels": map[string]any{"node.kubernetes.io/instance-type": "b2-15", "topology.kubernetes.io/region": "GRA7"}},
				"spec":   map[string]any{"providerID": "openstack:///9f1c2b44-0d1e-4b6c-a0a1-222222222222"},
				"status": map[string]any{"capacity": map[string]any{"cpu": "4", "memory": "15Gi"}, "allocatable": map[string]any{"cpu": "3920m"}},
			}))
		case "/api/v1/namespaces":
			write(list(obj("shop", "", nil)))
		case "/apis/autoscaling/v2/horizontalpodautoscalers":
			write(list(obj("web", "shop", map[string]any{"spec": map[string]any{"maxReplicas": 10, "scaleTargetRef": map[string]any{"kind": "Deployment", "name": "web"}}})))
		case "/apis/apps/v1/deployments":
			write(list(obj("web", "shop", map[string]any{"spec": map[string]any{"replicas": 3, "template": map[string]any{"spec": podTemplate("500m", "512Mi", "registry/shop/web:v2.3.0")}}})))
		case "/apis/apps/v1/statefulsets":
			write(list(obj("db", "shop", map[string]any{"spec": map[string]any{"replicas": 1, "template": map[string]any{"spec": podTemplate("2", "4Gi", "postgres:16")}}})))
		case "/apis/apps/v1/daemonsets":
			write(list(obj("agent", "kube-system", map[string]any{"spec": map[string]any{"template": map[string]any{"spec": podTemplate("100m", "128Mi", "agent@sha256:abc")}},
				"status": map[string]any{"desiredNumberScheduled": 3}})))
		case "/apis/batch/v1/cronjobs":
			w.WriteHeader(http.StatusForbidden) // permission optionnelle absente
		case "/apis/apps/v1/replicasets":
			rs := obj("web-7d9f", "shop", nil)
			rs["metadata"].(map[string]any)["ownerReferences"] = []any{map[string]any{"kind": "Deployment", "name": "web", "controller": true}}
			write(list(rs))
		case "/apis/batch/v1/jobs":
			w.WriteHeader(http.StatusNotFound)
		case "/api/v1/pods":
			if r.URL.Query().Get("limit") != "1" && r.URL.Query().Get("fieldSelector") != "status.phase!=Succeeded,status.phase!=Failed" {
				t.Errorf("pods must exclude finished phases")
			}
			pod := func(name, ownerKind, owner string) map[string]any {
				p := obj(name, "shop", map[string]any{"spec": map[string]any{"nodeName": "node-1", "containers": podTemplate("500m", "512Mi", "registry/shop/web:v2.3.0")["containers"],
					"initContainers": []any{map[string]any{"image": "init", "resources": map[string]any{"requests": map[string]any{"cpu": "1", "memory": "64Mi"}}}}},
					"status": map[string]any{"phase": "Running", "qosClass": "Burstable"}})
				p["metadata"].(map[string]any)["ownerReferences"] = []any{map[string]any{"kind": ownerKind, "name": owner, "controller": true}}
				return p
			}
			if r.URL.Query().Get("continue") == "" {
				page := list(pod("web-7d9f-abc", "ReplicaSet", "web-7d9f"))
				page["metadata"] = map[string]any{"continue": "tok-2"}
				write(page)
				return
			}
			write(list(pod("db-0", "StatefulSet", "db")))
		case "/api/v1/persistentvolumeclaims":
			write(list(obj("data-db-0", "shop", map[string]any{"spec": map[string]any{"storageClassName": "csi-cinder-high-speed", "volumeName": "pvc-1",
				"resources": map[string]any{"requests": map[string]any{"storage": "20Gi"}}}, "status": map[string]any{"phase": "Bound", "capacity": map[string]any{"storage": "20Gi"}}})))
		case "/apis/metrics.k8s.io/v1beta1/pods":
			write(list(map[string]any{"metadata": map[string]any{"name": "web-7d9f-abc", "namespace": "shop"},
				"containers": []any{map[string]any{"usage": map[string]any{"cpu": "250000000n", "memory": "300Mi"}}}}))
		case "/api/v1/events":
			write(list(
				map[string]any{"metadata": map[string]any{"name": "e1", "namespace": "shop"}, "reason": "SuccessfulRescale", "type": "Normal",
					"message": "New size: 5; reason: cpu resource utilization above target", "lastTimestamp": "2026-09-20T10:00:00Z",
					"involvedObject": map[string]any{"kind": "HorizontalPodAutoscaler", "name": "web", "namespace": "shop"}},
				map[string]any{"metadata": map[string]any{"name": "e2", "namespace": "shop"}, "reason": "Pulled", "type": "Normal", "message": "noise",
					"lastTimestamp": "2026-09-20T10:01:00Z", "involvedObject": map[string]any{"kind": "Pod", "name": "web-7d9f-abc", "namespace": "shop"}},
				map[string]any{"metadata": map[string]any{"name": "e3", "namespace": "shop"}, "reason": "OOMKilling", "type": "Warning", "message": "oom",
					"lastTimestamp": "2026-09-01T10:00:00Z", "involvedObject": map[string]any{"kind": "Pod", "name": "old", "namespace": "shop"}},
			))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newConn(t *testing.T, srv *httptest.Server, token string) *Conn {
	t.Helper()
	c, err := New(connector.Config{Settings: map[string]string{"cluster_name": "prod-gra", "api_server": srv.URL, "control_plane_tier": "free"},
		Secrets: map[string]string{"token": token}})
	if err != nil {
		t.Fatal(err)
	}
	conn := c.(*Conn)
	conn.now = func() time.Time { return time.Date(2026, 9, 25, 12, 0, 30, 0, time.UTC) }
	return conn
}

func TestInventory(t *testing.T) {
	c := newConn(t, fakeAPI(t), "sa-token")
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
		by[r.Type+"|"+r.ExternalID] = r
	}
	cluster := by[model.TypeK8sCluster+"|prod-gra"]
	if cluster.Attributes["control_plane_tier"] != "free" || cluster.Attributes["version"] != "v1.31.4" {
		t.Fatalf("cluster: %+v", cluster)
	}
	node := by[model.TypeK8sNode+"|node-1"]
	if node.Attributes["provider_instance_id"] != "9f1c2b44-0d1e-4b6c-a0a1-222222222222" || node.Attributes["cpu_capacity_cores"] != 4.0 ||
		node.Attributes["mem_capacity_gb"] != 15.0 || node.Attributes["instance_type"] != "b2-15" || node.Region != "GRA7" {
		t.Fatalf("node: %+v", node)
	}
	web := by[model.TypeK8sWorkload+"|prod-gra/shop/Deployment/web"]
	if web.Attributes["replicas"] != 3 || web.Attributes["cpu_request_cores"] != 0.5 || web.Attributes["mem_request_gb"] != 0.5 ||
		web.Attributes["image_tag"] != "v2.3.0" || web.Attributes["hpa_max"] != 10 {
		t.Fatalf("deployment: %+v", web.Attributes)
	}
	if ds := by[model.TypeK8sWorkload+"|prod-gra/kube-system/DaemonSet/agent"]; ds.Attributes["replicas"] != 3 || ds.Attributes["image_tag"] != "sha256:abc" {
		t.Fatalf("daemonset: %+v", ds.Attributes)
	}
	pod := by[model.TypeK8sPod+"|prod-gra/shop/web-7d9f-abc"]
	// Init container (1 cœur) supérieur à la somme des conteneurs (0,5) : règle du scheduler.
	if pod.Attributes["cpu_request_cores"] != 1.0 || pod.Attributes["k8s.workload"] != "web" || pod.Attributes["node"] != "node-1" {
		t.Fatalf("pod: %+v", pod.Attributes)
	}
	rels := map[string]string{}
	for _, e := range pod.Parents {
		rels[e.Relation] = e.ParentExternalID
	}
	if rels[model.RelOwns] != "prod-gra/shop/Deployment/web" || rels[model.RelRunsOn] != "node-1" || rels[model.RelContains] != "prod-gra/shop" {
		t.Fatalf("pod edges: %+v", rels)
	}
	if db := by[model.TypeK8sPod+"|prod-gra/shop/db-0"]; db.Attributes["k8s.workload"] != "db" {
		t.Fatalf("second page / statefulset owner: %+v", db.Attributes)
	}
	if pvc := by[model.TypeK8sPVC+"|prod-gra/shop/data-db-0"]; pvc.Attributes["size_gb"] != 20.0 {
		t.Fatalf("pvc: %+v", pvc.Attributes)
	}
}

func TestMetricsAndEvents(t *testing.T) {
	c := newConn(t, fakeAPI(t), "sa-token")
	ctx, sink := connector.WithErrorSink(context.Background())
	now := c.now()
	ch, err := c.SyncMetrics(ctx, connector.TimeWindow{From: now.Add(-time.Hour), To: now})
	if err != nil {
		t.Fatal(err)
	}
	pts := connector.Collect(ch)
	if err := sink.Err(); err != nil {
		t.Fatal(err)
	}
	vals := map[string]float64{}
	for _, p := range pts {
		vals[p.ResourceExternalID+"|"+p.Metric] = p.Value
	}
	if vals["prod-gra/shop/web-7d9f-abc|"+model.MetricCPUUsageCores] != 0.25 || vals["prod-gra/shop/web-7d9f-abc|"+model.MetricMemUsageBytes] != 300*(1<<20) ||
		vals["node-1|"+model.MetricCPUCapacityCores] != 4 {
		t.Fatalf("metrics: %v", vals)
	}
	if _, err := c.SyncMetrics(context.Background(), connector.TimeWindow{From: now.AddDate(0, 0, -3), To: now.AddDate(0, 0, -2)}); !errors.Is(err, connector.ErrNotSupported) {
		t.Fatal("historical windows are not available from the API server")
	}
	ectx, esink := connector.WithErrorSink(context.Background())
	evch, err := c.SyncEvents(ectx, time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	evs := connector.Collect(evch)
	if err := esink.Err(); err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Kind != model.EventHPAScale || evs[0].ResourceExternalID != "prod-gra/shop/Deployment/web" {
		t.Fatalf("events (noise and old events filtered): %+v", evs)
	}
}

func TestQuantitiesAndErrors(t *testing.T) {
	for q, want := range map[string]float64{"250m": 0.25, "2": 2, "1.5": 1.5, "512Mi": 512 << 20, "1Gi": 1 << 30, "1G": 1e9, "1e3": 1000, "100000000n": 0.1, "": 0, "abc": 0} {
		if got := ParseQuantity(q); got != want {
			t.Fatalf("ParseQuantity(%q) = %v, want %v", q, got, want)
		}
	}
	c := newConn(t, fakeAPI(t), "bad")
	if err := c.Validate(context.Background(), connector.Config{}); !errors.Is(err, connector.ErrPermission) {
		t.Fatalf("expected permission error: %v", err)
	}
	if _, err := New(connector.Config{Settings: map[string]string{"api_server": "https://x"}}); !errors.Is(err, connector.ErrMissingConfig) {
		t.Fatalf("cluster_name required: %v", err)
	}
	if got := providerInstanceID("aws:///eu-west-3a/i-0abc"); got != "i-0abc" {
		t.Fatalf("provider id: %s", got)
	}
}
