package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const orgID = "0190f5a0-0000-7000-8000-00000000a001"

func fakeAPI(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer kairn_test" {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"title": "Unauthorized", "status": 401, "detail": "invalid or expired credentials"})
			return
		}
		write := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		base := "/api/v1/orgs/" + orgID
		switch r.URL.Path {
		case "/api/v1/me":
			write(map[string]any{"user": map[string]any{"email": "ci@acme.example"}, "memberships": []any{},
				"permissions": map[string]any{orgID: []string{"costs:read"}}})
		case base + "/costs":
			if got := r.URL.Query()["group_by"]; len(got) != 2 || got[1] != "cost_type" {
				t.Errorf("group_by: %v", got)
			}
			write(map[string]any{"currency": "EUR", "total": "2479.64", "group_by": []string{"provider", "cost_type"}, "rows": []any{
				map[string]any{"period": "0001-01-01T00:00:00Z", "keys": map[string]string{"provider": "openstack", "cost_type": "compute"}, "amount": "1756.38"},
			}})
		case base + "/recommendations/r1/status":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if r.Method != http.MethodPost || body["status"] != "accepted" {
				t.Errorf("status update: %v", body)
			}
			w.WriteHeader(http.StatusNoContent)
		case base + "/costs/export":
			w.Header().Set("Content-Type", "text/csv")
			_, _ = w.Write([]byte("day,amount\n2026-09-01,12.50\n"))
		case base + "/assistant/chat":
			w.Header().Set("Content-Type", "text/event-stream")
			for _, ev := range []string{`{"type":"text","text":"Dépense : "}`, `{"type":"text","text":"2 479,64 €"}`, `{"type":"sources","sources":["#1 get_cost_summary"]}`, `{"type":"done"}`} {
				_, _ = w.Write([]byte("data: " + ev + "\n\n"))
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func setup(t *testing.T) *bytes.Buffer {
	t.Helper()
	t.Setenv("KAIRN_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("KAIRN_URL", "")
	t.Setenv("KAIRN_TOKEN", "")
	t.Setenv("KAIRN_ORG", "")
	buf := &bytes.Buffer{}
	out = buf
	t.Cleanup(func() { out = os.Stdout })
	return buf
}

func TestLoginAndCommands(t *testing.T) {
	buf := setup(t)
	srv := fakeAPI(t)
	ctx := context.Background()
	// Seul https est accepté, sauf localhost.
	if err := run(ctx, []string{"login", "--url", "http://example.com", "--token", "kairn_test"}); err == nil {
		t.Fatal("plain http accepted")
	}
	url := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	if err := run(ctx, []string{"login", "--url", url, "--token", "kairn_test"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), orgID) {
		t.Fatalf("single-org token must select the organization: %s", buf.String())
	}
	info, err := os.Stat(os.Getenv("KAIRN_CONFIG"))
	if err != nil || (info.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/') {
		t.Fatalf("config file must be private: %v %v", info, err)
	}
	buf.Reset()
	if err := run(ctx, []string{"costs", "--group-by", "provider,cost_type"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "1756.38") || !strings.Contains(buf.String(), "Total : 2479.64 EUR") {
		t.Fatalf("costs table: %s", buf.String())
	}
	buf.Reset()
	if err := run(ctx, []string{"costs", "--group-by", "provider,cost_type", "-o", "csv"}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(buf.String(), "PROVIDER,COST_TYPE,MONTANT (EUR)\nopenstack,compute,1756.38") {
		t.Fatalf("csv: %q", buf.String())
	}
	if err := run(ctx, []string{"recommendations", "accept", "r1"}); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if err := run(ctx, []string{"export", "costs"}); err != nil || buf.String() != "day,amount\n2026-09-01,12.50\n" {
		t.Fatalf("export: %v %q", err, buf.String())
	}
	buf.Reset()
	if err := run(ctx, []string{"ask", "Combien", "ce", "mois-ci", "?"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Dépense : 2 479,64 €") || !strings.Contains(buf.String(), "#1 get_cost_summary") {
		t.Fatalf("ask: %s", buf.String())
	}
}

func TestErrorsAreReadable(t *testing.T) {
	setup(t)
	srv := fakeAPI(t)
	t.Setenv("KAIRN_URL", srv.URL)
	t.Setenv("KAIRN_TOKEN", "kairn_wrong")
	t.Setenv("KAIRN_ORG", orgID)
	err := run(context.Background(), []string{"summary"})
	if err == nil || !strings.Contains(err.Error(), "invalid or expired credentials") {
		t.Fatalf("problem+json detail expected: %v", err)
	}
	t.Setenv("KAIRN_TOKEN", "")
	if err := run(context.Background(), []string{"summary"}); err == nil || !strings.Contains(err.Error(), "kairn login") {
		t.Fatalf("login hint expected: %v", err)
	}
	if err := run(context.Background(), []string{"nope"}); err == nil {
		t.Fatal("unknown command accepted")
	}
}
