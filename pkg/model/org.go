// Package model définit les entités du domaine Kairn, partagées par l'API,
// les workers et les dépôts. Toute entité client porte OrgID.
package model

import (
	"time"

	"github.com/shopspring/decimal"
)

// Role est un rôle RBAC au sein d'une organisation.
type Role string

// Rôles RBAC (M-11).
const (
	RoleOwner    Role = "owner"
	RoleAdmin    Role = "admin"
	RoleFinance  Role = "finance"
	RoleEngineer Role = "engineer"
	RoleViewer   Role = "viewer"
)

// Roles liste les rôles valides du plus au moins privilégié.
var Roles = []Role{RoleOwner, RoleAdmin, RoleFinance, RoleEngineer, RoleViewer}

// Valid indique si le rôle est connu.
func (r Role) Valid() bool {
	for _, x := range Roles {
		if x == r {
			return true
		}
	}
	return false
}

// Plan est un plan commercial (§12).
type Plan string

// Plans commerciaux.
const (
	PlanStarter    Plan = "starter"
	PlanTeam       Plan = "team"
	PlanEnterprise Plan = "enterprise"
	PlanMSP        Plan = "msp"
)

// Organization est un locataire. ParentOrgID est renseigné pour les
// organisations clientes gérées par un MSP.
type Organization struct {
	ID          string           `json:"id" db:"id"`
	ParentOrgID *string          `json:"parent_org_id,omitempty" db:"parent_org_id"`
	Name        string           `json:"name" db:"name"`
	Slug        string           `json:"slug" db:"slug"`
	Plan        Plan             `json:"plan" db:"plan"`
	Currency    string           `json:"currency" db:"currency"`
	Locale      string           `json:"locale" db:"locale"`
	Timezone    string           `json:"timezone" db:"timezone"`
	VATRate     *decimal.Decimal `json:"vat_rate,omitempty" db:"vat_rate"`
	Settings    OrgSettings      `json:"settings" db:"settings"`
	TrialEndsAt *time.Time       `json:"trial_ends_at,omitempty" db:"trial_ends_at"`
	CreatedAt   time.Time        `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at" db:"updated_at"`
}

// OrgSettings regroupe les réglages d'organisation stockés en JSONB.
type OrgSettings struct {
	// LLM : fournisseur autorisé ("anthropic", "mistral", "local", "none").
	LLMProvider string `json:"llm_provider,omitempty"`
	// AllowExternalLLM doit être vrai pour qu'un appel LLM sorte de l'UE.
	AllowExternalLLM bool `json:"allow_external_llm"`
	// K8sAllocationMethod : "max" (défaut), "requests" ou "usage".
	K8sAllocationMethod string `json:"k8s_allocation_method,omitempty"`
	// K8sIdleMode : "keep" (ligne idle par cluster) ou "distribute".
	K8sIdleMode string `json:"k8s_idle_mode,omitempty"`
	// PreferInvoice : quand une facture est disponible, elle fait foi.
	PreferInvoice bool `json:"prefer_invoice"`
	// RightsizingPercentile : percentile utilisé pour le rightsizing (95 par défaut).
	RightsizingPercentile int `json:"rightsizing_percentile,omitempty"`
	// RightsizingWindowDays : fenêtre glissante (14 par défaut).
	RightsizingWindowDays int `json:"rightsizing_window_days,omitempty"`
	// SSOIdPAlias : alias du fournisseur d'identité Keycloak (OIDC/SAML).
	SSOIdPAlias string `json:"sso_idp_alias,omitempty"`
	// SSOEnforced interdit la connexion hors SSO.
	SSOEnforced bool `json:"sso_enforced"`
	// WhiteLabel : personnalisation des rapports (MSP).
	WhiteLabel *WhiteLabel `json:"white_label,omitempty"`
	// ReportRecipients : destinataires du rapport mensuel exécutif.
	ReportRecipients []string `json:"report_recipients,omitempty"`
}

// WhiteLabel décrit la marque blanche d'un MSP.
type WhiteLabel struct {
	CompanyName  string `json:"company_name"`
	LogoURL      string `json:"logo_url,omitempty"`
	PrimaryColor string `json:"primary_color,omitempty"`
	SupportEmail string `json:"support_email,omitempty"`
}

// User est une identité globale ; son appartenance aux organisations passe par Membership.
type User struct {
	ID          string    `json:"id" db:"id"`
	Email       string    `json:"email" db:"email"`
	Name        string    `json:"name" db:"name"`
	OIDCSubject *string   `json:"-" db:"oidc_subject"`
	Locale      string    `json:"locale" db:"locale"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}

// Membership lie un utilisateur à une organisation avec un rôle et des scopes
// optionnels (identifiants de nœuds d'allocation ; vide = toute l'organisation).
type Membership struct {
	OrgID     string    `json:"org_id" db:"org_id"`
	UserID    string    `json:"user_id" db:"user_id"`
	Role      Role      `json:"role" db:"role"`
	Scopes    []string  `json:"scopes" db:"scopes"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	// Champs dénormalisés pour l'affichage.
	Email string `json:"email,omitempty" db:"-"`
	Name  string `json:"name,omitempty" db:"-"`
}

// APIToken est un jeton d'API scoppé. Seul le hash est stocké.
type APIToken struct {
	ID         string     `json:"id" db:"id"`
	OrgID      string     `json:"org_id" db:"org_id"`
	Name       string     `json:"name" db:"name"`
	Prefix     string     `json:"prefix" db:"prefix"`
	Hash       string     `json:"-" db:"hash"`
	Role       Role       `json:"role" db:"role"`
	Scopes     []string   `json:"scopes" db:"scopes"`
	CreatedBy  *string    `json:"created_by,omitempty" db:"created_by"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty" db:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty" db:"last_used_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty" db:"revoked_at"`
	CreatedAt  time.Time  `json:"created_at" db:"created_at"`
}

// AuditEvent est une entrée du journal d'audit.
type AuditEvent struct {
	ID         string         `json:"id" db:"id"`
	OrgID      string         `json:"org_id" db:"org_id"`
	ActorType  string         `json:"actor_type" db:"actor_type"`
	ActorID    string         `json:"actor_id" db:"actor_id"`
	Action     string         `json:"action" db:"action"`
	TargetType string         `json:"target_type" db:"target_type"`
	TargetID   string         `json:"target_id" db:"target_id"`
	IP         string         `json:"ip" db:"ip"`
	UserAgent  string         `json:"user_agent" db:"user_agent"`
	Details    map[string]any `json:"details" db:"details"`
	At         time.Time      `json:"at" db:"at"`
}

// Subscription porte l'état de facturation SaaS (Stripe).
type Subscription struct {
	OrgID                string     `json:"org_id" db:"org_id"`
	Plan                 Plan       `json:"plan" db:"plan"`
	Status               string     `json:"status" db:"status"`
	StripeCustomerID     *string    `json:"-" db:"stripe_customer_id"`
	StripeSubscriptionID *string    `json:"-" db:"stripe_subscription_id"`
	CurrentPeriodEnd     *time.Time `json:"current_period_end,omitempty" db:"current_period_end"`
	TrialEndsAt          *time.Time `json:"trial_ends_at,omitempty" db:"trial_ends_at"`
	UpdatedAt            time.Time  `json:"updated_at" db:"updated_at"`
}
