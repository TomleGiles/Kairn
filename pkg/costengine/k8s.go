package costengine

import (
	"math"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/model"
)

// Méthodes de répartition du coût des nodes Kubernetes.
const (
	K8sMethodMax      = "max"      // max(requests, usage) — défaut
	K8sMethodRequests = "requests" // requests uniquement
	K8sMethodUsage    = "usage"    // usage uniquement
)

// Modes de traitement du coût idle.
const (
	IdleKeep       = "keep"       // ligne idle par node
	IdleDistribute = "distribute" // réparti sur les pods du node au prorata
)

// Labels techniques posés sur les lignes Kubernetes.
const (
	LabelK8sNodeID = "kairn.node_id"
	LabelBackingVM = "kairn.vm_id"
	// LabelUnderlyingSKU porte les SKU du node, pour que remises et
	// engagements s'appliquent aussi au coût réparti sur les pods.
	LabelUnderlyingSKU = "kairn.sku"
)

// allocateKubernetes répartit, heure par heure, le coût de chaque node entre
// ses pods selon max(requests, usage) (méthode configurable). La part non
// attribuée devient le coût idle du node. La somme est conservée.
func (e *engine) allocateKubernetes() {
	method := e.in.Settings.K8sAllocationMethod
	if method == "" {
		method = K8sMethodMax
	}
	for _, nodeID := range e.ids {
		node, _ := e.latest(nodeID)
		if node.Type != model.TypeK8sNode {
			continue
		}
		srcID := nodeID
		for _, ed := range e.parents[nodeID] {
			if ed.Relation == model.RelBacks {
				if _, ok := e.versions[ed.ParentID]; ok {
					srcID = ed.ParentID
				}
			}
		}
		srcLatest, _ := e.latest(srcID)

		// Coût horaire du node (ou de la VM qui le porte).
		var hourly [24]decimal.Decimal
		source, version := "", ""
		var skus []string
		estimated := decimal.Zero
		for _, v := range e.versions[srcID] {
			cs := e.components(v)
			if len(cs) == 0 {
				continue
			}
			if source == "" {
				source, version = cs[0].source, cs[0].version
			}
			for _, c := range cs {
				if !containsStr(skus, c.sku) {
					skus = append(skus, c.sku)
				}
			}
			rate := hourlyRate(cs)
			for h := 0; h < 24; h++ {
				hs, he := e.hourBounds(h)
				c := rate.Mul(hoursOf(v.Overlap(hs, he)))
				hourly[h] = hourly[h].Add(c)
				estimated = estimated.Add(c)
			}
		}
		if estimated.IsZero() {
			if len(e.children[nodeID]) > 0 {
				e.warn("kubernetes node %s has no price (flavor or on-prem model missing)", node.Name)
			}
			continue
		}
		// Préférence facture : le coût facturé de la VM est réparti au même prorata horaire.
		if e.in.Settings.PreferInvoice {
			if inv, ref, ok := e.invoiceTotal(srcID); ok {
				scale := inv.Div(estimated)
				for h := range hourly {
					hourly[h] = hourly[h].Mul(scale)
				}
				source, version = model.SourceInvoice, ref
			}
		}

		capCPU, capMem := computeShape(node)
		if capCPU.IsZero() || capMem.IsZero() {
			vc, vm := computeShape(srcLatest)
			if capCPU.IsZero() {
				capCPU = vc
			}
			if capMem.IsZero() {
				capMem = vm
			}
		}
		cpuW := decimal.Zero
		if den := capCPU.Mul(e.in.CPURAMRatio).Add(capMem); den.IsPositive() {
			cpuW = capCPU.Mul(e.in.CPURAMRatio).Div(den)
		}
		memW := one.Sub(cpuW)

		pods := e.podsOn(nodeID)
		podAmount := map[string]decimal.Decimal{}
		podHours := map[string]decimal.Decimal{}
		idle := decimal.Zero
		for h := 0; h < 24; h++ {
			ch := hourly[h]
			if ch.IsZero() {
				continue
			}
			type alloc struct{ cpu, mem, frac decimal.Decimal }
			allocs := map[string]alloc{}
			sumCPU, sumMem := decimal.Zero, decimal.Zero
			for _, pod := range pods {
				frac := e.podFraction(pod, nodeID, h)
				if frac.IsZero() {
					continue
				}
				cpu, mem := e.podAlloc(pod, h, method)
				cpu, mem = cpu.Mul(frac), mem.Mul(frac)
				allocs[pod] = alloc{cpu, mem, frac}
				sumCPU, sumMem = sumCPU.Add(cpu), sumMem.Add(mem)
			}
			effCPU := decimal.Max(capCPU, sumCPU)
			effMem := decimal.Max(capMem, sumMem)
			spent := decimal.Zero
			for _, pod := range pods {
				a, ok := allocs[pod]
				if !ok {
					continue
				}
				share := decimal.Zero
				if effCPU.IsPositive() {
					share = share.Add(cpuW.Mul(a.cpu).Div(effCPU))
				}
				if effMem.IsPositive() {
					share = share.Add(memW.Mul(a.mem).Div(effMem))
				}
				c := ch.Mul(share)
				podAmount[pod] = podAmount[pod].Add(c)
				podHours[pod] = podHours[pod].Add(a.frac)
				spent = spent.Add(c)
			}
			idle = idle.Add(ch.Sub(spent))
		}

		for _, pod := range pods {
			amt, ok := podAmount[pod]
			if !ok {
				continue
			}
			pv, _ := e.latest(pod)
			l := e.baseLine(pv)
			// Le coût provient du connecteur qui facture la machine (ex. le cloud qui porte le node).
			l.Provider, l.ConnectorID = srcLatest.Provider, srcLatest.ConnectorID
			l.CostType = model.CostK8sWork
			l.SKU = "k8s.allocation"
			l.Unit = model.UnitHour
			l.Quantity = podHours[pod]
			l.Amount = amt
			l.Source = source
			l.CatalogVersion = version
			l.Labels = withLabels(l.Labels, LabelK8sNodeID, nodeID, LabelBackingVM, backing(srcID, nodeID), LabelUnderlyingSKU, strings.Join(skus, ","))
			e.lines = append(e.lines, l)
		}
		l := e.baseLine(node)
		l.Provider, l.ConnectorID = srcLatest.Provider, srcLatest.ConnectorID
		l.CostType = model.CostK8sIdle
		l.SKU = "k8s.idle"
		l.Unit = model.UnitHour
		l.Quantity = hoursOf(node.Overlap(e.dayStart, e.dayEnd))
		l.Amount = idle
		l.Source = source
		l.CatalogVersion = version
		l.Labels = withLabels(l.Labels, LabelK8sNodeID, nodeID, LabelBackingVM, backing(srcID, nodeID), LabelUnderlyingSKU, strings.Join(skus, ","))
		e.lines = append(e.lines, l)
	}
}

func containsStr(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func backing(srcID, nodeID string) string {
	if srcID == nodeID {
		return ""
	}
	return srcID
}

// withLabels renvoie une copie des labels complétée des paires clé/valeur non vides.
func withLabels(base map[string]string, kv ...string) map[string]string {
	out := make(map[string]string, len(base)+len(kv)/2)
	for k, v := range base {
		out[k] = v
	}
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] != "" {
			out[kv[i]] = kv[i+1]
		}
	}
	return out
}

// podsOn liste les pods ayant tourné sur le node dans la journée.
func (e *engine) podsOn(nodeID string) []string {
	seen := map[string]bool{}
	var out []string
	for _, ed := range e.children[nodeID] {
		if ed.Relation != model.RelRunsOn || seen[ed.ChildID] {
			continue
		}
		if _, ok := e.versions[ed.ChildID]; !ok {
			continue
		}
		seen[ed.ChildID] = true
		out = append(out, ed.ChildID)
	}
	return out
}

// podFraction renvoie la fraction de l'heure h pendant laquelle le pod tourne sur le node.
func (e *engine) podFraction(pod, nodeID string, h int) decimal.Decimal {
	hs, he := e.hourBounds(h)
	alive := decimal.Zero
	for _, v := range e.versions[pod] {
		alive = alive.Add(hoursOf(v.Overlap(hs, he)))
	}
	if alive.IsZero() {
		return alive
	}
	onNode := decimal.Zero
	for _, ed := range e.children[nodeID] {
		if ed.ChildID != pod || ed.Relation != model.RelRunsOn {
			continue
		}
		s, en := ed.ValidFrom, he
		if ed.ValidTo != nil && ed.ValidTo.Before(en) {
			en = *ed.ValidTo
		}
		if hs.After(s) {
			s = hs
		}
		if en.After(s) {
			onNode = onNode.Add(hoursOf(en.Sub(s)))
		}
	}
	return decimal.Min(alive, onNode)
}

// podAlloc renvoie les cœurs CPU et Go de RAM attribués au pod sur l'heure h.
func (e *engine) podAlloc(pod string, h int, method string) (cpu, mem decimal.Decimal) {
	hs, _ := e.hourBounds(h)
	var reqCPU, reqMem decimal.Decimal
	for _, v := range e.versions[pod] {
		if v.Alive(hs) || (reqCPU.IsZero() && reqMem.IsZero()) {
			reqCPU, reqMem = v.AttrDecimal("cpu_request_cores"), v.AttrDecimal("mem_request_gb")
		}
	}
	if e.in.Metrics != nil {
		if reqCPU.IsZero() {
			reqCPU = e.metricAt(pod, model.MetricCPURequestCores, h, one)
		}
		if reqMem.IsZero() {
			reqMem = e.metricAt(pod, model.MetricMemRequestBytes, h, gib)
		}
	}
	var useCPU, useMem decimal.Decimal
	if e.in.Metrics != nil {
		useCPU = e.metricAt(pod, model.MetricCPUUsageCores, h, one)
		useMem = e.metricAt(pod, model.MetricMemUsageBytes, h, gib)
	}
	switch method {
	case K8sMethodRequests:
		return reqCPU, reqMem
	case K8sMethodUsage:
		return useCPU, useMem
	default:
		return decimal.Max(reqCPU, useCPU), decimal.Max(reqMem, useMem)
	}
}

// metricAt lit la moyenne horaire d'une métrique, divisée par unit.
func (e *engine) metricAt(id, metric string, h int, unit decimal.Decimal) decimal.Decimal {
	vals := e.in.Metrics.Hourly(id, metric)
	if h >= len(vals) || math.IsNaN(vals[h]) {
		return decimal.Zero
	}
	return dec(vals[h]).Div(unit)
}

// distributeIdle répartit le coût idle d'un node sur ses pods au prorata de leur coût.
func (e *engine) distributeIdle() {
	if e.in.Settings.K8sIdleMode != IdleDistribute {
		return
	}
	podLines := map[string][]int{} // node → index des lignes de pods
	for i, l := range e.lines {
		if l.CostType == model.CostK8sWork {
			podLines[l.Labels[LabelK8sNodeID]] = append(podLines[l.Labels[LabelK8sNodeID]], i)
		}
	}
	var kept []model.CostLine
	var added []model.CostLine
	for _, l := range e.lines {
		if l.CostType != model.CostK8sIdle {
			kept = append(kept, l)
			continue
		}
		idx := podLines[l.Labels[LabelK8sNodeID]]
		total := decimal.Zero
		for _, i := range idx {
			total = total.Add(e.lines[i].Amount)
		}
		if total.IsZero() {
			kept = append(kept, l)
			continue
		}
		remaining := l.Amount
		for n, i := range idx {
			p := e.lines[i]
			share := l.Amount.Mul(p.Amount).Div(total).Round(9)
			if n == len(idx)-1 {
				share = remaining
			}
			remaining = remaining.Sub(share)
			nl := p
			nl.CostType = model.CostK8sIdle
			nl.SKU = "k8s.idle"
			nl.Quantity = decimal.Zero
			nl.Amount = share
			nl.SourceRef = "idle:" + l.ResourceID
			added = append(added, nl)
		}
	}
	e.lines = append(kept, added...)
}
