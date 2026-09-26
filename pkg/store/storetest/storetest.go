// Package storetest est la suite de tests de contrat de store.Store : elle
// s'exécute sur l'implémentation mémoire (toujours) et sur PostgreSQL (tests
// d'intégration), pour garantir des comportements identiques — en
// particulier l'isolation stricte entre organisations.
package storetest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/ids"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/tenancy"
)

// Options décrit les capacités de l'implémentation testée.
type Options struct {
	// Rollback : InTx annule réellement les écritures en cas d'erreur.
	Rollback bool
}

// Run exécute la suite sur un store neuf ou partagé (les données sont préfixées aléatoirement).
func Run(t *testing.T, st store.Store, opts Options) {
	t.Helper()
	f := newFixture(t, st)
	f.opts = opts
	t.Run("orgs_users_memberships", f.orgsUsersMemberships)
	t.Run("tokens_audit_subscriptions", f.tokensAuditSubscriptions)
	t.Run("connectors_runs", f.connectorsRuns)
	t.Run("resources_edges", f.resourcesEdges)
	t.Run("pricing_rates_reconciliations", f.pricing)
	t.Run("generic_crud_pagination", f.genericCRUD)
	t.Run("recommendations_anomalies_forecasts", f.analytics)
	t.Run("alerts_incidents_reports_llm", f.ops)
	t.Run("isolation", f.isolation)
	t.Run("transactions", f.transactions)
	t.Run("delete_organization", f.deleteOrganization)
}

type fixture struct {
	st         store.Store
	suffix     string
	orgA, orgB string
	userA      string
	userB      string
	ctxA, ctxB context.Context
	now        time.Time
	opts       Options
}

func newFixture(t *testing.T, st store.Store) *fixture {
	f := &fixture{st: st, suffix: ids.New()[24:], orgA: ids.New(), orgB: ids.New(), now: time.Now().UTC().Truncate(time.Second)}
	f.ctxA = tenancy.WithOrg(context.Background(), f.orgA)
	f.ctxB = tenancy.WithOrg(context.Background(), f.orgB)
	bg := context.Background()
	for _, o := range []struct {
		id, name string
		ctx      context.Context
	}{{f.orgA, "Alpha", f.ctxA}, {f.orgB, "Beta", f.ctxB}} {
		org := model.Organization{ID: o.id, Name: o.name + " " + f.suffix, Slug: o.name + "-" + f.suffix, Plan: model.PlanTeam,
			Currency: "EUR", Locale: "fr", Timezone: "Europe/Paris", Settings: model.OrgSettings{LLMProvider: "mistral", ReportRecipients: []string{"cfo@example.com"}}}
		must(t, st.Orgs().Create(o.ctx, &org))
	}
	ua := model.User{Email: "alice-" + f.suffix + "@example.com", Name: "Alice", Locale: "fr"}
	must(t, st.Users().Create(bg, &ua))
	ub := model.User{Email: "bob-" + f.suffix + "@example.com", Name: "Bob", Locale: "en"}
	must(t, st.Users().Create(bg, &ub))
	f.userA, f.userB = ua.ID, ub.ID
	must(t, st.Memberships().Upsert(f.ctxA, &model.Membership{UserID: f.userA, Role: model.RoleOwner}))
	must(t, st.Memberships().Upsert(f.ctxB, &model.Membership{UserID: f.userB, Role: model.RoleOwner}))
	return f
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func expectErr(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("expected %v, got %v", want, err)
	}
}

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func (f *fixture) orgsUsersMemberships(t *testing.T) {
	st := f.st
	o, err := st.Orgs().Get(f.ctxA, f.orgA)
	must(t, err)
	if o.Settings.LLMProvider != "mistral" || len(o.Settings.ReportRecipients) != 1 || o.CreatedAt.IsZero() {
		t.Fatalf("org roundtrip: %+v", o)
	}
	vat := dec("0.2")
	o.VATRate = &vat
	o.Settings.AllowExternalLLM = true
	must(t, st.Orgs().Update(f.ctxA, &o))
	o2, err := st.Orgs().Get(f.ctxA, f.orgA)
	must(t, err)
	if o2.VATRate == nil || !o2.VATRate.Equal(vat) || !o2.Settings.AllowExternalLLM {
		t.Fatalf("org update: %+v", o2)
	}
	// Slug unique.
	dup := model.Organization{ID: ids.New(), Name: "Dup", Slug: o.Slug, Plan: model.PlanStarter, Currency: "EUR", Locale: "fr", Timezone: "UTC"}
	expectErr(t, st.Orgs().Create(tenancy.WithOrg(context.Background(), dup.ID), &dup), store.ErrConflict)
	// Organisation cliente (MSP).
	child := model.Organization{ID: ids.New(), ParentOrgID: &f.orgA, Name: "Client " + f.suffix, Slug: "client-" + f.suffix, Plan: model.PlanTeam,
		Currency: "EUR", Locale: "fr", Timezone: "UTC"}
	must(t, st.Orgs().Create(tenancy.WithOrg(context.Background(), child.ID), &child))
	kids, err := st.Orgs().ListChildren(f.ctxA)
	must(t, err)
	if len(kids) != 1 || kids[0].ID != child.ID {
		t.Fatalf("children: %+v", kids)
	}
	if _, err := st.Orgs().Get(f.ctxA, child.ID); err != nil {
		t.Fatalf("parent must see its client org: %v", err)
	}
	if _, err := st.Orgs().Get(f.ctxB, f.orgA); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("org B must not see org A: %v", err)
	}
	// Utilisateurs.
	u, err := st.Users().Get(tenancy.WithUser(context.Background(), f.userA), f.userA)
	must(t, err)
	u.Name = "Alice Martin"
	must(t, st.Users().Update(tenancy.WithUser(context.Background(), f.userA), &u))
	found, err := st.System().FindUser(context.Background(), "", "alice-"+f.suffix+"@example.com")
	must(t, err)
	if found.ID != f.userA || found.Name != "Alice Martin" {
		t.Fatalf("find user: %+v", found)
	}
	expectErr(t, st.Users().Create(context.Background(), &model.User{Email: u.Email, Locale: "fr"}), store.ErrConflict)
	// Appartenances.
	must(t, st.Memberships().Upsert(f.ctxA, &model.Membership{UserID: f.userB, Role: model.RoleViewer, Scopes: []string{ids.New()}}))
	ms, err := st.Memberships().List(f.ctxA)
	must(t, err)
	if len(ms) != 2 || ms[0].Email == "" {
		t.Fatalf("memberships: %+v", ms)
	}
	m, err := st.Memberships().Get(f.ctxA, f.userB)
	must(t, err)
	if m.Role != model.RoleViewer || len(m.Scopes) != 1 {
		t.Fatalf("membership: %+v", m)
	}
	must(t, st.Memberships().Upsert(f.ctxA, &model.Membership{UserID: f.userB, Role: model.RoleFinance}))
	m, _ = st.Memberships().Get(f.ctxA, f.userB)
	if m.Role != model.RoleFinance || len(m.Scopes) != 0 {
		t.Fatalf("membership upsert: %+v", m)
	}
	forB, err := st.Memberships().ForUser(context.Background(), f.userB)
	must(t, err)
	if len(forB) != 2 {
		t.Fatalf("memberships for B: %+v", forB)
	}
	orgsB, err := st.Orgs().ListForUser(context.Background(), f.userB)
	must(t, err)
	if len(orgsB) != 2 {
		t.Fatalf("orgs for B: %d", len(orgsB))
	}
	must(t, st.Memberships().Delete(f.ctxA, f.userB))
	expectErr(t, st.Memberships().Delete(f.ctxA, f.userB), store.ErrNotFound)
	orgsB, _ = st.Orgs().ListForUser(context.Background(), f.userB)
	if len(orgsB) != 1 || orgsB[0].ID != f.orgB {
		t.Fatalf("orgs for B after removal: %+v", orgsB)
	}
}

func (f *fixture) tokensAuditSubscriptions(t *testing.T) {
	st := f.st
	tok := model.APIToken{Name: "ci", Prefix: "kairn_" + f.suffix, Hash: "h", Role: model.RoleViewer, CreatedBy: &f.userA}
	must(t, st.Tokens().Create(f.ctxA, &tok))
	got, err := st.System().FindAPIToken(context.Background(), tok.Prefix)
	must(t, err)
	if got.OrgID != f.orgA || got.CreatedBy == nil || *got.CreatedBy != f.userA || got.Scopes == nil {
		t.Fatalf("token: %+v", got)
	}
	must(t, st.Tokens().Touch(f.ctxA, tok.ID, f.now))
	list, err := st.Tokens().List(f.ctxA)
	must(t, err)
	if len(list) != 1 || list[0].LastUsedAt == nil || !list[0].LastUsedAt.Equal(f.now) {
		t.Fatalf("tokens: %+v", list)
	}
	must(t, st.Tokens().Revoke(f.ctxA, tok.ID, f.now))
	_, err = st.System().FindAPIToken(context.Background(), tok.Prefix)
	expectErr(t, err, store.ErrNotFound)
	expectErr(t, st.Tokens().Revoke(f.ctxB, tok.ID, f.now), store.ErrNotFound)

	for i, action := range []string{"org.update", "member.add", "org.delete"} {
		must(t, st.Audit().Append(f.ctxA, &model.AuditEvent{ActorType: "user", ActorID: f.userA, Action: action, At: f.now.Add(time.Duration(i) * time.Minute),
			Details: map[string]any{"n": i}}))
	}
	evs, err := st.Audit().List(f.ctxA, store.AuditFilter{Action: "org."})
	must(t, err)
	if len(evs) != 2 || evs[0].Action != "org.delete" {
		t.Fatalf("audit filter/order: %+v", evs)
	}
	page, err := st.Audit().List(f.ctxA, store.AuditFilter{Limit: 1, Cursor: evs[0].ID})
	must(t, err)
	if len(page) != 1 || page[0].Action != "member.add" {
		t.Fatalf("audit cursor: %+v", page)
	}
	if evs, _ := st.Audit().List(f.ctxB, store.AuditFilter{}); len(evs) != 0 {
		t.Fatalf("audit leak: %d", len(evs))
	}

	cust := "cus_" + f.suffix
	must(t, st.Subscriptions().Upsert(f.ctxA, &model.Subscription{Plan: model.PlanTeam, Status: "active", StripeCustomerID: &cust}))
	must(t, st.Subscriptions().Upsert(f.ctxA, &model.Subscription{Plan: model.PlanEnterprise, Status: "active", StripeCustomerID: &cust}))
	sub, err := st.Subscriptions().Get(f.ctxA)
	must(t, err)
	if sub.Plan != model.PlanEnterprise {
		t.Fatalf("subscription: %+v", sub)
	}
	org, err := st.System().FindOrgByStripeCustomer(context.Background(), cust)
	must(t, err)
	if org != f.orgA {
		t.Fatalf("stripe org: %s", org)
	}
	_, err = st.Subscriptions().Get(f.ctxB)
	expectErr(t, err, store.ErrNotFound)
}

func (f *fixture) connector(t *testing.T, ctx context.Context) model.Connector {
	hook := "wh_" + ids.New()
	c := model.Connector{Type: "openstack", Name: "OS " + f.suffix, Settings: map[string]string{"auth_url": "https://keystone"}, SecretsEnc: []byte{1, 2, 3},
		Status: model.ConnectorPending, IntervalSeconds: 3600, Enabled: true, WebhookToken: &hook}
	must(t, f.st.Connectors().Create(ctx, &c))
	return c
}

func (f *fixture) connectorsRuns(t *testing.T) {
	st := f.st
	c := f.connector(t, f.ctxA)
	got, err := st.Connectors().Get(f.ctxA, c.ID)
	must(t, err)
	if got.Settings["auth_url"] != "https://keystone" || string(got.SecretsEnc) != "\x01\x02\x03" || got.LastSyncAt != nil {
		t.Fatalf("connector roundtrip: %+v", got)
	}
	must(t, st.Connectors().UpdateStatus(f.ctxA, c.ID, model.ConnectorError, "boom", f.now, false))
	must(t, st.Connectors().UpdateStatus(f.ctxA, c.ID, model.ConnectorOK, "", f.now.Add(time.Hour), true))
	must(t, st.Connectors().UpdateStatus(f.ctxA, c.ID, model.ConnectorDegraded, "partial", f.now.Add(2*time.Hour), false))
	got, _ = st.Connectors().Get(f.ctxA, c.ID)
	if got.Status != model.ConnectorDegraded || got.LastSuccessAt == nil || !got.LastSuccessAt.Equal(f.now.Add(time.Hour)) || !got.LastSyncAt.Equal(f.now.Add(2*time.Hour)) {
		t.Fatalf("connector status: %+v", got)
	}
	org, id, typ, err := st.System().FindConnectorByWebhook(context.Background(), *c.WebhookToken)
	must(t, err)
	if org != f.orgA || id != c.ID || typ != "openstack" {
		t.Fatalf("webhook lookup: %s %s %s", org, id, typ)
	}
	filtered, err := st.Connectors().List(f.ctxA, store.ListQuery{Filters: map[string]string{"type": "openstack", "enabled": "true"}})
	must(t, err)
	if len(filtered) != 1 {
		t.Fatalf("connector filters: %d", len(filtered))
	}
	if none, _ := st.Connectors().List(f.ctxA, store.ListQuery{Filters: map[string]string{"unknown_field": "x"}}); len(none) != 0 {
		t.Fatalf("unknown filter must match nothing")
	}
	for i := 0; i < 3; i++ {
		run := model.ConnectorRun{ConnectorID: c.ID, Kind: "inventory", Status: "running", StartedAt: f.now.Add(time.Duration(i) * time.Minute)}
		must(t, st.Runs().Create(f.ctxA, &run))
		fin := run.StartedAt.Add(time.Second)
		run.Status, run.Items, run.FinishedAt = "ok", 10+i, &fin
		must(t, st.Runs().Finish(f.ctxA, &run))
	}
	runs, err := st.Runs().ListByConnector(f.ctxA, c.ID, 2)
	must(t, err)
	if len(runs) != 2 || runs[0].Items != 12 || runs[0].FinishedAt == nil {
		t.Fatalf("runs: %+v", runs)
	}
	if runs, _ := st.Runs().ListByConnector(f.ctxB, c.ID, 10); len(runs) != 0 {
		t.Fatalf("runs leak")
	}
}

func (f *fixture) resourcesEdges(t *testing.T) {
	st := f.st
	c := f.connector(t, f.ctxA)
	t0 := f.now.Add(-48 * time.Hour)
	project := model.Resource{ID: ids.Resource(f.orgA, c.ID, model.TypeProject, "p1"), ConnectorID: c.ID, Provider: "openstack", Type: model.TypeProject,
		ExternalID: "p1", Name: "shop-prod", Region: "GRA", Labels: map[string]string{"team": "shop"}, Attributes: map[string]any{}, ValidFrom: t0}
	vm := model.Resource{ID: ids.Resource(f.orgA, c.ID, model.TypeInstance, "vm1"), ConnectorID: c.ID, Provider: "openstack", Type: model.TypeInstance,
		ExternalID: "vm1", Name: "Shop-Web-1", Region: "GRA", Labels: map[string]string{"team": "shop", "env": "prod"},
		Attributes: map[string]any{"flavor": "b2-7", "vcpus": 2, "ram_gb": 7.5}, ValidFrom: t0}
	must(t, st.Resources().InsertVersions(f.ctxA, []model.Resource{project, vm}))
	expectErr(t, st.Resources().InsertVersions(f.ctxA, []model.Resource{vm}), store.ErrConflict)
	// Redimensionnement : nouvelle version.
	t1 := f.now.Add(-24 * time.Hour)
	must(t, st.Resources().CloseVersions(f.ctxA, []string{vm.ID}, t1))
	vm2 := vm
	vm2.Attributes = map[string]any{"flavor": "b2-15", "vcpus": 4, "ram_gb": 15}
	vm2.ValidFrom, vm2.ValidTo = t1, nil
	must(t, st.Resources().InsertVersions(f.ctxA, []model.Resource{vm2}))

	cur, err := st.Resources().Current(f.ctxA, store.ResourceFilter{Types: []string{model.TypeInstance}})
	must(t, err)
	if len(cur) != 1 || cur[0].Attr("flavor") != "b2-15" || cur[0].AttrDecimal("vcpus").String() != "4" {
		t.Fatalf("current: %+v", cur)
	}
	past, err := st.Resources().Current(f.ctxA, store.ResourceFilter{At: t0.Add(time.Hour), Types: []string{model.TypeInstance}})
	must(t, err)
	if len(past) != 1 || past[0].Attr("flavor") != "b2-7" || past[0].AttrDecimal("ram_gb").String() != "7.5" {
		t.Fatalf("history at t0: %+v", past)
	}
	for _, q := range []store.ResourceFilter{
		{Query: "web"}, {Labels: map[string]string{"env": "prod"}}, {IDs: []string{vm.ID}}, {Provider: "openstack", Region: "GRA", Types: []string{model.TypeInstance}},
	} {
		got, err := st.Resources().Current(f.ctxA, q)
		must(t, err)
		if len(got) != 1 || got[0].ID != vm.ID {
			t.Fatalf("filter %+v: %d", q, len(got))
		}
	}
	all, _ := st.Resources().Current(f.ctxA, store.ResourceFilter{ConnectorID: c.ID, Limit: 1})
	if len(all) != 1 {
		t.Fatalf("limit: %d", len(all))
	}
	next, _ := st.Resources().Current(f.ctxA, store.ResourceFilter{ConnectorID: c.ID, Cursor: all[0].ID})
	if len(next) != 1 || next[0].ID == all[0].ID {
		t.Fatalf("cursor: %+v", next)
	}
	hist, err := st.Resources().History(f.ctxA, vm.ID)
	must(t, err)
	if len(hist) != 2 || hist[0].ValidTo == nil || !hist[0].ValidTo.Equal(t1) {
		t.Fatalf("history: %+v", hist)
	}
	win, err := st.Resources().InWindow(f.ctxA, t0, f.now)
	must(t, err)
	if len(win) < 3 {
		t.Fatalf("window: %d", len(win))
	}
	n, err := st.Resources().Count(f.ctxA)
	must(t, err)
	if n < 2 {
		t.Fatalf("count: %d", n)
	}
	// Disparition : la dernière version reste lisible.
	must(t, st.Resources().CloseVersions(f.ctxA, []string{vm.ID}, f.now))
	gone, err := st.Resources().Get(f.ctxA, vm.ID)
	must(t, err)
	if gone.ValidTo == nil || gone.Attr("flavor") != "b2-15" {
		t.Fatalf("closed resource: %+v", gone)
	}
	// Fermeture au même instant que l'ouverture : au moins une seconde de vie.
	flash := model.Resource{ID: ids.Resource(f.orgA, c.ID, model.TypeIP, "ip1"), ConnectorID: c.ID, Provider: "openstack", Type: model.TypeIP, ExternalID: "ip1", ValidFrom: f.now}
	must(t, st.Resources().InsertVersions(f.ctxA, []model.Resource{flash}))
	must(t, st.Resources().CloseVersions(f.ctxA, []string{flash.ID}, f.now))
	fl, _ := st.Resources().Get(f.ctxA, flash.ID)
	if fl.ValidTo == nil || !fl.ValidTo.After(fl.ValidFrom) {
		t.Fatalf("flash close: %+v", fl)
	}

	edges := []model.ResourceEdge{{ParentID: project.ID, ChildID: vm.ID, Relation: model.RelContains, ValidFrom: t0}}
	must(t, st.Resources().InsertEdges(f.ctxA, edges))
	ce, err := st.Resources().CurrentEdges(f.ctxA, c.ID)
	must(t, err)
	if len(ce) != 1 || ce[0].OrgID != f.orgA {
		t.Fatalf("current edges: %+v", ce)
	}
	if other, _ := st.Resources().CurrentEdges(f.ctxA, ids.New()); len(other) != 0 {
		t.Fatalf("edges of another connector: %d", len(other))
	}
	must(t, st.Resources().CloseEdges(f.ctxA, edges, t1))
	if cur, _ := st.Resources().Edges(f.ctxA, time.Time{}, time.Time{}); len(cur) != 0 {
		t.Fatalf("closed edge still current")
	}
	if hist, _ := st.Resources().Edges(f.ctxA, t0, f.now); len(hist) != 1 {
		t.Fatalf("edge history: %d", len(hist))
	}
	if leak, _ := st.Resources().Current(f.ctxB, store.ResourceFilter{}); len(leak) != 0 {
		t.Fatalf("resources leak: %d", len(leak))
	}
	_, err = st.Resources().Get(f.ctxB, vm.ID)
	expectErr(t, err, store.ErrNotFound)
}

func (f *fixture) pricing(t *testing.T) {
	st := f.st
	sys := tenancy.WithSystem(context.Background())
	pub := model.PriceCatalog{Provider: "ovh", Version: "2026-09-" + f.suffix, Source: "https://www.ovhcloud.com", Currency: "EUR", ValidFrom: f.now}
	items := []model.PriceItem{
		{SKU: "b2-7", Region: "", Unit: model.UnitHour, Price: dec("0.0681"), Currency: "EUR", Attributes: map[string]string{"vcpus": "2"}},
		{SKU: "b2-15", Region: "GRA", Unit: model.UnitHour, Price: dec("0.1361"), Currency: "EUR"},
	}
	expectErr(t, st.Pricing().CreateCatalog(f.ctxA, &model.PriceCatalog{Provider: "ovh", Version: "x", Source: "s", Currency: "EUR"}, nil), tenancy.ErrNoOrg)
	must(t, st.Pricing().CreateCatalog(sys, &pub, items))
	expectErr(t, st.Pricing().CreateCatalog(sys, &model.PriceCatalog{Provider: "ovh", Version: pub.Version, Source: "s", Currency: "EUR", ValidFrom: f.now}, nil), store.ErrConflict)
	neg := model.PriceCatalog{OrgID: &f.orgA, Provider: "ovh", Version: "negotiated-" + f.suffix, Source: "contract", Currency: "EUR", ValidFrom: f.now}
	must(t, st.Pricing().CreateCatalog(f.ctxA, &neg, []model.PriceItem{{SKU: "b2-7", Unit: model.UnitHour, Price: dec("0.05"), Currency: "EUR"}}))
	expectErr(t, st.Pricing().CreateCatalog(f.ctxB, &model.PriceCatalog{OrgID: &f.orgA, Provider: "ovh", Version: "evil", Source: "s", Currency: "EUR"}, nil), tenancy.ErrCrossOrg)

	has := func(ctx context.Context, id string) bool {
		cs, err := st.Pricing().ListCatalogs(ctx)
		must(t, err)
		for _, c := range cs {
			if c.ID == id {
				return true
			}
		}
		return false
	}
	if !has(f.ctxA, pub.ID) || !has(f.ctxA, neg.ID) || !has(f.ctxB, pub.ID) || has(f.ctxB, neg.ID) {
		t.Fatalf("catalog visibility")
	}
	its, err := st.Pricing().Items(f.ctxB, pub.ID)
	must(t, err)
	if len(its) != 2 || its[0].CatalogID != pub.ID {
		t.Fatalf("items: %+v", its)
	}
	for _, it := range its {
		if it.SKU == "b2-7" && (!it.Price.Equal(dec("0.0681")) || it.Attributes["vcpus"] != "2") {
			t.Fatalf("item roundtrip: %+v", it)
		}
	}
	_, err = st.Pricing().Items(f.ctxB, neg.ID)
	expectErr(t, err, store.ErrNotFound)
	expectErr(t, st.Pricing().DeleteCatalog(f.ctxA, pub.ID), store.ErrNotFound)
	expectErr(t, st.Pricing().DeleteCatalog(f.ctxB, neg.ID), store.ErrNotFound)
	must(t, st.Pricing().DeleteCatalog(f.ctxA, neg.ID))
	must(t, st.Pricing().DeleteCatalog(sys, pub.ID))

	base := "X" + f.suffix[:2]
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	must(t, st.Rates().Upsert(context.Background(), []model.ExchangeRate{
		{Base: base, Quote: "EUR", Day: day, Rate: dec("0.91")}, {Base: base, Quote: "EUR", Day: day.AddDate(0, 0, 2), Rate: dec("0.92")},
	}))
	must(t, st.Rates().Upsert(context.Background(), []model.ExchangeRate{{Base: base, Quote: "EUR", Day: day, Rate: dec("0.90")}}))
	r, err := st.Rates().Get(context.Background(), base, "EUR", day.AddDate(0, 0, 1))
	must(t, err)
	if !r.Rate.Equal(dec("0.90")) || !r.Day.Equal(day) {
		t.Fatalf("rate: %+v", r)
	}
	_, err = st.Rates().Get(context.Background(), base, "EUR", day.AddDate(0, 0, -1))
	expectErr(t, err, store.ErrNotFound)
	one, _ := st.Rates().Get(context.Background(), "EUR", "EUR", day)
	if !one.Rate.Equal(decimal.NewFromInt(1)) {
		t.Fatalf("identity rate")
	}

	c := f.connector(t, f.ctxA)
	month := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	must(t, st.Reconciliations().Upsert(f.ctxA, &model.Reconciliation{ConnectorID: c.ID, Provider: "ovh", Month: month, Estimated: dec("100"), Billed: dec("101"), Currency: "EUR"}))
	must(t, st.Reconciliations().Upsert(f.ctxA, &model.Reconciliation{ConnectorID: c.ID, Provider: "ovh", Month: month, Estimated: dec("100.5"), Billed: dec("101"), Currency: "EUR"}))
	rs, err := st.Reconciliations().List(f.ctxA)
	must(t, err)
	if len(rs) != 1 || !rs[0].Estimated.Equal(dec("100.5")) || !rs[0].Month.Equal(month) {
		t.Fatalf("reconciliations: %+v", rs)
	}
	if rs, _ := st.Reconciliations().List(f.ctxB); len(rs) != 0 {
		t.Fatalf("reconciliation leak")
	}
}

func (f *fixture) genericCRUD(t *testing.T) {
	st := f.st
	root := model.AllocationNode{Kind: model.NodeOrganization, Name: "Root", Path: "/root"}
	must(t, st.AllocationNodes().Create(f.ctxA, &root))
	var created []string
	for i := 0; i < 5; i++ {
		n := model.AllocationNode{ParentID: &root.ID, Kind: model.NodeTeam, Name: fmt.Sprintf("Team %d", i), Path: fmt.Sprintf("/root/team-%d", i)}
		must(t, st.AllocationNodes().Create(f.ctxA, &n))
		created = append(created, n.ID)
	}
	page1, err := st.AllocationNodes().List(f.ctxA, store.ListQuery{Limit: 4})
	must(t, err)
	page2, err := st.AllocationNodes().List(f.ctxA, store.ListQuery{Limit: 4, Cursor: page1[len(page1)-1].ID})
	must(t, err)
	if len(page1) != 4 || len(page2) != 2 || page2[0].ID <= page1[3].ID {
		t.Fatalf("pagination: %d %d", len(page1), len(page2))
	}
	all, err := store.ListAll(f.ctxA, st.AllocationNodes(), func(n model.AllocationNode) string { return n.ID }, map[string]string{"parent_id": root.ID})
	must(t, err)
	if len(all) != 5 {
		t.Fatalf("filter parent_id: %d", len(all))
	}
	rule := model.AllocationRule{NodeID: created[0], Name: "team label", Priority: 10, Enabled: true,
		Conditions: []model.Condition{{Field: "label:team", Op: model.OpIn, Values: []string{"shop", "search"}}}}
	must(t, st.AllocationRules().Create(f.ctxA, &rule))
	got, err := st.AllocationRules().Get(f.ctxA, rule.ID)
	must(t, err)
	if len(got.Conditions) != 1 || len(got.Conditions[0].Values) != 2 || got.CreatedAt.IsZero() {
		t.Fatalf("rule roundtrip: %+v", got)
	}
	got.Priority = 5
	must(t, st.AllocationRules().Update(f.ctxA, &got))
	got2, _ := st.AllocationRules().Get(f.ctxA, rule.ID)
	if got2.Priority != 5 {
		t.Fatalf("rule update")
	}
	b := model.Budget{NodeID: &created[1], Name: "Team 1", Period: "monthly", Amount: dec("1500.00"), Currency: "EUR", Thresholds: []int{50, 80, 100}, ChannelIDs: []string{}}
	must(t, st.Budgets().Create(f.ctxA, &b))
	gb, err := st.Budgets().Get(f.ctxA, b.ID)
	must(t, err)
	if !gb.Amount.Equal(dec("1500")) || len(gb.Thresholds) != 3 || gb.Thresholds[2] != 100 || gb.NodeID == nil {
		t.Fatalf("budget roundtrip: %+v", gb)
	}
	rule2 := model.AlertRule{Name: "anomalies", Kind: model.AlertAnomaly, Config: map[string]string{"min_severity": "warning"}, ChannelIDs: []string{},
		BusinessHours: &model.BusinessHours{Timezone: "Europe/Paris", Days: []int{1, 2, 3, 4, 5}, Start: "09:00", End: "18:00"}, GroupWindowSeconds: 600, Enabled: true}
	must(t, st.AlertRules().Create(f.ctxA, &rule2))
	gr, _ := st.AlertRules().Get(f.ctxA, rule2.ID)
	if gr.BusinessHours == nil || len(gr.BusinessHours.Days) != 5 || gr.Config["min_severity"] != "warning" {
		t.Fatalf("alert rule roundtrip: %+v", gr)
	}
	noBH := model.AlertRule{Name: "uptime", Kind: model.AlertUptime, Enabled: true}
	must(t, st.AlertRules().Create(f.ctxA, &noBH))
	gn, _ := st.AlertRules().Get(f.ctxA, noBH.ID)
	if gn.BusinessHours != nil {
		t.Fatalf("nil business hours must stay nil")
	}
	// Mise à jour et suppression inter-organisations refusées.
	expectErr(t, st.AllocationRules().Update(f.ctxB, &got), tenancy.ErrCrossOrg)
	expectErr(t, st.AllocationRules().Delete(f.ctxB, rule.ID), store.ErrNotFound)
	_, err = st.AllocationRules().Get(f.ctxB, rule.ID)
	expectErr(t, err, store.ErrNotFound)
	_, err = st.AllocationRules().Get(f.ctxA, "not-a-uuid")
	expectErr(t, err, store.ErrNotFound)
	must(t, st.AllocationRules().Delete(f.ctxA, rule.ID))
	expectErr(t, st.AllocationRules().Delete(f.ctxA, rule.ID), store.ErrNotFound)
	// Création avec OrgID d'une autre organisation refusée.
	evil := model.Silence{OrgID: f.orgA, Matchers: map[string]string{}, StartsAt: f.now, EndsAt: f.now.Add(time.Hour)}
	expectErr(t, st.Silences().Create(f.ctxB, &evil), tenancy.ErrCrossOrg)
}

func (f *fixture) analytics(t *testing.T) {
	st := f.st
	res := ids.New()
	r := model.Recommendation{Type: model.RecoRightsizeVM, ResourceID: res, Fingerprint: "fp-" + f.suffix, Title: "Réduire vm", SavingsMonthly: dec("120.50"),
		Currency: "EUR", Risk: "low", Evidence: map[string]any{"p95_cpu": 0.12}, Remediation: model.Remediation{Steps: []string{"a"}, CLI: "openstack server resize"}}
	must(t, st.Recommendations().Upsert(f.ctxA, &r))
	if r.ID == "" || r.Status != model.RecoOpen {
		t.Fatalf("reco create: %+v", r)
	}
	r.Status, r.StatusReason = model.RecoAccepted, "planned"
	must(t, st.Recommendations().Update(f.ctxA, &r))
	again := model.Recommendation{Type: model.RecoRightsizeVM, ResourceID: res, Fingerprint: r.Fingerprint, Title: "Réduire vm (maj)", SavingsMonthly: dec("130"),
		Currency: "EUR", Risk: "low", Remediation: model.Remediation{Steps: []string{"b"}}}
	must(t, st.Recommendations().Upsert(f.ctxA, &again))
	if again.ID != r.ID || again.Status != model.RecoAccepted || again.StatusReason != "planned" || !again.SavingsMonthly.Equal(dec("130")) {
		t.Fatalf("reco upsert must keep user status: %+v", again)
	}
	for i, s := range []string{"50", "200", "130"} {
		x := model.Recommendation{Type: model.RecoOrphanVolume, ResourceID: ids.New(), Fingerprint: fmt.Sprintf("fp-%s-%d", f.suffix, i), Title: "Volume",
			SavingsMonthly: dec(s), Currency: "EUR", Risk: "low"}
		must(t, st.Recommendations().Upsert(f.ctxA, &x))
	}
	list, err := st.Recommendations().List(f.ctxA, store.RecommendationFilter{})
	must(t, err)
	if len(list) != 4 || !list[0].SavingsMonthly.Equal(dec("200")) || !list[3].SavingsMonthly.Equal(dec("50")) {
		t.Fatalf("reco order: %+v", list)
	}
	p1, _ := st.Recommendations().List(f.ctxA, store.RecommendationFilter{Limit: 2})
	p2, _ := st.Recommendations().List(f.ctxA, store.RecommendationFilter{Limit: 2, Cursor: p1[1].ID})
	if len(p2) != 2 || p2[0].ID == p1[0].ID || p2[0].ID == p1[1].ID || !p2[1].SavingsMonthly.Equal(dec("50")) {
		t.Fatalf("reco cursor: %+v / %+v", p1, p2)
	}
	open, _ := st.Recommendations().List(f.ctxA, store.RecommendationFilter{Status: []string{model.RecoOpen}, Types: []string{model.RecoOrphanVolume}})
	if len(open) != 3 {
		t.Fatalf("reco filters: %d", len(open))
	}
	if byRes, _ := st.Recommendations().List(f.ctxA, store.RecommendationFilter{ResourceID: res}); len(byRes) != 1 {
		t.Fatalf("reco by resource")
	}
	if leak, _ := st.Recommendations().List(f.ctxB, store.RecommendationFilter{}); len(leak) != 0 {
		t.Fatalf("reco leak")
	}

	ws := f.now.Add(-72 * time.Hour)
	a := model.Anomaly{SeriesKey: "cost:node:data", Kind: "cost", Title: "Hausse", WindowStart: ws, WindowEnd: ws.Add(48 * time.Hour), Severity: "warning",
		Expected: dec("18.06"), Actual: dec("48.63"), Currency: "EUR", Score: 4.2, Explanation: "spark-worker", ExplanationSources: []string{"event:1"},
		CorrelatedEvents: []model.CorrelatedEvent{{Event: model.Event{Kind: model.EventDeployment, Title: "v4", TS: ws}, Score: 0.9}}}
	must(t, st.Anomalies().Upsert(f.ctxA, &a))
	a.Status = "acknowledged"
	must(t, st.Anomalies().Update(f.ctxA, &a))
	b := model.Anomaly{SeriesKey: a.SeriesKey, Kind: "cost", Title: "Hausse (maj)", WindowStart: ws, WindowEnd: ws.Add(72 * time.Hour), Severity: "critical",
		Expected: dec("18.06"), Actual: dec("52"), Currency: "EUR", Score: 5}
	must(t, st.Anomalies().Upsert(f.ctxA, &b))
	if b.ID != a.ID || b.Status != "acknowledged" || b.Explanation != "spark-worker" || len(b.ExplanationSources) != 1 || b.Severity != "critical" {
		t.Fatalf("anomaly upsert: %+v", b)
	}
	got, err := st.Anomalies().Get(f.ctxA, a.ID)
	must(t, err)
	if len(got.CorrelatedEvents) != 0 || !got.Actual.Equal(dec("52")) {
		t.Fatalf("anomaly refresh: %+v", got)
	}
	if l, _ := st.Anomalies().List(f.ctxA, ws.Add(10*time.Hour), time.Time{}, "acknowledged"); len(l) != 1 {
		t.Fatalf("anomaly list overlap: %d", len(l))
	}
	if l, _ := st.Anomalies().List(f.ctxA, f.now.Add(time.Hour), time.Time{}, ""); len(l) != 0 {
		t.Fatalf("anomaly list after window: %d", len(l))
	}
	if l, _ := st.Anomalies().List(f.ctxA, time.Time{}, ws, ""); len(l) != 0 {
		t.Fatalf("anomaly list before window: %d", len(l))
	}

	fc := model.Forecast{NodeID: "", GeneratedAt: f.now, Model: "holt-winters", Currency: "EUR", PeriodEnd: f.now.Add(96 * time.Hour),
		Points: []model.ForecastPoint{{Day: f.now, Value: dec("100"), Lower: dec("90"), Upper: dec("110")}}, Total: dec("3002.13"), Lower: dec("2900"), Upper: dec("3100")}
	must(t, st.Forecasts().Upsert(f.ctxA, &fc))
	fc.Total = dec("3010")
	must(t, st.Forecasts().Upsert(f.ctxA, &fc))
	nf := fc
	nf.NodeID = ids.New()
	must(t, st.Forecasts().Upsert(f.ctxA, &nf))
	g, err := st.Forecasts().Get(f.ctxA, "")
	must(t, err)
	if !g.Total.Equal(dec("3010")) || len(g.Points) != 1 || !g.Points[0].Upper.Equal(dec("110")) {
		t.Fatalf("forecast: %+v", g)
	}
	if l, _ := st.Forecasts().List(f.ctxA); len(l) != 2 || l[0].NodeID != "" {
		t.Fatalf("forecast list: %+v", l)
	}
	_, err = st.Forecasts().Get(f.ctxB, "")
	expectErr(t, err, store.ErrNotFound)
}

func (f *fixture) ops(t *testing.T) {
	st := f.st
	for i := 0; i < 3; i++ {
		e := model.AlertEvent{Kind: model.AlertBudgetActual, Severity: "warning", Fingerprint: "fp-budget-" + f.suffix, Title: fmt.Sprintf("Budget %d", i),
			Status: model.AlertResolved, Count: 1, FirstAt: f.now, LastAt: f.now}
		if i == 2 {
			e.Status = model.AlertFiring
		}
		must(t, st.AlertEvents().Create(f.ctxA, &e))
	}
	active, err := st.AlertEvents().FindActive(f.ctxA, "fp-budget-"+f.suffix)
	must(t, err)
	if active.Title != "Budget 2" {
		t.Fatalf("active alert: %+v", active)
	}
	_, err = st.AlertEvents().FindActive(f.ctxB, "fp-budget-"+f.suffix)
	expectErr(t, err, store.ErrNotFound)
	evs, err := st.AlertEvents().List(f.ctxA, store.ListQuery{Filters: map[string]string{"status": model.AlertResolved}})
	must(t, err)
	if len(evs) != 2 || evs[0].Title != "Budget 1" {
		t.Fatalf("alert list: %+v", evs)
	}
	pg, _ := st.AlertEvents().List(f.ctxA, store.ListQuery{Limit: 1, Cursor: evs[0].ID, Filters: map[string]string{"kind": model.AlertBudgetActual}})
	if len(pg) != 1 || pg[0].Title != "Budget 0" {
		t.Fatalf("alert cursor: %+v", pg)
	}

	check := model.UptimeCheck{Name: "shop", Kind: "http", Target: "https://shop.example", IntervalSeconds: 60, TimeoutMS: 5000, Regions: []string{"eu-west", "eu-central"},
		ExpectedStatus: 200, FailThreshold: 2, Enabled: true}
	must(t, st.UptimeChecks().Create(f.ctxA, &check))
	gc, _ := st.UptimeChecks().Get(f.ctxA, check.ID)
	if len(gc.Regions) != 2 {
		t.Fatalf("check regions: %+v", gc)
	}
	for i, status := range []string{"resolved", "open"} {
		inc := model.Incident{CheckID: &check.ID, Title: fmt.Sprintf("Incident %d", i), Status: status, Source: "uptime", StartedAt: f.now.Add(time.Duration(i) * time.Hour),
			Updates: []model.IncidentUpdate{{At: f.now, Status: status, Message: "m"}}}
		must(t, st.Incidents().Create(f.ctxA, &inc))
	}
	openInc, err := st.Incidents().OpenForCheck(f.ctxA, check.ID)
	must(t, err)
	if openInc.Title != "Incident 1" || len(openInc.Updates) != 1 {
		t.Fatalf("open incident: %+v", openInc)
	}
	incs, _ := st.Incidents().List(f.ctxA, store.ListQuery{})
	if len(incs) != 2 || incs[0].Title != "Incident 1" {
		t.Fatalf("incident order: %+v", incs)
	}
	hash := "hash"
	page := model.StatusPage{Slug: "status-" + f.suffix, Title: "Statut", Public: false, AccessHash: &hash, CheckIDs: []string{check.ID}, Branding: map[string]string{}}
	must(t, st.StatusPages().Create(f.ctxA, &page))
	org, pid, public, h, err := st.System().FindStatusPage(context.Background(), page.Slug)
	must(t, err)
	if org != f.orgA || pid != page.ID || public || h != "hash" {
		t.Fatalf("status page lookup")
	}

	rep := model.Report{Kind: "monthly_exec", Period: "2026-08", Status: "pending", Summary: map[string]any{"headline": "x", "total": "12.50"}}
	must(t, st.Reports().Create(f.ctxA, &rep))
	gr, err := st.Reports().GetByPeriod(f.ctxA, "monthly_exec", "2026-08")
	must(t, err)
	if gr.ID != rep.ID || gr.Summary["headline"] != "x" {
		t.Fatalf("report: %+v", gr)
	}
	_, err = st.Reports().GetByPeriod(f.ctxB, "monthly_exec", "2026-08")
	expectErr(t, err, store.ErrNotFound)
	expectErr(t, st.Reports().Create(f.ctxA, &model.Report{Kind: "monthly_exec", Period: "2026-08", Status: "pending"}), store.ErrConflict)

	month := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for _, c := range []string{"0.012345", "0.5"} {
		must(t, st.LLMUsage().Append(f.ctxA, &model.LLMUsage{At: month.Add(time.Hour), Feature: "assistant", Provider: "anthropic", Model: "claude-opus-5",
			InputTokens: 1000, OutputTokens: 200, Cost: dec(c), Currency: "USD"}))
	}
	must(t, st.LLMUsage().Append(f.ctxA, &model.LLMUsage{At: month.AddDate(0, -1, 0), Feature: "report", Provider: "anthropic", Model: "m", InputTokens: 5, Cost: dec("1"), Currency: "USD"}))
	tot, err := st.LLMUsage().Totals(f.ctxA, month, month.AddDate(0, 1, 0))
	must(t, err)
	if tot.InputTokens != 2000 || tot.OutputTokens != 400 || tot.Calls != 2 || !tot.Cost.Equal(dec("0.512345")) {
		t.Fatalf("llm totals: %+v", tot)
	}
	if tb, _ := st.LLMUsage().Totals(f.ctxB, month, month.AddDate(0, 1, 0)); tb.Calls != 0 {
		t.Fatalf("llm leak")
	}
}

func (f *fixture) isolation(t *testing.T) {
	st := f.st
	bg := context.Background()
	// Sans organisation, toute lecture ou écriture client échoue.
	if _, err := st.Connectors().List(bg, store.ListQuery{}); !errors.Is(err, tenancy.ErrNoOrg) {
		t.Fatalf("list without org: %v", err)
	}
	if _, err := st.Resources().Current(bg, store.ResourceFilter{}); !errors.Is(err, tenancy.ErrNoOrg) {
		t.Fatalf("resources without org: %v", err)
	}
	if err := st.Budgets().Create(bg, &model.Budget{Name: "x", Period: "monthly", Amount: dec("1"), Currency: "EUR"}); !errors.Is(err, tenancy.ErrNoOrg) {
		t.Fatalf("create without org: %v", err)
	}
	// Une organisation ne peut pas modifier une autre organisation.
	o, _ := st.Orgs().Get(f.ctxA, f.orgA)
	if err := st.Orgs().Update(f.ctxB, &o); !errors.Is(err, tenancy.ErrCrossOrg) {
		t.Fatalf("cross-org update: %v", err)
	}
	c := f.connector(t, f.ctxA)
	if _, err := st.Connectors().Get(f.ctxB, c.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-org get: %v", err)
	}
	if err := st.Connectors().UpdateStatus(f.ctxB, c.ID, model.ConnectorOK, "", f.now, true); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-org status: %v", err)
	}
	c.OrgID = f.orgB
	if err := st.Connectors().Update(f.ctxB, &c); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("moving a connector to another org must fail: %v", err)
	}
	orgIDs, err := st.System().ListOrgIDs(bg)
	must(t, err)
	seen := 0
	for _, id := range orgIDs {
		if id == f.orgA || id == f.orgB {
			seen++
		}
	}
	if seen != 2 {
		t.Fatalf("system list org ids")
	}
}

func (f *fixture) transactions(t *testing.T) {
	st := f.st
	boom := errors.New("boom")
	var nodeID string
	err := st.InTx(f.ctxA, func(ctx context.Context) error {
		n := model.AllocationNode{Kind: model.NodeTeam, Name: "Tx " + f.suffix, Path: "/tx"}
		if err := st.AllocationNodes().Create(ctx, &n); err != nil {
			return err
		}
		nodeID = n.ID
		// Lecture de sa propre écriture dans la transaction.
		if _, err := st.AllocationNodes().Get(ctx, n.ID); err != nil {
			return err
		}
		return boom
	})
	expectErr(t, err, boom)
	if !f.opts.Rollback {
		return // le store mémoire n'offre pas d'annulation
	}
	if _, err := st.AllocationNodes().Get(f.ctxA, nodeID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("rolled back write must not persist: %v", err)
	}
}

func (f *fixture) deleteOrganization(t *testing.T) {
	st := f.st
	id := ids.New()
	ctx := tenancy.WithOrg(context.Background(), id)
	o := model.Organization{ID: id, Name: "Gone " + f.suffix, Slug: "gone-" + f.suffix, Plan: model.PlanTeam, Currency: "EUR", Locale: "fr", Timezone: "UTC"}
	must(t, st.Orgs().Create(ctx, &o))
	must(t, st.Memberships().Upsert(ctx, &model.Membership{UserID: f.userA, Role: model.RoleOwner}))
	c := f.connector(t, ctx)
	must(t, st.Resources().InsertVersions(ctx, []model.Resource{{ID: ids.Resource(id, c.ID, model.TypeVolume, "v"), ConnectorID: c.ID, Provider: "openstack",
		Type: model.TypeVolume, ExternalID: "v", ValidFrom: f.now}}))
	must(t, st.Audit().Append(ctx, &model.AuditEvent{ActorType: "user", ActorID: f.userA, Action: "org.create"}))
	child := model.Organization{ID: ids.New(), ParentOrgID: &id, Name: "Gone child", Slug: "gone-child-" + f.suffix, Plan: model.PlanTeam, Currency: "EUR", Locale: "fr", Timezone: "UTC"}
	must(t, st.Orgs().Create(tenancy.WithOrg(context.Background(), child.ID), &child))

	expectErr(t, st.Orgs().Delete(f.ctxA, id), tenancy.ErrCrossOrg)
	expectErr(t, st.Orgs().Delete(ctx, id), store.ErrConflict) // client MSP rattaché
	must(t, st.Orgs().Delete(tenancy.WithOrg(context.Background(), child.ID), child.ID))
	must(t, st.Orgs().Delete(ctx, id))

	_, err := st.Orgs().Get(ctx, id)
	expectErr(t, err, store.ErrNotFound)
	if n, _ := st.Resources().Count(ctx); n != 0 {
		t.Fatalf("resources survived deletion: %d", n)
	}
	if cs, _ := st.Connectors().List(ctx, store.ListQuery{}); len(cs) != 0 {
		t.Fatalf("connectors survived deletion")
	}
	if evs, _ := st.Audit().List(ctx, store.AuditFilter{}); len(evs) != 0 {
		t.Fatalf("audit survived deletion")
	}
	orgs, _ := st.Orgs().ListForUser(context.Background(), f.userA)
	for _, x := range orgs {
		if x.ID == id {
			t.Fatalf("membership survived deletion")
		}
	}
	// Les autres organisations sont intactes.
	if _, err := st.Orgs().Get(f.ctxA, f.orgA); err != nil {
		t.Fatalf("other org affected: %v", err)
	}
}
