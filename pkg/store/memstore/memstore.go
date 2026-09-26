package memstore

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/ids"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/tenancy"
)

// Store est l'implémentation mémoire.
type Store struct {
	now func() time.Time

	mu            sync.RWMutex
	orgs          map[string]model.Organization
	users         map[string]model.User
	memberships   map[string]model.Membership // clé org|user
	tokens        *crud[model.APIToken]
	audit         *crud[model.AuditEvent]
	subscriptions map[string]model.Subscription
	connectors    *crud[model.Connector]
	runs          *crud[model.ConnectorRun]
	resources     *resourceRepo
	catalogs      map[string]model.PriceCatalog
	items         map[string][]model.PriceItem
	onprem        *crud[model.OnPremCostModel]
	adjustments   *crud[model.PricingAdjustment]
	rates         map[string][]model.ExchangeRate // clé base|quote, trié par jour
	recons        map[string]model.Reconciliation
	nodes         *crud[model.AllocationNode]
	rules         *crud[model.AllocationRule]
	shared        *crud[model.SharedCostRule]
	unitMetrics   *crud[model.UnitMetric]
	channels      *crud[model.NotificationChannel]
	budgets       *crud[model.Budget]
	alertRules    *crud[model.AlertRule]
	alertEvents   *crud[model.AlertEvent]
	silences      *crud[model.Silence]
	recos         *crud[model.Recommendation]
	anomalies     *crud[model.Anomaly]
	forecasts     map[string]model.Forecast // clé org|node
	checks        *crud[model.UptimeCheck]
	pages         *crud[model.StatusPage]
	incidents     *crud[model.Incident]
	reports       *crud[model.Report]
	exports       *crud[model.ExportJob]
	webhooks      *crud[model.WebhookSubscription]
	llm           *crud[model.LLMUsage]
}

// New crée un store mémoire vide.
func New() *Store {
	now := func() time.Time { return time.Now().UTC() }
	return &Store{
		now:           now,
		orgs:          map[string]model.Organization{},
		users:         map[string]model.User{},
		memberships:   map[string]model.Membership{},
		tokens:        newCRUD[model.APIToken](now),
		audit:         newCRUD[model.AuditEvent](now),
		subscriptions: map[string]model.Subscription{},
		connectors:    newCRUD[model.Connector](now),
		runs:          newCRUD[model.ConnectorRun](now),
		resources:     newResourceRepo(),
		catalogs:      map[string]model.PriceCatalog{},
		items:         map[string][]model.PriceItem{},
		onprem:        newCRUD[model.OnPremCostModel](now),
		adjustments:   newCRUD[model.PricingAdjustment](now),
		rates:         map[string][]model.ExchangeRate{},
		recons:        map[string]model.Reconciliation{},
		nodes:         newCRUD[model.AllocationNode](now),
		rules:         newCRUD[model.AllocationRule](now),
		shared:        newCRUD[model.SharedCostRule](now),
		unitMetrics:   newCRUD[model.UnitMetric](now),
		channels:      newCRUD[model.NotificationChannel](now),
		budgets:       newCRUD[model.Budget](now),
		alertRules:    newCRUD[model.AlertRule](now),
		alertEvents:   newCRUD[model.AlertEvent](now),
		silences:      newCRUD[model.Silence](now),
		recos:         newCRUD[model.Recommendation](now),
		anomalies:     newCRUD[model.Anomaly](now),
		forecasts:     map[string]model.Forecast{},
		checks:        newCRUD[model.UptimeCheck](now),
		pages:         newCRUD[model.StatusPage](now),
		incidents:     newCRUD[model.Incident](now),
		reports:       newCRUD[model.Report](now),
		exports:       newCRUD[model.ExportJob](now),
		webhooks:      newCRUD[model.WebhookSubscription](now),
		llm:           newCRUD[model.LLMUsage](now),
	}
}

var _ store.Store = (*Store)(nil)

// InTx exécute fn directement (le store mémoire n'offre pas d'isolation transactionnelle).
func (s *Store) InTx(ctx context.Context, fn func(ctx context.Context) error) error { return fn(ctx) }

func (s *Store) Orgs() store.OrgRepo                               { return orgRepo{s} }
func (s *Store) Users() store.UserRepo                             { return userRepo{s} }
func (s *Store) Memberships() store.MembershipRepo                 { return membershipRepo{s} }
func (s *Store) Tokens() store.TokenRepo                           { return tokenRepo{s.tokens} }
func (s *Store) Audit() store.AuditRepo                            { return auditRepo{s.audit} }
func (s *Store) Subscriptions() store.SubscriptionRepo             { return subRepo{s} }
func (s *Store) Connectors() store.ConnectorRepo                   { return connectorRepo{s.connectors} }
func (s *Store) Runs() store.RunRepo                               { return runRepo{s.runs} }
func (s *Store) Resources() store.ResourceRepo                     { return s.resources }
func (s *Store) Pricing() store.PricingRepo                        { return pricingRepo{s} }
func (s *Store) OnPremModels() store.CRUD[model.OnPremCostModel]   { return s.onprem }
func (s *Store) Adjustments() store.CRUD[model.PricingAdjustment]  { return s.adjustments }
func (s *Store) Rates() store.RateRepo                             { return rateRepo{s} }
func (s *Store) Reconciliations() store.ReconciliationRepo         { return reconRepo{s} }
func (s *Store) AllocationNodes() store.CRUD[model.AllocationNode] { return s.nodes }
func (s *Store) AllocationRules() store.CRUD[model.AllocationRule] { return s.rules }
func (s *Store) SharedRules() store.CRUD[model.SharedCostRule]     { return s.shared }
func (s *Store) UnitMetrics() store.CRUD[model.UnitMetric]         { return s.unitMetrics }
func (s *Store) Channels() store.CRUD[model.NotificationChannel]   { return s.channels }
func (s *Store) Budgets() store.CRUD[model.Budget]                 { return s.budgets }
func (s *Store) AlertRules() store.CRUD[model.AlertRule]           { return s.alertRules }
func (s *Store) AlertEvents() store.AlertEventRepo                 { return alertEventRepo{s.alertEvents} }
func (s *Store) Silences() store.CRUD[model.Silence]               { return s.silences }
func (s *Store) Recommendations() store.RecommendationRepo         { return recoRepo{s.recos, s.now} }
func (s *Store) Anomalies() store.AnomalyRepo                      { return anomalyRepo{s.anomalies} }
func (s *Store) Forecasts() store.ForecastRepo                     { return forecastRepo{s} }
func (s *Store) UptimeChecks() store.CRUD[model.UptimeCheck]       { return s.checks }
func (s *Store) StatusPages() store.StatusPageRepo                 { return statusPageRepo{s.pages} }
func (s *Store) Incidents() store.IncidentRepo                     { return incidentRepo{s.incidents} }
func (s *Store) Reports() store.ReportRepo                         { return reportRepo{s.reports} }
func (s *Store) Exports() store.CRUD[model.ExportJob]              { return s.exports }
func (s *Store) Webhooks() store.CRUD[model.WebhookSubscription]   { return s.webhooks }
func (s *Store) LLMUsage() store.LLMUsageRepo                      { return llmRepo{s.llm} }
func (s *Store) System() store.SystemRepo                          { return systemRepo{s} }

// ------------------------------------------------------------------ organisations

type orgRepo struct{ s *Store }

func (r orgRepo) visible(ctx context.Context, o model.Organization) bool {
	org, _ := tenancy.OrgID(ctx)
	if org != "" && (o.ID == org || (o.ParentOrgID != nil && *o.ParentOrgID == org)) {
		return true
	}
	return false
}

func (r orgRepo) Get(ctx context.Context, id string) (model.Organization, error) {
	if _, err := tenancy.OrgID(ctx); err != nil {
		return model.Organization{}, err
	}
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	o, ok := r.s.orgs[id]
	if !ok || !r.visible(ctx, o) {
		return model.Organization{}, store.ErrNotFound
	}
	return o, nil
}

func (r orgRepo) Create(ctx context.Context, o *model.Organization) error {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return err
	}
	if o.ID == "" {
		o.ID = org
	}
	if o.ID != org {
		return tenancy.ErrCrossOrg
	}
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if _, ok := r.s.orgs[o.ID]; ok {
		return store.ErrConflict
	}
	for _, x := range r.s.orgs {
		if x.Slug == o.Slug {
			return store.ErrConflict
		}
	}
	if o.CreatedAt.IsZero() {
		o.CreatedAt = r.s.now()
	}
	o.UpdatedAt = o.CreatedAt
	r.s.orgs[o.ID] = *o
	return nil
}

func (r orgRepo) Update(ctx context.Context, o *model.Organization) error {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return err
	}
	if o.ID != org {
		return tenancy.ErrCrossOrg
	}
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if _, ok := r.s.orgs[o.ID]; !ok {
		return store.ErrNotFound
	}
	o.UpdatedAt = r.s.now()
	r.s.orgs[o.ID] = *o
	return nil
}

func (r orgRepo) ListChildren(ctx context.Context) ([]model.Organization, error) {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return nil, err
	}
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	var out []model.Organization
	for _, o := range r.s.orgs {
		if o.ParentOrgID != nil && *o.ParentOrgID == org {
			out = append(out, o)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (r orgRepo) ListForUser(ctx context.Context, userID string) ([]model.Organization, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	var out []model.Organization
	for _, m := range r.s.memberships {
		if m.UserID == userID {
			if o, ok := r.s.orgs[m.OrgID]; ok {
				out = append(out, o)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Delete supprime l'organisation courante et toutes ses données.
func (r orgRepo) Delete(ctx context.Context, id string) error {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return err
	}
	if id != org {
		return tenancy.ErrCrossOrg
	}
	r.s.mu.Lock()
	if _, ok := r.s.orgs[org]; !ok {
		r.s.mu.Unlock()
		return store.ErrNotFound
	}
	for _, o := range r.s.orgs {
		if o.ParentOrgID != nil && *o.ParentOrgID == org {
			r.s.mu.Unlock()
			return store.ErrConflict
		}
	}
	delete(r.s.orgs, org)
	delete(r.s.subscriptions, org)
	for k, m := range r.s.memberships {
		if m.OrgID == org {
			delete(r.s.memberships, k)
		}
	}
	for id, c := range r.s.catalogs {
		if c.OrgID != nil && *c.OrgID == org {
			delete(r.s.catalogs, id)
			delete(r.s.items, id)
		}
	}
	for k, x := range r.s.recons {
		if x.OrgID == org {
			delete(r.s.recons, k)
		}
	}
	for k, f := range r.s.forecasts {
		if f.OrgID == org {
			delete(r.s.forecasts, k)
		}
	}
	r.s.mu.Unlock()
	s := r.s
	s.tokens.deleteWhere(org, func(*model.APIToken) bool { return true })
	s.audit.deleteWhere(org, func(*model.AuditEvent) bool { return true })
	s.connectors.deleteWhere(org, func(*model.Connector) bool { return true })
	s.runs.deleteWhere(org, func(*model.ConnectorRun) bool { return true })
	s.onprem.deleteWhere(org, func(*model.OnPremCostModel) bool { return true })
	s.adjustments.deleteWhere(org, func(*model.PricingAdjustment) bool { return true })
	s.nodes.deleteWhere(org, func(*model.AllocationNode) bool { return true })
	s.rules.deleteWhere(org, func(*model.AllocationRule) bool { return true })
	s.shared.deleteWhere(org, func(*model.SharedCostRule) bool { return true })
	s.unitMetrics.deleteWhere(org, func(*model.UnitMetric) bool { return true })
	s.channels.deleteWhere(org, func(*model.NotificationChannel) bool { return true })
	s.budgets.deleteWhere(org, func(*model.Budget) bool { return true })
	s.alertRules.deleteWhere(org, func(*model.AlertRule) bool { return true })
	s.alertEvents.deleteWhere(org, func(*model.AlertEvent) bool { return true })
	s.silences.deleteWhere(org, func(*model.Silence) bool { return true })
	s.recos.deleteWhere(org, func(*model.Recommendation) bool { return true })
	s.anomalies.deleteWhere(org, func(*model.Anomaly) bool { return true })
	s.checks.deleteWhere(org, func(*model.UptimeCheck) bool { return true })
	s.pages.deleteWhere(org, func(*model.StatusPage) bool { return true })
	s.incidents.deleteWhere(org, func(*model.Incident) bool { return true })
	s.reports.deleteWhere(org, func(*model.Report) bool { return true })
	s.exports.deleteWhere(org, func(*model.ExportJob) bool { return true })
	s.webhooks.deleteWhere(org, func(*model.WebhookSubscription) bool { return true })
	s.llm.deleteWhere(org, func(*model.LLMUsage) bool { return true })
	s.resources.purge(org)
	return nil
}

// ------------------------------------------------------------------ utilisateurs

type userRepo struct{ s *Store }

func (r userRepo) Get(_ context.Context, id string) (model.User, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	u, ok := r.s.users[id]
	if !ok {
		return model.User{}, store.ErrNotFound
	}
	return u, nil
}

func (r userRepo) Create(_ context.Context, u *model.User) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if u.ID == "" {
		u.ID = ids.New()
	}
	for _, x := range r.s.users {
		if strings.EqualFold(x.Email, u.Email) {
			return store.ErrConflict
		}
	}
	if u.CreatedAt.IsZero() {
		u.CreatedAt = r.s.now()
	}
	r.s.users[u.ID] = *u
	return nil
}

func (r userRepo) Update(_ context.Context, u *model.User) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if _, ok := r.s.users[u.ID]; !ok {
		return store.ErrNotFound
	}
	r.s.users[u.ID] = *u
	return nil
}

// ------------------------------------------------------------------ appartenances

type membershipRepo struct{ s *Store }

func mkey(org, user string) string { return org + "|" + user }

func (r membershipRepo) decorate(m model.Membership) model.Membership {
	if u, ok := r.s.users[m.UserID]; ok {
		m.Email, m.Name = u.Email, u.Name
	}
	return m
}

func (r membershipRepo) List(ctx context.Context) ([]model.Membership, error) {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return nil, err
	}
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	var out []model.Membership
	for _, m := range r.s.memberships {
		if m.OrgID == org {
			out = append(out, r.decorate(m))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	return out, nil
}

func (r membershipRepo) Get(ctx context.Context, userID string) (model.Membership, error) {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return model.Membership{}, err
	}
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	m, ok := r.s.memberships[mkey(org, userID)]
	if !ok {
		return model.Membership{}, store.ErrNotFound
	}
	return r.decorate(m), nil
}

func (r membershipRepo) Upsert(ctx context.Context, m *model.Membership) error {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return err
	}
	if m.OrgID == "" {
		m.OrgID = org
	}
	if m.OrgID != org {
		return tenancy.ErrCrossOrg
	}
	if m.Scopes == nil {
		m.Scopes = []string{}
	}
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if prev, ok := r.s.memberships[mkey(org, m.UserID)]; ok {
		m.CreatedAt = prev.CreatedAt
	} else if m.CreatedAt.IsZero() {
		m.CreatedAt = r.s.now()
	}
	r.s.memberships[mkey(org, m.UserID)] = *m
	return nil
}

func (r membershipRepo) Delete(ctx context.Context, userID string) error {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return err
	}
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if _, ok := r.s.memberships[mkey(org, userID)]; !ok {
		return store.ErrNotFound
	}
	delete(r.s.memberships, mkey(org, userID))
	return nil
}

func (r membershipRepo) ForUser(_ context.Context, userID string) ([]model.Membership, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	var out []model.Membership
	for _, m := range r.s.memberships {
		if m.UserID == userID {
			out = append(out, r.decorate(m))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OrgID < out[j].OrgID })
	return out, nil
}

// ------------------------------------------------------------------ jetons, audit, abonnement

type tokenRepo struct{ c *crud[model.APIToken] }

func (r tokenRepo) List(ctx context.Context) ([]model.APIToken, error) { return r.c.all(ctx, nil) }
func (r tokenRepo) Create(ctx context.Context, t *model.APIToken) error {
	if t.Scopes == nil {
		t.Scopes = []string{}
	}
	return r.c.Create(ctx, t)
}
func (r tokenRepo) Revoke(ctx context.Context, id string, at time.Time) error {
	t, err := r.c.Get(ctx, id)
	if err != nil {
		return err
	}
	t.RevokedAt = &at
	return r.c.Update(ctx, &t)
}
func (r tokenRepo) Touch(ctx context.Context, id string, at time.Time) error {
	t, err := r.c.Get(ctx, id)
	if err != nil {
		return err
	}
	t.LastUsedAt = &at
	return r.c.Update(ctx, &t)
}

type auditRepo struct{ c *crud[model.AuditEvent] }

func (r auditRepo) Append(ctx context.Context, e *model.AuditEvent) error {
	if e.At.IsZero() {
		e.At = r.c.now()
	}
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	return r.c.Create(ctx, e)
}

func (r auditRepo) List(ctx context.Context, f store.AuditFilter) ([]model.AuditEvent, error) {
	rows, err := r.c.all(ctx, func(e *model.AuditEvent) bool {
		if !f.From.IsZero() && e.At.Before(f.From) {
			return false
		}
		if !f.To.IsZero() && !e.At.Before(f.To) {
			return false
		}
		if f.Action != "" && !strings.HasPrefix(e.Action, f.Action) {
			return false
		}
		if f.ActorID != "" && e.ActorID != f.ActorID {
			return false
		}
		return f.Cursor == "" || e.ID < f.Cursor
	})
	if err != nil {
		return nil, err
	}
	// Plus récent d'abord.
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID > rows[j].ID })
	q := store.ListQuery{Limit: f.Limit}.Normalize()
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
	}
	return rows, nil
}

type subRepo struct{ s *Store }

func (r subRepo) Get(ctx context.Context) (model.Subscription, error) {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return model.Subscription{}, err
	}
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	sub, ok := r.s.subscriptions[org]
	if !ok {
		return model.Subscription{}, store.ErrNotFound
	}
	return sub, nil
}

func (r subRepo) Upsert(ctx context.Context, sub *model.Subscription) error {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return err
	}
	sub.OrgID = org
	sub.UpdatedAt = r.s.now()
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.subscriptions[org] = *sub
	return nil
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
	c, err := r.Get(ctx, id)
	if err != nil {
		return err
	}
	c.Status, c.StatusMessage = status, msg
	c.LastSyncAt = &syncAt
	if success {
		c.LastSuccessAt = &syncAt
	}
	return r.crud.Update(ctx, &c)
}

type runRepo struct{ c *crud[model.ConnectorRun] }

func (r runRepo) Create(ctx context.Context, run *model.ConnectorRun) error {
	return r.c.Create(ctx, run)
}
func (r runRepo) Finish(ctx context.Context, run *model.ConnectorRun) error {
	return r.c.Update(ctx, run)
}
func (r runRepo) ListByConnector(ctx context.Context, connectorID string, limit int) ([]model.ConnectorRun, error) {
	rows, err := r.c.all(ctx, func(x *model.ConnectorRun) bool { return x.ConnectorID == connectorID })
	if err != nil {
		return nil, err
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].StartedAt.After(rows[j].StartedAt) })
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

// ------------------------------------------------------------------ tarification

type pricingRepo struct{ s *Store }

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
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	for _, x := range r.s.catalogs {
		if x.Provider == c.Provider && x.Version == c.Version && ptrEq(x.OrgID, c.OrgID) {
			return store.ErrConflict
		}
	}
	r.s.catalogs[c.ID] = *c
	cp := make([]model.PriceItem, len(items))
	for i, it := range items {
		it.CatalogID = c.ID
		cp[i] = it
	}
	r.s.items[c.ID] = cp
	return nil
}

func ptrEq(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func (r pricingRepo) catalogVisible(ctx context.Context, c model.PriceCatalog) bool {
	if c.OrgID == nil {
		return true
	}
	org, _ := tenancy.OrgID(ctx)
	return org != "" && *c.OrgID == org
}

func (r pricingRepo) ListCatalogs(ctx context.Context) ([]model.PriceCatalog, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	var out []model.PriceCatalog
	for _, c := range r.s.catalogs {
		if r.catalogVisible(ctx, c) {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		return out[i].ValidFrom.Before(out[j].ValidFrom)
	})
	return out, nil
}

func (r pricingRepo) Items(ctx context.Context, catalogID string) ([]model.PriceItem, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	c, ok := r.s.catalogs[catalogID]
	if !ok || !r.catalogVisible(ctx, c) {
		return nil, store.ErrNotFound
	}
	return append([]model.PriceItem(nil), r.s.items[catalogID]...), nil
}

func (r pricingRepo) DeleteCatalog(ctx context.Context, id string) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	c, ok := r.s.catalogs[id]
	if !ok {
		return store.ErrNotFound
	}
	if c.OrgID == nil {
		if !tenancy.IsSystem(ctx) {
			return store.ErrNotFound
		}
	} else if err := tenancy.Check(ctx, *c.OrgID); err != nil {
		return store.ErrNotFound
	}
	delete(r.s.catalogs, id)
	delete(r.s.items, id)
	return nil
}

type rateRepo struct{ s *Store }

func (r rateRepo) Upsert(_ context.Context, rates []model.ExchangeRate) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	for _, x := range rates {
		k := x.Base + "|" + x.Quote
		list := r.s.rates[k]
		replaced := false
		for i := range list {
			if list[i].Day.Equal(x.Day) {
				list[i] = x
				replaced = true
			}
		}
		if !replaced {
			list = append(list, x)
		}
		sort.Slice(list, func(i, j int) bool { return list[i].Day.Before(list[j].Day) })
		r.s.rates[k] = list
	}
	return nil
}

func (r rateRepo) Get(_ context.Context, base, quote string, day time.Time) (model.ExchangeRate, error) {
	if base == quote {
		return model.ExchangeRate{Base: base, Quote: quote, Day: day, Rate: decimal.NewFromInt(1)}, nil
	}
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	list := r.s.rates[base+"|"+quote]
	for i := len(list) - 1; i >= 0; i-- {
		if !list[i].Day.After(day) {
			return list[i], nil
		}
	}
	return model.ExchangeRate{}, store.ErrNotFound
}

type reconRepo struct{ s *Store }

func (r reconRepo) Upsert(ctx context.Context, x *model.Reconciliation) error {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return err
	}
	x.OrgID = org
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.recons[org+"|"+x.ConnectorID+"|"+x.Month.Format("2006-01")] = *x
	return nil
}

func (r reconRepo) List(ctx context.Context) ([]model.Reconciliation, error) {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return nil, err
	}
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	var out []model.Reconciliation
	for _, x := range r.s.recons {
		if x.OrgID == org {
			out = append(out, x)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Month.Equal(out[j].Month) {
			return out[i].Month.After(out[j].Month)
		}
		return out[i].ConnectorID < out[j].ConnectorID
	})
	return out, nil
}

// ------------------------------------------------------------------ alertes

type alertEventRepo struct{ c *crud[model.AlertEvent] }

func (r alertEventRepo) List(ctx context.Context, q store.ListQuery) ([]model.AlertEvent, error) {
	rows, err := r.c.all(ctx, func(e *model.AlertEvent) bool {
		return matches(e, q.Filters) && (q.Cursor == "" || e.ID < q.Cursor)
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID > rows[j].ID })
	q = q.Normalize()
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
	}
	return rows, nil
}
func (r alertEventRepo) Get(ctx context.Context, id string) (model.AlertEvent, error) {
	return r.c.Get(ctx, id)
}
func (r alertEventRepo) FindActive(ctx context.Context, fp string) (model.AlertEvent, error) {
	rows, err := r.c.all(ctx, func(e *model.AlertEvent) bool {
		return e.Fingerprint == fp && e.Status != model.AlertResolved
	})
	if err != nil {
		return model.AlertEvent{}, err
	}
	if len(rows) == 0 {
		return model.AlertEvent{}, store.ErrNotFound
	}
	return rows[len(rows)-1], nil
}
func (r alertEventRepo) Create(ctx context.Context, e *model.AlertEvent) error {
	return r.c.Create(ctx, e)
}
func (r alertEventRepo) Update(ctx context.Context, e *model.AlertEvent) error {
	return r.c.Update(ctx, e)
}

// ------------------------------------------------------------------ recommandations, anomalies, prévisions

type recoRepo struct {
	c   *crud[model.Recommendation]
	now func() time.Time
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func (r recoRepo) List(ctx context.Context, f store.RecommendationFilter) ([]model.Recommendation, error) {
	rows, err := r.c.all(ctx, func(x *model.Recommendation) bool {
		if len(f.Status) > 0 && !contains(f.Status, x.Status) {
			return false
		}
		if len(f.Types) > 0 && !contains(f.Types, x.Type) {
			return false
		}
		if f.ResourceID != "" && x.ResourceID != f.ResourceID {
			return false
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if c := rows[i].SavingsMonthly.Cmp(rows[j].SavingsMonthly); c != 0 {
			return c > 0
		}
		return rows[i].ID < rows[j].ID
	})
	q := store.ListQuery{Limit: f.Limit}.Normalize()
	if f.Cursor != "" {
		for i, x := range rows {
			if x.ID == f.Cursor {
				rows = rows[i+1:]
				break
			}
		}
	}
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
	}
	return rows, nil
}

func (r recoRepo) Get(ctx context.Context, id string) (model.Recommendation, error) {
	return r.c.Get(ctx, id)
}

func (r recoRepo) Upsert(ctx context.Context, x *model.Recommendation) error {
	existing, err := r.c.all(ctx, func(e *model.Recommendation) bool { return e.Fingerprint == x.Fingerprint })
	if err != nil {
		return err
	}
	if len(existing) == 0 {
		if x.Status == "" {
			x.Status = model.RecoOpen
		}
		return r.c.Create(ctx, x)
	}
	cur := existing[0]
	// Le statut décidé par l'utilisateur est conservé ; les preuves et montants sont rafraîchis.
	x.ID, x.CreatedAt = cur.ID, cur.CreatedAt
	x.Status, x.StatusReason, x.PostponedUntil = cur.Status, cur.StatusReason, cur.PostponedUntil
	x.AppliedAt = cur.AppliedAt
	if x.MeasuredSavingsMonthly == nil {
		x.MeasuredSavingsMonthly = cur.MeasuredSavingsMonthly
	}
	x.OrgID = cur.OrgID
	return r.c.Update(ctx, x)
}

func (r recoRepo) Update(ctx context.Context, x *model.Recommendation) error {
	return r.c.Update(ctx, x)
}

type anomalyRepo struct{ c *crud[model.Anomaly] }

func (r anomalyRepo) List(ctx context.Context, from, to time.Time, status string) ([]model.Anomaly, error) {
	rows, err := r.c.all(ctx, func(a *model.Anomaly) bool {
		if !from.IsZero() && a.WindowEnd.Before(from) {
			return false
		}
		if !to.IsZero() && !a.WindowStart.Before(to) {
			return false
		}
		return status == "" || a.Status == status
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].WindowStart.After(rows[j].WindowStart) })
	return rows, nil
}

func (r anomalyRepo) Get(ctx context.Context, id string) (model.Anomaly, error) {
	return r.c.Get(ctx, id)
}

func (r anomalyRepo) Upsert(ctx context.Context, a *model.Anomaly) error {
	existing, err := r.c.all(ctx, func(e *model.Anomaly) bool {
		return e.SeriesKey == a.SeriesKey && e.WindowStart.Equal(a.WindowStart)
	})
	if err != nil {
		return err
	}
	if len(existing) == 0 {
		if a.Status == "" {
			a.Status = "open"
		}
		return r.c.Create(ctx, a)
	}
	cur := existing[0]
	a.ID, a.OrgID, a.CreatedAt, a.Status = cur.ID, cur.OrgID, cur.CreatedAt, cur.Status
	if a.Explanation == "" {
		a.Explanation, a.ExplanationSources = cur.Explanation, cur.ExplanationSources
	}
	return r.c.Update(ctx, a)
}

func (r anomalyRepo) Update(ctx context.Context, a *model.Anomaly) error { return r.c.Update(ctx, a) }

type forecastRepo struct{ s *Store }

func (r forecastRepo) Upsert(ctx context.Context, f *model.Forecast) error {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return err
	}
	f.OrgID = org
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.forecasts[org+"|"+f.NodeID] = *f
	return nil
}

func (r forecastRepo) Get(ctx context.Context, nodeID string) (model.Forecast, error) {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return model.Forecast{}, err
	}
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	f, ok := r.s.forecasts[org+"|"+nodeID]
	if !ok {
		return model.Forecast{}, store.ErrNotFound
	}
	return f, nil
}

func (r forecastRepo) List(ctx context.Context) ([]model.Forecast, error) {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return nil, err
	}
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	var out []model.Forecast
	for _, f := range r.s.forecasts {
		if f.OrgID == org {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NodeID < out[j].NodeID })
	return out, nil
}

// ------------------------------------------------------------------ uptime, rapports, IA

type statusPageRepo struct{ *crud[model.StatusPage] }

type incidentRepo struct{ *crud[model.Incident] }

func (r incidentRepo) OpenForCheck(ctx context.Context, checkID string) (model.Incident, error) {
	rows, err := r.all(ctx, func(i *model.Incident) bool {
		return i.CheckID != nil && *i.CheckID == checkID && i.Status == "open"
	})
	if err != nil {
		return model.Incident{}, err
	}
	if len(rows) == 0 {
		return model.Incident{}, store.ErrNotFound
	}
	return rows[len(rows)-1], nil
}

func (r incidentRepo) List(ctx context.Context, q store.ListQuery) ([]model.Incident, error) {
	rows, err := r.all(ctx, func(i *model.Incident) bool { return matches(i, q.Filters) })
	if err != nil {
		return nil, err
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].StartedAt.After(rows[j].StartedAt) })
	q = q.Normalize()
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
	}
	return rows, nil
}

type reportRepo struct{ *crud[model.Report] }

// Create applique l'unicité (organisation, type, période) de PostgreSQL.
func (r reportRepo) Create(ctx context.Context, x *model.Report) error {
	if _, err := r.GetByPeriod(ctx, x.Kind, x.Period); err == nil {
		return store.ErrConflict
	}
	return r.crud.Create(ctx, x)
}

func (r reportRepo) GetByPeriod(ctx context.Context, kind, period string) (model.Report, error) {
	rows, err := r.all(ctx, func(x *model.Report) bool { return x.Kind == kind && x.Period == period })
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
		u.At = r.c.now()
	}
	return r.c.Create(ctx, u)
}

func (r llmRepo) Totals(ctx context.Context, from, to time.Time) (store.LLMTotals, error) {
	rows, err := r.c.all(ctx, func(u *model.LLMUsage) bool {
		return !u.At.Before(from) && u.At.Before(to)
	})
	if err != nil {
		return store.LLMTotals{}, err
	}
	var t store.LLMTotals
	for _, u := range rows {
		t.InputTokens += u.InputTokens
		t.OutputTokens += u.OutputTokens
		t.Cost = t.Cost.Add(u.Cost)
		t.Calls++
	}
	return t, nil
}

// ------------------------------------------------------------------ accès système

type systemRepo struct{ s *Store }

func (r systemRepo) ListOrgIDs(_ context.Context) ([]string, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	out := make([]string, 0, len(r.s.orgs))
	for id := range r.s.orgs {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

func (r systemRepo) FindUser(_ context.Context, subject, email string) (model.User, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	if subject != "" {
		for _, u := range r.s.users {
			if u.OIDCSubject != nil && *u.OIDCSubject == subject {
				return u, nil
			}
		}
	}
	if email != "" {
		for _, u := range r.s.users {
			if strings.EqualFold(u.Email, email) {
				return u, nil
			}
		}
	}
	return model.User{}, store.ErrNotFound
}

func (r systemRepo) FindAPIToken(_ context.Context, prefix string) (model.APIToken, error) {
	r.s.tokens.mu.RLock()
	defer r.s.tokens.mu.RUnlock()
	for _, t := range r.s.tokens.rows {
		if t.Prefix == prefix && t.RevokedAt == nil {
			return t, nil
		}
	}
	return model.APIToken{}, store.ErrNotFound
}

func (r systemRepo) FindConnectorByWebhook(_ context.Context, token string) (string, string, string, error) {
	r.s.connectors.mu.RLock()
	defer r.s.connectors.mu.RUnlock()
	for _, c := range r.s.connectors.rows {
		if c.WebhookToken != nil && *c.WebhookToken == token && c.Enabled {
			return c.OrgID, c.ID, c.Type, nil
		}
	}
	return "", "", "", store.ErrNotFound
}

func (r systemRepo) FindStatusPage(_ context.Context, slug string) (string, string, bool, string, error) {
	r.s.pages.mu.RLock()
	defer r.s.pages.mu.RUnlock()
	for _, p := range r.s.pages.rows {
		if p.Slug == slug {
			h := ""
			if p.AccessHash != nil {
				h = *p.AccessHash
			}
			return p.OrgID, p.ID, p.Public, h, nil
		}
	}
	return "", "", false, "", store.ErrNotFound
}

func (r systemRepo) FindOrgByStripeCustomer(_ context.Context, customerID string) (string, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	for org, sub := range r.s.subscriptions {
		if sub.StripeCustomerID != nil && *sub.StripeCustomerID == customerID {
			return org, nil
		}
	}
	return "", store.ErrNotFound
}
