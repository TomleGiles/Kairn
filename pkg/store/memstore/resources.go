package memstore

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/tenancy"
)

type resourceRepo struct {
	mu       sync.RWMutex
	versions map[string]map[string][]model.Resource // org → id → versions (triées)
	edges    map[string][]model.ResourceEdge        // org → arêtes
}

func newResourceRepo() *resourceRepo {
	return &resourceRepo{
		versions: map[string]map[string][]model.Resource{},
		edges:    map[string][]model.ResourceEdge{},
	}
}

var _ store.ResourceRepo = (*resourceRepo)(nil)

func matchResource(r model.Resource, f store.ResourceFilter) bool {
	if f.ConnectorID != "" && r.ConnectorID != f.ConnectorID {
		return false
	}
	if len(f.Types) > 0 && !contains(f.Types, r.Type) {
		return false
	}
	if f.Provider != "" && r.Provider != f.Provider {
		return false
	}
	if f.Region != "" && r.Region != f.Region {
		return false
	}
	if len(f.IDs) > 0 && !contains(f.IDs, r.ID) {
		return false
	}
	if f.Query != "" {
		q := strings.ToLower(f.Query)
		if !strings.Contains(strings.ToLower(r.Name), q) && !strings.Contains(strings.ToLower(r.ExternalID), q) {
			return false
		}
	}
	for k, v := range f.Labels {
		if r.Labels[k] != v {
			return false
		}
	}
	return true
}

func (r *resourceRepo) Current(ctx context.Context, f store.ResourceFilter) ([]model.Resource, error) {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []model.Resource
	for id, vs := range r.versions[org] {
		if f.Cursor != "" && id <= f.Cursor {
			continue
		}
		for _, v := range vs {
			alive := v.ValidTo == nil
			if !f.At.IsZero() {
				alive = v.Alive(f.At)
			}
			if alive && matchResource(v, f) {
				out = append(out, v)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

func (r *resourceRepo) Get(ctx context.Context, id string) (model.Resource, error) {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return model.Resource{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	vs := r.versions[org][id]
	if len(vs) == 0 {
		return model.Resource{}, store.ErrNotFound
	}
	return vs[len(vs)-1], nil
}

func (r *resourceRepo) History(ctx context.Context, id string) ([]model.Resource, error) {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	vs := r.versions[org][id]
	if len(vs) == 0 {
		return nil, store.ErrNotFound
	}
	return append([]model.Resource(nil), vs...), nil
}

func (r *resourceRepo) InWindow(ctx context.Context, from, to time.Time) ([]model.Resource, error) {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []model.Resource
	for _, vs := range r.versions[org] {
		for _, v := range vs {
			if v.Overlap(from, to) > 0 {
				out = append(out, v)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		return out[i].ValidFrom.Before(out[j].ValidFrom)
	})
	return out, nil
}

func (r *resourceRepo) InsertVersions(ctx context.Context, rs []model.Resource) error {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	byID := r.versions[org]
	if byID == nil {
		byID = map[string][]model.Resource{}
		r.versions[org] = byID
	}
	for _, x := range rs {
		if x.OrgID == "" {
			x.OrgID = org
		}
		if x.OrgID != org {
			return tenancy.ErrCrossOrg
		}
		vs := byID[x.ID]
		for _, v := range vs {
			if v.ValidTo == nil && x.ValidTo == nil {
				return store.ErrConflict
			}
			if v.ValidFrom.Equal(x.ValidFrom) {
				return store.ErrConflict
			}
		}
		vs = append(vs, x)
		sort.Slice(vs, func(i, j int) bool { return vs[i].ValidFrom.Before(vs[j].ValidFrom) })
		byID[x.ID] = vs
	}
	return nil
}

func (r *resourceRepo) CloseVersions(ctx context.Context, idsToClose []string, at time.Time) error {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range idsToClose {
		vs := r.versions[org][id]
		for i := range vs {
			if vs[i].ValidTo == nil {
				t := at
				if !t.After(vs[i].ValidFrom) {
					t = vs[i].ValidFrom.Add(time.Second)
				}
				vs[i].ValidTo = &t
			}
		}
	}
	return nil
}

func (r *resourceRepo) Edges(ctx context.Context, from, to time.Time) ([]model.ResourceEdge, error) {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []model.ResourceEdge
	for _, e := range r.edges[org] {
		if from.IsZero() && to.IsZero() {
			if e.ValidTo == nil {
				out = append(out, e)
			}
			continue
		}
		if e.ValidFrom.Before(to) && (e.ValidTo == nil || e.ValidTo.After(from)) {
			out = append(out, e)
		}
	}
	return out, nil
}

func (r *resourceRepo) CurrentEdges(ctx context.Context, connectorID string) ([]model.ResourceEdge, error) {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	// Les arêtes n'ont pas de connecteur : on retient celles dont l'enfant appartient au connecteur.
	var out []model.ResourceEdge
	for _, e := range r.edges[org] {
		if e.ValidTo != nil {
			continue
		}
		vs := r.versions[org][e.ChildID]
		if connectorID == "" || (len(vs) > 0 && vs[0].ConnectorID == connectorID) {
			out = append(out, e)
		}
	}
	return out, nil
}

func (r *resourceRepo) InsertEdges(ctx context.Context, es []model.ResourceEdge) error {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range es {
		e.OrgID = org
		r.edges[org] = append(r.edges[org], e)
	}
	return nil
}

func (r *resourceRepo) CloseEdges(ctx context.Context, es []model.ResourceEdge, at time.Time) error {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	list := r.edges[org]
	for _, c := range es {
		for i := range list {
			e := &list[i]
			if e.ValidTo == nil && e.ParentID == c.ParentID && e.ChildID == c.ChildID && e.Relation == c.Relation {
				t := at
				e.ValidTo = &t
			}
		}
	}
	return nil
}

func (r *resourceRepo) Count(ctx context.Context) (int, error) {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return 0, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	n := 0
	for _, vs := range r.versions[org] {
		if len(vs) > 0 && vs[len(vs)-1].ValidTo == nil {
			n++
		}
	}
	return n, nil
}

func (r *resourceRepo) purge(org string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.versions, org)
	delete(r.edges, org)
}
