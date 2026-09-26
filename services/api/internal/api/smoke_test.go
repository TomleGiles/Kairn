package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/seed"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/tenancy"
)

// TestEveryReadEndpointOnSeededOrg appelle chaque opération GET d'organisation
// (sans identifiant d'objet) sur l'organisation de démonstration : aucune ne
// doit échouer en erreur serveur, et toutes celles sans paramètre obligatoire
// doivent répondre 200.
func TestEveryReadEndpointOnSeededOrg(t *testing.T) {
	e, owner := seededEnv(t)
	doc := e.srv.API.OpenAPI()
	paths := make([]string, 0, len(doc.Paths))
	for p := range doc.Paths {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	checked := 0
	for _, p := range paths {
		item := doc.Paths[p]
		if item.Get == nil || !strings.Contains(p, "{org_id}") || strings.Count(p, "{") > 1 {
			continue
		}
		required := false
		for _, prm := range item.Get.Parameters {
			if prm.In == "query" && prm.Required {
				required = true
			}
		}
		url := strings.ReplaceAll(p, "{org_id}", seed.OrgID)
		r := e.do(http.MethodGet, url, owner, nil)
		checked++
		switch {
		case r.Code >= 500:
			t.Errorf("GET %s: server error %d: %s", p, r.Code, truncate(string(r.Body), 300))
		case r.Code == http.StatusPaymentRequired:
			// fonctionnalité hors plan (ex. MSP pour un plan Enterprise) : refus attendu
		case !required && r.Code != http.StatusOK:
			t.Errorf("GET %s: expected 200, got %d: %s", p, r.Code, truncate(string(r.Body), 300))
		}
	}
	if checked < 40 {
		t.Fatalf("too few endpoints checked: %d", checked)
	}
}

// TestCRUDLifecycles crée, lit, modifie puis supprime chaque type d'objet configurable.
func TestCRUDLifecycles(t *testing.T) {
	e, owner := seededEnv(t)
	base := "/api/v1/orgs/" + seed.OrgID
	create := func(t *testing.T, path string, body any) string {
		t.Helper()
		r := e.do(http.MethodPost, base+path, owner, body)
		if r.Code != http.StatusCreated {
			t.Fatalf("POST %s: %d %s", path, r.Code, truncate(string(r.Body), 400))
		}
		var obj struct {
			ID string `json:"id"`
		}
		r.JSON(t, &obj)
		return obj.ID
	}
	node := create(t, "/allocation/nodes", map[string]any{"kind": "team", "name": "Équipe Test"})
	node2 := create(t, "/allocation/nodes", map[string]any{"kind": "team", "name": "Équipe Test 2"})
	channel := create(t, "/channels", map[string]any{"kind": "webhook", "name": "Hook", "secrets": map[string]string{"webhook_url": "https://hooks.example.com/kairn"}})
	check := create(t, "/uptime/checks", map[string]any{"name": "Boutique", "kind": "http", "target": "https://shop.example.com"})
	now := time.Now().UTC()

	cases := []struct {
		path   string
		body   map[string]any
		update map[string]any
	}{
		{"/budgets", map[string]any{"name": "Budget test", "period": "monthly", "amount": "1500.00", "node_id": node},
			map[string]any{"name": "Budget test 2", "period": "quarterly", "amount": "4000"}},
		{"/alert-rules", map[string]any{"name": "Anomalies", "kind": "anomaly", "channel_ids": []string{channel}},
			map[string]any{"name": "Anomalies critiques", "kind": "anomaly", "channel_ids": []string{channel}, "config": map[string]string{"min_severity": "critical"}}},
		{"/allocation/rules", map[string]any{"node_id": node, "name": "Label équipe", "conditions": []map[string]any{{"field": "label:team", "op": "eq", "value": "test"}}},
			map[string]any{"node_id": node, "name": "Label équipe (maj)", "priority": 5, "conditions": []map[string]any{{"field": "label:team", "op": "in", "values": []string{"test", "qa"}}}}},
		{"/allocation/shared-rules", map[string]any{"name": "Monitoring", "method": "fixed", "source": []map[string]any{{"field": "label:role", "op": "eq", "value": "monitoring"}},
			"targets": []map[string]any{{"node_id": node, "weight": "60"}, {"node_id": node2, "weight": "40"}}},
			map[string]any{"name": "Monitoring (pondéré)", "method": "weighted", "source": []map[string]any{{"field": "label:role", "op": "eq", "value": "monitoring"}},
				"targets": []map[string]any{{"node_id": node, "weight": "2"}, {"node_id": node2, "weight": "1"}}}},
		{"/pricing/adjustments", map[string]any{"kind": "discount", "name": "Remise contrat", "provider": "openstack", "percent": "10", "valid_from": now.Format(time.RFC3339)},
			map[string]any{"kind": "discount", "name": "Remise contrat 2027", "provider": "openstack", "percent": "12", "valid_from": now.Format(time.RFC3339)}},
		{"/pricing/onprem-models", map[string]any{"name": "Baie Paris", "amortization_months": 36, "valid_from": now.Format(time.RFC3339), "hardware_cost": "120000",
			"selector": map[string]string{"site": "paris"}, "capacity_vcpu": "512", "capacity_ram_gb": "4096", "capacity_storage_gb": "100000"},
			map[string]any{"name": "Baie Paris (maj)", "amortization_months": 48, "valid_from": now.Format(time.RFC3339), "hardware_cost": "120000",
				"selector": map[string]string{"site": "paris"}, "capacity_vcpu": "512", "capacity_ram_gb": "4096", "capacity_storage_gb": "100000"}},
		{"/silences", map[string]any{"matchers": map[string]string{"kind": "anomaly"}, "starts_at": now.Format(time.RFC3339), "ends_at": now.Add(time.Hour).Format(time.RFC3339), "reason": "maintenance"},
			map[string]any{"matchers": map[string]string{"kind": "uptime"}, "starts_at": now.Format(time.RFC3339), "ends_at": now.Add(2 * time.Hour).Format(time.RFC3339), "reason": "migration"}},
		{"/status-pages", map[string]any{"slug": "boutique-test", "title": "Statut boutique", "check_ids": []string{check}},
			map[string]any{"slug": "boutique-test", "title": "Statut de la boutique", "check_ids": []string{check}}},
		{"/unit-metrics", map[string]any{"name": "Commandes", "unit_label": "commande", "source": "api", "node_id": node},
			map[string]any{"name": "Commandes payées", "unit_label": "commande", "source": "api"}},
		{"/webhooks", map[string]any{"url": "https://hooks.example.com/events", "events": []string{"anomaly.detected"}, "secret": "0123456789abcdef-signing"},
			map[string]any{"url": "https://hooks.example.com/events2", "events": []string{"anomaly.detected", "report.ready"}}},
		{"/exports", map[string]any{"name": "Export mensuel", "format": "csv", "schedule": "monthly",
			"destination": map[string]string{"endpoint": "s3.gra.io.cloud.ovh.net", "bucket": "finance", "prefix": "kairn/"},
			"secrets":     map[string]string{"access_key": "ak", "secret_key": "sk"}},
			map[string]any{"name": "Export quotidien", "format": "parquet", "schedule": "daily",
				"destination": map[string]string{"endpoint": "s3.gra.io.cloud.ovh.net", "bucket": "finance", "prefix": "kairn/"}}},
		{"/incidents", map[string]any{"title": "Lenteurs boutique"}, map[string]any{"title": "Lenteurs boutique", "status": "resolved"}},
		{"/uptime/checks", map[string]any{"name": "API", "kind": "tcp", "target": "api.example.com:443"},
			map[string]any{"name": "API TLS", "kind": "tcp", "target": "api.example.com:443", "interval_seconds": 120}},
	}
	for _, c := range cases {
		t.Run(strings.Trim(c.path, "/"), func(t *testing.T) {
			id := create(t, c.path, c.body)
			list := e.do(http.MethodGet, base+c.path, owner, nil)
			expect(t, list, http.StatusOK)
			if !strings.Contains(string(list.Body), id) {
				t.Fatalf("list %s does not contain %s", c.path, id)
			}
			expect(t, e.do(http.MethodGet, base+c.path+"/"+id, owner, nil), http.StatusOK)
			up := e.do(http.MethodPut, base+c.path+"/"+id, owner, c.update)
			if up.Code != http.StatusOK {
				t.Fatalf("PUT %s: %d %s", c.path, up.Code, truncate(string(up.Body), 400))
			}
			if strings.Contains(string(up.Body), `"secret_key"`) || strings.Contains(string(up.Body), `"sk"`) {
				t.Fatalf("secrets must never be returned: %s", up.Body)
			}
			expect(t, e.do(http.MethodDelete, base+c.path+"/"+id, owner, nil), http.StatusNoContent)
			expect(t, e.do(http.MethodGet, base+c.path+"/"+id, owner, nil), http.StatusNotFound)
		})
	}
	// Validation : les montants sont des décimaux, jamais des flottants approximés.
	bad := e.do(http.MethodPost, base+"/budgets", owner, map[string]any{"name": "x", "period": "monthly", "amount": "abc"})
	if bad.Code < 400 || bad.Code >= 500 {
		t.Fatalf("invalid amount must be rejected: %d", bad.Code)
	}
}

// TestWorkflowEndpoints couvre les actions métier (hors CRUD).
func TestWorkflowEndpoints(t *testing.T) {
	e, owner := seededEnv(t)
	base := "/api/v1/orgs/" + seed.OrgID
	now := time.Now().UTC()
	get := func(path string, out any) {
		t.Helper()
		r := e.do(http.MethodGet, base+path, owner, nil)
		expect(t, r, http.StatusOK)
		if out != nil {
			r.JSON(t, out)
		}
	}
	// Coûts : lignes, export CSV, recalcul.
	from, to := now.AddDate(0, 0, -7).Format("2006-01-02"), now.AddDate(0, 0, 1).Format("2006-01-02")
	get("/costs/lines?from="+from+"T00:00:00Z&to="+to+"T00:00:00Z&limit=10", nil)
	exp := e.do(http.MethodGet, base+"/costs/export?from="+from+"T00:00:00Z&to="+to+"T00:00:00Z&group_by=provider", owner, nil)
	expect(t, exp, http.StatusOK)
	if !strings.Contains(exp.Hdr.Get("Content-Type"), "csv") || !strings.Contains(string(exp.Body), "openstack") {
		t.Fatalf("csv export: %s %s", exp.Hdr.Get("Content-Type"), truncate(string(exp.Body), 200))
	}
	rc := e.do(http.MethodPost, base+"/costs/recompute", owner, map[string]any{"from": now.AddDate(0, 0, -2).Format(time.RFC3339), "to": now.Format(time.RFC3339)})
	if rc.Code >= 300 {
		t.Fatalf("recompute: %d %s", rc.Code, rc.Body)
	}
	// Recommandations : cycle de vie.
	var recos struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	get("/recommendations?limit=5", &recos)
	if len(recos.Items) == 0 {
		// Le service analytics ne tourne pas en test : recommandation insérée directement.
		ctx := tenancy.WithOrg(context.Background(), seed.OrgID)
		vms, err := e.st.Resources().Current(ctx, store.ResourceFilter{Types: []string{model.TypeInstance}, Limit: 1})
		if err != nil || len(vms) == 0 {
			t.Fatalf("seeded instances: %v", err)
		}
		r := model.Recommendation{Type: model.RecoRightsizeVM, ResourceID: vms[0].ID, Fingerprint: "smoke-test", Title: "Réduire " + vms[0].Name,
			SavingsMonthly: decimal.RequireFromString("42.50"), Currency: "EUR", Risk: "low", Remediation: model.Remediation{Steps: []string{"resize"}}}
		if err := e.st.Recommendations().Upsert(ctx, &r); err != nil {
			t.Fatal(err)
		}
		get("/recommendations?limit=5", &recos)
	}
	id := recos.Items[0].ID
	get("/recommendations/"+id, nil)
	// Cycle de vie : ouverte → reportée → rouverte → écartée → rouverte → acceptée → appliquée (terminal).
	for _, st := range []map[string]any{
		{"status": "postponed", "reason": "fin de trimestre", "postponed_until": now.AddDate(0, 0, 7).Format(time.RFC3339)},
		{"status": "open"},
		{"status": "dismissed", "reason": "non pertinent"},
		{"status": "open"},
		{"status": "accepted"},
		{"status": "applied"},
	} {
		r := e.do(http.MethodPost, base+"/recommendations/"+id+"/status", owner, st)
		if r.Code >= 300 {
			t.Fatalf("status %v: %d %s", st, r.Code, r.Body)
		}
	}
	expect(t, e.do(http.MethodPost, base+"/recommendations/"+id+"/status", owner, map[string]any{"status": "open"}), http.StatusConflict)
	// Simulation « what-if » et prévisions.
	wi := e.do(http.MethodPost, base+"/whatif", owner, map[string]any{"scenarios": []map[string]any{
		{"kind": "add_nodes", "provider": "openstack", "flavor": "b2-15", "count": 2},
		{"kind": "change_flavor", "flavor": "b2-15", "resource_ids": []string{recos.Items[0].ID}},
		{"kind": "migrate", "provider": "openstack", "target_provider": "scaleway"},
	}})
	if wi.Code != http.StatusOK {
		t.Fatalf("whatif: %d %s", wi.Code, wi.Body)
	}
	var sim map[string]any
	_ = json.Unmarshal(wi.Body, &sim)
	if len(sim) == 0 {
		t.Fatalf("empty simulation")
	}
}
