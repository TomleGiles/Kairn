package api

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/auth"
	"github.com/kairn-io/kairn/pkg/bus"
	"github.com/kairn-io/kairn/pkg/ids"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/notify"
	"github.com/kairn-io/kairn/pkg/objstore"
	"github.com/kairn-io/kairn/pkg/plans"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/tenancy"
	"github.com/kairn-io/kairn/pkg/tsdb"
)

// Les routes /internal/v1 sont réservées aux services Kairn (analytics, ai),
// authentifiés par jeton de service. Elles n'apparaissent pas dans l'OpenAPI publique.
const internalPrefix = "/internal/v1"

// serviceOrg vérifie le principal de service et renvoie un contexte d'organisation.
func (s *Server) serviceOrg(ctx context.Context, orgID string) (context.Context, model.Organization, error) {
	p, err := principal(ctx)
	if err != nil {
		return ctx, model.Organization{}, err
	}
	if p.Kind != auth.KindService {
		return ctx, model.Organization{}, huma.Error403Forbidden("service token required")
	}
	if !ids.Valid(orgID) {
		return ctx, model.Organization{}, huma.Error404NotFound("organization not found")
	}
	octx := tenancy.WithOrg(ctx, orgID)
	o, err := s.Store.Orgs().Get(octx, orgID)
	if err != nil {
		return ctx, model.Organization{}, huma.Error404NotFound("organization not found")
	}
	return octx, o, nil
}

type internalOrg struct {
	model.Organization
	Limits plans.Limits `json:"limits"`
}

type metricsQuery struct {
	ResourceIDs []string  `json:"resource_ids" maxItems:"5000"`
	Metrics     []string  `json:"metrics"`
	From        time.Time `json:"from" required:"true"`
	To          time.Time `json:"to" required:"true"`
	StepSeconds int       `json:"step_seconds" minimum:"0"`
	Agg         string    `json:"agg,omitempty" enum:"avg,max,min,p95,sum,last"`
}

type costsQuery struct {
	From        time.Time           `json:"from" required:"true"`
	To          time.Time           `json:"to" required:"true"`
	Granularity string              `json:"granularity,omitempty" enum:"day,week,month,total"`
	GroupBy     []string            `json:"group_by,omitempty"`
	Filters     map[string][]string `json:"filters,omitempty"`
	Limit       int                 `json:"limit,omitempty"`
}

type recoBatch struct {
	Items []model.Recommendation `json:"items" required:"true"`
	// Types remplacés : les recommandations ouvertes de ces types absentes du lot sont closes.
	ReplaceTypes []string `json:"replace_types,omitempty"`
	// Measured : économies mesurées des recommandations appliquées (id → montant mensuel).
	Measured map[string]decimal.Decimal `json:"measured,omitempty"`
}

type reportUpload struct {
	Kind      string         `json:"kind,omitempty"`
	Status    string         `json:"status" required:"true" enum:"ready,error,sent"`
	Summary   map[string]any `json:"summary,omitempty"`
	PDFBase64 string         `json:"pdf_base64,omitempty"`
}

type batchResult struct {
	Created int `json:"created"`
	Updated int `json:"updated"`
	Closed  int `json:"closed"`
}

func (s *Server) publish(ctx context.Context, subject, orgID string, data any) {
	if s.Bus == nil {
		return
	}
	if err := s.Bus.Publish(ctx, subject, orgID, data); err != nil {
		s.Log.Warn("bus publish failed", "subject", subject, "err", err)
	}
}

// AnomalyBatch est un lot d'anomalies publié par le service analytics.
type AnomalyBatch struct {
	Items []model.Anomaly `json:"items" required:"true"`
}

// ForecastBatch est un lot de prévisions publié par le service analytics.
type ForecastBatch struct {
	Items []model.Forecast `json:"items" required:"true"`
}

func (s *Server) registerInternal() {
	tag := "Interne"
	op := func(id, method, path, summary string) huma.Operation {
		return huma.Operation{OperationID: "internal-" + id, Method: method, Path: internalPrefix + path, Tags: []string{tag}, Summary: summary, Hidden: true}
	}

	huma.Register(s.API, op("list-orgs", http.MethodGet, "/orgs", "Organisations (services)"),
		func(ctx context.Context, _ *struct{}) (*Out[[]internalOrg], error) {
			p, err := principal(ctx)
			if err != nil || p.Kind != auth.KindService {
				return nil, huma.Error403Forbidden("service token required")
			}
			idsList, err := s.Store.System().ListOrgIDs(ctx)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			res := []internalOrg{}
			for _, id := range idsList {
				o, err := s.Store.Orgs().Get(tenancy.WithOrg(ctx, id), id)
				if err != nil {
					continue
				}
				res = append(res, internalOrg{Organization: o, Limits: plans.For(o, s.now())})
			}
			return out(res), nil
		})

	type orgIn struct {
		OrgID string `path:"org_id"`
	}
	huma.Register(s.API, op("list-resources", http.MethodGet, "/orgs/{org_id}/resources", "Inventaire courant complet"),
		func(ctx context.Context, in *struct {
			OrgID string   `path:"org_id"`
			Type  []string `query:"type,explode"`
		}) (*Out[[]model.Resource], error) {
			ctx, _, err := s.serviceOrg(ctx, in.OrgID)
			if err != nil {
				return nil, err
			}
			rs, err := s.Store.Resources().Current(ctx, store.ResourceFilter{Types: in.Type})
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			if rs == nil {
				rs = []model.Resource{}
			}
			return out(rs), nil
		})
	huma.Register(s.API, op("list-edges", http.MethodGet, "/orgs/{org_id}/edges", "Arêtes courantes du graphe"),
		func(ctx context.Context, in *orgIn) (*Out[[]model.ResourceEdge], error) {
			ctx, _, err := s.serviceOrg(ctx, in.OrgID)
			if err != nil {
				return nil, err
			}
			es, err := s.Store.Resources().Edges(ctx, time.Time{}, time.Time{})
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			if es == nil {
				es = []model.ResourceEdge{}
			}
			return out(es), nil
		})
	huma.Register(s.API, op("list-allocation-nodes", http.MethodGet, "/orgs/{org_id}/allocation-nodes", "Arbre d'allocation"),
		func(ctx context.Context, in *orgIn) (*Out[[]model.AllocationNode], error) {
			ctx, _, err := s.serviceOrg(ctx, in.OrgID)
			if err != nil {
				return nil, err
			}
			nodes, err := store.ListAll(ctx, s.Store.AllocationNodes(), func(n model.AllocationNode) string { return n.ID }, nil)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			if nodes == nil {
				nodes = []model.AllocationNode{}
			}
			return out(nodes), nil
		})
	huma.Register(s.API, op("list-history", http.MethodGet, "/orgs/{org_id}/resources/{id}/history", "Historique d'une ressource"),
		func(ctx context.Context, in *IDPath) (*Out[[]model.Resource], error) {
			ctx, _, err := s.serviceOrg(ctx, in.OrgID)
			if err != nil {
				return nil, err
			}
			h, err := s.Store.Resources().History(ctx, in.ID)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			return out(h), nil
		})
	huma.Register(s.API, op("query-metrics", http.MethodPost, "/orgs/{org_id}/metrics/query", "Séries de métriques en masse"),
		func(ctx context.Context, in *CreateIn[metricsQuery]) (*Out[[]tsdb.Series], error) {
			ctx, _, err := s.serviceOrg(ctx, in.OrgID)
			if err != nil {
				return nil, err
			}
			b := in.Body
			series, err := s.TSDB.QueryMetrics(ctx, tsdb.MetricQuery{ResourceIDs: b.ResourceIDs, Metrics: b.Metrics, From: b.From, To: b.To,
				Step: time.Duration(b.StepSeconds) * time.Second, Agg: b.Agg})
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			if series == nil {
				series = []tsdb.Series{}
			}
			return out(series), nil
		})
	huma.Register(s.API, op("query-costs", http.MethodPost, "/orgs/{org_id}/costs/query", "Agrégats de coût"),
		func(ctx context.Context, in *CreateIn[costsQuery]) (*Out[[]tsdb.CostRow], error) {
			ctx, _, err := s.serviceOrg(ctx, in.OrgID)
			if err != nil {
				return nil, err
			}
			b := in.Body
			q := tsdb.CostQuery{From: b.From, To: b.To, Granularity: b.Granularity, GroupBy: b.GroupBy, Filters: b.Filters, Limit: b.Limit}
			if q.Granularity == "" {
				q.Granularity = tsdb.GranDay
			}
			if err := q.Validate(); err != nil {
				return nil, invalid(err.Error())
			}
			rows, err := s.TSDB.QueryCosts(ctx, q)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			if rows == nil {
				rows = []tsdb.CostRow{}
			}
			return out(rows), nil
		})
	huma.Register(s.API, op("list-events", http.MethodGet, "/orgs/{org_id}/events", "Événements d'une fenêtre"),
		func(ctx context.Context, in *struct {
			OrgID string    `path:"org_id"`
			From  time.Time `query:"from"`
			To    time.Time `query:"to"`
		}) (*Out[[]model.Event], error) {
			ctx, _, err := s.serviceOrg(ctx, in.OrgID)
			if err != nil {
				return nil, err
			}
			evs, err := s.TSDB.QueryEvents(ctx, tsdb.EventQuery{From: in.From, To: in.To, Limit: 10000})
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			if evs == nil {
				evs = []model.Event{}
			}
			return out(evs), nil
		})
	type catalogItem struct {
		Provider   string            `json:"provider"`
		Version    string            `json:"version"`
		SKU        string            `json:"sku"`
		Region     string            `json:"region"`
		Unit       string            `json:"unit"`
		Price      decimal.Decimal   `json:"price"`
		Currency   string            `json:"currency"`
		Attributes map[string]string `json:"attributes"`
	}
	huma.Register(s.API, op("get-catalog", http.MethodGet, "/orgs/{org_id}/catalog", "Articles tarifaires en vigueur"),
		func(ctx context.Context, in *orgIn) (*Out[[]catalogItem], error) {
			ctx, _, err := s.serviceOrg(ctx, in.OrgID)
			if err != nil {
				return nil, err
			}
			book, err := s.book(ctx)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			res := []catalogItem{}
			for _, p := range book.Providers() {
				for _, m := range book.Items(p, s.now()) {
					res = append(res, catalogItem{Provider: p, Version: m.Catalog.Version, SKU: m.Item.SKU, Region: m.Item.Region,
						Unit: m.Item.Unit, Price: m.Item.Price, Currency: m.Item.Currency, Attributes: m.Item.Attributes})
				}
			}
			return out(res), nil
		})
	huma.Register(s.API, op("list-recommendations", http.MethodGet, "/orgs/{org_id}/recommendations", "Recommandations existantes"),
		func(ctx context.Context, in *orgIn) (*Out[[]model.Recommendation], error) {
			ctx, _, err := s.serviceOrg(ctx, in.OrgID)
			if err != nil {
				return nil, err
			}
			var all []model.Recommendation
			cursor := ""
			for {
				page, err := s.Store.Recommendations().List(ctx, store.RecommendationFilter{Cursor: cursor, Limit: store.MaxLimit})
				if err != nil {
					return nil, s.fail(ctx, err)
				}
				all = append(all, page...)
				if len(page) < store.MaxLimit {
					break
				}
				cursor = page[len(page)-1].ID
			}
			if all == nil {
				all = []model.Recommendation{}
			}
			return out(all), nil
		})

	huma.Register(s.API, op("upsert-recommendations", http.MethodPost, "/orgs/{org_id}/recommendations/batch", "Publie un lot de recommandations"),
		func(ctx context.Context, in *CreateIn[recoBatch]) (*Out[batchResult], error) {
			ctx, o, err := s.serviceOrg(ctx, in.OrgID)
			if err != nil {
				return nil, err
			}
			var res batchResult
			seen := map[string]bool{}
			existing, err := s.Store.Recommendations().List(ctx, store.RecommendationFilter{Limit: store.MaxLimit})
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			byFP := map[string]model.Recommendation{}
			for _, r := range existing {
				byFP[r.Fingerprint] = r
			}
			for _, r := range in.Body.Items {
				if r.Fingerprint == "" || r.Type == "" {
					return nil, invalid("fingerprint and type are required")
				}
				r.OrgID = o.ID
				if r.Currency == "" {
					r.Currency = o.Currency
				}
				if r.Evidence == nil {
					r.Evidence = map[string]any{}
				}
				seen[r.Fingerprint] = true
				_, exists := byFP[r.Fingerprint]
				if err := s.Store.Recommendations().Upsert(ctx, &r); err != nil {
					return nil, s.fail(ctx, err)
				}
				if exists {
					res.Updated++
				} else {
					res.Created++
					s.publish(ctx, bus.SubjectAlert, o.ID, notify.AlertRequest{Kind: model.AlertRecommendation, Severity: "info",
						Fingerprint: "reco:" + r.Fingerprint, Title: r.Title, Body: r.Summary, Link: "/recommendations/" + r.ID,
						Payload: map[string]any{"savings_monthly": r.SavingsMonthly.StringFixed(2), "currency": r.Currency, "type": r.Type, "risk": r.Risk}})
				}
			}
			now := s.now()
			for _, r := range existing {
				if seen[r.Fingerprint] || !containsStr(in.Body.ReplaceTypes, r.Type) {
					continue
				}
				if r.Status == model.RecoOpen || (r.Status == model.RecoPostponed && r.PostponedUntil != nil && r.PostponedUntil.Before(now)) {
					r.Status, r.StatusReason = model.RecoDismissed, "clôturée automatiquement : la condition n'est plus observée"
					if err := s.Store.Recommendations().Update(ctx, &r); err != nil {
						return nil, s.fail(ctx, err)
					}
					res.Closed++
				}
			}
			for id, v := range in.Body.Measured {
				r, err := s.Store.Recommendations().Get(ctx, id)
				if err != nil || r.Status != model.RecoApplied {
					continue
				}
				m := v.Round(6)
				r.MeasuredSavingsMonthly = &m
				_ = s.Store.Recommendations().Update(ctx, &r)
			}
			return out(res), nil
		})

	huma.Register(s.API, op("upsert-anomalies", http.MethodPost, "/orgs/{org_id}/anomalies/batch", "Publie un lot d'anomalies"),
		func(ctx context.Context, in *CreateIn[AnomalyBatch]) (*Out[batchResult], error) {
			ctx, o, err := s.serviceOrg(ctx, in.OrgID)
			if err != nil {
				return nil, err
			}
			var res batchResult
			existing, err := s.Store.Anomalies().List(ctx, time.Time{}, time.Time{}, "")
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			known := map[string]bool{}
			for _, a := range existing {
				known[a.SeriesKey+"|"+a.WindowStart.UTC().Format(time.RFC3339)] = true
			}
			for _, an := range in.Body.Items {
				an.OrgID = o.ID
				if an.CorrelatedEvents == nil {
					an.CorrelatedEvents = []model.CorrelatedEvent{}
				}
				if an.ExplanationSources == nil {
					an.ExplanationSources = []string{}
				}
				key := an.SeriesKey + "|" + an.WindowStart.UTC().Format(time.RFC3339)
				if err := s.Store.Anomalies().Upsert(ctx, &an); err != nil {
					return nil, s.fail(ctx, err)
				}
				if known[key] {
					res.Updated++
					continue
				}
				res.Created++
				s.publish(ctx, bus.SubjectAlert, o.ID, notify.AlertRequest{Kind: model.AlertAnomaly, Severity: an.Severity,
					Fingerprint: "anomaly:" + an.SeriesKey, Title: an.Title, Body: an.Explanation, Link: "/anomalies/" + an.ID,
					Payload: map[string]any{"expected": an.Expected.StringFixed(2), "actual": an.Actual.StringFixed(2), "currency": an.Currency}})
			}
			return out(res), nil
		})

	huma.Register(s.API, op("upsert-forecasts", http.MethodPost, "/orgs/{org_id}/forecasts/batch", "Publie des prévisions"),
		func(ctx context.Context, in *CreateIn[ForecastBatch]) (*Out[batchResult], error) {
			ctx, o, err := s.serviceOrg(ctx, in.OrgID)
			if err != nil {
				return nil, err
			}
			var res batchResult
			for _, f := range in.Body.Items {
				f.OrgID = o.ID
				if f.Currency == "" {
					f.Currency = o.Currency
				}
				if err := s.Store.Forecasts().Upsert(ctx, &f); err != nil {
					return nil, s.fail(ctx, err)
				}
				res.Updated++
			}
			return out(res), nil
		})

	huma.Register(s.API, op("record-llm-usage", http.MethodPost, "/orgs/{org_id}/llm-usage", "Enregistre une consommation LLM"),
		func(ctx context.Context, in *CreateIn[model.LLMUsage]) (*Empty, error) {
			ctx, o, err := s.serviceOrg(ctx, in.OrgID)
			if err != nil {
				return nil, err
			}
			u := in.Body
			u.ID, u.OrgID = "", o.ID
			if u.Currency == "" {
				u.Currency = o.Currency
			}
			if err := s.Store.LLMUsage().Append(ctx, &u); err != nil {
				return nil, s.fail(ctx, err)
			}
			return &Empty{}, nil
		})

	huma.Register(s.API, op("upload-report", http.MethodPut, "/orgs/{org_id}/reports/{period}", "Dépose un rapport généré"),
		func(ctx context.Context, in *struct {
			OrgID  string `path:"org_id"`
			Period string `path:"period" pattern:"^[0-9]{4}-[0-9]{2}$"`
			Body   reportUpload
		}) (*Out[model.Report], error) {
			ctx, o, err := s.serviceOrg(ctx, in.OrgID)
			if err != nil {
				return nil, err
			}
			kind := in.Body.Kind
			if kind == "" {
				kind = "monthly_exec"
			}
			r, err := s.Store.Reports().GetByPeriod(ctx, kind, in.Period)
			isNew := err != nil
			if isNew {
				r = model.Report{OrgID: o.ID, Kind: kind, Period: in.Period}
			}
			r.Status = in.Body.Status
			if in.Body.Summary != nil {
				r.Summary = in.Body.Summary
			}
			if r.Summary == nil {
				r.Summary = map[string]any{}
			}
			if in.Body.PDFBase64 != "" {
				pdf, err := base64.StdEncoding.DecodeString(in.Body.PDFBase64)
				if err != nil || len(pdf) > 20<<20 {
					return nil, invalid("invalid pdf payload")
				}
				key := objstore.Key(o.ID, "reports", fmt.Sprintf("%s-%s.pdf", kind, in.Period))
				if err := s.Objects.Put(ctx, key, pdf, "application/pdf"); err != nil {
					return nil, s.fail(ctx, err)
				}
				r.ObjectKey = key
			}
			if r.Status == "sent" {
				now := s.now()
				r.SentAt = &now
			}
			if isNew {
				err = s.Store.Reports().Create(ctx, &r)
			} else {
				err = s.Store.Reports().Update(ctx, &r)
			}
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			if r.Status == "ready" {
				s.publish(ctx, bus.SubjectWebhookOut, o.ID, map[string]any{"event": "report.ready", "report_id": r.ID, "period": r.Period})
			}
			return out(r), nil
		})

	huma.Register(s.API, op("emit-alert", http.MethodPost, "/orgs/{org_id}/alerts", "Émet une alerte vers le notifier"),
		func(ctx context.Context, in *CreateIn[notify.AlertRequest]) (*Empty, error) {
			ctx, o, err := s.serviceOrg(ctx, in.OrgID)
			if err != nil {
				return nil, err
			}
			s.publish(ctx, bus.SubjectAlert, o.ID, in.Body)
			return &Empty{}, nil
		})
}
