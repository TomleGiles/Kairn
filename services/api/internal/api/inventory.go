package api

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/auth"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/tsdb"
)

// resourceView enrichit une ressource de son coût récent.
type resourceView struct {
	model.Resource
	Cost30d  decimal.Decimal `json:"cost_30d"`
	Currency string          `json:"currency"`
}

// costByResource renvoie le coût par ressource sur [from, to).
func (s *Server) costByResource(ctx context.Context, idsList []string, from, to time.Time) (map[string]decimal.Decimal, error) {
	out := map[string]decimal.Decimal{}
	if len(idsList) == 0 {
		return out, nil
	}
	rows, err := s.TSDB.QueryCosts(ctx, tsdb.CostQuery{
		From: from, To: to, Granularity: tsdb.GranTotal, GroupBy: []string{"resource_id"},
		Filters: map[string][]string{"resource_id": idsList},
	})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.Keys["resource_id"]] = out[r.Keys["resource_id"]].Add(r.Amount)
	}
	return out, nil
}

type resourcesIn struct {
	OrgPath
	Type     []string  `query:"type,explode" doc:"Types de ressource (répétable)"`
	Provider string    `query:"provider"`
	Region   string    `query:"region"`
	Q        string    `query:"q" doc:"Recherche dans le nom ou l'identifiant externe"`
	Label    []string  `query:"label,explode" doc:"Filtre clé:valeur (répétable)"`
	At       time.Time `query:"at" doc:"Instantané historique (RFC 3339)"`
	Cursor   string    `query:"cursor"`
	Limit    int       `query:"limit" minimum:"0" maximum:"500"`
}

// topologyNode est un nœud du graphe de topologie.
type topologyNode struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Name     string          `json:"name"`
	Provider string          `json:"provider"`
	Cost30d  decimal.Decimal `json:"cost_30d"`
}

type topology struct {
	Nodes []topologyNode       `json:"nodes"`
	Edges []model.ResourceEdge `json:"edges"`
}

type orphan struct {
	Resource            model.Resource  `json:"resource"`
	Reason              string          `json:"reason"`
	MonthlyCostEstimate decimal.Decimal `json:"monthly_cost_estimate"`
	Currency            string          `json:"currency"`
}

type resourceDetail struct {
	Resource        model.Resource             `json:"resource"`
	History         []model.Resource           `json:"history"`
	Parents         []topologyNode             `json:"parents"`
	Children        []topologyNode             `json:"children"`
	Edges           []model.ResourceEdge       `json:"edges"`
	CostByType      map[string]decimal.Decimal `json:"cost_by_type_30d"`
	DailyCost       []tsdb.CostRow             `json:"daily_cost_30d"`
	Recommendations []model.Recommendation     `json:"recommendations"`
	Currency        string                     `json:"currency"`
}

func (s *Server) registerInventory() {
	tag := "Inventaire"
	huma.Register(s.API, huma.Operation{OperationID: "list-resources", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/resources", Tags: []string{tag},
		Summary: "Inventaire unifié (état courant ou historique)"},
		func(ctx context.Context, in *resourcesIn) (*Out[Page[resourceView]], error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermCostsRead, "")
			if err != nil {
				return nil, err
			}
			labels := map[string]string{}
			for _, l := range in.Label {
				if k, v, ok := strings.Cut(l, ":"); ok {
					labels[k] = v
				}
			}
			lim := store.ListQuery{Limit: in.Limit}.Normalize().Limit
			rs, err := s.Store.Resources().Current(ctx, store.ResourceFilter{
				Types: in.Type, Provider: in.Provider, Region: in.Region, Query: in.Q, Labels: labels, At: in.At, Cursor: in.Cursor, Limit: lim,
			})
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			idsList := make([]string, 0, len(rs))
			for _, r := range rs {
				idsList = append(idsList, r.ID)
			}
			to := tsdb.TruncDay(s.now()).AddDate(0, 0, 1)
			costs, err := s.costByResource(ctx, idsList, to.AddDate(0, 0, -30), to)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			views := make([]resourceView, 0, len(rs))
			for _, r := range rs {
				views = append(views, resourceView{Resource: r, Cost30d: costs[r.ID].Round(2), Currency: a.Org.Currency})
			}
			return out(page(views, lim, func(v resourceView) string { return v.ID })), nil
		})

	huma.Register(s.API, huma.Operation{OperationID: "get-resource", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/resources/{id}", Tags: []string{tag},
		Summary: "Ressource : historique, relations, coûts et recommandations"},
		func(ctx context.Context, in *IDPath) (*Out[resourceDetail], error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermCostsRead, "")
			if err != nil {
				return nil, err
			}
			hist, err := s.Store.Resources().History(ctx, in.ID)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			d := resourceDetail{Resource: hist[len(hist)-1], History: hist, Currency: a.Org.Currency,
				Parents: []topologyNode{}, Children: []topologyNode{}, CostByType: map[string]decimal.Decimal{}}
			edges, err := s.Store.Resources().Edges(ctx, time.Time{}, time.Time{})
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			var parentIDs, childIDs []string
			for _, e := range edges {
				if e.ChildID == in.ID {
					parentIDs = append(parentIDs, e.ParentID)
					d.Edges = append(d.Edges, e)
				}
				if e.ParentID == in.ID {
					childIDs = append(childIDs, e.ChildID)
					d.Edges = append(d.Edges, e)
				}
			}
			if d.Edges == nil {
				d.Edges = []model.ResourceEdge{}
			}
			related, err := s.Store.Resources().Current(ctx, store.ResourceFilter{IDs: append(append([]string{}, parentIDs...), childIDs...)})
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			byID := map[string]model.Resource{}
			for _, r := range related {
				byID[r.ID] = r
			}
			for _, id := range parentIDs {
				if r, ok := byID[id]; ok {
					d.Parents = append(d.Parents, topologyNode{ID: r.ID, Type: r.Type, Name: r.Name, Provider: r.Provider})
				}
			}
			for _, id := range childIDs {
				if r, ok := byID[id]; ok {
					d.Children = append(d.Children, topologyNode{ID: r.ID, Type: r.Type, Name: r.Name, Provider: r.Provider})
				}
			}
			to := tsdb.TruncDay(s.now()).AddDate(0, 0, 1)
			from := to.AddDate(0, 0, -30)
			rows, err := s.TSDB.QueryCosts(ctx, tsdb.CostQuery{From: from, To: to, Granularity: tsdb.GranDay, GroupBy: []string{"cost_type"},
				Filters: map[string][]string{"resource_id": {in.ID}}})
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			daily := map[time.Time]decimal.Decimal{}
			for _, r := range rows {
				d.CostByType[r.Keys["cost_type"]] = d.CostByType[r.Keys["cost_type"]].Add(r.Amount)
				daily[r.Period] = daily[r.Period].Add(r.Amount)
			}
			for day := from; day.Before(to); day = day.AddDate(0, 0, 1) {
				d.DailyCost = append(d.DailyCost, tsdb.CostRow{Period: day, Amount: daily[day].Round(6), Currency: a.Org.Currency, Keys: map[string]string{}})
			}
			recos, err := s.Store.Recommendations().List(ctx, store.RecommendationFilter{ResourceID: in.ID, Limit: 50})
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			if recos == nil {
				recos = []model.Recommendation{}
			}
			d.Recommendations = recos
			return out(d), nil
		})

	type topoIn struct {
		OrgPath
		Root  string   `query:"root" doc:"Ressource racine (vide = projets et clusters)"`
		Depth int      `query:"depth" minimum:"0" maximum:"5" default:"2"`
		Type  []string `query:"type,explode" doc:"Limiter aux types"`
	}
	huma.Register(s.API, huma.Operation{OperationID: "get-topology", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/topology", Tags: []string{tag},
		Summary: "Graphe de relations (projet → VM → volume ; cluster → node → pod → workload)"},
		func(ctx context.Context, in *topoIn) (*Out[topology], error) {
			ctx, _, err := s.enter(ctx, in.OrgID, auth.PermCostsRead, "")
			if err != nil {
				return nil, err
			}
			all, err := s.Store.Resources().Current(ctx, store.ResourceFilter{})
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			edges, err := s.Store.Resources().Edges(ctx, time.Time{}, time.Time{})
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			byID := map[string]model.Resource{}
			for _, r := range all {
				byID[r.ID] = r
			}
			children := map[string][]model.ResourceEdge{}
			hasParent := map[string]bool{}
			for _, e := range edges {
				children[e.ParentID] = append(children[e.ParentID], e)
				hasParent[e.ChildID] = true
			}
			var frontier []string
			if in.Root != "" {
				frontier = []string{in.Root}
			} else {
				for _, r := range all {
					if !hasParent[r.ID] && (r.Type == model.TypeProject || r.Type == model.TypeK8sCluster) {
						frontier = append(frontier, r.ID)
					}
				}
			}
			depth := in.Depth
			if depth == 0 {
				depth = 2
			}
			seen := map[string]bool{}
			typeOK := func(t string) bool {
				if len(in.Type) == 0 {
					return true
				}
				for _, x := range in.Type {
					if x == t {
						return true
					}
				}
				return false
			}
			var tp topology
			for level := 0; level <= depth && len(frontier) > 0 && len(tp.Nodes) < 1000; level++ {
				var next []string
				for _, id := range frontier {
					if seen[id] {
						continue
					}
					seen[id] = true
					r, ok := byID[id]
					if !ok {
						continue
					}
					tp.Nodes = append(tp.Nodes, topologyNode{ID: r.ID, Type: r.Type, Name: r.Name, Provider: r.Provider})
					if level == depth {
						continue
					}
					for _, e := range children[id] {
						if c, ok := byID[e.ChildID]; ok && typeOK(c.Type) {
							tp.Edges = append(tp.Edges, e)
							next = append(next, e.ChildID)
						}
					}
				}
				frontier = next
			}
			idsList := make([]string, 0, len(tp.Nodes))
			for _, n := range tp.Nodes {
				idsList = append(idsList, n.ID)
			}
			to := tsdb.TruncDay(s.now()).AddDate(0, 0, 1)
			costs, err := s.costByResource(ctx, idsList, to.AddDate(0, 0, -30), to)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			for i := range tp.Nodes {
				tp.Nodes[i].Cost30d = costs[tp.Nodes[i].ID].Round(2)
			}
			if tp.Nodes == nil {
				tp.Nodes = []topologyNode{}
			}
			if tp.Edges == nil {
				tp.Edges = []model.ResourceEdge{}
			}
			return out(tp), nil
		})

	huma.Register(s.API, huma.Operation{OperationID: "list-orphans", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/orphans", Tags: []string{tag},
		Summary: "Ressources orphelines : volumes non attachés, IP inutilisées, snapshots anciens"},
		func(ctx context.Context, in *OrgPath) (*Out[[]orphan], error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermCostsRead, "")
			if err != nil {
				return nil, err
			}
			orphans, err := s.findOrphans(ctx, a)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			return out(orphans), nil
		})

	type eventsIn struct {
		OrgPath
		From       time.Time `query:"from"`
		To         time.Time `query:"to"`
		Kind       []string  `query:"kind,explode"`
		ResourceID string    `query:"resource_id"`
		Limit      int       `query:"limit" minimum:"0" maximum:"2000"`
	}
	huma.Register(s.API, huma.Operation{OperationID: "list-events", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/events", Tags: []string{tag},
		Summary: "Événements : déploiements, HPA, incidents, changements d'inventaire"},
		func(ctx context.Context, in *eventsIn) (*Out[[]model.Event], error) {
			ctx, _, err := s.enter(ctx, in.OrgID, auth.PermCostsRead, "")
			if err != nil {
				return nil, err
			}
			q := tsdb.EventQuery{From: in.From, To: in.To, Kinds: in.Kind, Limit: in.Limit}
			if q.To.IsZero() {
				q.To = s.now().Add(time.Minute)
			}
			if q.From.IsZero() {
				q.From = q.To.AddDate(0, 0, -7)
			}
			if q.Limit == 0 {
				q.Limit = 500
			}
			if in.ResourceID != "" {
				q.ResourceIDs = []string{in.ResourceID}
			}
			evs, err := s.TSDB.QueryEvents(ctx, q)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			if evs == nil {
				evs = []model.Event{}
			}
			return out(evs), nil
		})
}

// findOrphans détecte les ressources orphelines et estime leur coût mensuel.
func (s *Server) findOrphans(ctx context.Context, a access) ([]orphan, error) {
	rs, err := s.Store.Resources().Current(ctx, store.ResourceFilter{Types: []string{model.TypeVolume, model.TypeIP, model.TypeSnapshot}})
	if err != nil {
		return nil, err
	}
	now := s.now()
	var cands []orphan
	for _, r := range rs {
		switch r.Type {
		case model.TypeVolume:
			if r.Attr("attached_to") == "" && strings.EqualFold(r.Attr("status"), "available") {
				cands = append(cands, orphan{Resource: r, Reason: "volume non attaché"})
			}
		case model.TypeIP:
			if r.Attr("attached_to") == "" {
				cands = append(cands, orphan{Resource: r, Reason: "IP publique non associée"})
			}
		case model.TypeSnapshot:
			created := r.ValidFrom
			if t, err := time.Parse(time.RFC3339, r.Attr("created_at")); err == nil {
				created = t
			}
			if now.Sub(created) > 90*24*time.Hour {
				cands = append(cands, orphan{Resource: r, Reason: "snapshot de plus de 90 jours"})
			}
		}
	}
	idsList := make([]string, 0, len(cands))
	for _, c := range cands {
		idsList = append(idsList, c.Resource.ID)
	}
	to := tsdb.TruncDay(now)
	costs, err := s.costByResource(ctx, idsList, to.AddDate(0, 0, -7), to)
	if err != nil {
		return nil, err
	}
	factor := decimal.NewFromInt(30).Div(decimal.NewFromInt(7))
	for i := range cands {
		cands[i].MonthlyCostEstimate = costs[cands[i].Resource.ID].Mul(factor).Round(2)
		cands[i].Currency = a.Org.Currency
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].MonthlyCostEstimate.GreaterThan(cands[j].MonthlyCostEstimate) })
	if cands == nil {
		cands = []orphan{}
	}
	return cands, nil
}
