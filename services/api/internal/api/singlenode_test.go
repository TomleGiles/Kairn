package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "github.com/kairn-io/kairn/connectors/agent"
	"github.com/kairn-io/kairn/pkg/gateway"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/tenancy"
	"github.com/kairn-io/kairn/pkg/tsdb"
)

// En mode nœud unique, la passerelle d'ingestion est servie par l'API : le
// jeton de l'agent (porté en Bearer) ne doit pas être évalué comme un jeton
// d'API, sinon l'agent et les webhooks authentifiés par Bearer sont rejetés.
func TestSingleNodeGatewayBypassesAPIAuth(t *testing.T) {
	e := newEnv(t)
	gw := &gateway.Gateway{Store: e.st, TSDB: e.db, Keyring: e.srv.Keyring}
	e.srv.Handle("/ingest/", gw.Handler())

	alice := e.login("alice@example.com")
	org := e.org(alice, "Acme")
	r := e.do(http.MethodPost, "/api/v1/orgs/"+org.ID+"/connectors", alice, map[string]any{"type": "agent", "name": "datacenter"})
	expect(t, r, http.StatusCreated)
	var conn connectorView
	r.JSON(t, &conn)
	token := conn.WebhookURL[strings.LastIndex(conn.WebhookURL, "/")+1:]

	body := `{"resourceMetrics":[{"resource":{"attributes":[{"key":"host.id","value":{"stringValue":"srv-1"}}]},` +
		`"scopeMetrics":[{"metrics":[{"name":"system.cpu.utilization","gauge":{"dataPoints":[{"timeUnixNano":"1789900000000000000","asDouble":0.42}]}}]}]}]}`
	post := func(tok string) int {
		req := httptest.NewRequest(http.MethodPost, "/ingest/v1/otlp/v1/metrics", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		e.srv.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := post(token); code != http.StatusOK {
		t.Fatalf("agent push through the API server: got %d, want 200", code)
	}
	if code := post("not-a-token"); code != http.StatusUnauthorized {
		t.Fatalf("the gateway still authenticates the agent: got %d", code)
	}
	at := time.Unix(0, 1789900000000000000).UTC()
	series, err := e.db.QueryMetrics(tenancy.WithOrg(context.Background(), org.ID),
		tsdb.MetricQuery{Metrics: []string{model.MetricCPUUtil}, From: at.Add(-time.Hour), To: at.Add(time.Hour)})
	if err != nil || len(series) != 1 {
		t.Fatalf("pushed point must be stored: %v %+v", err, series)
	}
	// Les routes de l'API restent protégées.
	if r := e.do(http.MethodGet, "/api/v1/orgs/"+org.ID+"/connectors", "not-a-token", nil); r.Code != http.StatusUnauthorized {
		t.Fatalf("API auth unchanged: %d", r.Code)
	}
}
