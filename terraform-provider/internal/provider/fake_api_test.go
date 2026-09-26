package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
)

const testOrg = "0192f0c0-0000-7000-8000-000000000001"

// fakeAPI simule les routes de l'API Kairn utilisées par le provider.
type fakeAPI struct {
	mu         sync.Mutex
	seq        int
	org        map[string]any
	connectors map[string]map[string]any
	secrets    map[string]map[string]string
	nodes      map[string]map[string]any
	rules      map[string]map[string]any
	budgets    map[string]map[string]any
	calls      []string
}

func newFakeAPI(t *testing.T) (*fakeAPI, *httptest.Server) {
	t.Helper()
	f := &fakeAPI{
		org: map[string]any{"id": testOrg, "name": "Acme", "slug": "acme", "plan": "team", "currency": "EUR", "locale": "fr",
			"timezone": "Europe/Paris", "vat_rate": "0"},
		connectors: map[string]map[string]any{}, secrets: map[string]map[string]string{},
		nodes: map[string]map[string]any{}, rules: map[string]map[string]any{}, budgets: map[string]map[string]any{},
	}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeAPI) id(prefix string) string {
	f.seq++
	return fmt.Sprintf("%s-%d", prefix, f.seq)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func problem(w http.ResponseWriter, code int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": code, "title": http.StatusText(code), "detail": detail})
}

// allowed liste les champs acceptés par chaque corps de requête (comme l'API, qui refuse les autres).
var allowed = map[string][]string{
	"org":       {"name", "currency", "locale", "timezone", "vat_rate", "settings"},
	"connector": {"type", "name", "settings", "secrets", "interval_seconds", "enabled", "backfill_days"},
	"patch":     {"name", "settings", "secrets", "interval_seconds", "enabled"},
	"node":      {"kind", "name", "parent_id"},
	"rule":      {"node_id", "name", "priority", "enabled", "conditions"},
	"budget":    {"node_id", "name", "period", "amount", "currency", "thresholds", "forecast_alert", "channel_ids"},
}

func unknownField(kind string, body map[string]any) string {
	for k := range body {
		ok := false
		for _, a := range allowed[kind] {
			ok = ok || a == k
		}
		if !ok {
			return k
		}
	}
	return ""
}

func (f *fakeAPI) serve(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer kairn_test" {
		problem(w, http.StatusUnauthorized, "bad token")
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	var body map[string]any
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	p := strings.TrimPrefix(r.URL.Path, "/api/v1")
	if p == "/orgs" && r.Method == http.MethodGet {
		writeJSON(w, 200, []any{f.org})
		return
	}
	prefix := "/orgs/" + testOrg
	if !strings.HasPrefix(p, prefix) {
		problem(w, 404, "organization not found")
		return
	}
	rest := strings.TrimPrefix(p, prefix)
	seg := strings.Split(strings.Trim(rest, "/"), "/")
	if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
		kind := map[string]string{"": "org", "connectors": "connector", "allocation": "node", "budgets": "budget"}[seg[0]]
		if seg[0] == "connectors" && r.Method == http.MethodPatch {
			kind = "patch"
		}
		if seg[0] == "allocation" && len(seg) > 1 && seg[1] == "rules" {
			kind = "rule"
		}
		if k := unknownField(kind, body); k != "" {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": 422, "title": "Unprocessable Entity", "detail": "validation failed",
				"errors": []any{map[string]any{"message": "unexpected property", "location": "body." + k}}})
			return
		}
	}
	switch {
	case rest == "":
		if r.Method == http.MethodPatch {
			for k, v := range body {
				f.org[k] = v
			}
		}
		writeJSON(w, 200, f.org)
	case seg[0] == "connectors":
		f.connector(w, r, seg, body)
	case seg[0] == "allocation" && len(seg) >= 2 && seg[1] == "nodes":
		f.crud(w, r, f.nodes, "node", seg[2:], body, func(m map[string]any) {
			m["path"] = m["name"]
			if pid, _ := m["parent_id"].(string); pid != "" {
				if parent, ok := f.nodes[pid]; ok {
					m["path"] = fmt.Sprint(parent["path"], "/", m["name"])
				}
			}
		})
	case seg[0] == "allocation" && len(seg) >= 2 && seg[1] == "rules":
		f.crud(w, r, f.rules, "rule", seg[2:], body, nil)
	case seg[0] == "budgets":
		f.crud(w, r, f.budgets, "budget", seg[1:], body, func(m map[string]any) {
			if m["currency"] == nil || m["currency"] == "" {
				m["currency"] = f.org["currency"]
			}
			if m["thresholds"] == nil {
				m["thresholds"] = []any{50, 80, 100}
			}
			if m["forecast_alert"] == nil {
				m["forecast_alert"] = true
			}
		})
	default:
		problem(w, 404, "not found")
	}
}

// crud gère une collection simple (POST, GET, PUT, DELETE) ; fill applique les valeurs par défaut.
func (f *fakeAPI) crud(w http.ResponseWriter, r *http.Request, coll map[string]map[string]any, prefix string, seg []string, body map[string]any, fill func(map[string]any)) {
	if len(seg) == 0 || seg[0] == "" {
		if r.Method != http.MethodPost {
			problem(w, 405, "method")
			return
		}
		body["id"] = f.id(prefix)
		if fill != nil {
			fill(body)
		}
		coll[body["id"].(string)] = body
		writeJSON(w, 201, body)
		return
	}
	cur, ok := coll[seg[0]]
	if !ok {
		problem(w, 404, prefix+" not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, cur)
	case http.MethodPut:
		body["id"] = seg[0]
		if fill != nil {
			fill(body)
		}
		coll[seg[0]] = body
		writeJSON(w, 200, body)
	case http.MethodDelete:
		delete(coll, seg[0])
		w.WriteHeader(http.StatusNoContent)
	default:
		problem(w, 405, "method")
	}
}

func (f *fakeAPI) view(id string) map[string]any {
	c := f.connectors[id]
	keys := make([]string, 0, len(f.secrets[id]))
	for k := range f.secrets[id] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := map[string]any{}
	for k, v := range c {
		out[k] = v
	}
	out["secret_keys"] = keys
	return out
}

func toStringMap(v any) map[string]string {
	out := map[string]string{}
	if m, ok := v.(map[string]any); ok {
		for k, x := range m {
			out[k] = fmt.Sprint(x)
		}
	}
	return out
}

func (f *fakeAPI) connector(w http.ResponseWriter, r *http.Request, seg []string, body map[string]any) {
	if len(seg) == 1 {
		if r.Method != http.MethodPost {
			problem(w, 405, "method")
			return
		}
		id := f.id("conn")
		settings := toStringMap(body["settings"])
		settings["server_default"] = "added-by-kairn" // le serveur peut compléter les paramètres
		interval := body["interval_seconds"]
		if interval == nil {
			interval = 3600
		}
		enabled := true
		if e, ok := body["enabled"].(bool); ok {
			enabled = e
		}
		c := map[string]any{"id": id, "type": body["type"], "name": body["name"], "enabled": enabled, "interval_seconds": interval,
			"settings": settings, "status": "pending"}
		if body["type"] == "gitlab" {
			c["webhook_url"] = "https://kairn.example/ingest/v1/webhooks/secret-token"
		}
		f.connectors[id] = c
		f.secrets[id] = toStringMap(body["secrets"])
		writeJSON(w, 201, f.view(id))
		return
	}
	id := seg[1]
	c, ok := f.connectors[id]
	if !ok {
		problem(w, 404, "connector not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, f.view(id))
	case http.MethodPatch:
		for _, k := range []string{"name", "enabled", "interval_seconds"} {
			if v, ok := body[k]; ok {
				c[k] = v
			}
		}
		settings := c["settings"].(map[string]string)
		for k, v := range toStringMap(body["settings"]) {
			if v == "" {
				delete(settings, k)
			} else {
				settings[k] = v
			}
		}
		for k, v := range toStringMap(body["secrets"]) {
			if v == "" {
				delete(f.secrets[id], k)
			} else {
				f.secrets[id][k] = v
			}
		}
		writeJSON(w, 200, f.view(id))
	case http.MethodDelete:
		delete(f.connectors, id)
		w.WriteHeader(http.StatusNoContent)
	default:
		problem(w, 405, "method")
	}
}
