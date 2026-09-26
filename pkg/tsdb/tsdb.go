// Package tsdb définit l'accès aux séries temporelles, lignes de coût et
// événements (ClickHouse en production, mémoire pour tests et démo).
// Comme pour store, l'organisation est toujours lue depuis le contexte.
package tsdb

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/model"
)

// Agrégations supportées.
const (
	AggAvg  = "avg"
	AggMax  = "max"
	AggMin  = "min"
	AggP95  = "p95"
	AggSum  = "sum"
	AggLast = "last"
)

// MetricQuery interroge des séries de métriques.
type MetricQuery struct {
	ResourceIDs []string
	Metrics     []string
	From, To    time.Time
	Step        time.Duration // 0 = résolution native
	Agg         string
}

// Point est un point de série.
type Point struct {
	TS    time.Time `json:"ts"`
	Value float64   `json:"value"`
}

// Series est une série pour une ressource et une métrique.
type Series struct {
	ResourceID string  `json:"resource_id"`
	Metric     string  `json:"metric"`
	Points     []Point `json:"points"`
}

// Granularités de requête de coût.
const (
	GranDay   = "day"
	GranWeek  = "week"
	GranMonth = "month"
	GranTotal = "total"
)

// Dimensions de regroupement des coûts. "label:<clé>" regroupe par label.
var CostDims = []string{"provider", "resource_type", "region", "cost_type", "allocation_node_id", "resource_id", "connector_id", "source", "sku", "source_ref", "catalog_version"}

// CostQuery interroge les lignes de coût journalières sur [From, To).
type CostQuery struct {
	From, To    time.Time
	Granularity string
	GroupBy     []string
	Filters     map[string][]string
	// NodeIDs restreint aux nœuds d'allocation autorisés (scopes RBAC) ; nil = pas de restriction.
	NodeIDs []string
	Limit   int
}

// Validate vérifie les dimensions et la fenêtre.
func (q CostQuery) Validate() error {
	if !q.To.After(q.From) {
		return fmt.Errorf("tsdb: invalid window %s → %s", q.From, q.To)
	}
	switch q.Granularity {
	case "", GranDay, GranWeek, GranMonth, GranTotal:
	default:
		return fmt.Errorf("tsdb: unknown granularity %q", q.Granularity)
	}
	for _, d := range q.GroupBy {
		if !ValidDim(d) {
			return fmt.Errorf("tsdb: unknown dimension %q", d)
		}
	}
	for d := range q.Filters {
		if !ValidDim(d) {
			return fmt.Errorf("tsdb: unknown filter dimension %q", d)
		}
	}
	return nil
}

// ValidDim indique si d est une dimension de coût valide.
func ValidDim(d string) bool {
	if strings.HasPrefix(d, "label:") {
		k := strings.TrimPrefix(d, "label:")
		return k != "" && labelKeyOK(k)
	}
	for _, x := range CostDims {
		if x == d {
			return true
		}
	}
	return false
}

func labelKeyOK(k string) bool {
	for _, r := range k {
		if !(r == '.' || r == '-' || r == '_' || r == '/' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return len(k) <= 128
}

// CostRow est une ligne agrégée.
type CostRow struct {
	Period   time.Time         `json:"period"`
	Keys     map[string]string `json:"keys"`
	Amount   decimal.Decimal   `json:"amount"`
	Currency string            `json:"currency"`
}

// EventQuery interroge les événements.
type EventQuery struct {
	From, To    time.Time
	Kinds       []string
	ResourceIDs []string
	Limit       int
}

// UptimeQuery interroge les résultats de sondes.
type UptimeQuery struct {
	CheckIDs []string
	From, To time.Time
	Region   string
	Limit    int
}

// TSDB est l'interface des données volumineuses.
type TSDB interface {
	WriteMetrics(ctx context.Context, pts []model.MetricPoint) error
	QueryMetrics(ctx context.Context, q MetricQuery) ([]Series, error)
	// Rollup calcule les agrégats 5 min et 1 h des heures terminées de [from, to).
	Rollup(ctx context.Context, from, to time.Time) error

	// ReplaceCostLines remplace toutes les lignes calculées de l'organisation pour le jour donné.
	ReplaceCostLines(ctx context.Context, day time.Time, lines []model.CostLine) error
	QueryCosts(ctx context.Context, q CostQuery) ([]CostRow, error)
	CostLines(ctx context.Context, q CostQuery) ([]model.CostLine, error)

	ReplaceBillingLines(ctx context.Context, connectorID string, from, to time.Time, lines []model.BillingLine) error
	BillingLines(ctx context.Context, connectorID string, from, to time.Time) ([]model.BillingLine, error)

	WriteEvents(ctx context.Context, evs []model.Event) error
	QueryEvents(ctx context.Context, q EventQuery) ([]model.Event, error)

	WriteUptime(ctx context.Context, rs []model.UptimeResult) error
	QueryUptime(ctx context.Context, q UptimeQuery) ([]model.UptimeResult, error)

	WriteUnitValues(ctx context.Context, metricID string, values map[time.Time]decimal.Decimal) error
	UnitValues(ctx context.Context, metricID string, from, to time.Time) (map[time.Time]decimal.Decimal, error)

	// PurgeOrg supprime toutes les données de l'organisation courante (droit à l'effacement).
	PurgeOrg(ctx context.Context) error
}

// TruncDay tronque à minuit UTC.
func TruncDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// PeriodStart renvoie le début de la période contenant day selon la granularité (semaine ISO, lundi).
func PeriodStart(day time.Time, gran string) time.Time {
	d := TruncDay(day)
	switch gran {
	case GranWeek:
		wd := int(d.Weekday())
		if wd == 0 {
			wd = 7
		}
		return d.AddDate(0, 0, -(wd - 1))
	case GranMonth:
		return time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC)
	case GranTotal:
		return time.Time{}
	default:
		return d
	}
}

// Percentile calcule un percentile (0-100) par interpolation linéaire.
func Percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	s := append([]float64(nil), values...)
	sort.Float64s(s)
	if p <= 0 {
		return s[0]
	}
	if p >= 100 {
		return s[len(s)-1]
	}
	rank := p / 100 * float64(len(s)-1)
	lo := int(rank)
	frac := rank - float64(lo)
	if lo+1 >= len(s) {
		return s[lo]
	}
	return s[lo] + frac*(s[lo+1]-s[lo])
}

// Aggregate applique une agrégation à des valeurs.
func Aggregate(values []float64, agg string) float64 {
	if len(values) == 0 {
		return 0
	}
	switch agg {
	case AggMax:
		m := values[0]
		for _, v := range values[1:] {
			if v > m {
				m = v
			}
		}
		return m
	case AggMin:
		m := values[0]
		for _, v := range values[1:] {
			if v < m {
				m = v
			}
		}
		return m
	case AggP95:
		return Percentile(values, 95)
	case AggSum:
		s := 0.0
		for _, v := range values {
			s += v
		}
		return s
	case AggLast:
		return values[len(values)-1]
	default:
		s := 0.0
		for _, v := range values {
			s += v
		}
		return s / float64(len(values))
	}
}

// DimValue extrait la valeur d'une dimension d'une ligne de coût.
func DimValue(l model.CostLine, dim string) string {
	switch dim {
	case "provider":
		return l.Provider
	case "resource_type":
		return l.ResourceType
	case "region":
		return l.Region
	case "cost_type":
		return l.CostType
	case "allocation_node_id":
		return l.AllocationNodeID
	case "resource_id":
		return l.ResourceID
	case "connector_id":
		return l.ConnectorID
	case "source":
		return l.Source
	case "sku":
		return l.SKU
	case "source_ref":
		return l.SourceRef
	case "catalog_version":
		return l.CatalogVersion
	}
	if strings.HasPrefix(dim, "label:") {
		return l.Labels[strings.TrimPrefix(dim, "label:")]
	}
	return ""
}
