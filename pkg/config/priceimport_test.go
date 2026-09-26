package config

import (
	"os"
	"strings"
	"testing"
)

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "") // restauration automatique en fin de test
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
}

func TestPriceImport(t *testing.T) {
	clearEnv(t)
	unsetEnv(t, "KAIRN_PRICE_IMPORT")
	if got := strings.Join(priceImport(), ","); got != "ovh,scaleway,outscale" {
		t.Fatalf("default providers: %q", got)
	}
	t.Setenv("KAIRN_MODE", "demo")
	if c, _ := Load("api"); len(c.PriceImport) != 0 {
		t.Fatalf("the demo stays offline unless asked: %v", c.PriceImport)
	}
	t.Setenv("KAIRN_PRICE_IMPORT", " ovh , scaleway ")
	if c, _ := Load("api"); strings.Join(c.PriceImport, ",") != "ovh,scaleway" {
		t.Fatalf("explicit list: %v", c.PriceImport)
	}
	for _, off := range []string{"none", "OFF", "false", ""} {
		t.Setenv("KAIRN_PRICE_IMPORT", off)
		if got := priceImport(); len(got) != 0 {
			t.Fatalf("%q must disable the import: %v", off, got)
		}
	}
}
