// Package store définit les dépôts de données relationnelles (PostgreSQL).
// Toutes les méthodes lisent l'organisation courante via tenancy.OrgID(ctx)
// et échouent sans elle ; les implémentations filtrent systématiquement sur
// org_id, en plus de la RLS côté PostgreSQL.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/model"
)

// Erreurs communes.
var (
	ErrNotFound = errors.New("store: not found")
	ErrConflict = errors.New("store: conflict")
)

// DefaultLimit et MaxLimit bornent la pagination.
const (
	DefaultLimit = 50
	MaxLimit     = 500
)

// ListQuery est une requête paginée par curseur (identifiants UUIDv7 ordonnés).
type ListQuery struct {
	Cursor  string
	Limit   int
	Filters map[string]string
}

// Normalize applique les bornes de pagination.
func (q ListQuery) Normalize() ListQuery {
	if q.Limit <= 0 {
		q.Limit = DefaultLimit
	}
	if q.Limit > MaxLimit {
		q.Limit = MaxLimit
	}
	return q
}

// CRUD est le dépôt générique d'une entité rattachée à une organisation.
type CRUD[T any] interface {
	Get(ctx context.Context, id string) (T, error)
	List(ctx context.Context, q ListQuery) ([]T, error)
	Create(ctx context.Context, v *T) error
	Update(ctx context.Context, v *T) error
	Delete(ctx context.Context, id string) error
}

// ListAll parcourt toutes les pages d'un dépôt CRUD.
func ListAll[T any](ctx context.Context, c CRUD[T], id func(T) string, filters map[string]string) ([]T, error) {
	var out []T
	cursor := ""
	for {
		page, err := c.List(ctx, ListQuery{Cursor: cursor, Limit: MaxLimit, Filters: filters})
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		if len(page) < MaxLimit {
			return out, nil
		}
		cursor = id(page[len(page)-1])
	}
}

// Store regroupe les dépôts.
type Store interface {
	// InTx exécute fn dans une transaction unique (atomicité multi-dépôts).
	InTx(ctx context.Context, fn func(ctx context.Context) error) error

	Orgs() OrgRepo
	Users() UserRepo
	Memberships() MembershipRepo
	Tokens() TokenRepo
	Audit() AuditRepo
	Subscriptions() SubscriptionRepo
	Connectors() ConnectorRepo
	Runs() RunRepo
	Resources() ResourceRepo
	Pricing() PricingRepo
	OnPremModels() CRUD[model.OnPremCostModel]
	Adjustments() CRUD[model.PricingAdjustment]
	Rates() RateRepo
	Reconciliations() ReconciliationRepo
	AllocationNodes() CRUD[model.AllocationNode]
	AllocationRules() CRUD[model.AllocationRule]
	SharedRules() CRUD[model.SharedCostRule]
	UnitMetrics() CRUD[model.UnitMetric]
	Channels() CRUD[model.NotificationChannel]
	Budgets() CRUD[model.Budget]
	AlertRules() CRUD[model.AlertRule]
	AlertEvents() AlertEventRepo
	Silences() CRUD[model.Silence]
	Recommendations() RecommendationRepo
	Anomalies() AnomalyRepo
	Forecasts() ForecastRepo
	UptimeChecks() CRUD[model.UptimeCheck]
	StatusPages() StatusPageRepo
	Incidents() IncidentRepo
	Reports() ReportRepo
	Exports() CRUD[model.ExportJob]
	Webhooks() CRUD[model.WebhookSubscription]
	LLMUsage() LLMUsageRepo
	System() SystemRepo
}

// OrgRepo gère les organisations. Create exige que le contexte porte l'identifiant de la nouvelle organisation.
type OrgRepo interface {
	Get(ctx context.Context, id string) (model.Organization, error)
	Create(ctx context.Context, o *model.Organization) error
	Update(ctx context.Context, o *model.Organization) error
	ListChildren(ctx context.Context) ([]model.Organization, error)
	// ListForUser liste les organisations dont l'utilisateur est membre (contexte utilisateur).
	ListForUser(ctx context.Context, userID string) ([]model.Organization, error)
	// Delete supprime l'organisation courante et toutes ses données (droit à l'effacement, RGPD).
	// Refusé (ErrConflict) tant que des organisations clientes (MSP) y sont rattachées.
	Delete(ctx context.Context, id string) error
}

// UserRepo gère les identités.
type UserRepo interface {
	Get(ctx context.Context, id string) (model.User, error)
	Create(ctx context.Context, u *model.User) error
	Update(ctx context.Context, u *model.User) error
}

// MembershipRepo gère les appartenances de l'organisation courante.
type MembershipRepo interface {
	List(ctx context.Context) ([]model.Membership, error)
	Get(ctx context.Context, userID string) (model.Membership, error)
	Upsert(ctx context.Context, m *model.Membership) error
	Delete(ctx context.Context, userID string) error
	// ForUser liste les appartenances d'un utilisateur, toutes organisations confondues (contexte utilisateur).
	ForUser(ctx context.Context, userID string) ([]model.Membership, error)
}

// TokenRepo gère les jetons d'API.
type TokenRepo interface {
	List(ctx context.Context) ([]model.APIToken, error)
	Create(ctx context.Context, t *model.APIToken) error
	Revoke(ctx context.Context, id string, at time.Time) error
	Touch(ctx context.Context, id string, at time.Time) error
}

// AuditFilter filtre le journal d'audit.
type AuditFilter struct {
	From, To time.Time
	Action   string
	ActorID  string
	Cursor   string
	Limit    int
}

// AuditRepo gère le journal d'audit (ajout seul).
type AuditRepo interface {
	Append(ctx context.Context, e *model.AuditEvent) error
	List(ctx context.Context, f AuditFilter) ([]model.AuditEvent, error)
}

// SubscriptionRepo gère l'abonnement SaaS de l'organisation courante.
type SubscriptionRepo interface {
	Get(ctx context.Context) (model.Subscription, error)
	Upsert(ctx context.Context, s *model.Subscription) error
}

// ConnectorRepo gère les connecteurs.
type ConnectorRepo interface {
	CRUD[model.Connector]
	UpdateStatus(ctx context.Context, id string, status model.ConnectorStatus, msg string, syncAt time.Time, success bool) error
}

// RunRepo trace les exécutions de synchronisation.
type RunRepo interface {
	Create(ctx context.Context, r *model.ConnectorRun) error
	Finish(ctx context.Context, r *model.ConnectorRun) error
	ListByConnector(ctx context.Context, connectorID string, limit int) ([]model.ConnectorRun, error)
}

// ResourceFilter filtre l'inventaire.
type ResourceFilter struct {
	ConnectorID string
	Types       []string
	Provider    string
	Region      string
	Query       string            // recherche dans le nom / l'identifiant externe
	Labels      map[string]string // égalité stricte
	IDs         []string
	// At : instantané historique ; zéro = état courant.
	At     time.Time
	Cursor string
	Limit  int
}

// ResourceRepo gère l'inventaire historisé et le graphe de topologie.
type ResourceRepo interface {
	// Current liste les versions courantes (ou à l'instant f.At).
	Current(ctx context.Context, f ResourceFilter) ([]model.Resource, error)
	// Get renvoie la version courante, ou la dernière version si la ressource a disparu.
	Get(ctx context.Context, id string) (model.Resource, error)
	History(ctx context.Context, id string) ([]model.Resource, error)
	// InWindow renvoie toutes les versions chevauchant [from, to).
	InWindow(ctx context.Context, from, to time.Time) ([]model.Resource, error)
	// InsertVersions ajoute de nouvelles versions.
	InsertVersions(ctx context.Context, rs []model.Resource) error
	// CloseVersions ferme les versions courantes des ressources données.
	CloseVersions(ctx context.Context, ids []string, at time.Time) error
	// Edges renvoie les arêtes chevauchant [from, to) ; zéro = arêtes courantes.
	Edges(ctx context.Context, from, to time.Time) ([]model.ResourceEdge, error)
	CurrentEdges(ctx context.Context, connectorID string) ([]model.ResourceEdge, error)
	InsertEdges(ctx context.Context, es []model.ResourceEdge) error
	CloseEdges(ctx context.Context, es []model.ResourceEdge, at time.Time) error
	Count(ctx context.Context) (int, error)
}

// PricingRepo gère les grilles tarifaires publiques et négociées.
type PricingRepo interface {
	// CreateCatalog crée une grille et ses articles. Grille publique : c.OrgID nul et contexte système.
	CreateCatalog(ctx context.Context, c *model.PriceCatalog, items []model.PriceItem) error
	ListCatalogs(ctx context.Context) ([]model.PriceCatalog, error)
	Items(ctx context.Context, catalogID string) ([]model.PriceItem, error)
	DeleteCatalog(ctx context.Context, id string) error
}

// RateRepo gère les taux de change (données de référence globales).
type RateRepo interface {
	Upsert(ctx context.Context, rates []model.ExchangeRate) error
	// Get renvoie le taux le plus récent disponible au jour donné ou avant.
	Get(ctx context.Context, base, quote string, day time.Time) (model.ExchangeRate, error)
}

// ReconciliationRepo gère les rapprochements estimé/facturé.
type ReconciliationRepo interface {
	Upsert(ctx context.Context, r *model.Reconciliation) error
	List(ctx context.Context) ([]model.Reconciliation, error)
}

// AlertEventRepo gère les alertes déclenchées.
type AlertEventRepo interface {
	List(ctx context.Context, q ListQuery) ([]model.AlertEvent, error)
	Get(ctx context.Context, id string) (model.AlertEvent, error)
	// FindActive renvoie l'alerte active (non résolue) d'empreinte donnée.
	FindActive(ctx context.Context, fingerprint string) (model.AlertEvent, error)
	Create(ctx context.Context, e *model.AlertEvent) error
	Update(ctx context.Context, e *model.AlertEvent) error
}

// RecommendationFilter filtre les recommandations.
type RecommendationFilter struct {
	Status     []string
	Types      []string
	ResourceID string
	Cursor     string
	Limit      int
}

// RecommendationRepo gère les recommandations.
type RecommendationRepo interface {
	List(ctx context.Context, f RecommendationFilter) ([]model.Recommendation, error)
	Get(ctx context.Context, id string) (model.Recommendation, error)
	// Upsert crée ou met à jour par empreinte en conservant le statut décidé par l'utilisateur.
	Upsert(ctx context.Context, r *model.Recommendation) error
	Update(ctx context.Context, r *model.Recommendation) error
}

// AnomalyRepo gère les anomalies.
type AnomalyRepo interface {
	List(ctx context.Context, from, to time.Time, status string) ([]model.Anomaly, error)
	Get(ctx context.Context, id string) (model.Anomaly, error)
	// Upsert crée ou met à jour par (série, début de fenêtre).
	Upsert(ctx context.Context, a *model.Anomaly) error
	Update(ctx context.Context, a *model.Anomaly) error
}

// ForecastRepo stocke la dernière prévision par nœud.
type ForecastRepo interface {
	Upsert(ctx context.Context, f *model.Forecast) error
	Get(ctx context.Context, nodeID string) (model.Forecast, error)
	List(ctx context.Context) ([]model.Forecast, error)
}

// StatusPageRepo gère les pages de statut.
type StatusPageRepo interface {
	CRUD[model.StatusPage]
}

// IncidentRepo gère les incidents.
type IncidentRepo interface {
	CRUD[model.Incident]
	OpenForCheck(ctx context.Context, checkID string) (model.Incident, error)
}

// ReportRepo gère les rapports générés.
type ReportRepo interface {
	CRUD[model.Report]
	GetByPeriod(ctx context.Context, kind, period string) (model.Report, error)
}

// LLMTotals agrège la consommation LLM d'une période.
type LLMTotals struct {
	InputTokens  int             `json:"input_tokens"`
	OutputTokens int             `json:"output_tokens"`
	Cost         decimal.Decimal `json:"cost"`
	Calls        int             `json:"calls"`
}

// LLMUsageRepo trace la consommation LLM.
type LLMUsageRepo interface {
	Append(ctx context.Context, u *model.LLMUsage) error
	Totals(ctx context.Context, from, to time.Time) (LLMTotals, error)
}

// SystemRepo expose les seuls accès transverses autorisés (fonctions SECURITY DEFINER).
type SystemRepo interface {
	ListOrgIDs(ctx context.Context) ([]string, error)
	FindUser(ctx context.Context, subject, email string) (model.User, error)
	FindAPIToken(ctx context.Context, prefix string) (model.APIToken, error)
	FindConnectorByWebhook(ctx context.Context, token string) (orgID, connectorID, typ string, err error)
	FindStatusPage(ctx context.Context, slug string) (orgID, pageID string, public bool, accessHash string, err error)
	FindOrgByStripeCustomer(ctx context.Context, customerID string) (string, error)
}
