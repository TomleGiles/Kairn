// Commande openapi : exporte la spécification OpenAPI 3.1 de l'API publique
// (générée depuis le code) vers docs/api/openapi.json et openapi.yaml.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	_ "github.com/kairn-io/kairn/connectors/all"
	"github.com/kairn-io/kairn/pkg/config"
	"github.com/kairn-io/kairn/services/api/internal/api"
)

func main() {
	out := "docs/api"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	srv := api.New(api.Deps{Config: config.Config{APIURL: "https://api.kairn.io"}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	doc := srv.API.OpenAPI()
	js, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(filepath.Join(out, "openapi.json"), append(js, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	y, err := doc.YAML()
	if err == nil {
		_ = os.WriteFile(filepath.Join(out, "openapi.yaml"), y, 0o644)
	}
	fmt.Printf("OpenAPI %s : %d chemins → %s\n", doc.Info.Version, len(doc.Paths), out)
}
