// Package kubernetes est le connecteur Kubernetes (M-01) : inventaire du
// cluster via l'API server (nodes, namespaces, workloads, pods, PVC, HPA),
// utilisation instantanée via metrics.k8s.io et événements. Lecture seule :
// un ClusterRole « get/list/watch » suffit (deploy/kubernetes/kairn-reader.yaml).
//
// L'historique d'utilisation provient du connecteur Prometheus rattaché à ce
// cluster (paramètre target_connector_id).
package kubernetes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/kairn-io/kairn/connectors/internal/rest"
	"github.com/kairn-io/kairn/pkg/connector"
	"github.com/kairn-io/kairn/pkg/model"
)

// Type est l'identifiant du connecteur.
const Type = "kubernetes"

var permissions = []connector.Permission{
	{Scope: "nodes, namespaces, pods, persistentvolumeclaims, events: get, list", Description: "Inventaire et événements du cluster."},
	{Scope: "apps/deployments, statefulsets, daemonsets, replicasets: get, list", Description: "Workloads et rattachement des pods."},
	{Scope: "batch/jobs, cronjobs: get, list", Description: "Rattachement des pods de jobs.", Optional: true},
	{Scope: "autoscaling/horizontalpodautoscalers: get, list", Description: "Bornes d'autoscaling des workloads.", Optional: true},
	{Scope: "metrics.k8s.io/pods, nodes: get, list", Description: "Utilisation instantanée (metrics-server).", Optional: true},
}

func init() {
	connector.Register(connector.TypeInfo{
		Type: Type, DisplayName: "Kubernetes (on-prem, OVHcloud MKS, Scaleway Kapsule…)", Category: connector.CategoryKubernetes, Provider: "kubernetes",
		Resources:       []string{model.TypeK8sCluster, model.TypeK8sNode, model.TypeK8sNamespace, model.TypeK8sWorkload, model.TypeK8sPod, model.TypeK8sPVC},
		DefaultInterval: 15 * time.Minute, Metrics: true, DocsURL: "/docs/connectors/kubernetes", Permissions: permissions,
		Fields: []connector.Field{
			{Name: "cluster_name", Label: "Nom du cluster", Required: true, Help: "Identifiant stable, repris par le connecteur Prometheus"},
			{Name: "api_server", Label: "URL de l'API server", Help: "ex. https://xxxx.c1.gra7.k8s.ovh.net ; vide = dans le cluster"},
			{Name: "token", Label: "Jeton du compte de service (lecture seule)", Secret: true},
			{Name: "ca_cert", Label: "Certificat d'autorité du cluster (PEM)"},
			{Name: "control_plane_tier", Label: "Offre du plan de contrôle", Default: "standard", Help: "free, standard… (grille tarifaire kubernetes)"},
			{Name: "event_reasons", Label: "Événements suivis", Help: "Raisons séparées par des virgules ; vide = avertissements et mises à l'échelle"},
		},
	}, New)
}

// Conn est une instance du connecteur.
type Conn struct {
	cluster string
	api     string
	tier    string
	reasons map[string]bool
	cl      *rest.Client
	now     func() time.Time
}

const inClusterToken = "/var/run/secrets/kubernetes.io/serviceaccount/token" //nolint:gosec // chemin standard, pas un secret
const inClusterCA = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"

// New construit le connecteur.
func New(cfg connector.Config) (connector.Connector, error) {
	cluster := cfg.Setting("cluster_name", "")
	if cluster == "" {
		return nil, fmt.Errorf("%w: cluster_name", connector.ErrMissingConfig)
	}
	api := strings.TrimRight(cfg.Setting("api_server", ""), "/")
	token := cfg.Secret("token")
	ca := cfg.Setting("ca_cert", "")
	if api == "" {
		// Mode « dans le cluster » : compte de service monté par Kubernetes.
		host, port := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT")
		if host == "" {
			return nil, fmt.Errorf("%w: api_server", connector.ErrMissingConfig)
		}
		api = "https://" + host + ":" + port
		if token == "" {
			b, err := os.ReadFile(inClusterToken)
			if err != nil {
				return nil, fmt.Errorf("read service account token: %w", err)
			}
			token = strings.TrimSpace(string(b))
		}
		if ca == "" {
			if b, err := os.ReadFile(inClusterCA); err == nil {
				ca = string(b)
			}
		}
	}
	if token == "" {
		return nil, fmt.Errorf("%w: token", connector.ErrMissingConfig)
	}
	hc, err := rest.NewHTTP(rest.Options{CAPEM: ca})
	if err != nil {
		return nil, err
	}
	c := &Conn{cluster: cluster, api: api, tier: cfg.Setting("control_plane_tier", "standard"), now: func() time.Time { return time.Now().UTC() }}
	c.cl = &rest.Client{HTTP: hc, Auth: func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }}
	if rs := cfg.Setting("event_reasons", ""); rs != "" {
		c.reasons = map[string]bool{}
		for _, r := range strings.Split(rs, ",") {
			c.reasons[strings.TrimSpace(r)] = true
		}
	}
	return c, nil
}

func (c *Conn) Type() string                                { return Type }
func (c *Conn) RequiredPermissions() []connector.Permission { return permissions }

func (c *Conn) SyncBilling(context.Context, connector.Period) (<-chan connector.CostLine, error) {
	return nil, connector.ErrNotSupported
}

// Validate vérifie l'accès aux nodes et aux pods.
func (c *Conn) Validate(ctx context.Context, _ connector.Config) error {
	for _, p := range []string{"api/v1/nodes", "api/v1/pods", "apis/apps/v1/deployments"} {
		var out objectList
		if err := c.cl.Get(ctx, rest.Join(c.api, p, url.Values{"limit": {"1"}}), &out); err != nil {
			return err
		}
	}
	return nil
}

// Health interroge /version.
func (c *Conn) Health(ctx context.Context) connector.HealthStatus {
	h := connector.HealthStatus{Status: connector.HealthOK, CheckedAt: c.now()}
	var v struct {
		GitVersion string `json:"gitVersion"`
	}
	if err := c.cl.Get(ctx, rest.Join(c.api, "version", nil), &v); err != nil {
		h.Status, h.Message = connector.HealthDown, err.Error()
	}
	return h
}

// ------------------------------------------------------------------ objets Kubernetes

type meta struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	UID               string            `json:"uid"`
	Labels            map[string]string `json:"labels"`
	CreationTimestamp time.Time         `json:"creationTimestamp"`
	OwnerReferences   []struct {
		Kind       string `json:"kind"`
		Name       string `json:"name"`
		Controller bool   `json:"controller"`
	} `json:"ownerReferences"`
}

type objectList struct {
	Metadata struct {
		Continue string `json:"continue"`
	} `json:"metadata"`
	Items []json.RawMessage `json:"items"`
}

type container struct {
	Image     string `json:"image"`
	Resources struct {
		Requests map[string]string `json:"requests"`
		Limits   map[string]string `json:"limits"`
	} `json:"resources"`
}

type podSpec struct {
	NodeName       string      `json:"nodeName"`
	Containers     []container `json:"containers"`
	InitContainers []container `json:"initContainers"`
}

// list parcourt toutes les pages d'une liste d'objets.
func (c *Conn) list(ctx context.Context, path string, q url.Values, fn func(json.RawMessage) error) error {
	if q == nil {
		q = url.Values{}
	}
	q.Set("limit", "500")
	for {
		var page objectList
		if err := c.cl.Get(ctx, rest.Join(c.api, path, q), &page); err != nil {
			return err
		}
		for _, it := range page.Items {
			if err := fn(it); err != nil {
				return err
			}
		}
		if page.Metadata.Continue == "" {
			return nil
		}
		q.Set("continue", page.Metadata.Continue)
	}
}

// requests additionne les requests des conteneurs (les init containers comptent pour leur maximum, règle du scheduler).
func requests(spec podSpec) (cpu, memGB float64) {
	var initCPU, initMem float64
	for _, ct := range spec.Containers {
		cpu += ParseQuantity(ct.Resources.Requests["cpu"])
		memGB += ParseQuantity(ct.Resources.Requests["memory"]) / (1 << 30)
	}
	for _, ct := range spec.InitContainers {
		initCPU = max(initCPU, ParseQuantity(ct.Resources.Requests["cpu"]))
		initMem = max(initMem, ParseQuantity(ct.Resources.Requests["memory"])/(1<<30))
	}
	return round6(max(cpu, initCPU)), round6(max(memGB, initMem))
}

func imageTag(spec podSpec) string {
	if len(spec.Containers) == 0 {
		return ""
	}
	img := spec.Containers[0].Image
	if i := strings.LastIndex(img, "@"); i >= 0 {
		return img[i+1:]
	}
	if i := strings.LastIndex(img, ":"); i > strings.LastIndex(img, "/") {
		return img[i+1:]
	}
	return "latest"
}

func round6(v float64) float64 { return float64(int64(v*1e6+0.5)) / 1e6 }

// Identifiants externes, partagés avec le connecteur Prometheus.
func (c *Conn) nsID(ns string) string { return c.cluster + "/" + ns }
func (c *Conn) workloadID(ns, kind, name string) string {
	return c.cluster + "/" + ns + "/" + kind + "/" + name
}
func (c *Conn) podID(ns, name string) string { return c.cluster + "/" + ns + "/" + name }

// SyncInventory émet le cluster, ses nodes, namespaces, workloads, pods et PVC.
func (c *Conn) SyncInventory(ctx context.Context, _ time.Time) (<-chan connector.Resource, error) {
	return connector.Stream(ctx, 512, func(ctx context.Context, emit func(connector.Resource) bool) error {
		var ver struct {
			GitVersion string `json:"gitVersion"`
		}
		if err := c.cl.Get(ctx, rest.Join(c.api, "version", nil), &ver); err != nil {
			return fmt.Errorf("version: %w", err)
		}
		inCluster := connector.Edge{Relation: model.RelContains, ParentType: model.TypeK8sCluster, ParentExternalID: c.cluster}
		if !emit(connector.Resource{Type: model.TypeK8sCluster, ExternalID: c.cluster, Name: c.cluster,
			Attributes: map[string]any{"version": ver.GitVersion, "control_plane_tier": c.tier, "k8s.cluster": c.cluster}, Labels: map[string]string{}}) {
			return nil
		}
		if err := c.nodes(ctx, inCluster, emit); err != nil {
			return err
		}
		if err := c.namespaces(ctx, inCluster, emit); err != nil {
			return err
		}
		owners, err := c.workloads(ctx, emit)
		if err != nil {
			return err
		}
		if err := c.pods(ctx, owners, emit); err != nil {
			return err
		}
		return c.pvcs(ctx, emit)
	}), nil
}

func (c *Conn) nodes(ctx context.Context, inCluster connector.Edge, emit func(connector.Resource) bool) error {
	return c.list(ctx, "api/v1/nodes", nil, func(raw json.RawMessage) error {
		var n struct {
			Metadata meta `json:"metadata"`
			Spec     struct {
				ProviderID    string `json:"providerID"`
				Unschedulable bool   `json:"unschedulable"`
			} `json:"spec"`
			Status struct {
				Capacity    map[string]string `json:"capacity"`
				Allocatable map[string]string `json:"allocatable"`
				NodeInfo    struct {
					KubeletVersion string `json:"kubeletVersion"`
				} `json:"nodeInfo"`
			} `json:"status"`
		}
		if err := json.Unmarshal(raw, &n); err != nil {
			return err
		}
		attrs := map[string]any{
			"cpu_capacity_cores": ParseQuantity(n.Status.Capacity["cpu"]), "mem_capacity_gb": round6(ParseQuantity(n.Status.Capacity["memory"]) / (1 << 30)),
			"cpu_allocatable_cores": ParseQuantity(n.Status.Allocatable["cpu"]), "k8s.cluster": c.cluster, "kubelet_version": n.Status.NodeInfo.KubeletVersion,
			"unschedulable": n.Spec.Unschedulable,
		}
		if it := n.Metadata.Labels["node.kubernetes.io/instance-type"]; it != "" {
			attrs["instance_type"] = it
		} else if it := n.Metadata.Labels["beta.kubernetes.io/instance-type"]; it != "" {
			attrs["instance_type"] = it
		}
		if id := providerInstanceID(n.Spec.ProviderID); id != "" {
			attrs["provider_instance_id"] = id
		}
		created := n.Metadata.CreationTimestamp.UTC()
		if !emit(connector.Resource{Type: model.TypeK8sNode, ExternalID: n.Metadata.Name, Name: n.Metadata.Name,
			Region: n.Metadata.Labels["topology.kubernetes.io/region"], Attributes: attrs, Labels: nonNil(n.Metadata.Labels),
			Parents: []connector.Edge{inCluster}, CreatedAt: &created}) {
			return context.Canceled
		}
		return nil
	})
}

// providerInstanceID extrait l'identifiant de VM d'un providerID (openstack:///<uuid>, aws:///zone/i-…).
func providerInstanceID(p string) string {
	if p == "" {
		return ""
	}
	p = strings.TrimRight(p, "/")
	return p[strings.LastIndex(p, "/")+1:]
}

func nonNil(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func (c *Conn) namespaces(ctx context.Context, inCluster connector.Edge, emit func(connector.Resource) bool) error {
	return c.list(ctx, "api/v1/namespaces", nil, func(raw json.RawMessage) error {
		var ns struct {
			Metadata meta `json:"metadata"`
		}
		if err := json.Unmarshal(raw, &ns); err != nil {
			return err
		}
		created := ns.Metadata.CreationTimestamp.UTC()
		if !emit(connector.Resource{Type: model.TypeK8sNamespace, ExternalID: c.nsID(ns.Metadata.Name), Name: ns.Metadata.Name,
			Attributes: map[string]any{"k8s.namespace": ns.Metadata.Name, "k8s.cluster": c.cluster}, Labels: nonNil(ns.Metadata.Labels),
			Parents: []connector.Edge{inCluster}, CreatedAt: &created}) {
			return context.Canceled
		}
		return nil
	})
}

// ownerKey identifie un contrôleur intermédiaire (ReplicaSet, Job) dans un namespace.
type ownerKey struct{ ns, kind, name string }

// workloads émet Deployments, StatefulSets, DaemonSets et CronJobs, et renvoie
// la résolution ReplicaSet → Deployment et Job → CronJob.
func (c *Conn) workloads(ctx context.Context, emit func(connector.Resource) bool) (map[ownerKey]ownerKey, error) {
	hpaMax := map[ownerKey]int{}
	_ = c.list(ctx, "apis/autoscaling/v2/horizontalpodautoscalers", nil, func(raw json.RawMessage) error {
		var h struct {
			Metadata meta `json:"metadata"`
			Spec     struct {
				MaxReplicas    int `json:"maxReplicas"`
				ScaleTargetRef struct {
					Kind string `json:"kind"`
					Name string `json:"name"`
				} `json:"scaleTargetRef"`
			} `json:"spec"`
		}
		if json.Unmarshal(raw, &h) == nil {
			hpaMax[ownerKey{h.Metadata.Namespace, h.Spec.ScaleTargetRef.Kind, h.Spec.ScaleTargetRef.Name}] = h.Spec.MaxReplicas
		}
		return nil
	}) // HPA facultatifs : une erreur de permission n'interrompt pas l'inventaire.

	emitWorkload := func(kind string, m meta, replicas int, spec podSpec) bool {
		cpu, mem := requests(spec)
		attrs := map[string]any{"kind": kind, "replicas": replicas, "k8s.namespace": m.Namespace, "k8s.cluster": c.cluster, "k8s.workload": m.Name,
			"cpu_request_cores": cpu, "mem_request_gb": mem, "image_tag": imageTag(spec)}
		if mx, ok := hpaMax[ownerKey{m.Namespace, kind, m.Name}]; ok {
			attrs["hpa_max"] = mx
		}
		created := m.CreationTimestamp.UTC()
		return emit(connector.Resource{Type: model.TypeK8sWorkload, ExternalID: c.workloadID(m.Namespace, kind, m.Name), Name: m.Name,
			Attributes: attrs, Labels: nonNil(m.Labels), CreatedAt: &created,
			Parents: []connector.Edge{{Relation: model.RelContains, ParentType: model.TypeK8sNamespace, ParentExternalID: c.nsID(m.Namespace)}}})
	}
	type workloadObj struct {
		Metadata meta `json:"metadata"`
		Spec     struct {
			Replicas *int `json:"replicas"`
			Template struct {
				Spec podSpec `json:"spec"`
			} `json:"template"`
			JobTemplate struct {
				Spec struct {
					Template struct {
						Spec podSpec `json:"spec"`
					} `json:"template"`
				} `json:"spec"`
			} `json:"jobTemplate"`
		} `json:"spec"`
		Status struct {
			DesiredNumberScheduled int `json:"desiredNumberScheduled"`
		} `json:"status"`
	}
	for _, w := range []struct{ path, kind string }{
		{"apis/apps/v1/deployments", "Deployment"}, {"apis/apps/v1/statefulsets", "StatefulSet"},
		{"apis/apps/v1/daemonsets", "DaemonSet"}, {"apis/batch/v1/cronjobs", "CronJob"},
	} {
		err := c.list(ctx, w.path, nil, func(raw json.RawMessage) error {
			var o workloadObj
			if err := json.Unmarshal(raw, &o); err != nil {
				return err
			}
			replicas := 1
			if o.Spec.Replicas != nil {
				replicas = *o.Spec.Replicas
			}
			spec := o.Spec.Template.Spec
			switch w.kind {
			case "DaemonSet":
				replicas = o.Status.DesiredNumberScheduled
			case "CronJob":
				spec, replicas = o.Spec.JobTemplate.Spec.Template.Spec, 0
			}
			if !emitWorkload(w.kind, o.Metadata, replicas, spec) {
				return context.Canceled
			}
			return nil
		})
		if err != nil {
			if w.kind == "CronJob" && (errors.Is(err, connector.ErrPermission) || rest.IsNotFound(err)) {
				continue
			}
			return nil, fmt.Errorf("%s: %w", w.path, err)
		}
	}
	// Résolution des contrôleurs intermédiaires.
	owners := map[ownerKey]ownerKey{}
	for _, inter := range []struct{ path, kind string }{{"apis/apps/v1/replicasets", "ReplicaSet"}, {"apis/batch/v1/jobs", "Job"}} {
		err := c.list(ctx, inter.path, nil, func(raw json.RawMessage) error {
			var o struct {
				Metadata meta `json:"metadata"`
			}
			if err := json.Unmarshal(raw, &o); err != nil {
				return err
			}
			for _, ref := range o.Metadata.OwnerReferences {
				if ref.Controller {
					owners[ownerKey{o.Metadata.Namespace, inter.kind, o.Metadata.Name}] = ownerKey{o.Metadata.Namespace, ref.Kind, ref.Name}
				}
			}
			return nil
		})
		if err != nil && !(inter.kind == "Job" && (errors.Is(err, connector.ErrPermission) || rest.IsNotFound(err))) {
			return nil, fmt.Errorf("%s: %w", inter.path, err)
		}
	}
	return owners, nil
}

// workloadOf remonte les ownerReferences d'un pod jusqu'au workload.
func workloadOf(m meta, owners map[ownerKey]ownerKey) (kind, name string) {
	for _, ref := range m.OwnerReferences {
		if !ref.Controller {
			continue
		}
		k := ownerKey{m.Namespace, ref.Kind, ref.Name}
		if parent, ok := owners[k]; ok {
			// ReplicaSet → Deployment ; Job → CronJob.
			return parent.kind, parent.name
		}
		switch ref.Kind {
		case "ReplicaSet", "Job":
			return "", "" // contrôleur orphelin : pod sans workload suivi
		}
		return ref.Kind, ref.Name
	}
	return "", ""
}

func (c *Conn) pods(ctx context.Context, owners map[ownerKey]ownerKey, emit func(connector.Resource) bool) error {
	q := url.Values{"fieldSelector": {"status.phase!=Succeeded,status.phase!=Failed"}}
	return c.list(ctx, "api/v1/pods", q, func(raw json.RawMessage) error {
		var p struct {
			Metadata meta    `json:"metadata"`
			Spec     podSpec `json:"spec"`
			Status   struct {
				Phase    string `json:"phase"`
				QOSClass string `json:"qosClass"`
			} `json:"status"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		cpu, mem := requests(p.Spec)
		attrs := map[string]any{"cpu_request_cores": cpu, "mem_request_gb": mem, "k8s.namespace": p.Metadata.Namespace, "k8s.cluster": c.cluster,
			"node": p.Spec.NodeName, "phase": p.Status.Phase, "qos_class": p.Status.QOSClass, "image_tag": imageTag(p.Spec)}
		parents := []connector.Edge{{Relation: model.RelContains, ParentType: model.TypeK8sNamespace, ParentExternalID: c.nsID(p.Metadata.Namespace)}}
		if kind, name := workloadOf(p.Metadata, owners); name != "" {
			attrs["k8s.workload"] = name
			attrs["k8s.workload_kind"] = kind
			parents = append(parents, connector.Edge{Relation: model.RelOwns, ParentType: model.TypeK8sWorkload,
				ParentExternalID: c.workloadID(p.Metadata.Namespace, kind, name)})
		}
		if p.Spec.NodeName != "" {
			parents = append(parents, connector.Edge{Relation: model.RelRunsOn, ParentType: model.TypeK8sNode, ParentExternalID: p.Spec.NodeName})
		}
		created := p.Metadata.CreationTimestamp.UTC()
		if !emit(connector.Resource{Type: model.TypeK8sPod, ExternalID: c.podID(p.Metadata.Namespace, p.Metadata.Name), Name: p.Metadata.Name,
			Attributes: attrs, Labels: nonNil(p.Metadata.Labels), Parents: parents, CreatedAt: &created}) {
			return context.Canceled
		}
		return nil
	})
}

func (c *Conn) pvcs(ctx context.Context, emit func(connector.Resource) bool) error {
	return c.list(ctx, "api/v1/persistentvolumeclaims", nil, func(raw json.RawMessage) error {
		var p struct {
			Metadata meta `json:"metadata"`
			Spec     struct {
				StorageClassName string `json:"storageClassName"`
				VolumeName       string `json:"volumeName"`
				Resources        struct {
					Requests map[string]string `json:"requests"`
				} `json:"resources"`
			} `json:"spec"`
			Status struct {
				Phase    string            `json:"phase"`
				Capacity map[string]string `json:"capacity"`
			} `json:"status"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		size := p.Status.Capacity["storage"]
		if size == "" {
			size = p.Spec.Resources.Requests["storage"]
		}
		created := p.Metadata.CreationTimestamp.UTC()
		if !emit(connector.Resource{Type: model.TypeK8sPVC, ExternalID: c.podID(p.Metadata.Namespace, p.Metadata.Name), Name: p.Metadata.Name,
			Attributes: map[string]any{"size_gb": round6(ParseQuantity(size) / (1 << 30)), "storage_class": p.Spec.StorageClassName, "volume_name": p.Spec.VolumeName,
				"phase": p.Status.Phase, "k8s.namespace": p.Metadata.Namespace, "k8s.cluster": c.cluster},
			Labels: nonNil(p.Metadata.Labels), CreatedAt: &created,
			Parents: []connector.Edge{{Relation: model.RelContains, ParentType: model.TypeK8sNamespace, ParentExternalID: c.nsID(p.Metadata.Namespace)}}}) {
			return context.Canceled
		}
		return nil
	})
}

// ------------------------------------------------------------------ métriques et événements

// SyncMetrics émet la capacité des nodes et l'utilisation instantanée (metrics-server) si elle tombe dans la fenêtre.
func (c *Conn) SyncMetrics(ctx context.Context, w connector.TimeWindow) (<-chan connector.MetricPoint, error) {
	now := c.now().Truncate(time.Minute)
	if now.Before(w.From) || !now.Before(w.To.Add(time.Minute)) {
		return nil, connector.ErrNotSupported // pas d'historique côté API server : voir le connecteur Prometheus
	}
	return connector.Stream(ctx, 1024, func(ctx context.Context, emit func(connector.MetricPoint) bool) error {
		type usage struct {
			Metadata   meta `json:"metadata"`
			Containers []struct {
				Usage map[string]string `json:"usage"`
			} `json:"containers"`
			Usage map[string]string `json:"usage"`
		}
		err := c.list(ctx, "apis/metrics.k8s.io/v1beta1/pods", nil, func(raw json.RawMessage) error {
			var u usage
			if err := json.Unmarshal(raw, &u); err != nil {
				return err
			}
			var cpu, mem float64
			for _, ct := range u.Containers {
				cpu += ParseQuantity(ct.Usage["cpu"])
				mem += ParseQuantity(ct.Usage["memory"])
			}
			id := c.podID(u.Metadata.Namespace, u.Metadata.Name)
			if !emit(connector.MetricPoint{ResourceType: model.TypeK8sPod, ResourceExternalID: id, Metric: model.MetricCPUUsageCores, TS: now, Value: round6(cpu)}) ||
				!emit(connector.MetricPoint{ResourceType: model.TypeK8sPod, ResourceExternalID: id, Metric: model.MetricMemUsageBytes, TS: now, Value: mem}) {
				return context.Canceled
			}
			return nil
		})
		if err != nil && !errors.Is(err, connector.ErrPermission) && !rest.IsNotFound(err) {
			return fmt.Errorf("metrics.k8s.io: %w", err)
		}
		return c.list(ctx, "api/v1/nodes", nil, func(raw json.RawMessage) error {
			var n struct {
				Metadata meta `json:"metadata"`
				Status   struct {
					Capacity map[string]string `json:"capacity"`
				} `json:"status"`
			}
			if err := json.Unmarshal(raw, &n); err != nil {
				return err
			}
			if !emit(connector.MetricPoint{ResourceType: model.TypeK8sNode, ResourceExternalID: n.Metadata.Name, Metric: model.MetricCPUCapacityCores, TS: now,
				Value: ParseQuantity(n.Status.Capacity["cpu"])}) ||
				!emit(connector.MetricPoint{ResourceType: model.TypeK8sNode, ResourceExternalID: n.Metadata.Name, Metric: model.MetricMemCapacityBytes, TS: now,
					Value: ParseQuantity(n.Status.Capacity["memory"])}) {
				return context.Canceled
			}
			return nil
		})
	}), nil
}

var defaultReasons = map[string]bool{"SuccessfulRescale": true, "ScalingReplicaSet": true, "OOMKilling": true, "Evicted": true,
	"FailedScheduling": true, "NodeNotReady": true, "BackOff": true}

// SyncEvents remonte les événements Kubernetes depuis since (mises à l'échelle, erreurs de planification, OOM…).
func (c *Conn) SyncEvents(ctx context.Context, since time.Time) (<-chan connector.Event, error) {
	return connector.Stream(ctx, 256, func(ctx context.Context, emit func(connector.Event) bool) error {
		return c.list(ctx, "api/v1/events", nil, func(raw json.RawMessage) error {
			var e struct {
				Metadata       meta      `json:"metadata"`
				Reason         string    `json:"reason"`
				Message        string    `json:"message"`
				Type           string    `json:"type"`
				LastTimestamp  time.Time `json:"lastTimestamp"`
				EventTime      time.Time `json:"eventTime"`
				Count          int       `json:"count"`
				InvolvedObject struct {
					Kind      string `json:"kind"`
					Name      string `json:"name"`
					Namespace string `json:"namespace"`
				} `json:"involvedObject"`
			}
			if err := json.Unmarshal(raw, &e); err != nil {
				return err
			}
			ts := e.LastTimestamp
			if ts.IsZero() {
				ts = e.EventTime
			}
			if ts.Before(since) {
				return nil
			}
			tracked := defaultReasons[e.Reason] || e.Type == "Warning"
			if c.reasons != nil {
				tracked = c.reasons[e.Reason]
			}
			if !tracked {
				return nil
			}
			kind := model.EventK8s
			if e.Reason == "SuccessfulRescale" {
				kind = model.EventHPAScale
			}
			ev := connector.Event{Kind: kind, TS: ts.UTC(), Title: e.Reason + " " + e.InvolvedObject.Kind + "/" + e.InvolvedObject.Name + ": " + truncate(e.Message, 200),
				Payload: map[string]any{"reason": e.Reason, "type": e.Type, "count": e.Count, "namespace": e.InvolvedObject.Namespace}}
			io := e.InvolvedObject
			switch io.Kind {
			case "Pod":
				ev.ResourceType, ev.ResourceExternalID = model.TypeK8sPod, c.podID(io.Namespace, io.Name)
			case "Node":
				ev.ResourceType, ev.ResourceExternalID = model.TypeK8sNode, io.Name
			case "Deployment", "StatefulSet", "DaemonSet":
				ev.ResourceType, ev.ResourceExternalID = model.TypeK8sWorkload, c.workloadID(io.Namespace, io.Kind, io.Name)
			case "HorizontalPodAutoscaler":
				// La cible de l'HPA porte généralement le même nom que le Deployment.
				ev.ResourceType, ev.ResourceExternalID = model.TypeK8sWorkload, c.workloadID(io.Namespace, "Deployment", io.Name)
			}
			if !emit(ev) {
				return context.Canceled
			}
			return nil
		})
	}), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
