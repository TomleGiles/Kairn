// Package plans définit les limites et fonctionnalités de chaque plan
// commercial (§12). Elles sont appliquées côté API, jamais uniquement côté UI.
package plans

import (
	"time"

	"github.com/kairn-io/kairn/pkg/model"
)

// Feature est une fonctionnalité soumise au plan.
type Feature string

// Fonctionnalités.
const (
	FeatureCosts          Feature = "costs"
	FeatureUsage          Feature = "usage"
	FeatureRecommendation Feature = "recommendations"
	FeatureBudgets        Feature = "budgets"
	FeatureAllocation     Feature = "allocation"
	FeatureAnomalies      Feature = "anomalies"
	FeatureAssistant      Feature = "assistant"
	FeatureReports        Feature = "reports"
	FeatureSSO            Feature = "sso"
	FeatureSCIM           Feature = "scim"
	FeatureSovereignLLM   Feature = "sovereign_llm"
	FeatureMSP            Feature = "msp"
	FeatureWhiteLabel     Feature = "white_label"
	FeatureForecast       Feature = "forecast"
	FeatureUptime         Feature = "uptime"
	FeatureExports        Feature = "exports"
	FeatureUnitCosts      Feature = "unit_costs"
	FeatureAPI            Feature = "api"
	FeatureSelfHosted     Feature = "self_hosted"
)

// Unlimited représente une limite illimitée.
const Unlimited = -1

// Limits décrit un plan.
type Limits struct {
	Plan           model.Plan       `json:"plan"`
	MaxUsers       int              `json:"max_users"`
	MaxConnectors  int              `json:"max_connectors"`
	MaxProviders   int              `json:"max_cloud_providers"`
	LLMTokensMonth int              `json:"llm_tokens_month"`
	RetentionDays  int              `json:"retention_days"`
	Features       map[Feature]bool `json:"features"`
}

func features(fs ...Feature) map[Feature]bool {
	m := map[Feature]bool{}
	for _, f := range fs {
		m[f] = true
	}
	return m
}

var base = []Feature{FeatureCosts, FeatureUsage, FeatureRecommendation, FeatureBudgets, FeatureAPI, FeatureExports}
var team = append(append([]Feature{}, base...), FeatureAllocation, FeatureAnomalies, FeatureAssistant, FeatureReports,
	FeatureSSO, FeatureForecast, FeatureUptime, FeatureUnitCosts)
var enterprise = append(append([]Feature{}, team...), FeatureSCIM, FeatureSovereignLLM, FeatureSelfHosted, FeatureWhiteLabel)
var msp = append(append([]Feature{}, enterprise...), FeatureMSP)

var catalog = map[model.Plan]Limits{
	model.PlanStarter:    {Plan: model.PlanStarter, MaxUsers: 3, MaxConnectors: 3, MaxProviders: 1, LLMTokensMonth: 0, RetentionDays: 400, Features: features(base...)},
	model.PlanTeam:       {Plan: model.PlanTeam, MaxUsers: 50, MaxConnectors: Unlimited, MaxProviders: Unlimited, LLMTokensMonth: 2_000_000, RetentionDays: 760, Features: features(team...)},
	model.PlanEnterprise: {Plan: model.PlanEnterprise, MaxUsers: Unlimited, MaxConnectors: Unlimited, MaxProviders: Unlimited, LLMTokensMonth: 10_000_000, RetentionDays: 760, Features: features(enterprise...)},
	model.PlanMSP:        {Plan: model.PlanMSP, MaxUsers: Unlimited, MaxConnectors: Unlimited, MaxProviders: Unlimited, LLMTokensMonth: 10_000_000, RetentionDays: 760, Features: features(msp...)},
}

// TrialDays est la durée d'essai (fonctionnalités du plan Team).
const TrialDays = 14

// For renvoie les limites effectives d'une organisation : pendant l'essai, un
// Starter bénéficie des fonctionnalités Team.
func For(o model.Organization, now time.Time) Limits {
	l, ok := catalog[o.Plan]
	if !ok {
		l = catalog[model.PlanStarter]
	}
	if o.Plan == model.PlanStarter && o.TrialEndsAt != nil && now.Before(*o.TrialEndsAt) {
		t := catalog[model.PlanTeam]
		t.Plan = model.PlanStarter
		return t
	}
	return l
}

// Get renvoie les limites d'un plan.
func Get(p model.Plan) (Limits, bool) {
	l, ok := catalog[p]
	return l, ok
}

// Allows indique si la fonctionnalité est incluse.
func (l Limits) Allows(f Feature) bool { return f == "" || l.Features[f] }

// Within indique si une quantité respecte une limite.
func Within(limit, value int) bool { return limit == Unlimited || value <= limit }
