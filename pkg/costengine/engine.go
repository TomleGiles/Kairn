// Package costengine calcule les lignes de coût journalières (M-03, M-04).
//
// Le moteur est déterministe et rejouable : mêmes entrées et même version de
// grille tarifaire produisent exactement les mêmes lignes. Il ne fait aucune
// entrée/sortie ; le service cost-engine charge les entrées et écrit les sorties.
//
// Aucun flottant n'est utilisé pour les montants. Les métriques d'usage
// (flottantes par nature) sont converties en décimal via leur représentation
// la plus courte avant d'entrer dans un calcul monétaire.
package costengine

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/allocation"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/money"
	"github.com/kairn-io/kairn/pkg/pricing"
)

// MetricSource fournit les métriques d'usage nécessaires au calcul.
type MetricSource interface {
	// Hourly renvoie 24 moyennes horaires pour le jour calculé (NaN = absente).
	Hourly(resourceID, metric string) []float64
	// DailyAvg renvoie la moyenne journalière si disponible.
	DailyAvg(resourceID, metric string) (float64, bool)
}

// RateFunc renvoie le taux de conversion de from vers to pour un jour.
type RateFunc func(from, to string, day time.Time) (decimal.Decimal, error)

// Input rassemble toutes les entrées du calcul d'un jour pour une organisation.
type Input struct {
	OrgID       string
	Currency    string
	Settings    model.OrgSettings
	VATRate     *decimal.Decimal
	Day         time.Time
	Resources   []model.Resource     // versions chevauchant le jour
	Edges       []model.ResourceEdge // arêtes chevauchant le jour
	Book        *pricing.Book
	OnPrem      []model.OnPremCostModel
	Adjustments []model.PricingAdjustment
	Rates       RateFunc
	Metrics     MetricSource
	Billing     []model.BillingLine // lignes de facture du jour
	Rules       []model.AllocationRule
	SharedRules []model.SharedCostRule
	// PriorCredit : montant déjà consommé de chaque crédit avant ce jour.
	PriorCredit map[string]decimal.Decimal
	// CPURAMRatio : coût relatif d'1 vCPU par rapport à 1 Go de RAM (7,5 par défaut).
	CPURAMRatio decimal.Decimal
}

// Output est le résultat du calcul.
type Output struct {
	Lines    []model.CostLine
	Warnings []string
}

var (
	hoursPerMonth = money.HoursPerMonth
	secondsPerHr  = decimal.NewFromInt(3600)
	gib           = decimal.NewFromInt(1 << 30)
	gb            = decimal.NewFromInt(1_000_000_000)
	hundred       = decimal.NewFromInt(100)
	one           = decimal.NewFromInt(1)
)

// engine porte l'état d'un calcul.
type engine struct {
	in       Input
	dayStart time.Time
	dayEnd   time.Time
	versions map[string][]model.Resource // id → versions triées
	ids      []string                    // ids triés
	parents  map[string][]model.ResourceEdge
	children map[string][]model.ResourceEdge
	labeler  *allocation.Labeler
	matcher  *allocation.Matcher
	lines    []model.CostLine
	warnings map[string]bool
}

// Compute calcule les lignes de coût d'un jour.
func Compute(in Input) (Output, error) {
	if in.OrgID == "" {
		return Output{}, fmt.Errorf("costengine: missing org")
	}
	if in.Currency == "" {
		in.Currency = string(money.EUR)
	}
	if in.Book == nil {
		in.Book = pricing.NewBook(nil)
	}
	if in.CPURAMRatio.IsZero() {
		in.CPURAMRatio = decimal.RequireFromString("7.5")
	}
	day := in.Day.UTC()
	e := &engine{
		in:       in,
		dayStart: time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC),
		versions: map[string][]model.Resource{},
		parents:  map[string][]model.ResourceEdge{},
		children: map[string][]model.ResourceEdge{},
		warnings: map[string]bool{},
	}
	e.dayEnd = e.dayStart.AddDate(0, 0, 1)

	m, err := allocation.Compile(in.Rules)
	if err != nil {
		return Output{}, err
	}
	e.matcher = m

	e.index()
	e.priceResources()
	e.allocateKubernetes()
	e.applyInvoicePreference()
	e.allocate()
	e.distributeIdle()
	if err := e.applySharedRules(); err != nil {
		return Output{}, err
	}
	e.applyAdjustments()
	e.applyTax()
	lines := e.finalize()

	warnings := make([]string, 0, len(e.warnings))
	for w := range e.warnings {
		warnings = append(warnings, w)
	}
	sort.Strings(warnings)
	return Output{Lines: lines, Warnings: warnings}, nil
}

func (e *engine) warn(format string, args ...any) { e.warnings[fmt.Sprintf(format, args...)] = true }

// index prépare les structures de parcours.
func (e *engine) index() {
	for _, r := range e.in.Resources {
		if r.Overlap(e.dayStart, e.dayEnd) <= 0 {
			continue
		}
		if _, ok := e.versions[r.ID]; !ok {
			e.ids = append(e.ids, r.ID)
		}
		e.versions[r.ID] = append(e.versions[r.ID], r)
	}
	sort.Strings(e.ids)
	for id := range e.versions {
		vs := e.versions[id]
		sort.Slice(vs, func(i, j int) bool { return vs[i].ValidFrom.Before(vs[j].ValidFrom) })
	}
	var dayEdges []model.ResourceEdge
	for _, ed := range e.in.Edges {
		if !ed.ValidFrom.Before(e.dayEnd) || (ed.ValidTo != nil && !ed.ValidTo.After(e.dayStart)) {
			continue
		}
		dayEdges = append(dayEdges, ed)
		e.parents[ed.ChildID] = append(e.parents[ed.ChildID], ed)
		e.children[ed.ParentID] = append(e.children[ed.ParentID], ed)
	}
	for _, m := range []map[string][]model.ResourceEdge{e.parents, e.children} {
		for k := range m {
			list := m[k]
			sort.Slice(list, func(i, j int) bool {
				a, b := list[i], list[j]
				if a.ParentID != b.ParentID {
					return a.ParentID < b.ParentID
				}
				if a.ChildID != b.ChildID {
					return a.ChildID < b.ChildID
				}
				if a.Relation != b.Relation {
					return a.Relation < b.Relation
				}
				return a.ValidFrom.Before(b.ValidFrom)
			})
		}
	}
	sort.SliceStable(dayEdges, func(i, j int) bool {
		a, b := dayEdges[i], dayEdges[j]
		if a.ChildID != b.ChildID {
			return a.ChildID < b.ChildID
		}
		if a.ParentID != b.ParentID {
			return a.ParentID < b.ParentID
		}
		return a.Relation < b.Relation
	})
	e.labeler = allocation.NewLabeler(e.latest, dayEdges)
}

// latest renvoie la version la plus récente d'une ressource sur le jour.
func (e *engine) latest(id string) (model.Resource, bool) {
	vs := e.versions[id]
	if len(vs) == 0 {
		return model.Resource{}, false
	}
	return vs[len(vs)-1], true
}

// hoursOf convertit une durée en heures décimales exactes (à la seconde).
func hoursOf(d time.Duration) decimal.Decimal {
	return decimal.NewFromInt(int64(d / time.Second)).Div(secondsPerHr)
}

// dec convertit une valeur de métrique en décimal de manière déterministe.
func dec(v float64) decimal.Decimal {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return decimal.Zero
	}
	return decimal.NewFromFloat(v).Round(9)
}

// convert convertit un montant vers la devise de l'organisation.
func (e *engine) convert(amount decimal.Decimal, from string) (decimal.Decimal, bool) {
	if from == "" || from == e.in.Currency {
		return amount, true
	}
	if e.in.Rates == nil {
		e.warn("no exchange rate %s→%s", from, e.in.Currency)
		return decimal.Zero, false
	}
	rate, err := e.in.Rates(from, e.in.Currency, e.dayStart)
	if err != nil {
		e.warn("no exchange rate %s→%s on %s", from, e.in.Currency, e.dayStart.Format("2006-01-02"))
		return decimal.Zero, false
	}
	return amount.Mul(rate), true
}

// baseLine construit une ligne rattachée à une version de ressource.
func (e *engine) baseLine(r model.Resource) model.CostLine {
	return model.CostLine{
		OrgID:        e.in.OrgID,
		Day:          e.dayStart,
		ResourceID:   r.ID,
		ConnectorID:  r.ConnectorID,
		Provider:     r.Provider,
		ResourceType: r.Type,
		Region:       r.Region,
		Currency:     e.in.Currency,
		Labels:       e.effectiveLabels(r.ID),
	}
}

// finalize fusionne les doublons de clé, arrondit et trie.
func (e *engine) finalize() []model.CostLine {
	type key struct {
		res, costType, sku, source, node, ref string
	}
	merged := map[key]*model.CostLine{}
	var order []key
	for _, l := range e.lines {
		k := key{l.ResourceID, l.CostType, l.SKU, l.Source, l.AllocationNodeID, l.SourceRef}
		if cur, ok := merged[k]; ok {
			cur.Amount = cur.Amount.Add(l.Amount)
			cur.Quantity = cur.Quantity.Add(l.Quantity)
			continue
		}
		cp := l
		merged[k] = &cp
		order = append(order, k)
	}
	out := make([]model.CostLine, 0, len(order))
	for _, k := range order {
		l := *merged[k]
		l.Amount = l.Amount.Round(money.StoragePlaces)
		l.Quantity = l.Quantity.Round(money.StoragePlaces)
		if l.Amount.IsZero() && l.Quantity.IsZero() {
			continue
		}
		out = append(out, l)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		for _, p := range [][2]string{
			{a.ResourceID, b.ResourceID}, {a.CostType, b.CostType}, {a.SKU, b.SKU},
			{a.Source, b.Source}, {a.AllocationNodeID, b.AllocationNodeID}, {a.SourceRef, b.SourceRef},
		} {
			if p[0] != p[1] {
				return p[0] < p[1]
			}
		}
		return false
	})
	return out
}

// Total additionne les montants de lignes.
func Total(lines []model.CostLine) decimal.Decimal {
	t := decimal.Zero
	for _, l := range lines {
		t = t.Add(l.Amount)
	}
	return t
}

// globMatch compare un SKU à un motif glob simple (* seulement).
func globMatch(pattern, s string) bool {
	if pattern == "" || pattern == "*" {
		return true
	}
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == s
	}
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	for i := 1; i < len(parts)-1; i++ {
		idx := strings.Index(s, parts[i])
		if idx < 0 {
			return false
		}
		s = s[idx+len(parts[i]):]
	}
	return strings.HasSuffix(s, parts[len(parts)-1])
}

// EstimateHourly estime le coût horaire courant d'une ressource à partir des
// grilles (simulations what-if et recommandations). Les nodes Kubernetes et
// les ressources sans prix renvoient ok=false.
func EstimateHourly(book *pricing.Book, r model.Resource, currency string, rates RateFunc, day time.Time) (decimal.Decimal, bool) {
	e := &engine{
		in:       Input{OrgID: r.OrgID, Currency: currency, Book: book, Rates: rates, Day: day},
		warnings: map[string]bool{},
	}
	if e.in.Currency == "" {
		e.in.Currency = string(money.EUR)
	}
	d := day.UTC()
	e.dayStart = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
	e.dayEnd = e.dayStart.AddDate(0, 0, 1)
	e.labeler = allocation.NewLabeler(func(string) (model.Resource, bool) { return r, true }, nil)
	cs := e.components(r)
	if len(cs) == 0 {
		return decimal.Zero, false
	}
	return hourlyRate(cs), true
}
