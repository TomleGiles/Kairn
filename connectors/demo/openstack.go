package demo

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/connector"
	"github.com/kairn-io/kairn/pkg/model"
)

// Types de connecteurs démo.
const (
	TypeOpenStack  = "demo-openstack"
	TypeKubernetes = "demo-kubernetes"
)

func init() {
	connector.Register(connector.TypeInfo{
		Type: TypeOpenStack, DisplayName: "Démo — cloud OpenStack (OVHcloud)", Category: connector.CategoryCloud, Provider: "openstack",
		Resources:       []string{model.TypeProject, model.TypeInstance, model.TypeVolume, model.TypeSnapshot, model.TypeIP, model.TypeLoadBalancer, model.TypeBucket},
		DefaultInterval: time.Hour, Metrics: true, Billing: true,
		Fields: []connector.Field{
			{Name: "epoch", Label: "Fin du monde simulé (RFC3339)", Help: "Par défaut : aujourd'hui à minuit UTC"},
			{Name: "seed", Label: "Graine", Default: "kairn"},
		},
		Permissions: []connector.Permission{{Scope: "aucune", Description: "Données simulées, aucun accès externe"}},
	}, func(cfg connector.Config) (connector.Connector, error) { return newBase(cfg, TypeOpenStack) })
	connector.Register(connector.TypeInfo{
		Type: TypeKubernetes, DisplayName: "Démo — cluster Kubernetes", Category: connector.CategoryKubernetes, Provider: "kubernetes",
		Resources:       []string{model.TypeK8sCluster, model.TypeK8sNode, model.TypeK8sNamespace, model.TypeK8sWorkload, model.TypeK8sPod},
		DefaultInterval: 15 * time.Minute, Metrics: true,
		Fields: []connector.Field{
			{Name: "epoch", Label: "Fin du monde simulé (RFC3339)"},
			{Name: "seed", Label: "Graine", Default: "kairn"},
		},
		Permissions: []connector.Permission{{Scope: "aucune", Description: "Données simulées, aucun accès externe"}},
	}, func(cfg connector.Config) (connector.Connector, error) { return newBase(cfg, TypeKubernetes) })
}

// base porte le monde et l'horloge communs aux deux connecteurs démo.
type base struct {
	typ   string
	world *World
	now   func() time.Time
}

func newBase(cfg connector.Config, typ string) (*base, error) {
	epoch := time.Now().UTC().Truncate(24 * time.Hour)
	if s := cfg.Setting("epoch", ""); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return nil, fmt.Errorf("demo: invalid epoch: %w", err)
		}
		epoch = t.UTC()
	}
	return &base{typ: typ, world: NewWorld(epoch, cfg.Setting("seed", "kairn")), now: func() time.Time { return time.Now().UTC() }}, nil
}

func (b *base) Type() string                                     { return b.typ }
func (b *base) Validate(context.Context, connector.Config) error { return nil }
func (b *base) RequiredPermissions() []connector.Permission {
	i, _ := connector.Info(b.typ)
	return i.Permissions
}
func (b *base) WorldStart() time.Time { return b.world.Start() }
func (b *base) Health(context.Context) connector.HealthStatus {
	return connector.HealthStatus{Status: connector.HealthOK, Message: "simulation", CheckedAt: b.now()}
}

func (b *base) BackfillStep() time.Duration {
	if b.typ == TypeKubernetes {
		return time.Hour
	}
	return 24 * time.Hour
}

func (b *base) SyncInventory(ctx context.Context, _ time.Time) (<-chan connector.Resource, error) {
	return b.SyncInventoryAt(ctx, b.now())
}

func (b *base) SyncInventoryAt(ctx context.Context, at time.Time) (<-chan connector.Resource, error) {
	var list []connector.Resource
	if b.typ == TypeOpenStack {
		list = b.world.openstackInventory(at)
	} else {
		list = b.world.kubernetesInventory(at)
	}
	return connector.Stream(ctx, 64, func(ctx context.Context, emit func(connector.Resource) bool) error {
		for _, r := range list {
			if !emit(r) {
				return nil
			}
		}
		return nil
	}), nil
}

func (b *base) SyncMetrics(ctx context.Context, w connector.TimeWindow) (<-chan connector.MetricPoint, error) {
	step := w.Step
	if step <= 0 {
		step = time.Hour
	}
	from := w.From.UTC().Truncate(step)
	return connector.Stream(ctx, 1024, func(ctx context.Context, emit func(connector.MetricPoint) bool) error {
		for t := from; t.Before(w.To); t = t.Add(step) {
			var pts []connector.MetricPoint
			if b.typ == TypeOpenStack {
				pts = b.world.openstackMetrics(t)
			} else {
				pts = b.world.kubernetesMetrics(t)
			}
			for _, p := range pts {
				if !emit(p) {
					return nil
				}
			}
		}
		return nil
	}), nil
}

func (b *base) SyncBilling(ctx context.Context, p connector.Period) (<-chan connector.CostLine, error) {
	if b.typ != TypeOpenStack {
		return nil, connector.ErrNotSupported
	}
	return connector.Stream(ctx, 256, func(ctx context.Context, emit func(connector.CostLine) bool) error {
		for d := p.From.UTC().Truncate(24 * time.Hour); d.Before(p.To) && d.Before(b.now().Truncate(24*time.Hour)); d = d.AddDate(0, 0, 1) {
			for _, l := range b.world.invoiceLines(d) {
				if !emit(l) {
					return nil
				}
			}
		}
		return nil
	}), nil
}

func (b *base) SyncEvents(ctx context.Context, since time.Time) (<-chan connector.Event, error) {
	if b.typ != TypeKubernetes {
		return connector.Stream(ctx, 1, func(context.Context, func(connector.Event) bool) error { return nil }), nil
	}
	evs := b.world.kubernetesEvents(since, b.now())
	return connector.Stream(ctx, 64, func(ctx context.Context, emit func(connector.Event) bool) error {
		for _, e := range evs {
			if !emit(e) {
				return nil
			}
		}
		return nil
	}), nil
}

var (
	_ connector.Connector           = (*base)(nil)
	_ connector.EventSource         = (*base)(nil)
	_ connector.HistoricalInventory = (*base)(nil)
)

// ------------------------------------------------------------------ inventaire OpenStack

func (w *World) projectOf(id string) project {
	for _, p := range projects {
		if p.id == id {
			return p
		}
	}
	return project{}
}

func (w *World) openstackInventory(at time.Time) []connector.Resource {
	var out []connector.Resource
	start := w.Start()
	for _, p := range projects {
		out = append(out, connector.Resource{
			Type: model.TypeProject, ExternalID: p.id, Name: p.name, Region: "GRA11",
			Attributes: map[string]any{"project_id": p.id, "env": p.env},
			Labels:     map[string]string{"team": p.team, "env": p.env}, CreatedAt: &start,
		})
	}
	inProject := func(pid string) connector.Edge {
		return connector.Edge{Relation: model.RelContains, ParentType: model.TypeProject, ParentExternalID: pid}
	}
	alive := map[string]bool{}
	for _, v := range w.vms {
		if !w.aliveVM(v, at) {
			continue
		}
		alive[v.id] = true
		f := w.vmFlavor(v, at)
		created := w.day(v.createdDay)
		p := w.projectOf(v.project)
		attrs := map[string]any{
			"flavor": f.name, "vcpus": f.vcpus, "ram_gb": f.ramGB, "status": "ACTIVE", "billing_state": "running",
			"project_id": v.project, "image": "Ubuntu 24.04", "env": p.env,
		}
		if v.license != "" {
			attrs["license"] = v.license
			attrs["image"] = "Windows Server 2025"
		}
		labels := map[string]string{"role": v.kind}
		if v.k8sNode != "" {
			labels["kubernetes-cluster"] = "prod-gra"
		}
		out = append(out, connector.Resource{
			Type: model.TypeInstance, ExternalID: v.id, Name: v.name, Region: "GRA11", Attributes: attrs, Labels: labels,
			Parents: []connector.Edge{inProject(v.project)}, CreatedAt: &created,
		})
	}
	for _, vol := range w.volumes {
		if at.Before(w.day(vol.createdDay)) {
			continue
		}
		created := w.day(vol.createdDay)
		attrs := map[string]any{"size_gb": vol.sizeGB, "volume_type": vol.vtype, "status": "available", "project_id": vol.project}
		parents := []connector.Edge{inProject(vol.project)}
		if vol.attachedTo != "" && alive[vol.attachedTo] {
			attrs["status"] = "in-use"
			attrs["attached_to"] = vol.attachedTo
			parents = append(parents, connector.Edge{Relation: model.RelAttachedTo, ParentType: model.TypeInstance, ParentExternalID: vol.attachedTo})
		}
		out = append(out, connector.Resource{Type: model.TypeVolume, ExternalID: vol.id, Name: vol.name, Region: "GRA11",
			Attributes: attrs, Labels: map[string]string{}, Parents: parents, CreatedAt: &created})
	}
	for _, s := range w.snapshots {
		if at.Before(w.day(s.createdDay)) {
			continue
		}
		created := w.day(s.createdDay)
		out = append(out, connector.Resource{Type: model.TypeSnapshot, ExternalID: s.id, Name: s.name, Region: "GRA11",
			Attributes: map[string]any{"size_gb": s.sizeGB, "created_at": created.Format(time.RFC3339), "project_id": s.project},
			Labels:     map[string]string{}, Parents: []connector.Edge{inProject(s.project)}, CreatedAt: &created})
	}
	for _, ip := range w.ips {
		attrs := map[string]any{"ip_kind": "floating", "project_id": ip.project, "status": "DOWN"}
		parents := []connector.Edge{inProject(ip.project)}
		if ip.attachedTo != "" && alive[ip.attachedTo] {
			attrs["status"] = "ACTIVE"
			attrs["attached_to"] = ip.attachedTo
			parents = append(parents, connector.Edge{Relation: model.RelAttachedTo, ParentType: model.TypeInstance, ParentExternalID: ip.attachedTo})
		}
		out = append(out, connector.Resource{Type: model.TypeIP, ExternalID: ip.id, Name: ip.id, Region: "GRA11",
			Attributes: attrs, Labels: map[string]string{}, Parents: parents, CreatedAt: &start})
	}
	for _, lb := range []struct{ id, name, project string }{{"lb-shop", "shop-public", "p-shop-prod"}, {"lb-k8s", "k8s-ingress", "p-platform"}} {
		out = append(out, connector.Resource{Type: model.TypeLoadBalancer, ExternalID: lb.id, Name: lb.name, Region: "GRA11",
			Attributes: map[string]any{"flavor": "small", "project_id": lb.project}, Labels: map[string]string{},
			Parents: []connector.Edge{inProject(lb.project)}, CreatedAt: &start})
	}
	for _, bk := range []struct{ id, project string }{{"shop-media", "p-shop-prod"}, {"datalake-raw", "p-data-lab"}} {
		out = append(out, connector.Resource{Type: model.TypeBucket, ExternalID: bk.id, Name: bk.id, Region: "GRA",
			Attributes: map[string]any{"storage_class": "standard", "project_id": bk.project}, Labels: map[string]string{},
			Parents: []connector.Edge{inProject(bk.project)}, CreatedAt: &start})
	}
	return out
}

// ------------------------------------------------------------------ métriques OpenStack

func point(typ, ext, metric string, t time.Time, v float64) connector.MetricPoint {
	return connector.MetricPoint{ResourceType: typ, ResourceExternalID: ext, Metric: metric, TS: t, Value: v}
}

func (w *World) openstackMetrics(t time.Time) []connector.MetricPoint {
	var out []connector.MetricPoint
	for _, v := range w.vms {
		if !w.aliveVM(v, t) {
			continue
		}
		f := w.vmFlavor(v, t)
		cpu := w.vmCPU(v, t)
		// Le passage en b2-30 réduit l'utilisation relative de la base.
		if v.resizedTo != "" && !t.Before(w.day(v.resizeDay)) {
			cpu = cpu * float64(flavors[v.flavor].vcpus) / float64(f.vcpus) * 1.3
		}
		mem := w.vmMem(v, t)
		out = append(out,
			point(model.TypeInstance, v.id, model.MetricCPUUtil, t, round4(cpu)),
			point(model.TypeInstance, v.id, model.MetricCPUUsageCores, t, round4(cpu*float64(f.vcpus))),
			point(model.TypeInstance, v.id, model.MetricMemUtil, t, round4(mem)),
			point(model.TypeInstance, v.id, model.MetricNetRxBytesPerSec, t, round4(2e6*profile(t, v.kind)*v.util)),
		)
	}
	for _, vol := range w.volumes {
		if t.Before(w.day(vol.createdDay)) {
			continue
		}
		iops := 0.0
		if vol.attachedTo != "" {
			iops = 40 + 400*profile(t, "prod")*noise(w.Seed, vol.id, t.Unix()/3600)
			if vol.id == "vol-stg-db" {
				iops = 3 + 5*noise(w.Seed, vol.id, t.Unix()/3600) // high-speed sous-utilisé
			}
		}
		out = append(out, point(model.TypeVolume, vol.id, model.MetricDiskIOPS, t, round4(iops)))
	}
	if t.Hour() == 0 {
		days := float64(w.dayIndex(t))
		out = append(out,
			point(model.TypeBucket, "shop-media", model.MetricStorageBytes, t, (800+days*2.5)*1e9),
			point(model.TypeBucket, "datalake-raw", model.MetricStorageBytes, t, (3000+days*25)*1e9),
		)
	}
	return out
}

func round4(v float64) float64 { return float64(int64(v*10000+0.5)) / 10000 }

// ------------------------------------------------------------------ facturation

// Prix unitaires utilisés par la facture simulée (cohérents avec la grille d'exemple).
var invoicePrices = map[string]string{
	"b2-7": "0.0681", "b2-15": "0.1331", "b2-30": "0.2651", "b2-60": "0.5271", "c2-30": "0.4171", "d2-2": "0.0099",
	"classic": "0.04", "high-speed": "0.08", "snapshot": "0.01", "ip": "0.0025", "lb": "0.0139", "object": "0.0112", "windows": "0.0125",
}

func (w *World) invoiceLines(day time.Time) []connector.CostLine {
	var out []connector.CostLine
	hoursAlive := func(from time.Time, to time.Time) decimal.Decimal {
		s, e := day, day.AddDate(0, 0, 1)
		if from.After(s) {
			s = from
		}
		if !to.IsZero() && to.Before(e) {
			e = to
		}
		if !e.After(s) {
			return decimal.Zero
		}
		return decimal.NewFromInt(int64(e.Sub(s) / time.Second)).Div(decimal.NewFromInt(3600))
	}
	// Écart de facturation simulé : ±1,5 % (arrondis, remises ponctuelles).
	jitter := func(key string) decimal.Decimal {
		return decimal.NewFromFloat(0.985 + 0.03*noise(w.Seed, "invoice", key, day.Unix())).Round(4)
	}
	invoice := "OVH-" + day.Format("200601")
	for _, v := range w.vms {
		var deleted time.Time
		if v.deletedDay > 0 {
			deleted = w.day(v.deletedDay)
		}
		// Découpage au redimensionnement.
		segments := []struct {
			from, to time.Time
			fl       string
		}{{w.day(v.createdDay), deleted, v.flavor}}
		if v.resizedTo != "" {
			segments = []struct {
				from, to time.Time
				fl       string
			}{{w.day(v.createdDay), w.day(v.resizeDay), v.flavor}, {w.day(v.resizeDay), deleted, v.resizedTo}}
		}
		for _, seg := range segments {
			h := hoursAlive(seg.from, seg.to)
			if h.IsZero() {
				continue
			}
			amount := h.Mul(decimal.RequireFromString(invoicePrices[seg.fl])).Mul(jitter(v.id))
			out = append(out, connector.CostLine{ResourceType: model.TypeInstance, ResourceExternalID: v.id, Day: day,
				Service: "instance", SKU: seg.fl, CostType: model.CostCompute, Quantity: h, Unit: model.UnitHour,
				Amount: amount.Round(6), Currency: "EUR", InvoiceID: invoice})
			if v.license != "" {
				lic := h.Mul(decimal.NewFromInt(int64(flavors[seg.fl].vcpus))).Mul(decimal.RequireFromString(invoicePrices[v.license]))
				out = append(out, connector.CostLine{ResourceType: model.TypeInstance, ResourceExternalID: v.id, Day: day,
					Service: "license", SKU: v.license, CostType: model.CostLicense, Quantity: h, Unit: model.UnitHour,
					Amount: lic.Round(6), Currency: "EUR", InvoiceID: invoice})
			}
		}
	}
	gbMonth := func(size int, h decimal.Decimal, price string) decimal.Decimal {
		return decimal.NewFromInt(int64(size)).Mul(h).Div(decimal.NewFromInt(730)).Mul(decimal.RequireFromString(price))
	}
	for _, vol := range w.volumes {
		h := hoursAlive(w.day(vol.createdDay), time.Time{})
		if h.IsZero() {
			continue
		}
		out = append(out, connector.CostLine{ResourceType: model.TypeVolume, ResourceExternalID: vol.id, Day: day,
			Service: "volume", SKU: vol.vtype, CostType: model.CostStorage, Quantity: h, Unit: model.UnitHour,
			Amount: gbMonth(vol.sizeGB, h, invoicePrices[vol.vtype]).Mul(jitter(vol.id)).Round(6), Currency: "EUR", InvoiceID: invoice})
	}
	for _, s := range w.snapshots {
		h := hoursAlive(w.day(s.createdDay), time.Time{})
		if h.IsZero() {
			continue
		}
		out = append(out, connector.CostLine{ResourceType: model.TypeSnapshot, ResourceExternalID: s.id, Day: day,
			Service: "snapshot", SKU: "snapshot", CostType: model.CostStorage, Quantity: h, Unit: model.UnitHour,
			Amount: gbMonth(s.sizeGB, h, invoicePrices["snapshot"]).Round(6), Currency: "EUR", InvoiceID: invoice})
	}
	for _, ip := range w.ips {
		out = append(out, connector.CostLine{ResourceType: model.TypeIP, ResourceExternalID: ip.id, Day: day,
			Service: "ip", SKU: "floating-ip", CostType: model.CostNetwork, Quantity: decimal.NewFromInt(24), Unit: model.UnitHour,
			Amount: decimal.NewFromInt(24).Mul(decimal.RequireFromString(invoicePrices["ip"])), Currency: "EUR", InvoiceID: invoice})
	}
	days := int64(w.dayIndex(day))
	for _, bk := range []struct {
		id        string
		base, inc int64
	}{{"shop-media", 800, 25}, {"datalake-raw", 3000, 250}} {
		// Taille moyenne du jour (croissance linéaire, cf. openstackMetrics), en Go.
		size := decimal.NewFromInt(bk.base*10 + days*bk.inc).Div(decimal.NewFromInt(10))
		amount := size.Mul(decimal.NewFromInt(24)).Div(decimal.NewFromInt(730)).Mul(decimal.RequireFromString(invoicePrices["object"]))
		out = append(out, connector.CostLine{ResourceType: model.TypeBucket, ResourceExternalID: bk.id, Day: day,
			Service: "object-storage", SKU: "standard", CostType: model.CostStorage, Quantity: size, Unit: model.UnitGBMonth,
			Amount: amount.Round(6), Currency: "EUR", InvoiceID: invoice})
	}
	for _, lb := range []string{"lb-shop", "lb-k8s"} {
		out = append(out, connector.CostLine{ResourceType: model.TypeLoadBalancer, ResourceExternalID: lb, Day: day,
			Service: "loadbalancer", SKU: "small", CostType: model.CostNetwork, Quantity: decimal.NewFromInt(24), Unit: model.UnitHour,
			Amount: decimal.NewFromInt(24).Mul(decimal.RequireFromString(invoicePrices["lb"])), Currency: "EUR", InvoiceID: invoice})
	}
	return out
}
