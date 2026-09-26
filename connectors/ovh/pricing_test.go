package ovh

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/pricing/catalogs"
)

// testdata/public-catalog.json est un extrait réel du catalogue public
// OVHcloud (GET /1.0/order/catalog/public/cloud?ovhSubsidiary=FR).
func TestPriceImporter(t *testing.T) {
	raw, err := os.ReadFile("testdata/public-catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/1.0/order/catalog/public/cloud" || r.URL.Query().Get("ovhSubsidiary") != "FR" || r.Header.Get("Authorization") != "" {
			http.Error(w, "unexpected "+r.URL.String(), http.StatusNotFound)
			return
		}
		_, _ = w.Write(raw)
	}))
	defer srv.Close()

	f, err := PriceImporter{BaseURL: srv.URL + "/1.0"}.Fetch(context.Background(), srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	f.Normalize()
	if f.Currency != "EUR" || f.Provider != "ovh" {
		t.Fatalf("header: %+v", f)
	}
	got := map[string]catalogs.Item{}
	for _, it := range f.Items {
		got[it.SKU] = it
	}
	want := map[string]struct{ unit, price string }{
		"compute.flavor.b2-7":            {model.UnitHour, "0.0709"},
		"compute.flavor.win-b2-7":        {model.UnitHour, "0.1907"},
		"compute.flavor.d2-2":            {model.UnitHour, "0.0104"},
		"storage.volume.classic":         {model.UnitGBMonth, "0.04307"}, // 0,000059 €/Go/h × 730
		"storage.volume.high-speed-gen2": {model.UnitGBMonth, "0.08687"},
		"storage.snapshot":               {model.UnitGBMonth, "0.04307"},
		"storage.instance_backup":        {model.UnitGBMonth, "0.01095"},
		"storage.object.standard":        {model.UnitGBMonth, "0.007096"}, // 0,00000972 × 730, arrondi à 6 décimales
		"network.ip.floating":            {model.UnitHour, "0.0027"},
		"network.ip.public":              {model.UnitHour, "0"},
		"network.lb.small":               {model.UnitHour, "0.0083"},
		"k8s.control_plane.standard":     {model.UnitHour, "0.09"},
		"k8s.control_plane.free":         {model.UnitHour, "0"},
	}
	for sku, w := range want {
		it, ok := got[sku]
		if !ok {
			t.Errorf("missing %s", sku)
			continue
		}
		if it.Unit != w.unit || it.Price.String() != w.price {
			t.Errorf("%s: got %s %s, want %s %s", sku, it.Price, it.Unit, w.price, w.unit)
		}
	}
	if a := got["compute.flavor.b2-7"].Attributes; a["vcpus"] != "2" || a["ram_gb"] != "7" || a["family"] != "general-purpose" {
		t.Errorf("flavor attributes: %v", a)
	}
	if _, ok := got["network.lb.xl"]; !ok {
		t.Error("Octavia flavor name must come from the technical blob")
	}
	for _, sku := range []string{"compute.flavor.c3-32", "compute.flavor.b2-7.monthly.postpaid"} {
		if _, ok := got[sku]; ok {
			t.Errorf("%s must be skipped (3AZ / monthly offers)", sku)
		}
	}
	for sku := range got {
		if len(sku) > 9 && (sku[:9] == "databases" || sku[:9] == "bandwidth") {
			t.Errorf("unexpected SKU %s", sku)
		}
	}
}
