package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kairn-io/kairn/pkg/config"
	"github.com/kairn-io/kairn/services/api/internal/api"
)

// Les noms de composants OpenAPI doivent respecter ^[a-zA-Z0-9._-]+$
// (sinon les générateurs de clients échouent), et docs/api doit être à jour.
func TestOpenAPISpec(t *testing.T) {
	srv := api.New(api.Deps{Config: config.Config{APIURL: "https://api.kairn.io"}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	doc := srv.API.OpenAPI()
	valid := regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	for name := range doc.Components.Schemas.Map() {
		if !valid.MatchString(name) {
			t.Errorf("invalid schema name %q: give the request body a named type", name)
		}
	}
	js, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "docs", "api", "openapi.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.ReplaceAll(string(committed), "\r\n", "\n") != string(js)+"\n" {
		t.Error("docs/api/openapi.json is stale: run `make gen`")
	}
}
