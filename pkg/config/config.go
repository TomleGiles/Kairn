// Package config charge la configuration des services depuis l'environnement
// (préfixe KAIRN_). Aucune valeur secrète n'a de défaut hors mode démo.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config regroupe les paramètres communs aux services Go.
type Config struct {
	// Mode : "demo" (tout en mémoire, un seul processus) ou "production".
	Mode        string
	HTTPAddr    string
	PublicURL   string // URL publique de l'application web (liens, redirections)
	APIURL      string // URL publique de l'API
	LogLevel    string
	LogFormat   string // json | text
	PostgresURL string
	// ClickHouse
	ClickHouseAddr     string
	ClickHouseDB       string
	ClickHouseUser     string
	ClickHousePassword string
	NATSURL            string
	RedisURL           string
	// Stockage objet S3-compatible
	S3Endpoint     string
	S3Bucket       string
	S3AccessKey    string
	S3SecretKey    string
	S3Region       string
	S3UseSSL       bool
	LocalObjectDir string
	// Sécurité
	SessionSecret string
	SessionTTL    time.Duration
	KEK           string // clé maîtresse locale (base64, 32 octets)
	VaultAddr     string
	VaultToken    string
	VaultKey      string
	ServiceToken  string // jeton partagé des services internes (analytics, ai)
	// OIDC (Keycloak ou IdP externe)
	OIDCIssuer       string
	OIDCClientID     string
	OIDCClientSecret string
	DevLogin         bool // connexion par e-mail sans mot de passe (démo/développement uniquement)
	// Services
	AIServiceURL        string
	AnalyticsServiceURL string
	// Facturation SaaS
	StripeSecretKey     string
	StripeWebhookSecret string
	StripePrices        map[string]string // plan → price id
	// Notifications
	SMTPAddr string
	SMTPFrom string
	SMTPUser string
	SMTPPass string
	// Observabilité
	OTLPEndpoint string
	ServiceName  string
	Region       string // région de sonde uptime
	CORSOrigins  []string
	// TrustedProxies : réseaux (CIDR) des proxys dont X-Forwarded-For est accepté.
	TrustedProxies []string
	DemoSeed       bool
	// PriceImport : fournisseurs dont la grille publique est importée chaque
	// jour (M-03). « none » désactive l'import (installation sans accès sortant).
	PriceImport []string
}

// Load lit la configuration depuis l'environnement.
func Load(service string) (Config, error) {
	c := Config{
		Mode:                str("KAIRN_MODE", "production"),
		HTTPAddr:            str("KAIRN_HTTP_ADDR", ":8080"),
		PublicURL:           str("KAIRN_PUBLIC_URL", "http://localhost:3000"),
		APIURL:              str("KAIRN_API_URL", "http://localhost:8080"),
		LogLevel:            str("KAIRN_LOG_LEVEL", "info"),
		LogFormat:           str("KAIRN_LOG_FORMAT", "json"),
		PostgresURL:         str("KAIRN_POSTGRES_URL", ""),
		ClickHouseAddr:      str("KAIRN_CLICKHOUSE_ADDR", ""),
		ClickHouseDB:        str("KAIRN_CLICKHOUSE_DB", "kairn"),
		ClickHouseUser:      str("KAIRN_CLICKHOUSE_USER", "default"),
		ClickHousePassword:  str("KAIRN_CLICKHOUSE_PASSWORD", ""),
		NATSURL:             str("KAIRN_NATS_URL", ""),
		RedisURL:            str("KAIRN_REDIS_URL", ""),
		S3Endpoint:          str("KAIRN_S3_ENDPOINT", ""),
		S3Bucket:            str("KAIRN_S3_BUCKET", "kairn"),
		S3AccessKey:         str("KAIRN_S3_ACCESS_KEY", ""),
		S3SecretKey:         str("KAIRN_S3_SECRET_KEY", ""),
		S3Region:            str("KAIRN_S3_REGION", "gra"),
		S3UseSSL:            boolean("KAIRN_S3_SSL", true),
		LocalObjectDir:      str("KAIRN_LOCAL_OBJECT_DIR", ".data/objects"),
		SessionSecret:       str("KAIRN_SESSION_SECRET", ""),
		SessionTTL:          dur("KAIRN_SESSION_TTL", 12*time.Hour),
		KEK:                 str("KAIRN_KEK", ""),
		VaultAddr:           str("KAIRN_VAULT_ADDR", ""),
		VaultToken:          str("KAIRN_VAULT_TOKEN", ""),
		VaultKey:            str("KAIRN_VAULT_TRANSIT_KEY", "kairn"),
		ServiceToken:        str("KAIRN_SERVICE_TOKEN", ""),
		OIDCIssuer:          str("KAIRN_OIDC_ISSUER", ""),
		OIDCClientID:        str("KAIRN_OIDC_CLIENT_ID", "kairn"),
		OIDCClientSecret:    str("KAIRN_OIDC_CLIENT_SECRET", ""),
		DevLogin:            boolean("KAIRN_DEV_LOGIN", false),
		AIServiceURL:        str("KAIRN_AI_URL", ""),
		AnalyticsServiceURL: str("KAIRN_ANALYTICS_URL", ""),
		StripeSecretKey:     str("KAIRN_STRIPE_SECRET_KEY", ""),
		StripeWebhookSecret: str("KAIRN_STRIPE_WEBHOOK_SECRET", ""),
		StripePrices:        kv("KAIRN_STRIPE_PRICES"),
		SMTPAddr:            str("KAIRN_SMTP_ADDR", ""),
		SMTPFrom:            str("KAIRN_SMTP_FROM", "Kairn <no-reply@kairn.local>"),
		SMTPUser:            str("KAIRN_SMTP_USER", ""),
		SMTPPass:            str("KAIRN_SMTP_PASSWORD", ""),
		OTLPEndpoint:        str("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		ServiceName:         str("OTEL_SERVICE_NAME", "kairn-"+service),
		Region:              str("KAIRN_REGION", "eu-west-gra"),
		CORSOrigins:         list("KAIRN_CORS_ORIGINS"),
		TrustedProxies:      list("KAIRN_TRUSTED_PROXIES"),
		DemoSeed:            boolean("KAIRN_DEMO_SEED", true),
		PriceImport:         priceImport(),
	}
	if c.Mode == "demo" {
		// Valeurs de confort pour la démo locale : jamais utilisées en production.
		if _, set := os.LookupEnv("KAIRN_PRICE_IMPORT"); !set {
			c.PriceImport = nil // la démo reste hors ligne
		}
		if c.SessionSecret == "" {
			c.SessionSecret = "demo-only-session-secret-change-me-0123456789"
		}
		if c.ServiceToken == "" {
			c.ServiceToken = "demo-service-token"
		}
		c.DevLogin = true
		c.LogFormat = str("KAIRN_LOG_FORMAT", "text")
		return c, nil
	}
	return c, c.validate(service)
}

func (c Config) validate(service string) error {
	var missing []string
	need := func(name, v string) {
		if v == "" {
			missing = append(missing, name)
		}
	}
	switch service {
	case "api":
		need("KAIRN_POSTGRES_URL", c.PostgresURL)
		need("KAIRN_CLICKHOUSE_ADDR", c.ClickHouseAddr)
		need("KAIRN_SESSION_SECRET", c.SessionSecret)
		need("KAIRN_SERVICE_TOKEN", c.ServiceToken)
		if c.KEK == "" && c.VaultAddr == "" {
			missing = append(missing, "KAIRN_KEK ou KAIRN_VAULT_ADDR")
		}
		if len(c.SessionSecret) > 0 && len(c.SessionSecret) < 32 {
			return fmt.Errorf("config: KAIRN_SESSION_SECRET must be at least 32 characters")
		}
	case "ingest", "cost-engine", "notifier":
		need("KAIRN_POSTGRES_URL", c.PostgresURL)
		need("KAIRN_CLICKHOUSE_ADDR", c.ClickHouseAddr)
		need("KAIRN_NATS_URL", c.NATSURL)
		if service != "cost-engine" && c.KEK == "" && c.VaultAddr == "" {
			missing = append(missing, "KAIRN_KEK ou KAIRN_VAULT_ADDR")
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("config: missing %s", strings.Join(missing, ", "))
	}
	return nil
}

// Demo indique le mode démo.
func (c Config) Demo() bool { return c.Mode == "demo" }

func str(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return strings.TrimSpace(v)
	}
	return def
}

func boolean(key string, def bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def
	}
	b, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		return def
	}
	return b
}

func dur(key string, def time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def
	}
	d, err := time.ParseDuration(strings.TrimSpace(v))
	if err != nil {
		return def
	}
	return d
}

func list(key string) []string {
	v := str(key, "")
	if v == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// DefaultPriceImport liste les fournisseurs importés par défaut.
var DefaultPriceImport = []string{"ovh", "scaleway", "outscale"}

func priceImport() []string {
	v := list("KAIRN_PRICE_IMPORT")
	if _, set := os.LookupEnv("KAIRN_PRICE_IMPORT"); !set {
		return append([]string(nil), DefaultPriceImport...)
	}
	if len(v) == 1 && (strings.EqualFold(v[0], "none") || strings.EqualFold(v[0], "off") || strings.EqualFold(v[0], "false")) {
		return nil
	}
	return v
}

func kv(key string) map[string]string {
	out := map[string]string{}
	for _, p := range list(key) {
		if k, v, ok := strings.Cut(p, "="); ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}
