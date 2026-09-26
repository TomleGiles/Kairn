// Package pgstore implémente store.Store sur PostgreSQL 16+ (pgx v5).
//
// Isolation multi-tenant, en deux couches indépendantes :
//  1. chaque requête filtre explicitement sur org_id (règle CLAUDE.md §11) ;
//  2. chaque transaction positionne app.org_id / app.user_id / app.system
//     (set_config local à la transaction) et la Row-Level Security FORCE de
//     PostgreSQL refuse toute ligne d'une autre organisation.
//
// Le rôle de connexion ne doit pas avoir BYPASSRLS (voir
// docs/adr/0002-isolation-multi-tenant-rls.md).
package pgstore

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kairn-io/kairn/pkg/ids"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/tenancy"
)

// Store est l'implémentation PostgreSQL.
type Store struct {
	pool *pgxpool.Pool
	now  func() time.Time

	cols map[string]map[string]colInfo // table → colonne → type

	mu   sync.Mutex
	maps map[string]*tableMap // table|type → correspondance
}

var _ store.Store = (*Store)(nil)

// Open ouvre un pool de connexions et charge les métadonnées du schéma.
func Open(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("pgstore: parse dsn: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("pgstore: connect: %w", err)
	}
	s := &Store{pool: pool, now: func() time.Time { return time.Now().UTC() }, maps: map[string]*tableMap{}}
	if err := s.loadColumns(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

// Close ferme le pool.
func (s *Store) Close() { s.pool.Close() }

// Ping vérifie la connexion (sonde de disponibilité).
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// SetClock remplace l'horloge (tests).
func (s *Store) SetClock(now func() time.Time) { s.now = now }

func (s *Store) loadColumns(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `SELECT table_name, column_name, data_type, udt_name, is_nullable = 'YES'
		FROM information_schema.columns WHERE table_schema = current_schema()`)
	if err != nil {
		return fmt.Errorf("pgstore: load columns: %w", err)
	}
	defer rows.Close()
	s.cols = map[string]map[string]colInfo{}
	for rows.Next() {
		var table string
		var c colInfo
		if err := rows.Scan(&table, &c.name, &c.dataType, &c.udt, &c.nullable); err != nil {
			return fmt.Errorf("pgstore: scan columns: %w", err)
		}
		if s.cols[table] == nil {
			s.cols[table] = map[string]colInfo{}
		}
		s.cols[table][c.name] = c
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("pgstore: load columns: %w", err)
	}
	if len(s.cols["organizations"]) == 0 {
		return errors.New("pgstore: schema not found (run migrations first)")
	}
	return nil
}

// ------------------------------------------------------------------ transactions

type txKey struct{}

type txState struct {
	tx                  pgx.Tx
	org, user, system   string
	settingsInitialized bool
}

type settings struct{ org, user, system string }

func settingsOf(ctx context.Context) (settings, error) {
	org, _ := tenancy.OrgID(ctx)
	if org != "" && !ids.Valid(org) {
		return settings{}, store.ErrNotFound
	}
	user := tenancy.UserID(ctx)
	if !ids.Valid(user) {
		user = ""
	}
	sys := ""
	if tenancy.IsSystem(ctx) {
		sys = "on"
	}
	return settings{org, user, sys}, nil
}

func (st *txState) apply(ctx context.Context, want settings) error {
	if st.settingsInitialized && st.org == want.org && st.user == want.user && st.system == want.system {
		return nil
	}
	if _, err := st.tx.Exec(ctx, `SELECT set_config('app.org_id', $1, true), set_config('app.user_id', $2, true), set_config('app.system', $3, true)`,
		want.org, want.user, want.system); err != nil {
		return fmt.Errorf("pgstore: set tenancy: %w", err)
	}
	st.org, st.user, st.system, st.settingsInitialized = want.org, want.user, want.system, true
	return nil
}

// do exécute fn dans la transaction courante (InTx) ou dans une nouvelle
// transaction, après avoir positionné le contexte RLS du ctx.
func (s *Store) do(ctx context.Context, fn func(tx pgx.Tx) error) error {
	want, err := settingsOf(ctx)
	if err != nil {
		return err
	}
	if st, ok := ctx.Value(txKey{}).(*txState); ok {
		if err := st.apply(ctx, want); err != nil {
			return err
		}
		return mapErr(fn(st.tx))
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("pgstore: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	st := &txState{tx: tx}
	if err := st.apply(ctx, want); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return mapErr(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return mapErr(fmt.Errorf("pgstore: commit: %w", err))
	}
	return nil
}

// InTx exécute fn dans une transaction unique ; les dépôts appelés avec le
// contexte fourni à fn la réutilisent.
func (s *Store) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(*txState); ok {
		return fn(ctx)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("pgstore: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := fn(context.WithValue(ctx, txKey{}, &txState{tx: tx})); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return mapErr(fmt.Errorf("pgstore: commit: %w", err))
	}
	return nil
}

// mapErr traduit les erreurs PostgreSQL en erreurs du domaine.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return store.ErrNotFound
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "23505", "23503": // unicité, clé étrangère
			return fmt.Errorf("%w: %s", store.ErrConflict, pg.ConstraintName)
		case "42501": // violation de politique RLS
			return tenancy.ErrCrossOrg
		case "22P02": // identifiant mal formé
			return store.ErrNotFound
		}
	}
	return err
}

// orgOf renvoie l'organisation du contexte (erreur sans organisation).
func orgOf(ctx context.Context) (string, error) {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return "", err
	}
	if !ids.Valid(org) {
		return "", store.ErrNotFound
	}
	return org, nil
}

// ------------------------------------------------------------------ dépôts

func (s *Store) Orgs() store.OrgRepo               { return orgRepo{s} }
func (s *Store) Users() store.UserRepo             { return userRepo{s} }
func (s *Store) Memberships() store.MembershipRepo { return membershipRepo{s} }
func (s *Store) Tokens() store.TokenRepo           { return tokenRepo{newCRUD[model.APIToken](s, "api_tokens")} }
func (s *Store) Audit() store.AuditRepo {
	return auditRepo{newCRUD[model.AuditEvent](s, "audit_events")}
}
func (s *Store) Subscriptions() store.SubscriptionRepo { return subRepo{s} }
func (s *Store) Connectors() store.ConnectorRepo {
	return connectorRepo{newCRUD[model.Connector](s, "connectors")}
}
func (s *Store) Runs() store.RunRepo {
	return runRepo{newCRUD[model.ConnectorRun](s, "connector_runs")}
}
func (s *Store) Resources() store.ResourceRepo { return resourceRepo{s} }
func (s *Store) Pricing() store.PricingRepo    { return pricingRepo{s} }
func (s *Store) OnPremModels() store.CRUD[model.OnPremCostModel] {
	return newCRUD[model.OnPremCostModel](s, "onprem_cost_models")
}
func (s *Store) Adjustments() store.CRUD[model.PricingAdjustment] {
	return newCRUD[model.PricingAdjustment](s, "pricing_adjustments")
}
func (s *Store) Rates() store.RateRepo                     { return rateRepo{s} }
func (s *Store) Reconciliations() store.ReconciliationRepo { return reconRepo{s} }
func (s *Store) AllocationNodes() store.CRUD[model.AllocationNode] {
	return newCRUD[model.AllocationNode](s, "allocation_nodes")
}
func (s *Store) AllocationRules() store.CRUD[model.AllocationRule] {
	return newCRUD[model.AllocationRule](s, "allocation_rules")
}
func (s *Store) SharedRules() store.CRUD[model.SharedCostRule] {
	return newCRUD[model.SharedCostRule](s, "shared_cost_rules")
}
func (s *Store) UnitMetrics() store.CRUD[model.UnitMetric] {
	return newCRUD[model.UnitMetric](s, "unit_metrics")
}
func (s *Store) Channels() store.CRUD[model.NotificationChannel] {
	return newCRUD[model.NotificationChannel](s, "notification_channels")
}
func (s *Store) Budgets() store.CRUD[model.Budget] { return newCRUD[model.Budget](s, "budgets") }
func (s *Store) AlertRules() store.CRUD[model.AlertRule] {
	return newCRUD[model.AlertRule](s, "alert_rules")
}
func (s *Store) AlertEvents() store.AlertEventRepo {
	return alertEventRepo{newCRUD[model.AlertEvent](s, "alert_events")}
}
func (s *Store) Silences() store.CRUD[model.Silence] { return newCRUD[model.Silence](s, "silences") }
func (s *Store) Recommendations() store.RecommendationRepo {
	return recoRepo{newCRUD[model.Recommendation](s, "recommendations")}
}
func (s *Store) Anomalies() store.AnomalyRepo {
	return anomalyRepo{newCRUD[model.Anomaly](s, "anomalies")}
}
func (s *Store) Forecasts() store.ForecastRepo { return forecastRepo{s} }
func (s *Store) UptimeChecks() store.CRUD[model.UptimeCheck] {
	return newCRUD[model.UptimeCheck](s, "uptime_checks")
}
func (s *Store) StatusPages() store.StatusPageRepo {
	return statusPageRepo{newCRUD[model.StatusPage](s, "status_pages")}
}
func (s *Store) Incidents() store.IncidentRepo {
	return incidentRepo{newCRUD[model.Incident](s, "incidents")}
}
func (s *Store) Reports() store.ReportRepo { return reportRepo{newCRUD[model.Report](s, "reports")} }
func (s *Store) Exports() store.CRUD[model.ExportJob] {
	return newCRUD[model.ExportJob](s, "export_jobs")
}
func (s *Store) Webhooks() store.CRUD[model.WebhookSubscription] {
	return newCRUD[model.WebhookSubscription](s, "webhook_subscriptions")
}
func (s *Store) LLMUsage() store.LLMUsageRepo {
	return llmRepo{newCRUD[model.LLMUsage](s, "llm_usage")}
}
func (s *Store) System() store.SystemRepo { return systemRepo{s} }

// mapOf renvoie (et met en cache) la correspondance struct ↔ table.
func (s *Store) mapOf(table string, t reflect.Type) *tableMap {
	key := table + "|" + t.String()
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.maps[key]; ok {
		return m
	}
	m := buildMap(table, t, s.cols[table])
	s.maps[key] = m
	return m
}
