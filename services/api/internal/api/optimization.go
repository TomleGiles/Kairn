package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/auth"
	"github.com/kairn-io/kairn/pkg/budget"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/plans"
	"github.com/kairn-io/kairn/pkg/pricing"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/tsdb"
	"github.com/kairn-io/kairn/pkg/whatif"
)

// transitions autorisées du cycle de vie d'une recommandation (M-06).
var recoTransitions = map[string][]string{
	model.RecoOpen:      {model.RecoAccepted, model.RecoPostponed, model.RecoDismissed},
	model.RecoAccepted:  {model.RecoApplied, model.RecoDismissed, model.RecoOpen},
	model.RecoPostponed: {model.RecoOpen, model.RecoAccepted, model.RecoDismissed},
	model.RecoDismissed: {model.RecoOpen},
	model.RecoApplied:   {},
}

func transitionAllowed(from, to string) bool {
	for _, x := range recoTransitions[from] {
		if x == to {
			return true
		}
	}
	return false
}

type recoSummary struct {
	Currency         string                     `json:"currency"`
	OpenCount        int                        `json:"open_count"`
	PotentialMonthly decimal.Decimal            `json:"potential_monthly"`
	AcceptedMonthly  decimal.Decimal            `json:"accepted_monthly"`
	RealizedMonthly  decimal.Decimal            `json:"realized_monthly"`
	ByType           map[string]decimal.Decimal `json:"potential_by_type"`
	ByRisk           map[string]int             `json:"open_by_risk"`
}

type forecastOut struct {
	Forecast model.Forecast `json:"forecast"`
	History  []tsdb.CostRow `json:"history"`
	Source   string         `json:"source" doc:"analytics (modèle saisonnier) ou linear (repli)"`
}

// WhatIfBody liste les scénarios d'une simulation what-if.
type WhatIfBody struct {
	Scenarios []whatif.Scenario `json:"scenarios" required:"true" minItems:"1" maxItems:"20"`
}

func (s *Server) registerOptimization() {
	tag := "Recommandations"
	type recoIn struct {
		OrgPath
		Status     []string `query:"status,explode" doc:"open, accepted, postponed, dismissed, applied"`
		Type       []string `query:"type,explode"`
		ResourceID string   `query:"resource_id"`
		Cursor     string   `query:"cursor"`
		Limit      int      `query:"limit" minimum:"0" maximum:"500"`
	}
	huma.Register(s.API, huma.Operation{OperationID: "list-recommendations", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/recommendations", Tags: []string{tag},
		Summary: "Recommandations triées par économie mensuelle"},
		func(ctx context.Context, in *recoIn) (*Out[Page[model.Recommendation]], error) {
			ctx, _, err := s.enter(ctx, in.OrgID, auth.PermCostsRead, plans.FeatureRecommendation)
			if err != nil {
				return nil, err
			}
			lim := store.ListQuery{Limit: in.Limit}.Normalize().Limit
			rs, err := s.Store.Recommendations().List(ctx, store.RecommendationFilter{Status: in.Status, Types: in.Type, ResourceID: in.ResourceID, Cursor: in.Cursor, Limit: lim})
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			return out(page(rs, lim, func(r model.Recommendation) string { return r.ID })), nil
		})
	huma.Register(s.API, huma.Operation{OperationID: "get-recommendation-summary", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/recommendations/summary", Tags: []string{tag},
		Summary: "Économies potentielles, acceptées et réalisées"},
		func(ctx context.Context, in *OrgPath) (*Out[recoSummary], error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermCostsRead, plans.FeatureRecommendation)
			if err != nil {
				return nil, err
			}
			all, err := s.Store.Recommendations().List(ctx, store.RecommendationFilter{Limit: store.MaxLimit})
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			sm := recoSummary{Currency: a.Org.Currency, ByType: map[string]decimal.Decimal{}, ByRisk: map[string]int{}}
			for _, r := range all {
				switch r.Status {
				case model.RecoOpen:
					sm.OpenCount++
					sm.PotentialMonthly = sm.PotentialMonthly.Add(r.SavingsMonthly)
					sm.ByType[r.Type] = sm.ByType[r.Type].Add(r.SavingsMonthly)
					sm.ByRisk[r.Risk]++
				case model.RecoAccepted:
					sm.AcceptedMonthly = sm.AcceptedMonthly.Add(r.SavingsMonthly)
				case model.RecoApplied:
					if r.MeasuredSavingsMonthly != nil {
						sm.RealizedMonthly = sm.RealizedMonthly.Add(*r.MeasuredSavingsMonthly)
					}
				}
			}
			sm.PotentialMonthly, sm.AcceptedMonthly, sm.RealizedMonthly = sm.PotentialMonthly.Round(2), sm.AcceptedMonthly.Round(2), sm.RealizedMonthly.Round(2)
			for k, v := range sm.ByType {
				sm.ByType[k] = v.Round(2)
			}
			return out(sm), nil
		})
	huma.Register(s.API, huma.Operation{OperationID: "get-recommendation", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/recommendations/{id}", Tags: []string{tag},
		Summary: "Détail : preuves, risque, commande prête à l'emploi"},
		func(ctx context.Context, in *IDPath) (*Out[model.Recommendation], error) {
			ctx, _, err := s.enter(ctx, in.OrgID, auth.PermCostsRead, plans.FeatureRecommendation)
			if err != nil {
				return nil, err
			}
			r, err := s.Store.Recommendations().Get(ctx, in.ID)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			return out(r), nil
		})
	type statusIn struct {
		IDPath
		Body struct {
			Status         string     `json:"status" required:"true" enum:"open,accepted,postponed,dismissed,applied"`
			Reason         string     `json:"reason,omitempty" maxLength:"1000"`
			PostponedUntil *time.Time `json:"postponed_until,omitempty"`
		}
	}
	huma.Register(s.API, huma.Operation{OperationID: "update-recommendation-status", Method: http.MethodPost, Path: Prefix + "/orgs/{org_id}/recommendations/{id}/status", Tags: []string{tag},
		Summary: "Accepte, reporte, ignore (avec motif) ou marque comme appliquée"},
		func(ctx context.Context, in *statusIn) (*Out[model.Recommendation], error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermRecoManage, plans.FeatureRecommendation)
			if err != nil {
				return nil, err
			}
			r, err := s.Store.Recommendations().Get(ctx, in.ID)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			b := in.Body
			if !transitionAllowed(r.Status, b.Status) {
				return nil, huma.Error409Conflict("transition " + r.Status + " → " + b.Status + " is not allowed")
			}
			switch b.Status {
			case model.RecoDismissed, model.RecoPostponed:
				if b.Reason == "" {
					return nil, invalid("a reason is required")
				}
			}
			if b.Status == model.RecoPostponed {
				if b.PostponedUntil == nil || !b.PostponedUntil.After(s.now()) {
					return nil, invalid("postponed_until must be in the future")
				}
				r.PostponedUntil = b.PostponedUntil
			} else {
				r.PostponedUntil = nil
			}
			if b.Status == model.RecoApplied {
				now := s.now()
				r.AppliedAt = &now
			}
			prev := r.Status
			r.Status, r.StatusReason = b.Status, b.Reason
			if err := s.Store.Recommendations().Update(ctx, &r); err != nil {
				return nil, s.fail(ctx, err)
			}
			s.audit(ctx, a, "recommendation."+b.Status, "recommendation", r.ID, map[string]any{"from": prev, "reason": b.Reason})
			return out(r), nil
		})

	huma.Register(s.API, huma.Operation{OperationID: "run-analytics", Method: http.MethodPost, Path: Prefix + "/orgs/{org_id}/analytics/run", Tags: []string{tag},
		Summary: "Relance anomalies, recommandations et prévisions", DefaultStatus: http.StatusAccepted},
		func(ctx context.Context, in *OrgPath) (*Empty, error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermRecoManage, "")
			if err != nil {
				return nil, err
			}
			if err := s.Jobs.RequestAnalytics(ctx, a.Org.ID); err != nil {
				return nil, s.fail(ctx, err)
			}
			return &Empty{}, nil
		})

	// ---- Anomalies (M-07)
	atag := "Anomalies"
	type anomIn struct {
		OrgPath
		From   time.Time `query:"from"`
		To     time.Time `query:"to"`
		Status string    `query:"status" enum:"open,acknowledged,resolved"`
	}
	huma.Register(s.API, huma.Operation{OperationID: "list-anomalies", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/anomalies", Tags: []string{atag},
		Summary: "Anomalies de coût et d'usage, avec événements corrélés et explication"},
		func(ctx context.Context, in *anomIn) (*Out[[]model.Anomaly], error) {
			ctx, _, err := s.enter(ctx, in.OrgID, auth.PermCostsRead, plans.FeatureAnomalies)
			if err != nil {
				return nil, err
			}
			from := in.From
			if from.IsZero() {
				from = s.now().AddDate(0, 0, -90)
			}
			as, err := s.Store.Anomalies().List(ctx, from, in.To, in.Status)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			if as == nil {
				as = []model.Anomaly{}
			}
			return out(as), nil
		})
	huma.Register(s.API, huma.Operation{OperationID: "get-anomaly", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/anomalies/{id}", Tags: []string{atag},
		Summary: "Détail d'une anomalie"},
		func(ctx context.Context, in *IDPath) (*Out[model.Anomaly], error) {
			ctx, _, err := s.enter(ctx, in.OrgID, auth.PermCostsRead, plans.FeatureAnomalies)
			if err != nil {
				return nil, err
			}
			an, err := s.Store.Anomalies().Get(ctx, in.ID)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			return out(an), nil
		})
	huma.Register(s.API, huma.Operation{OperationID: "update-anomaly-status", Method: http.MethodPost, Path: Prefix + "/orgs/{org_id}/anomalies/{id}/status", Tags: []string{atag},
		Summary: "Acquitte ou résout une anomalie"},
		func(ctx context.Context, in *struct {
			IDPath
			Body struct {
				Status string `json:"status" required:"true" enum:"open,acknowledged,resolved"`
			}
		}) (*Out[model.Anomaly], error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermRecoManage, plans.FeatureAnomalies)
			if err != nil {
				return nil, err
			}
			an, err := s.Store.Anomalies().Get(ctx, in.ID)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			an.Status = in.Body.Status
			if err := s.Store.Anomalies().Update(ctx, &an); err != nil {
				return nil, s.fail(ctx, err)
			}
			s.audit(ctx, a, "anomaly."+an.Status, "anomaly", an.ID, nil)
			return out(an), nil
		})

	// ---- Prévisions et simulations (M-09)
	ftag := "Prévisions"
	huma.Register(s.API, huma.Operation{OperationID: "get-forecast", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/forecast", Tags: []string{ftag},
		Summary: "Prévision de fin de mois avec intervalle de confiance"},
		func(ctx context.Context, in *struct {
			OrgPath
			NodeID string `query:"node_id"`
		}) (*Out[forecastOut], error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermCostsRead, plans.FeatureForecast)
			if err != nil {
				return nil, err
			}
			res, err := s.forecast(ctx, a, in.NodeID)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			return out(res), nil
		})
	huma.Register(s.API, huma.Operation{OperationID: "simulate-whatif", Method: http.MethodPost, Path: Prefix + "/orgs/{org_id}/whatif", Tags: []string{ftag},
		Summary: "Simulation what-if : ajout de nodes, changement de gamme, migration de cloud"},
		func(ctx context.Context, in *CreateIn[WhatIfBody]) (*Out[whatif.Result], error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermCostsRead, plans.FeatureForecast)
			if err != nil {
				return nil, err
			}
			book, err := s.book(ctx)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			rs, err := s.Store.Resources().Current(ctx, store.ResourceFilter{})
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			fc, err := s.forecast(ctx, a, "")
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			res, err := whatif.Run(whatif.Input{Book: book, Resources: rs, Currency: a.Org.Currency, Now: s.now(),
				BaselineMonthly: fc.Forecast.Total, Rates: func(from, to string, day time.Time) (decimal.Decimal, error) {
					r, err := s.Store.Rates().Get(ctx, from, to, day)
					return r.Rate, err
				}}, in.Body.Scenarios)
			if err != nil {
				return nil, invalid(err.Error())
			}
			return out(res), nil
		})
}

// book construit l'index des grilles visibles par l'organisation.
func (s *Server) book(ctx context.Context) (*pricing.Book, error) {
	cats, err := s.Store.Pricing().ListCatalogs(ctx)
	if err != nil {
		return nil, err
	}
	var all []pricing.Catalog
	for _, c := range cats {
		items, err := s.Store.Pricing().Items(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		all = append(all, pricing.Catalog{Catalog: c, Items: items})
	}
	return pricing.NewBook(all), nil
}

// forecast renvoie la prévision du mois courant (modèle analytics ou repli linéaire).
func (s *Server) forecast(ctx context.Context, a access, nodeID string) (forecastOut, error) {
	now := s.now()
	today := tsdb.TruncDay(now)
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	q := tsdb.CostQuery{From: today.AddDate(0, 0, -90), To: today.AddDate(0, 0, 1), Granularity: tsdb.GranDay}
	allowed, err := s.allowedNodes(ctx, a)
	if err != nil {
		return forecastOut{}, err
	}
	q.NodeIDs = allowed
	if nodeID != "" {
		tree, _, err := s.tree(ctx)
		if err != nil {
			return forecastOut{}, err
		}
		q.NodeIDs = tree.Subtree(nodeID)
		if allowed != nil {
			q.NodeIDs = intersect(q.NodeIDs, allowed)
		}
	}
	hist, err := s.TSDB.QueryCosts(ctx, q)
	if err != nil {
		return forecastOut{}, err
	}
	for i := range hist {
		hist[i].Amount = hist[i].Amount.Round(2)
	}
	if hist == nil {
		hist = []tsdb.CostRow{}
	}
	if f, err := s.Store.Forecasts().Get(ctx, nodeID); err == nil && f.PeriodEnd.Equal(monthEnd) && (allowed == nil || nodeID != "") {
		return forecastOut{Forecast: f, History: hist, Source: "analytics"}, nil
	}
	actual := decimal.Zero
	byDay := map[time.Time]decimal.Decimal{}
	for _, r := range hist {
		byDay[r.Period] = r.Amount
		if !r.Period.Before(monthStart) && r.Period.Before(today) {
			actual = actual.Add(r.Amount)
		}
	}
	var daily []decimal.Decimal
	for d := today.AddDate(0, 0, -14); d.Before(today); d = d.AddDate(0, 0, 1) {
		daily = append(daily, byDay[d])
	}
	total := budget.Forecast(actual, daily, today, monthEnd)
	f := model.Forecast{OrgID: a.Org.ID, NodeID: nodeID, GeneratedAt: now, Model: "linear-7d", Currency: a.Org.Currency, PeriodEnd: monthEnd,
		Total: total.Round(2), Lower: total.Mul(decimal.RequireFromString("0.95")).Round(2), Upper: total.Mul(decimal.RequireFromString("1.05")).Round(2)}
	avg := decimal.Zero
	if len(daily) > 0 {
		w := daily
		if len(w) > 7 {
			w = w[len(w)-7:]
		}
		for _, v := range w {
			avg = avg.Add(v)
		}
		avg = avg.Div(decimal.NewFromInt(int64(len(w))))
	}
	for d := today; d.Before(monthEnd); d = d.AddDate(0, 0, 1) {
		f.Points = append(f.Points, model.ForecastPoint{Day: d, Value: avg.Round(2), Lower: avg.Mul(decimal.RequireFromString("0.9")).Round(2), Upper: avg.Mul(decimal.RequireFromString("1.1")).Round(2)})
	}
	return forecastOut{Forecast: f, History: hist, Source: "linear"}, nil
}
