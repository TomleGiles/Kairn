// Package ingest exécute les synchronisations de connecteurs (M-01) :
// inventaire, métriques, facturation et événements, de manière incrémentale,
// idempotente et rejouable (backfill jusqu'à 13 mois).
package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kairn-io/kairn/pkg/connector"
	"github.com/kairn-io/kairn/pkg/ids"
	"github.com/kairn-io/kairn/pkg/inventory"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/secrets"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/tenancy"
	"github.com/kairn-io/kairn/pkg/tsdb"
)

// MaxBackfill est la profondeur maximale de backfill (13 mois).
const MaxBackfill = 396 * 24 * time.Hour

const metricBatch = 10_000

// Syncer orchestre les synchronisations.
type Syncer struct {
	Store   store.Store
	TSDB    tsdb.TSDB
	Keyring *secrets.Keyring
	Log     *slog.Logger
	Now     func() time.Time
	// OnSynced est appelé après une synchronisation réussie (publication sur le bus).
	OnSynced func(ctx context.Context, c model.Connector, r Report)
}

// Options choisit les volets à synchroniser.
type Options struct {
	Inventory bool
	Metrics   bool
	Billing   bool
	Events    bool
	// MetricsFrom force le début de la fenêtre de métriques (sinon : depuis le dernier succès).
	MetricsFrom time.Time
	// BillingFrom force le début de la période de facturation (sinon : mois courant et précédent).
	BillingFrom time.Time
}

// All synchronise tous les volets.
var All = Options{Inventory: true, Metrics: true, Billing: true, Events: true}

// Report résume une synchronisation.
type Report struct {
	Inventory inventory.Result `json:"inventory"`
	Metrics   int              `json:"metrics"`
	Billing   int              `json:"billing"`
	Events    int              `json:"events"`
	Errors    []string         `json:"errors,omitempty"`
}

func (s *Syncer) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *Syncer) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// SecretsAAD lie les secrets d'un connecteur à son organisation.
func SecretsAAD(orgID, connectorID string) []byte {
	return secrets.AAD(orgID, "connector", connectorID)
}

// Config déchiffre la configuration d'un connecteur.
func Config(ctx context.Context, kr *secrets.Keyring, c model.Connector) (connector.Config, error) {
	cfg := connector.Config{OrgID: c.OrgID, ConnectorID: c.ID, Settings: c.Settings, Secrets: map[string]string{}}
	if len(c.SecretsEnc) > 0 {
		if kr == nil {
			return cfg, errors.New("ingest: connector has secrets but no keyring configured")
		}
		sec, err := kr.DecryptMap(ctx, c.SecretsEnc, SecretsAAD(c.OrgID, c.ID))
		if err != nil {
			return cfg, fmt.Errorf("decrypt connector secrets: %w", err)
		}
		cfg.Secrets = sec
	}
	return cfg, nil
}

// TargetSetting désigne le connecteur dont les ressources reçoivent les
// métriques, lignes de facture et événements produits par un connecteur
// (ex. Prometheus → ressources du connecteur Kubernetes du même cluster).
const TargetSetting = "target_connector_id"

// inheritedSettings sont recopiés du connecteur cible s'ils ne sont pas définis.
var inheritedSettings = []string{"cluster_name"}

// OwnerID renvoie l'identifiant du connecteur propriétaire des ressources
// référencées par c. L'identifiant de ressource dérive aussi de l'organisation :
// une cible appartenant à une autre organisation ne peut rien désigner.
func OwnerID(c model.Connector) string {
	if t := c.Settings[TargetSetting]; ids.Valid(t) {
		return t
	}
	return c.ID
}

// Instantiate construit l'instance de connecteur.
func (s *Syncer) Instantiate(ctx context.Context, c model.Connector) (connector.Connector, error) {
	cfg, err := Config(ctx, s.Keyring, c)
	if err != nil {
		return nil, err
	}
	if owner := OwnerID(c); owner != c.ID {
		if target, err := s.Store.Connectors().Get(ctx, owner); err == nil {
			settings := make(map[string]string, len(cfg.Settings)+len(inheritedSettings))
			for k, v := range cfg.Settings {
				settings[k] = v
			}
			for _, k := range inheritedSettings {
				if settings[k] == "" && target.Settings[k] != "" {
					settings[k] = target.Settings[k]
				}
			}
			cfg.Settings = settings
		}
	}
	return connector.New(c.Type, cfg)
}

func providerOf(c model.Connector) string {
	if info, ok := connector.Info(c.Type); ok && info.Provider != "" {
		return info.Provider
	}
	return c.Type
}

// Sync exécute une synchronisation incrémentale d'un connecteur.
func (s *Syncer) Sync(ctx context.Context, c model.Connector, opt Options) (Report, error) {
	if err := tenancy.Check(ctx, c.OrgID); err != nil {
		return Report{}, err
	}
	conn, err := s.Instantiate(ctx, c)
	if err != nil {
		s.markStatus(ctx, c, model.ConnectorError, err.Error(), false)
		return Report{}, err
	}
	now := s.now()
	var rep Report
	if po, ok := conn.(connector.PushOnly); ok && po.PushOnly() {
		// Données poussées (agent, webhooks) : seule la santé est rafraîchie.
		h := conn.Health(ctx)
		st := model.ConnectorOK
		if h.Status != connector.HealthOK {
			st = model.ConnectorDegraded
		}
		s.markStatus(ctx, c, st, h.Message, h.Status == connector.HealthOK)
		return rep, nil
	}
	record := func(kind string, fn func() (int, error)) {
		run := model.ConnectorRun{OrgID: c.OrgID, ConnectorID: c.ID, Kind: kind, Status: "running", StartedAt: s.now()}
		_ = s.Store.Runs().Create(ctx, &run)
		n, err := fn()
		fin := s.now()
		run.FinishedAt, run.Items, run.Status = &fin, n, "ok"
		if err != nil {
			run.Status, run.Error = "error", truncate(err.Error(), 500)
			rep.Errors = append(rep.Errors, kind+": "+err.Error())
			s.log().Warn("connector sync failed", "org", c.OrgID, "connector", c.ID, "type", c.Type, "kind", kind, "err", err)
		}
		_ = s.Store.Runs().Finish(ctx, &run)
	}
	if opt.Inventory {
		record("inventory", func() (int, error) {
			res, err := s.syncInventory(ctx, c, conn, now)
			rep.Inventory = res
			return res.Observed, err
		})
	}
	if opt.Metrics {
		from := opt.MetricsFrom
		if from.IsZero() {
			from = now.Add(-24 * time.Hour)
			if c.LastSuccessAt != nil {
				from = c.LastSuccessAt.Add(-15 * time.Minute) // recouvrement : l'écriture est idempotente
			}
		}
		record("metrics", func() (int, error) {
			n, err := s.syncMetrics(ctx, c, conn, connector.TimeWindow{From: from, To: now})
			rep.Metrics = n
			return n, err
		})
	}
	if info, ok := connector.Info(c.Type); opt.Billing && ok && info.Billing {
		from := opt.BillingFrom
		if from.IsZero() {
			m := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
			from = m.AddDate(0, -1, 0)
		}
		record("billing", func() (int, error) {
			n, err := s.syncBilling(ctx, c, conn, connector.Period{From: from, To: tsdb.TruncDay(now).AddDate(0, 0, 1)})
			rep.Billing = n
			return n, err
		})
	}
	if es, ok := conn.(connector.EventSource); opt.Events && ok {
		since := now.Add(-24 * time.Hour)
		if c.LastSuccessAt != nil {
			since = c.LastSuccessAt.Add(-15 * time.Minute)
		}
		record("events", func() (int, error) {
			n, err := s.syncEvents(ctx, c, es, since, now)
			rep.Events = n
			return n, err
		})
	}
	health := conn.Health(ctx)
	status, msg, ok := model.ConnectorOK, health.Message, len(rep.Errors) == 0
	switch {
	case !ok:
		status, msg = model.ConnectorDegraded, truncate(fmt.Sprint(rep.Errors), 500)
	case health.Status == connector.HealthDegraded:
		status = model.ConnectorDegraded
	case health.Status == connector.HealthDown:
		status = model.ConnectorError
	}
	s.markStatus(ctx, c, status, msg, ok)
	if ok && s.OnSynced != nil {
		s.OnSynced(ctx, c, rep)
	}
	if !ok {
		return rep, fmt.Errorf("sync %s: %v", c.ID, rep.Errors)
	}
	return rep, nil
}

func (s *Syncer) markStatus(ctx context.Context, c model.Connector, st model.ConnectorStatus, msg string, success bool) {
	if err := s.Store.Connectors().UpdateStatus(ctx, c.ID, st, msg, s.now(), success); err != nil {
		s.log().Warn("update connector status", "connector", c.ID, "err", err)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func (s *Syncer) syncInventory(ctx context.Context, c model.Connector, conn connector.Connector, at time.Time) (inventory.Result, error) {
	sctx, sink := connector.WithErrorSink(ctx)
	ch, err := conn.SyncInventory(sctx, at.Add(-24*time.Hour))
	if errors.Is(err, connector.ErrNotSupported) {
		return inventory.Result{}, nil // connecteur de métriques ou d'événements : aucune ressource propre
	}
	if err != nil {
		return inventory.Result{}, err
	}
	return s.applyInventory(ctx, c, ch, sink, at)
}

func (s *Syncer) applyInventory(ctx context.Context, c model.Connector, ch <-chan connector.Resource, sink *connector.ErrorSink, at time.Time) (inventory.Result, error) {
	items := connector.Collect(ch)
	streamErr := sink.Err()
	res, err := inventory.Apply(ctx, s.Store, inventory.Snapshot{
		ConnectorID: c.ID, Provider: providerOf(c), ObservedAt: at, Resources: items, Complete: streamErr == nil,
	})
	if err != nil {
		return res, err
	}
	if len(res.Events) > 0 {
		if err := s.TSDB.WriteEvents(ctx, res.Events); err != nil {
			return res, fmt.Errorf("write inventory events: %w", err)
		}
	}
	if _, err := inventory.Link(ctx, s.Store, at); err != nil {
		return res, fmt.Errorf("link: %w", err)
	}
	return res, streamErr
}

func (s *Syncer) syncMetrics(ctx context.Context, c model.Connector, conn connector.Connector, w connector.TimeWindow) (int, error) {
	sctx, sink := connector.WithErrorSink(ctx)
	ch, err := conn.SyncMetrics(sctx, w)
	if errors.Is(err, connector.ErrNotSupported) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n := 0
	owner := OwnerID(c)
	batch := make([]model.MetricPoint, 0, metricBatch)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := s.TSDB.WriteMetrics(ctx, batch); err != nil {
			return fmt.Errorf("write metrics: %w", err)
		}
		n += len(batch)
		batch = batch[:0]
		return nil
	}
	for p := range ch {
		batch = append(batch, model.MetricPoint{
			OrgID: c.OrgID, ResourceID: ids.Resource(c.OrgID, owner, p.ResourceType, p.ResourceExternalID),
			Metric: p.Metric, TS: p.TS.UTC(), Value: p.Value,
		})
		if len(batch) >= metricBatch {
			if err := flush(); err != nil {
				for range ch {
				}
				return n, err
			}
		}
	}
	if err := flush(); err != nil {
		return n, err
	}
	// Agrégats 5 min / 1 h des heures couvertes, aussitôt : lors d'un backfill,
	// les points bruts de plus de 15 jours expirent (TTL) et seuls les agrégats
	// conservent l'historique (ADR-0005). Le rollup est idempotent.
	if n > 0 && !w.From.IsZero() && w.To.After(w.From) {
		if err := s.TSDB.Rollup(ctx, w.From, w.To); err != nil {
			return n, fmt.Errorf("rollup metrics: %w", err)
		}
	}
	return n, sink.Err()
}

func (s *Syncer) syncBilling(ctx context.Context, c model.Connector, conn connector.Connector, p connector.Period) (int, error) {
	sctx, sink := connector.WithErrorSink(ctx)
	ch, err := conn.SyncBilling(sctx, p)
	if errors.Is(err, connector.ErrNotSupported) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	provider := providerOf(c)
	var lines []model.BillingLine
	for l := range ch {
		res := ""
		if l.ResourceExternalID != "" {
			res = ids.Resource(c.OrgID, OwnerID(c), l.ResourceType, l.ResourceExternalID)
		}
		lines = append(lines, model.BillingLine{
			OrgID: c.OrgID, ConnectorID: c.ID, Provider: provider, Day: tsdb.TruncDay(l.Day), ResourceID: res,
			Service: l.Service, SKU: l.SKU, CostType: l.CostType, Quantity: l.Quantity, Unit: l.Unit,
			Amount: l.Amount, Currency: l.Currency, InvoiceID: l.InvoiceID,
		})
	}
	if err := sink.Err(); err != nil {
		// Période incomplète : on ne remplace pas les lignes existantes.
		return 0, err
	}
	if err := s.TSDB.ReplaceBillingLines(ctx, c.ID, p.From, p.To, lines); err != nil {
		return 0, fmt.Errorf("write billing: %w", err)
	}
	return len(lines), nil
}

func (s *Syncer) syncEvents(ctx context.Context, c model.Connector, es connector.EventSource, since, until time.Time) (int, error) {
	sctx, sink := connector.WithErrorSink(ctx)
	ch, err := es.SyncEvents(sctx, since)
	if err != nil {
		return 0, err
	}
	var evs []model.Event
	for e := range ch {
		if e.TS.After(until) {
			continue
		}
		evs = append(evs, ToModelEvent(c, e))
	}
	if len(evs) > 0 {
		if err := s.TSDB.WriteEvents(ctx, evs); err != nil {
			return 0, fmt.Errorf("write events: %w", err)
		}
	}
	return len(evs), sink.Err()
}

// ToModelEvent convertit un événement de connecteur.
func ToModelEvent(c model.Connector, e connector.Event) model.Event {
	res := ""
	if e.ResourceExternalID != "" && e.ResourceType != "" {
		res = ids.Resource(c.OrgID, OwnerID(c), e.ResourceType, e.ResourceExternalID)
	}
	payload := e.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	return model.Event{OrgID: c.OrgID, TS: e.TS.UTC(), Kind: e.Kind, Source: c.Type, ResourceID: res, Title: e.Title, Payload: payload}
}

// Backfill rejoue l'historique d'un connecteur depuis from : inventaire (si
// le connecteur sait le restituer), métriques jour par jour, facturation et
// événements. Idempotent : peut être relancé sans doublon.
func (s *Syncer) Backfill(ctx context.Context, c model.Connector, from time.Time) (Report, error) {
	if err := tenancy.Check(ctx, c.OrgID); err != nil {
		return Report{}, err
	}
	now := s.now()
	if now.Sub(from) > MaxBackfill {
		from = now.Add(-MaxBackfill)
	}
	conn, err := s.Instantiate(ctx, c)
	if err != nil {
		return Report{}, err
	}
	var rep Report
	if po, ok := conn.(connector.PushOnly); ok && po.PushOnly() {
		return rep, nil
	}
	if hist, ok := conn.(connector.HistoricalInventory); ok {
		step := hist.BackfillStep()
		if step <= 0 {
			step = 24 * time.Hour
		}
		for at := from.Truncate(step); at.Before(now); at = at.Add(step) {
			sctx, sink := connector.WithErrorSink(ctx)
			ch, err := hist.SyncInventoryAt(sctx, at)
			if err != nil {
				return rep, err
			}
			res, err := s.applyInventory(ctx, c, ch, sink, at)
			if err != nil {
				return rep, fmt.Errorf("backfill inventory at %s: %w", at, err)
			}
			rep.Inventory.Created += res.Created
			rep.Inventory.Updated += res.Updated
			rep.Inventory.Deleted += res.Deleted
			rep.Inventory.Observed = res.Observed
		}
	}
	res, err := s.syncInventory(ctx, c, conn, now)
	if err != nil {
		return rep, err
	}
	rep.Inventory.Created += res.Created
	rep.Inventory.Updated += res.Updated
	rep.Inventory.Deleted += res.Deleted
	rep.Inventory.Observed = res.Observed
	for day := tsdb.TruncDay(from); day.Before(now); day = day.AddDate(0, 0, 1) {
		end := day.AddDate(0, 0, 1)
		if end.After(now) {
			end = now
		}
		n, err := s.syncMetrics(ctx, c, conn, connector.TimeWindow{From: day, To: end})
		rep.Metrics += n
		if err != nil {
			return rep, fmt.Errorf("backfill metrics %s: %w", day.Format("2006-01-02"), err)
		}
	}
	if info, ok := connector.Info(c.Type); ok && info.Billing {
		n, err := s.syncBilling(ctx, c, conn, connector.Period{From: tsdb.TruncDay(from), To: tsdb.TruncDay(now).AddDate(0, 0, 1)})
		rep.Billing = n
		if err != nil {
			return rep, err
		}
	}
	if es, ok := conn.(connector.EventSource); ok {
		n, err := s.syncEvents(ctx, c, es, from, now)
		rep.Events = n
		if err != nil {
			return rep, err
		}
	}
	s.markStatus(ctx, c, model.ConnectorOK, "backfill terminé", true)
	return rep, nil
}
