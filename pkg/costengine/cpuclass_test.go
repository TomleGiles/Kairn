package costengine

import (
	"testing"

	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/pricing"
)

// Une instance tarifée par vCore selon sa classe de CPU (grille OUTSCALE) :
// le prix de la classe l'emporte sur le prix vCore générique, qui reste le
// repli pour une classe absente de la grille.
func TestVCPUClassPricing(t *testing.T) {
	cat := model.PriceCatalog{ID: "cat-osc", Provider: "outscale", Version: "pub-1", Currency: "EUR", ValidFrom: day.AddDate(0, -1, 0)}
	it := func(sku, unit, price string) model.PriceItem {
		return model.PriceItem{SKU: sku, Region: "eu-west-2", Unit: unit, Price: d(price), Currency: "EUR", Attributes: map[string]string{}}
	}
	book := pricing.NewBook([]pricing.Catalog{{Catalog: cat, Items: []model.PriceItem{
		it("compute.vcpu.v6-p2", model.UnitVCPUHour, "0.035"),
		it("compute.vcpu", model.UnitVCPUHour, "0.05"),
		it("compute.ram_gb", model.UnitGBHour, "0.005"),
	}}})
	res := func(id, class string) model.Resource {
		return model.Resource{ID: id, OrgID: "org", ConnectorID: "c-osc", Provider: "outscale", Type: model.TypeInstance,
			ExternalID: id, Name: id, Region: "eu-west-2", ValidFrom: day.AddDate(0, 0, -1),
			Attributes: map[string]any{"flavor": "tinav6.c2r4p2", "vcpus": 2.0, "ram_gb": 4.0, "cpu_class": class}}
	}
	in := Input{OrgID: "org", Currency: "EUR", Day: day, Book: book, Resources: []model.Resource{res("i-known", "v6-p2"), res("i-unknown", "v9-p1")}}
	out := mustCompute(t, in)
	byRes := func(id string) (skus map[string]bool, total string) {
		skus = map[string]bool{}
		sum := sumWhere(out.Lines, func(l model.CostLine) bool { return l.ResourceID == id })
		for _, l := range out.Lines {
			if l.ResourceID == id {
				skus[l.SKU] = true
			}
		}
		return skus, sum.String()
	}
	// 24 h × (2 × 0,035 + 4 × 0,005) = 2,16
	skus, total := byRes("i-known")
	if !skus["compute.vcpu.v6-p2"] || !skus["compute.ram_gb"] || total != "2.16" {
		t.Fatalf("class price expected: skus=%v total=%s", skus, total)
	}
	// Classe inconnue : repli sur compute.vcpu, 24 × (2 × 0,05 + 4 × 0,005) = 2,88
	skus, total = byRes("i-unknown")
	if !skus["compute.vcpu"] || total != "2.88" {
		t.Fatalf("generic vCPU fallback expected: skus=%v total=%s", skus, total)
	}
}
