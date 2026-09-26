package costengine

import (
	"sort"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/allocation"
	"github.com/kairn-io/kairn/pkg/model"
)

// invoiceTotal renvoie le montant facturé d'une ressource sur le jour.
func (e *engine) invoiceTotal(resourceID string) (decimal.Decimal, string, bool) {
	total := decimal.Zero
	ref := ""
	found := false
	for _, b := range e.in.Billing {
		if b.ResourceID != resourceID {
			continue
		}
		amt, ok := e.convert(b.Amount, b.Currency)
		if !ok {
			continue
		}
		total = total.Add(amt)
		if ref == "" {
			ref = "invoice:" + b.InvoiceID
		}
		found = true
	}
	return total, ref, found
}

// applyInvoicePreference remplace, quand l'organisation le demande, les
// estimations par les lignes de facture réelles de la ressource.
func (e *engine) applyInvoicePreference() {
	if !e.in.Settings.PreferInvoice || len(e.in.Billing) == 0 {
		return
	}
	invoiced := map[string]bool{}
	for _, b := range e.in.Billing {
		if b.ResourceID != "" {
			if _, known := e.versions[b.ResourceID]; known {
				invoiced[b.ResourceID] = true
			}
		}
	}
	kept := e.lines[:0:0]
	for _, l := range e.lines {
		if invoiced[l.ResourceID] && (l.Source == model.SourceEstimate || l.Source == model.SourceOnPrem) &&
			l.CostType != model.CostK8sWork && l.CostType != model.CostK8sIdle {
			continue
		}
		kept = append(kept, l)
	}
	e.lines = kept
	bills := append([]model.BillingLine(nil), e.in.Billing...)
	sort.SliceStable(bills, func(i, j int) bool {
		if bills[i].ResourceID != bills[j].ResourceID {
			return bills[i].ResourceID < bills[j].ResourceID
		}
		return bills[i].SKU < bills[j].SKU
	})
	for _, b := range bills {
		// Les VM adossées à un node sont déjà réparties via allocateKubernetes.
		if b.ResourceID != "" && e.backedNode(b.ResourceID) != "" {
			continue
		}
		if b.ResourceID != "" && !invoiced[b.ResourceID] {
			continue
		}
		amt, ok := e.convert(b.Amount, b.Currency)
		if !ok {
			continue
		}
		var l model.CostLine
		if r, ok := e.latest(b.ResourceID); ok {
			l = e.baseLine(r)
		} else {
			l = model.CostLine{OrgID: e.in.OrgID, Day: e.dayStart, ConnectorID: b.ConnectorID, Provider: b.Provider, Currency: e.in.Currency}
		}
		l.CostType = b.CostType
		if l.CostType == "" {
			l.CostType = model.CostOther
		}
		l.SKU = b.SKU
		if l.SKU == "" {
			l.SKU = b.Service
		}
		l.Unit = b.Unit
		l.Quantity = b.Quantity
		l.Amount = amt
		l.Source = model.SourceInvoice
		l.CatalogVersion = "invoice:" + b.InvoiceID
		e.lines = append(e.lines, l)
	}
}

// allocate attribue chaque ligne à un nœud d'allocation selon les règles.
func (e *engine) allocate() {
	cache := map[string]string{}
	for i := range e.lines {
		l := &e.lines[i]
		if l.AllocationNodeID != "" {
			continue
		}
		if l.ResourceID == "" {
			l.AllocationNodeID = model.UnallocatedNodeID
			continue
		}
		node, ok := cache[l.ResourceID]
		if !ok {
			node, _ = e.matcher.Match(e.subject(l.ResourceID))
			cache[l.ResourceID] = node
		}
		l.AllocationNodeID = node
	}
}

// applySharedRules redistribue les coûts partagés vers les nœuds cibles.
func (e *engine) applySharedRules() error {
	rules := append([]model.SharedCostRule(nil), e.in.SharedRules...)
	sort.Slice(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })
	type compiled struct {
		rule  model.SharedCostRule
		conds allocation.Conditions
	}
	var active []compiled
	for _, r := range rules {
		if !r.Enabled || len(r.Targets) == 0 {
			continue
		}
		conds, err := allocation.CompileConditions(r.Source)
		if err != nil {
			return err
		}
		active = append(active, compiled{r, conds})
	}
	if len(active) == 0 {
		return nil
	}
	// Coût direct par nœud, avant redistribution (base du mode proportionnel).
	direct := map[string]decimal.Decimal{}
	matched := make([]int, len(e.lines)) // index de règle + 1, 0 = aucune
	for i, l := range e.lines {
		if l.ResourceID != "" {
			s := e.subject(l.ResourceID)
			s.Labels = l.Labels
			for ri, c := range active {
				if c.conds.Match(s) {
					matched[i] = ri + 1
					break
				}
			}
		}
		if matched[i] == 0 {
			direct[l.AllocationNodeID] = direct[l.AllocationNodeID].Add(l.Amount)
		}
	}
	var out []model.CostLine
	for i, l := range e.lines {
		if matched[i] == 0 {
			out = append(out, l)
			continue
		}
		rule := active[matched[i]-1].rule
		weights := make([]decimal.Decimal, len(rule.Targets))
		sum := decimal.Zero
		for ti, t := range rule.Targets {
			switch rule.Method {
			case model.ShareProportional:
				weights[ti] = direct[t.NodeID]
			default: // fixed (pourcentages) et weighted (poids relatifs) se normalisent pareil
				weights[ti] = t.Weight
			}
			if weights[ti].IsNegative() {
				weights[ti] = decimal.Zero
			}
			sum = sum.Add(weights[ti])
		}
		if sum.IsZero() { // répartition égale à défaut de base
			for ti := range weights {
				weights[ti] = one
			}
			sum = decimal.NewFromInt(int64(len(weights)))
		}
		remaining := l.Amount
		for ti, t := range rule.Targets {
			share := l.Amount.Mul(weights[ti]).Div(sum).Round(9)
			if ti == len(rule.Targets)-1 {
				share = remaining
			}
			remaining = remaining.Sub(share)
			nl := l
			nl.Labels = withLabels(l.Labels, "kairn.shared_from", l.AllocationNodeID)
			nl.AllocationNodeID = t.NodeID
			nl.CostType = model.CostShared
			// La source d'origine (estimation, facture, on-prem) est conservée : la
			// redistribution est tracée par SourceRef, pas par la source.
			nl.SourceRef = "shared:" + rule.ID
			nl.Amount = share
			nl.Quantity = decimal.Zero
			out = append(out, nl)
		}
	}
	e.lines = out
	return nil
}

// adjustmentActive indique si un ajustement est valide le jour calculé.
func (e *engine) adjustmentActive(a model.PricingAdjustment) bool {
	return !a.ValidFrom.After(e.dayStart) && (a.ValidTo == nil || a.ValidTo.After(e.dayStart))
}

func adjustmentMatches(a model.PricingAdjustment, l model.CostLine) bool {
	if a.Provider != "" && !strings.EqualFold(a.Provider, l.Provider) {
		return false
	}
	if globMatch(a.SKUPattern, l.SKU) {
		return true
	}
	// Lignes Kubernetes : le SKU pertinent est celui du node sous-jacent.
	if under := l.Labels[LabelUnderlyingSKU]; under != "" {
		for _, sku := range strings.Split(under, ",") {
			if globMatch(a.SKUPattern, sku) {
				return true
			}
		}
	}
	return false
}

// applyAdjustments applique remises, engagements puis crédits, dans cet ordre.
func (e *engine) applyAdjustments() {
	adjs := append([]model.PricingAdjustment(nil), e.in.Adjustments...)
	sort.Slice(adjs, func(i, j int) bool { return adjs[i].ID < adjs[j].ID })
	for _, kind := range []string{model.AdjustmentDiscount, model.AdjustmentCommitment, model.AdjustmentCredit} {
		for _, a := range adjs {
			if a.Kind != kind || !e.adjustmentActive(a) {
				continue
			}
			switch kind {
			case model.AdjustmentDiscount:
				e.applyDiscount(a)
			case model.AdjustmentCommitment:
				e.applyCommitment(a)
			case model.AdjustmentCredit:
				e.applyCredit(a)
			}
		}
	}
}

func (e *engine) isBaseLine(l model.CostLine) bool {
	switch l.CostType {
	case model.CostDiscount, model.CostCommitment, model.CostCredit, model.CostTax:
		return false
	}
	return true
}

// applyDiscount ajoute une ligne négative par ligne correspondante.
func (e *engine) applyDiscount(a model.PricingAdjustment) {
	if a.Percent == nil || !a.Percent.IsPositive() {
		return
	}
	rate := a.Percent.Div(hundred)
	n := len(e.lines)
	for i := 0; i < n; i++ {
		l := e.lines[i]
		if !e.isBaseLine(l) || !adjustmentMatches(a, l) || l.Amount.IsZero() {
			continue
		}
		d := l
		d.CostType = model.CostDiscount
		d.Amount = l.Amount.Mul(rate).Neg()
		d.Quantity = decimal.Zero
		d.SourceRef = "adjustment:" + a.ID
		e.lines = append(e.lines, d)
	}
}

// applyCommitment : un forfait mensuel couvre CoveredUnits heures-unités par
// heure du SKU. La consommation couverte est retirée, le forfait est réparti
// au prorata de la couverture et la part inutilisée reste non allouée.
func (e *engine) applyCommitment(a model.PricingAdjustment) {
	if a.Amount == nil || a.CoveredUnits == nil || !a.CoveredUnits.IsPositive() {
		return
	}
	fee, ok := e.convert(*a.Amount, a.Currency)
	if !ok {
		return
	}
	daysInMonth := decimal.NewFromInt(int64(e.dayStart.AddDate(0, 1, -e.dayStart.Day()).Day()))
	dailyFee := fee.Div(daysInMonth)
	capacity := a.CoveredUnits.Mul(decimal.NewFromInt(24))
	var idx []int
	for i, l := range e.lines {
		if e.isBaseLine(l) && l.Unit == model.UnitHour && adjustmentMatches(a, l) && l.Quantity.IsPositive() {
			idx = append(idx, i)
		}
	}
	sort.SliceStable(idx, func(x, y int) bool { return e.lines[idx[x]].ResourceID < e.lines[idx[y]].ResourceID })
	remaining := capacity
	consumedTotal := decimal.Zero
	for _, i := range idx {
		if !remaining.IsPositive() {
			break
		}
		l := e.lines[i]
		c := decimal.Min(l.Quantity, remaining)
		remaining = remaining.Sub(c)
		consumedTotal = consumedTotal.Add(c)
		covered := l.Amount.Mul(c).Div(l.Quantity)
		share := dailyFee.Mul(c).Div(capacity)
		adj := l
		adj.CostType = model.CostCommitment
		adj.Quantity = c
		adj.Amount = share.Sub(covered)
		adj.SourceRef = "adjustment:" + a.ID
		e.lines = append(e.lines, adj)
	}
	if unused := capacity.Sub(consumedTotal); unused.IsPositive() {
		e.lines = append(e.lines, model.CostLine{
			OrgID: e.in.OrgID, Day: e.dayStart, Provider: a.Provider, CostType: model.CostCommitment,
			SKU: a.SKUPattern, Unit: model.UnitHour, Quantity: unused,
			Amount: dailyFee.Mul(unused).Div(capacity), Currency: e.in.Currency,
			Source: model.SourceEstimate, SourceRef: "adjustment:" + a.ID + ":unused",
			AllocationNodeID: model.UnallocatedNodeID,
		})
	}
}

// applyCredit consomme un crédit jusqu'à épuisement, au prorata des lignes correspondantes.
func (e *engine) applyCredit(a model.PricingAdjustment) {
	if a.Amount == nil {
		return
	}
	total, ok := e.convert(*a.Amount, a.Currency)
	if !ok {
		return
	}
	remaining := total.Sub(e.in.PriorCredit[a.ID])
	if !remaining.IsPositive() {
		return
	}
	var idx []int
	sum := decimal.Zero
	for i, l := range e.lines {
		if l.CostType != model.CostTax && l.CostType != model.CostCredit && adjustmentMatches(a, l) {
			idx = append(idx, i)
			sum = sum.Add(l.Amount)
		}
	}
	if !sum.IsPositive() {
		return
	}
	use := decimal.Min(remaining, sum)
	left := use
	for n, i := range idx {
		l := e.lines[i]
		share := use.Mul(l.Amount).Div(sum).Round(9)
		if n == len(idx)-1 {
			share = left
		}
		left = left.Sub(share)
		if share.IsZero() {
			continue
		}
		c := l
		c.CostType = model.CostCredit
		c.Amount = share.Neg()
		c.Quantity = decimal.Zero
		c.SourceRef = "adjustment:" + a.ID
		e.lines = append(e.lines, c)
	}
}

// applyTax ajoute la TVA par nœud d'allocation si l'organisation l'a activée.
func (e *engine) applyTax() {
	if e.in.VATRate == nil || !e.in.VATRate.IsPositive() {
		return
	}
	byNode := map[string]decimal.Decimal{}
	var nodes []string
	for _, l := range e.lines {
		if _, ok := byNode[l.AllocationNodeID]; !ok {
			nodes = append(nodes, l.AllocationNodeID)
		}
		byNode[l.AllocationNodeID] = byNode[l.AllocationNodeID].Add(l.Amount)
	}
	sort.Strings(nodes)
	for _, n := range nodes {
		base := byNode[n]
		if base.IsZero() {
			continue
		}
		e.lines = append(e.lines, model.CostLine{
			OrgID: e.in.OrgID, Day: e.dayStart, CostType: model.CostTax, SKU: "tax.vat",
			Quantity: base, Unit: "base", Amount: base.Mul(*e.in.VATRate), Currency: e.in.Currency,
			Source: model.SourceEstimate, AllocationNodeID: n, SourceRef: "vat",
		})
	}
}
