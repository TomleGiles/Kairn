package pricing

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/model"
)

func item(sku, price string) model.PriceItem {
	return model.PriceItem{SKU: sku, Unit: model.UnitHour, Price: decimal.RequireFromString(price), Currency: "EUR"}
}

func TestLookupPriority(t *testing.T) {
	org := "org-1"
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	book := NewBook([]Catalog{
		{Catalog: model.PriceCatalog{Provider: "ovh", Version: "sample-1", Source: model.CatalogSourceSample, ValidFrom: t0},
			Items: []model.PriceItem{item("a", "1"), item("only-in-sample", "9")}},
		{Catalog: model.PriceCatalog{Provider: "ovh", Version: "pub-old", Source: "api", ValidFrom: t0.AddDate(0, 3, 0)},
			Items: []model.PriceItem{item("a", "2"), item("retired", "5")}},
		{Catalog: model.PriceCatalog{Provider: "ovh", Version: "pub-new", Source: "api", ValidFrom: t0.AddDate(0, 6, 0)},
			Items: []model.PriceItem{item("a", "3"), {SKU: "a", Region: "GRA", Unit: model.UnitHour, Price: decimal.RequireFromString("3.5"), Currency: "EUR"}}},
		{Catalog: model.PriceCatalog{Provider: "ovh", OrgID: &org, Version: "negotiated", Source: "upload", ValidFrom: t0.AddDate(0, 7, 0)},
			Items: []model.PriceItem{item("a", "1.5")}},
	})
	price := func(sku, region string, at time.Time) string {
		m, ok := book.Lookup("ovh", sku, region, at)
		if !ok {
			return "-"
		}
		return m.Item.Price.String() + "@" + m.Catalog.Version
	}
	cases := []struct {
		name, sku, region string
		at                time.Time
		want              string
	}{
		{"sample only before any official catalog", "a", "", t0.AddDate(0, 1, 0), "1@sample-1"},
		{"sample SKU usable while no official catalog", "only-in-sample", "", t0.AddDate(0, 1, 0), "9@sample-1"},
		{"official catalog replaces sample", "a", "", t0.AddDate(0, 4, 0), "2@pub-old"},
		{"sample never completes an official catalog", "only-in-sample", "", t0.AddDate(0, 4, 0), "-"},
		{"latest official version", "a", "", t0.AddDate(0, 6, 1), "3@pub-new"},
		{"exact region first", "a", "GRA", t0.AddDate(0, 6, 1), "3.5@pub-new"},
		{"region fallback to global item", "a", "SBG", t0.AddDate(0, 6, 1), "3@pub-new"},
		{"retired SKU keeps last official price", "retired", "", t0.AddDate(0, 6, 1), "5@pub-old"},
		{"negotiated catalog wins", "a", "GRA", t0.AddDate(0, 8, 0), "1.5@negotiated"},
		{"negotiated falls through to public", "retired", "", t0.AddDate(0, 8, 0), "5@pub-old"},
		{"nothing valid before first catalog", "a", "", t0.AddDate(0, -1, 0), "-"},
	}
	for _, c := range cases {
		if got := price(c.sku, c.region, c.at); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
	if items := book.Items("ovh", t0.AddDate(0, 4, 0)); len(items) != 2 {
		t.Fatalf("Items must ignore sample catalogs once an official one is valid: %+v", items)
	}
	if got := book.Providers(); len(got) != 1 || got[0] != "ovh" {
		t.Fatalf("providers: %v", got)
	}
}
