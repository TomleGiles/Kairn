// Package costrun charge les entrées du cost-engine depuis les dépôts,
// exécute le calcul jour par jour et écrit les lignes de coût. Il est utilisé
// par le service cost-engine et par le mode démo tout-en-un.
package costrun

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/costengine"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/pricing"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/tenancy"
	"github.com/kairn-io/kairn/pkg/tsdb"
)

// Runner exécute le cost-engine pour l'organisation du contexte.
type Runner struct {
	Store store.Store
	TSDB  tsdb.TSDB
	Log   *slog.Logger
}

// DaySummary résume le calcul d'un jour.
type DaySummary struct {
	Day      time.Time       `json:"day"`
	Lines    int             `json:"lines"`
	Total    decimal.Decimal `json:"total"`
	Currency string          `json:"currency"`
	Warnings []string        `json:"warnings"`
}

// shared regroupe les entrées communes à plusieurs jours.
type shared struct {
	org         model.Organization
	book        *pricing.Book
	onprem      []model.OnPremCostModel
	adjustments []model.PricingAdjustment
	rules       []model.AllocationRule
	sharedRules []model.SharedCostRule
}

func (r *Runner) log() *slog.Logger {
	if r.Log != nil {
		return r.Log
	}
	return slog.Default()
}

func (r *Runner) loadShared(ctx context.Context) (shared, error) {
	orgID, err := tenancy.OrgID(ctx)
	if err != nil {
		return shared{}, err
	}
	var s shared
	if s.org, err = r.Store.Orgs().Get(ctx, orgID); err != nil {
		return s, fmt.Errorf("load org: %w", err)
	}
	cats, err := r.Store.Pricing().ListCatalogs(ctx)
	if err != nil {
		return s, fmt.Errorf("load catalogs: %w", err)
	}
	var withItems []pricing.Catalog
	for _, c := range cats {
		items, err := r.Store.Pricing().Items(ctx, c.ID)
		if err != nil {
			return s, fmt.Errorf("load catalog %s: %w", c.ID, err)
		}
		withItems = append(withItems, pricing.Catalog{Catalog: c, Items: items})
	}
	s.book = pricing.NewBook(withItems)
	if s.onprem, err = store.ListAll(ctx, r.Store.OnPremModels(), func(m model.OnPremCostModel) string { return m.ID }, nil); err != nil {
		return s, err
	}
	if s.adjustments, err = store.ListAll(ctx, r.Store.Adjustments(), func(m model.PricingAdjustment) string { return m.ID }, nil); err != nil {
		return s, err
	}
	if s.rules, err = store.ListAll(ctx, r.Store.AllocationRules(), func(m model.AllocationRule) string { return m.ID }, nil); err != nil {
		return s, err
	}
	if s.sharedRules, err = store.ListAll(ctx, r.Store.SharedRules(), func(m model.SharedCostRule) string { return m.ID }, nil); err != nil {
		return s, err
	}
	return s, nil
}

// ComputeRange calcule chaque jour de [from, to) dans l'ordre (les crédits en dépendent).
func (r *Runner) ComputeRange(ctx context.Context, from, to time.Time) ([]DaySummary, error) {
	sh, err := r.loadShared(ctx)
	if err != nil {
		return nil, err
	}
	var out []DaySummary
	for day := tsdb.TruncDay(from); day.Before(to); day = day.AddDate(0, 0, 1) {
		sum, err := r.computeDay(ctx, sh, day)
		if err != nil {
			return out, fmt.Errorf("compute %s: %w", day.Format("2006-01-02"), err)
		}
		out = append(out, sum)
	}
	return out, nil
}

// ComputeDay calcule un seul jour.
func (r *Runner) ComputeDay(ctx context.Context, day time.Time) (DaySummary, error) {
	sh, err := r.loadShared(ctx)
	if err != nil {
		return DaySummary{}, err
	}
	return r.computeDay(ctx, sh, tsdb.TruncDay(day))
}

func (r *Runner) computeDay(ctx context.Context, sh shared, day time.Time) (DaySummary, error) {
	end := day.AddDate(0, 0, 1)
	resources, err := r.Store.Resources().InWindow(ctx, day, end)
	if err != nil {
		return DaySummary{}, fmt.Errorf("load resources: %w", err)
	}
	edges, err := r.Store.Resources().Edges(ctx, day, end)
	if err != nil {
		return DaySummary{}, fmt.Errorf("load edges: %w", err)
	}
	metrics, err := r.loadMetrics(ctx, day, resources)
	if err != nil {
		return DaySummary{}, err
	}
	billing, err := r.TSDB.BillingLines(ctx, "", day, end)
	if err != nil {
		return DaySummary{}, fmt.Errorf("load billing: %w", err)
	}
	prior, err := r.priorCredit(ctx, sh.adjustments, day)
	if err != nil {
		return DaySummary{}, err
	}
	in := costengine.Input{
		OrgID: sh.org.ID, Currency: sh.org.Currency, Settings: sh.org.Settings, VATRate: sh.org.VATRate, Day: day,
		Resources: resources, Edges: edges, Book: sh.book, OnPrem: sh.onprem, Adjustments: sh.adjustments,
		Rates: r.rate(ctx), Metrics: metrics, Billing: billing, Rules: sh.rules, SharedRules: sh.sharedRules,
		PriorCredit: prior,
	}
	out, err := costengine.Compute(in)
	if err != nil {
		return DaySummary{}, err
	}
	if err := r.TSDB.ReplaceCostLines(ctx, day, out.Lines); err != nil {
		return DaySummary{}, fmt.Errorf("write cost lines: %w", err)
	}
	for _, w := range out.Warnings {
		r.log().Debug("cost engine warning", "org", sh.org.ID, "day", day.Format("2006-01-02"), "warning", w)
	}
	return DaySummary{Day: day, Lines: len(out.Lines), Total: costengine.Total(out.Lines), Currency: sh.org.Currency, Warnings: out.Warnings}, nil
}

func (r *Runner) rate(ctx context.Context) costengine.RateFunc {
	return func(from, to string, day time.Time) (decimal.Decimal, error) {
		x, err := r.Store.Rates().Get(ctx, from, to, day)
		if err == nil {
			return x.Rate, nil
		}
		// Taux inverse si seul celui-ci est connu.
		inv, err2 := r.Store.Rates().Get(ctx, to, from, day)
		if err2 == nil && inv.Rate.IsPositive() {
			return decimal.NewFromInt(1).Div(inv.Rate), nil
		}
		return decimal.Zero, err
	}
}

// priorCredit calcule la consommation antérieure de chaque crédit.
func (r *Runner) priorCredit(ctx context.Context, adjs []model.PricingAdjustment, day time.Time) (map[string]decimal.Decimal, error) {
	out := map[string]decimal.Decimal{}
	for _, a := range adjs {
		if a.Kind != model.AdjustmentCredit || !a.ValidFrom.Before(day) {
			continue
		}
		rows, err := r.TSDB.QueryCosts(ctx, tsdb.CostQuery{
			From: tsdb.TruncDay(a.ValidFrom), To: day, Granularity: tsdb.GranTotal,
			Filters: map[string][]string{"cost_type": {model.CostCredit}, "source_ref": {"adjustment:" + a.ID}},
		})
		if err != nil {
			return nil, fmt.Errorf("prior credit %s: %w", a.ID, err)
		}
		used := decimal.Zero
		for _, row := range rows {
			used = used.Sub(row.Amount) // lignes de crédit négatives
		}
		out[a.ID] = used
	}
	return out, nil
}

// hourlyMetrics implémente costengine.MetricSource.
type hourlyMetrics struct {
	day  time.Time
	data map[string]map[string][]float64
}

func (h hourlyMetrics) Hourly(id, metric string) []float64 {
	if v, ok := h.data[id][metric]; ok {
		return v
	}
	return nil
}

func (h hourlyMetrics) DailyAvg(id, metric string) (float64, bool) {
	v, ok := h.data[id][metric]
	if !ok {
		return 0, false
	}
	sum, n := 0.0, 0
	for _, x := range v {
		if !math.IsNaN(x) {
			sum += x
			n++
		}
	}
	if n == 0 {
		return 0, false
	}
	return sum / float64(n), true
}

// loadMetrics charge les moyennes horaires utiles au calcul (pods et buckets).
func (r *Runner) loadMetrics(ctx context.Context, day time.Time, resources []model.Resource) (hourlyMetrics, error) {
	h := hourlyMetrics{day: day, data: map[string]map[string][]float64{}}
	var pods, buckets []string
	seen := map[string]bool{}
	for _, res := range resources {
		if seen[res.ID] {
			continue
		}
		seen[res.ID] = true
		switch res.Type {
		case model.TypeK8sPod:
			pods = append(pods, res.ID)
		case model.TypeBucket:
			buckets = append(buckets, res.ID)
		}
	}
	load := func(idsList []string, metrics []string) error {
		const batch = 500
		sort.Strings(idsList)
		for i := 0; i < len(idsList); i += batch {
			j := i + batch
			if j > len(idsList) {
				j = len(idsList)
			}
			series, err := r.TSDB.QueryMetrics(ctx, tsdb.MetricQuery{
				ResourceIDs: idsList[i:j], Metrics: metrics, From: day, To: day.AddDate(0, 0, 1), Step: time.Hour, Agg: tsdb.AggAvg,
			})
			if err != nil {
				return fmt.Errorf("load metrics: %w", err)
			}
			for _, s := range series {
				vals := make([]float64, 24)
				for k := range vals {
					vals[k] = math.NaN()
				}
				for _, p := range s.Points {
					if idx := int(p.TS.Sub(day) / time.Hour); idx >= 0 && idx < 24 {
						vals[idx] = p.Value
					}
				}
				if h.data[s.ResourceID] == nil {
					h.data[s.ResourceID] = map[string][]float64{}
				}
				h.data[s.ResourceID][s.Metric] = vals
			}
		}
		return nil
	}
	if err := load(pods, []string{model.MetricCPUUsageCores, model.MetricMemUsageBytes, model.MetricCPURequestCores, model.MetricMemRequestBytes}); err != nil {
		return h, err
	}
	if err := load(buckets, []string{model.MetricStorageBytes}); err != nil {
		return h, err
	}
	return h, nil
}

// Reconcile compare, pour un mois, l'estimé (hors facture) et le facturé de
// chaque connecteur, sur les seuls jours disposant à la fois d'une estimation
// et d'une facture (un mois en cours n'est comparé que sur ses jours connus).
func (r *Runner) Reconcile(ctx context.Context, month time.Time) ([]model.Reconciliation, error) {
	from := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, 0)
	orgID, err := tenancy.OrgID(ctx)
	if err != nil {
		return nil, err
	}
	org, err := r.Store.Orgs().Get(ctx, orgID)
	if err != nil {
		return nil, err
	}
	bills, err := r.TSDB.BillingLines(ctx, "", from, to)
	if err != nil {
		return nil, err
	}
	type key struct {
		conn string
		day  time.Time
	}
	billed := map[key]decimal.Decimal{}
	provider := map[string]string{}
	for _, b := range bills {
		amt := b.Amount
		if b.Currency != org.Currency {
			rate, err := r.rate(ctx)(b.Currency, org.Currency, b.Day)
			if err != nil {
				continue
			}
			amt = amt.Mul(rate)
		}
		k := key{b.ConnectorID, tsdb.TruncDay(b.Day)}
		billed[k] = billed[k].Add(amt)
		provider[b.ConnectorID] = b.Provider
	}
	rows, err := r.TSDB.QueryCosts(ctx, tsdb.CostQuery{
		From: from, To: to, Granularity: tsdb.GranDay, GroupBy: []string{"connector_id"},
		Filters: map[string][]string{"source": {model.SourceEstimate, model.SourceOnPrem}},
	})
	if err != nil {
		return nil, err
	}
	estimated := map[key]decimal.Decimal{}
	for _, row := range rows {
		k := key{row.Keys["connector_id"], row.Period}
		estimated[k] = estimated[k].Add(row.Amount)
	}
	totals := map[string][2]decimal.Decimal{}
	for k, b := range billed {
		e, ok := estimated[k]
		if !ok {
			continue
		}
		t := totals[k.conn]
		t[0], t[1] = t[0].Add(e), t[1].Add(b)
		totals[k.conn] = t
	}
	conns := make([]string, 0, len(totals))
	for c := range totals {
		conns = append(conns, c)
	}
	sort.Strings(conns)
	var out []model.Reconciliation
	for _, c := range conns {
		rec := model.Reconciliation{
			OrgID: orgID, ConnectorID: c, Provider: provider[c], Month: from,
			Estimated: totals[c][0].Round(6), Billed: totals[c][1].Round(6), Currency: org.Currency, ComputedAt: time.Now().UTC(),
		}
		if err := r.Store.Reconciliations().Upsert(ctx, &rec); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}
