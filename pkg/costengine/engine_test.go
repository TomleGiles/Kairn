package costengine

import (
	"math"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/pricing"
)

var day = time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }
func dp(s string) *decimal.Decimal {
	v := d(s)
	return &v
}
func sp(s string) *string       { return &s }
func tp(t time.Time) *time.Time { return &t }

func testBook() *pricing.Book {
	item := func(sku, unit, price string, attrs ...string) model.PriceItem {
		it := model.PriceItem{SKU: sku, Unit: unit, Price: d(price), Currency: "EUR", Attributes: map[string]string{}}
		for i := 0; i+1 < len(attrs); i += 2 {
			it.Attributes[attrs[i]] = attrs[i+1]
		}
		return it
	}
	return pricing.NewBook([]pricing.Catalog{
		{
			Catalog: model.PriceCatalog{ID: "cat-os", Provider: "openstack", Version: "2026-09", Currency: "EUR", ValidFrom: day.AddDate(0, -1, 0)},
			Items: []model.PriceItem{
				item("compute.flavor.b2-7", model.UnitHour, "0.0681", "vcpus", "2", "ram_gb", "7"),
				item("compute.flavor.b2-15", model.UnitHour, "0.1331", "vcpus", "4", "ram_gb", "15"),
				item("storage.volume.classic", model.UnitGBMonth, "0.04"),
				item("storage.volume.high-speed", model.UnitGBMonth, "0.08"),
				item("storage.snapshot", model.UnitGBMonth, "0.01"),
				item("network.ip.floating", model.UnitHour, "0.0025"),
				item("license.windows", model.UnitVCPUHour, "0.01"),
			},
		},
		{
			Catalog: model.PriceCatalog{ID: "cat-aws", Provider: "aws", Version: "v1", Currency: "USD", ValidFrom: day.AddDate(-1, 0, 0)},
			Items:   []model.PriceItem{usd(item("compute.flavor.m5.large", model.UnitHour, "0.1", "vcpus", "2", "ram_gb", "8"))},
		},
	})
}

func usd(it model.PriceItem) model.PriceItem {
	it.Currency = "USD"
	return it
}

func vm(id, flavor string, from time.Time, to *time.Time, labels map[string]string) model.Resource {
	return model.Resource{
		ID: id, OrgID: "org", ConnectorID: "c-os", Provider: "openstack", Type: model.TypeInstance,
		ExternalID: id, Name: id, Region: "GRA11",
		Attributes: map[string]any{"flavor": flavor, "vcpus": 4.0, "ram_gb": 15.0},
		Labels:     labels, ValidFrom: from, ValidTo: to,
	}
}

func base(res ...model.Resource) Input {
	return Input{OrgID: "org", Currency: "EUR", Day: day, Resources: res, Book: testBook()}
}

func mustCompute(t *testing.T, in Input) Output {
	t.Helper()
	out, err := Compute(in)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func assertCents(t *testing.T, got decimal.Decimal, want string) {
	t.Helper()
	if !got.Round(2).Equal(d(want).Round(2)) {
		t.Fatalf("got %s, want %s (au centime)", got.StringFixed(6), want)
	}
}

func sumWhere(lines []model.CostLine, pred func(model.CostLine) bool) decimal.Decimal {
	t := decimal.Zero
	for _, l := range lines {
		if pred(l) {
			t = t.Add(l.Amount)
		}
	}
	return t
}

func TestFullDayInstance(t *testing.T) {
	out := mustCompute(t, base(vm("vm1", "b2-7", day.AddDate(0, 0, -3), nil, nil)))
	if len(out.Lines) != 1 {
		t.Fatalf("want 1 line, got %d: %+v", len(out.Lines), out.Lines)
	}
	l := out.Lines[0]
	if !l.Amount.Equal(d("1.6344")) || !l.Quantity.Equal(d("24")) {
		t.Fatalf("24h × 0.0681 = 1.6344, got amount=%s qty=%s", l.Amount, l.Quantity)
	}
	if l.CatalogVersion != "openstack@2026-09" || l.Source != model.SourceEstimate || l.SKU != "compute.flavor.b2-7" {
		t.Fatalf("traceability fields wrong: %+v", l)
	}
	if l.AllocationNodeID != model.UnallocatedNodeID {
		t.Fatalf("no rule → unallocated, got %q", l.AllocationNodeID)
	}
}

func TestPartialDayInstance(t *testing.T) {
	out := mustCompute(t, base(vm("vm1", "b2-7", day.Add(6*time.Hour+30*time.Minute), nil, nil)))
	// 17,5 h × 0,0681 = 1,19175
	if got := Total(out.Lines); !got.Equal(d("1.19175")) {
		t.Fatalf("got %s", got)
	}
}

func TestResizeMidDay(t *testing.T) {
	noon := day.Add(12 * time.Hour)
	out := mustCompute(t, base(
		vm("vm1", "b2-7", day.AddDate(0, 0, -1), tp(noon), nil),
		vm("vm1", "b2-15", noon, nil, nil),
	))
	// 12 × 0,0681 + 12 × 0,1331 = 2,4144
	if got := Total(out.Lines); !got.Equal(d("2.4144")) {
		t.Fatalf("got %s", got)
	}
}

func TestStorageNetworkAndLicense(t *testing.T) {
	vol := model.Resource{ID: "vol1", OrgID: "org", ConnectorID: "c-os", Provider: "openstack", Type: model.TypeVolume,
		Attributes: map[string]any{"size_gb": 100.0, "volume_type": "classic"}, ValidFrom: day.AddDate(0, 0, -1)}
	ip := model.Resource{ID: "ip1", OrgID: "org", ConnectorID: "c-os", Provider: "openstack", Type: model.TypeIP,
		Attributes: map[string]any{}, ValidFrom: day.AddDate(0, 0, -1)}
	win := vm("vm-win", "b2-7", day.AddDate(0, 0, -1), nil, nil)
	win.Attributes["license"] = "windows"
	win.Attributes["vcpus"] = 2.0
	out := mustCompute(t, base(vol, ip, win))
	// Volume : 100 Go × 0,04 × 24/730 = 0,131506849…
	assertCents(t, sumWhere(out.Lines, func(l model.CostLine) bool { return l.ResourceID == "vol1" }), "0.13")
	if v := sumWhere(out.Lines, func(l model.CostLine) bool { return l.ResourceID == "vol1" }); !v.Equal(d("0.131507")) {
		t.Fatalf("volume stored at 6 decimals: %s", v)
	}
	// IP : 24 × 0,0025 = 0,06
	if v := sumWhere(out.Lines, func(l model.CostLine) bool { return l.ResourceID == "ip1" }); !v.Equal(d("0.06")) {
		t.Fatalf("ip: %s", v)
	}
	// Licence Windows : 2 vCPU × 0,01 × 24 = 0,48
	if v := sumWhere(out.Lines, func(l model.CostLine) bool { return l.CostType == model.CostLicense }); !v.Equal(d("0.48")) {
		t.Fatalf("license: %s", v)
	}
}

func TestUnbilledAndShelvedInstances(t *testing.T) {
	a := vm("a", "b2-7", day.AddDate(0, 0, -1), nil, nil)
	a.Attributes["billing_state"] = BillingStoppedUnbiled
	b := vm("b", "b2-7", day.AddDate(0, 0, -1), nil, nil)
	b.Attributes["status"] = "SHELVED_OFFLOADED"
	out := mustCompute(t, base(a, b))
	if len(out.Lines) != 0 {
		t.Fatalf("stopped-unbilled and shelved instances cost nothing, got %+v", out.Lines)
	}
}

func TestMissingPriceWarns(t *testing.T) {
	out := mustCompute(t, base(vm("vm1", "unknown-flavor", day.AddDate(0, 0, -1), nil, nil)))
	if len(out.Lines) != 0 || len(out.Warnings) == 0 {
		t.Fatalf("expected warning and no line, got lines=%v warnings=%v", out.Lines, out.Warnings)
	}
}

func TestCurrencyConversion(t *testing.T) {
	r := model.Resource{ID: "i1", OrgID: "org", Provider: "aws", Type: model.TypeInstance,
		Attributes: map[string]any{"instance_type": "m5.large"}, ValidFrom: day.AddDate(0, 0, -1)}
	in := base(r)
	in.Rates = func(from, to string, _ time.Time) (decimal.Decimal, error) {
		if from == "USD" && to == "EUR" {
			return d("0.9"), nil
		}
		t.Fatalf("unexpected rate %s→%s", from, to)
		return decimal.Zero, nil
	}
	// 24 × 0,1 USD × 0,9 = 2,16 EUR
	if got := Total(mustCompute(t, in).Lines); !got.Equal(d("2.16")) {
		t.Fatalf("got %s", got)
	}
}

type fakeMetrics map[string]map[string][]float64

func (f fakeMetrics) Hourly(id, metric string) []float64 {
	if v, ok := f[id][metric]; ok {
		return v
	}
	out := make([]float64, 24)
	for i := range out {
		out[i] = math.NaN()
	}
	return out
}

func (f fakeMetrics) DailyAvg(id, metric string) (float64, bool) {
	v, ok := f[id][metric]
	if !ok || len(v) == 0 {
		return 0, false
	}
	s := 0.0
	for _, x := range v {
		s += x
	}
	return s / float64(len(v)), true
}

func constant(v float64) []float64 {
	out := make([]float64, 24)
	for i := range out {
		out[i] = v
	}
	return out
}

func k8sFixture() Input {
	since := day.AddDate(0, 0, -2)
	node := model.Resource{ID: "node1", OrgID: "org", ConnectorID: "c-k8s", Provider: "kubernetes", Type: model.TypeK8sNode,
		Name: "node-1", Attributes: map[string]any{"cpu_capacity_cores": 4.0, "mem_capacity_gb": 15.0}, ValidFrom: since}
	ns := model.Resource{ID: "ns-pay", OrgID: "org", ConnectorID: "c-k8s", Provider: "kubernetes", Type: model.TypeK8sNamespace,
		Name: "payments", Labels: map[string]string{"team": "payments"}, ValidFrom: since}
	podA := model.Resource{ID: "podA", OrgID: "org", ConnectorID: "c-k8s", Provider: "kubernetes", Type: model.TypeK8sPod,
		Name: "api-1", Attributes: map[string]any{"cpu_request_cores": 1.0, "mem_request_gb": 2.0, "k8s.namespace": "payments"}, ValidFrom: since}
	podB := model.Resource{ID: "podB", OrgID: "org", ConnectorID: "c-k8s", Provider: "kubernetes", Type: model.TypeK8sPod,
		Name: "worker-1", Attributes: map[string]any{"cpu_request_cores": 0.5, "mem_request_gb": 4.0, "k8s.namespace": "kube-system"}, ValidFrom: since}
	edges := []model.ResourceEdge{
		{OrgID: "org", ParentID: "vm-node1", ChildID: "node1", Relation: model.RelBacks, ValidFrom: since},
		{OrgID: "org", ParentID: "node1", ChildID: "podA", Relation: model.RelRunsOn, ValidFrom: since},
		{OrgID: "org", ParentID: "node1", ChildID: "podB", Relation: model.RelRunsOn, ValidFrom: since},
		{OrgID: "org", ParentID: "ns-pay", ChildID: "podA", Relation: model.RelContains, ValidFrom: since},
	}
	in := base(vm("vm-node1", "b2-15", since, nil, nil), node, ns, podA, podB)
	in.Edges = edges
	in.Metrics = fakeMetrics{
		"podA": {model.MetricCPUUsageCores: constant(0.2), model.MetricMemUsageBytes: constant(1 << 30)},
		"podB": {model.MetricCPUUsageCores: constant(1.0), model.MetricMemUsageBytes: constant(2 << 30)},
	}
	return in
}

func TestKubernetesAllocation(t *testing.T) {
	out := mustCompute(t, k8sFixture())
	// Coût du node = VM b2-15 : 24 × 0,1331 = 3,1944 ; poids CPU = 4×7,5/(30+15) = 2/3.
	// Pod A = 2/3×1/4 + 1/3×2/15 = 19/90 ; pod B (max) = 2/3×1/4 + 1/3×4/15 = 23/90 ; idle = 48/90.
	total := Total(out.Lines)
	if !total.Equal(d("3.1944")) {
		t.Fatalf("node cost must be conserved: %s", total)
	}
	podA := sumWhere(out.Lines, func(l model.CostLine) bool { return l.ResourceID == "podA" })
	podB := sumWhere(out.Lines, func(l model.CostLine) bool { return l.ResourceID == "podB" })
	idle := sumWhere(out.Lines, func(l model.CostLine) bool { return l.CostType == model.CostK8sIdle })
	if !podA.Equal(d("3.1944").Mul(d("19")).Div(d("90")).Round(6)) {
		t.Fatalf("pod A: %s", podA)
	}
	if !podB.Equal(d("3.1944").Mul(d("23")).Div(d("90")).Round(6)) {
		t.Fatalf("pod B: %s", podB)
	}
	assertCents(t, idle, "1.70")
	if n := len(out.Lines); n != 3 {
		t.Fatalf("VM backing a node must not be billed twice: %d lines %+v", n, out.Lines)
	}
	for _, l := range out.Lines {
		if l.Provider != "openstack" || l.Labels[LabelBackingVM] != "vm-node1" {
			t.Fatalf("k8s lines carry the paying provider and backing VM: %+v", l)
		}
	}
}

func TestKubernetesRequestsMethodAndIdleDistribution(t *testing.T) {
	in := k8sFixture()
	in.Settings.K8sAllocationMethod = K8sMethodRequests
	in.Settings.K8sIdleMode = IdleDistribute
	out := mustCompute(t, in)
	if got := Total(out.Lines); !got.Equal(d("3.1944")) {
		t.Fatalf("total conserved: %s", got)
	}
	for _, l := range out.Lines {
		if l.ResourceID == "node1" {
			t.Fatalf("idle must be distributed to pods: %+v", l)
		}
	}
	// Méthode requests : pod B = 2/3×0,5/4 + 1/3×4/15 = 1/12 + 4/45 → part du total inchangée après idle.
	podB := sumWhere(out.Lines, func(l model.CostLine) bool { return l.ResourceID == "podB" })
	podA := sumWhere(out.Lines, func(l model.CostLine) bool { return l.ResourceID == "podA" })
	if podA.Add(podB).Round(6).Cmp(d("3.1944")) != 0 {
		t.Fatalf("pods absorb 100%%: %s + %s", podA, podB)
	}
}

func TestAllocationRulesAndInheritance(t *testing.T) {
	in := k8sFixture()
	in.Rules = []model.AllocationRule{
		{ID: "r1", NodeID: "team-payments", Priority: 10, Enabled: true, Conditions: []model.Condition{{Field: "label.team", Op: model.OpEq, Value: "payments"}}},
		{ID: "r2", NodeID: "platform", Priority: 20, Enabled: true, Conditions: []model.Condition{{Field: "attr.k8s.namespace", Op: model.OpEq, Value: "kube-system"}}},
		{ID: "r3", NodeID: "never", Priority: 1, Enabled: false, Conditions: []model.Condition{{Field: "type", Op: model.OpExists}}},
	}
	out := mustCompute(t, in)
	for _, l := range out.Lines {
		switch l.ResourceID {
		case "podA":
			if l.AllocationNodeID != "team-payments" {
				t.Fatalf("pod inherits namespace label team=payments: %+v", l)
			}
		case "podB":
			if l.AllocationNodeID != "platform" {
				t.Fatalf("pod matched by attribute: %+v", l)
			}
		case "node1":
			if l.AllocationNodeID != model.UnallocatedNodeID {
				t.Fatalf("idle unallocated: %+v", l)
			}
		}
	}
}

func TestSharedCostsFixedSplit(t *testing.T) {
	in := k8sFixture()
	in.Rules = []model.AllocationRule{
		{ID: "r1", NodeID: "team-payments", Priority: 10, Enabled: true, Conditions: []model.Condition{{Field: "label.team", Op: model.OpEq, Value: "payments"}}},
	}
	in.SharedRules = []model.SharedCostRule{{
		ID: "s1", Enabled: true, Method: model.ShareFixed,
		Source:  []model.Condition{{Field: "attr.k8s.namespace", Op: model.OpEq, Value: "kube-system"}},
		Targets: []model.ShareTarget{{NodeID: "team-payments", Weight: d("70")}, {NodeID: "team-search", Weight: d("30")}},
	}}
	out := mustCompute(t, in)
	if got := Total(out.Lines); !got.Equal(d("3.1944")) {
		t.Fatalf("shared redistribution conserves total: %s", got)
	}
	podB := d("3.1944").Mul(d("23")).Div(d("90"))
	search := sumWhere(out.Lines, func(l model.CostLine) bool { return l.AllocationNodeID == "team-search" })
	assertCents(t, search, podB.Mul(d("0.3")).StringFixed(6))
	for _, l := range out.Lines {
		if l.ResourceID == "podB" && l.CostType != model.CostShared {
			t.Fatalf("source line must be replaced by shared lines: %+v", l)
		}
	}
}

func TestDiscountAndVAT(t *testing.T) {
	in := base(vm("vm1", "b2-7", day.AddDate(0, 0, -1), nil, nil))
	in.Adjustments = []model.PricingAdjustment{{ID: "a1", Kind: model.AdjustmentDiscount, Provider: "openstack", SKUPattern: "compute.*", Percent: dp("10"), ValidFrom: day.AddDate(0, -1, 0)}}
	in.VATRate = dp("0.2")
	out := mustCompute(t, in)
	// 1,6344 − 10 % = 1,47096 ; TVA 20 % → 1,765152
	if got := Total(out.Lines); !got.Equal(d("1.765152")) {
		t.Fatalf("got %s", got)
	}
	disc := sumWhere(out.Lines, func(l model.CostLine) bool { return l.CostType == model.CostDiscount })
	if !disc.Equal(d("-0.16344")) {
		t.Fatalf("discount line: %s", disc)
	}
}

func TestExpiredAdjustmentIgnored(t *testing.T) {
	in := base(vm("vm1", "b2-7", day.AddDate(0, 0, -1), nil, nil))
	in.Adjustments = []model.PricingAdjustment{{ID: "a1", Kind: model.AdjustmentDiscount, Percent: dp("50"), ValidFrom: day.AddDate(0, -2, 0), ValidTo: tp(day)}}
	if got := Total(mustCompute(t, in).Lines); !got.Equal(d("1.6344")) {
		t.Fatalf("expired discount must not apply: %s", got)
	}
}

func TestCommitment(t *testing.T) {
	in := base(
		vm("vm1", "b2-7", day.AddDate(0, 0, -1), nil, nil),
		vm("vm2", "b2-7", day.AddDate(0, 0, -1), nil, nil),
	)
	// Forfait 30 €/mois (septembre : 30 jours → 1 €/jour) couvrant 1 instance b2-7.
	in.Adjustments = []model.PricingAdjustment{{ID: "c1", Kind: model.AdjustmentCommitment, Provider: "openstack",
		SKUPattern: "compute.flavor.b2-7", Amount: dp("30"), CoveredUnits: dp("1"), Currency: "EUR", ValidFrom: day.AddDate(0, -1, 0)}}
	out := mustCompute(t, in)
	// Coût = 1 € de forfait + vm2 à la demande (1,6344) = 2,6344
	if got := Total(out.Lines); !got.Equal(d("2.6344")) {
		t.Fatalf("got %s", got)
	}
}

func TestCreditConsumption(t *testing.T) {
	in := base(vm("vm1", "b2-7", day.AddDate(0, 0, -1), nil, nil))
	in.Adjustments = []model.PricingAdjustment{{ID: "k1", Kind: model.AdjustmentCredit, Amount: dp("100"), Currency: "EUR", ValidFrom: day.AddDate(0, -1, 0)}}
	in.PriorCredit = map[string]decimal.Decimal{"k1": d("99")}
	// Reste 1 € de crédit sur 1,6344 → 0,6344
	if got := Total(mustCompute(t, in).Lines); !got.Equal(d("0.6344")) {
		t.Fatalf("got %s", got)
	}
	in.PriorCredit = map[string]decimal.Decimal{"k1": d("100")}
	if got := Total(mustCompute(t, in).Lines); !got.Equal(d("1.6344")) {
		t.Fatalf("exhausted credit: %s", got)
	}
}

func TestOnPremModel(t *testing.T) {
	host := model.Resource{ID: "h1", OrgID: "org", ConnectorID: "c-agent", Provider: "onprem", Type: model.TypeHost,
		Attributes: map[string]any{"vcpus": 4.0, "ram_gb": 16.0}, ValidFrom: day.AddDate(0, 0, -1)}
	in := base(host)
	in.OnPrem = []model.OnPremCostModel{{
		ID: "m1", ConnectorID: sp("c-agent"), Currency: "EUR",
		HardwareCost: d("36000"), AmortizationMonths: 36, PowerKW: d("2"), PowerPricePerKWh: d("0.2"), PUE: d("1.5"),
		LaborMonthly: d("562"), CapacityVCPU: d("100"), CapacityRAMGB: d("400"),
		WeightCPU: d("0.5"), WeightRAM: d("0.5"), ValidFrom: day.AddDate(-1, 0, 0),
	}}
	// Mensuel = 1000 + 2×730×0,2×1,5 (438) + 562 = 2000 € ; 1000 € CPU / 100 vCPU / 730 h ; 1000 € RAM / 400 Go / 730 h.
	// Jour = 24 × 4 × 10/730 (1,315068…) + 24 × 16 × 2,5/730 (1,315068…), chaque ligne arrondie à 6 décimales.
	out := mustCompute(t, in)
	if got := Total(out.Lines); !got.Equal(d("2.630136")) {
		t.Fatalf("got %s", got)
	}
	if out.Lines[0].Source != model.SourceOnPrem || out.Lines[0].CatalogVersion != "onprem:m1" {
		t.Fatalf("onprem traceability: %+v", out.Lines[0])
	}
}

func TestInvoicePreference(t *testing.T) {
	in := base(vm("vm1", "b2-7", day.AddDate(0, 0, -1), nil, nil), vm("vm2", "b2-7", day.AddDate(0, 0, -1), nil, nil))
	in.Settings.PreferInvoice = true
	in.Billing = []model.BillingLine{
		{ConnectorID: "c-os", Day: day, ResourceID: "vm1", SKU: "b2-7", CostType: model.CostCompute, Amount: d("1.50"), Currency: "EUR", InvoiceID: "FR123"},
		{ConnectorID: "c-os", Day: day, Service: "support", CostType: model.CostOther, Amount: d("2"), Currency: "EUR", InvoiceID: "FR123"},
	}
	out := mustCompute(t, in)
	// vm1 facturée 1,50 ; vm2 estimée 1,6344 ; support 2 → 5,1344
	if got := Total(out.Lines); !got.Equal(d("5.1344")) {
		t.Fatalf("got %s", got)
	}
	for _, l := range out.Lines {
		if l.ResourceID == "vm1" && l.Source != model.SourceInvoice {
			t.Fatalf("vm1 must come from invoice: %+v", l)
		}
	}
}

func TestDeterministicAcrossInputOrder(t *testing.T) {
	in := k8sFixture()
	in.Rules = []model.AllocationRule{{ID: "r1", NodeID: "n", Priority: 1, Enabled: true, Conditions: []model.Condition{{Field: "label.team", Op: model.OpEq, Value: "payments"}}}}
	first := mustCompute(t, in)
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 5; i++ {
		shuffled := in
		shuffled.Resources = append([]model.Resource(nil), in.Resources...)
		shuffled.Edges = append([]model.ResourceEdge(nil), in.Edges...)
		rng.Shuffle(len(shuffled.Resources), func(a, b int) {
			shuffled.Resources[a], shuffled.Resources[b] = shuffled.Resources[b], shuffled.Resources[a]
		})
		rng.Shuffle(len(shuffled.Edges), func(a, b int) { shuffled.Edges[a], shuffled.Edges[b] = shuffled.Edges[b], shuffled.Edges[a] })
		again := mustCompute(t, shuffled)
		if !reflect.DeepEqual(first.Lines, again.Lines) {
			t.Fatalf("output depends on input order")
		}
	}
}

func TestGlobMatch(t *testing.T) {
	cases := []struct {
		p, s string
		want bool
	}{
		{"", "x", true}, {"*", "x", true}, {"compute.*", "compute.flavor.b2-7", true},
		{"compute.*", "storage.volume", false}, {"*.b2-7", "compute.flavor.b2-7", true},
		{"compute.*.b2-*", "compute.flavor.b2-15", true}, {"exact", "exact", true}, {"exact", "exactly", false},
	}
	for _, c := range cases {
		if got := globMatch(c.p, c.s); got != c.want {
			t.Errorf("globMatch(%q,%q)=%v want %v", c.p, c.s, got, c.want)
		}
	}
}
