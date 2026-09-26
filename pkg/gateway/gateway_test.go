package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	colmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"

	_ "github.com/kairn-io/kairn/connectors/agent"
	_ "github.com/kairn-io/kairn/connectors/webhooks"
	"github.com/kairn-io/kairn/pkg/bus"
	"github.com/kairn-io/kairn/pkg/ids"
	"github.com/kairn-io/kairn/pkg/ingest"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/secrets"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/store/memstore"
	"github.com/kairn-io/kairn/pkg/tenancy"
	"github.com/kairn-io/kairn/pkg/tsdb"
	"github.com/kairn-io/kairn/pkg/tsdb/memtsdb"
)

type fixture struct {
	gw  *Gateway
	srv *httptest.Server
	st  *memstore.Store
	db  *memtsdb.DB
	org string
	ctx context.Context
	now time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	kek, _ := secrets.GenerateKEK()
	w, _ := secrets.NewLocalKEK(kek)
	f := &fixture{st: memstore.New(), db: memtsdb.New(), org: ids.New(), now: time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)}
	f.ctx = tenancy.WithOrg(context.Background(), f.org)
	o := model.Organization{ID: f.org, Name: "Acme", Slug: "acme", Plan: model.PlanTeam, Currency: "EUR", Locale: "fr", Timezone: "UTC"}
	if err := f.st.Orgs().Create(f.ctx, &o); err != nil {
		t.Fatal(err)
	}
	f.gw = &Gateway{Store: f.st, TSDB: f.db, Bus: bus.NewMemory(slog.New(slog.NewTextHandler(io.Discard, nil))), Keyring: secrets.NewKeyring(w),
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), ServiceToken: "svc", Now: func() time.Time { return f.now }}
	f.srv = httptest.NewServer(f.gw.Handler())
	t.Cleanup(f.srv.Close)
	return f
}

// connector crée un connecteur avec jeton de webhook et secrets chiffrés.
func (f *fixture) connector(t *testing.T, typ string, secretsMap map[string]string) (model.Connector, string) {
	t.Helper()
	tok := "wh_" + ids.New()
	c := model.Connector{ID: ids.New(), Type: typ, Name: typ, Settings: map[string]string{}, Enabled: true, Status: model.ConnectorPending,
		IntervalSeconds: 3600, WebhookToken: &tok}
	if len(secretsMap) > 0 {
		enc, err := f.gw.Keyring.EncryptMap(f.ctx, secretsMap, ingest.SecretsAAD(f.org, c.ID))
		if err != nil {
			t.Fatal(err)
		}
		c.SecretsEnc = enc
	}
	if err := f.st.Connectors().Create(f.ctx, &c); err != nil {
		t.Fatal(err)
	}
	return c, tok
}

func (f *fixture) post(t *testing.T, path string, headers map[string]string, body []byte) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, f.srv.URL+path, bytes.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestWebhookDeploymentIsLinkedToWorkload(t *testing.T) {
	f := newFixture(t)
	c, tok := f.connector(t, "gitlab", map[string]string{"webhook_secret": "s3cret"})
	// Workload existant (connecteur Kubernetes) portant le nom du projet GitLab.
	k8s := ids.New()
	wl := model.Resource{ID: ids.Resource(f.org, k8s, model.TypeK8sWorkload, "prod/search/Deployment/search-api"), ConnectorID: k8s, Provider: "kubernetes",
		Type: model.TypeK8sWorkload, ExternalID: "prod/search/Deployment/search-api", Name: "search-api", ValidFrom: f.now.Add(-time.Hour),
		Attributes: map[string]any{"k8s.namespace": "search"}}
	if err := f.st.Resources().InsertVersions(f.ctx, []model.Resource{wl}); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"object_kind":"deployment","status":"success","status_changed_at":"2026-09-25T10:00:00Z","environment":"production","ref":"v2.3.0",
		"short_sha":"a1b2c3d4","project":{"name":"search-api"},"user":{"username":"alice"}}`)
	if r := f.post(t, "/ingest/v1/webhooks/"+tok, map[string]string{"X-Gitlab-Token": "wrong"}, body); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad token accepted: %d", r.StatusCode)
	}
	if r := f.post(t, "/ingest/v1/webhooks/unknown", map[string]string{"X-Gitlab-Token": "s3cret"}, body); r.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown webhook: %d", r.StatusCode)
	}
	if r := f.post(t, "/ingest/v1/webhooks/"+tok, map[string]string{"X-Gitlab-Token": "s3cret"}, body); r.StatusCode != http.StatusAccepted {
		t.Fatalf("webhook: %d", r.StatusCode)
	}
	evs, _ := f.db.QueryEvents(f.ctx, tsdb.EventQuery{})
	if len(evs) != 1 || evs[0].Kind != model.EventDeployment || evs[0].ResourceID != wl.ID || evs[0].Source != c.Type {
		t.Fatalf("event: %+v", evs)
	}
}

func TestAlertmanagerOpensAndResolvesIncidents(t *testing.T) {
	f := newFixture(t)
	_, tok := f.connector(t, "alertmanager", map[string]string{"webhook_secret": "am"})
	send := func(status string) {
		body, _ := json.Marshal(map[string]any{"alerts": []any{map[string]any{"status": status, "fingerprint": "fp1", "startsAt": "2026-09-25T10:00:00Z",
			"endsAt": "2026-09-25T11:00:00Z", "labels": map[string]string{"alertname": "HighLatency"}, "annotations": map[string]string{"summary": "Latence élevée"}}}})
		if r := f.post(t, "/ingest/v1/webhooks/"+tok, map[string]string{"Authorization": "Bearer am"}, body); r.StatusCode != http.StatusAccepted {
			t.Fatalf("alertmanager %s: %d", status, r.StatusCode)
		}
	}
	send("firing")
	incs, _ := f.st.Incidents().List(f.ctx, store.ListQuery{})
	if len(incs) != 1 || incs[0].Status != "open" || incs[0].Title != "Latence élevée" {
		t.Fatalf("incident opened: %+v", incs)
	}
	send("firing") // doublon : pas de second incident
	send("resolved")
	incs, _ = f.st.Incidents().List(f.ctx, store.ListQuery{})
	if len(incs) != 1 || incs[0].Status != "resolved" || incs[0].ResolvedAt == nil {
		t.Fatalf("incident resolved: %+v", incs)
	}
}

func otlpRequest(host string, value float64, ts time.Time) []byte {
	kv := func(k, v string) *commonpb.KeyValue {
		return &commonpb.KeyValue{Key: k, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v}}}
	}
	req := &colmetrics.ExportMetricsServiceRequest{ResourceMetrics: []*metricspb.ResourceMetrics{{
		Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{kv("host.id", host)}},
		ScopeMetrics: []*metricspb.ScopeMetrics{{Metrics: []*metricspb.Metric{
			{Name: "system.cpu.utilization", Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{DataPoints: []*metricspb.NumberDataPoint{
				{TimeUnixNano: uint64(ts.UnixNano()), Value: &metricspb.NumberDataPoint_AsDouble{AsDouble: value}}}}}}, //nolint:gosec
			{Name: "kairn.memory.usage_bytes", Data: &metricspb.Metric_Sum{Sum: &metricspb.Sum{DataPoints: []*metricspb.NumberDataPoint{
				{TimeUnixNano: uint64(ts.UnixNano()), Value: &metricspb.NumberDataPoint_AsInt{AsInt: 8 << 30}}}}}}, //nolint:gosec
		}}},
	}}}
	b, _ := proto.Marshal(req)
	return b
}

func TestAgentInventoryAndOTLP(t *testing.T) {
	f := newFixture(t)
	c, tok := f.connector(t, "agent", nil)
	auth := map[string]string{"Authorization": "Bearer " + tok}
	inv, _ := json.Marshal(AgentHost{HostID: "machine-1", Hostname: "db-1", OS: "Ubuntu 24.04", Arch: "amd64", VCPUs: 8, RAMGB: 32, Labels: map[string]string{"site": "par1"}})
	if r := f.post(t, "/ingest/v1/agent/inventory", map[string]string{"Authorization": "Bearer nope"}, inv); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad agent token: %d", r.StatusCode)
	}
	if r := f.post(t, "/ingest/v1/agent/inventory", auth, inv); r.StatusCode != http.StatusAccepted {
		t.Fatalf("inventory: %d", r.StatusCode)
	}
	hosts, _ := f.st.Resources().Current(f.ctx, store.ResourceFilter{Types: []string{model.TypeHost}})
	if len(hosts) != 1 || hosts[0].Labels["site"] != "par1" || hosts[0].ConnectorID != c.ID {
		t.Fatalf("host: %+v", hosts)
	}
	h := map[string]string{"Authorization": "Bearer " + tok, "Content-Type": "application/x-protobuf"}
	if r := f.post(t, "/ingest/v1/otlp/v1/metrics", h, otlpRequest("machine-1", 0.42, f.now)); r.StatusCode != http.StatusOK {
		t.Fatalf("otlp: %d", r.StatusCode)
	}
	if r := f.post(t, "/ingest/v1/otlp/v1/metrics", h, []byte("garbage")); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid otlp: %d", r.StatusCode)
	}
	series, _ := f.db.QueryMetrics(f.ctx, tsdb.MetricQuery{ResourceIDs: []string{hosts[0].ID}, From: f.now.Add(-time.Minute), To: f.now.Add(time.Minute)})
	if len(series) != 2 {
		t.Fatalf("metrics attached to the host: %+v", series)
	}
}

func TestProbesAndUptimeIncidents(t *testing.T) {
	f := newFixture(t)
	check := model.UptimeCheck{Name: "Boutique", Kind: "http", Target: "https://shop.example.com", IntervalSeconds: 60, Regions: []string{"eu-west-gra", "eu-central-waw"},
		FailThreshold: 2, Enabled: true}
	if err := f.st.UptimeChecks().Create(f.ctx, &check); err != nil {
		t.Fatal(err)
	}
	get := func(token, region string) (int, []ProbeCheck) {
		req, _ := http.NewRequest(http.MethodGet, f.srv.URL+"/ingest/v1/probes/checks?region="+region, nil)
		req.Header.Set("X-Kairn-Service-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out []ProbeCheck
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	if code, _ := get("nope", "eu-west-gra"); code != http.StatusUnauthorized {
		t.Fatalf("probe endpoints require the service token: %d", code)
	}
	if code, checks := get("svc", "eu-west-gra"); code != http.StatusOK || len(checks) != 1 || checks[0].OrgID != f.org {
		t.Fatalf("checks: %d %+v", code, checks)
	}
	if _, checks := get("svc", "us-east"); len(checks) != 0 {
		t.Fatalf("region filter: %+v", checks)
	}
	report := func(upGra, upWaw bool) {
		body, _ := json.Marshal([]model.UptimeResult{
			{OrgID: f.org, CheckID: check.ID, Region: "eu-west-gra", TS: f.now, Up: upGra, StatusCode: 503},
			{OrgID: f.org, CheckID: check.ID, Region: "eu-central-waw", TS: f.now, Up: upWaw, StatusCode: 503},
			{OrgID: "not-a-uuid", CheckID: check.ID, Region: "x", TS: f.now},
		})
		if r := f.post(t, "/ingest/v1/probes/results", map[string]string{"X-Kairn-Service-Token": "svc"}, body); r.StatusCode >= 300 {
			t.Fatalf("results: %d", r.StatusCode)
		}
	}
	report(false, true) // une seule région en échec : sous le seuil
	if _, err := f.st.Incidents().OpenForCheck(f.ctx, check.ID); err == nil {
		t.Fatal("incident opened below the failure threshold")
	}
	f.now = f.now.Add(time.Minute)
	report(false, false)
	inc, err := f.st.Incidents().OpenForCheck(f.ctx, check.ID)
	if err != nil || inc.Title != "Boutique indisponible" {
		t.Fatalf("incident: %+v %v", inc, err)
	}
	f.now = f.now.Add(time.Minute)
	report(true, true)
	if _, err := f.st.Incidents().OpenForCheck(f.ctx, check.ID); err == nil {
		t.Fatal("incident must be resolved once every region is up")
	}
}
