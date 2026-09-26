package outscale

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/pricing/catalogs"
)

// testdata/public-catalog.json est un extrait réel de ReadPublicCatalog (eu-west-2).
func TestPriceImporter(t *testing.T) {
	raw, err := os.ReadFile("testdata/public-catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var regions []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		region, rest, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
		if r.Method != http.MethodPost || rest != "api/v1/ReadPublicCatalog" || r.Header.Get("Authorization") != "" {
			http.Error(w, "unexpected "+r.URL.String(), http.StatusNotFound)
			return
		}
		regions = append(regions, region)
		_, _ = w.Write(raw)
	}))
	defer srv.Close()

	f, err := PriceImporter{Regions: []string{"eu-west-2", "cloudgouv-eu-west-1"}, Endpoint: srv.URL + "/%s"}.Fetch(context.Background(), srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(regions, ",") != "eu-west-2,cloudgouv-eu-west-1" {
		t.Fatalf("regions called: %v", regions)
	}
	f.Normalize()
	got := map[string]catalogs.Item{}
	for _, it := range f.Items {
		got[it.SKU+"@"+it.Region] = it
	}
	want := map[string]struct{ unit, price string }{
		"compute.vcpu.v6-p2":                    {model.UnitVCPUHour, "0.035"},
		"compute.vcpu.v7-p1":                    {model.UnitVCPUHour, "0.043"},
		"compute.ram_gb":                        {model.UnitGBHour, "0.005"},
		"storage.volume.standard":               {model.UnitGBMonth, "0.039"},
		"storage.volume.gp2":                    {model.UnitGBMonth, "0.11"},
		"storage.volume.io1":                    {model.UnitGBMonth, "0.13"},
		"storage.snapshot":                      {model.UnitGBMonth, "0.055"},
		"storage.object.standard":               {model.UnitGBMonth, "0.025"},
		"network.ip.floating":                   {model.UnitHour, "0.005"},
		"network.lb":                            {model.UnitHour, "0.03"},
		"network.nat_gateway":                   {model.UnitHour, "0.05"},
		"network.vpn":                           {model.UnitHour, "0.03"},
		"k8s.control_plane.cp.3.masters.medium": {model.UnitHour, "0.26"},
		"k8s.control_plane.cp.mono.master":      {model.UnitHour, "0.04"},
		"gpu.nvidia-a100":                       {model.UnitHour, "2"},
		"license.windows":                       {model.UnitHour, "0.121"},
	}
	for _, region := range []string{"eu-west-2", "cloudgouv-eu-west-1"} {
		for sku, w := range want {
			it, ok := got[sku+"@"+region]
			if !ok {
				t.Errorf("missing %s@%s", sku, region)
				continue
			}
			if it.Unit != w.unit || it.Price.String() != w.price {
				t.Errorf("%s@%s: got %s %s, want %s %s", sku, region, it.Price, it.Unit, w.price, w.unit)
			}
		}
	}
	if len(got) != 2*len(want) {
		for k := range got {
			if _, ok := want[strings.Split(k, "@")[0]]; !ok {
				t.Errorf("unexpected item %s", k)
			}
		}
	}
}

func TestTinaClass(t *testing.T) {
	m := tinaClass.FindStringSubmatch("tinav6.c2r4p2")
	if m == nil || "v"+m[1]+"-p"+m[2] != "v6-p2" {
		t.Fatalf("tinav6.c2r4p2: %v", m)
	}
	if tinaClass.MatchString("m4.large") {
		t.Fatal("AWS-compatible types have no Tina class")
	}
}
