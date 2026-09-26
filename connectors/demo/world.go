// Package demo fournit deux connecteurs synthétiques et déterministes
// (demo-openstack et demo-kubernetes) décrivant la même infrastructure :
// un projet e-commerce hébergé sur un cloud OpenStack de type OVHcloud et un
// cluster Kubernetes dont les nodes sont des VM de ce cloud.
//
// Le monde contient volontairement des situations à détecter : VM
// surdimensionnée, environnements hors production allumés la nuit, volumes et
// IP orphelins, snapshots anciens, pic de coût après un déploiement.
package demo

import (
	"fmt"
	"hash/fnv"
	"math"
	"time"
)

// noise renvoie une valeur pseudo-aléatoire déterministe dans [0, 1).
func noise(parts ...any) float64 {
	h := fnv.New64a()
	for _, p := range parts {
		fmt.Fprint(h, p, "|")
	}
	return float64(h.Sum64()%1_000_000) / 1_000_000
}

// profile renvoie un facteur d'activité [0..1] selon l'heure et le jour.
func profile(t time.Time, kind string) float64 {
	t = t.In(paris)
	hour := float64(t.Hour()) + float64(t.Minute())/60
	daily := 0.35 + 0.65*math.Exp(-math.Pow((hour-14)/4.5, 2)) // pic à 14 h
	weekend := t.Weekday() == time.Saturday || t.Weekday() == time.Sunday
	switch kind {
	case "prod":
		if weekend {
			daily *= 0.7
		}
		return daily
	case "staging", "dev":
		if weekend || hour < 8 || hour > 19 {
			return 0.04
		}
		return 0.2 + 0.5*daily
	case "batch":
		if hour >= 1 && hour < 5 {
			return 0.9
		}
		return 0.08
	default:
		return 0.5
	}
}

var paris = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		return time.FixedZone("CET", 3600)
	}
	return loc
}()

// flavor décrit une gamme de VM.
type flavor struct {
	name  string
	vcpus int
	ramGB int
}

var flavors = map[string]flavor{
	"d2-2":  {"d2-2", 1, 2},
	"b2-7":  {"b2-7", 2, 7},
	"b2-15": {"b2-15", 4, 15},
	"b2-30": {"b2-30", 8, 30},
	"b2-60": {"b2-60", 16, 60},
	"c2-30": {"c2-30", 16, 30},
}

// vmSpec décrit une VM du monde.
type vmSpec struct {
	id, name, project string
	flavor            string
	resizedTo         string // nouvelle gamme à partir de ResizeDay
	resizeDay         int
	createdDay        int     // jour de création (0 = début du monde)
	deletedDay        int     // 0 = jamais
	util              float64 // utilisation CPU moyenne en pointe
	kind              string  // prod | staging | dev | batch
	license           string
	k8sNode           string // nom du node Kubernetes porté
}

// project décrit un projet OpenStack.
type project struct {
	id, name, team, env string
}

var projects = []project{
	{"p-shop-prod", "shop-prod", "shop", "prod"},
	{"p-shop-staging", "shop-staging", "shop", "staging"},
	{"p-data-lab", "data-lab", "data", "dev"},
	{"p-platform", "platform", "platform", "prod"},
}

// Le monde démarre 120 jours avant l'époque de référence (voir World.Epoch).
const worldDays = 120

func buildVMs() []vmSpec {
	vms := []vmSpec{
		{id: "vm-web-1", name: "shop-web-1", project: "p-shop-prod", flavor: "b2-7", util: 0.55, kind: "prod"},
		{id: "vm-web-2", name: "shop-web-2", project: "p-shop-prod", flavor: "b2-7", util: 0.52, kind: "prod"},
		{id: "vm-web-3", name: "shop-web-3", project: "p-shop-prod", flavor: "b2-7", util: 0.5, kind: "prod", createdDay: 60},
		{id: "vm-api-1", name: "shop-api-1", project: "p-shop-prod", flavor: "b2-15", util: 0.6, kind: "prod"},
		{id: "vm-api-2", name: "shop-api-2", project: "p-shop-prod", flavor: "b2-15", util: 0.58, kind: "prod"},
		{id: "vm-db-1", name: "shop-db-1", project: "p-shop-prod", flavor: "b2-15", resizedTo: "b2-30", resizeDay: 80, util: 0.45, kind: "prod"},
		{id: "vm-cache-1", name: "shop-cache-1", project: "p-shop-prod", flavor: "b2-7", util: 0.3, kind: "prod"},
		{id: "vm-win-ad", name: "corp-ad-1", project: "p-shop-prod", flavor: "b2-7", util: 0.12, kind: "prod", license: "windows"},
		{id: "vm-stg-web", name: "stg-web-1", project: "p-shop-staging", flavor: "b2-7", util: 0.35, kind: "staging"},
		{id: "vm-stg-api", name: "stg-api-1", project: "p-shop-staging", flavor: "b2-15", util: 0.3, kind: "staging"},
		{id: "vm-stg-db", name: "stg-db-1", project: "p-shop-staging", flavor: "b2-15", util: 0.2, kind: "staging"},
		{id: "vm-jupyter", name: "jupyter-hub", project: "p-data-lab", flavor: "b2-60", util: 0.06, kind: "dev"},
		{id: "vm-spark-1", name: "spark-worker-1", project: "p-data-lab", flavor: "c2-30", util: 0.85, kind: "batch", createdDay: 100},
		{id: "vm-spark-2", name: "spark-worker-2", project: "p-data-lab", flavor: "c2-30", util: 0.85, kind: "batch", createdDay: 100},
		{id: "vm-spark-3", name: "spark-worker-3", project: "p-data-lab", flavor: "c2-30", util: 0.85, kind: "batch", createdDay: 100},
		{id: "vm-old-etl", name: "etl-legacy", project: "p-data-lab", flavor: "b2-15", util: 0.4, kind: "batch", deletedDay: 45},
		{id: "vm-bastion", name: "bastion", project: "p-platform", flavor: "d2-2", util: 0.03, kind: "prod"},
		{id: "vm-runner", name: "gitlab-runner", project: "p-platform", flavor: "b2-15", util: 0.25, kind: "prod"},
	}
	for i := 1; i <= 6; i++ {
		created := 0
		if i > 4 {
			created = 70 // le cluster passe de 4 à 6 nodes
		}
		vms = append(vms, vmSpec{
			id: fmt.Sprintf("vm-k8s-%d", i), name: fmt.Sprintf("k8s-prod-gra-node-%d", i), project: "p-platform",
			flavor: "b2-15", util: 0.5, kind: "prod", createdDay: created, k8sNode: fmt.Sprintf("prod-gra-node-%d", i),
		})
	}
	return vms
}

type volumeSpec struct {
	id, name, project, vtype, attachedTo string
	sizeGB                               int
	createdDay                           int
}

func buildVolumes() []volumeSpec {
	return []volumeSpec{
		{id: "vol-db-data", name: "shop-db-data", project: "p-shop-prod", vtype: "high-speed", attachedTo: "vm-db-1", sizeGB: 500},
		{id: "vol-backup", name: "shop-backups", project: "p-shop-prod", vtype: "classic", attachedTo: "vm-db-1", sizeGB: 1000},
		{id: "vol-stg-db", name: "stg-db-data", project: "p-shop-staging", vtype: "high-speed", attachedTo: "vm-stg-db", sizeGB: 200},
		{id: "vol-jupyter", name: "jupyter-data", project: "p-data-lab", vtype: "classic", attachedTo: "vm-jupyter", sizeGB: 300},
		{id: "vol-orphan-1", name: "old-etl-scratch", project: "p-data-lab", vtype: "classic", sizeGB: 250, createdDay: 5},
		{id: "vol-orphan-2", name: "migration-tmp", project: "p-shop-prod", vtype: "high-speed", sizeGB: 400, createdDay: 12},
		{id: "vol-orphan-3", name: "test-restore", project: "p-shop-staging", vtype: "classic", sizeGB: 150, createdDay: 30},
		{id: "vol-runner-cache", name: "runner-cache", project: "p-platform", vtype: "classic", attachedTo: "vm-runner", sizeGB: 100},
	}
}

type snapshotSpec struct {
	id, name, project  string
	sizeGB, createdDay int
}

func buildSnapshots() []snapshotSpec {
	return []snapshotSpec{
		{"snap-db-2026a", "db-before-upgrade", "p-shop-prod", 500, 1},
		{"snap-db-2026b", "db-weekly", "p-shop-prod", 500, 90},
		{"snap-etl", "etl-final", "p-data-lab", 150, 10},
		{"snap-stg", "stg-seed", "p-shop-staging", 200, 3},
	}
}

type ipSpec struct {
	id, project, attachedTo string
}

func buildIPs() []ipSpec {
	return []ipSpec{
		{"fip-web", "p-shop-prod", "vm-web-1"},
		{"fip-api", "p-shop-prod", "vm-api-1"},
		{"fip-bastion", "p-platform", "vm-bastion"},
		{"fip-stg", "p-shop-staging", "vm-stg-web"},
		{"fip-unused", "p-shop-prod", ""},
	}
}

// deploymentSpec décrit un workload Kubernetes.
type deploymentSpec struct {
	name, namespace  string
	replicas         int
	cpuReq, memReqGB float64
	util             float64 // usage / requests
	kind             string
	hpaMax           int
	// changeDay : déploiement qui modifie requests et usage (ex. nouvelle version gourmande).
	changeDay            int
	changeCPU, changeUtl float64
	changeVersion        string
}

type namespaceSpec struct {
	name, team string
}

var namespaces = []namespaceSpec{
	{"shop", "shop"}, {"search", "search"}, {"data-pipelines", "data"},
	{"kube-system", ""}, {"monitoring", ""}, {"ingress-nginx", ""},
}

var deployments = []deploymentSpec{
	{name: "shop-frontend", namespace: "shop", replicas: 3, cpuReq: 0.5, memReqGB: 1, util: 0.7, kind: "prod", hpaMax: 6},
	{name: "shop-api", namespace: "shop", replicas: 3, cpuReq: 1, memReqGB: 2, util: 0.65, kind: "prod", hpaMax: 8},
	{name: "checkout", namespace: "shop", replicas: 2, cpuReq: 0.5, memReqGB: 1, util: 0.5, kind: "prod"},
	{name: "search-api", namespace: "search", replicas: 2, cpuReq: 1, memReqGB: 4, util: 0.55, kind: "prod",
		changeDay: 108, changeCPU: 2, changeUtl: 0.9, changeVersion: "v2.3.0"},
	{name: "indexer", namespace: "search", replicas: 1, cpuReq: 2, memReqGB: 6, util: 0.25, kind: "prod"},
	{name: "airflow-worker", namespace: "data-pipelines", replicas: 2, cpuReq: 2, memReqGB: 4, util: 0.2, kind: "batch"},
	{name: "coredns", namespace: "kube-system", replicas: 2, cpuReq: 0.1, memReqGB: 0.07, util: 0.3, kind: "prod"},
	{name: "prometheus", namespace: "monitoring", replicas: 1, cpuReq: 1, memReqGB: 4, util: 0.8, kind: "prod"},
	{name: "grafana", namespace: "monitoring", replicas: 1, cpuReq: 0.2, memReqGB: 0.5, util: 0.3, kind: "prod"},
	{name: "ingress-nginx-controller", namespace: "ingress-nginx", replicas: 2, cpuReq: 0.5, memReqGB: 0.5, util: 0.6, kind: "prod"},
}

// World est l'état du monde synthétique.
type World struct {
	// Epoch est la fin du monde (généralement « maintenant » tronqué au jour).
	Epoch     time.Time
	Seed      string
	vms       []vmSpec
	volumes   []volumeSpec
	snapshots []snapshotSpec
	ips       []ipSpec
}

// NewWorld construit le monde. Le début est Epoch - 120 jours.
func NewWorld(epoch time.Time, seed string) *World {
	return &World{
		Epoch: epoch.UTC(), Seed: seed,
		vms: buildVMs(), volumes: buildVolumes(), snapshots: buildSnapshots(), ips: buildIPs(),
	}
}

// Start renvoie le début du monde.
func (w *World) Start() time.Time { return w.Epoch.AddDate(0, 0, -worldDays) }

func (w *World) day(n int) time.Time { return w.Start().AddDate(0, 0, n) }

// dayIndex renvoie le numéro de jour de t depuis le début du monde.
func (w *World) dayIndex(t time.Time) int {
	return int(t.Sub(w.Start()) / (24 * time.Hour))
}

func (w *World) aliveVM(v vmSpec, t time.Time) bool {
	if t.Before(w.day(v.createdDay)) {
		return false
	}
	return v.deletedDay == 0 || t.Before(w.day(v.deletedDay))
}

func (w *World) vmFlavor(v vmSpec, t time.Time) flavor {
	if v.resizedTo != "" && !t.Before(w.day(v.resizeDay)) {
		return flavors[v.resizedTo]
	}
	return flavors[v.flavor]
}

// vmCPU renvoie l'utilisation CPU (0..1) d'une VM à l'instant t.
func (w *World) vmCPU(v vmSpec, t time.Time) float64 {
	u := v.util * profile(t, v.kind)
	u *= 0.9 + 0.2*noise(w.Seed, v.id, t.Unix()/300)
	return math.Min(1, math.Max(0.005, u))
}

// vmMem renvoie l'utilisation mémoire (0..1) d'une VM.
func (w *World) vmMem(v vmSpec, t time.Time) float64 {
	base := 0.35 + 0.4*v.util
	if v.id == "vm-jupyter" {
		base = 0.12
	}
	return math.Min(0.97, base*(0.95+0.1*noise(w.Seed, v.id, "mem", t.Unix()/900)))
}

// deploymentState renvoie requests CPU, ratio d'usage et version d'un workload à t.
func (w *World) deploymentState(d deploymentSpec, t time.Time) (cpuReq, util float64, version string) {
	cpuReq, util, version = d.cpuReq, d.util, "v1.0.0"
	if d.changeDay > 0 && !t.Before(w.day(d.changeDay)) {
		cpuReq, util, version = d.changeCPU, d.changeUtl, d.changeVersion
	}
	return
}

// replicasAt renvoie le nombre de réplicas (HPA) à t.
func (w *World) replicasAt(d deploymentSpec, t time.Time) int {
	if d.hpaMax == 0 {
		return d.replicas
	}
	p := profile(t, d.kind)
	extra := int(math.Round(float64(d.hpaMax-d.replicas) * math.Max(0, (p-0.6)/0.4)))
	return d.replicas + extra
}
