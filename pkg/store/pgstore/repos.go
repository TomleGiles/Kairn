package pgstore

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/ids"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/tenancy"
)

// ------------------------------------------------------------------ organisations

type orgRepo struct{ s *Store }

func (r orgRepo) m() *tableMap {
	return r.s.mapOf("organizations", reflect.TypeFor[model.Organization]())
}

func (r orgRepo) Get(ctx context.Context, id string) (model.Organization, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return model.Organization{}, err
	}
	if !ids.Valid(id) {
		return model.Organization{}, store.ErrNotFound
	}
	var out []model.Organization
	err = r.s.do(ctx, func(tx pgx.Tx) error {
		out, err = queryRows[model.Organization](ctx, tx, r.m(), "SELECT "+r.m().selectList("")+
			" FROM organizations WHERE id = $1::uuid AND (id = $2::uuid OR parent_org_id = $2::uuid)", id, org)
		return err
	})
	if err != nil {
		return model.Organization{}, err
	}
	if len(out) == 0 {
		return model.Organization{}, store.ErrNotFound
	}
	return out[0], nil
}

func (r orgRepo) Create(ctx context.Context, o *model.Organization) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}
	if o.ID == "" {
		o.ID = org
	}
	if o.ID != org {
		return tenancy.ErrCrossOrg
	}
	if o.CreatedAt.IsZero() {
		o.CreatedAt = r.s.now()
	}
	o.UpdatedAt = o.CreatedAt
	cols, holders, args, err := r.m().values(reflect.ValueOf(o).Elem(), nil)
	if err != nil {
		return err
	}
	return r.s.do(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO organizations ("+strings.Join(cols, ", ")+") VALUES ("+strings.Join(holders, ", ")+")", args...)
		return err
	})
}

func (r orgRepo) Update(ctx context.Context, o *model.Organization) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}
	if o.ID != org {
		return tenancy.ErrCrossOrg
	}
	o.UpdatedAt = r.s.now()
	cols, holders, args, err := r.m().values(reflect.ValueOf(o).Elem(), map[string]bool{"id": true, "created_at": true})
	if err != nil {
		return err
	}
	sets := make([]string, len(cols))
	for i := range cols {
		sets[i] = cols[i] + " = " + holders[i]
	}
	args = append(args, org)
	return r.s.do(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, "UPDATE organizations SET "+strings.Join(sets, ", ")+" WHERE id = $"+strconv.Itoa(len(args))+"::uuid", args...)
		if err == nil && tag.RowsAffected() == 0 {
			return store.ErrNotFound
		}
		return err
	})
}

func (r orgRepo) ListChildren(ctx context.Context) ([]model.Organization, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}
	var out []model.Organization
	err = r.s.do(ctx, func(tx pgx.Tx) error {
		out, err = queryRows[model.Organization](ctx, tx, r.m(), "SELECT "+r.m().selectList("")+
			" FROM organizations WHERE parent_org_id = $1::uuid ORDER BY name, id", org)
		return err
	})
	return out, err
}

// ListForUser liste les organisations d'un utilisateur ; le contexte RLS porte cet utilisateur.
func (r orgRepo) ListForUser(ctx context.Context, userID string) ([]model.Organization, error) {
	if !ids.Valid(userID) {
		return nil, nil
	}
	ctx = tenancy.WithUser(ctx, userID)
	var out []model.Organization
	var err error
	err = r.s.do(ctx, func(tx pgx.Tx) error {
		out, err = queryRows[model.Organization](ctx, tx, r.m(), "SELECT "+r.m().selectList("o")+
			" FROM organizations o JOIN memberships m ON m.org_id = o.id WHERE m.user_id = $1::uuid ORDER BY o.name, o.id", userID)
		return err
	})
	return out, err
}

// Delete supprime l'organisation courante ; les clés étrangères ON DELETE CASCADE
// emportent toutes ses données relationnelles.
func (r orgRepo) Delete(ctx context.Context, id string) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}
	if id != org {
		return tenancy.ErrCrossOrg
	}
	return r.s.do(ctx, func(tx pgx.Tx) error {
		var children int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM organizations WHERE parent_org_id = $1::uuid", org).Scan(&children); err != nil {
			return err
		}
		if children > 0 {
			return store.ErrConflict
		}
		tag, err := tx.Exec(ctx, "DELETE FROM organizations WHERE id = $1::uuid", org)
		if err == nil && tag.RowsAffected() == 0 {
			return store.ErrNotFound
		}
		return err
	})
}

// ------------------------------------------------------------------ utilisateurs

type userRepo struct{ s *Store }

func (r userRepo) m() *tableMap { return r.s.mapOf("users", reflect.TypeFor[model.User]()) }

func (r userRepo) Get(ctx context.Context, id string) (model.User, error) {
	if !ids.Valid(id) {
		return model.User{}, store.ErrNotFound
	}
	// Lecture de soi-même ou d'un membre de l'organisation courante (RLS) ;
	// sans utilisateur dans le contexte, l'identité demandée fait foi.
	if tenancy.UserID(ctx) == "" {
		ctx = tenancy.WithUser(ctx, id)
	}
	var out []model.User
	var err error
	err = r.s.do(ctx, func(tx pgx.Tx) error {
		out, err = queryRows[model.User](ctx, tx, r.m(), "SELECT "+r.m().selectList("")+" FROM users WHERE id = $1::uuid", id)
		return err
	})
	if err != nil {
		return model.User{}, err
	}
	if len(out) == 0 {
		return model.User{}, store.ErrNotFound
	}
	return out[0], nil
}

func (r userRepo) Create(ctx context.Context, u *model.User) error {
	if u.ID == "" {
		u.ID = ids.New()
	}
	if u.CreatedAt.IsZero() {
		u.CreatedAt = r.s.now()
	}
	u.Email = strings.TrimSpace(u.Email)
	cols, holders, args, err := r.m().values(reflect.ValueOf(u).Elem(), nil)
	if err != nil {
		return err
	}
	// La politique users_insert exige que la ligne créée soit l'utilisateur du contexte.
	ctx = tenancy.WithUser(ctx, u.ID)
	return r.s.do(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO users ("+strings.Join(cols, ", ")+") VALUES ("+strings.Join(holders, ", ")+")", args...)
		return err
	})
}

func (r userRepo) Update(ctx context.Context, u *model.User) error {
	if !ids.Valid(u.ID) {
		return store.ErrNotFound
	}
	cols, holders, args, err := r.m().values(reflect.ValueOf(u).Elem(), map[string]bool{"id": true, "created_at": true})
	if err != nil {
		return err
	}
	sets := make([]string, len(cols))
	for i := range cols {
		sets[i] = cols[i] + " = " + holders[i]
	}
	args = append(args, u.ID)
	ctx = tenancy.WithUser(ctx, u.ID)
	return r.s.do(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, "UPDATE users SET "+strings.Join(sets, ", ")+" WHERE id = $"+strconv.Itoa(len(args))+"::uuid", args...)
		if err == nil && tag.RowsAffected() == 0 {
			return store.ErrNotFound
		}
		return err
	})
}

// ------------------------------------------------------------------ appartenances

type membershipRepo struct{ s *Store }

const membershipCols = "m.org_id::text, m.user_id::text, m.role, m.scopes, m.created_at, coalesce(u.email, ''), coalesce(u.name, '')"

func scanMemberships(rows pgx.Rows) ([]model.Membership, error) {
	defer rows.Close()
	var out []model.Membership
	for rows.Next() {
		var m model.Membership
		var role string
		if err := rows.Scan(&m.OrgID, &m.UserID, &role, &m.Scopes, &m.CreatedAt, &m.Email, &m.Name); err != nil {
			return nil, err
		}
		m.Role = model.Role(role)
		m.CreatedAt = m.CreatedAt.UTC()
		if m.Scopes == nil {
			m.Scopes = []string{}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r membershipRepo) List(ctx context.Context) ([]model.Membership, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}
	var out []model.Membership
	err = r.s.do(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, "SELECT "+membershipCols+" FROM memberships m LEFT JOIN users u ON u.id = m.user_id WHERE m.org_id = $1::uuid ORDER BY u.email", org)
		if err != nil {
			return err
		}
		out, err = scanMemberships(rows)
		return err
	})
	return out, err
}

func (r membershipRepo) Get(ctx context.Context, userID string) (model.Membership, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return model.Membership{}, err
	}
	if !ids.Valid(userID) {
		return model.Membership{}, store.ErrNotFound
	}
	var out []model.Membership
	err = r.s.do(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, "SELECT "+membershipCols+" FROM memberships m LEFT JOIN users u ON u.id = m.user_id WHERE m.org_id = $1::uuid AND m.user_id = $2::uuid", org, userID)
		if err != nil {
			return err
		}
		out, err = scanMemberships(rows)
		return err
	})
	if err != nil {
		return model.Membership{}, err
	}
	if len(out) == 0 {
		return model.Membership{}, store.ErrNotFound
	}
	return out[0], nil
}

func (r membershipRepo) Upsert(ctx context.Context, m *model.Membership) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}
	if m.OrgID == "" {
		m.OrgID = org
	}
	if m.OrgID != org {
		return tenancy.ErrCrossOrg
	}
	if !ids.Valid(m.UserID) {
		return store.ErrNotFound
	}
	if m.Scopes == nil {
		m.Scopes = []string{}
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = r.s.now()
	}
	return r.s.do(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `INSERT INTO memberships (org_id, user_id, role, scopes, created_at) VALUES ($1::uuid, $2::uuid, $3, $4, $5)
			ON CONFLICT (org_id, user_id) DO UPDATE SET role = EXCLUDED.role, scopes = EXCLUDED.scopes
			RETURNING created_at`, org, m.UserID, string(m.Role), m.Scopes, m.CreatedAt).Scan(&m.CreatedAt)
	})
}

func (r membershipRepo) Delete(ctx context.Context, userID string) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}
	if !ids.Valid(userID) {
		return store.ErrNotFound
	}
	return r.s.do(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, "DELETE FROM memberships WHERE org_id = $1::uuid AND user_id = $2::uuid", org, userID)
		if err == nil && tag.RowsAffected() == 0 {
			return store.ErrNotFound
		}
		return err
	})
}

// ForUser liste les appartenances d'un utilisateur ; le contexte RLS porte cet utilisateur.
func (r membershipRepo) ForUser(ctx context.Context, userID string) ([]model.Membership, error) {
	if !ids.Valid(userID) {
		return nil, nil
	}
	ctx = tenancy.WithUser(ctx, userID)
	var out []model.Membership
	err := r.s.do(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, "SELECT "+membershipCols+" FROM memberships m LEFT JOIN users u ON u.id = m.user_id WHERE m.user_id = $1::uuid ORDER BY m.org_id", userID)
		if err != nil {
			return err
		}
		out, err = scanMemberships(rows)
		return err
	})
	return out, err
}

// ------------------------------------------------------------------ jetons, audit, abonnement

type tokenRepo struct{ c *crud[model.APIToken] }

func (r tokenRepo) List(ctx context.Context) ([]model.APIToken, error) { return r.c.all(ctx, "", "id") }
func (r tokenRepo) Create(ctx context.Context, t *model.APIToken) error {
	if t.Scopes == nil {
		t.Scopes = []string{}
	}
	return r.c.Create(ctx, t)
}

func (r tokenRepo) setTime(ctx context.Context, col, id string, at time.Time) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}
	if !ids.Valid(id) {
		return store.ErrNotFound
	}
	return r.c.s.do(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, "UPDATE api_tokens SET "+col+" = $1 WHERE org_id = $2::uuid AND id = $3::uuid", at.UTC(), org, id)
		if err == nil && tag.RowsAffected() == 0 {
			return store.ErrNotFound
		}
		return err
	})
}

func (r tokenRepo) Revoke(ctx context.Context, id string, at time.Time) error {
	return r.setTime(ctx, "revoked_at", id, at)
}
func (r tokenRepo) Touch(ctx context.Context, id string, at time.Time) error {
	return r.setTime(ctx, "last_used_at", id, at)
}

type auditRepo struct{ c *crud[model.AuditEvent] }

func (r auditRepo) Append(ctx context.Context, e *model.AuditEvent) error {
	if e.At.IsZero() {
		e.At = r.c.s.now()
	}
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	return r.c.Create(ctx, e)
}

func (r auditRepo) List(ctx context.Context, f store.AuditFilter) ([]model.AuditEvent, error) {
	var conds []string
	var args []any
	add := func(sql string, v any) {
		args = append(args, v)
		conds = append(conds, strings.ReplaceAll(sql, "?", "$"+strconv.Itoa(len(args)+1)))
	}
	if !f.From.IsZero() {
		add("at >= ?", f.From.UTC())
	}
	if !f.To.IsZero() {
		add("at < ?", f.To.UTC())
	}
	if f.Action != "" {
		add("starts_with(action, ?)", f.Action)
	}
	if f.ActorID != "" {
		add("actor_id = ?", f.ActorID)
	}
	if f.Cursor != "" {
		add("id::text < ?", f.Cursor)
	}
	q := store.ListQuery{Limit: f.Limit}.Normalize()
	order := "id DESC LIMIT " + strconv.Itoa(q.Limit)
	return r.c.all(ctx, strings.Join(conds, " AND "), order, args...)
}

type subRepo struct{ s *Store }

func (r subRepo) m() *tableMap {
	return r.s.mapOf("subscriptions", reflect.TypeFor[model.Subscription]())
}

func (r subRepo) Get(ctx context.Context) (model.Subscription, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return model.Subscription{}, err
	}
	var out []model.Subscription
	err = r.s.do(ctx, func(tx pgx.Tx) error {
		out, err = queryRows[model.Subscription](ctx, tx, r.m(), "SELECT "+r.m().selectList("")+" FROM subscriptions WHERE org_id = $1::uuid", org)
		return err
	})
	if err != nil {
		return model.Subscription{}, err
	}
	if len(out) == 0 {
		return model.Subscription{}, store.ErrNotFound
	}
	return out[0], nil
}

func (r subRepo) Upsert(ctx context.Context, sub *model.Subscription) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}
	sub.OrgID = org
	sub.UpdatedAt = r.s.now()
	return r.s.do(ctx, func(tx pgx.Tx) error { return upsert(ctx, tx, r.m(), sub, []string{"org_id"}, nil) })
}

// upsert insère v ou met à jour toutes ses colonnes en cas de conflit sur key.
func upsert(ctx context.Context, tx pgx.Tx, m *tableMap, v any, key []string, keep map[string]bool) error {
	cols, holders, args, err := m.values(reflect.ValueOf(v).Elem(), nil)
	if err != nil {
		return err
	}
	isKey := map[string]bool{}
	for _, k := range key {
		isKey[k] = true
	}
	var sets []string
	for _, c := range cols {
		if !isKey[c] && !keep[c] {
			sets = append(sets, c+" = EXCLUDED."+c)
		}
	}
	sql := "INSERT INTO " + m.table + " (" + strings.Join(cols, ", ") + ") VALUES (" + strings.Join(holders, ", ") + ") ON CONFLICT (" +
		strings.Join(key, ", ") + ") DO UPDATE SET " + strings.Join(sets, ", ")
	_, err = tx.Exec(ctx, sql, args...)
	return err
}

// ------------------------------------------------------------------ connecteurs

type connectorRepo struct{ *crud[model.Connector] }

func (r connectorRepo) Create(ctx context.Context, c *model.Connector) error {
	if c.Settings == nil {
		c.Settings = map[string]string{}
	}
	return r.crud.Create(ctx, c)
}

func (r connectorRepo) UpdateStatus(ctx context.Context, id string, status model.ConnectorStatus, msg string, syncAt time.Time, success bool) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}
	if !ids.Valid(id) {
		return store.ErrNotFound
	}
	return r.s.do(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE connectors SET status = $1, status_message = $2, last_sync_at = $3,
			last_success_at = CASE WHEN $4 THEN $3 ELSE last_success_at END, updated_at = $5
			WHERE org_id = $6::uuid AND id = $7::uuid`, string(status), msg, syncAt.UTC(), success, r.s.now(), org, id)
		if err == nil && tag.RowsAffected() == 0 {
			return store.ErrNotFound
		}
		return err
	})
}

type runRepo struct{ c *crud[model.ConnectorRun] }

func (r runRepo) Create(ctx context.Context, run *model.ConnectorRun) error {
	return r.c.Create(ctx, run)
}
func (r runRepo) Finish(ctx context.Context, run *model.ConnectorRun) error {
	return r.c.Update(ctx, run)
}
func (r runRepo) ListByConnector(ctx context.Context, connectorID string, limit int) ([]model.ConnectorRun, error) {
	if !ids.Valid(connectorID) {
		return nil, nil
	}
	order := "started_at DESC"
	if limit > 0 {
		order += " LIMIT " + strconv.Itoa(limit)
	}
	return r.c.all(ctx, "connector_id = $2::uuid", order, connectorID)
}

// ------------------------------------------------------------------ tarification

type pricingRepo struct{ s *Store }

func (r pricingRepo) cm() *tableMap {
	return r.s.mapOf("price_catalogs", reflect.TypeFor[model.PriceCatalog]())
}
func (r pricingRepo) im() *tableMap {
	return r.s.mapOf("price_items", reflect.TypeFor[model.PriceItem]())
}

func (r pricingRepo) CreateCatalog(ctx context.Context, c *model.PriceCatalog, items []model.PriceItem) error {
	if c.OrgID == nil {
		if !tenancy.IsSystem(ctx) {
			return tenancy.ErrNoOrg
		}
	} else if err := tenancy.Check(ctx, *c.OrgID); err != nil {
		return err
	}
	if c.ID == "" {
		c.ID = ids.New()
	}
	if c.ImportedAt.IsZero() {
		c.ImportedAt = r.s.now()
	}
	cols, holders, args, err := r.cm().values(reflect.ValueOf(c).Elem(), nil)
	if err != nil {
		return err
	}
	return r.s.do(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO price_catalogs ("+strings.Join(cols, ", ")+") VALUES ("+strings.Join(holders, ", ")+")", args...); err != nil {
			return err
		}
		const chunk = 1000
		for start := 0; start < len(items); start += chunk {
			end := min(start+chunk, len(items))
			n := end - start
			skus, regions, units, prices, currencies, attrs := make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n)
			for i, it := range items[start:end] {
				it.CatalogID = c.ID
				items[start+i].CatalogID = c.ID
				a, err := fieldMap{col: colInfo{dataType: "jsonb"}}.encode(reflect.ValueOf(it.Attributes))
				if err != nil {
					return err
				}
				skus[i], regions[i], units[i], prices[i], currencies[i], attrs[i] = it.SKU, it.Region, it.Unit, it.Price.String(), it.Currency, a.(string)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO price_items (catalog_id, org_id, sku, region, unit, price, currency, attributes)
				SELECT $1::uuid, $2::uuid, s, rg, u, p::numeric, cur, a::jsonb
				FROM unnest($3::text[], $4::text[], $5::text[], $6::text[], $7::text[], $8::text[]) AS t(s, rg, u, p, cur, a)`,
				c.ID, c.OrgID, skus, regions, units, prices, currencies, attrs); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r pricingRepo) ListCatalogs(ctx context.Context) ([]model.PriceCatalog, error) {
	org, _ := tenancy.OrgID(ctx)
	if org != "" && !ids.Valid(org) {
		return nil, store.ErrNotFound
	}
	var out []model.PriceCatalog
	var err error
	err = r.s.do(ctx, func(tx pgx.Tx) error {
		out, err = queryRows[model.PriceCatalog](ctx, tx, r.cm(), "SELECT "+r.cm().selectList("")+
			" FROM price_catalogs WHERE org_id IS NULL OR org_id::text = $1 ORDER BY provider, valid_from, id", org)
		return err
	})
	return out, err
}

func (r pricingRepo) visible(ctx context.Context, tx pgx.Tx, id string) (bool, *string, error) {
	org, _ := tenancy.OrgID(ctx)
	var owner *string
	err := tx.QueryRow(ctx, "SELECT org_id::text FROM price_catalogs WHERE id = $1::uuid AND (org_id IS NULL OR org_id::text = $2)", id, org).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil, nil
	}
	return err == nil, owner, err
}

func (r pricingRepo) Items(ctx context.Context, catalogID string) ([]model.PriceItem, error) {
	if !ids.Valid(catalogID) {
		return nil, store.ErrNotFound
	}
	var out []model.PriceItem
	err := r.s.do(ctx, func(tx pgx.Tx) error {
		ok, owner, err := r.visible(ctx, tx, catalogID)
		if err != nil {
			return err
		}
		if !ok {
			return store.ErrNotFound
		}
		// Filtre explicite sur le propriétaire de la grille (publique ou organisation courante).
		cond := "org_id IS NULL"
		args := []any{catalogID}
		if owner != nil {
			cond = "org_id = $2::uuid"
			args = append(args, *owner)
		}
		out, err = queryRows[model.PriceItem](ctx, tx, r.im(), "SELECT "+r.im().selectList("")+
			" FROM price_items WHERE catalog_id = $1::uuid AND "+cond+" ORDER BY sku, region", args...)
		return err
	})
	return out, err
}

func (r pricingRepo) DeleteCatalog(ctx context.Context, id string) error {
	if !ids.Valid(id) {
		return store.ErrNotFound
	}
	return r.s.do(ctx, func(tx pgx.Tx) error {
		ok, owner, err := r.visible(ctx, tx, id)
		if err != nil {
			return err
		}
		if !ok || (owner == nil && !tenancy.IsSystem(ctx)) {
			return store.ErrNotFound
		}
		if owner == nil {
			_, err = tx.Exec(ctx, "DELETE FROM price_catalogs WHERE id = $1::uuid AND org_id IS NULL", id)
		} else {
			_, err = tx.Exec(ctx, "DELETE FROM price_catalogs WHERE id = $1::uuid AND org_id = $2::uuid", id, *owner)
		}
		return err
	})
}

// Taux de change : données de référence globales (sans org_id ni RLS).
type rateRepo struct{ s *Store }

func (r rateRepo) Upsert(ctx context.Context, rates []model.ExchangeRate) error {
	if len(rates) == 0 {
		return nil
	}
	bases, quotes, days, values := make([]string, len(rates)), make([]string, len(rates)), make([]time.Time, len(rates)), make([]string, len(rates))
	for i, x := range rates {
		bases[i], quotes[i], days[i], values[i] = x.Base, x.Quote, x.Day.UTC(), x.Rate.String()
	}
	return r.s.do(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO exchange_rates (base, quote, day, rate)
			SELECT b, q, d::date, v::numeric FROM unnest($1::text[], $2::text[], $3::timestamptz[], $4::text[]) AS t(b, q, d, v)
			ON CONFLICT (base, quote, day) DO UPDATE SET rate = EXCLUDED.rate`, bases, quotes, days, values)
		return err
	})
}

func (r rateRepo) Get(ctx context.Context, base, quote string, day time.Time) (model.ExchangeRate, error) {
	if base == quote {
		return model.ExchangeRate{Base: base, Quote: quote, Day: day, Rate: decimal.NewFromInt(1)}, nil
	}
	var out model.ExchangeRate
	err := r.s.do(ctx, func(tx pgx.Tx) error {
		var rate string
		if err := tx.QueryRow(ctx, `SELECT base, quote, day, rate::text FROM exchange_rates
			WHERE base = $1 AND quote = $2 AND day <= $3::date ORDER BY day DESC LIMIT 1`, base, quote, day.UTC()).Scan(&out.Base, &out.Quote, &out.Day, &rate); err != nil {
			return err
		}
		out.Day = out.Day.UTC()
		d, err := decimal.NewFromString(rate)
		out.Rate = d
		return err
	})
	return out, err
}

type reconRepo struct{ s *Store }

func (r reconRepo) m() *tableMap {
	return r.s.mapOf("reconciliations", reflect.TypeFor[model.Reconciliation]())
}

func (r reconRepo) Upsert(ctx context.Context, x *model.Reconciliation) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}
	x.OrgID = org
	if x.ComputedAt.IsZero() {
		x.ComputedAt = r.s.now()
	}
	return r.s.do(ctx, func(tx pgx.Tx) error {
		return upsert(ctx, tx, r.m(), x, []string{"org_id", "connector_id", "month"}, nil)
	})
}

func (r reconRepo) List(ctx context.Context) ([]model.Reconciliation, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}
	var out []model.Reconciliation
	err = r.s.do(ctx, func(tx pgx.Tx) error {
		out, err = queryRows[model.Reconciliation](ctx, tx, r.m(), "SELECT "+r.m().selectList("")+
			" FROM reconciliations WHERE org_id = $1::uuid ORDER BY month DESC, connector_id", org)
		return err
	})
	return out, err
}

// ------------------------------------------------------------------ alertes

type alertEventRepo struct{ c *crud[model.AlertEvent] }

func (r alertEventRepo) List(ctx context.Context, q store.ListQuery) ([]model.AlertEvent, error) {
	args := []any{}
	cond, ok := r.c.filterSQL(q.Filters, &args)
	if !ok {
		return []model.AlertEvent{}, nil
	}
	// all() réserve $1 à org_id : décalage des paramètres des filtres.
	cond = shiftParams(cond, 1)
	if q.Cursor != "" {
		args = append(args, q.Cursor)
		c := "id::text < $" + strconv.Itoa(len(args)+1)
		if cond != "" {
			cond += " AND " + c
		} else {
			cond = c
		}
	}
	q = q.Normalize()
	return r.c.all(ctx, cond, "id DESC LIMIT "+strconv.Itoa(q.Limit), args...)
}

func (r alertEventRepo) Get(ctx context.Context, id string) (model.AlertEvent, error) {
	return r.c.Get(ctx, id)
}

func (r alertEventRepo) FindActive(ctx context.Context, fp string) (model.AlertEvent, error) {
	rows, err := r.c.all(ctx, "fingerprint = $2 AND status <> 'resolved'", "id DESC LIMIT 1", fp)
	if err != nil {
		return model.AlertEvent{}, err
	}
	if len(rows) == 0 {
		return model.AlertEvent{}, store.ErrNotFound
	}
	return rows[0], nil
}

func (r alertEventRepo) Create(ctx context.Context, e *model.AlertEvent) error {
	if e.Payload == nil {
		e.Payload = map[string]any{}
	}
	return r.c.Create(ctx, e)
}
func (r alertEventRepo) Update(ctx context.Context, e *model.AlertEvent) error {
	return r.c.Update(ctx, e)
}

// shiftParams renumérote les paramètres $n d'une condition de delta.
func shiftParams(cond string, delta int) string {
	var b strings.Builder
	for i := 0; i < len(cond); i++ {
		if cond[i] == '$' {
			j := i + 1
			for j < len(cond) && cond[j] >= '0' && cond[j] <= '9' {
				j++
			}
			if j > i+1 {
				n, _ := strconv.Atoi(cond[i+1 : j])
				b.WriteString("$" + strconv.Itoa(n+delta))
				i = j - 1
				continue
			}
		}
		b.WriteByte(cond[i])
	}
	return b.String()
}

// ------------------------------------------------------------------ recommandations, anomalies, prévisions

type recoRepo struct{ c *crud[model.Recommendation] }

func (r recoRepo) List(ctx context.Context, f store.RecommendationFilter) ([]model.Recommendation, error) {
	var conds []string
	args := []any{}
	next := func() string { return "$" + strconv.Itoa(len(args)+1) } // $1 = org_id
	if len(f.Status) > 0 {
		args = append(args, f.Status)
		conds = append(conds, "status = ANY("+next()+"::text[])")
	}
	if len(f.Types) > 0 {
		args = append(args, f.Types)
		conds = append(conds, "type = ANY("+next()+"::text[])")
	}
	if f.ResourceID != "" {
		if !ids.Valid(f.ResourceID) {
			return []model.Recommendation{}, nil
		}
		args = append(args, f.ResourceID)
		conds = append(conds, "resource_id = "+next()+"::uuid")
	}
	if f.Cursor != "" && ids.Valid(f.Cursor) {
		args = append(args, f.Cursor)
		p := next()
		cur := "(SELECT savings_monthly FROM recommendations WHERE org_id = $1::uuid AND id = " + p + "::uuid)"
		// Pagination par clé sur (économie décroissante, id croissant).
		conds = append(conds, "(savings_monthly < "+cur+" OR (savings_monthly = "+cur+" AND id > "+p+"::uuid))")
	}
	q := store.ListQuery{Limit: f.Limit}.Normalize()
	return r.c.all(ctx, strings.Join(conds, " AND "), "savings_monthly DESC, id LIMIT "+strconv.Itoa(q.Limit), args...)
}

func (r recoRepo) Get(ctx context.Context, id string) (model.Recommendation, error) {
	return r.c.Get(ctx, id)
}

// Upsert crée ou rafraîchit par empreinte ; le statut décidé par l'utilisateur est conservé.
func (r recoRepo) Upsert(ctx context.Context, x *model.Recommendation) error {
	org, err := r.c.prepareCreate(ctx, x)
	if err != nil {
		return err
	}
	if x.Status == "" {
		x.Status = model.RecoOpen
	}
	if x.Evidence == nil {
		x.Evidence = map[string]any{}
	}
	cols, holders, args, err := r.c.m.values(reflect.ValueOf(x).Elem(), nil)
	if err != nil {
		return err
	}
	keep := map[string]bool{"id": true, "org_id": true, "fingerprint": true, "status": true, "status_reason": true, "postponed_until": true,
		"applied_at": true, "created_at": true, "measured_savings_monthly": true}
	var sets []string
	for _, c := range cols {
		if !keep[c] {
			sets = append(sets, c+" = EXCLUDED."+c)
		}
	}
	sets = append(sets, "measured_savings_monthly = coalesce(EXCLUDED.measured_savings_monthly, recommendations.measured_savings_monthly)")
	sql := "INSERT INTO recommendations (" + strings.Join(cols, ", ") + ") VALUES (" + strings.Join(holders, ", ") + ")" +
		" ON CONFLICT (org_id, fingerprint) DO UPDATE SET " + strings.Join(sets, ", ") +
		" RETURNING " + r.c.m.selectList("recommendations")
	_ = org
	return r.c.s.do(ctx, func(tx pgx.Tx) error {
		out, err := queryRows[model.Recommendation](ctx, tx, r.c.m, sql, args...)
		if err != nil {
			return err
		}
		if len(out) == 1 {
			*x = out[0]
		}
		return nil
	})
}

func (r recoRepo) Update(ctx context.Context, x *model.Recommendation) error {
	return r.c.Update(ctx, x)
}

type anomalyRepo struct{ c *crud[model.Anomaly] }

func (r anomalyRepo) List(ctx context.Context, from, to time.Time, status string) ([]model.Anomaly, error) {
	var conds []string
	args := []any{}
	next := func() string { return "$" + strconv.Itoa(len(args)+1) }
	if !from.IsZero() {
		args = append(args, from.UTC())
		conds = append(conds, "window_end >= "+next())
	}
	if !to.IsZero() {
		args = append(args, to.UTC())
		conds = append(conds, "window_start < "+next())
	}
	if status != "" {
		args = append(args, status)
		conds = append(conds, "status = "+next())
	}
	return r.c.all(ctx, strings.Join(conds, " AND "), "window_start DESC, id", args...)
}

func (r anomalyRepo) Get(ctx context.Context, id string) (model.Anomaly, error) {
	return r.c.Get(ctx, id)
}

func (r anomalyRepo) Upsert(ctx context.Context, a *model.Anomaly) error {
	if _, err := r.c.prepareCreate(ctx, a); err != nil {
		return err
	}
	if a.Status == "" {
		a.Status = "open"
	}
	cols, holders, args, err := r.c.m.values(reflect.ValueOf(a).Elem(), nil)
	if err != nil {
		return err
	}
	keep := map[string]bool{"id": true, "org_id": true, "series_key": true, "window_start": true, "status": true, "created_at": true,
		"explanation": true, "explanation_sources": true}
	var sets []string
	for _, c := range cols {
		if !keep[c] {
			sets = append(sets, c+" = EXCLUDED."+c)
		}
	}
	// Une explication vide ne remplace pas l'explication existante.
	sets = append(sets,
		"explanation = CASE WHEN EXCLUDED.explanation = '' THEN anomalies.explanation ELSE EXCLUDED.explanation END",
		"explanation_sources = CASE WHEN EXCLUDED.explanation = '' THEN anomalies.explanation_sources ELSE EXCLUDED.explanation_sources END")
	sql := "INSERT INTO anomalies (" + strings.Join(cols, ", ") + ") VALUES (" + strings.Join(holders, ", ") + ")" +
		" ON CONFLICT (org_id, series_key, window_start) DO UPDATE SET " + strings.Join(sets, ", ") +
		" RETURNING " + r.c.m.selectList("anomalies")
	return r.c.s.do(ctx, func(tx pgx.Tx) error {
		out, err := queryRows[model.Anomaly](ctx, tx, r.c.m, sql, args...)
		if err == nil && len(out) == 1 {
			*a = out[0]
		}
		return err
	})
}

func (r anomalyRepo) Update(ctx context.Context, a *model.Anomaly) error { return r.c.Update(ctx, a) }

type forecastRepo struct{ s *Store }

func (r forecastRepo) m() *tableMap { return r.s.mapOf("forecasts", reflect.TypeFor[model.Forecast]()) }

func (r forecastRepo) Upsert(ctx context.Context, f *model.Forecast) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}
	f.OrgID = org
	return r.s.do(ctx, func(tx pgx.Tx) error { return upsert(ctx, tx, r.m(), f, []string{"org_id", "node_id"}, nil) })
}

func (r forecastRepo) list(ctx context.Context, cond string, args ...any) ([]model.Forecast, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}
	var out []model.Forecast
	err = r.s.do(ctx, func(tx pgx.Tx) error {
		out, err = queryRows[model.Forecast](ctx, tx, r.m(), "SELECT "+r.m().selectList("")+
			" FROM forecasts WHERE org_id = $1::uuid"+cond+" ORDER BY node_id", append([]any{org}, args...)...)
		return err
	})
	return out, err
}

func (r forecastRepo) Get(ctx context.Context, nodeID string) (model.Forecast, error) {
	out, err := r.list(ctx, " AND node_id = $2", nodeID)
	if err != nil {
		return model.Forecast{}, err
	}
	if len(out) == 0 {
		return model.Forecast{}, store.ErrNotFound
	}
	return out[0], nil
}

func (r forecastRepo) List(ctx context.Context) ([]model.Forecast, error) { return r.list(ctx, "") }

// ------------------------------------------------------------------ uptime, rapports, IA

type statusPageRepo struct{ *crud[model.StatusPage] }

type incidentRepo struct{ *crud[model.Incident] }

func (r incidentRepo) OpenForCheck(ctx context.Context, checkID string) (model.Incident, error) {
	if !ids.Valid(checkID) {
		return model.Incident{}, store.ErrNotFound
	}
	rows, err := r.all(ctx, "check_id = $2::uuid AND status = 'open'", "id DESC LIMIT 1", checkID)
	if err != nil {
		return model.Incident{}, err
	}
	if len(rows) == 0 {
		return model.Incident{}, store.ErrNotFound
	}
	return rows[0], nil
}

func (r incidentRepo) List(ctx context.Context, q store.ListQuery) ([]model.Incident, error) {
	args := []any{}
	cond, ok := r.filterSQL(q.Filters, &args)
	if !ok {
		return []model.Incident{}, nil
	}
	q = q.Normalize()
	return r.all(ctx, shiftParams(cond, 1), "started_at DESC, id LIMIT "+strconv.Itoa(q.Limit), args...)
}

type reportRepo struct{ *crud[model.Report] }

func (r reportRepo) GetByPeriod(ctx context.Context, kind, period string) (model.Report, error) {
	rows, err := r.all(ctx, "kind = $2 AND period = $3", "id LIMIT 1", kind, period)
	if err != nil {
		return model.Report{}, err
	}
	if len(rows) == 0 {
		return model.Report{}, store.ErrNotFound
	}
	return rows[0], nil
}

type llmRepo struct{ c *crud[model.LLMUsage] }

func (r llmRepo) Append(ctx context.Context, u *model.LLMUsage) error {
	if u.At.IsZero() {
		u.At = r.c.s.now()
	}
	return r.c.Create(ctx, u)
}

func (r llmRepo) Totals(ctx context.Context, from, to time.Time) (store.LLMTotals, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return store.LLMTotals{}, err
	}
	var t store.LLMTotals
	err = r.c.s.do(ctx, func(tx pgx.Tx) error {
		var cost string
		if err := tx.QueryRow(ctx, `SELECT coalesce(sum(input_tokens), 0), coalesce(sum(output_tokens), 0), coalesce(sum(cost), 0)::text, count(*)
			FROM llm_usage WHERE org_id = $1::uuid AND at >= $2 AND at < $3`, org, from.UTC(), to.UTC()).Scan(&t.InputTokens, &t.OutputTokens, &cost, &t.Calls); err != nil {
			return err
		}
		d, err := decimal.NewFromString(cost)
		t.Cost = d
		return err
	})
	return t, err
}

// ------------------------------------------------------------------ accès système (fonctions SECURITY DEFINER)

type systemRepo struct{ s *Store }

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (r systemRepo) ListOrgIDs(ctx context.Context) ([]string, error) {
	var out []string
	err := r.s.do(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, "SELECT id::text FROM kairn_list_org_ids() AS id")
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	return out, err
}

func (r systemRepo) FindUser(ctx context.Context, subject, email string) (model.User, error) {
	m := r.s.mapOf("users", reflect.TypeFor[model.User]())
	var out []model.User
	var err error
	err = r.s.do(ctx, func(tx pgx.Tx) error {
		out, err = queryRows[model.User](ctx, tx, m, "SELECT "+m.selectList("")+" FROM kairn_find_user($1, $2)", nullable(subject), nullable(email))
		return err
	})
	if err != nil {
		return model.User{}, err
	}
	if len(out) == 0 {
		return model.User{}, store.ErrNotFound
	}
	return out[0], nil
}

func (r systemRepo) FindAPIToken(ctx context.Context, prefix string) (model.APIToken, error) {
	m := r.s.mapOf("api_tokens", reflect.TypeFor[model.APIToken]())
	var out []model.APIToken
	var err error
	err = r.s.do(ctx, func(tx pgx.Tx) error {
		out, err = queryRows[model.APIToken](ctx, tx, m, "SELECT "+m.selectList("")+" FROM kairn_find_api_token($1)", prefix)
		return err
	})
	if err != nil {
		return model.APIToken{}, err
	}
	if len(out) == 0 {
		return model.APIToken{}, store.ErrNotFound
	}
	return out[0], nil
}

func (r systemRepo) FindConnectorByWebhook(ctx context.Context, token string) (string, string, string, error) {
	var org, id, typ string
	err := r.s.do(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT org_id::text, id::text, type FROM kairn_find_connector_by_webhook($1)", token).Scan(&org, &id, &typ)
	})
	return org, id, typ, err
}

func (r systemRepo) FindStatusPage(ctx context.Context, slug string) (string, string, bool, string, error) {
	var org, id string
	var public bool
	var hash *string
	err := r.s.do(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT org_id::text, id::text, public, access_hash FROM kairn_find_status_page($1)", slug).Scan(&org, &id, &public, &hash)
	})
	h := ""
	if hash != nil {
		h = *hash
	}
	return org, id, public, h, err
}

func (r systemRepo) FindOrgByStripeCustomer(ctx context.Context, customerID string) (string, error) {
	var org string
	err := r.s.do(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT id::text FROM kairn_find_org_by_stripe_customer($1) AS id", customerID).Scan(&org)
	})
	return org, err
}
