// Package agent déclare le connecteur « Agent Kairn » : un binaire léger
// installé sur les hôtes sans pile de métriques, qui pousse inventaire et
// métriques (OTLP) vers la passerelle d'ingestion avec le jeton du connecteur.
package agent

import (
	"context"
	"time"

	"github.com/kairn-io/kairn/pkg/connector"
	"github.com/kairn-io/kairn/pkg/model"
)

// Type est l'identifiant du connecteur.
const Type = "agent"

func init() {
	connector.Register(connector.TypeInfo{
		Type: Type, DisplayName: "Agent Kairn (hôtes on-prem / VM)", Category: connector.CategoryAgent, Provider: "onprem",
		Resources: []string{model.TypeHost}, DefaultInterval: 5 * time.Minute, Metrics: true, Webhook: true, DocsURL: "/docs/connectors/agent",
		Fields: []connector.Field{
			{Name: "site", Label: "Site / datacenter", Help: "Libellé ajouté aux hôtes (label site)"},
		},
		Permissions: []connector.Permission{
			{Scope: "sortant HTTPS vers la passerelle", Description: "L'agent pousse ses données ; aucun accès entrant n'est requis"},
			{Scope: "lecture /proc, /sys", Description: "Collecte CPU, mémoire, disque et réseau de l'hôte (utilisateur non privilégié)"},
		},
	}, func(cfg connector.Config) (connector.Connector, error) { return &Agent{cfg: cfg}, nil })
}

// Agent est un connecteur alimenté par poussée.
type Agent struct{ cfg connector.Config }

// PushOnly indique qu'aucune synchronisation pull n'est nécessaire.
func (a *Agent) PushOnly() bool { return true }

// Type implémente connector.Connector.
func (a *Agent) Type() string { return Type }

// Validate implémente connector.Connector.
func (a *Agent) Validate(context.Context, connector.Config) error { return nil }

// RequiredPermissions implémente connector.Connector.
func (a *Agent) RequiredPermissions() []connector.Permission {
	i, _ := connector.Info(Type)
	return i.Permissions
}

// SyncInventory n'est pas utilisé : l'inventaire est poussé.
func (a *Agent) SyncInventory(context.Context, time.Time) (<-chan connector.Resource, error) {
	return nil, connector.ErrNotSupported
}

// SyncMetrics n'est pas utilisé : les métriques sont poussées en OTLP.
func (a *Agent) SyncMetrics(context.Context, connector.TimeWindow) (<-chan connector.MetricPoint, error) {
	return nil, connector.ErrNotSupported
}

// SyncBilling n'est pas supporté.
func (a *Agent) SyncBilling(context.Context, connector.Period) (<-chan connector.CostLine, error) {
	return nil, connector.ErrNotSupported
}

// Health implémente connector.Connector.
func (a *Agent) Health(context.Context) connector.HealthStatus {
	return connector.HealthStatus{Status: connector.HealthOK, Message: "en attente des données de l'agent", CheckedAt: time.Now().UTC()}
}
