package model

import (
	"time"

	"github.com/shopspring/decimal"
)

// Budget couvre un nœud d'allocation (ou toute l'organisation si NodeID est nul).
type Budget struct {
	ID            string          `json:"id" db:"id"`
	OrgID         string          `json:"org_id" db:"org_id"`
	NodeID        *string         `json:"node_id,omitempty" db:"node_id"`
	Name          string          `json:"name" db:"name"`
	Period        string          `json:"period" db:"period"` // monthly | quarterly | yearly
	Amount        decimal.Decimal `json:"amount" db:"amount"`
	Currency      string          `json:"currency" db:"currency"`
	Thresholds    []int           `json:"thresholds" db:"thresholds"` // pourcentages
	ForecastAlert bool            `json:"forecast_alert" db:"forecast_alert"`
	ChannelIDs    []string        `json:"channel_ids" db:"channel_ids"`
	CreatedAt     time.Time       `json:"created_at" db:"created_at"`
}

// BudgetStatus est l'état calculé d'un budget sur la période courante.
type BudgetStatus struct {
	Budget          Budget          `json:"budget"`
	PeriodStart     time.Time       `json:"period_start"`
	PeriodEnd       time.Time       `json:"period_end"`
	Actual          decimal.Decimal `json:"actual"`
	Forecast        decimal.Decimal `json:"forecast"`
	ActualPercent   decimal.Decimal `json:"actual_percent"`
	ForecastPercent decimal.Decimal `json:"forecast_percent"`
	Currency        string          `json:"currency"`
}

// Types de règles d'alerte.
const (
	AlertBudgetActual   = "budget_actual"
	AlertBudgetForecast = "budget_forecast"
	AlertAnomaly        = "anomaly"
	AlertUptime         = "uptime"
	AlertRecommendation = "recommendation"
	AlertConnector      = "connector"
)

// BusinessHours restreint les notifications à des plages horaires.
type BusinessHours struct {
	Timezone string `json:"timezone"`
	Days     []int  `json:"days"`  // 1 = lundi … 7 = dimanche
	Start    string `json:"start"` // "09:00"
	End      string `json:"end"`   // "18:00"
}

// AlertRule définit quand et où notifier.
type AlertRule struct {
	ID                 string            `json:"id" db:"id"`
	OrgID              string            `json:"org_id" db:"org_id"`
	Name               string            `json:"name" db:"name"`
	Kind               string            `json:"kind" db:"kind"`
	Config             map[string]string `json:"config" db:"config"` // ex. min_severity, min_savings
	ChannelIDs         []string          `json:"channel_ids" db:"channel_ids"`
	BusinessHours      *BusinessHours    `json:"business_hours,omitempty" db:"business_hours"`
	GroupWindowSeconds int               `json:"group_window_seconds" db:"group_window_seconds"`
	Enabled            bool              `json:"enabled" db:"enabled"`
	CreatedAt          time.Time         `json:"created_at" db:"created_at"`
}

// États d'un événement d'alerte.
const (
	AlertFiring     = "firing"
	AlertResolved   = "resolved"
	AlertSilenced   = "silenced"
	AlertSuppressed = "suppressed"
)

// AlertEvent est une alerte déclenchée, dédupliquée par empreinte.
type AlertEvent struct {
	ID          string         `json:"id" db:"id"`
	OrgID       string         `json:"org_id" db:"org_id"`
	RuleID      *string        `json:"rule_id,omitempty" db:"rule_id"`
	Kind        string         `json:"kind" db:"kind"`
	Severity    string         `json:"severity" db:"severity"` // info | warning | critical
	Fingerprint string         `json:"fingerprint" db:"fingerprint"`
	Title       string         `json:"title" db:"title"`
	Body        string         `json:"body" db:"body"`
	Link        string         `json:"link" db:"link"`
	Payload     map[string]any `json:"payload" db:"payload"`
	Status      string         `json:"status" db:"status"`
	Count       int            `json:"count" db:"count"`
	FirstAt     time.Time      `json:"first_at" db:"first_at"`
	LastAt      time.Time      `json:"last_at" db:"last_at"`
	NotifiedAt  *time.Time     `json:"notified_at,omitempty" db:"notified_at"`
}

// Types de canaux de notification.
const (
	ChannelEmail      = "email"
	ChannelSlack      = "slack"
	ChannelTeams      = "teams"
	ChannelMattermost = "mattermost"
	ChannelWebhook    = "webhook"
	ChannelPagerDuty  = "pagerduty"
)

// NotificationChannel est une destination de notification.
// Les éléments secrets (URL de webhook, clé d'intégration) sont chiffrés.
type NotificationChannel struct {
	ID         string            `json:"id" db:"id"`
	OrgID      string            `json:"org_id" db:"org_id"`
	Kind       string            `json:"kind" db:"kind"`
	Name       string            `json:"name" db:"name"`
	Settings   map[string]string `json:"settings" db:"settings"`
	SecretsEnc []byte            `json:"-" db:"secrets_enc"`
	Enabled    bool              `json:"enabled" db:"enabled"`
	CreatedAt  time.Time         `json:"created_at" db:"created_at"`
}

// Silence suspend les notifications correspondant à ses critères.
type Silence struct {
	ID        string            `json:"id" db:"id"`
	OrgID     string            `json:"org_id" db:"org_id"`
	Matchers  map[string]string `json:"matchers" db:"matchers"` // kind, severity, fingerprint…
	StartsAt  time.Time         `json:"starts_at" db:"starts_at"`
	EndsAt    time.Time         `json:"ends_at" db:"ends_at"`
	Reason    string            `json:"reason" db:"reason"`
	CreatedBy string            `json:"created_by" db:"created_by"`
	CreatedAt time.Time         `json:"created_at" db:"created_at"`
}

// Types de recommandation (M-06).
const (
	RecoRightsizeVM       = "rightsize_vm"
	RecoRightsizeWorkload = "rightsize_workload"
	RecoOrphanVolume      = "orphan_volume"
	RecoOrphanIP          = "orphan_ip"
	RecoOldSnapshot       = "old_snapshot"
	RecoIdleResource      = "idle_resource"
	RecoOffHours          = "off_hours_schedule"
	RecoFlavorChange      = "flavor_change"
	RecoStorageTier       = "storage_tier"
	RecoCommitment        = "commitment"
)

// États du cycle de vie d'une recommandation.
const (
	RecoOpen      = "open"
	RecoAccepted  = "accepted"
	RecoPostponed = "postponed"
	RecoDismissed = "dismissed"
	RecoApplied   = "applied"
)

// Remediation contient les étapes et commandes prêtes à l'emploi.
type Remediation struct {
	Steps     []string `json:"steps"`
	CLI       string   `json:"cli,omitempty"`       // CLI OpenStack / kubectl / fournisseur
	Manifest  string   `json:"manifest,omitempty"`  // patch/manifest K8s
	Terraform string   `json:"terraform,omitempty"` // extrait Terraform/OpenTofu
}

// Recommendation est une action d'optimisation proposée.
type Recommendation struct {
	ID                     string           `json:"id" db:"id"`
	OrgID                  string           `json:"org_id" db:"org_id"`
	Type                   string           `json:"type" db:"type"`
	ResourceID             string           `json:"resource_id" db:"resource_id"`
	Fingerprint            string           `json:"fingerprint" db:"fingerprint"`
	Title                  string           `json:"title" db:"title"`
	Summary                string           `json:"summary" db:"summary"`
	SavingsMonthly         decimal.Decimal  `json:"savings_monthly" db:"savings_monthly"`
	Currency               string           `json:"currency" db:"currency"`
	Risk                   string           `json:"risk" db:"risk"` // low | medium | high
	Status                 string           `json:"status" db:"status"`
	StatusReason           string           `json:"status_reason" db:"status_reason"`
	PostponedUntil         *time.Time       `json:"postponed_until,omitempty" db:"postponed_until"`
	Evidence               map[string]any   `json:"evidence" db:"evidence"`
	Remediation            Remediation      `json:"remediation" db:"remediation"`
	AppliedAt              *time.Time       `json:"applied_at,omitempty" db:"applied_at"`
	MeasuredSavingsMonthly *decimal.Decimal `json:"measured_savings_monthly,omitempty" db:"measured_savings_monthly"`
	CreatedAt              time.Time        `json:"created_at" db:"created_at"`
	UpdatedAt              time.Time        `json:"updated_at" db:"updated_at"`
}

// CorrelatedEvent est un événement rapproché d'une anomalie, avec son score.
type CorrelatedEvent struct {
	Event Event   `json:"event"`
	Score float64 `json:"score"`
	Why   string  `json:"why"`
}

// Anomaly est une déviation détectée sur une série de coût ou d'usage.
type Anomaly struct {
	ID                 string            `json:"id" db:"id"`
	OrgID              string            `json:"org_id" db:"org_id"`
	SeriesKey          string            `json:"series_key" db:"series_key"`
	Kind               string            `json:"kind" db:"kind"` // cost | usage
	Title              string            `json:"title" db:"title"`
	WindowStart        time.Time         `json:"window_start" db:"window_start"`
	WindowEnd          time.Time         `json:"window_end" db:"window_end"`
	Severity           string            `json:"severity" db:"severity"`
	Expected           decimal.Decimal   `json:"expected" db:"expected"`
	Actual             decimal.Decimal   `json:"actual" db:"actual"`
	Currency           string            `json:"currency" db:"currency"`
	Score              float64           `json:"score" db:"score"`
	CorrelatedEvents   []CorrelatedEvent `json:"correlated_events" db:"correlated_events"`
	Explanation        string            `json:"explanation" db:"explanation"`
	ExplanationSources []string          `json:"explanation_sources" db:"explanation_sources"`
	Status             string            `json:"status" db:"status"` // open | acknowledged | resolved
	CreatedAt          time.Time         `json:"created_at" db:"created_at"`
}

// ForecastPoint est un point de prévision avec intervalle de confiance.
type ForecastPoint struct {
	Day   time.Time       `json:"day"`
	Value decimal.Decimal `json:"value"`
	Lower decimal.Decimal `json:"lower"`
	Upper decimal.Decimal `json:"upper"`
}

// Forecast est la prévision de coût d'un nœud jusqu'à la fin de période.
type Forecast struct {
	OrgID       string          `json:"org_id" db:"org_id"`
	NodeID      string          `json:"node_id" db:"node_id"` // "" = organisation
	GeneratedAt time.Time       `json:"generated_at" db:"generated_at"`
	Model       string          `json:"model" db:"model"`
	Currency    string          `json:"currency" db:"currency"`
	Points      []ForecastPoint `json:"points" db:"points"`
	PeriodEnd   time.Time       `json:"period_end" db:"period_end"`
	Total       decimal.Decimal `json:"total" db:"total"`
	Lower       decimal.Decimal `json:"lower" db:"lower"`
	Upper       decimal.Decimal `json:"upper" db:"upper"`
}

// UptimeCheck est une sonde HTTP/TCP/ICMP exécutée depuis plusieurs régions.
type UptimeCheck struct {
	ID              string    `json:"id" db:"id"`
	OrgID           string    `json:"org_id" db:"org_id"`
	Name            string    `json:"name" db:"name"`
	Kind            string    `json:"kind" db:"kind"` // http | tcp | icmp
	Target          string    `json:"target" db:"target"`
	IntervalSeconds int       `json:"interval_seconds" db:"interval_seconds"`
	TimeoutMS       int       `json:"timeout_ms" db:"timeout_ms"`
	Regions         []string  `json:"regions" db:"regions"`
	ExpectedStatus  int       `json:"expected_status" db:"expected_status"`
	Keyword         string    `json:"keyword" db:"keyword"`
	NodeID          *string   `json:"node_id,omitempty" db:"node_id"`
	FailThreshold   int       `json:"fail_threshold" db:"fail_threshold"` // régions en échec avant incident
	Enabled         bool      `json:"enabled" db:"enabled"`
	CreatedAt       time.Time `json:"created_at" db:"created_at"`
}

// UptimeResult est le résultat d'une exécution de sonde.
type UptimeResult struct {
	OrgID      string    `json:"org_id"`
	CheckID    string    `json:"check_id"`
	Region     string    `json:"region"`
	TS         time.Time `json:"ts"`
	Up         bool      `json:"up"`
	LatencyMS  float64   `json:"latency_ms"`
	StatusCode int       `json:"status_code"`
	Error      string    `json:"error,omitempty"`
}

// StatusPage est une page de statut publique ou privée.
type StatusPage struct {
	ID         string            `json:"id" db:"id"`
	OrgID      string            `json:"org_id" db:"org_id"`
	Slug       string            `json:"slug" db:"slug"`
	Title      string            `json:"title" db:"title"`
	Public     bool              `json:"public" db:"public"`
	AccessHash *string           `json:"-" db:"access_hash"`
	CheckIDs   []string          `json:"check_ids" db:"check_ids"`
	Branding   map[string]string `json:"branding" db:"branding"`
	CreatedAt  time.Time         `json:"created_at" db:"created_at"`
}

// IncidentUpdate est une mise à jour d'incident.
type IncidentUpdate struct {
	At      time.Time `json:"at"`
	Status  string    `json:"status"`
	Message string    `json:"message"`
}

// Incident est une indisponibilité (sonde, PagerDuty, saisie manuelle).
type Incident struct {
	ID         string           `json:"id" db:"id"`
	OrgID      string           `json:"org_id" db:"org_id"`
	CheckID    *string          `json:"check_id,omitempty" db:"check_id"`
	Title      string           `json:"title" db:"title"`
	Status     string           `json:"status" db:"status"` // open | resolved
	Source     string           `json:"source" db:"source"`
	StartedAt  time.Time        `json:"started_at" db:"started_at"`
	ResolvedAt *time.Time       `json:"resolved_at,omitempty" db:"resolved_at"`
	Updates    []IncidentUpdate `json:"updates" db:"updates"`
}

// Report est un rapport généré (rapport mensuel exécutif).
type Report struct {
	ID        string         `json:"id" db:"id"`
	OrgID     string         `json:"org_id" db:"org_id"`
	Kind      string         `json:"kind" db:"kind"`
	Period    string         `json:"period" db:"period"` // "2026-08"
	Status    string         `json:"status" db:"status"` // pending | ready | sent | error
	ObjectKey string         `json:"object_key" db:"object_key"`
	Summary   map[string]any `json:"summary" db:"summary"`
	CreatedAt time.Time      `json:"created_at" db:"created_at"`
	SentAt    *time.Time     `json:"sent_at,omitempty" db:"sent_at"`
}

// ExportJob est un export planifié vers un stockage S3-compatible.
type ExportJob struct {
	ID          string            `json:"id" db:"id"`
	OrgID       string            `json:"org_id" db:"org_id"`
	Name        string            `json:"name" db:"name"`
	Format      string            `json:"format" db:"format"` // csv | parquet
	Destination map[string]string `json:"destination" db:"destination"`
	SecretsEnc  []byte            `json:"-" db:"secrets_enc"`
	Schedule    string            `json:"schedule" db:"schedule"` // daily | monthly
	LastRunAt   *time.Time        `json:"last_run_at,omitempty" db:"last_run_at"`
	Enabled     bool              `json:"enabled" db:"enabled"`
	CreatedAt   time.Time         `json:"created_at" db:"created_at"`
}

// WebhookSubscription pousse des événements Kairn vers une URL cliente.
type WebhookSubscription struct {
	ID        string    `json:"id" db:"id"`
	OrgID     string    `json:"org_id" db:"org_id"`
	URL       string    `json:"url" db:"url"`
	SecretEnc []byte    `json:"-" db:"secret_enc"`
	Events    []string  `json:"events" db:"events"`
	Enabled   bool      `json:"enabled" db:"enabled"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// LLMUsage trace la consommation LLM d'une organisation.
type LLMUsage struct {
	ID           string          `json:"id" db:"id"`
	OrgID        string          `json:"org_id" db:"org_id"`
	At           time.Time       `json:"at" db:"at"`
	Feature      string          `json:"feature" db:"feature"` // assistant | report | explain | mcp
	Provider     string          `json:"provider" db:"provider"`
	Model        string          `json:"model" db:"model"`
	InputTokens  int             `json:"input_tokens" db:"input_tokens"`
	OutputTokens int             `json:"output_tokens" db:"output_tokens"`
	Cost         decimal.Decimal `json:"cost" db:"cost"`
	Currency     string          `json:"currency" db:"currency"`
}
