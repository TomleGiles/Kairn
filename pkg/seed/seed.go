// Package seed crée l'organisation de démonstration « Acme Retail » :
// OpenStack (type OVHcloud) + Kubernetes, arbre d'allocation, règles,
// budgets, sondes d'uptime, puis importe l'historique et calcule les coûts.
// Utilisé par `make seed` et par le mode démo de l'API.
package seed

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/connectors/demo"
	"github.com/kairn-io/kairn/pkg/costrun"
	"github.com/kairn-io/kairn/pkg/ids"
	"github.com/kairn-io/kairn/pkg/ingest"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/pricing/catalogs"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/tenancy"
	"github.com/kairn-io/kairn/pkg/tsdb"
)

// Identifiants fixes de la démo (stables entre redémarrages).
const (
	OrgID     = "0190f5a0-0000-7000-8000-00000000a001"
	UserID    = "0190f5a0-0000-7000-8000-00000000b001"
	UserEmail = "demo@kairn.local"
)

// Options paramètre le semis.
type Options struct {
	Days int       // historique importé (60 par défaut)
	Now  time.Time // horloge (maintenant par défaut)
	Log  *slog.Logger
}

// Result décrit la démo créée.
type Result struct {
	OrgID   string
	UserID  string
	Created bool
}

func sp(s string) *string { return &s }

// Run crée la démo si elle n'existe pas. Idempotent.
func Run(ctx context.Context, st store.Store, db tsdb.TSDB, opt Options) (Result, error) {
	if opt.Days == 0 {
		opt.Days = 60
	}
	if opt.Now.IsZero() {
		opt.Now = time.Now().UTC()
	}
	log := opt.Log
	if log == nil {
		log = slog.Default()
	}
	res := Result{OrgID: OrgID, UserID: UserID}
	octx := tenancy.WithUser(tenancy.WithOrg(ctx, OrgID), UserID)
	if _, err := st.Orgs().Get(octx, OrgID); err == nil {
		return res, nil
	}
	if _, err := catalogs.InstallSamples(ctx, st); err != nil {
		return res, fmt.Errorf("install catalogs: %w", err)
	}
	if _, err := st.System().FindUser(ctx, "", UserEmail); errors.Is(err, store.ErrNotFound) {
		u := model.User{ID: UserID, Email: UserEmail, Name: "Utilisateur démo", Locale: "fr"}
		if err := st.Users().Create(tenancy.WithUser(ctx, UserID), &u); err != nil {
			return res, fmt.Errorf("create user: %w", err)
		}
	}
	org := model.Organization{
		ID: OrgID, Name: "Acme Retail", Slug: "acme-retail", Plan: model.PlanEnterprise, Currency: "EUR", Locale: "fr", Timezone: "Europe/Paris",
		Settings: model.OrgSettings{LLMProvider: "mistral", K8sAllocationMethod: "max", K8sIdleMode: "keep",
			RightsizingPercentile: 95, RightsizingWindowDays: 14, ReportRecipients: []string{"direction@acme.example"}},
	}
	if err := st.Orgs().Create(octx, &org); err != nil {
		return res, fmt.Errorf("create org: %w", err)
	}
	res.Created = true
	if err := st.Memberships().Upsert(octx, &model.Membership{OrgID: OrgID, UserID: UserID, Role: model.RoleOwner}); err != nil {
		return res, err
	}
	if err := st.Subscriptions().Upsert(octx, &model.Subscription{Plan: model.PlanEnterprise, Status: "active"}); err != nil {
		return res, err
	}

	// Arbre d'allocation.
	node := func(parent *string, kind, name string) (string, error) {
		n := model.AllocationNode{ID: ids.New(), ParentID: parent, Kind: kind, Name: name}
		if parent == nil {
			n.Path = "/" + n.ID + "/"
		} else {
			p, err := st.AllocationNodes().Get(octx, *parent)
			if err != nil {
				return "", err
			}
			n.Path = p.Path + n.ID + "/"
		}
		return n.ID, st.AllocationNodes().Create(octx, &n)
	}
	root, err := node(nil, model.NodeOrganization, "Acme Retail")
	if err != nil {
		return res, err
	}
	bu := map[string]string{}
	for _, name := range []string{"E-commerce", "Data", "Plateforme"} {
		if bu[name], err = node(&root, model.NodeBusinessUnit, name); err != nil {
			return res, err
		}
	}
	shop, _ := node(sp(bu["E-commerce"]), model.NodeTeam, "Équipe Shop")
	search, _ := node(sp(bu["E-commerce"]), model.NodeTeam, "Équipe Search")
	data, _ := node(sp(bu["Data"]), model.NodeTeam, "Équipe Data")
	platform, _ := node(sp(bu["Plateforme"]), model.NodeTeam, "Équipe Plateforme")
	shopSvc, _ := node(&shop, model.NodeService, "Boutique en ligne")
	shopProd, _ := node(&shopSvc, model.NodeEnvironment, "production")
	shopStg, _ := node(&shopSvc, model.NodeEnvironment, "staging")

	rule := func(nodeID, name string, prio int, conds ...model.Condition) error {
		r := model.AllocationRule{NodeID: nodeID, Name: name, Priority: prio, Conditions: conds, Enabled: true}
		return st.AllocationRules().Create(octx, &r)
	}
	eq := func(f, v string) model.Condition { return model.Condition{Field: f, Op: model.OpEq, Value: v} }
	for _, r := range []struct {
		node, name string
		prio       int
		conds      []model.Condition
	}{
		{shopStg, "Shop — staging", 10, []model.Condition{eq("label.team", "shop"), eq("label.env", "staging")}},
		{shopProd, "Shop — production", 20, []model.Condition{eq("label.team", "shop")}},
		{search, "Search", 30, []model.Condition{eq("label.team", "search")}},
		{data, "Data", 40, []model.Condition{eq("label.team", "data")}},
		{platform, "Namespaces plateforme", 50, []model.Condition{{Field: "attr.k8s.namespace", Op: model.OpIn, Values: []string{"kube-system", "monitoring", "ingress-nginx"}}}},
		{platform, "Projet plateforme", 60, []model.Condition{eq("label.team", "platform")}},
		{platform, "Capacité inutilisée des nodes Kubernetes", 70, []model.Condition{eq("type", model.TypeK8sNode)}},
	} {
		if err := rule(r.node, r.name, r.prio, r.conds...); err != nil {
			return res, err
		}
	}
	shared := model.SharedCostRule{Name: "Services partagés Kubernetes", Method: model.ShareProportional, Enabled: true,
		Source:  []model.Condition{{Field: "attr.k8s.namespace", Op: model.OpIn, Values: []string{"kube-system", "monitoring", "ingress-nginx"}}},
		Targets: []model.ShareTarget{{NodeID: shopProd}, {NodeID: search}, {NodeID: data}}}
	if err := st.SharedRules().Create(octx, &shared); err != nil {
		return res, err
	}

	// Budgets et règles d'alerte (sans canal : visibles dans l'application).
	for _, b := range []model.Budget{
		{Name: "Cloud — global", Period: "monthly", Amount: decimal.NewFromInt(3000), Currency: "EUR", Thresholds: []int{50, 80, 100}, ForecastAlert: true, ChannelIDs: []string{}},
		{NodeID: &shop, Name: "Équipe Shop", Period: "monthly", Amount: decimal.NewFromInt(1500), Currency: "EUR", Thresholds: []int{80, 100}, ForecastAlert: true, ChannelIDs: []string{}},
		{NodeID: &data, Name: "Équipe Data", Period: "monthly", Amount: decimal.NewFromInt(700), Currency: "EUR", Thresholds: []int{80, 100}, ForecastAlert: true, ChannelIDs: []string{}},
	} {
		b := b
		if err := st.Budgets().Create(octx, &b); err != nil {
			return res, err
		}
	}
	for _, r := range []model.AlertRule{
		{Name: "Anomalies de coût", Kind: model.AlertAnomaly, Config: map[string]string{"min_severity": "warning"}, ChannelIDs: []string{}, GroupWindowSeconds: 3600, Enabled: true},
		{Name: "Recommandations à fort impact", Kind: model.AlertRecommendation, Config: map[string]string{"min_savings": "100"}, ChannelIDs: []string{}, GroupWindowSeconds: 86400, Enabled: true},
		{Name: "Indisponibilités", Kind: model.AlertUptime, Config: map[string]string{}, ChannelIDs: []string{}, GroupWindowSeconds: 900, Enabled: true},
	} {
		r := r
		if err := st.AlertRules().Create(octx, &r); err != nil {
			return res, err
		}
	}

	// Connecteurs démo et historique.
	epoch := tsdb.TruncDay(opt.Now)
	settings := map[string]string{"epoch": epoch.Format(time.RFC3339), "seed": "acme"}
	conns := []model.Connector{
		{ID: ids.New(), Type: demo.TypeOpenStack, Name: "OVHcloud — GRA11 (démo)", Settings: settings, Enabled: true, IntervalSeconds: 3600, Status: model.ConnectorPending},
		{ID: ids.New(), Type: demo.TypeKubernetes, Name: "Cluster prod-gra (démo)", Settings: settings, Enabled: true, IntervalSeconds: 900, Status: model.ConnectorPending},
	}
	syncer := &ingest.Syncer{Store: st, TSDB: db, Log: log, Now: func() time.Time { return opt.Now }}
	from := epoch.AddDate(0, 0, -opt.Days)
	for i := range conns {
		if err := st.Connectors().Create(octx, &conns[i]); err != nil {
			return res, err
		}
		start := time.Now()
		rep, err := syncer.Backfill(octx, conns[i], from)
		if err != nil {
			return res, fmt.Errorf("backfill %s: %w", conns[i].Type, err)
		}
		log.Info("demo backfill", "connector", conns[i].Type, "resources", rep.Inventory.Observed, "metrics", rep.Metrics,
			"billing", rep.Billing, "events", rep.Events, "duration", time.Since(start).Round(time.Millisecond))
	}
	start := time.Now()
	runner := &costrun.Runner{Store: st, TSDB: db, Log: log}
	if _, err := runner.ComputeRange(octx, from, epoch.AddDate(0, 0, 1)); err != nil {
		return res, fmt.Errorf("compute costs: %w", err)
	}
	for m := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC); !m.After(epoch); m = m.AddDate(0, 1, 0) {
		if _, err := runner.Reconcile(octx, m); err != nil {
			return res, err
		}
	}
	log.Info("demo costs computed", "days", opt.Days, "duration", time.Since(start).Round(time.Millisecond))

	// Coût unitaire : requêtes servies par shop-api.
	if err := unitMetric(octx, st, db, conns[1], shopProd, from, epoch); err != nil {
		return res, err
	}
	if err := uptime(octx, st, db, opt.Now); err != nil {
		return res, err
	}
	return res, nil
}

// unitMetric crée la métrique métier « requêtes » et ses valeurs journalières.
func unitMetric(ctx context.Context, st store.Store, db tsdb.TSDB, k8s model.Connector, node string, from, to time.Time) error {
	m := model.UnitMetric{Name: "Coût par millier de requêtes API", UnitLabel: "1 000 requêtes", NodeID: &node, Source: "api"}
	if err := st.UnitMetrics().Create(ctx, &m); err != nil {
		return err
	}
	wl := ids.Resource(OrgID, k8s.ID, model.TypeK8sWorkload, "prod-gra/shop/Deployment/shop-api")
	series, err := db.QueryMetrics(ctx, tsdb.MetricQuery{ResourceIDs: []string{wl}, Metrics: []string{model.MetricRequestsPerSec},
		From: from, To: to, Step: time.Hour, Agg: tsdb.AggAvg})
	if err != nil {
		return err
	}
	daily := map[time.Time]float64{}
	for _, s := range series {
		for _, p := range s.Points {
			daily[tsdb.TruncDay(p.TS)] += p.Value * 3600 / 1000
		}
	}
	vals := map[time.Time]decimal.Decimal{}
	for d, v := range daily {
		vals[d] = decimal.NewFromFloat(math.Round(v*100) / 100)
	}
	return db.WriteUnitValues(ctx, m.ID, vals)
}

// uptime crée deux sondes, une page de statut et 30 jours de résultats simulés
// (dont un incident de 25 minutes il y a 9 jours).
func uptime(ctx context.Context, st store.Store, db tsdb.TSDB, now time.Time) error {
	checks := []model.UptimeCheck{
		{Name: "Boutique — page d'accueil", Kind: "http", Target: "https://shop.acme.example/", IntervalSeconds: 60, TimeoutMS: 10000,
			Regions: []string{"eu-west-gra", "eu-west-par"}, ExpectedStatus: 200, FailThreshold: 2, Enabled: false},
		{Name: "API publique", Kind: "http", Target: "https://api.acme.example/health", IntervalSeconds: 60, TimeoutMS: 10000,
			Regions: []string{"eu-west-gra", "eu-central-waw"}, ExpectedStatus: 200, FailThreshold: 2, Enabled: false},
	}
	var idsList []string
	for i := range checks {
		if err := st.UptimeChecks().Create(ctx, &checks[i]); err != nil {
			return err
		}
		idsList = append(idsList, checks[i].ID)
	}
	outageStart := tsdb.TruncDay(now).AddDate(0, 0, -9).Add(14*time.Hour + 5*time.Minute)
	outageEnd := outageStart.Add(25 * time.Minute)
	var rs []model.UptimeResult
	for t := now.AddDate(0, 0, -30).Truncate(5 * time.Minute); t.Before(now); t = t.Add(5 * time.Minute) {
		for ci, c := range checks {
			for ri, region := range c.Regions {
				up := !(ci == 1 && !t.Before(outageStart) && t.Before(outageEnd))
				lat := 45 + float64((t.Unix()/300+int64(ci*7+ri*3))%40)
				r := model.UptimeResult{CheckID: c.ID, Region: region, TS: t, Up: up, LatencyMS: lat, StatusCode: 200}
				if !up {
					r.StatusCode, r.Error, r.LatencyMS = 503, "HTTP 503 Service Unavailable", 0
				}
				rs = append(rs, r)
			}
		}
	}
	if err := db.WriteUptime(ctx, rs); err != nil {
		return err
	}
	resolved := outageEnd
	inc := model.Incident{CheckID: &idsList[1], Title: "API publique indisponible", Status: "resolved", Source: "uptime",
		StartedAt: outageStart, ResolvedAt: &resolved, Updates: []model.IncidentUpdate{
			{At: outageStart, Status: "open", Message: "Erreurs 503 détectées depuis 2 régions."},
			{At: outageEnd, Status: "resolved", Message: "Service rétabli après redémarrage du pool d'API."},
		}}
	if err := st.Incidents().Create(ctx, &inc); err != nil {
		return err
	}
	page := model.StatusPage{Slug: "acme", Title: "Statut des services Acme", Public: true, CheckIDs: idsList, Branding: map[string]string{"primary_color": "#0f766e"}}
	return st.StatusPages().Create(ctx, &page)
}
