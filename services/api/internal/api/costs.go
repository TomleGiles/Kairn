package api

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/allocation"
	"github.com/kairn-io/kairn/pkg/auth"
	"github.com/kairn-io/kairn/pkg/budget"
	"github.com/kairn-io/kairn/pkg/export"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/plans"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/tsdb"
)

// costsIn sont les paramètres communs des requêtes de coût.
type costsIn struct {
	OrgPath
	From        time.Time `query:"from" doc:"Début inclus (défaut : J-30)"`
	To          time.Time `query:"to" doc:"Fin exclue (défaut : demain)"`
	Granularity string    `query:"granularity" enum:"day,week,month,total" default:"day"`
	GroupBy     []string  `query:"group_by,explode" doc:"Dimensions : provider, resource_type, region, cost_type, allocation_node_id, resource_id, connector_id, source, sku, label:<clé>"`
	Filter      []string  `query:"filter,explode" doc:"Filtres dimension:valeur (répétables)"`
	NodeID      string    `query:"node_id" doc:"Restreint au sous-arbre d'un nœud d'allocation"`
	Limit       int       `query:"limit" minimum:"0" maximum:"100000"`
}

// costQuery construit la requête tsdb en appliquant les scopes RBAC.
func (s *Server) costQuery(ctx context.Context, a access, in costsIn) (tsdb.CostQuery, error) {
	q := tsdb.CostQuery{From: in.From, To: in.To, Granularity: in.Granularity, Filters: map[string][]string{}, Limit: in.Limit}
	if q.To.IsZero() {
		q.To = tsdb.TruncDay(s.now()).AddDate(0, 0, 1)
	}
	if q.From.IsZero() {
		q.From = q.To.AddDate(0, 0, -30)
	}
	if q.Granularity == "" {
		q.Granularity = tsdb.GranDay
	}
	if q.To.Sub(q.From) > 800*24*time.Hour {
		return q, invalid("window too large (max 800 days)")
	}
	for _, g := range in.GroupBy {
		for _, part := range strings.Split(g, ",") {
			if part = strings.TrimSpace(part); part != "" {
				q.GroupBy = append(q.GroupBy, part)
			}
		}
	}
	for _, f := range in.Filter {
		dim, val, ok := strings.Cut(f, ":")
		if !ok {
			return q, invalid("filter must be dimension:value")
		}
		// « label:team:shop » → dimension label:team
		if dim == "label" {
			k, v, ok := strings.Cut(val, ":")
			if !ok {
				return q, invalid("label filter must be label:key:value")
			}
			dim, val = "label:"+k, v
		}
		q.Filters[dim] = append(q.Filters[dim], val)
	}
	allowed, err := s.allowedNodes(ctx, a)
	if err != nil {
		return q, err
	}
	if in.NodeID != "" {
		nodes, err := store.ListAll(ctx, s.Store.AllocationNodes(), func(n model.AllocationNode) string { return n.ID }, nil)
		if err != nil {
			return q, err
		}
		sub := allocation.NewTree(nodes).Subtree(in.NodeID)
		if allowed != nil {
			sub = intersect(sub, allowed)
		}
		q.NodeIDs = sub
	} else if allowed != nil {
		q.NodeIDs = allowed
	}
	if err := q.Validate(); err != nil {
		return q, invalid(err.Error())
	}
	return q, nil
}

func intersect(a, b []string) []string {
	set := map[string]bool{}
	for _, x := range b {
		set[x] = true
	}
	out := []string{}
	for _, x := range a {
		if set[x] {
			out = append(out, x)
		}
	}
	return out
}

type costsOut struct {
	Currency    string          `json:"currency"`
	From        time.Time       `json:"from"`
	To          time.Time       `json:"to"`
	Granularity string          `json:"granularity"`
	GroupBy     []string        `json:"group_by"`
	Total       decimal.Decimal `json:"total"`
	Rows        []tsdb.CostRow  `json:"rows"`
}

type summaryOut struct {
	Currency            string          `json:"currency"`
	MonthToDate         decimal.Decimal `json:"month_to_date"`
	PreviousMonthToDate decimal.Decimal `json:"previous_month_to_date"`
	PreviousMonthTotal  decimal.Decimal `json:"previous_month_total"`
	ChangePercent       decimal.Decimal `json:"change_percent"`
	ForecastMonthEnd    decimal.Decimal `json:"forecast_month_end"`
	ForecastLower       decimal.Decimal `json:"forecast_lower"`
	ForecastUpper       decimal.Decimal `json:"forecast_upper"`
	ForecastModel       string          `json:"forecast_model"`
	Daily               []tsdb.CostRow  `json:"daily"`
	ByProvider          []tsdb.CostRow  `json:"by_provider"`
	ByNode              []tsdb.CostRow  `json:"by_node"`
	ByCostType          []tsdb.CostRow  `json:"by_cost_type"`
	TopMovers           []mover         `json:"top_movers"`
	AllocationCoverage  decimal.Decimal `json:"allocation_coverage_percent"`
	PotentialSavings    decimal.Decimal `json:"potential_savings_monthly"`
	RealizedSavings     decimal.Decimal `json:"realized_savings_monthly"`
	OpenRecommendations int             `json:"open_recommendations"`
	OpenAnomalies       int             `json:"open_anomalies"`
	ResourceCount       int             `json:"resource_count"`
}

type mover struct {
	ResourceID string          `json:"resource_id"`
	Name       string          `json:"name"`
	Type       string          `json:"type"`
	Previous   decimal.Decimal `json:"previous_7d"`
	Current    decimal.Decimal `json:"current_7d"`
	Delta      decimal.Decimal `json:"delta"`
}

func sumRows(rows []tsdb.CostRow) decimal.Decimal {
	t := decimal.Zero
	for _, r := range rows {
		t = t.Add(r.Amount)
	}
	return t
}

func pct(cur, prev decimal.Decimal) decimal.Decimal {
	if prev.IsZero() {
		return decimal.Zero
	}
	return cur.Sub(prev).Div(prev).Mul(decimal.NewFromInt(100)).Round(1)
}

func (s *Server) registerCosts() {
	tag := "Coûts"
	huma.Register(s.API, huma.Operation{OperationID: "query-costs", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/costs", Tags: []string{tag},
		Summary: "Explorateur de coûts : agrégation par dimensions et granularité"},
		func(ctx context.Context, in *costsIn) (*Out[costsOut], error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermCostsRead, plans.FeatureCosts)
			if err != nil {
				return nil, err
			}
			q, err := s.costQuery(ctx, a, *in)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			rows, err := s.TSDB.QueryCosts(ctx, q)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			for i := range rows {
				rows[i].Amount = rows[i].Amount.Round(6)
			}
			if rows == nil {
				rows = []tsdb.CostRow{}
			}
			gb := q.GroupBy
			if gb == nil {
				gb = []string{}
			}
			return out(costsOut{Currency: a.Org.Currency, From: q.From, To: q.To, Granularity: q.Granularity, GroupBy: gb, Total: sumRows(rows).Round(6), Rows: rows}), nil
		})

	huma.Register(s.API, huma.Operation{OperationID: "get-cost-summary", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/costs/summary", Tags: []string{tag},
		Summary: "Vue d'ensemble : mois en cours, tendance, prévision, principales variations"},
		func(ctx context.Context, in *OrgPath) (*Out[summaryOut], error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermCostsRead, plans.FeatureCosts)
			if err != nil {
				return nil, err
			}
			sum, err := s.summary(ctx, a)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			return out(sum), nil
		})

	huma.Register(s.API, huma.Operation{OperationID: "list-cost-lines", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/costs/lines", Tags: []string{tag},
		Summary: "Lignes de coût brutes : traçabilité jusqu'à la source (grille, facture, règle)"},
		func(ctx context.Context, in *costsIn) (*Out[[]model.CostLine], error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermCostsRead, plans.FeatureCosts)
			if err != nil {
				return nil, err
			}
			q, err := s.costQuery(ctx, a, *in)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			if q.Limit == 0 || q.Limit > 5000 {
				q.Limit = 5000
			}
			lines, err := s.TSDB.CostLines(ctx, q)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			if lines == nil {
				lines = []model.CostLine{}
			}
			return out(lines), nil
		})

	// Champs déclarés à plat : huma ne lie pas les paramètres d'une structure
	// imbriquée sur deux niveaux (exportIn → costsIn → OrgPath).
	type exportIn struct {
		OrgPath
		From    time.Time `query:"from" doc:"Début inclus (défaut : J-30)"`
		To      time.Time `query:"to" doc:"Fin exclue (défaut : demain)"`
		GroupBy []string  `query:"group_by,explode"`
		Filter  []string  `query:"filter,explode"`
		NodeID  string    `query:"node_id"`
		Format  string    `query:"format" enum:"csv,parquet" default:"csv"`
	}
	type fileOut struct {
		ContentType        string `header:"Content-Type"`
		ContentDisposition string `header:"Content-Disposition"`
		Body               []byte
	}
	huma.Register(s.API, huma.Operation{OperationID: "export-costs", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/costs/export", Tags: []string{tag},
		Summary: "Export des lignes de coût (CSV ou Parquet)"},
		func(ctx context.Context, in *exportIn) (*fileOut, error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermExport, plans.FeatureExports)
			if err != nil {
				return nil, err
			}
			q, err := s.costQuery(ctx, a, costsIn{OrgPath: in.OrgPath, From: in.From, To: in.To, Granularity: tsdb.GranDay,
				GroupBy: in.GroupBy, Filter: in.Filter, NodeID: in.NodeID})
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			q.Limit = 1_000_000
			lines, err := s.TSDB.CostLines(ctx, q)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			data, ctype, err := export.EncodeCostLines(lines, in.Format)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			s.audit(ctx, a, "costs.export", "costs", "", map[string]any{"rows": len(lines), "format": in.Format})
			name := fmt.Sprintf("kairn-costs-%s-%s.%s", q.From.Format("20060102"), q.To.Format("20060102"), in.Format)
			return &fileOut{ContentType: ctype, ContentDisposition: `attachment; filename="` + name + `"`, Body: data}, nil
		})

	type recomputeIn struct {
		OrgPath
		Body struct {
			From time.Time `json:"from" required:"true"`
			To   time.Time `json:"to" required:"true"`
		}
	}
	huma.Register(s.API, huma.Operation{OperationID: "recompute-costs", Method: http.MethodPost, Path: Prefix + "/orgs/{org_id}/costs/recompute", Tags: []string{tag},
		Summary: "Recalcule les coûts d'une période (déterministe et rejouable)", DefaultStatus: http.StatusAccepted},
		func(ctx context.Context, in *recomputeIn) (*Empty, error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermPricingManage, "")
			if err != nil {
				return nil, err
			}
			if !in.Body.To.After(in.Body.From) || in.Body.To.Sub(in.Body.From) > 400*24*time.Hour {
				return nil, invalid("invalid period (max 400 days)")
			}
			if err := s.Jobs.RequestRecompute(ctx, a.Org.ID, in.Body.From, in.Body.To); err != nil {
				return nil, s.fail(ctx, err)
			}
			s.audit(ctx, a, "costs.recompute", "costs", "", map[string]any{"from": in.Body.From, "to": in.Body.To})
			return &Empty{}, nil
		})

	type reconView struct {
		model.Reconciliation
		DeltaPercent decimal.Decimal `json:"delta_percent"`
	}
	huma.Register(s.API, huma.Operation{OperationID: "list-reconciliations", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/reconciliation", Tags: []string{tag},
		Summary: "Rapprochement estimé vs facturé par connecteur et par mois"},
		func(ctx context.Context, in *OrgPath) (*Out[[]reconView], error) {
			ctx, _, err := s.enter(ctx, in.OrgID, auth.PermCostsRead, "")
			if err != nil {
				return nil, err
			}
			recs, err := s.Store.Reconciliations().List(ctx)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			views := make([]reconView, 0, len(recs))
			for _, r := range recs {
				views = append(views, reconView{Reconciliation: r, DeltaPercent: r.DeltaPercent()})
			}
			return out(views), nil
		})

	s.registerUsage()
}

func (s *Server) summary(ctx context.Context, a access) (summaryOut, error) {
	now := s.now()
	today := tsdb.TruncDay(now)
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	prevStart := monthStart.AddDate(0, -1, 0)
	elapsed := today.AddDate(0, 0, 1).Sub(monthStart)
	allowed, err := s.allowedNodes(ctx, a)
	if err != nil {
		return summaryOut{}, err
	}
	query := func(from, to time.Time, gran string, groupBy ...string) ([]tsdb.CostRow, error) {
		return s.TSDB.QueryCosts(ctx, tsdb.CostQuery{From: from, To: to, Granularity: gran, GroupBy: groupBy, NodeIDs: allowed})
	}
	out := summaryOut{Currency: a.Org.Currency}
	mtd, err := query(monthStart, today.AddDate(0, 0, 1), tsdb.GranDay)
	if err != nil {
		return out, err
	}
	out.MonthToDate = sumRows(mtd).Round(2)
	prevSame, err := query(prevStart, prevStart.Add(elapsed), tsdb.GranTotal)
	if err != nil {
		return out, err
	}
	out.PreviousMonthToDate = sumRows(prevSame).Round(2)
	prevAll, err := query(prevStart, monthStart, tsdb.GranTotal)
	if err != nil {
		return out, err
	}
	out.PreviousMonthTotal = sumRows(prevAll).Round(2)
	out.ChangePercent = pct(out.MonthToDate, out.PreviousMonthToDate)

	// Prévision : modèle du service analytics si disponible, sinon projection linéaire.
	if f, err := s.Store.Forecasts().Get(ctx, ""); err == nil && f.PeriodEnd.Equal(monthStart.AddDate(0, 1, 0)) && allowed == nil {
		out.ForecastMonthEnd, out.ForecastLower, out.ForecastUpper, out.ForecastModel = f.Total.Round(2), f.Lower.Round(2), f.Upper.Round(2), f.Model
	} else {
		var daily []decimal.Decimal
		byDay := map[time.Time]decimal.Decimal{}
		for _, r := range mtd {
			byDay[r.Period] = byDay[r.Period].Add(r.Amount)
		}
		complete := decimal.Zero
		for d := monthStart; d.Before(today); d = d.AddDate(0, 0, 1) {
			daily = append(daily, byDay[d])
			complete = complete.Add(byDay[d])
		}
		fc := budget.Forecast(complete, daily, today, monthStart.AddDate(0, 1, 0))
		if fc.LessThan(out.MonthToDate) {
			fc = out.MonthToDate
		}
		out.ForecastMonthEnd, out.ForecastLower, out.ForecastUpper, out.ForecastModel = fc.Round(2), fc.Mul(decimal.RequireFromString("0.95")).Round(2), fc.Mul(decimal.RequireFromString("1.05")).Round(2), "linear-7d"
	}
	if out.Daily, err = query(today.AddDate(0, 0, -29), today.AddDate(0, 0, 1), tsdb.GranDay); err != nil {
		return out, err
	}
	if out.ByProvider, err = query(monthStart, today.AddDate(0, 0, 1), tsdb.GranTotal, "provider"); err != nil {
		return out, err
	}
	if out.ByNode, err = query(monthStart, today.AddDate(0, 0, 1), tsdb.GranTotal, "allocation_node_id"); err != nil {
		return out, err
	}
	if out.ByCostType, err = query(monthStart, today.AddDate(0, 0, 1), tsdb.GranTotal, "cost_type"); err != nil {
		return out, err
	}
	allocated := decimal.Zero
	for _, r := range out.ByNode {
		if r.Keys["allocation_node_id"] != model.UnallocatedNodeID {
			allocated = allocated.Add(r.Amount)
		}
	}
	if total := sumRows(out.ByNode); total.IsPositive() {
		out.AllocationCoverage = allocated.Div(total).Mul(decimal.NewFromInt(100)).Round(1)
	}
	// Principales variations : 7 derniers jours complets vs 7 précédents.
	cur, err := query(today.AddDate(0, 0, -7), today, tsdb.GranTotal, "resource_id")
	if err != nil {
		return out, err
	}
	prev, err := query(today.AddDate(0, 0, -14), today.AddDate(0, 0, -7), tsdb.GranTotal, "resource_id")
	if err != nil {
		return out, err
	}
	moves := map[string]*mover{}
	for _, r := range prev {
		id := r.Keys["resource_id"]
		moves[id] = &mover{ResourceID: id, Previous: r.Amount}
	}
	for _, r := range cur {
		id := r.Keys["resource_id"]
		if moves[id] == nil {
			moves[id] = &mover{ResourceID: id}
		}
		moves[id].Current = moves[id].Current.Add(r.Amount)
	}
	var list []mover
	var idsList []string
	for id, m := range moves {
		if id == "" {
			continue
		}
		m.Delta = m.Current.Sub(m.Previous)
		m.Previous, m.Current, m.Delta = m.Previous.Round(2), m.Current.Round(2), m.Delta.Round(2)
		list = append(list, *m)
	}
	sort.Slice(list, func(i, j int) bool {
		if c := list[i].Delta.Abs().Cmp(list[j].Delta.Abs()); c != 0 {
			return c > 0
		}
		return list[i].ResourceID < list[j].ResourceID
	})
	if len(list) > 8 {
		list = list[:8]
	}
	for _, m := range list {
		idsList = append(idsList, m.ResourceID)
	}
	if len(idsList) > 0 {
		rs, err := s.Store.Resources().Current(ctx, store.ResourceFilter{IDs: idsList})
		if err == nil {
			names := map[string]model.Resource{}
			for _, r := range rs {
				names[r.ID] = r
			}
			for i := range list {
				r, ok := names[list[i].ResourceID]
				if !ok {
					// Ressource disparue (ex. pod d'un réplica HPA) : dernière version connue.
					if last, err := s.Store.Resources().Get(ctx, list[i].ResourceID); err == nil {
						r, ok = last, true
					}
				}
				if ok {
					list[i].Name, list[i].Type = r.Name, r.Type
				}
			}
		}
	}
	if list == nil {
		list = []mover{}
	}
	out.TopMovers = list
	recos, err := s.Store.Recommendations().List(ctx, store.RecommendationFilter{Status: []string{model.RecoOpen, model.RecoAccepted, model.RecoApplied}, Limit: store.MaxLimit})
	if err != nil {
		return out, err
	}
	for _, r := range recos {
		switch r.Status {
		case model.RecoOpen:
			out.OpenRecommendations++
			out.PotentialSavings = out.PotentialSavings.Add(r.SavingsMonthly)
		case model.RecoApplied:
			if r.MeasuredSavingsMonthly != nil {
				out.RealizedSavings = out.RealizedSavings.Add(*r.MeasuredSavingsMonthly)
			} else {
				out.RealizedSavings = out.RealizedSavings.Add(r.SavingsMonthly)
			}
		}
	}
	out.PotentialSavings, out.RealizedSavings = out.PotentialSavings.Round(2), out.RealizedSavings.Round(2)
	anoms, err := s.Store.Anomalies().List(ctx, today.AddDate(0, 0, -30), time.Time{}, "open")
	if err != nil {
		return out, err
	}
	out.OpenAnomalies = len(anoms)
	if out.ResourceCount, err = s.Store.Resources().Count(ctx); err != nil {
		return out, err
	}
	for i := range out.Daily {
		out.Daily[i].Amount = out.Daily[i].Amount.Round(2)
	}
	// Les répartitions n'affichent pas les lignes nulles (ex. nodes K8s dont le coût est porté par la VM sous-jacente).
	out.ByProvider, out.ByNode, out.ByCostType = nonZeroRounded(out.ByProvider), nonZeroRounded(out.ByNode), nonZeroRounded(out.ByCostType)
	return out, nil
}

// ------------------------------------------------------------------ usage et efficience (M-05)

type usageIn struct {
	OrgPath
	ResourceID []string  `query:"resource_id,explode" required:"true"`
	Metric     []string  `query:"metric,explode"`
	From       time.Time `query:"from"`
	To         time.Time `query:"to"`
	Step       string    `query:"step" doc:"Pas (ex. 5m, 1h, 1d)" default:"1h"`
	Agg        string    `query:"agg" enum:"avg,max,min,p95,sum,last" default:"avg"`
}

type efficiencyRow struct {
	Key           string          `json:"key"`
	Name          string          `json:"name"`
	Kind          string          `json:"kind"`
	Cost30d       decimal.Decimal `json:"cost_30d"`
	CPUAvg        float64         `json:"cpu_avg"`
	CPUP95        float64         `json:"cpu_p95"`
	MemAvg        float64         `json:"mem_avg"`
	RequestedCPU  float64         `json:"requested_cpu,omitempty"`
	UsedCPU       float64         `json:"used_cpu,omitempty"`
	Efficiency    float64         `json:"efficiency" doc:"Part utilisée de la capacité payée (0..1)"`
	WasteEstimate decimal.Decimal `json:"waste_estimate_30d"`
	Currency      string          `json:"currency"`
}

func round3(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return math.Round(v*1000) / 1000
}

func (s *Server) registerUsage() {
	tag := "Utilisation"
	huma.Register(s.API, huma.Operation{OperationID: "query-usage", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/usage", Tags: []string{tag},
		Summary: "Séries d'utilisation normalisées (CPU, RAM, disque, réseau, IOPS, requêtes/s)"},
		func(ctx context.Context, in *usageIn) (*Out[[]tsdb.Series], error) {
			ctx, _, err := s.enter(ctx, in.OrgID, auth.PermCostsRead, plans.FeatureUsage)
			if err != nil {
				return nil, err
			}
			step, err := time.ParseDuration(strings.Replace(in.Step, "d", "h", 1))
			if strings.HasSuffix(in.Step, "d") && err == nil {
				step *= 24
			}
			if err != nil || step < time.Minute {
				return nil, invalid("invalid step")
			}
			to := in.To
			if to.IsZero() {
				to = s.now()
			}
			from := in.From
			if from.IsZero() {
				from = to.AddDate(0, 0, -7)
			}
			if len(in.ResourceID) > 200 {
				return nil, invalid("too many resources (max 200)")
			}
			series, err := s.TSDB.QueryMetrics(ctx, tsdb.MetricQuery{ResourceIDs: in.ResourceID, Metrics: in.Metric, From: from, To: to, Step: step, Agg: in.Agg})
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			if series == nil {
				series = []tsdb.Series{}
			}
			return out(series), nil
		})

	type effIn struct {
		OrgPath
		Level string `query:"level" enum:"resource,workload,node" default:"resource"`
	}
	huma.Register(s.API, huma.Operation{OperationID: "get-efficiency", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/efficiency", Tags: []string{tag},
		Summary: "Coût × utilisation sur 30 jours (VM, workloads Kubernetes, nœuds d'allocation)"},
		func(ctx context.Context, in *effIn) (*Out[[]efficiencyRow], error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermCostsRead, plans.FeatureUsage)
			if err != nil {
				return nil, err
			}
			rows, err := s.efficiency(ctx, a, in.Level)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			return out(rows), nil
		})
}

// efficiency calcule l'efficience coût × usage au niveau demandé.
func (s *Server) efficiency(ctx context.Context, a access, level string) ([]efficiencyRow, error) {
	to := tsdb.TruncDay(s.now())
	from := to.AddDate(0, 0, -30)
	var out []efficiencyRow
	switch level {
	case "workload":
		lines, err := s.TSDB.CostLines(ctx, tsdb.CostQuery{From: from, To: to, Granularity: tsdb.GranTotal,
			Filters: map[string][]string{"cost_type": {model.CostK8sWork}}, Limit: 2_000_000})
		if err != nil {
			return nil, err
		}
		type agg struct {
			cost decimal.Decimal
			pods map[string]bool
			ns   string
		}
		byWL := map[string]*agg{}
		for _, l := range lines {
			wl := l.Labels["k8s.workload"]
			if wl == "" {
				continue
			}
			key := l.Labels["k8s.namespace"] + "/" + wl
			if byWL[key] == nil {
				byWL[key] = &agg{pods: map[string]bool{}, ns: l.Labels["k8s.namespace"]}
			}
			byWL[key].cost = byWL[key].cost.Add(l.Amount)
			byWL[key].pods[l.ResourceID] = true
		}
		for key, g := range byWL {
			var pods []string
			for p := range g.pods {
				pods = append(pods, p)
			}
			series, err := s.TSDB.QueryMetrics(ctx, tsdb.MetricQuery{ResourceIDs: pods, Metrics: []string{model.MetricCPUUsageCores, model.MetricCPURequestCores},
				From: from, To: to, Step: time.Hour, Agg: tsdb.AggAvg})
			if err != nil {
				return nil, err
			}
			used, req := hourlySum(series, model.MetricCPUUsageCores), hourlySum(series, model.MetricCPURequestCores)
			row := efficiencyRow{Key: key, Name: strings.SplitN(key, "/", 2)[1], Kind: "workload", Cost30d: g.cost.Round(2), Currency: a.Org.Currency,
				RequestedCPU: round3(avgOf(req)), UsedCPU: round3(avgOf(used)), CPUP95: round3(tsdb.Percentile(used, 95))}
			if row.RequestedCPU > 0 {
				row.Efficiency = round3(row.UsedCPU / row.RequestedCPU)
				row.CPUAvg = row.Efficiency
				waste := math.Max(0, 1-row.Efficiency)
				row.WasteEstimate = g.cost.Mul(decimal.NewFromFloat(waste).Round(4)).Round(2)
			}
			out = append(out, row)
		}
	case "node":
		res, err := s.efficiency(ctx, a, "resource")
		if err != nil {
			return nil, err
		}
		byRes := map[string]efficiencyRow{}
		for _, r := range res {
			byRes[r.Key] = r
		}
		rows, err := s.TSDB.QueryCosts(ctx, tsdb.CostQuery{From: from, To: to, Granularity: tsdb.GranTotal, GroupBy: []string{"allocation_node_id", "resource_id"}})
		if err != nil {
			return nil, err
		}
		nodes, err := store.ListAll(ctx, s.Store.AllocationNodes(), func(n model.AllocationNode) string { return n.ID }, nil)
		if err != nil {
			return nil, err
		}
		names := map[string]string{model.UnallocatedNodeID: "Non alloué"}
		for _, n := range nodes {
			names[n.ID] = n.Name
		}
		type agg struct {
			cost, weighted decimal.Decimal
			weight         decimal.Decimal
		}
		byNode := map[string]*agg{}
		for _, r := range rows {
			n := r.Keys["allocation_node_id"]
			if byNode[n] == nil {
				byNode[n] = &agg{}
			}
			byNode[n].cost = byNode[n].cost.Add(r.Amount)
			if e, ok := byRes[r.Keys["resource_id"]]; ok && e.CPUAvg > 0 {
				byNode[n].weighted = byNode[n].weighted.Add(r.Amount.Mul(decimal.NewFromFloat(e.CPUAvg)))
				byNode[n].weight = byNode[n].weight.Add(r.Amount)
			}
		}
		for n, g := range byNode {
			row := efficiencyRow{Key: n, Name: names[n], Kind: "allocation_node", Cost30d: g.cost.Round(2), Currency: a.Org.Currency}
			if g.weight.IsPositive() {
				f, _ := g.weighted.Div(g.weight).Float64()
				row.CPUAvg, row.Efficiency = round3(f), round3(f)
			}
			out = append(out, row)
		}
	default:
		rs, err := s.Store.Resources().Current(ctx, store.ResourceFilter{Types: []string{model.TypeInstance, model.TypeHost}})
		if err != nil {
			return nil, err
		}
		idsList := make([]string, 0, len(rs))
		for _, r := range rs {
			idsList = append(idsList, r.ID)
		}
		costs, err := s.costByResource(ctx, idsList, from, to)
		if err != nil {
			return nil, err
		}
		series, err := s.TSDB.QueryMetrics(ctx, tsdb.MetricQuery{ResourceIDs: idsList, Metrics: []string{model.MetricCPUUtil, model.MetricMemUtil},
			From: from, To: to, Step: time.Hour, Agg: tsdb.AggAvg})
		if err != nil {
			return nil, err
		}
		vals := map[string]map[string][]float64{}
		for _, sr := range series {
			if vals[sr.ResourceID] == nil {
				vals[sr.ResourceID] = map[string][]float64{}
			}
			for _, p := range sr.Points {
				vals[sr.ResourceID][sr.Metric] = append(vals[sr.ResourceID][sr.Metric], p.Value)
			}
		}
		for _, r := range rs {
			cpu, mem := vals[r.ID][model.MetricCPUUtil], vals[r.ID][model.MetricMemUtil]
			row := efficiencyRow{Key: r.ID, Name: r.Name, Kind: r.Type, Cost30d: costs[r.ID].Round(2), Currency: a.Org.Currency,
				CPUAvg: round3(avgOf(cpu)), CPUP95: round3(tsdb.Percentile(cpu, 95)), MemAvg: round3(avgOf(mem))}
			row.Efficiency = round3(math.Max(row.CPUAvg, row.MemAvg))
			if len(cpu) > 0 {
				waste := math.Max(0, 1-math.Max(row.CPUP95, row.MemAvg))
				row.WasteEstimate = costs[r.ID].Mul(decimal.NewFromFloat(waste).Round(4)).Round(2)
			}
			out = append(out, row)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if c := out[i].Cost30d.Cmp(out[j].Cost30d); c != 0 {
			return c > 0
		}
		return out[i].Key < out[j].Key
	})
	if out == nil {
		out = []efficiencyRow{}
	}
	return out, nil
}

// hourlySum additionne heure par heure les séries d'une métrique.
func hourlySum(series []tsdb.Series, metric string) []float64 {
	byTS := map[time.Time]float64{}
	for _, s := range series {
		if s.Metric != metric {
			continue
		}
		for _, p := range s.Points {
			byTS[p.TS] += p.Value
		}
	}
	out := make([]float64, 0, len(byTS))
	for _, v := range byTS {
		out = append(out, v)
	}
	return out
}

func avgOf(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

// nonZeroRounded arrondit au centime et retire les lignes nulles.
func nonZeroRounded(rows []tsdb.CostRow) []tsdb.CostRow {
	out := make([]tsdb.CostRow, 0, len(rows))
	for _, r := range rows {
		r.Amount = r.Amount.Round(2)
		if !r.Amount.IsZero() {
			out = append(out, r)
		}
	}
	return out
}
