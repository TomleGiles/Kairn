package costengine

import (
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/model"
)

// component est un élément tarifé d'une ressource, exprimé en coût horaire
// dans la devise de l'organisation.
type component struct {
	sku      string
	costType string
	unit     string
	qty      decimal.Decimal // multiplicateur (Go, vCPU, 1…)
	hourly   decimal.Decimal // coût pour une heure d'existence
	source   string
	version  string // traçabilité : fournisseur@version ou onprem:<id>
}

// États de facturation normalisés posés par les connecteurs (attribut billing_state).
const (
	BillingRunning        = "running"
	BillingStoppedBilled  = "stopped_billed"
	BillingStoppedUnbiled = "stopped_unbilled"
)

// unitHourFactor renvoie le facteur appliqué au prix pour une heure.
// Les unités à l'usage (gb transféré, requête) ne sont pas estimables à partir de l'inventaire.
func unitHourFactor(unit string) (decimal.Decimal, bool) {
	switch unit {
	case model.UnitHour, model.UnitGBHour, model.UnitVCPUHour:
		return one, true
	case model.UnitMonth, model.UnitGBMonth:
		return one.Div(hoursPerMonth), true
	}
	return decimal.Zero, false
}

// priced cherche un SKU et construit le composant correspondant.
func (e *engine) priced(r model.Resource, sku, costType string, qty decimal.Decimal) (component, bool) {
	// La grille retenue est celle valide au début du jour calculé (déterminisme).
	m, ok := e.in.Book.Lookup(r.Provider, sku, r.Region, e.dayStart)
	if !ok {
		return component{}, false
	}
	factor, ok := unitHourFactor(m.Item.Unit)
	if !ok {
		e.warn("unit %q of %s/%s cannot be estimated from inventory", m.Item.Unit, r.Provider, sku)
		return component{}, false
	}
	price, ok := e.convert(m.Item.Price, m.Item.Currency)
	if !ok {
		return component{}, false
	}
	return component{
		sku:      sku,
		costType: costType,
		unit:     m.Item.Unit,
		qty:      qty,
		hourly:   price.Mul(qty).Mul(factor),
		source:   model.SourceEstimate,
		version:  m.Catalog.Provider + "@" + m.Catalog.Version,
	}, true
}

// onPremModel renvoie le modèle on-prem applicable à la ressource, s'il existe.
func (e *engine) onPremModel(r model.Resource) (model.OnPremCostModel, bool) {
	for _, m := range e.in.OnPrem {
		if m.ValidFrom.After(e.dayStart) || (m.ValidTo != nil && !m.ValidTo.After(e.dayStart)) {
			continue
		}
		if m.ConnectorID != nil && *m.ConnectorID == r.ConnectorID {
			return m, true
		}
		if len(m.Selector) > 0 {
			labels := e.effectiveLabels(r.ID)
			all := true
			for k, v := range m.Selector {
				if labels[k] != v {
					all = false
					break
				}
			}
			if all {
				return m, true
			}
		}
	}
	return model.OnPremCostModel{}, false
}

// OnPremRates calcule les coûts unitaires horaires d'un modèle on-prem :
// par vCPU-heure, par Go de RAM-heure et par Go de stockage-mois.
func OnPremRates(m model.OnPremCostModel) (vcpuHour, ramGBHour, storageGBMonth decimal.Decimal) {
	months := decimal.NewFromInt(int64(maxInt(m.AmortizationMonths, 1)))
	pue := m.PUE
	if pue.IsZero() {
		pue = one
	}
	monthly := m.HardwareCost.Div(months).
		Add(m.PowerKW.Mul(hoursPerMonth).Mul(m.PowerPricePerKWh).Mul(pue)).
		Add(m.LicensesMonthly).Add(m.LaborMonthly).Add(m.OtherMonthly)
	wsum := m.WeightCPU.Add(m.WeightRAM).Add(m.WeightStorage)
	if wsum.IsZero() {
		return decimal.Zero, decimal.Zero, decimal.Zero
	}
	share := func(w decimal.Decimal) decimal.Decimal { return monthly.Mul(w).Div(wsum) }
	if m.CapacityVCPU.IsPositive() {
		vcpuHour = share(m.WeightCPU).Div(m.CapacityVCPU).Div(hoursPerMonth)
	}
	if m.CapacityRAMGB.IsPositive() {
		ramGBHour = share(m.WeightRAM).Div(m.CapacityRAMGB).Div(hoursPerMonth)
	}
	if m.CapacityStorageGB.IsPositive() {
		storageGBMonth = share(m.WeightStorage).Div(m.CapacityStorageGB)
	}
	return vcpuHour, ramGBHour, storageGBMonth
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// onPremComponents tarifie une ressource à partir d'un modèle on-prem.
func (e *engine) onPremComponents(r model.Resource, m model.OnPremCostModel) []component {
	vcpuH, ramH, stoM := OnPremRates(m)
	conv := func(v decimal.Decimal) decimal.Decimal {
		c, _ := e.convert(v, m.Currency)
		return c
	}
	version := "onprem:" + m.ID
	var out []component
	switch r.Type {
	case model.TypeInstance, model.TypeHost, model.TypeK8sNode:
		vcpus, ram := computeShape(r)
		if vcpus.IsPositive() && vcpuH.IsPositive() {
			out = append(out, component{sku: "onprem.vcpu", costType: model.CostCompute, unit: model.UnitVCPUHour, qty: vcpus, hourly: conv(vcpuH).Mul(vcpus), source: model.SourceOnPrem, version: version})
		}
		if ram.IsPositive() && ramH.IsPositive() {
			out = append(out, component{sku: "onprem.ram_gb", costType: model.CostCompute, unit: model.UnitGBHour, qty: ram, hourly: conv(ramH).Mul(ram), source: model.SourceOnPrem, version: version})
		}
	case model.TypeVolume, model.TypeSnapshot:
		size := r.AttrDecimal("size_gb")
		if size.IsPositive() && stoM.IsPositive() {
			out = append(out, component{sku: "onprem.storage_gb", costType: model.CostStorage, unit: model.UnitGBMonth, qty: size, hourly: conv(stoM).Mul(size).Div(hoursPerMonth), source: model.SourceOnPrem, version: version})
		}
	}
	return out
}

// computeShape renvoie vCPU et Go de RAM d'une machine (VM, hôte, node).
func computeShape(r model.Resource) (vcpus, ramGB decimal.Decimal) {
	vcpus = r.AttrDecimal("vcpus")
	if vcpus.IsZero() {
		vcpus = r.AttrDecimal("cpu_capacity_cores")
	}
	ramGB = r.AttrDecimal("ram_gb")
	if ramGB.IsZero() {
		ramGB = r.AttrDecimal("mem_capacity_gb")
	}
	return vcpus, ramGB
}

// components renvoie les composants tarifés d'une version de ressource.
func (e *engine) components(r model.Resource) []component {
	if m, ok := e.onPremModel(r); ok {
		return e.onPremComponents(r, m)
	}
	var out []component
	add := func(c component, ok bool) bool {
		if ok {
			out = append(out, c)
		}
		return ok
	}
	switch r.Type {
	case model.TypeInstance, model.TypeK8sNode:
		switch strings.ToLower(r.Attr("billing_state")) {
		case BillingStoppedUnbiled:
			return nil
		}
		switch strings.ToLower(r.Attr("status")) {
		case "shelved", "shelved_offloaded", "deleted", "terminated":
			return nil
		}
		flavor := r.Attr("flavor")
		if flavor == "" {
			flavor = r.Attr("instance_type")
		}
		priced := flavor != "" && add(e.priced(r, "compute.flavor."+flavor, model.CostCompute, one))
		if !priced {
			vcpus, ram := computeShape(r)
			vcpuSKU := "compute.vcpu"
			if cls := r.Attr("cpu_class"); cls != "" {
				// Prix par vCore propre à une classe de CPU (ex. Outscale compute.vcpu.v6-p2).
				if _, ok := e.in.Book.Lookup(r.Provider, vcpuSKU+"."+cls, r.Region, e.dayStart); ok {
					vcpuSKU += "." + cls
				}
			}
			c1, ok1 := e.priced(r, vcpuSKU, model.CostCompute, vcpus)
			c2, ok2 := e.priced(r, "compute.ram_gb", model.CostCompute, ram)
			if ok1 && ok2 && vcpus.IsPositive() {
				out = append(out, c1, c2)
			} else if r.Type == model.TypeInstance {
				e.warn("no price for %s instance flavor %q", r.Provider, flavor)
			}
		}
		if lic := r.Attr("license"); lic != "" {
			vcpus, _ := computeShape(r)
			qty := one
			if m, ok := e.in.Book.Lookup(r.Provider, "license."+lic, r.Region, e.dayStart); ok && m.Item.Unit == model.UnitVCPUHour {
				qty = vcpus
			}
			if !add(e.priced(r, "license."+lic, model.CostLicense, qty)) {
				e.warn("no price for %s license %q", r.Provider, lic)
			}
		}
	case model.TypeVolume:
		size := r.AttrDecimal("size_gb")
		vt := r.Attr("volume_type")
		if !(vt != "" && add(e.priced(r, "storage.volume."+vt, model.CostStorage, size))) {
			if !add(e.priced(r, "storage.volume", model.CostStorage, size)) {
				e.warn("no price for %s volume type %q", r.Provider, vt)
			}
		}
	case model.TypeSnapshot:
		if !add(e.priced(r, "storage.snapshot", model.CostStorage, r.AttrDecimal("size_gb"))) {
			e.warn("no price for %s snapshots", r.Provider)
		}
	case model.TypeBucket:
		size := r.AttrDecimal("size_gb")
		if e.in.Metrics != nil {
			if v, ok := e.in.Metrics.DailyAvg(r.ID, model.MetricStorageBytes); ok {
				size = dec(v).Div(gb)
			}
		}
		class := r.Attr("storage_class")
		if class == "" {
			class = "standard"
		}
		if !add(e.priced(r, "storage.object."+class, model.CostStorage, size)) {
			e.warn("no price for %s object storage class %q", r.Provider, class)
		}
	case model.TypeIP:
		kind := r.Attr("ip_kind")
		if kind == "" {
			kind = "floating"
		}
		add(e.priced(r, "network.ip."+kind, model.CostNetwork, one))
	case model.TypeLoadBalancer:
		fl := r.Attr("flavor")
		if !(fl != "" && add(e.priced(r, "network.lb."+fl, model.CostNetwork, one))) {
			if !add(e.priced(r, "network.lb", model.CostNetwork, one)) {
				e.warn("no price for %s load balancer %q", r.Provider, fl)
			}
		}
	case model.TypeDatabase:
		sku := "database." + r.Attr("engine") + "." + r.Attr("flavor")
		if !add(e.priced(r, sku, model.CostCompute, one)) {
			e.warn("no price for %s database %q", r.Provider, sku)
		}
	case model.TypeK8sCluster:
		tier := r.Attr("control_plane_tier")
		if tier == "" {
			tier = "standard"
		}
		add(e.priced(r, "k8s.control_plane."+tier, model.CostCompute, one))
	}
	return out
}

// hourlyRate additionne les composants d'une version.
func hourlyRate(cs []component) decimal.Decimal {
	t := decimal.Zero
	for _, c := range cs {
		t = t.Add(c.hourly)
	}
	return t
}

// emit ajoute les lignes de composants pour une durée donnée.
func (e *engine) emit(r model.Resource, cs []component, hours decimal.Decimal) {
	for _, c := range cs {
		l := e.baseLine(r)
		l.CostType = c.costType
		l.SKU = c.sku
		l.Unit = c.unit
		qty := c.qty.Mul(hours)
		if f, _ := unitHourFactor(c.unit); !f.Equal(one) {
			qty = qty.Mul(f)
		}
		l.Quantity = qty
		l.Amount = c.hourly.Mul(hours)
		l.Source = c.source
		l.CatalogVersion = c.version
		e.lines = append(e.lines, l)
	}
}

// priceResources tarifie toutes les versions, hors nodes Kubernetes et VM
// adossées à un node (leur coût est réparti par allocateKubernetes).
func (e *engine) priceResources() {
	for _, id := range e.ids {
		for _, v := range e.versions[id] {
			if v.Type == model.TypeK8sNode || e.backedNode(id) != "" {
				continue
			}
			cs := e.components(v)
			if len(cs) == 0 {
				continue
			}
			e.emit(v, cs, hoursOf(v.Overlap(e.dayStart, e.dayEnd)))
		}
	}
}

// backedNode renvoie le node Kubernetes adossé à une VM, s'il existe ce jour.
func (e *engine) backedNode(vmID string) string {
	for _, ed := range e.children[vmID] {
		if ed.Relation == model.RelBacks {
			if _, ok := e.versions[ed.ChildID]; ok {
				return ed.ChildID
			}
		}
	}
	return ""
}

// hourBounds renvoie les bornes de l'heure h du jour.
func (e *engine) hourBounds(h int) (time.Time, time.Time) {
	s := e.dayStart.Add(time.Duration(h) * time.Hour)
	return s, s.Add(time.Hour)
}
