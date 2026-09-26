// Package connector définit le contrat des connecteurs (CLAUDE.md §6).
// Ajouter un fournisseur = créer un package sous connectors/ qui implémente
// Connector et s'enregistre via Register, sans toucher au cœur.
//
// Les connecteurs sont strictement en lecture seule.
package connector

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// Config est la configuration d'une instance de connecteur.
type Config struct {
	OrgID       string
	ConnectorID string
	// Settings contient les paramètres non secrets (URL, région, projet…).
	Settings map[string]string
	// Secrets contient les éléments déchiffrés (mots de passe, jetons). Ne jamais journaliser.
	Secrets map[string]string
}

// Setting renvoie un paramètre ou la valeur par défaut.
func (c Config) Setting(key, def string) string {
	if v, ok := c.Settings[key]; ok && v != "" {
		return v
	}
	return def
}

// Secret renvoie un secret ou "".
func (c Config) Secret(key string) string { return c.Secrets[key] }

// Require vérifie la présence de paramètres ou secrets obligatoires.
func (c Config) Require(keys ...string) error {
	var missing []string
	for _, k := range keys {
		if c.Settings[k] == "" && c.Secrets[k] == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: %v", ErrMissingConfig, missing)
	}
	return nil
}

// ErrMissingConfig signale un paramètre obligatoire absent.
var ErrMissingConfig = errors.New("connector: missing configuration")

// ErrNotSupported est renvoyée par une méthode optionnelle non implémentée (ex. SyncBilling).
var ErrNotSupported = errors.New("connector: not supported")

// ErrPermission signale un manque de permission côté fournisseur.
var ErrPermission = errors.New("connector: insufficient permissions")

// Permission est une permission minimale requise, documentée dans l'UI.
type Permission struct {
	Scope       string `json:"scope"`
	Description string `json:"description"`
	Optional    bool   `json:"optional"`
}

// Edge relie une ressource à un parent, identifié par son type et son identifiant externe.
type Edge struct {
	Relation         string `json:"relation"`
	ParentType       string `json:"parent_type"`
	ParentExternalID string `json:"parent_external_id"`
}

// Resource est une ressource d'inventaire telle qu'observée chez le fournisseur.
type Resource struct {
	Type       string            `json:"type"`
	ExternalID string            `json:"external_id"`
	Name       string            `json:"name"`
	Region     string            `json:"region"`
	Attributes map[string]any    `json:"attributes"`
	Labels     map[string]string `json:"labels"`
	Parents    []Edge            `json:"parents,omitempty"`
	// CreatedAt est la date de création chez le fournisseur, si connue (backfill).
	CreatedAt *time.Time `json:"created_at,omitempty"`
}

// MetricPoint est un point de métrique rattaché à une ressource externe.
type MetricPoint struct {
	ResourceType       string    `json:"resource_type"`
	ResourceExternalID string    `json:"resource_external_id"`
	Metric             string    `json:"metric"`
	TS                 time.Time `json:"ts"`
	Value              float64   `json:"value"`
}

// CostLine est une ligne de facture réelle remontée par le fournisseur.
type CostLine struct {
	ResourceType       string          `json:"resource_type,omitempty"`
	ResourceExternalID string          `json:"resource_external_id,omitempty"`
	Day                time.Time       `json:"day"`
	Service            string          `json:"service"`
	SKU                string          `json:"sku"`
	CostType           string          `json:"cost_type"`
	Quantity           decimal.Decimal `json:"quantity"`
	Unit               string          `json:"unit"`
	Amount             decimal.Decimal `json:"amount"`
	Currency           string          `json:"currency"`
	InvoiceID          string          `json:"invoice_id,omitempty"`
}

// Event est un événement (déploiement, incident, HPA) remonté par une source.
type Event struct {
	Kind               string         `json:"kind"`
	TS                 time.Time      `json:"ts"`
	Title              string         `json:"title"`
	ResourceType       string         `json:"resource_type,omitempty"`
	ResourceExternalID string         `json:"resource_external_id,omitempty"`
	Payload            map[string]any `json:"payload,omitempty"`
}

// TimeWindow est une fenêtre de collecte de métriques.
type TimeWindow struct {
	From time.Time
	To   time.Time
	Step time.Duration
}

// Period est une période de facturation [From, To).
type Period struct {
	From time.Time
	To   time.Time
}

// États de santé.
const (
	HealthOK       = "ok"
	HealthDegraded = "degraded"
	HealthDown     = "down"
)

// HealthStatus décrit la santé d'un connecteur.
type HealthStatus struct {
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	CheckedAt time.Time `json:"checked_at"`
}

// Connector est l'interface que chaque fournisseur implémente.
type Connector interface {
	Type() string
	Validate(ctx context.Context, cfg Config) error
	RequiredPermissions() []Permission
	SyncInventory(ctx context.Context, since time.Time) (<-chan Resource, error)
	SyncMetrics(ctx context.Context, window TimeWindow) (<-chan MetricPoint, error)
	SyncBilling(ctx context.Context, period Period) (<-chan CostLine, error) // optionnel : ErrNotSupported
	Health(ctx context.Context) HealthStatus
}

// EventSource est implémentée par les connecteurs qui remontent des événements
// en mode pull (ex. événements Kubernetes). Les webhooks passent par WebhookParser.
type EventSource interface {
	SyncEvents(ctx context.Context, since time.Time) (<-chan Event, error)
}

// HistoricalInventory est implémentée par les connecteurs capables de
// restituer l'inventaire à une date passée : le backfill rejoue alors
// l'historique à intervalle régulier au lieu de ne connaître que l'état courant.
type HistoricalInventory interface {
	SyncInventoryAt(ctx context.Context, at time.Time) (<-chan Resource, error)
	BackfillStep() time.Duration
}

// PushOnly est implémentée par les connecteurs alimentés uniquement par
// poussée (agent Kairn, webhooks) : aucune synchronisation pull n'est lancée.
type PushOnly interface {
	PushOnly() bool
}

// WebhookParser décode un webhook entrant (GitLab, GitHub, Argo CD, Alertmanager…).
type WebhookParser interface {
	// VerifyWebhook contrôle la signature ou le jeton partagé.
	VerifyWebhook(headers map[string]string, body []byte, secret string) error
	ParseWebhook(headers map[string]string, body []byte) ([]Event, error)
}

// Catégories de connecteurs.
const (
	CategoryCloud      = "cloud"
	CategoryKubernetes = "kubernetes"
	CategoryMetrics    = "metrics"
	CategoryEvents     = "events"
	CategoryBilling    = "billing"
	CategoryAgent      = "agent"
)

// Field décrit un champ de configuration affiché dans l'UI d'onboarding.
type Field struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Secret   bool   `json:"secret"`
	Required bool   `json:"required"`
	Default  string `json:"default,omitempty"`
	Help     string `json:"help,omitempty"`
}

// TypeInfo décrit un type de connecteur : ressources couvertes, fréquence,
// permissions minimales et champs de configuration.
type TypeInfo struct {
	Type            string        `json:"type"`
	DisplayName     string        `json:"display_name"`
	Category        string        `json:"category"`
	Provider        string        `json:"provider"`
	Resources       []string      `json:"resources"`
	DefaultInterval time.Duration `json:"default_interval"`
	Fields          []Field       `json:"fields"`
	Permissions     []Permission  `json:"permissions"`
	Metrics         bool          `json:"metrics"`
	Billing         bool          `json:"billing"`
	Webhook         bool          `json:"webhook"`
	DocsURL         string        `json:"docs_url"`
}

// Factory construit une instance de connecteur.
type Factory func(cfg Config) (Connector, error)

type entry struct {
	info    TypeInfo
	factory Factory
}

var (
	regMu    sync.RWMutex
	registry = map[string]entry{}
)

// Register enregistre un type de connecteur. Appelé depuis init() des packages connecteurs.
func Register(info TypeInfo, f Factory) {
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := registry[info.Type]; dup {
		panic("connector: duplicate registration for " + info.Type)
	}
	registry[info.Type] = entry{info: info, factory: f}
}

// New instancie un connecteur enregistré.
func New(typ string, cfg Config) (Connector, error) {
	regMu.RLock()
	e, ok := registry[typ]
	regMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("connector: unknown type %q", typ)
	}
	return e.factory(cfg)
}

// Info renvoie la description d'un type.
func Info(typ string) (TypeInfo, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	e, ok := registry[typ]
	return e.info, ok
}

// Types liste les types enregistrés, triés.
func Types() []TypeInfo {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]TypeInfo, 0, len(registry))
	for _, e := range registry {
		out = append(out, e.info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out
}
