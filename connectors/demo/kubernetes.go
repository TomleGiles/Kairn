package demo

import (
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"time"

	"github.com/kairn-io/kairn/pkg/connector"
	"github.com/kairn-io/kairn/pkg/model"
)

const clusterName = "prod-gra"

// k8sNodes renvoie les nodes vivants à t (portés par les VM vm-k8s-*).
func (w *World) k8sNodes(t time.Time) []vmSpec {
	var out []vmSpec
	for _, v := range w.vms {
		if v.k8sNode != "" && w.aliveVM(v, t) {
			out = append(out, v)
		}
	}
	return out
}

type podState struct {
	name, deployment, namespace, node, version string
	cpuReq, memReqGB, util                     float64
	kind                                       string
	created                                    time.Time
}

func shortHash(s string) string {
	h := fnv.New32a()
	h.Write([]byte(s))
	return fmt.Sprintf("%08x", h.Sum32())[:5]
}

// pods renvoie les pods en cours d'exécution à t.
func (w *World) pods(t time.Time) []podState {
	nodes := w.k8sNodes(t)
	if len(nodes) == 0 {
		return nil
	}
	var out []podState
	for _, d := range deployments {
		cpuReq, util, version := w.deploymentState(d, t)
		rs := shortHash(d.name + version)
		created := w.Start()
		if d.changeDay > 0 && !t.Before(w.day(d.changeDay)) {
			created = w.day(d.changeDay)
		}
		n := w.replicasAt(d, t)
		for i := 0; i < n; i++ {
			name := fmt.Sprintf("%s-%s-%d", d.name, rs, i)
			hc := t.Truncate(time.Hour)
			if i >= d.replicas { // réplica HPA : créé à l'heure courante
				created = hc
			}
			node := nodes[int(noise(w.Seed, "placement", name)*float64(len(nodes)))%len(nodes)]
			out = append(out, podState{
				name: name, deployment: d.name, namespace: d.namespace, node: node.k8sNode, version: version,
				cpuReq: cpuReq, memReqGB: d.memReqGB, util: util, kind: d.kind, created: created,
			})
		}
	}
	return out
}

func (w *World) kubernetesInventory(at time.Time) []connector.Resource {
	start := w.Start()
	out := []connector.Resource{{
		Type: model.TypeK8sCluster, ExternalID: clusterName, Name: clusterName, Region: "GRA",
		Attributes: map[string]any{"version": "1.31", "control_plane_tier": "free", "k8s.cluster": clusterName},
		Labels:     map[string]string{"env": "prod"}, CreatedAt: &start,
	}}
	inCluster := connector.Edge{Relation: model.RelContains, ParentType: model.TypeK8sCluster, ParentExternalID: clusterName}
	for _, v := range w.k8sNodes(at) {
		f := w.vmFlavor(v, at)
		created := w.day(v.createdDay)
		out = append(out, connector.Resource{
			Type: model.TypeK8sNode, ExternalID: v.k8sNode, Name: v.k8sNode, Region: "GRA",
			Attributes: map[string]any{
				"cpu_capacity_cores": f.vcpus, "mem_capacity_gb": f.ramGB, "instance_type": f.name,
				"provider_instance_id": v.id, "k8s.cluster": clusterName, "kubelet_version": "v1.31.4",
			},
			Labels:  map[string]string{"node.kubernetes.io/instance-type": f.name, "topology.kubernetes.io/zone": "gra-a"},
			Parents: []connector.Edge{inCluster}, CreatedAt: &created,
		})
	}
	for _, ns := range namespaces {
		labels := map[string]string{}
		if ns.team != "" {
			labels["team"] = ns.team
		}
		out = append(out, connector.Resource{
			Type: model.TypeK8sNamespace, ExternalID: clusterName + "/" + ns.name, Name: ns.name, Region: "GRA",
			Attributes: map[string]any{"k8s.namespace": ns.name, "k8s.cluster": clusterName}, Labels: labels,
			Parents: []connector.Edge{inCluster}, CreatedAt: &start,
		})
	}
	for _, d := range deployments {
		cpuReq, _, version := w.deploymentState(d, at)
		out = append(out, connector.Resource{
			Type: model.TypeK8sWorkload, ExternalID: clusterName + "/" + d.namespace + "/Deployment/" + d.name, Name: d.name, Region: "GRA",
			Attributes: map[string]any{
				"kind": "Deployment", "replicas": w.replicasAt(d, at), "k8s.namespace": d.namespace, "k8s.cluster": clusterName,
				"k8s.workload": d.name, "cpu_request_cores": cpuReq, "mem_request_gb": d.memReqGB, "image_tag": version,
				"hpa_max": d.hpaMax,
			},
			Labels:    map[string]string{"app.kubernetes.io/name": d.name},
			Parents:   []connector.Edge{{Relation: model.RelContains, ParentType: model.TypeK8sNamespace, ParentExternalID: clusterName + "/" + d.namespace}},
			CreatedAt: &start,
		})
	}
	for _, p := range w.pods(at) {
		created := p.created
		out = append(out, connector.Resource{
			Type: model.TypeK8sPod, ExternalID: clusterName + "/" + p.namespace + "/" + p.name, Name: p.name, Region: "GRA",
			Attributes: map[string]any{
				"cpu_request_cores": p.cpuReq, "mem_request_gb": p.memReqGB, "k8s.namespace": p.namespace,
				"k8s.cluster": clusterName, "k8s.workload": p.deployment, "node": p.node, "phase": "Running", "image_tag": p.version,
			},
			Labels: map[string]string{"app.kubernetes.io/name": p.deployment},
			Parents: []connector.Edge{
				{Relation: model.RelOwns, ParentType: model.TypeK8sWorkload, ParentExternalID: clusterName + "/" + p.namespace + "/Deployment/" + p.deployment},
				{Relation: model.RelContains, ParentType: model.TypeK8sNamespace, ParentExternalID: clusterName + "/" + p.namespace},
				{Relation: model.RelRunsOn, ParentType: model.TypeK8sNode, ParentExternalID: p.node},
			},
			CreatedAt: &created,
		})
	}
	return out
}

func (w *World) kubernetesMetrics(t time.Time) []connector.MetricPoint {
	var out []connector.MetricPoint
	for _, p := range w.pods(t) {
		ext := clusterName + "/" + p.namespace + "/" + p.name
		load := profile(t, p.kind)
		cpu := p.cpuReq * p.util * (0.6 + 0.8*load) * (0.9 + 0.2*noise(w.Seed, ext, t.Unix()/300))
		mem := p.memReqGB * (0.55 + 0.35*p.util) * (0.97 + 0.06*noise(w.Seed, ext, "m", t.Unix()/900))
		out = append(out,
			point(model.TypeK8sPod, ext, model.MetricCPUUsageCores, t, round4(cpu)),
			point(model.TypeK8sPod, ext, model.MetricMemUsageBytes, t, math.Round(mem*float64(1<<30))),
			point(model.TypeK8sPod, ext, model.MetricCPURequestCores, t, p.cpuReq),
			point(model.TypeK8sPod, ext, model.MetricMemRequestBytes, t, math.Round(p.memReqGB*float64(1<<30))),
		)
	}
	for _, d := range deployments {
		ext := clusterName + "/" + d.namespace + "/Deployment/" + d.name
		out = append(out, point(model.TypeK8sWorkload, ext, model.MetricReplicas, t, float64(w.replicasAt(d, t))))
		if d.name == "shop-api" {
			rps := 120 + 900*profile(t, "prod")*(0.9+0.2*noise(w.Seed, "rps", t.Unix()/600))
			out = append(out, point(model.TypeK8sWorkload, ext, model.MetricRequestsPerSec, t, round4(rps)))
		}
	}
	for _, v := range w.k8sNodes(t) {
		f := w.vmFlavor(v, t)
		out = append(out,
			point(model.TypeK8sNode, v.k8sNode, model.MetricCPUCapacityCores, t, float64(f.vcpus)),
			point(model.TypeK8sNode, v.k8sNode, model.MetricMemCapacityBytes, t, float64(f.ramGB)*float64(1<<30)),
		)
	}
	return out
}

// kubernetesEvents renvoie déploiements, mises à l'échelle HPA et ajouts de nodes sur [from, to).
func (w *World) kubernetesEvents(from, to time.Time) []connector.Event {
	var out []connector.Event
	if from.Before(w.Start()) {
		from = w.Start()
	}
	for day := from.Truncate(24 * time.Hour); day.Before(to); day = day.AddDate(0, 0, 1) {
		idx := w.dayIndex(day)
		for _, d := range deployments {
			ext := clusterName + "/" + d.namespace + "/Deployment/" + d.name
			if d.changeDay == idx {
				ts := day.Add(10*time.Hour + 12*time.Minute)
				out = append(out, connector.Event{Kind: model.EventDeployment, TS: ts,
					Title:        fmt.Sprintf("%s %s déployé (requests CPU %.1f → %.1f)", d.name, d.changeVersion, d.cpuReq, d.changeCPU),
					ResourceType: model.TypeK8sWorkload, ResourceExternalID: ext,
					Payload: map[string]any{"source": "argocd", "version": d.changeVersion, "previous_version": "v1.0.0",
						"author": "ci-bot", "commit": shortHash(d.name + d.changeVersion)}})
			} else if (d.name == "shop-api" || d.name == "shop-frontend") && idx%7 == 3 {
				ts := day.Add(9*time.Hour + 30*time.Minute)
				ver := fmt.Sprintf("v1.%d.0", idx/7)
				out = append(out, connector.Event{Kind: model.EventDeployment, TS: ts,
					Title:        fmt.Sprintf("%s %s déployé", d.name, ver),
					ResourceType: model.TypeK8sWorkload, ResourceExternalID: ext,
					Payload: map[string]any{"source": "gitlab", "version": ver, "author": "dev-team"}})
			}
			if d.hpaMax > 0 && day.Weekday() != time.Saturday && day.Weekday() != time.Sunday {
				peak := day.Add(12 * time.Hour)
				out = append(out, connector.Event{Kind: model.EventHPAScale, TS: peak,
					Title:        fmt.Sprintf("HPA %s : %d → %d réplicas", d.name, d.replicas, w.replicasAt(d, day.Add(14*time.Hour))),
					ResourceType: model.TypeK8sWorkload, ResourceExternalID: ext,
					Payload: map[string]any{"from": d.replicas, "to": w.replicasAt(d, day.Add(14*time.Hour))}})
			}
		}
		for _, v := range w.vms {
			if v.k8sNode != "" && v.createdDay == idx && idx > 0 {
				out = append(out, connector.Event{Kind: model.EventK8s, TS: day.Add(8 * time.Hour), Title: "Node " + v.k8sNode + " ajouté au cluster",
					ResourceType: model.TypeK8sNode, ResourceExternalID: v.k8sNode, Payload: map[string]any{"reason": "NodeReady"}})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].TS.Before(out[j].TS) })
	var filtered []connector.Event
	for _, e := range out {
		if !e.TS.Before(from) && e.TS.Before(to) {
			filtered = append(filtered, e)
		}
	}
	return filtered
}
