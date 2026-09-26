package pgstore

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kairn-io/kairn/pkg/ids"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/tenancy"
)

// crud est le dépôt générique d'une entité portant ID et OrgID.
type crud[T any] struct {
	s     *Store
	table string
	m     *tableMap
}

func newCRUD[T any](s *Store, table string) *crud[T] {
	return &crud[T]{s: s, table: table, m: s.mapOf(table, reflect.TypeFor[T]())}
}

func (c *crud[T]) field(v *T, name string) reflect.Value {
	return reflect.ValueOf(v).Elem().FieldByName(name)
}

// query lit des lignes complètes de T.
func (c *crud[T]) query(ctx context.Context, tx pgx.Tx, where string, args ...any) ([]T, error) {
	return queryRows[T](ctx, tx, c.m, "SELECT "+c.m.selectList("")+" FROM "+c.table+" "+where, args...)
}

func queryRows[T any](ctx context.Context, tx pgx.Tx, m *tableMap, sql string, args ...any) ([]T, error) {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		sc := m.scanner()
		if err := rows.Scan(sc.dests...); err != nil {
			return nil, fmt.Errorf("pgstore: scan %s: %w", m.table, err)
		}
		var v T
		if err := sc.into(reflect.ValueOf(&v).Elem()); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (c *crud[T]) Get(ctx context.Context, id string) (T, error) {
	var zero T
	org, err := orgOf(ctx)
	if err != nil {
		return zero, err
	}
	if !ids.Valid(id) {
		return zero, store.ErrNotFound
	}
	var out []T
	err = c.s.do(ctx, func(tx pgx.Tx) error {
		out, err = c.query(ctx, tx, "WHERE org_id = $1::uuid AND id = $2::uuid", org, id)
		return err
	})
	if err != nil {
		return zero, err
	}
	if len(out) == 0 {
		return zero, store.ErrNotFound
	}
	return out[0], nil
}

// filterSQL traduit les filtres (clé = tag json) en conditions ; une clé inconnue ne correspond à rien.
func (c *crud[T]) filterSQL(filters map[string]string, args *[]any) (string, bool) {
	var conds []string
	keys := make([]string, 0, len(filters))
	for k := range filters {
		keys = append(keys, k)
	}
	// Ordre stable des paramètres.
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	for _, k := range keys {
		col, ok := c.m.byJSON[k]
		if !ok {
			return "", false
		}
		*args = append(*args, filters[k])
		n := "$" + strconv.Itoa(len(*args))
		if c.m.fields[c.m.byCol[col]].col.nullable {
			conds = append(conds, "coalesce("+col+"::text, '') = "+n)
		} else {
			conds = append(conds, col+"::text = "+n)
		}
	}
	return strings.Join(conds, " AND "), true
}

func (c *crud[T]) List(ctx context.Context, q store.ListQuery) ([]T, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}
	q = q.Normalize()
	args := []any{org}
	where := "WHERE org_id = $1::uuid"
	if q.Cursor != "" {
		args = append(args, q.Cursor)
		if ids.Valid(q.Cursor) {
			where += " AND id > $" + strconv.Itoa(len(args)) + "::uuid"
		} else {
			where += " AND id::text > $" + strconv.Itoa(len(args))
		}
	}
	cond, ok := c.filterSQL(q.Filters, &args)
	if !ok {
		return []T{}, nil
	}
	if cond != "" {
		where += " AND " + cond
	}
	args = append(args, q.Limit)
	var out []T
	err = c.s.do(ctx, func(tx pgx.Tx) error {
		out, err = c.query(ctx, tx, where+" ORDER BY id LIMIT $"+strconv.Itoa(len(args)), args...)
		return err
	})
	return out, err
}

// all renvoie toutes les lignes de l'organisation vérifiant la condition SQL.
func (c *crud[T]) all(ctx context.Context, cond, order string, args ...any) ([]T, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}
	where := "WHERE org_id = $1::uuid"
	if cond != "" {
		where += " AND (" + cond + ")"
	}
	if order == "" {
		order = "id"
	}
	var out []T
	err = c.s.do(ctx, func(tx pgx.Tx) error {
		out, err = c.query(ctx, tx, where+" ORDER BY "+order, append([]any{org}, args...)...)
		return err
	})
	return out, err
}

func (c *crud[T]) prepareCreate(ctx context.Context, v *T) (string, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return "", err
	}
	if f := c.field(v, "OrgID"); f.IsValid() {
		switch f.String() {
		case "":
			f.SetString(org)
		case org:
		default:
			return "", tenancy.ErrCrossOrg
		}
	}
	if f := c.field(v, "ID"); f.IsValid() && f.String() == "" {
		f.SetString(ids.New())
	}
	now := c.s.now()
	for _, name := range []string{"CreatedAt", "UpdatedAt"} {
		if f := c.field(v, name); f.IsValid() && f.Type() == timeType && f.Interface().(time.Time).IsZero() {
			f.Set(reflect.ValueOf(now))
		}
	}
	return org, nil
}

func (c *crud[T]) insert(ctx context.Context, tx pgx.Tx, v *T, suffix string) error {
	cols, holders, args, err := c.m.values(reflect.ValueOf(v).Elem(), nil)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO "+c.table+" ("+strings.Join(cols, ", ")+") VALUES ("+strings.Join(holders, ", ")+") "+suffix, args...)
	return err
}

func (c *crud[T]) Create(ctx context.Context, v *T) error {
	if _, err := c.prepareCreate(ctx, v); err != nil {
		return err
	}
	return c.s.do(ctx, func(tx pgx.Tx) error { return c.insert(ctx, tx, v, "") })
}

func (c *crud[T]) Update(ctx context.Context, v *T) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}
	if f := c.field(v, "OrgID"); f.IsValid() && f.String() != org {
		return tenancy.ErrCrossOrg
	}
	id := c.field(v, "ID").String()
	if !ids.Valid(id) {
		return store.ErrNotFound
	}
	if f := c.field(v, "UpdatedAt"); f.IsValid() && f.Type() == timeType {
		f.Set(reflect.ValueOf(c.s.now()))
	}
	cols, holders, args, err := c.m.values(reflect.ValueOf(v).Elem(), map[string]bool{"id": true, "org_id": true})
	if err != nil {
		return err
	}
	sets := make([]string, len(cols))
	for i := range cols {
		sets[i] = cols[i] + " = " + holders[i]
	}
	args = append(args, org, id)
	sql := "UPDATE " + c.table + " SET " + strings.Join(sets, ", ") +
		" WHERE org_id = $" + strconv.Itoa(len(args)-1) + "::uuid AND id = $" + strconv.Itoa(len(args)) + "::uuid"
	return c.s.do(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, sql, args...)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return store.ErrNotFound
		}
		return nil
	})
}

func (c *crud[T]) Delete(ctx context.Context, id string) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}
	if !ids.Valid(id) {
		return store.ErrNotFound
	}
	return c.s.do(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, "DELETE FROM "+c.table+" WHERE org_id = $1::uuid AND id = $2::uuid", org, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return store.ErrNotFound
		}
		return nil
	})
}
