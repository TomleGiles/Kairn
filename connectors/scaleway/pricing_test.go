package scaleway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/pricing/catalogs"
)

// testdata/catalog-page*.json sont des extraits réels du catalogue public
// Scaleway (GET /product-catalog/v2alpha1/public-catalog/products).
func TestPriceImporter(t *testing.T) {
	pages := map[string][]byte{}
	for _, p := range []string{"1", "2"} {
		b, err := os.ReadFile("testdata/catalog-page" + p + ".json")
		if err != nil {
			t.Fatal(err)
		}
		pages[p] = b
	}
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		b, ok := pages[r.URL.Query().Get("page")]
		if r.URL.Path != "/product-catalog/v2alpha1/public-catalog/products" || r.URL.Query().Get("page_size") != "14" || !ok || r.Header.Get("X-Auth-Token") != "" {
			http.Error(w, "unexpected "+r.URL.String(), http.StatusNotFound)
			return
		}
		_, _ = w.Write(b)
	}))
	defer srv.Close()

	f, err := PriceImporter{BaseURL: srv.URL, PageSize: 14}.Fetch(context.Background(), srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("pagination must stop at total_count, got %d calls", calls)
	}
	f.Normalize()
	got := map[string]catalogs.Item{}
	for _, it := range f.Items {
		got[it.SKU+"@"+it.Region] = it
	}
	want := map[string]struct{ unit, price string }{
		"compute.flavor.PRO2-XXS@fr-par-1":                 {model.UnitHour, "0.0561"},
		"compute.flavor.PRO2-XXS@fr-par-2":                 {model.UnitHour, "0.0561"},
		"compute.flavor.DEV1-S@fr-par-1":                   {model.UnitHour, "0.008976"},
		"compute.flavor.VC1L@fr-par-1":                     {model.UnitHour, "0.011"}, // offre retirée mais encore facturée
		"storage.volume.sbs_5k@fr-par-1":                   {model.UnitGBMonth, "0.094899"},
		"storage.volume.sbs_15k@fr-par-1":                  {model.UnitGBMonth, "0.12921"},
		"storage.volume.b_ssd@fr-par-1":                    {model.UnitGBMonth, "0.094899"},
		"storage.volume.l_ssd@fr-par-1":                    {model.UnitGBMonth, "0.03577"},
		"storage.snapshot@fr-par-1":                        {model.UnitGBMonth, "0.03577"},
		"storage.object.standard@fr-par":                   {model.UnitGBMonth, "0.01606"}, // offre disponible préférée à l'offre retirée
		"storage.object.glacier@fr-par":                    {model.UnitGBMonth, "0.00254"},
		"k8s.control_plane.free@fr-par":                    {model.UnitHour, "0"},
		"k8s.control_plane.dedicated-4@fr-par":             {model.UnitHour, "0.11"},
		"k8s.control_plane.dedicated-8@fr-par":             {model.UnitHour, "0.18"},
		"k8s.control_plane.dedicated-16@fr-par":            {model.UnitHour, "0.35"},
		"k8s.control_plane.multicloud-dedicated-8@fr-par":  {model.UnitHour, "0.3244"},
		"k8s.control_plane.multicloud-dedicated-16@fr-par": {model.UnitHour, "0.4944"},
		"k8s.control_plane.multicloud@fr-par":              {model.UnitHour, "0.1444"},
		"k8s.control_plane.multicloud-dedicated-4@fr-par":  {model.UnitHour, "0.2544"},
		"network.lb.LB-S@nl-ams-1":                         {model.UnitHour, "0.023"},
		"network.lb.LB-GP-M@nl-ams-1":                      {model.UnitHour, "0.054"},
	}
	for k, w := range want {
		it, ok := got[k]
		if !ok {
			t.Errorf("missing %s", k)
			continue
		}
		if it.Unit != w.unit || it.Price.String() != w.price {
			t.Errorf("%s: got %s %s, want %s %s", k, it.Price, it.Unit, w.price, w.unit)
		}
	}
	if a := got["compute.flavor.PRO2-XXS@fr-par-1"].Attributes; a["vcpus"] != "2" || a["ram_gb"] != "8" || a["family"] != "General Purpose" {
		t.Errorf("flavor attributes: %v", a)
	}
	if _, ok := got["compute.flavor.VC1L@fr-par-2"]; ok {
		t.Error("retired zero-priced offers must be skipped")
	}
	if len(got) != len(want) {
		for k := range got {
			if _, ok := want[k]; !ok {
				t.Errorf("unexpected item %s", k)
			}
		}
	}
	if f.Currency != "EUR" {
		t.Fatalf("currency %q", f.Currency)
	}
}

func TestControlPlaneTier(t *testing.T) {
	for in, want := range map[string]string{"kapsule": "free", "": "free", "kapsule-dedicated-8": "dedicated-8",
		"multicloud": "multicloud", "Multicloud-Dedicated-4": "multicloud-dedicated-4"} {
		if got := controlPlaneTier(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}
