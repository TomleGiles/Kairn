package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// La documentation des connecteurs versionnée doit correspondre au code :
// sinon, lancer `make gen`.
func TestConnectorDocsUpToDate(t *testing.T) {
	dir := t.TempDir()
	n, err := generate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if n < 9 {
		t.Fatalf("expected a page per documented connector family, got %d", n)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		want, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "docs", "connectors", e.Name()))
		if err != nil || strings.ReplaceAll(string(got), "\r\n", "\n") != string(want) {
			t.Errorf("docs/connectors/%s is stale: run `make gen`", e.Name())
		}
	}
	page, _ := os.ReadFile(filepath.Join(dir, "webhooks.md"))
	for _, anchor := range []string{`id="gitlab"`, `id="argo-cd"`, "`opsgenie`", "Permissions minimales"} {
		if !strings.Contains(string(page), anchor) {
			t.Errorf("webhooks page misses %s", anchor)
		}
	}
	if strings.Contains(string(page), "<!-- type:") {
		t.Error("every type marker must be replaced by its card")
	}
}

func TestInterval(t *testing.T) {
	for in, want := range map[string]string{"15m": "15 min", "1h": "1 h", "6h": "6 h", "24h": "1 j", "90m": "90 min"} {
		d, _ := time.ParseDuration(in)
		if got := interval(d); got != want {
			t.Errorf("%s: got %q, want %q", in, got, want)
		}
	}
}
