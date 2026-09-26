package model

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/shopspring/decimal"
)

// ConnectorStatus décrit l'état de santé d'un connecteur.
type ConnectorStatus string

// États de connecteur.
const (
	ConnectorPending  ConnectorStatus = "pending"
	ConnectorOK       ConnectorStatus = "ok"
	ConnectorDegraded ConnectorStatus = "degraded"
	ConnectorError    ConnectorStatus = "error"
	ConnectorDisabled ConnectorStatus = "disabled"
)

// Connector est une source de données configurée pour une organisation.
// La configuration secrète est chiffrée (enveloppe) et n'est jamais exposée.
type Connector struct {
	ID              string            `json:"id" db:"id"`
	OrgID           string            `json:"org_id" db:"org_id"`
	Type            string            `json:"type" db:"type"`
	Name            string            `json:"name" db:"name"`
	Settings        map[string]string `json:"settings" db:"settings"`
	SecretsEnc      []byte            `json:"-" db:"secrets_enc"`
	Status          ConnectorStatus   `json:"status" db:"status"`
	StatusMessage   string            `json:"status_message" db:"status_message"`
	IntervalSeconds int               `json:"interval_seconds" db:"interval_seconds"`
	Enabled         bool              `json:"enabled" db:"enabled"`
	LastSyncAt      *time.Time        `json:"last_sync_at,omitempty" db:"last_sync_at"`
	LastSuccessAt   *time.Time        `json:"last_success_at,omitempty" db:"last_success_at"`
	WebhookToken    *string           `json:"-" db:"webhook_token"`
	CreatedAt       time.Time         `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at" db:"updated_at"`
}

// ConnectorRun trace une exécution de synchronisation.
type ConnectorRun struct {
	ID          string     `json:"id" db:"id"`
	OrgID       string     `json:"org_id" db:"org_id"`
	ConnectorID string     `json:"connector_id" db:"connector_id"`
	Kind        string     `json:"kind" db:"kind"`     // inventory | metrics | billing | events
	Status      string     `json:"status" db:"status"` // running | ok | error
	Items       int        `json:"items" db:"items"`
	Error       string     `json:"error" db:"error"`
	StartedAt   time.Time  `json:"started_at" db:"started_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty" db:"finished_at"`
}

// Types de ressources normalisés, communs à tous les fournisseurs.
const (
	TypeProject      = "project"
	TypeInstance     = "compute.instance"
	TypeFlavor       = "compute.flavor"
	TypeVolume       = "storage.volume"
	TypeSnapshot     = "storage.snapshot"
	TypeBucket       = "storage.bucket"
	TypeIP           = "network.ip"
	TypeLoadBalancer = "network.loadbalancer"
	TypeNetwork      = "network.network"
	TypeDatabase     = "database.instance"
	TypeK8sCluster   = "k8s.cluster"
	TypeK8sNode      = "k8s.node"
	TypeK8sNamespace = "k8s.namespace"
	TypeK8sWorkload  = "k8s.workload"
	TypeK8sPod       = "k8s.pod"
	TypeK8sPVC       = "k8s.pvc"
	TypeHost         = "host" // machine suivie par l'agent Kairn
	TypeService      = "service"
)

// Relations du graphe de topologie.
const (
	RelContains   = "contains"    // projet → VM, cluster → node, namespace → workload
	RelAttachedTo = "attached_to" // volume → VM, IP → VM
	RelRunsOn     = "runs_on"     // pod → node, node → VM
	RelOwns       = "owns"        // workload → pod
	RelBacks      = "backs"       // VM → node K8s
)

// Resource est une version d'une ressource d'inventaire. ID est stable entre
// versions ; une nouvelle version est créée quand un attribut tarifant change.
type Resource struct {
	ID          string            `json:"id" db:"id"`
	OrgID       string            `json:"org_id" db:"org_id"`
	ConnectorID string            `json:"connector_id" db:"connector_id"`
	Provider    string            `json:"provider" db:"provider"`
	Type        string            `json:"type" db:"type"`
	ExternalID  string            `json:"external_id" db:"external_id"`
	Name        string            `json:"name" db:"name"`
	Region      string            `json:"region" db:"region"`
	Attributes  map[string]any    `json:"attributes" db:"attributes"`
	Labels      map[string]string `json:"labels" db:"labels"`
	ValidFrom   time.Time         `json:"valid_from" db:"valid_from"`
	ValidTo     *time.Time        `json:"valid_to,omitempty" db:"valid_to"`
}

// Alive indique si la version couvre l'instant t.
func (r Resource) Alive(t time.Time) bool {
	if t.Before(r.ValidFrom) {
		return false
	}
	return r.ValidTo == nil || t.Before(*r.ValidTo)
}

// Overlap renvoie la durée de vie de la version dans [from, to).
func (r Resource) Overlap(from, to time.Time) time.Duration {
	start := r.ValidFrom
	if from.After(start) {
		start = from
	}
	end := to
	if r.ValidTo != nil && r.ValidTo.Before(end) {
		end = *r.ValidTo
	}
	if !end.After(start) {
		return 0
	}
	return end.Sub(start)
}

// Attr renvoie un attribut chaîne.
func (r Resource) Attr(key string) string {
	v, ok := r.Attributes[key]
	if !ok || v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case bool:
		return strconv.FormatBool(x)
	default:
		return fmt.Sprint(x)
	}
}

// AttrDecimal renvoie un attribut numérique en décimal (0 si absent ou invalide).
// Les quantités (vCPU, Go) sont converties via leur représentation texte la
// plus courte, ce qui évite toute dérive binaire.
func (r Resource) AttrDecimal(key string) decimal.Decimal {
	s := r.Attr(key)
	if s == "" {
		return decimal.Zero
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero
	}
	return d
}

// ResourceEdge est une relation du graphe de topologie, historisée.
type ResourceEdge struct {
	OrgID     string     `json:"org_id" db:"org_id"`
	ParentID  string     `json:"parent_id" db:"parent_id"`
	ChildID   string     `json:"child_id" db:"child_id"`
	Relation  string     `json:"relation" db:"relation"`
	ValidFrom time.Time  `json:"valid_from" db:"valid_from"`
	ValidTo   *time.Time `json:"valid_to,omitempty" db:"valid_to"`
}

// MetricPoint est un point de série temporelle normalisé.
type MetricPoint struct {
	OrgID      string    `json:"org_id"`
	ResourceID string    `json:"resource_id"`
	Metric     string    `json:"metric"`
	TS         time.Time `json:"ts"`
	Value      float64   `json:"value"`
}

// Métriques normalisées (M-05).
const (
	MetricCPUUsageCores    = "cpu.usage_cores"
	MetricCPUUtil          = "cpu.utilization" // 0..1
	MetricCPURequestCores  = "cpu.request_cores"
	MetricCPULimitCores    = "cpu.limit_cores"
	MetricCPUCapacityCores = "cpu.capacity_cores"
	MetricMemUsageBytes    = "mem.usage_bytes"
	MetricMemUtil          = "mem.utilization" // 0..1
	MetricMemRequestBytes  = "mem.request_bytes"
	MetricMemLimitBytes    = "mem.limit_bytes"
	MetricMemCapacityBytes = "mem.capacity_bytes"
	MetricDiskUsedBytes    = "disk.used_bytes"
	MetricDiskIOPS         = "disk.iops"
	MetricNetRxBytesPerSec = "net.rx_bytes_per_sec"
	MetricNetTxBytesPerSec = "net.tx_bytes_per_sec"
	MetricRequestsPerSec   = "http.requests_per_sec"
	MetricStorageBytes     = "storage.bytes" // taille d'un bucket
	MetricReplicas         = "k8s.replicas"
)

// Event est un événement horodaté : déploiement, HPA, incident, changement d'inventaire.
type Event struct {
	OrgID      string         `json:"org_id"`
	TS         time.Time      `json:"ts"`
	Kind       string         `json:"kind"`
	Source     string         `json:"source"`
	ResourceID string         `json:"resource_id,omitempty"`
	Title      string         `json:"title"`
	Payload    map[string]any `json:"payload,omitempty"`
}

// Types d'événements.
const (
	EventDeployment      = "deployment"
	EventHPAScale        = "hpa_scale"
	EventIncident        = "incident"
	EventInventoryChange = "inventory_change"
	EventMetricSpike     = "metric_spike"
	EventK8s             = "k8s_event"
)
