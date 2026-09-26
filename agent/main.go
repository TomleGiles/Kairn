// Commande kairn-agent : agent Kairn (M-01), pour les hôtes et VM sans pile
// de métriques. Binaire unique, sans privilège : il lit /proc et /sys, pousse
// ses métriques en OTLP/HTTP (protobuf) vers la passerelle d'ingestion et
// déclare l'hôte dans l'inventaire, avec le jeton du connecteur « agent ».
//
//	KAIRN_AGENT_GATEWAY=https://ingest.kairn.example KAIRN_AGENT_TOKEN=… kairn-agent
//
// Options : --gateway, --token-file, --interval (défaut 30 s), --label k=v
// (répétable), --host-root /host (conteneur DaemonSet), --once.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	colmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"
)

var version = "dev"

// point est une mesure instantanée.
type point struct {
	name  string
	ts    time.Time
	value float64
}

// HostInfo est la description d'hôte envoyée à la passerelle.
type HostInfo struct {
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

//nolint:unused // utilisé par sample_linux.go (build Linux)
func numCPU() int { return runtime.NumCPU() }

type labelFlag map[string]string

func (l labelFlag) String() string { return fmt.Sprint(map[string]string(l)) }
func (l labelFlag) Set(v string) error {
	k, val, ok := strings.Cut(v, "=")
	if !ok || k == "" {
		return errors.New("label must be key=value")
	}
	l[k] = val
	return nil
}

// Agent collecte et pousse les données.
type Agent struct {
	gateway  string
	token    string
	interval time.Duration
	labels   map[string]string
	sampler  *sampler
	http     *http.Client
	log      *slog.Logger
	host     HostInfo
	// buffer conserve les points non envoyés (borne : maxBuffered) pour survivre à une coupure réseau.
	buffer []point
}

const maxBuffered = 20_000

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(log, os.Args[1:]); err != nil {
		log.Error("kairn-agent stopped", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("kairn-agent", flag.ContinueOnError)
	gateway := fs.String("gateway", os.Getenv("KAIRN_AGENT_GATEWAY"), "URL de la passerelle d'ingestion")
	tokenFile := fs.String("token-file", os.Getenv("KAIRN_AGENT_TOKEN_FILE"), "fichier contenant le jeton du connecteur")
	interval := fs.Duration("interval", 30*time.Second, "période de collecte")
	hostRoot := fs.String("host-root", os.Getenv("KAIRN_AGENT_HOST_ROOT"), "racine du système hôte (ex. /host dans un conteneur)")
	hostID := fs.String("host-id", "", "identifiant de l'hôte (défaut : /etc/machine-id)")
	once := fs.Bool("once", false, "une seule collecte puis sortie (tests)")
	labels := labelFlag{}
	fs.Var(labels, "label", "étiquette key=value ajoutée à l'hôte (répétable)")
	showVersion := fs.Bool("version", false, "affiche la version")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		fmt.Println("kairn-agent", version)
		return nil
	}
	token := os.Getenv("KAIRN_AGENT_TOKEN")
	if *tokenFile != "" {
		b, err := os.ReadFile(*tokenFile)
		if err != nil {
			return fmt.Errorf("read token file: %w", err)
		}
		token = strings.TrimSpace(string(b))
	}
	if *gateway == "" || token == "" {
		return errors.New("KAIRN_AGENT_GATEWAY and KAIRN_AGENT_TOKEN (or --token-file) are required")
	}
	if !strings.HasPrefix(*gateway, "https://") && !strings.HasPrefix(*gateway, "http://localhost") && !strings.HasPrefix(*gateway, "http://127.0.0.1") {
		return errors.New("the gateway must use https:// (the agent token is a secret)")
	}
	if *interval < 10*time.Second {
		*interval = 10 * time.Second
	}
	a := &Agent{gateway: strings.TrimRight(*gateway, "/"), token: token, interval: *interval, labels: labels,
		sampler: &sampler{root: strings.TrimRight(*hostRoot, "/")}, http: &http.Client{Timeout: 30 * time.Second}, log: log}
	if err := a.describe(*hostID); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return a.Run(ctx, *once)
}

// describe construit la fiche d'hôte.
func (a *Agent) describe(id string) error {
	h, err := a.sampler.Host()
	if err != nil {
		a.log.Warn("host description incomplete", "err", err)
	}
	if id != "" {
		h.HostID = id
	}
	hostname, _ := os.Hostname()
	if h.HostID == "" {
		h.HostID = hostname
	}
	if h.HostID == "" {
		return errors.New("cannot determine a host id: pass --host-id")
	}
	h.Hostname, h.Arch, h.VCPUs, h.Version = hostname, runtime.GOARCH, runtime.NumCPU(), version
	if h.OS == "" {
		h.OS = runtime.GOOS
	}
	h.Labels = a.labels
	a.host = h
	return nil
}

// Run collecte à intervalle régulier ; l'inventaire est rafraîchi toutes les heures.
func (a *Agent) Run(ctx context.Context, once bool) error {
	if err := a.sendInventory(ctx); err != nil {
		a.log.Warn("inventory not sent", "err", err)
	}
	lastInventory := time.Now()
	t := time.NewTicker(a.interval)
	defer t.Stop()
	for {
		pts, err := a.sampler.Sample()
		switch {
		case errors.Is(err, ErrUnsupported):
			a.log.Warn("metrics unsupported on this platform, inventory only")
		case err != nil:
			a.log.Warn("sample failed", "err", err)
		default:
			a.buffer = append(a.buffer, pts...)
			if len(a.buffer) > maxBuffered {
				a.buffer = a.buffer[len(a.buffer)-maxBuffered:] // les points les plus anciens sont abandonnés
			}
			if err := a.flush(ctx); err != nil {
				a.log.Warn("metrics not sent, kept in buffer", "err", err, "buffered", len(a.buffer))
			}
		}
		if once {
			return nil
		}
		if time.Since(lastInventory) > time.Hour {
			if err := a.sendInventory(ctx); err == nil {
				lastInventory = time.Now()
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

func (a *Agent) post(ctx context.Context, path, contentType string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.gateway+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", "kairn-agent/"+version)
	resp, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s: HTTP %d", path, resp.StatusCode)
	}
	return nil
}

func (a *Agent) sendInventory(ctx context.Context) error {
	b, err := json.Marshal(a.host)
	if err != nil {
		return err
	}
	return a.post(ctx, "/ingest/v1/agent/inventory", "application/json", b)
}

// flush envoie le tampon en OTLP protobuf (lots de 5 000 points).
func (a *Agent) flush(ctx context.Context) error {
	for len(a.buffer) > 0 {
		n := min(len(a.buffer), 5000)
		body, err := proto.Marshal(a.otlp(a.buffer[:n]))
		if err != nil {
			return err
		}
		if err := a.post(ctx, "/ingest/v1/otlp/v1/metrics", "application/x-protobuf", body); err != nil {
			return err
		}
		a.buffer = a.buffer[n:]
	}
	return nil
}

// otlp construit une requête ExportMetricsService (jauges) pour l'hôte.
func (a *Agent) otlp(pts []point) *colmetrics.ExportMetricsServiceRequest {
	str := func(k, v string) *commonpb.KeyValue {
		return &commonpb.KeyValue{Key: k, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v}}}
	}
	byName := map[string][]*metricspb.NumberDataPoint{}
	var order []string
	for _, p := range pts {
		if _, ok := byName[p.name]; !ok {
			order = append(order, p.name)
		}
		byName[p.name] = append(byName[p.name], &metricspb.NumberDataPoint{
			TimeUnixNano: uint64(p.ts.UnixNano()), //nolint:gosec // horodatage postérieur à 1970
			Value:        &metricspb.NumberDataPoint_AsDouble{AsDouble: p.value},
		})
	}
	var metrics []*metricspb.Metric
	for _, name := range order {
		metrics = append(metrics, &metricspb.Metric{Name: name, Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{DataPoints: byName[name]}}})
	}
	return &colmetrics.ExportMetricsServiceRequest{ResourceMetrics: []*metricspb.ResourceMetrics{{
		Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{str("host.id", a.host.HostID), str("host.name", a.host.Hostname),
			str("service.name", "kairn-agent")}},
		ScopeMetrics: []*metricspb.ScopeMetrics{{Scope: &commonpb.InstrumentationScope{Name: "kairn-agent", Version: version}, Metrics: metrics}},
	}}}
}
