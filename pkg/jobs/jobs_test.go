package jobs

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	_ "github.com/kairn-io/kairn/connectors/demo"
	"github.com/kairn-io/kairn/pkg/bus"
	"github.com/kairn-io/kairn/pkg/costrun"
	"github.com/kairn-io/kairn/pkg/ingest"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/notify"
	"github.com/kairn-io/kairn/pkg/objstore"
	"github.com/kairn-io/kairn/pkg/secrets"
	"github.com/kairn-io/kairn/pkg/seed"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/store/memstore"
	"github.com/kairn-io/kairn/pkg/tenancy"
	"github.com/kairn-io/kairn/pkg/tsdb"
	"github.com/kairn-io/kairn/pkg/tsdb/memtsdb"
)

// services simule analytics et ai-service et enregistre les appels.
type services struct {
	mu    sync.Mutex
	calls []string
	srv   *httptest.Server
}

func newServices(t *testing.T) *services {
	s := &services{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Kairn-Service-Token") != "svc" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		s.calls = append(s.calls, r.URL.Path+" "+body["org_id"]+" "+body["period"])
		s.mu.Unlock()
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *services) list() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

type env struct {
	w    *Worker
	st   *memstore.Store
	db   *memtsdb.DB
	bus  *bus.Memory
	svc  *services
	now  time.Time
	ctx  context.Context
	conn model.Connector
}

func newEnv(t *testing.T) *env {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	now := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	st, db := memstore.New(), memtsdb.New()
	if _, err := seed.Run(context.Background(), st, db, seed.Options{Days: 3, Now: now, Log: log}); err != nil {
		t.Fatal(err)
	}
	kek, _ := secrets.GenerateKEK()
	kw, _ := secrets.NewLocalKEK(kek)
	kr := secrets.NewKeyring(kw)
	b := bus.NewMemory(log)
	b.Sync = true
	svc := newServices(t)
	clock := func() time.Time { return now }
	w := &Worker{Store: st, TSDB: db, Bus: b, Keyring: kr, Objects: objstore.Local{Dir: t.TempDir()}, Log: log, Now: clock,
		Syncer:       &ingest.Syncer{Store: st, TSDB: db, Keyring: kr, Log: log, Now: clock},
		Costs:        &costrun.Runner{Store: st, TSDB: db, Log: log},
		Notify:       &notify.Engine{Store: st, TSDB: db, Keyring: kr, Log: log, Now: clock, Senders: map[string]notify.Sender{}},
		AnalyticsURL: svc.srv.URL, AIURL: svc.srv.URL, ServiceToken: "svc"}
	e := &env{w: w, st: st, db: db, bus: b, svc: svc, now: now, ctx: tenancy.WithOrg(context.Background(), seed.OrgID)}
	conns, err := st.Connectors().List(e.ctx, store.ListQuery{})
	if err != nil || len(conns) == 0 {
		t.Fatalf("seeded connectors: %v", err)
	}
	e.conn = conns[0]
	return e
}

func TestSyncCostAndAnalyticsPipeline(t *testing.T) {
	e := newEnv(t)
	if err := e.w.Subscribe(RoleIngest, RoleCost, RoleNotifier, RoleAnalytics, RoleReports); err != nil {
		t.Fatal(err)
	}
	jobs := BusJobs{Bus: e.bus}
	if err := jobs.RequestSync(e.ctx, seed.OrgID, e.conn.ID, 0); err != nil {
		t.Fatal(err)
	}
	e.bus.Wait()
	c, _ := e.st.Connectors().Get(e.ctx, e.conn.ID)
	if c.LastSyncAt == nil || c.Status != model.ConnectorOK {
		t.Fatalf("connector not synced: %+v", c)
	}
	rows, err := e.db.QueryCosts(e.ctx, tsdb.CostQuery{From: e.now.AddDate(0, 0, -1), To: e.now.AddDate(0, 0, 1), Granularity: tsdb.GranTotal})
	if err != nil || len(rows) == 0 || !rows[0].Amount.IsPositive() {
		t.Fatalf("costs recomputed after sync: %+v %v", rows, err)
	}
	calls := e.svc.list()
	if len(calls) == 0 || calls[0] != "/v1/run "+seed.OrgID+" " {
		t.Fatalf("analytics triggered after cost computation: %v", calls)
	}
	// Recalcul explicite : l'analytics n'est pas relancée dans les 30 minutes.
	if err := jobs.RequestRecompute(e.ctx, seed.OrgID, e.now.AddDate(0, 0, -2), e.now); err != nil {
		t.Fatal(err)
	}
	e.bus.Wait()
	if n := len(e.svc.list()); n != len(calls) {
		t.Fatalf("analytics must be throttled: %d calls", n)
	}
	// Rapport mensuel : génération déléguée à ai-service ; pas d'e-mail sans SMTP configuré.
	if err := jobs.RequestReport(e.ctx, seed.OrgID, "2026-08"); err != nil {
		t.Fatal(err)
	}
	e.bus.Wait()
	last := e.svc.list()[len(e.svc.list())-1]
	if last != "/v1/reports/generate "+seed.OrgID+" 2026-08" {
		t.Fatalf("report generation: %v", e.svc.list())
	}
}

func TestSyncFailureAlertsAndMarksConnector(t *testing.T) {
	e := newEnv(t)
	var alerts []bus.Message
	_ = e.bus.Subscribe(bus.SubjectAlert, "test", func(_ context.Context, m bus.Message) error { alerts = append(alerts, m); return nil })
	if err := e.w.Subscribe(RoleIngest); err != nil {
		t.Fatal(err)
	}
	broken := model.Connector{Type: "openstack", Name: "cassé", Settings: map[string]string{"auth_url": "http://127.0.0.1:1/v3", "application_credential_id": "x"},
		Enabled: true, Status: model.ConnectorPending, IntervalSeconds: 3600}
	if err := e.st.Connectors().Create(e.ctx, &broken); err != nil {
		t.Fatal(err)
	}
	_ = BusJobs{Bus: e.bus}.RequestSync(e.ctx, seed.OrgID, broken.ID, 0)
	e.bus.Wait()
	c, _ := e.st.Connectors().Get(e.ctx, broken.ID)
	if c.Status != model.ConnectorError || c.StatusMessage == "" {
		t.Fatalf("connector status after failure: %+v", c)
	}
	if len(alerts) == 0 {
		t.Fatal("a connector alert must be raised")
	}
}

// recorder est un bus qui enregistre les publications (ordonnanceur).
type recorder struct {
	mu   sync.Mutex
	subs []string
}

func (r *recorder) Publish(_ context.Context, subject, orgID string, _ any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.subs = append(r.subs, subject)
	return nil
}
func (r *recorder) Subscribe(string, string, bus.Handler) error { return nil }
func (r *recorder) Close() error                                { return nil }

func (r *recorder) count(subject string) int {
	n := 0
	for _, s := range r.subs {
		if s == subject {
			n++
		}
	}
	return n
}

func TestSchedulerPublishesDueJobs(t *testing.T) {
	e := newEnv(t)
	rec := &recorder{}
	e.w.Bus = rec
	// Deux heures après le semis : les connecteurs horaires sont dus.
	later := e.now.Add(2 * time.Hour)
	e.w.Now = func() time.Time { return later }
	sched := &Scheduler{Worker: e.w}
	sched.RunOnce(context.Background())
	conns, _ := e.st.Connectors().List(e.ctx, store.ListQuery{})
	if rec.count(bus.SubjectSyncRequested) == 0 || rec.count(bus.SubjectSyncRequested) > len(conns) {
		t.Fatalf("sync jobs: %v", rec.subs)
	}
	if rec.count(bus.SubjectCostRequested) != 1 || rec.count(bus.SubjectAnalytics) != 1 {
		t.Fatalf("hourly/daily jobs: %v", rec.subs)
	}
	// Second passage immédiat : rien de nouveau (intervalles respectés).
	before := len(rec.subs)
	sched.RunOnce(context.Background())
	if len(rec.subs) != before {
		t.Fatalf("jobs republished before their interval: %v", rec.subs[before:])
	}
	// Le 1er du mois à 6 h, le rapport du mois précédent est demandé une seule fois.
	e.w.Now = func() time.Time { return time.Date(2026, 10, 1, 7, 0, 0, 0, time.UTC) }
	sched.RunOnce(context.Background())
	sched.RunOnce(context.Background())
	if rec.count(bus.SubjectReport) != 1 {
		t.Fatalf("monthly report: %v", rec.subs)
	}
}
