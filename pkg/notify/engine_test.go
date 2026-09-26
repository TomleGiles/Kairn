package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/secrets"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/store/memstore"
	"github.com/kairn-io/kairn/pkg/tenancy"
	"github.com/kairn-io/kairn/pkg/tsdb/memtsdb"
)

type capture struct {
	mu     sync.Mutex
	bodies []map[string]any
	hdrs   []http.Header
}

func (c *capture) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		c.mu.Lock()
		c.bodies = append(c.bodies, m)
		c.hdrs = append(c.hdrs, r.Header.Clone())
		c.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}
}

func (c *capture) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.bodies)
}

type fixture struct {
	ctx   context.Context
	st    *memstore.Store
	eng   *Engine
	now   time.Time
	slack *capture
	pd    *capture
	chSlk model.NotificationChannel
	chPD  model.NotificationChannel
}

func setup(t *testing.T) *fixture {
	t.Helper()
	kek, _ := secrets.GenerateKEK()
	w, _ := secrets.NewLocalKEK(kek)
	kr := secrets.NewKeyring(w)
	f := &fixture{ctx: tenancy.WithOrg(context.Background(), "org-1"), st: memstore.New(), slack: &capture{}, pd: &capture{},
		now: time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)} // lundi 12 h à Paris
	slackSrv := httptest.NewServer(f.slack.handler())
	pdSrv := httptest.NewServer(f.pd.handler())
	t.Cleanup(slackSrv.Close)
	t.Cleanup(pdSrv.Close)
	if err := f.st.Orgs().Create(f.ctx, &model.Organization{ID: "org-1", Name: "Acme", Slug: "acme", Currency: "EUR"}); err != nil {
		t.Fatal(err)
	}
	mk := func(kind string, sec map[string]string) model.NotificationChannel {
		ch := model.NotificationChannel{ID: "ch-" + kind, OrgID: "org-1", Kind: kind, Name: kind, Settings: map[string]string{}, Enabled: true}
		enc, err := kr.EncryptMap(f.ctx, sec, ChannelAAD("org-1", ch.ID))
		if err != nil {
			t.Fatal(err)
		}
		ch.SecretsEnc = enc
		if err := f.st.Channels().Create(f.ctx, &ch); err != nil {
			t.Fatal(err)
		}
		return ch
	}
	f.chSlk = mk(model.ChannelSlack, map[string]string{"webhook_url": slackSrv.URL})
	f.chPD = mk(model.ChannelPagerDuty, map[string]string{"routing_key": "rk"})
	poster := HTTPPoster{Client: slackSrv.Client()}
	senders := DefaultSenders(Email{}, poster)
	senders[model.ChannelPagerDuty] = PagerDuty{HTTPPoster: poster, URL: pdSrv.URL}
	f.eng = &Engine{Store: f.st, TSDB: memtsdb.New(), Keyring: kr, Senders: senders, PublicURL: "https://app.kairn.test",
		Now: func() time.Time { return f.now }}
	return f
}

func (f *fixture) rule(t *testing.T, r model.AlertRule) {
	t.Helper()
	r.OrgID, r.Enabled = "org-1", true
	if err := f.st.AlertRules().Create(f.ctx, &r); err != nil {
		t.Fatal(err)
	}
}

func TestRoutingAndDeduplication(t *testing.T) {
	f := setup(t)
	f.rule(t, model.AlertRule{Name: "anomalies", Kind: model.AlertAnomaly, ChannelIDs: []string{f.chSlk.ID, f.chPD.ID}, GroupWindowSeconds: 3600})
	req := AlertRequest{Kind: model.AlertAnomaly, Severity: "critical", Fingerprint: "anomaly:cost:total", Title: "Hausse de coût", Body: "x", Link: "/anomalies/1"}
	if err := f.eng.Handle(f.ctx, req); err != nil {
		t.Fatal(err)
	}
	if f.slack.count() != 1 || f.pd.count() != 1 {
		t.Fatalf("first alert must reach both channels: slack=%d pd=%d", f.slack.count(), f.pd.count())
	}
	// Même empreinte dans la fenêtre : regroupée, pas de nouvel envoi.
	f.now = f.now.Add(10 * time.Minute)
	_ = f.eng.Handle(f.ctx, req)
	if f.slack.count() != 1 {
		t.Fatalf("duplicate within group window must not be sent again")
	}
	evs, _ := f.st.AlertEvents().List(f.ctx, store.ListQuery{})
	if len(evs) != 1 || evs[0].Count != 2 {
		t.Fatalf("deduplicated event: %+v", evs)
	}
	// Résolution : PagerDuty reçoit un resolve avec la même dedup_key.
	req.Resolved = true
	_ = f.eng.Handle(f.ctx, req)
	last := f.pd.bodies[len(f.pd.bodies)-1]
	if last["event_action"] != "resolve" || last["dedup_key"] != "anomaly:cost:total" {
		t.Fatalf("pagerduty resolve: %+v", last)
	}
}

func TestSilenceAndSeverityFilter(t *testing.T) {
	f := setup(t)
	f.rule(t, model.AlertRule{Name: "reco", Kind: model.AlertRecommendation, ChannelIDs: []string{f.chSlk.ID}, Config: map[string]string{"min_savings": "100"}})
	_ = f.eng.Handle(f.ctx, AlertRequest{Kind: model.AlertRecommendation, Fingerprint: "reco:a", Title: "petite", Payload: map[string]any{"savings_monthly": "12.00"}})
	if f.slack.count() != 0 {
		t.Fatal("recommendation under min_savings must not notify")
	}
	_ = f.eng.Handle(f.ctx, AlertRequest{Kind: model.AlertRecommendation, Fingerprint: "reco:b", Title: "grosse", Payload: map[string]any{"savings_monthly": "450.00"}})
	if f.slack.count() != 1 {
		t.Fatal("high impact recommendation must notify")
	}
	sil := model.Silence{OrgID: "org-1", Matchers: map[string]string{"kind": model.AlertRecommendation}, StartsAt: f.now.Add(-time.Hour), EndsAt: f.now.Add(time.Hour), Reason: "maintenance"}
	_ = f.st.Silences().Create(f.ctx, &sil)
	_ = f.eng.Handle(f.ctx, AlertRequest{Kind: model.AlertRecommendation, Fingerprint: "reco:c", Title: "silencieuse", Payload: map[string]any{"savings_monthly": "999"}})
	if f.slack.count() != 1 {
		t.Fatal("silenced alert must not notify")
	}
}

func TestBusinessHoursDeferral(t *testing.T) {
	f := setup(t)
	f.now = time.Date(2026, 9, 20, 21, 0, 0, 0, time.UTC) // dimanche soir
	f.rule(t, model.AlertRule{Name: "uptime", Kind: model.AlertUptime, ChannelIDs: []string{f.chSlk.ID},
		BusinessHours: &model.BusinessHours{Timezone: "Europe/Paris", Days: []int{1, 2, 3, 4, 5}, Start: "09:00", End: "18:00"}})
	_ = f.eng.Handle(f.ctx, AlertRequest{Kind: model.AlertUptime, Fingerprint: "uptime:x", Title: "down"})
	if f.slack.count() != 0 {
		t.Fatal("outside business hours: deferred")
	}
	f.now = time.Date(2026, 9, 21, 7, 30, 0, 0, time.UTC) // lundi 9 h 30 à Paris
	if err := f.eng.FlushDeferred(f.ctx); err != nil {
		t.Fatal(err)
	}
	if f.slack.count() != 1 {
		t.Fatal("deferred alert must be sent when business hours open")
	}
}

func TestBudgetAlerts(t *testing.T) {
	f := setup(t)
	db := f.eng.TSDB.(*memtsdb.DB)
	// 20 jours à 60 € = 1 200 € pour un budget de 1 000 €.
	for d := 1; d <= 20; d++ {
		day := time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC)
		_ = db.ReplaceCostLines(f.ctx, day, []model.CostLine{{ResourceID: "r", CostType: model.CostCompute, Amount: decimal.NewFromInt(60), Currency: "EUR", AllocationNodeID: model.UnallocatedNodeID}})
	}
	b := model.Budget{OrgID: "org-1", Name: "Cloud", Period: "monthly", Amount: decimal.NewFromInt(1000), Currency: "EUR",
		Thresholds: []int{50, 80, 100}, ForecastAlert: true, ChannelIDs: []string{f.chSlk.ID}}
	_ = f.st.Budgets().Create(f.ctx, &b)
	if err := f.eng.EvaluateBudgets(f.ctx); err != nil {
		t.Fatal(err)
	}
	if f.slack.count() != 1 {
		t.Fatalf("100%% threshold alert expected once, got %d", f.slack.count())
	}
	evs, _ := f.st.AlertEvents().List(f.ctx, store.ListQuery{})
	if len(evs) != 1 || evs[0].Severity != "critical" {
		t.Fatalf("budget event: %+v", evs)
	}
	// Réévaluation : pas de doublon.
	_ = f.eng.EvaluateBudgets(f.ctx)
	if f.slack.count() != 1 {
		t.Fatal("budget alert must be deduplicated")
	}
}

func TestWebhookSignature(t *testing.T) {
	sig := Sign("secret", []byte(`{"a":1}`))
	if len(sig) != 71 || sig[:7] != "sha256=" {
		t.Fatalf("signature format: %s", sig)
	}
}
