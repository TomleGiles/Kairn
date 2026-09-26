package main

import (
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/kairn-io/kairn/pkg/config"
	"github.com/kairn-io/kairn/pkg/logging"
	"github.com/kairn-io/kairn/pkg/platform"
	"github.com/kairn-io/kairn/pkg/pricing/catalogs"
)

// runImportPrices importe immédiatement les grilles publiques (M-03) :
//
//	kairn-api import-prices                 fournisseurs de KAIRN_PRICE_IMPORT
//	kairn-api import-prices --providers ovh fournisseurs choisis
//
// L'ordonnanceur fait de même chaque jour ; cette commande sert à
// l'installation initiale et au diagnostic. Le résultat est écrit en JSON.
func runImportPrices(args []string) error {
	fs := flag.NewFlagSet("import-prices", flag.ContinueOnError)
	providers := fs.String("providers", "", "fournisseurs séparés par des virgules (défaut : KAIRN_PRICE_IMPORT)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load("api")
	if err != nil {
		return err
	}
	log := logging.New("import-prices", cfg.LogLevel, cfg.LogFormat)
	want := cfg.PriceImport
	if *providers != "" {
		want = splitList(*providers)
	}
	if len(want) == 0 {
		want = config.DefaultPriceImport
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	be, err := platform.Open(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer be.Close()
	res, importErr := catalogs.ImportAll(ctx, be.Store, &http.Client{Timeout: 5 * time.Minute}, time.Now(), want, log)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		return err
	}
	return importErr
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
