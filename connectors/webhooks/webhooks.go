// Package webhooks regroupe les connecteurs d'événements alimentés par
// webhook (M-01) : déploiements (GitLab, GitHub, Argo CD, Flux) et incidents
// (Alertmanager, PagerDuty, Opsgenie). Ils n'interrogent jamais la source :
// la passerelle d'ingestion reçoit les appels sur /ingest/v1/webhooks/{jeton},
// vérifie la signature puis enregistre les événements, utilisés par la
// corrélation des anomalies (M-07).
package webhooks

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/kairn-io/kairn/pkg/connector"
)

// ErrSignature signale une signature ou un secret invalide.
var ErrSignature = errors.New("webhooks: invalid signature")

// source décrit un type de webhook.
type source struct {
	typ, name, provider, docs string
	secretHelp                string
	verify                    func(h map[string]string, body []byte, secret string) error
	parse                     func(h map[string]string, body []byte) ([]connector.Event, error)
}

// conn est l'instance d'un connecteur webhook (poussée uniquement).
type conn struct {
	src source
}

func (c *conn) Type() string                                     { return c.src.typ }
func (c *conn) RequiredPermissions() []connector.Permission      { return nil }
func (c *conn) Validate(context.Context, connector.Config) error { return nil }
func (c *conn) PushOnly() bool                                   { return true }
func (c *conn) Health(context.Context) connector.HealthStatus {
	return connector.HealthStatus{Status: connector.HealthOK, CheckedAt: time.Now().UTC()}
}
func (c *conn) SyncInventory(context.Context, time.Time) (<-chan connector.Resource, error) {
	return nil, connector.ErrNotSupported
}
func (c *conn) SyncMetrics(context.Context, connector.TimeWindow) (<-chan connector.MetricPoint, error) {
	return nil, connector.ErrNotSupported
}
func (c *conn) SyncBilling(context.Context, connector.Period) (<-chan connector.CostLine, error) {
	return nil, connector.ErrNotSupported
}

// VerifyWebhook refuse toute requête si aucun secret n'est configuré.
func (c *conn) VerifyWebhook(h map[string]string, body []byte, secret string) error {
	if secret == "" {
		return ErrSignature
	}
	return c.src.verify(h, body, secret)
}

func (c *conn) ParseWebhook(h map[string]string, body []byte) ([]connector.Event, error) {
	return c.src.parse(h, body)
}

func register(src source) {
	connector.Register(connector.TypeInfo{
		Type: src.typ, DisplayName: src.name, Category: connector.CategoryEvents, Provider: src.provider,
		Resources: []string{}, DefaultInterval: 24 * time.Hour, Webhook: true, DocsURL: src.docs,
		Permissions: []connector.Permission{{Scope: "webhook sortant", Description: "Aucun accès à la source : elle appelle l'URL fournie par Kairn."}},
		Fields: []connector.Field{
			{Name: "webhook_secret", Label: "Secret du webhook", Secret: true, Required: true, Help: src.secretHelp},
			{Name: "target_connector_id", Label: "Connecteur des ressources", Help: "Connecteur Kubernetes ou cloud auquel rattacher les événements"},
		},
	}, func(connector.Config) (connector.Connector, error) { return &conn{src: src}, nil })
}

// ---------------------------------------------------------------- vérifications

// equalSecret compare en temps constant.
func equalSecret(got, want string) bool {
	return got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func hmacHex(secret string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

// verifyHMAC contrôle une signature « sha256=<hex> » (ou <hex>) dans l'en-tête donné.
func verifyHMAC(header string) func(h map[string]string, body []byte, secret string) error {
	return func(h map[string]string, body []byte, secret string) error {
		sig := strings.TrimPrefix(strings.TrimSpace(h[header]), "sha256=")
		if !equalSecret(strings.ToLower(sig), hmacHex(secret, body)) {
			return ErrSignature
		}
		return nil
	}
}

// verifyToken contrôle un secret partagé transmis dans un en-tête.
func verifyToken(header string) func(h map[string]string, body []byte, secret string) error {
	return func(h map[string]string, _ []byte, secret string) error {
		if !equalSecret(strings.TrimSpace(h[header]), secret) {
			return ErrSignature
		}
		return nil
	}
}

// verifyBearer contrôle « Authorization: Bearer <secret> ».
func verifyBearer(h map[string]string, _ []byte, secret string) error {
	v := strings.TrimSpace(h["authorization"])
	if !strings.HasPrefix(strings.ToLower(v), "bearer ") || !equalSecret(strings.TrimSpace(v[7:]), secret) {
		return ErrSignature
	}
	return nil
}

func parseTS(s string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05 MST", "2006-01-02 15:04:05 -0700", "2006-01-02T15:04:05Z0700"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Now().UTC()
}

func shortSHA(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}
