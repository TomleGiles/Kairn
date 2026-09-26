package model

import (
	"time"

	"github.com/shopspring/decimal"
)

// Niveaux de la hiérarchie d'allocation.
const (
	NodeOrganization = "organization"
	NodeBusinessUnit = "business_unit"
	NodeTeam         = "team"
	NodeService      = "service"
	NodeEnvironment  = "environment"
)

// UnallocatedNodeID est l'identifiant logique des coûts non attribués.
const UnallocatedNodeID = "unallocated"

// AllocationNode est un nœud de l'arbre organisation → BU → équipe → service → environnement.
// Path contient les identifiants des ancêtres et du nœud, séparés par "/", ex. "/a/b/c/".
type AllocationNode struct {
	ID        string    `json:"id" db:"id"`
	OrgID     string    `json:"org_id" db:"org_id"`
	ParentID  *string   `json:"parent_id,omitempty" db:"parent_id"`
	Kind      string    `json:"kind" db:"kind"`
	Name      string    `json:"name" db:"name"`
	Path      string    `json:"path" db:"path"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// Opérateurs de règle d'allocation.
const (
	OpEq     = "eq"
	OpNeq    = "neq"
	OpIn     = "in"
	OpRegex  = "regex"
	OpExists = "exists"
	OpPrefix = "prefix"
)

// Condition teste un champ d'une ressource. Field accepte : "provider",
// "type", "name", "region", "connector_id", "external_id", "resource_id",
// "label.<clé>", "attr.<clé>" (ex. "attr.project_id", "attr.k8s.namespace").
type Condition struct {
	Field  string   `json:"field"`
	Op     string   `json:"op"`
	Value  string   `json:"value,omitempty"`
	Values []string `json:"values,omitempty"`
}

// AllocationRule attribue les ressources correspondant à toutes ses conditions à un nœud.
// Les règles sont évaluées par priorité croissante ; la première qui correspond gagne.
type AllocationRule struct {
	ID         string      `json:"id" db:"id"`
	OrgID      string      `json:"org_id" db:"org_id"`
	NodeID     string      `json:"node_id" db:"node_id"`
	Name       string      `json:"name" db:"name"`
	Priority   int         `json:"priority" db:"priority"`
	Conditions []Condition `json:"conditions" db:"conditions"`
	Enabled    bool        `json:"enabled" db:"enabled"`
	CreatedAt  time.Time   `json:"created_at" db:"created_at"`
}

// Méthodes de répartition des coûts partagés.
const (
	ShareProportional = "proportional" // au prorata du coût direct des cibles
	ShareFixed        = "fixed"        // pourcentages fixes (somme = 100)
	ShareWeighted     = "weighted"     // poids relatifs
)

// ShareTarget est une cible de répartition.
type ShareTarget struct {
	NodeID string          `json:"node_id"`
	Weight decimal.Decimal `json:"weight"`
}

// SharedCostRule répartit le coût de ressources communes (control plane,
// monitoring, ingress…) entre des nœuds cibles.
type SharedCostRule struct {
	ID        string        `json:"id" db:"id"`
	OrgID     string        `json:"org_id" db:"org_id"`
	Name      string        `json:"name" db:"name"`
	Source    []Condition   `json:"source" db:"source"`
	Method    string        `json:"method" db:"method"`
	Targets   []ShareTarget `json:"targets" db:"targets"`
	Enabled   bool          `json:"enabled" db:"enabled"`
	CreatedAt time.Time     `json:"created_at" db:"created_at"`
}

// UnitMetric définit une métrique métier servant au calcul de coût unitaire.
type UnitMetric struct {
	ID          string    `json:"id" db:"id"`
	OrgID       string    `json:"org_id" db:"org_id"`
	Name        string    `json:"name" db:"name"`
	UnitLabel   string    `json:"unit_label" db:"unit_label"` // ex. "requête", "client"
	NodeID      *string   `json:"node_id,omitempty" db:"node_id"`
	Source      string    `json:"source" db:"source"` // prometheus | api
	ConnectorID *string   `json:"connector_id,omitempty" db:"connector_id"`
	Query       string    `json:"query" db:"query"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}
