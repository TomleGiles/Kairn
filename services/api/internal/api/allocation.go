package api

import (
	"bytes"
	"context"
	"encoding/csv"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/allocation"
	"github.com/kairn-io/kairn/pkg/auth"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/plans"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/tsdb"
)

type nodeBody struct {
	ParentID *string `json:"parent_id,omitempty"`
	Kind     string  `json:"kind" required:"true" enum:"organization,business_unit,team,service,environment"`
	Name     string  `json:"name" required:"true" minLength:"1" maxLength:"120"`
}

type ruleBody struct {
	NodeID     string            `json:"node_id" required:"true"`
	Name       string            `json:"name" required:"true" minLength:"1" maxLength:"120"`
	Priority   int               `json:"priority" minimum:"0" maximum:"100000"`
	Conditions []model.Condition `json:"conditions" required:"true" minItems:"1"`
	Enabled    *bool             `json:"enabled,omitempty"`
}

type sharedBody struct {
	Name    string              `json:"name" required:"true" minLength:"1" maxLength:"120"`
	Source  []model.Condition   `json:"source" required:"true" minItems:"1"`
	Method  string              `json:"method" required:"true" enum:"proportional,fixed,weighted"`
	Targets []model.ShareTarget `json:"targets" required:"true" minItems:"1"`
	Enabled *bool               `json:"enabled,omitempty"`
}

type unitMetricBody struct {
	Name        string  `json:"name" required:"true" minLength:"1" maxLength:"120"`
	UnitLabel   string  `json:"unit_label" required:"true" minLength:"1" maxLength:"60"`
	NodeID      *string `json:"node_id,omitempty"`
	Source      string  `json:"source" required:"true" enum:"prometheus,api"`
	ConnectorID *string `json:"connector_id,omitempty"`
	Query       string  `json:"query,omitempty" maxLength:"2000"`
}

func (s *Server) tree(ctx context.Context) (*allocation.Tree, []model.AllocationNode, error) {
	nodes, err := store.ListAll(ctx, s.Store.AllocationNodes(), func(n model.AllocationNode) string { return n.ID }, nil)
	if err != nil {
		return nil, nil, err
	}
	return allocation.NewTree(nodes), nodes, nil
}

func (s *Server) nodeExists(ctx context.Context, id string) error {
	if _, err := s.Store.AllocationNodes().Get(ctx, id); err != nil {
		return invalid("unknown allocation node " + id)
	}
	return nil
}

type coverageOut struct {
	Currency        string          `json:"currency"`
	From            time.Time       `json:"from"`
	To              time.Time       `json:"to"`
	Total           decimal.Decimal `json:"total"`
	Allocated       decimal.Decimal `json:"allocated"`
	Unallocated     decimal.Decimal `json:"unallocated"`
	CoveragePercent decimal.Decimal `json:"coverage_percent"`
	Target          decimal.Decimal `json:"target_percent"`
	ByNode          []chargebackRow `json:"by_node"`
	TopUnallocated  []mover         `json:"top_unallocated"`
}

type chargebackRow struct {
	NodeID string          `json:"node_id"`
	Path   string          `json:"path" doc:"Chemin lisible, ex. Acme / Retail / Équipe shop"`
	Kind   string          `json:"kind"`
	Direct decimal.Decimal `json:"direct"`
	Shared decimal.Decimal `json:"shared"`
	Total  decimal.Decimal `json:"total"`
}

func (s *Server) chargeback(ctx context.Context, a access, from, to time.Time) ([]chargebackRow, error) {
	tree, nodes, err := s.tree(ctx)
	if err != nil {
		return nil, err
	}
	allowed, err := s.allowedNodes(ctx, a)
	if err != nil {
		return nil, err
	}
	rows, err := s.TSDB.QueryCosts(ctx, tsdb.CostQuery{From: from, To: to, Granularity: tsdb.GranTotal, GroupBy: []string{"allocation_node_id", "cost_type"}, NodeIDs: allowed})
	if err != nil {
		return nil, err
	}
	names := map[string]model.AllocationNode{}
	for _, n := range nodes {
		names[n.ID] = n
	}
	byNode := map[string]*chargebackRow{}
	for _, r := range rows {
		id := r.Keys["allocation_node_id"]
		row := byNode[id]
		if row == nil {
			row = &chargebackRow{NodeID: id}
			if n, ok := names[id]; ok {
				row.Kind = n.Kind
				parts := []string{}
				for _, anc := range tree.Ancestors(id) {
					parts = append(parts, names[anc].Name)
				}
				row.Path = strings.Join(append(parts, n.Name), " / ")
			} else {
				row.Path, row.Kind = "Non alloué", "unallocated"
			}
			byNode[id] = row
		}
		if r.Keys["cost_type"] == model.CostShared {
			row.Shared = row.Shared.Add(r.Amount)
		} else {
			row.Direct = row.Direct.Add(r.Amount)
		}
	}
	out := make([]chargebackRow, 0, len(byNode))
	for _, r := range byNode {
		r.Total = r.Direct.Add(r.Shared).Round(2)
		r.Direct, r.Shared = r.Direct.Round(2), r.Shared.Round(2)
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if c := out[i].Total.Cmp(out[j].Total); c != 0 {
			return c > 0
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

// RulePreviewBody porte les conditions d'une règle à prévisualiser.
type RulePreviewBody struct {
	Conditions []model.Condition `json:"conditions" required:"true" minItems:"1"`
}

func (s *Server) registerAllocation() {
	tag := "Allocation"
	after := func(ctx context.Context, a access) { s.recomputeRecent(ctx, a) }

	registerCRUD(s, crudSpec[model.AllocationNode, nodeBody]{
		Tag: tag, Path: "/allocation/nodes", Name: "allocation-node", Plural: "allocation-nodes", Summary: "nœuds d'allocation",
		Read: auth.PermCostsRead, Write: auth.PermAllocationManage, Feature: plans.FeatureAllocation, Repo: func() store.CRUD[model.AllocationNode] { return s.Store.AllocationNodes() },
		ID: func(n *model.AllocationNode) string { return n.ID }, Filters: []string{"kind"},
		Apply: func(ctx context.Context, a access, b *nodeBody, n *model.AllocationNode, isNew bool) error {
			tree, _, err := s.tree(ctx)
			if err != nil {
				return err
			}
			parent := ""
			if b.ParentID != nil {
				parent = *b.ParentID
			}
			if err := tree.ValidateParent(n.ID, parent, b.Kind); err != nil {
				return invalid(err.Error())
			}
			if isNew {
				n.ID = newID()
			}
			n.OrgID, n.ParentID, n.Kind, n.Name = a.Org.ID, b.ParentID, b.Kind, strings.TrimSpace(b.Name)
			if parent == "" {
				n.Path = "/" + n.ID + "/"
			} else {
				n.Path = tree.PathFor(parent) + n.ID + "/"
			}
			return nil
		},
		After: func(ctx context.Context, a access, n *model.AllocationNode, op string) {
			if op != "create" {
				after(ctx, a)
			}
		},
	})

	registerCRUD(s, crudSpec[model.AllocationRule, ruleBody]{
		Tag: tag, Path: "/allocation/rules", Name: "allocation-rule", Plural: "allocation-rules", Summary: "règles d'allocation",
		Read: auth.PermCostsRead, Write: auth.PermAllocationManage, Feature: plans.FeatureAllocation, Repo: func() store.CRUD[model.AllocationRule] { return s.Store.AllocationRules() },
		ID: func(r *model.AllocationRule) string { return r.ID }, Filters: []string{"node_id"},
		Apply: func(ctx context.Context, a access, b *ruleBody, r *model.AllocationRule, isNew bool) error {
			if err := s.nodeExists(ctx, b.NodeID); err != nil {
				return err
			}
			if _, err := allocation.CompileConditions(b.Conditions); err != nil {
				return invalid(err.Error())
			}
			*r = model.AllocationRule{ID: r.ID, OrgID: a.Org.ID, NodeID: b.NodeID, Name: b.Name, Priority: b.Priority,
				Conditions: b.Conditions, Enabled: b.Enabled == nil || *b.Enabled, CreatedAt: r.CreatedAt}
			return nil
		},
		After: func(ctx context.Context, a access, _ *model.AllocationRule, _ string) { after(ctx, a) },
	})

	registerCRUD(s, crudSpec[model.SharedCostRule, sharedBody]{
		Tag: tag, Path: "/allocation/shared-rules", Name: "shared-cost-rule", Plural: "shared-cost-rules", Summary: "règles de coûts partagés",
		Read: auth.PermCostsRead, Write: auth.PermAllocationManage, Feature: plans.FeatureAllocation, Repo: func() store.CRUD[model.SharedCostRule] { return s.Store.SharedRules() },
		ID: func(r *model.SharedCostRule) string { return r.ID },
		Apply: func(ctx context.Context, a access, b *sharedBody, r *model.SharedCostRule, isNew bool) error {
			if _, err := allocation.CompileConditions(b.Source); err != nil {
				return invalid(err.Error())
			}
			sum := decimal.Zero
			for _, t := range b.Targets {
				if err := s.nodeExists(ctx, t.NodeID); err != nil {
					return err
				}
				if t.Weight.IsNegative() {
					return invalid("weights must be positive")
				}
				sum = sum.Add(t.Weight)
			}
			if b.Method == model.ShareFixed && !sum.Equal(decimal.NewFromInt(100)) {
				return invalid("fixed shares must sum to 100")
			}
			*r = model.SharedCostRule{ID: r.ID, OrgID: a.Org.ID, Name: b.Name, Source: b.Source, Method: b.Method,
				Targets: b.Targets, Enabled: b.Enabled == nil || *b.Enabled, CreatedAt: r.CreatedAt}
			return nil
		},
		After: func(ctx context.Context, a access, _ *model.SharedCostRule, _ string) { after(ctx, a) },
	})

	type previewOut struct {
		Matched int              `json:"matched"`
		Sample  []model.Resource `json:"sample"`
	}
	huma.Register(s.API, huma.Operation{OperationID: "preview-allocation-rule", Method: http.MethodPost, Path: Prefix + "/orgs/{org_id}/allocation/rules/preview", Tags: []string{tag},
		Summary: "Prévisualise les ressources correspondant à des conditions (labels hérités inclus)"},
		func(ctx context.Context, in *CreateIn[RulePreviewBody]) (*Out[previewOut], error) {
			ctx, _, err := s.enter(ctx, in.OrgID, auth.PermCostsRead, plans.FeatureAllocation)
			if err != nil {
				return nil, err
			}
			conds, err := allocation.CompileConditions(in.Body.Conditions)
			if err != nil {
				return nil, invalid(err.Error())
			}
			rs, err := s.Store.Resources().Current(ctx, store.ResourceFilter{})
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			edges, err := s.Store.Resources().Edges(ctx, time.Time{}, time.Time{})
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			lab := allocation.NewLabelerFromList(rs, edges)
			res := previewOut{Sample: []model.Resource{}}
			for _, r := range rs {
				if conds.Match(lab.Subject(r.ID)) {
					res.Matched++
					if len(res.Sample) < 25 {
						res.Sample = append(res.Sample, r)
					}
				}
			}
			return out(res), nil
		})

	type periodIn struct {
		OrgPath
		From time.Time `query:"from"`
		To   time.Time `query:"to"`
	}
	huma.Register(s.API, huma.Operation{OperationID: "get-allocation-coverage", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/allocation/coverage", Tags: []string{tag},
		Summary: "Taux de couverture d'allocation (objectif > 95 %)"},
		func(ctx context.Context, in *periodIn) (*Out[coverageOut], error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermCostsRead, plans.FeatureAllocation)
			if err != nil {
				return nil, err
			}
			to := in.To
			if to.IsZero() {
				to = tsdb.TruncDay(s.now()).AddDate(0, 0, 1)
			}
			from := in.From
			if from.IsZero() {
				from = to.AddDate(0, 0, -30)
			}
			rows, err := s.chargeback(ctx, a, from, to)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			c := coverageOut{Currency: a.Org.Currency, From: from, To: to, ByNode: rows, Target: decimal.NewFromInt(95)}
			for _, r := range rows {
				c.Total = c.Total.Add(r.Total)
				if r.NodeID == model.UnallocatedNodeID {
					c.Unallocated = c.Unallocated.Add(r.Total)
				} else {
					c.Allocated = c.Allocated.Add(r.Total)
				}
			}
			if c.Total.IsPositive() {
				c.CoveragePercent = c.Allocated.Div(c.Total).Mul(decimal.NewFromInt(100)).Round(1)
			}
			un, err := s.TSDB.QueryCosts(ctx, tsdb.CostQuery{From: from, To: to, Granularity: tsdb.GranTotal, GroupBy: []string{"resource_id"},
				Filters: map[string][]string{"allocation_node_id": {model.UnallocatedNodeID}}, Limit: 10})
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			var idsList []string
			for _, r := range un {
				c.TopUnallocated = append(c.TopUnallocated, mover{ResourceID: r.Keys["resource_id"], Current: r.Amount.Round(2)})
				idsList = append(idsList, r.Keys["resource_id"])
			}
			if rs, err := s.Store.Resources().Current(ctx, store.ResourceFilter{IDs: idsList}); err == nil {
				byID := map[string]model.Resource{}
				for _, r := range rs {
					byID[r.ID] = r
				}
				for i := range c.TopUnallocated {
					if r, ok := byID[c.TopUnallocated[i].ResourceID]; ok {
						c.TopUnallocated[i].Name, c.TopUnallocated[i].Type = r.Name, r.Type
					}
				}
			}
			if c.TopUnallocated == nil {
				c.TopUnallocated = []mover{}
			}
			return out(c), nil
		})

	type chargebackIn struct {
		OrgPath
		Month  string `query:"month" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"Mois AAAA-MM (défaut : mois précédent)"`
		Format string `query:"format" enum:"json,csv" default:"json"`
	}
	type chargebackOut struct {
		ContentType        string `header:"Content-Type"`
		ContentDisposition string `header:"Content-Disposition,omitempty"`
		Body               []byte
	}
	huma.Register(s.API, huma.Operation{OperationID: "get-chargeback", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/chargeback", Tags: []string{tag},
		Summary: "Showback / chargeback mensuel par nœud (export de refacturation)"},
		func(ctx context.Context, in *chargebackIn) (*chargebackOut, error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermCostsRead, plans.FeatureAllocation)
			if err != nil {
				return nil, err
			}
			now := s.now()
			month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 0)
			if in.Month != "" {
				t, err := time.Parse("2006-01", in.Month)
				if err != nil {
					return nil, invalid("invalid month")
				}
				month = t
			}
			rows, err := s.chargeback(ctx, a, month, month.AddDate(0, 1, 0))
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			if in.Format == "csv" {
				if !auth.RoleCan(a.Role, auth.PermExport) {
					return nil, huma.Error403Forbidden("missing permission export")
				}
				var buf bytes.Buffer
				w := csv.NewWriter(&buf)
				_ = w.Write([]string{"month", "node_id", "path", "kind", "direct", "shared", "total", "currency"})
				for _, r := range rows {
					_ = w.Write([]string{month.Format("2006-01"), r.NodeID, r.Path, r.Kind, r.Direct.StringFixed(2), r.Shared.StringFixed(2), r.Total.StringFixed(2), a.Org.Currency})
				}
				w.Flush()
				s.audit(ctx, a, "chargeback.export", "chargeback", month.Format("2006-01"), nil)
				return &chargebackOut{ContentType: "text/csv; charset=utf-8", ContentDisposition: `attachment; filename="kairn-chargeback-` + month.Format("2006-01") + `.csv"`, Body: buf.Bytes()}, nil
			}
			b, err := jsonMarshal(map[string]any{"month": month.Format("2006-01"), "currency": a.Org.Currency, "rows": rows})
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			return &chargebackOut{ContentType: "application/json", Body: b}, nil
		})

	// Métriques métier et coût unitaire (M-05).
	registerCRUD(s, crudSpec[model.UnitMetric, unitMetricBody]{
		Tag: "Coût unitaire", Path: "/unit-metrics", Name: "unit-metric", Plural: "unit-metrics", Summary: "métriques métier",
		Read: auth.PermCostsRead, Write: auth.PermAllocationManage, Feature: plans.FeatureUnitCosts, Repo: func() store.CRUD[model.UnitMetric] { return s.Store.UnitMetrics() },
		ID: func(m *model.UnitMetric) string { return m.ID },
		Apply: func(ctx context.Context, a access, b *unitMetricBody, m *model.UnitMetric, isNew bool) error {
			if b.NodeID != nil {
				if err := s.nodeExists(ctx, *b.NodeID); err != nil {
					return err
				}
			}
			if b.Source == "prometheus" && (b.ConnectorID == nil || b.Query == "") {
				return invalid("prometheus source requires connector_id and query")
			}
			*m = model.UnitMetric{ID: m.ID, OrgID: a.Org.ID, Name: b.Name, UnitLabel: b.UnitLabel, NodeID: b.NodeID, Source: b.Source,
				ConnectorID: b.ConnectorID, Query: b.Query, CreatedAt: m.CreatedAt}
			return nil
		},
	})
	type valuesIn struct {
		IDPath
		Body struct {
			Values []struct {
				Day   time.Time       `json:"day" required:"true"`
				Value decimal.Decimal `json:"value" required:"true"`
			} `json:"values" required:"true" maxItems:"1000"`
		}
	}
	huma.Register(s.API, huma.Operation{OperationID: "push-unit-metric-values", Method: http.MethodPost, Path: Prefix + "/orgs/{org_id}/unit-metrics/{id}/values", Tags: []string{"Coût unitaire"},
		Summary: "Pousse des valeurs journalières d'une métrique métier", DefaultStatus: http.StatusNoContent},
		func(ctx context.Context, in *valuesIn) (*Empty, error) {
			ctx, _, err := s.enter(ctx, in.OrgID, auth.PermAllocationManage, plans.FeatureUnitCosts)
			if err != nil {
				return nil, err
			}
			if _, err := s.Store.UnitMetrics().Get(ctx, in.ID); err != nil {
				return nil, s.fail(ctx, err)
			}
			vals := map[time.Time]decimal.Decimal{}
			for _, v := range in.Body.Values {
				if v.Value.IsNegative() {
					return nil, invalid("values must be positive")
				}
				vals[tsdb.TruncDay(v.Day)] = v.Value
			}
			if err := s.TSDB.WriteUnitValues(ctx, in.ID, vals); err != nil {
				return nil, s.fail(ctx, err)
			}
			return &Empty{}, nil
		})
	type unitCost struct {
		Day         time.Time       `json:"day"`
		Cost        decimal.Decimal `json:"cost"`
		Units       decimal.Decimal `json:"units"`
		CostPerUnit decimal.Decimal `json:"cost_per_unit"`
	}
	type unitCostsOut struct {
		Metric   model.UnitMetric `json:"metric"`
		Currency string           `json:"currency"`
		Points   []unitCost       `json:"points"`
		Average  decimal.Decimal  `json:"average_cost_per_unit"`
	}
	huma.Register(s.API, huma.Operation{OperationID: "get-unit-costs", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/unit-metrics/{id}/costs", Tags: []string{"Coût unitaire"},
		Summary: "Coût par unité métier (par requête, par client…)"},
		func(ctx context.Context, in *struct {
			IDPath
			From time.Time `query:"from"`
			To   time.Time `query:"to"`
		}) (*Out[unitCostsOut], error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermCostsRead, plans.FeatureUnitCosts)
			if err != nil {
				return nil, err
			}
			m, err := s.Store.UnitMetrics().Get(ctx, in.ID)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			to := in.To
			if to.IsZero() {
				to = tsdb.TruncDay(s.now())
			}
			from := in.From
			if from.IsZero() {
				from = to.AddDate(0, 0, -30)
			}
			q := tsdb.CostQuery{From: from, To: to, Granularity: tsdb.GranDay}
			if m.NodeID != nil {
				tree, _, err := s.tree(ctx)
				if err != nil {
					return nil, s.fail(ctx, err)
				}
				q.NodeIDs = tree.Subtree(*m.NodeID)
			}
			rows, err := s.TSDB.QueryCosts(ctx, q)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			vals, err := s.TSDB.UnitValues(ctx, m.ID, from, to)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			costs := map[time.Time]decimal.Decimal{}
			for _, r := range rows {
				costs[r.Period] = costs[r.Period].Add(r.Amount)
			}
			res := unitCostsOut{Metric: m, Currency: a.Org.Currency, Points: []unitCost{}}
			totalCost, totalUnits := decimal.Zero, decimal.Zero
			for d := from; d.Before(to); d = d.AddDate(0, 0, 1) {
				u, ok := vals[d]
				if !ok || !u.IsPositive() {
					continue
				}
				c := costs[d]
				res.Points = append(res.Points, unitCost{Day: d, Cost: c.Round(2), Units: u, CostPerUnit: c.Div(u).Round(6)})
				totalCost, totalUnits = totalCost.Add(c), totalUnits.Add(u)
			}
			if totalUnits.IsPositive() {
				res.Average = totalCost.Div(totalUnits).Round(6)
			}
			return out(res), nil
		})
}
