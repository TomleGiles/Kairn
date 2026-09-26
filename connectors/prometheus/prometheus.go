// Package prometheus est le connecteur Prometheus (M-01, M-05) : historique
// d'utilisation via l'API query_range (Prometheus, VictoriaMetrics, Thanos,
// Mimir). Il ne crée aucune ressource : ses points sont rattachés aux
// ressources du connecteur cible (target_connector_id), Kubernetes pour le
// mode « kubernetes », OpenStack ou agent pour le mode « node ».
package prometheus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kairn-io/kairn/connectors/internal/rest"
	"github.com/kairn-io/kairn/pkg/connector"
	"github.com/kairn-io/kairn/pkg/model"
)

// Type est l'identifiant du connecteur.
const Type = "prometheus"

var permissions = []connector.Permission{
	{Scope: "GET /api/v1/query_range, /api/v1/query", Description: "Lecture des séries (jeton en lecture seule ou accès réseau restreint)."},
	{Scope: "cAdvisor + kube-state-metrics", Description: "Mode kubernetes : container_cpu_usage_seconds_total, container_memory_working_set_bytes, kube_pod_container_resource_requests, kube_node_status_capacity."},
	{Scope: "node_exporter", Description: "Mode node : node_cpu_seconds_total, node_memory_*, node_network_* avec un label identifiant la VM.", Optional: true},
}

func init() {
	connector.Register(connector.TypeInfo{
		Type: Type, DisplayName: "Prometheus / VictoriaMetrics / Thanos", Category: connector.CategoryMetrics, Provider: "prometheus",
		Resources: []string{}, DefaultInterval: 15 * time.Minute, Metrics: true, DocsURL: "/docs/connectors/prometheus", Permissions: permissions,
		Fields: []connector.Field{
			{Name: "url", Label: "URL de l'API Prometheus", Required: true, Help: "ex. https://prometheus.example/ ou …/select/0/prometheus (VictoriaMetrics)"},
			{Name: "target_connector_id", Label: "Connecteur des ressources", Required: true, Help: "Connecteur Kubernetes (mode kubernetes) ou OpenStack/agent (mode node)"},
			{Name: "mode", Label: "Mode", Default: "kubernetes", Help: "kubernetes (cAdvisor + kube-state-metrics) ou node (node_exporter)"},
			{Name: "matchers", Label: "Filtre de séries", Help: `ex. cluster="prod-gra" pour un Prometheus multi-clusters`},
			{Name: "vm_id_label", Label: "Label de l'identifiant de VM (mode node)", Default: "instance_id"},
			{Name: "step", Label: "Pas d'échantillonnage", Default: "5m"},
			{Name: "token", Label: "Jeton (Bearer)", Secret: true},
			{Name: "username", Label: "Utilisateur (authentification basique)"},
			{Name: "password", Label: "Mot de passe", Secret: true},
			{Name: "tenant_id", Label: "Tenant (X-Scope-OrgID, Mimir/Cortex)"},
			{Name: "ca_cert", Label: "Certificat d'autorité (PEM)"},
		},
	}, New)
}

// Conn est une instance du connecteur.
type Conn struct {
	base     string
	mode     string
	cluster  string
	matchers string
	idLabel  string
	step     time.Duration
	cl       *rest.Client
	now      func() time.Time
}

// New construit le connecteur.
func New(cfg connector.Config) (connector.Connector, error) {
	base := strings.TrimRight(cfg.Setting("url", ""), "/")
	if base == "" {
		return nil, fmt.Errorf("%w: url", connector.ErrMissingConfig)
	}
	mode := cfg.Setting("mode", "kubernetes")
	if mode != "kubernetes" && mode != "node" {
		return nil, fmt.Errorf("unknown mode %q (kubernetes or node)", mode)
	}
	c := &Conn{base: base, mode: mode, cluster: cfg.Setting("cluster_name", ""), idLabel: cfg.Setting("vm_id_label", "instance_id"),
		now: func() time.Time { return time.Now().UTC() }}
	if mode == "kubernetes" && c.cluster == "" {
		return nil, fmt.Errorf("%w: cluster_name (inherited from the target Kubernetes connector)", connector.ErrMissingConfig)
	}
	if !validLabel(c.idLabel) {
		return nil, fmt.Errorf("invalid vm_id_label %q", c.idLabel)
	}
	m, err := parseMatchers(cfg.Setting("matchers", ""))
	if err != nil {
		return nil, err
	}
	c.matchers = m
	step, err := time.ParseDuration(cfg.Setting("step", "5m"))
	if err != nil || step < 15*time.Second {
		return nil, fmt.Errorf("invalid step (minimum 15s)")
	}
	c.step = step
	hc, err := rest.NewHTTP(rest.Options{CAPEM: cfg.Setting("ca_cert", "")})
	if err != nil {
		return nil, err
	}
	headers := map[string]string{}
	if t := cfg.Setting("tenant_id", ""); t != "" {
		headers["X-Scope-OrgID"] = t
	}
	token, user, pass := cfg.Secret("token"), cfg.Setting("username", ""), cfg.Secret("password")
	c.cl = &rest.Client{HTTP: hc, Headers: headers, Auth: func(r *http.Request) {
		switch {
		case token != "":
			r.Header.Set("Authorization", "Bearer "+token)
		case user != "":
			r.SetBasicAuth(user, pass)
		}
	}}
	return c, nil
}

func validLabel(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if !(r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (i > 0 && r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

// parseMatchers valide une liste de sélecteurs label="valeur" (aucune injection PromQL possible).
func parseMatchers(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		op := ""
		for _, candidate := range []string{"!=", "=~", "!~", "="} {
			if i := strings.Index(part, candidate); i > 0 {
				op = candidate
				break
			}
		}
		if op == "" {
			return "", fmt.Errorf("invalid matcher %q", part)
		}
		i := strings.Index(part, op)
		name, value := strings.TrimSpace(part[:i]), strings.TrimSpace(part[i+len(op):])
		unq, err := strconv.Unquote(value)
		if !validLabel(name) || err != nil || !strings.HasPrefix(value, `"`) {
			return "", fmt.Errorf("invalid matcher %q (expected label=\"value\")", part)
		}
		out = append(out, name+op+strconv.Quote(unq))
	}
	return strings.Join(out, ","), nil
}

// sel insère les sélecteurs configurés dans un sélecteur de série.
func (c *Conn) sel(inner string) string {
	switch {
	case c.matchers == "":
		return "{" + inner + "}"
	case inner == "":
		return "{" + c.matchers + "}"
	}
	return "{" + inner + "," + c.matchers + "}"
}

func (c *Conn) Type() string                                { return Type }
func (c *Conn) RequiredPermissions() []connector.Permission { return permissions }

func (c *Conn) SyncInventory(context.Context, time.Time) (<-chan connector.Resource, error) {
	return nil, connector.ErrNotSupported
}

func (c *Conn) SyncBilling(context.Context, connector.Period) (<-chan connector.CostLine, error) {
	return nil, connector.ErrNotSupported
}

type apiResponse struct {
	Status    string `json:"status"`
	ErrorType string `json:"errorType"`
	Error     string `json:"error"`
	Data      struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Values [][2]any          `json:"values"`
			Value  [2]any            `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

func (c *Conn) query(ctx context.Context, path string, q url.Values) (apiResponse, error) {
	var out apiResponse
	if err := c.cl.Get(ctx, rest.Join(c.base, path, q), &out); err != nil {
		return out, err
	}
	if out.Status != "success" {
		return out, fmt.Errorf("prometheus: %s: %s", out.ErrorType, out.Error)
	}
	return out, nil
}

// Validate exécute une requête instantanée et vérifie la présence des séries attendues.
func (c *Conn) Validate(ctx context.Context, _ connector.Config) error {
	probe := "count(container_cpu_usage_seconds_total" + c.sel("") + ")"
	if c.mode == "node" {
		probe = "count(node_cpu_seconds_total" + c.sel("") + ")"
	}
	res, err := c.query(ctx, "api/v1/query", url.Values{"query": {probe}})
	if err != nil {
		return err
	}
	if len(res.Data.Result) == 0 {
		return errors.New("no matching series: check the mode, the matchers and that the exporters are scraped")
	}
	return nil
}

func (c *Conn) Health(ctx context.Context) connector.HealthStatus {
	h := connector.HealthStatus{Status: connector.HealthOK, CheckedAt: c.now()}
	if _, err := c.query(ctx, "api/v1/query", url.Values{"query": {"1"}}); err != nil {
		h.Status, h.Message = connector.HealthDown, err.Error()
	}
	return h
}

// spec associe une requête PromQL à une métrique normalisée et à une ressource.
type spec struct {
	metric string
	query  string
	rtype  string
	id     func(l map[string]string) string
}

func (c *Conn) specs() []spec {
	rate := "[" + promDuration(max(c.step, time.Minute)) + "]"
	ctr := `container!="",container!="POD",image!=""`
	if c.mode == "node" {
		id := func(l map[string]string) string { return l[c.idLabel] }
		by := " by (" + c.idLabel + ")"
		return []spec{
			{model.MetricCPUUtil, `1 - avg` + by + ` (rate(node_cpu_seconds_total` + c.sel(`mode="idle"`) + rate + `))`, model.TypeInstance, id},
			{model.MetricMemUtil, `1 - sum` + by + ` (node_memory_MemAvailable_bytes` + c.sel("") + `) / sum` + by + ` (node_memory_MemTotal_bytes` + c.sel("") + `)`, model.TypeInstance, id},
			{model.MetricMemUsageBytes, `sum` + by + ` (node_memory_MemTotal_bytes` + c.sel("") + ` - node_memory_MemAvailable_bytes` + c.sel("") + `)`, model.TypeInstance, id},
			{model.MetricNetRxBytesPerSec, `sum` + by + ` (rate(node_network_receive_bytes_total` + c.sel(`device!~"lo|veth.*|cali.*|docker.*|br-.*"`) + rate + `))`, model.TypeInstance, id},
			{model.MetricNetTxBytesPerSec, `sum` + by + ` (rate(node_network_transmit_bytes_total` + c.sel(`device!~"lo|veth.*|cali.*|docker.*|br-.*"`) + rate + `))`, model.TypeInstance, id},
		}
	}
	pod := func(l map[string]string) string {
		if l["namespace"] == "" || l["pod"] == "" {
			return ""
		}
		return c.cluster + "/" + l["namespace"] + "/" + l["pod"]
	}
	node := func(l map[string]string) string { return l["node"] }
	deploy := func(l map[string]string) string {
		if l["namespace"] == "" || l["deployment"] == "" {
			return ""
		}
		return c.cluster + "/" + l["namespace"] + "/Deployment/" + l["deployment"]
	}
	return []spec{
		{model.MetricCPUUsageCores, `sum by (namespace, pod) (rate(container_cpu_usage_seconds_total` + c.sel(ctr) + rate + `))`, model.TypeK8sPod, pod},
		{model.MetricMemUsageBytes, `sum by (namespace, pod) (container_memory_working_set_bytes` + c.sel(ctr) + `)`, model.TypeK8sPod, pod},
		{model.MetricCPURequestCores, `sum by (namespace, pod) (kube_pod_container_resource_requests` + c.sel(`resource="cpu"`) + `)`, model.TypeK8sPod, pod},
		{model.MetricMemRequestBytes, `sum by (namespace, pod) (kube_pod_container_resource_requests` + c.sel(`resource="memory"`) + `)`, model.TypeK8sPod, pod},
		{model.MetricCPUCapacityCores, `sum by (node) (kube_node_status_capacity` + c.sel(`resource="cpu"`) + `)`, model.TypeK8sNode, node},
		{model.MetricMemCapacityBytes, `sum by (node) (kube_node_status_capacity` + c.sel(`resource="memory"`) + `)`, model.TypeK8sNode, node},
		{model.MetricReplicas, `sum by (namespace, deployment) (kube_deployment_status_replicas` + c.sel("") + `)`, model.TypeK8sWorkload, deploy},
	}
}

func promDuration(d time.Duration) string {
	if d%time.Minute == 0 {
		return strconv.Itoa(int(d/time.Minute)) + "m"
	}
	return strconv.Itoa(int(d/time.Second)) + "s"
}

// SyncMetrics exécute les requêtes query_range sur la fenêtre, par tranches d'un jour au plus.
func (c *Conn) SyncMetrics(ctx context.Context, w connector.TimeWindow) (<-chan connector.MetricPoint, error) {
	step := c.step
	if w.Step > step {
		step = w.Step
	}
	return connector.Stream(ctx, 2048, func(ctx context.Context, emit func(connector.MetricPoint) bool) error {
		for from := w.From.Truncate(step); from.Before(w.To); from = from.Add(24 * time.Hour) {
			to := from.Add(24 * time.Hour)
			if to.After(w.To) {
				to = w.To
			}
			for _, sp := range c.specs() {
				q := url.Values{"query": {sp.query}, "start": {strconv.FormatInt(from.Unix(), 10)}, "end": {strconv.FormatInt(to.Unix(), 10)},
					"step": {strconv.Itoa(int(step / time.Second))}}
				res, err := c.query(ctx, "api/v1/query_range", q)
				if err != nil {
					return fmt.Errorf("%s: %w", sp.metric, err)
				}
				for _, series := range res.Data.Result {
					ext := sp.id(series.Metric)
					if ext == "" {
						continue
					}
					for _, v := range series.Values {
						ts, val, ok := sample(v)
						if !ok || !w.From.IsZero() && ts.Before(w.From) || !ts.Before(w.To) {
							continue
						}
						if !emit(connector.MetricPoint{ResourceType: sp.rtype, ResourceExternalID: ext, Metric: sp.metric, TS: ts, Value: val}) {
							return nil
						}
					}
				}
			}
		}
		return nil
	}), nil
}

// sample décode un échantillon [horodatage, "valeur"] ; NaN et infinis sont ignorés.
func sample(v [2]any) (time.Time, float64, bool) {
	var sec float64
	switch t := v[0].(type) {
	case json.Number:
		f, err := t.Float64()
		if err != nil {
			return time.Time{}, 0, false
		}
		sec = f
	case float64:
		sec = t
	default:
		return time.Time{}, 0, false
	}
	s, ok := v[1].(string)
	if !ok {
		return time.Time{}, 0, false
	}
	val, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(val) || math.IsInf(val, 0) {
		return time.Time{}, 0, false
	}
	return time.Unix(0, int64(sec*1e9)).UTC(), val, true
}
