// Package jobs orchestre les traitements asynchrones de Kairn autour du bus :
// synchronisations, recalculs de coûts, budgets, analytics, rapports et
// exports. Chaque service s'abonne aux sujets qui le concernent ; le mode démo
// les exécute tous dans un seul processus.
package jobs

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kairn-io/kairn/pkg/bus"
	"github.com/kairn-io/kairn/pkg/costrun"
	"github.com/kairn-io/kairn/pkg/export"
	"github.com/kairn-io/kairn/pkg/ingest"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/notify"
	"github.com/kairn-io/kairn/pkg/objstore"
	"github.com/kairn-io/kairn/pkg/plans"
	"github.com/kairn-io/kairn/pkg/pricing/catalogs"
	"github.com/kairn-io/kairn/pkg/secrets"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/tenancy"
	"github.com/kairn-io/kairn/pkg/tsdb"
)

// SyncJob demande la synchronisation d'un connecteur.
type SyncJob struct {
	ConnectorID  string `json:"connector_id"`
	BackfillDays int    `json:"backfill_days"`
}

// SyncedEvent signale une synchronisation terminée.
type SyncedEvent struct {
	ConnectorID string    `json:"connector_id"`
	From        time.Time `json:"from"`
	Backfill    bool      `json:"backfill"`
}

// CostJob demande le recalcul d'une période.
type CostJob struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// ReportJob demande un rapport.
type ReportJob struct {
	Period string `json:"period"`
}

// BusJobs implémente l'interface Jobs de l'API en publiant sur le bus.
type BusJobs struct{ Bus bus.Bus }

// RequestSync publie une demande de synchronisation.
func (b BusJobs) RequestSync(ctx context.Context, orgID, connectorID string, days int) error {
	return b.Bus.Publish(ctx, bus.SubjectSyncRequested, orgID, SyncJob{ConnectorID: connectorID, BackfillDays: days})
}

// RequestRecompute publie une demande de recalcul.
func (b BusJobs) RequestRecompute(ctx context.Context, orgID string, from, to time.Time) error {
	return b.Bus.Publish(ctx, bus.SubjectCostRequested, orgID, CostJob{From: from, To: to})
}

// RequestAnalytics publie une demande d'analyse.
func (b BusJobs) RequestAnalytics(ctx context.Context, orgID string) error {
	return b.Bus.Publish(ctx, bus.SubjectAnalytics, orgID, struct{}{})
}

// RequestReport publie une demande de rapport.
func (b BusJobs) RequestReport(ctx context.Context, orgID, period string) error {
	return b.Bus.Publish(ctx, bus.SubjectReport, orgID, ReportJob{Period: period})
}

// Worker exécute les traitements.
type Worker struct {
	Store   store.Store
	TSDB    tsdb.TSDB
	Bus     bus.Bus
	Syncer  *ingest.Syncer
	Costs   *costrun.Runner
	Notify  *notify.Engine
	Keyring *secrets.Keyring
	Objects objstore.Store
	Log     *slog.Logger
	Now     func() time.Time
	HTTP    *http.Client
	// Services Python (facultatifs).
	AnalyticsURL string
	AIURL        string
	ServiceToken string
	// E-mail des rapports.
	Email notify.Email
	// PriceImport : fournisseurs dont la grille publique est importée chaque jour.
	PriceImport []string

	mu       sync.Mutex
	inflight map[string]bool
	lastRun  map[string]time.Time
}

func (w *Worker) now() time.Time {
	if w.Now != nil {
		return w.Now().UTC()
	}
	return time.Now().UTC()
}

func (w *Worker) log() *slog.Logger {
	if w.Log != nil {
		return w.Log
	}
	return slog.Default()
}

func (w *Worker) client() *http.Client {
	if w.HTTP != nil {
		return w.HTTP
	}
	return &http.Client{Timeout: 10 * time.Minute}
}

// claim évite deux exécutions simultanées d'une même clé dans ce processus.
func (w *Worker) claim(key string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.inflight == nil {
		w.inflight = map[string]bool{}
	}
	if w.inflight[key] {
		return false
	}
	w.inflight[key] = true
	return true
}

func (w *Worker) release(key string) {
	w.mu.Lock()
	delete(w.inflight, key)
	w.mu.Unlock()
}

// Rôles d'abonnement.
const (
	RoleIngest    = "ingest"
	RoleCost      = "cost-engine"
	RoleNotifier  = "notifier"
	RoleAnalytics = "analytics-trigger"
	RoleReports   = "reports"
)

// Subscribe abonne le worker aux sujets des rôles demandés.
func (w *Worker) Subscribe(roles ...string) error {
	has := map[string]bool{}
	for _, r := range roles {
		has[r] = true
	}
	subs := []struct {
		role, subject string
		h             bus.Handler
	}{
		{RoleIngest, bus.SubjectSyncRequested, w.handleSync},
		{RoleCost, bus.SubjectConnectorSynced, w.handleSynced},
		{RoleCost, bus.SubjectCostRequested, w.handleCost},
		{RoleNotifier, bus.SubjectAlert, w.handleAlert},
		{RoleNotifier, bus.SubjectWebhookOut, w.handleWebhookOut},
		{RoleNotifier, bus.SubjectCostComputed, w.handleCostComputed},
		{RoleAnalytics, bus.SubjectAnalytics, w.handleAnalytics},
		{RoleReports, bus.SubjectReport, w.handleReport},
	}
	for _, s := range subs {
		if !has[s.role] {
			continue
		}
		if err := w.Bus.Subscribe(s.subject, s.role, s.h); err != nil {
			return fmt.Errorf("subscribe %s: %w", s.subject, err)
		}
	}
	return nil
}

func (w *Worker) handleSync(ctx context.Context, m bus.Message) error {
	var job SyncJob
	if err := m.Decode(&job); err != nil {
		w.log().Warn("invalid message dropped", "subject", m.Subject, "err", err)
		return nil //nolint:nilerr // message illisible : acquitté, une nouvelle tentative échouerait aussi
	}
	ctx = tenancy.WithOrg(ctx, m.OrgID)
	key := m.OrgID + "/" + job.ConnectorID
	if !w.claim("sync:" + key) {
		return nil
	}
	defer w.release("sync:" + key)
	c, err := w.Store.Connectors().Get(ctx, job.ConnectorID)
	if err != nil {
		return nil // connecteur supprimé
	}
	now := w.now()
	var from time.Time
	if job.BackfillDays > 0 {
		from = now.AddDate(0, 0, -job.BackfillDays)
		if _, err := w.Syncer.Backfill(ctx, c, from); err != nil {
			w.alertConnector(ctx, c, err)
			return nil
		}
	} else {
		if _, err := w.Syncer.Sync(ctx, c, ingest.All); err != nil {
			w.alertConnector(ctx, c, err)
			return nil
		}
		from = now.AddDate(0, 0, -1)
	}
	return w.Bus.Publish(ctx, bus.SubjectConnectorSynced, m.OrgID, SyncedEvent{ConnectorID: c.ID, From: from, Backfill: job.BackfillDays > 0})
}

func (w *Worker) alertConnector(ctx context.Context, c model.Connector, err error) {
	w.log().Warn("connector sync failed", "org", c.OrgID, "connector", c.ID, "err", err)
	_ = w.Bus.Publish(ctx, bus.SubjectAlert, c.OrgID, notify.AlertRequest{
		Kind: model.AlertConnector, Severity: "warning", Fingerprint: "connector:" + c.ID,
		Title: "Synchronisation en échec : " + c.Name, Body: truncate(err.Error(), 400), Link: "/connectors/" + c.ID,
		Payload: map[string]any{"connector": c.Name, "type": c.Type},
	})
	_ = w.Bus.Publish(ctx, bus.SubjectWebhookOut, c.OrgID, map[string]any{"event": "connector.failed", "connector_id": c.ID})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func (w *Worker) handleSynced(ctx context.Context, m bus.Message) error {
	var ev SyncedEvent
	if err := m.Decode(&ev); err != nil {
		w.log().Warn("invalid message dropped", "subject", m.Subject, "err", err)
		return nil //nolint:nilerr // message illisible : acquitté, une nouvelle tentative échouerait aussi
	}
	from := tsdb.TruncDay(ev.From)
	to := tsdb.TruncDay(w.now()).AddDate(0, 0, 1)
	return w.recompute(tenancy.WithOrg(ctx, m.OrgID), m.OrgID, from, to, ev.Backfill)
}

func (w *Worker) handleCost(ctx context.Context, m bus.Message) error {
	var job CostJob
	if err := m.Decode(&job); err != nil {
		w.log().Warn("invalid message dropped", "subject", m.Subject, "err", err)
		return nil //nolint:nilerr // message illisible : acquitté, une nouvelle tentative échouerait aussi
	}
	return w.recompute(tenancy.WithOrg(ctx, m.OrgID), m.OrgID, job.From, job.To, true)
}

// recompute recalcule une période puis déclenche budgets et analytics.
func (w *Worker) recompute(ctx context.Context, orgID string, from, to time.Time, reconcile bool) error {
	if !w.claim("cost:" + orgID) {
		// Un calcul est en cours : on relance après lui la période demandée.
		go func() {
			time.Sleep(5 * time.Second)
			_ = w.Bus.Publish(context.WithoutCancel(ctx), bus.SubjectCostRequested, orgID, CostJob{From: from, To: to})
		}()
		return nil
	}
	defer w.release("cost:" + orgID)
	if _, err := w.Costs.ComputeRange(ctx, from, to); err != nil {
		return fmt.Errorf("compute costs: %w", err)
	}
	if reconcile {
		for m := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC); m.Before(to); m = m.AddDate(0, 1, 0) {
			if _, err := w.Costs.Reconcile(ctx, m); err != nil {
				w.log().Warn("reconcile failed", "org", orgID, "err", err)
			}
		}
	}
	return w.Bus.Publish(ctx, bus.SubjectCostComputed, orgID, CostJob{From: from, To: to})
}

func (w *Worker) handleCostComputed(ctx context.Context, m bus.Message) error {
	ctx = tenancy.WithOrg(ctx, m.OrgID)
	if w.Notify != nil {
		if err := w.Notify.EvaluateBudgets(ctx); err != nil {
			w.log().Warn("budget evaluation failed", "org", m.OrgID, "err", err)
		}
	}
	// Analytics au plus toutes les 30 minutes par organisation après un recalcul.
	w.mu.Lock()
	if w.lastRun == nil {
		w.lastRun = map[string]time.Time{}
	}
	last := w.lastRun["analytics:"+m.OrgID]
	due := w.now().Sub(last) > 30*time.Minute
	if due {
		w.lastRun["analytics:"+m.OrgID] = w.now()
	}
	w.mu.Unlock()
	if due {
		return w.Bus.Publish(ctx, bus.SubjectAnalytics, m.OrgID, struct{}{})
	}
	return nil
}

func (w *Worker) handleAlert(ctx context.Context, m bus.Message) error {
	var req notify.AlertRequest
	if err := m.Decode(&req); err != nil {
		w.log().Warn("invalid message dropped", "subject", m.Subject, "err", err)
		return nil //nolint:nilerr // message illisible : acquitté, une nouvelle tentative échouerait aussi
	}
	return w.Notify.Handle(tenancy.WithOrg(ctx, m.OrgID), req)
}

func (w *Worker) handleWebhookOut(ctx context.Context, m bus.Message) error {
	var data map[string]any
	if err := m.Decode(&data); err != nil {
		w.log().Warn("invalid message dropped", "subject", m.Subject, "err", err)
		return nil //nolint:nilerr // message illisible : acquitté, une nouvelle tentative échouerait aussi
	}
	event, _ := data["event"].(string)
	if event == "" {
		return nil
	}
	w.Notify.DeliverWebhooks(tenancy.WithOrg(ctx, m.OrgID), event, data)
	return nil
}

// callService appelle un service Python interne.
func (w *Worker) callService(ctx context.Context, base, path string, payload any) error {
	if base == "" {
		return errors.New("service not configured")
	}
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Kairn-Service-Token", w.ServiceToken)
	resp, err := w.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return fmt.Errorf("%s: status %d: %s", path, resp.StatusCode, b)
	}
	return nil
}

func (w *Worker) handleAnalytics(ctx context.Context, m bus.Message) error {
	if w.AnalyticsURL == "" {
		w.log().Debug("analytics service not configured; skipping", "org", m.OrgID)
		return nil
	}
	if !w.claim("analytics:" + m.OrgID) {
		return nil
	}
	defer w.release("analytics:" + m.OrgID)
	if err := w.callService(ctx, w.AnalyticsURL, "/v1/run", map[string]string{"org_id": m.OrgID}); err != nil {
		w.log().Warn("analytics run failed", "org", m.OrgID, "err", err)
	}
	return nil
}

func (w *Worker) handleReport(ctx context.Context, m bus.Message) error {
	var job ReportJob
	if err := m.Decode(&job); err != nil {
		w.log().Warn("invalid message dropped", "subject", m.Subject, "err", err)
		return nil //nolint:nilerr // message illisible : acquitté, une nouvelle tentative échouerait aussi
	}
	ctx = tenancy.WithOrg(ctx, m.OrgID)
	if err := w.callService(ctx, w.AIURL, "/v1/reports/generate", map[string]string{"org_id": m.OrgID, "period": job.Period}); err != nil {
		w.log().Warn("report generation failed", "org", m.OrgID, "period", job.Period, "err", err)
		return nil
	}
	return w.SendReport(ctx, job.Period)
}

// SendReport envoie par e-mail le rapport prêt d'une période aux destinataires configurés.
func (w *Worker) SendReport(ctx context.Context, period string) error {
	orgID, err := tenancy.OrgID(ctx)
	if err != nil {
		return err
	}
	org, err := w.Store.Orgs().Get(ctx, orgID)
	if err != nil {
		return err
	}
	r, err := w.Store.Reports().GetByPeriod(ctx, "monthly_exec", period)
	if errors.Is(err, store.ErrNotFound) {
		return nil // rapport non encore généré
	}
	if err != nil {
		return err
	}
	if r.Status != "ready" || r.ObjectKey == "" || len(org.Settings.ReportRecipients) == 0 || w.Email.Addr == "" {
		return nil
	}
	pdf, err := w.Objects.Get(ctx, r.ObjectKey)
	if err != nil {
		return err
	}
	brand := reportBrand(r.Summary)
	name := "rapport-mensuel-" + period + ".pdf"
	part := "Content-Type: application/pdf; name=\"" + name + "\"\r\nContent-Disposition: attachment; filename=\"" + name +
		"\"\r\nContent-Transfer-Encoding: base64\r\n\r\n" + wrap76(base64.StdEncoding.EncodeToString(pdf))
	text := fmt.Sprintf("Bonjour,\n\nVeuillez trouver ci-joint le rapport mensuel %s de %s pour %s.\n", brand, org.Name, period)
	if summary, ok := r.Summary["headline"].(string); ok {
		text += "\n" + summary + "\n"
	}
	if err := notify.SendMail(w.Email, org.Settings.ReportRecipients, brand+" — rapport mensuel "+period, text, part); err != nil {
		return fmt.Errorf("send report: %w", err)
	}
	now := w.now()
	r.Status, r.SentAt = "sent", &now
	return w.Store.Reports().Update(ctx, &r)
}

// reportBrand renvoie la marque du rapport : Kairn ou la marque blanche du MSP
// posée par le service ai, sans caractère de contrôle (objet de l'e-mail).
func reportBrand(summary map[string]any) string {
	b, _ := summary["brand"].(string)
	b = strings.TrimSpace(strings.Map(func(c rune) rune {
		if c < 0x20 || c == 0x7f {
			return -1
		}
		return c
	}, b))
	if b == "" {
		return "Kairn"
	}
	return b
}

func wrap76(s string) string {
	var b strings.Builder
	for len(s) > 76 {
		b.WriteString(s[:76] + "\r\n")
		s = s[76:]
	}
	b.WriteString(s)
	return b.String()
}

// ------------------------------------------------------------------ ordonnanceur

// Scheduler déclenche les tâches périodiques.
type Scheduler struct {
	Worker *Worker
	// Tick est la période de la boucle (1 minute par défaut).
	Tick time.Duration
	// Probes fournit les régions de sonde actives (facultatif).
	last map[string]time.Time
}

// Run exécute la boucle jusqu'à l'annulation du contexte.
func (s *Scheduler) Run(ctx context.Context) {
	tick := s.Tick
	if tick == 0 {
		tick = time.Minute
	}
	s.last = map[string]time.Time{}
	t := time.NewTicker(tick)
	defer t.Stop()
	s.RunOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.RunOnce(ctx)
		}
	}
}

func (s *Scheduler) every(key string, d time.Duration, now time.Time) bool {
	if now.Sub(s.last[key]) >= d {
		s.last[key] = now
		return true
	}
	return false
}

// RunOnce exécute une itération de l'ordonnanceur.
func (s *Scheduler) RunOnce(ctx context.Context) {
	w := s.Worker
	if s.last == nil {
		s.last = map[string]time.Time{}
	}
	now := w.now()
	orgs, err := w.Store.System().ListOrgIDs(tenancy.WithSystem(ctx))
	if err != nil {
		w.log().Error("scheduler: list orgs", "err", err)
		return
	}
	hourly := s.every("hourly", time.Hour, now)
	daily := s.every("daily", 24*time.Hour, now)
	if daily {
		// Grilles publiques d'abord : le recalcul horaire qui suit en profite.
		s.ImportPrices(ctx, now, orgs)
	}
	for _, orgID := range orgs {
		octx := tenancy.WithOrg(ctx, orgID)
		conns, err := store.ListAll(octx, w.Store.Connectors(), func(c model.Connector) string { return c.ID }, nil)
		if err != nil {
			continue
		}
		for _, c := range conns {
			if !c.Enabled {
				continue
			}
			interval := time.Duration(c.IntervalSeconds) * time.Second
			if c.LastSyncAt == nil || now.Sub(*c.LastSyncAt) >= interval {
				if s.every("sync:"+c.ID, interval, now) {
					_ = w.Bus.Publish(octx, bus.SubjectSyncRequested, orgID, SyncJob{ConnectorID: c.ID})
				}
			}
		}
		if hourly {
			_ = w.Bus.Publish(octx, bus.SubjectCostRequested, orgID, CostJob{From: tsdb.TruncDay(now).AddDate(0, 0, -1), To: tsdb.TruncDay(now).AddDate(0, 0, 1)})
			if w.Notify != nil {
				_ = w.Notify.FlushDeferred(octx)
			}
			if err := w.TSDB.Rollup(octx, now.Add(-3*time.Hour).Truncate(time.Hour), now.Truncate(time.Hour)); err != nil {
				w.log().Warn("rollup failed", "org", orgID, "err", err)
			}
		}
		if daily {
			_ = w.Bus.Publish(octx, bus.SubjectAnalytics, orgID, struct{}{})
			s.runExports(octx, now)
		}
		// Rapport mensuel : le 1er du mois, à partir de 6 h UTC.
		if now.Day() == 1 && now.Hour() >= 6 {
			period := now.AddDate(0, -1, 0).Format("2006-01")
			if s.every("report:"+orgID+":"+period, 31*24*time.Hour, now) {
				if org, err := w.Store.Orgs().Get(octx, orgID); err == nil && plans.For(org, now).Allows(plans.FeatureReports) {
					_ = w.Bus.Publish(octx, bus.SubjectReport, orgID, ReportJob{Period: period})
				}
			}
		}
	}
}

// ImportPrices importe les grilles publiques des fournisseurs configurés.
// Quand une première grille officielle remplace une grille d'exemple (elle
// s'applique alors au passé), les coûts du mois précédent et du mois en cours
// sont recalculés pour chaque organisation ; l'historique plus ancien peut
// être recalculé à la demande (POST /costs/recompute).
func (s *Scheduler) ImportPrices(ctx context.Context, now time.Time, orgs []string) []catalogs.Result {
	w := s.Worker
	if len(w.PriceImport) == 0 {
		return nil
	}
	ictx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	res, err := catalogs.ImportAll(ictx, w.Store, w.client(), now, w.PriceImport, w.log())
	if err != nil {
		w.log().Warn("price catalog import incomplete", "err", err)
	}
	backdated := false
	for _, r := range res {
		if r.Created && r.ValidFrom.Equal(catalogs.FirstValidFrom) {
			backdated = true
		}
	}
	if backdated {
		today := tsdb.TruncDay(now)
		from := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 0)
		for _, orgID := range orgs {
			_ = w.Bus.Publish(tenancy.WithOrg(ctx, orgID), bus.SubjectCostRequested, orgID, CostJob{From: from, To: today.AddDate(0, 0, 1)})
		}
	}
	return res
}

// runExports exécute les exports planifiés dus.
func (s *Scheduler) runExports(ctx context.Context, now time.Time) {
	w := s.Worker
	jobs, err := store.ListAll(ctx, w.Store.Exports(), func(e model.ExportJob) string { return e.ID }, nil)
	if err != nil {
		return
	}
	for _, j := range jobs {
		if !j.Enabled {
			continue
		}
		if err := RunExport(ctx, w, j, now); err != nil {
			w.log().Warn("scheduled export failed", "export", j.ID, "err", err)
		}
	}
}

// RunExport exécute un export vers le stockage S3 du client.
func RunExport(ctx context.Context, w *Worker, j model.ExportJob, now time.Time) error {
	today := tsdb.TruncDay(now)
	from, to := today.AddDate(0, 0, -1), today
	if j.Schedule == "monthly" {
		if now.Day() != 1 && j.LastRunAt != nil {
			return nil
		}
		to = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		from = to.AddDate(0, -1, 0)
	}
	if j.LastRunAt != nil && !j.LastRunAt.Before(to) {
		return nil
	}
	lines, err := w.TSDB.CostLines(ctx, tsdb.CostQuery{From: from, To: to, Granularity: tsdb.GranDay, Limit: 5_000_000})
	if err != nil {
		return err
	}
	data, ctype, err := export.EncodeCostLines(lines, j.Format)
	if err != nil {
		return err
	}
	sec, err := w.Keyring.DecryptMap(ctx, j.SecretsEnc, secrets.AAD(j.OrgID, "export", j.ID))
	if err != nil {
		return err
	}
	dest, err := objstore.NewS3(j.Destination["endpoint"], sec["access_key"], sec["secret_key"], j.Destination["bucket"],
		j.Destination["region"], j.Destination["insecure"] != "true")
	if err != nil {
		return err
	}
	key := strings.Trim(j.Destination["prefix"], "/")
	if key != "" {
		key += "/"
	}
	key += fmt.Sprintf("kairn-costs-%s-%s.%s", from.Format("20060102"), to.Format("20060102"), j.Format)
	if err := dest.Put(ctx, key, data, ctype); err != nil {
		return err
	}
	j.LastRunAt = &now
	return w.Store.Exports().Update(ctx, &j)
}
