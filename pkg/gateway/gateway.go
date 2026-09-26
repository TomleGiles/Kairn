// Package gateway est la passerelle d'ingestion (service ingest, mode gateway) :
//   - webhooks entrants de déploiement et d'incident (GitLab, GitHub, Argo CD,
//     Flux, Alertmanager, PagerDuty, Opsgenie) : /ingest/v1/webhooks/{token}
//   - métriques OTLP/HTTP de l'agent Kairn : /ingest/v1/otlp/v1/metrics
//   - inventaire de l'agent : /ingest/v1/agent/inventory
//   - sondes d'uptime multi-régions : /ingest/v1/probes/*
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kairn-io/kairn/pkg/auth"
	"github.com/kairn-io/kairn/pkg/bus"
	"github.com/kairn-io/kairn/pkg/connector"
	"github.com/kairn-io/kairn/pkg/ids"
	"github.com/kairn-io/kairn/pkg/ingest"
	"github.com/kairn-io/kairn/pkg/inventory"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/notify"
	"github.com/kairn-io/kairn/pkg/secrets"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/tenancy"
	"github.com/kairn-io/kairn/pkg/tsdb"
)

// MaxBody borne la taille des requêtes entrantes.
const MaxBody = 10 << 20

// Gateway reçoit les données poussées vers Kairn.
type Gateway struct {
	Store        store.Store
	TSDB         tsdb.TSDB
	Bus          bus.Bus
	Keyring      *secrets.Keyring
	Log          *slog.Logger
	ServiceToken string
	Now          func() time.Time

	mu sync.Mutex
}

func (g *Gateway) now() time.Time {
	if g.Now != nil {
		return g.Now().UTC()
	}
	return time.Now().UTC()
}

func (g *Gateway) log() *slog.Logger {
	if g.Log != nil {
		return g.Log
	}
	return slog.Default()
}

// Mount enregistre les routes sur un routeur.
func (g *Gateway) Mount(r chi.Router) {
	r.Route("/ingest/v1", func(r chi.Router) {
		r.Post("/webhooks/{token}", g.webhook)
		r.Post("/otlp/v1/metrics", g.otlpMetrics)
		r.Post("/agent/inventory", g.agentInventory)
		r.Get("/probes/checks", g.probeChecks)
		r.Post("/probes/results", g.probeResults)
	})
}

// Handler renvoie un routeur autonome (service ingest en mode gateway).
func (g *Gateway) Handler() http.Handler {
	r := chi.NewRouter()
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	g.Mount(r)
	return r
}

func problem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"title": http.StatusText(status), "status": status, "detail": detail})
}

func headersOf(r *http.Request) map[string]string {
	h := map[string]string{}
	for k, v := range r.Header {
		if len(v) > 0 {
			h[strings.ToLower(k)] = v[0]
		}
	}
	return h
}

// connectorFor résout le connecteur associé à un jeton de webhook ou d'agent.
func (g *Gateway) connectorFor(ctx context.Context, token string) (context.Context, model.Connector, error) {
	if token == "" {
		return ctx, model.Connector{}, store.ErrNotFound
	}
	orgID, connID, _, err := g.Store.System().FindConnectorByWebhook(ctx, token)
	if err != nil {
		return ctx, model.Connector{}, err
	}
	octx := tenancy.WithOrg(ctx, orgID)
	c, err := g.Store.Connectors().Get(octx, connID)
	return octx, c, err
}

func (g *Gateway) webhook(w http.ResponseWriter, r *http.Request) {
	ctx, c, err := g.connectorFor(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		problem(w, http.StatusNotFound, "unknown webhook")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBody))
	if err != nil {
		problem(w, http.StatusRequestEntityTooLarge, "payload too large")
		return
	}
	cfg, err := ingest.Config(ctx, g.Keyring, c)
	if err != nil {
		problem(w, http.StatusInternalServerError, "connector unavailable")
		return
	}
	inst, err := connector.New(c.Type, cfg)
	if err != nil {
		problem(w, http.StatusInternalServerError, "connector unavailable")
		return
	}
	parser, ok := inst.(connector.WebhookParser)
	if !ok {
		problem(w, http.StatusBadRequest, "this connector does not accept webhooks")
		return
	}
	hdrs := headersOf(r)
	if err := parser.VerifyWebhook(hdrs, body, cfg.Secret("webhook_secret")); err != nil {
		problem(w, http.StatusUnauthorized, "invalid signature")
		return
	}
	evs, err := parser.ParseWebhook(hdrs, body)
	if err != nil {
		problem(w, http.StatusBadRequest, "unrecognized payload")
		return
	}
	var out []model.Event
	for _, e := range evs {
		me := ingest.ToModelEvent(c, e)
		if id := g.resolveResource(ctx, e); id != "" {
			me.ResourceID = id
		}
		out = append(out, me)
	}
	if len(out) > 0 {
		if err := g.TSDB.WriteEvents(ctx, out); err != nil {
			problem(w, http.StatusInternalServerError, "storage error")
			return
		}
	}
	for _, e := range out {
		if e.Kind == model.EventIncident {
			g.incidentFromEvent(ctx, c, e)
		}
	}
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]int{"accepted": len(out)})
}

// resolveResource rattache un événement à une ressource d'inventaire existante
// (tout connecteur de l'organisation) : par identifiant externe, sinon par nom
// de service (workload Kubernetes dans le namespace indiqué, puis instance).
// Un rattachement ambigu est écarté : mieux vaut aucun lien qu'un lien faux.
func (g *Gateway) resolveResource(ctx context.Context, e connector.Event) string {
	if e.ResourceExternalID != "" {
		rs, err := g.Store.Resources().Current(ctx, store.ResourceFilter{Query: e.ResourceExternalID, Limit: 20})
		if err != nil {
			return ""
		}
		for _, r := range rs {
			if r.ExternalID == e.ResourceExternalID || r.Name == e.ResourceExternalID {
				if e.ResourceType == "" || r.Type == e.ResourceType {
					return r.ID
				}
			}
		}
		return ""
	}
	name, _ := e.Payload["service"].(string)
	if name == "" {
		return ""
	}
	ns, _ := e.Payload["namespace"].(string)
	for _, typ := range []string{model.TypeK8sWorkload, model.TypeInstance} {
		rs, err := g.Store.Resources().Current(ctx, store.ResourceFilter{Types: []string{typ}, Query: name, Limit: 50})
		if err != nil {
			return ""
		}
		var match []string
		for _, r := range rs {
			if !strings.EqualFold(r.Name, name) || (ns != "" && typ == model.TypeK8sWorkload && r.Attr("k8s.namespace") != ns) {
				continue
			}
			match = append(match, r.ID)
		}
		switch {
		case len(match) == 1:
			return match[0]
		case len(match) > 1:
			return ""
		}
	}
	return ""
}

// incidentFromEvent ouvre ou résout un incident depuis un événement d'outil d'astreinte.
func (g *Gateway) incidentFromEvent(ctx context.Context, c model.Connector, e model.Event) {
	status, _ := e.Payload["status"].(string)
	key, _ := e.Payload["incident_key"].(string)
	if key == "" {
		return
	}
	incs, err := g.Store.Incidents().List(ctx, store.ListQuery{Limit: 200, Filters: map[string]string{"status": "open"}})
	if err != nil {
		return
	}
	var cur *model.Incident
	for i := range incs {
		if incs[i].Source == c.Type+":"+key {
			cur = &incs[i]
		}
	}
	now := g.now()
	switch {
	case status == "resolved" && cur != nil:
		cur.Status, cur.ResolvedAt = "resolved", &now
		cur.Updates = append(cur.Updates, model.IncidentUpdate{At: now, Status: "resolved", Message: e.Title})
		_ = g.Store.Incidents().Update(ctx, cur)
	case status != "resolved" && cur == nil:
		inc := model.Incident{Title: e.Title, Status: "open", Source: c.Type + ":" + key, StartedAt: e.TS,
			Updates: []model.IncidentUpdate{{At: now, Status: "open", Message: e.Title}}}
		_ = g.Store.Incidents().Create(ctx, &inc)
	}
}

// ------------------------------------------------------------------ sondes d'uptime

// ProbeCheck est une sonde à exécuter par une région.
type ProbeCheck struct {
	OrgID string            `json:"org_id"`
	Check model.UptimeCheck `json:"check"`
}

func (g *Gateway) serviceOnly(r *http.Request) bool {
	return auth.ServiceTokenValid(g.ServiceToken, r.Header.Get("X-Kairn-Service-Token"))
}

func (g *Gateway) probeChecks(w http.ResponseWriter, r *http.Request) {
	if !g.serviceOnly(r) {
		problem(w, http.StatusUnauthorized, "service token required")
		return
	}
	region := r.URL.Query().Get("region")
	ctx := r.Context()
	orgs, err := g.Store.System().ListOrgIDs(tenancy.WithSystem(ctx))
	if err != nil {
		problem(w, http.StatusInternalServerError, "storage error")
		return
	}
	out := []ProbeCheck{}
	for _, org := range orgs {
		octx := tenancy.WithOrg(ctx, org)
		checks, err := store.ListAll(octx, g.Store.UptimeChecks(), func(c model.UptimeCheck) string { return c.ID }, nil)
		if err != nil {
			continue
		}
		for _, c := range checks {
			if !c.Enabled {
				continue
			}
			for _, reg := range c.Regions {
				if reg == region || region == "" {
					out = append(out, ProbeCheck{OrgID: org, Check: c})
					break
				}
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (g *Gateway) probeResults(w http.ResponseWriter, r *http.Request) {
	if !g.serviceOnly(r) {
		problem(w, http.StatusUnauthorized, "service token required")
		return
	}
	var results []model.UptimeResult
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBody)).Decode(&results); err != nil {
		problem(w, http.StatusBadRequest, "invalid json")
		return
	}
	byOrg := map[string][]model.UptimeResult{}
	for _, res := range results {
		if ids.Valid(res.OrgID) && ids.Valid(res.CheckID) {
			byOrg[res.OrgID] = append(byOrg[res.OrgID], res)
		}
	}
	for org, rs := range byOrg {
		ctx := tenancy.WithOrg(r.Context(), org)
		if err := g.TSDB.WriteUptime(ctx, rs); err != nil {
			problem(w, http.StatusInternalServerError, "storage error")
			return
		}
		seen := map[string]bool{}
		for _, res := range rs {
			if !seen[res.CheckID] {
				seen[res.CheckID] = true
				if err := g.EvaluateCheck(ctx, res.CheckID); err != nil {
					g.log().Warn("uptime evaluation failed", "check", res.CheckID, "err", err)
				}
			}
		}
	}
	w.WriteHeader(http.StatusAccepted)
}

// EvaluateCheck ouvre ou résout l'incident d'une sonde selon le dernier état de chaque région.
func (g *Gateway) EvaluateCheck(ctx context.Context, checkID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	c, err := g.Store.UptimeChecks().Get(ctx, checkID)
	if err != nil {
		return err
	}
	now := g.now()
	rs, err := g.TSDB.QueryUptime(ctx, tsdb.UptimeQuery{CheckIDs: []string{checkID}, From: now.Add(-3 * time.Duration(max(c.IntervalSeconds, 60)) * time.Second), To: now.Add(time.Minute)})
	if err != nil {
		return err
	}
	latest := map[string]model.UptimeResult{}
	for _, r := range rs {
		if cur, ok := latest[r.Region]; !ok || r.TS.After(cur.TS) {
			latest[r.Region] = r
		}
	}
	down := 0
	var lastErr string
	for _, r := range latest {
		if !r.Up {
			down++
			lastErr = r.Error
		}
	}
	threshold := c.FailThreshold
	if threshold <= 0 {
		threshold = 1
	}
	orgID, _ := tenancy.OrgID(ctx)
	inc, err := g.Store.Incidents().OpenForCheck(ctx, checkID)
	hasOpen := err == nil
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	fp := "uptime:" + checkID
	switch {
	case down >= threshold && !hasOpen:
		id := checkID
		inc = model.Incident{CheckID: &id, Title: c.Name + " indisponible", Status: "open", Source: "uptime", StartedAt: now,
			Updates: []model.IncidentUpdate{{At: now, Status: "open", Message: fmt.Sprintf("Échec depuis %d région(s) : %s", down, lastErr)}}}
		if err := g.Store.Incidents().Create(ctx, &inc); err != nil {
			return err
		}
		_ = g.TSDB.WriteEvents(ctx, []model.Event{{TS: now, Kind: model.EventIncident, Source: "uptime", Title: inc.Title,
			Payload: map[string]any{"check_id": checkID, "status": "open"}}})
		if g.Bus != nil {
			_ = g.Bus.Publish(ctx, bus.SubjectAlert, orgID, notify.AlertRequest{Kind: model.AlertUptime, Severity: "critical", Fingerprint: fp,
				Title: inc.Title, Body: fmt.Sprintf("%s (%s) échoue depuis %d région(s) : %s", c.Name, c.Target, down, lastErr),
				Link: "/uptime", Payload: map[string]any{"check_id": checkID, "target": c.Target}})
		}
	case down < threshold && hasOpen && inc.Source == "uptime":
		inc.Status, inc.ResolvedAt = "resolved", &now
		inc.Updates = append(inc.Updates, model.IncidentUpdate{At: now, Status: "resolved", Message: "Service rétabli."})
		if err := g.Store.Incidents().Update(ctx, &inc); err != nil {
			return err
		}
		_ = g.TSDB.WriteEvents(ctx, []model.Event{{TS: now, Kind: model.EventIncident, Source: "uptime", Title: c.Name + " rétabli",
			Payload: map[string]any{"check_id": checkID, "status": "resolved"}}})
		if g.Bus != nil {
			_ = g.Bus.Publish(ctx, bus.SubjectAlert, orgID, notify.AlertRequest{Kind: model.AlertUptime, Fingerprint: fp, Title: c.Name + " rétabli", Resolved: true})
		}
	}
	return nil
}

// ------------------------------------------------------------------ agent Kairn

// AgentHost est l'inventaire poussé par l'agent.
type AgentHost struct {
	HostID   string            `json:"host_id"`
	Hostname string            `json:"hostname"`
	OS       string            `json:"os"`
	Arch     string            `json:"arch"`
	VCPUs    int               `json:"vcpus"`
	RAMGB    float64           `json:"ram_gb"`
	DiskGB   float64           `json:"disk_gb"`
	Labels   map[string]string `json:"labels"`
	Version  string            `json:"agent_version"`
}

func (g *Gateway) agentConnector(r *http.Request) (context.Context, model.Connector, error) {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return g.connectorFor(r.Context(), tok)
}

func (g *Gateway) agentInventory(w http.ResponseWriter, r *http.Request) {
	ctx, c, err := g.agentConnector(r)
	if err != nil || c.Type != "agent" {
		problem(w, http.StatusUnauthorized, "invalid agent token")
		return
	}
	var h AgentHost
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&h); err != nil || h.HostID == "" {
		problem(w, http.StatusBadRequest, "invalid host payload")
		return
	}
	res := connector.Resource{Type: model.TypeHost, ExternalID: h.HostID, Name: h.Hostname,
		Attributes: map[string]any{"vcpus": h.VCPUs, "ram_gb": h.RAMGB, "disk_gb": h.DiskGB, "os": h.OS, "arch": h.Arch, "agent_version": h.Version},
		Labels:     h.Labels}
	if _, err := inventory.Apply(ctx, g.Store, inventory.Snapshot{ConnectorID: c.ID, Provider: "onprem", ObservedAt: g.now(),
		Resources: []connector.Resource{res}, Complete: false}); err != nil {
		problem(w, http.StatusInternalServerError, "storage error")
		return
	}
	_ = g.Store.Connectors().UpdateStatus(ctx, c.ID, model.ConnectorOK, "agent "+h.Hostname+" actif", g.now(), true)
	w.WriteHeader(http.StatusAccepted)
}
