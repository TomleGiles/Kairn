package pgstore

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kairn-io/kairn/pkg/ids"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/tenancy"
)

type resourceRepo struct{ s *Store }

var _ store.ResourceRepo = resourceRepo{}

func (r resourceRepo) rm() *tableMap {
	return r.s.mapOf("resources", reflect.TypeFor[model.Resource]())
}
func (r resourceRepo) em() *tableMap {
	return r.s.mapOf("resource_edges", reflect.TypeFor[model.ResourceEdge]())
}

func (r resourceRepo) resources(ctx context.Context, cond, order string, args ...any) ([]model.Resource, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}
	sql := "SELECT " + r.rm().selectList("") + " FROM resources WHERE org_id = $1::uuid"
	if cond != "" {
		sql += " AND " + cond
	}
	var out []model.Resource
	err = r.s.do(ctx, func(tx pgx.Tx) error {
		out, err = queryRows[model.Resource](ctx, tx, r.rm(), sql+" ORDER BY "+order, append([]any{org}, args...)...)
		return err
	})
	return out, err
}

func (r resourceRepo) Current(ctx context.Context, f store.ResourceFilter) ([]model.Resource, error) {
	var conds []string
	args := []any{}
	next := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args)+1) // $1 = org_id
	}
	if f.At.IsZero() {
		conds = append(conds, "valid_to IS NULL")
	} else {
		p := next(f.At.UTC())
		conds = append(conds, "valid_from <= "+p+" AND (valid_to IS NULL OR valid_to > "+p+")")
	}
	if f.ConnectorID != "" {
		if !ids.Valid(f.ConnectorID) {
			return nil, nil
		}
		conds = append(conds, "connector_id = "+next(f.ConnectorID)+"::uuid")
	}
	if len(f.Types) > 0 {
		conds = append(conds, "type = ANY("+next(f.Types)+"::text[])")
	}
	if f.Provider != "" {
		conds = append(conds, "provider = "+next(f.Provider))
	}
	if f.Region != "" {
		conds = append(conds, "region = "+next(f.Region))
	}
	if len(f.IDs) > 0 {
		valid := make([]string, 0, len(f.IDs))
		for _, id := range f.IDs {
			if ids.Valid(id) {
				valid = append(valid, id)
			}
		}
		if len(valid) == 0 {
			return nil, nil
		}
		conds = append(conds, "id = ANY("+next(valid)+"::uuid[])")
	}
	if f.Query != "" {
		p := next("%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(strings.ToLower(f.Query)) + "%")
		conds = append(conds, "(lower(name) LIKE "+p+" OR lower(external_id) LIKE "+p+")")
	}
	if len(f.Labels) > 0 {
		b, _ := json.Marshal(f.Labels)
		conds = append(conds, "labels @> "+next(string(b))+"::jsonb")
	}
	if f.Cursor != "" {
		conds = append(conds, "id::text > "+next(f.Cursor))
	}
	order := "id, valid_from"
	if f.Limit > 0 {
		order += " LIMIT " + strconv.Itoa(f.Limit)
	}
	return r.resources(ctx, strings.Join(conds, " AND "), order, args...)
}

func (r resourceRepo) Get(ctx context.Context, id string) (model.Resource, error) {
	if !ids.Valid(id) {
		return model.Resource{}, store.ErrNotFound
	}
	out, err := r.resources(ctx, "id = $2::uuid", "valid_from DESC LIMIT 1", id)
	if err != nil {
		return model.Resource{}, err
	}
	if len(out) == 0 {
		return model.Resource{}, store.ErrNotFound
	}
	return out[0], nil
}

func (r resourceRepo) History(ctx context.Context, id string) ([]model.Resource, error) {
	if !ids.Valid(id) {
		return nil, store.ErrNotFound
	}
	out, err := r.resources(ctx, "id = $2::uuid", "valid_from", id)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, store.ErrNotFound
	}
	return out, nil
}

func (r resourceRepo) InWindow(ctx context.Context, from, to time.Time) ([]model.Resource, error) {
	return r.resources(ctx, "valid_from < $3 AND (valid_to IS NULL OR valid_to > $2) AND $2 < $3", "id, valid_from", from.UTC(), to.UTC())
}

const insertChunk = 2000

func (r resourceRepo) InsertVersions(ctx context.Context, rs []model.Resource) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}
	for i := range rs {
		if rs[i].OrgID == "" {
			rs[i].OrgID = org
		}
		if rs[i].OrgID != org {
			return tenancy.ErrCrossOrg
		}
	}
	return r.s.do(ctx, func(tx pgx.Tx) error {
		for start := 0; start < len(rs); start += insertChunk {
			part := rs[start:min(start+insertChunk, len(rs))]
			n := len(part)
			idv, conn, prov, typ, ext, name, region, attrs, labels := make([]string, n), make([]string, n), make([]string, n), make([]string, n),
				make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n)
			from, to := make([]time.Time, n), make([]*time.Time, n)
			for i, x := range part {
				a, err := json.Marshal(orEmpty(x.Attributes))
				if err != nil {
					return err
				}
				l, err := json.Marshal(orEmptyS(x.Labels))
				if err != nil {
					return err
				}
				idv[i], conn[i], prov[i], typ[i], ext[i], name[i], region[i], attrs[i], labels[i] =
					x.ID, x.ConnectorID, x.Provider, x.Type, x.ExternalID, x.Name, x.Region, string(a), string(l)
				from[i] = x.ValidFrom.UTC()
				if x.ValidTo != nil {
					t := x.ValidTo.UTC()
					to[i] = &t
				}
			}
			if _, err := tx.Exec(ctx, `INSERT INTO resources (id, org_id, connector_id, provider, type, external_id, name, region, attributes, labels, valid_from, valid_to)
				SELECT id::uuid, $1::uuid, conn::uuid, prov, typ, ext, nm, rg, a::jsonb, l::jsonb, vf, vt
				FROM unnest($2::text[], $3::text[], $4::text[], $5::text[], $6::text[], $7::text[], $8::text[], $9::text[], $10::text[], $11::timestamptz[], $12::timestamptz[])
				AS t(id, conn, prov, typ, ext, nm, rg, a, l, vf, vt)`,
				org, idv, conn, prov, typ, ext, name, region, attrs, labels, from, to); err != nil {
				return err
			}
		}
		return nil
	})
}

func orEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func orEmptyS(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func validIDs(in []string) []string {
	out := make([]string, 0, len(in))
	for _, id := range in {
		if ids.Valid(id) {
			out = append(out, id)
		}
	}
	return out
}

func (r resourceRepo) CloseVersions(ctx context.Context, idsToClose []string, at time.Time) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}
	list := validIDs(idsToClose)
	if len(list) == 0 {
		return nil
	}
	return r.s.do(ctx, func(tx pgx.Tx) error {
		// valid_to > valid_from est garanti (contrainte CHECK) : au moins une seconde de vie.
		_, err := tx.Exec(ctx, `UPDATE resources SET valid_to = greatest($3::timestamptz, valid_from + interval '1 second')
			WHERE org_id = $1::uuid AND id = ANY($2::uuid[]) AND valid_to IS NULL`, org, list, at.UTC())
		return err
	})
}

func (r resourceRepo) edges(ctx context.Context, sql string, args ...any) ([]model.ResourceEdge, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}
	var out []model.ResourceEdge
	err = r.s.do(ctx, func(tx pgx.Tx) error {
		out, err = queryRows[model.ResourceEdge](ctx, tx, r.em(), sql, append([]any{org}, args...)...)
		return err
	})
	return out, err
}

func (r resourceRepo) Edges(ctx context.Context, from, to time.Time) ([]model.ResourceEdge, error) {
	base := "SELECT " + r.em().selectList("") + " FROM resource_edges WHERE org_id = $1::uuid"
	if from.IsZero() && to.IsZero() {
		return r.edges(ctx, base+" AND valid_to IS NULL ORDER BY parent_id, child_id, relation")
	}
	return r.edges(ctx, base+" AND valid_from < $3 AND (valid_to IS NULL OR valid_to > $2) ORDER BY parent_id, child_id, relation, valid_from",
		from.UTC(), to.UTC())
}

func (r resourceRepo) CurrentEdges(ctx context.Context, connectorID string) ([]model.ResourceEdge, error) {
	base := "SELECT " + r.em().selectList("e") + " FROM resource_edges e WHERE e.org_id = $1::uuid AND e.valid_to IS NULL"
	if connectorID == "" {
		return r.edges(ctx, base+" ORDER BY e.parent_id, e.child_id, e.relation")
	}
	if !ids.Valid(connectorID) {
		return nil, nil
	}
	// Les arêtes n'ont pas de connecteur : on retient celles dont l'enfant appartient au connecteur.
	return r.edges(ctx, base+` AND EXISTS (SELECT 1 FROM resources r WHERE r.org_id = e.org_id AND r.id = e.child_id AND r.connector_id = $2::uuid)
		ORDER BY e.parent_id, e.child_id, e.relation`, connectorID)
}

func edgeArrays(es []model.ResourceEdge) (parents, children, relations []string, from []time.Time, to []*time.Time) {
	n := len(es)
	parents, children, relations, from, to = make([]string, n), make([]string, n), make([]string, n), make([]time.Time, n), make([]*time.Time, n)
	for i, e := range es {
		parents[i], children[i], relations[i], from[i] = e.ParentID, e.ChildID, e.Relation, e.ValidFrom.UTC()
		if e.ValidTo != nil {
			t := e.ValidTo.UTC()
			to[i] = &t
		}
	}
	return
}

func (r resourceRepo) InsertEdges(ctx context.Context, es []model.ResourceEdge) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}
	return r.s.do(ctx, func(tx pgx.Tx) error {
		for start := 0; start < len(es); start += insertChunk {
			p, c, rel, from, to := edgeArrays(es[start:min(start+insertChunk, len(es))])
			if _, err := tx.Exec(ctx, `INSERT INTO resource_edges (org_id, parent_id, child_id, relation, valid_from, valid_to)
				SELECT $1::uuid, p::uuid, c::uuid, rel, vf, vt FROM unnest($2::text[], $3::text[], $4::text[], $5::timestamptz[], $6::timestamptz[]) AS t(p, c, rel, vf, vt)
				ON CONFLICT DO NOTHING`, org, p, c, rel, from, to); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r resourceRepo) CloseEdges(ctx context.Context, es []model.ResourceEdge, at time.Time) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}
	if len(es) == 0 {
		return nil
	}
	p, c, rel, _, _ := edgeArrays(es)
	return r.s.do(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE resource_edges e SET valid_to = $5
			FROM unnest($2::text[], $3::text[], $4::text[]) AS t(p, c, rel)
			WHERE e.org_id = $1::uuid AND e.valid_to IS NULL AND e.parent_id = t.p::uuid AND e.child_id = t.c::uuid AND e.relation = t.rel`,
			org, p, c, rel, at.UTC())
		return err
	})
}

func (r resourceRepo) Count(ctx context.Context) (int, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return 0, err
	}
	var n int
	err = r.s.do(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM resources WHERE org_id = $1::uuid AND valid_to IS NULL", org).Scan(&n)
	})
	return n, err
}
