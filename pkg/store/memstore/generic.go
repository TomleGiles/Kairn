// Package memstore est une implémentation en mémoire de store.Store, utilisée
// par les tests et le mode démo. Elle applique les mêmes règles d'isolation
// que PostgreSQL : toute opération exige une organisation dans le contexte.
package memstore

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kairn-io/kairn/pkg/ids"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/tenancy"
)

// crud est un dépôt générique indexé par ID et filtré par OrgID (réflexion sur les champs).
type crud[T any] struct {
	mu   sync.RWMutex
	rows map[string]T
	now  func() time.Time
}

func newCRUD[T any](now func() time.Time) *crud[T] {
	return &crud[T]{rows: map[string]T{}, now: now}
}

func field(v any, name string) reflect.Value {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	return rv.FieldByName(name)
}

func strField(v any, name string) string {
	f := field(v, name)
	if !f.IsValid() {
		return ""
	}
	switch f.Kind() {
	case reflect.String:
		return f.String()
	case reflect.Pointer:
		if f.IsNil() {
			return ""
		}
		if f.Elem().Kind() == reflect.String {
			return f.Elem().String()
		}
	}
	return fmt.Sprint(f.Interface())
}

func setStr(v any, name, val string) {
	f := field(v, name)
	if f.IsValid() && f.CanSet() && f.Kind() == reflect.String {
		f.SetString(val)
	}
}

func setTimeIfZero(v any, name string, t time.Time) {
	f := field(v, name)
	if f.IsValid() && f.CanSet() && f.Type() == reflect.TypeOf(time.Time{}) && f.Interface().(time.Time).IsZero() {
		f.Set(reflect.ValueOf(t))
	}
}

// matches compare les filtres (clé = tag json) avec les champs de v.
func matches(v any, filters map[string]string) bool {
	if len(filters) == 0 {
		return true
	}
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	rt := rv.Type()
	for key, want := range filters {
		found := false
		for i := 0; i < rt.NumField(); i++ {
			tag := strings.Split(rt.Field(i).Tag.Get("json"), ",")[0]
			if tag != key {
				continue
			}
			found = true
			f := rv.Field(i)
			var got string
			switch {
			case f.Kind() == reflect.Pointer && f.IsNil():
				got = ""
			case f.Kind() == reflect.Pointer:
				got = fmt.Sprint(f.Elem().Interface())
			default:
				got = fmt.Sprint(f.Interface())
			}
			if got != want {
				return false
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (c *crud[T]) Get(ctx context.Context, id string) (T, error) {
	var zero T
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return zero, err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.rows[id]
	if !ok || strField(&v, "OrgID") != org {
		return zero, store.ErrNotFound
	}
	return v, nil
}

func (c *crud[T]) List(ctx context.Context, q store.ListQuery) ([]T, error) {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return nil, err
	}
	q = q.Normalize()
	c.mu.RLock()
	defer c.mu.RUnlock()
	keys := make([]string, 0, len(c.rows))
	for k, v := range c.rows {
		if strField(&v, "OrgID") == org && k > q.Cursor && matches(&v, q.Filters) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	if len(keys) > q.Limit {
		keys = keys[:q.Limit]
	}
	out := make([]T, 0, len(keys))
	for _, k := range keys {
		out = append(out, c.rows[k])
	}
	return out, nil
}

// all renvoie toutes les lignes de l'organisation (sans pagination).
func (c *crud[T]) all(ctx context.Context, pred func(*T) bool) ([]T, error) {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return nil, err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	keys := make([]string, 0, len(c.rows))
	for k, v := range c.rows {
		if strField(&v, "OrgID") == org && (pred == nil || pred(&v)) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	out := make([]T, 0, len(keys))
	for _, k := range keys {
		out = append(out, c.rows[k])
	}
	return out, nil
}

func (c *crud[T]) Create(ctx context.Context, v *T) error {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return err
	}
	switch strField(v, "OrgID") {
	case "":
		setStr(v, "OrgID", org)
	case org:
	default:
		return tenancy.ErrCrossOrg
	}
	if strField(v, "ID") == "" {
		setStr(v, "ID", ids.New())
	}
	setTimeIfZero(v, "CreatedAt", c.now())
	setTimeIfZero(v, "UpdatedAt", c.now())
	id := strField(v, "ID")
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.rows[id]; exists {
		return store.ErrConflict
	}
	c.rows[id] = *v
	return nil
}

func (c *crud[T]) Update(ctx context.Context, v *T) error {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return err
	}
	if strField(v, "OrgID") != org {
		return tenancy.ErrCrossOrg
	}
	id := strField(v, "ID")
	c.mu.Lock()
	defer c.mu.Unlock()
	cur, ok := c.rows[id]
	if !ok || strField(&cur, "OrgID") != org {
		return store.ErrNotFound
	}
	if f := field(v, "UpdatedAt"); f.IsValid() && f.CanSet() {
		f.Set(reflect.ValueOf(c.now()))
	}
	c.rows[id] = *v
	return nil
}

func (c *crud[T]) Delete(ctx context.Context, id string) error {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	cur, ok := c.rows[id]
	if !ok || strField(&cur, "OrgID") != org {
		return store.ErrNotFound
	}
	delete(c.rows, id)
	return nil
}

// deleteWhere supprime les lignes de l'organisation vérifiant pred (cascade).
func (c *crud[T]) deleteWhere(org string, pred func(*T) bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, v := range c.rows {
		if strField(&v, "OrgID") == org && pred(&v) {
			delete(c.rows, k)
		}
	}
}
