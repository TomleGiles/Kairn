package config

import (
	"strings"
	"testing"
	"time"
)

func clearEnv(t *testing.T) {
	for _, k := range []string{"KAIRN_MODE", "KAIRN_POSTGRES_URL", "KAIRN_CLICKHOUSE_ADDR", "KAIRN_SESSION_SECRET", "KAIRN_SERVICE_TOKEN", "KAIRN_KEK",
		"KAIRN_VAULT_ADDR", "KAIRN_NATS_URL", "KAIRN_DEV_LOGIN", "KAIRN_SESSION_TTL", "KAIRN_CORS_ORIGINS", "KAIRN_STRIPE_PRICES", "KAIRN_S3_SSL"} {
		t.Setenv(k, "")
	}
}

func TestDemoDefaults(t *testing.T) {
	clearEnv(t)
	t.Setenv("KAIRN_MODE", "demo")
	c, err := Load("api")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Demo() || !c.DevLogin || c.ServiceToken == "" || len(c.SessionSecret) < 32 || c.LogFormat != "text" {
		t.Fatalf("demo defaults: %+v", c)
	}
}

func TestProductionRequiresSecrets(t *testing.T) {
	clearEnv(t)
	t.Setenv("KAIRN_MODE", "production")
	_, err := Load("api")
	if err == nil || !strings.Contains(err.Error(), "KAIRN_POSTGRES_URL") || !strings.Contains(err.Error(), "KAIRN_KEK ou KAIRN_VAULT_ADDR") {
		t.Fatalf("missing variables must be listed: %v", err)
	}
	t.Setenv("KAIRN_POSTGRES_URL", "postgres://x")
	t.Setenv("KAIRN_CLICKHOUSE_ADDR", "http://ch:8123")
	t.Setenv("KAIRN_SERVICE_TOKEN", "svc")
	t.Setenv("KAIRN_KEK", "a2V5")
	t.Setenv("KAIRN_SESSION_SECRET", "too-short")
	if _, err := Load("api"); err == nil || !strings.Contains(err.Error(), "32 characters") {
		t.Fatalf("short session secret: %v", err)
	}
	t.Setenv("KAIRN_SESSION_SECRET", strings.Repeat("s", 40))
	t.Setenv("KAIRN_SESSION_TTL", "2h")
	t.Setenv("KAIRN_CORS_ORIGINS", "https://a.example, https://b.example")
	t.Setenv("KAIRN_STRIPE_PRICES", "team=price_1,starter=price_2")
	t.Setenv("KAIRN_S3_SSL", "false")
	c, err := Load("api")
	if err != nil {
		t.Fatal(err)
	}
	if c.Demo() || c.DevLogin || c.SessionTTL != 2*time.Hour || len(c.CORSOrigins) != 2 || c.StripePrices["team"] != "price_1" || c.S3UseSSL {
		t.Fatalf("parsed config: %+v", c)
	}
	// Les workers distribués exigent NATS ; la sonde n'exige rien.
	if _, err := Load("ingest"); err == nil || !strings.Contains(err.Error(), "KAIRN_NATS_URL") {
		t.Fatalf("ingest requires NATS: %v", err)
	}
	if _, err := Load("probe"); err != nil {
		t.Fatalf("probe: %v", err)
	}
}
