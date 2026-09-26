package webhooks

import (
	"errors"
	"testing"

	"github.com/kairn-io/kairn/pkg/connector"
	"github.com/kairn-io/kairn/pkg/model"
)

func inst(t *testing.T, typ string) *conn {
	t.Helper()
	c, err := connector.New(typ, connector.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return c.(*conn)
}

func TestGitLabDeployment(t *testing.T) {
	c := inst(t, "gitlab")
	body := []byte(`{"object_kind":"deployment","status":"success","status_changed_at":"2026-09-13 10:12:00 +0200","environment":"production",
		"ref":"v2.3.0","short_sha":"a1b2c3d4","project":{"name":"search-api","path_with_namespace":"acme/search-api"},"user":{"username":"alice"}}`)
	if err := c.VerifyWebhook(map[string]string{"x-gitlab-token": "s3cret"}, body, "s3cret"); err != nil {
		t.Fatal(err)
	}
	for _, h := range []map[string]string{{"x-gitlab-token": "nope"}, {}} {
		if err := c.VerifyWebhook(h, body, "s3cret"); !errors.Is(err, ErrSignature) {
			t.Fatalf("bad token accepted: %v", h)
		}
	}
	if err := c.VerifyWebhook(map[string]string{"x-gitlab-token": ""}, body, ""); !errors.Is(err, ErrSignature) {
		t.Fatal("empty secret must reject everything")
	}
	evs, err := c.ParseWebhook(nil, body)
	if err != nil || len(evs) != 1 {
		t.Fatalf("%v %+v", err, evs)
	}
	e := evs[0]
	if e.Kind != model.EventDeployment || e.Payload["service"] != "search-api" || e.Payload["version"] != "v2.3.0@a1b2c3d4" || e.TS.Hour() != 8 {
		t.Fatalf("event: %+v", e)
	}
	if evs, _ := c.ParseWebhook(nil, []byte(`{"object_kind":"deployment","status":"running"}`)); len(evs) != 0 {
		t.Fatal("running deployments are not events")
	}
}

func TestGitHubAndFluxSignatures(t *testing.T) {
	gh := inst(t, "github")
	body := []byte(`{"deployment_status":{"state":"success","created_at":"2026-09-13T08:12:00Z","environment":"prod"},
		"deployment":{"ref":"main","sha":"0123456789abcdef"},"repository":{"name":"shop-frontend"},"sender":{"login":"bob"}}`)
	sig := "sha256=" + hmacHex("k", body)
	if err := gh.VerifyWebhook(map[string]string{"x-hub-signature-256": sig}, body, "k"); err != nil {
		t.Fatal(err)
	}
	if err := gh.VerifyWebhook(map[string]string{"x-hub-signature-256": sig}, append(body, ' '), "k"); err == nil {
		t.Fatal("tampered body accepted")
	}
	evs, _ := gh.ParseWebhook(map[string]string{"x-github-event": "deployment_status"}, body)
	if len(evs) != 1 || evs[0].Payload["version"] != "main@01234567" {
		t.Fatalf("github: %+v", evs)
	}
	fl := inst(t, "flux")
	fb := []byte(`{"involvedObject":{"kind":"HelmRelease","name":"search-api","namespace":"search"},"severity":"info","timestamp":"2026-09-13T08:12:00Z",
		"reason":"UpgradeSucceeded","message":"upgrade done","metadata":{"revision":"2.3.0"}}`)
	if err := fl.VerifyWebhook(map[string]string{"x-signature": "sha256=" + hmacHex("f", fb)}, fb, "f"); err != nil {
		t.Fatal(err)
	}
	evs, _ = fl.ParseWebhook(nil, fb)
	if len(evs) != 1 || evs[0].Payload["namespace"] != "search" || evs[0].Kind != model.EventDeployment {
		t.Fatalf("flux: %+v", evs)
	}
}

func TestIncidentSources(t *testing.T) {
	am := inst(t, "alertmanager")
	body := []byte(`{"alerts":[{"status":"firing","labels":{"alertname":"HighLatency","severity":"critical","namespace":"shop","deployment":"web"},
		"annotations":{"summary":"Latence p95 > 1 s"},"startsAt":"2026-09-20T10:00:00Z","fingerprint":"fp1"},
		{"status":"resolved","labels":{"alertname":"DiskFull"},"annotations":{},"startsAt":"2026-09-20T09:00:00Z","endsAt":"2026-09-20T09:30:00Z","fingerprint":"fp2"}]}`)
	if err := am.VerifyWebhook(map[string]string{"authorization": "Bearer tok"}, body, "tok"); err != nil {
		t.Fatal(err)
	}
	if err := am.VerifyWebhook(map[string]string{"authorization": "Basic tok"}, body, "tok"); err == nil {
		t.Fatal("non-bearer accepted")
	}
	evs, _ := am.ParseWebhook(nil, body)
	if len(evs) != 2 || evs[0].Payload["status"] != "open" || evs[0].Payload["service"] != "web" || evs[1].Payload["status"] != "resolved" ||
		evs[1].TS.Minute() != 30 || evs[1].Title != "DiskFull" {
		t.Fatalf("alertmanager: %+v", evs)
	}
	pd := inst(t, "pagerduty")
	pb := []byte(`{"event":{"event_type":"incident.resolved","occurred_at":"2026-09-20T10:05:00Z","data":{"id":"P123","title":"DB down","urgency":"high","service":{"summary":"shop-db"}}}}`)
	if err := pd.VerifyWebhook(map[string]string{"x-pagerduty-signature": "v1=deadbeef, v1=" + hmacHex("p", pb)}, pb, "p"); err != nil {
		t.Fatal(err)
	}
	evs, _ = pd.ParseWebhook(nil, pb)
	if len(evs) != 1 || evs[0].Payload["incident_key"] != "P123" || evs[0].Payload["status"] != "resolved" {
		t.Fatalf("pagerduty: %+v", evs)
	}
	og := inst(t, "opsgenie")
	evs, _ = og.ParseWebhook(nil, []byte(`{"action":"Create","alert":{"alertId":"A1","message":"CPU haute","priority":"P2","entity":"batch-1"}}`))
	if len(evs) != 1 || evs[0].Payload["status"] != "open" || evs[0].Payload["service"] != "batch-1" {
		t.Fatalf("opsgenie: %+v", evs)
	}
	ar := inst(t, "argocd")
	evs, _ = ar.ParseWebhook(nil, []byte(`{"app":"search-api","namespace":"search","revision":"0123456789","phase":"Succeeded","timestamp":"2026-09-13T08:12:00Z",
		"images":["registry.example/search/api:v2.3.0"]}`))
	if len(evs) != 1 || evs[0].Payload["version"] != "v2.3.0" {
		t.Fatalf("argocd: %+v", evs)
	}
	if !inst(t, "gitlab").PushOnly() {
		t.Fatal("webhook connectors are push only")
	}
}
