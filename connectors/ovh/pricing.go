package ovh

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/pricing/catalogs"
)

// PriceImporter importe la grille publique OVHcloud Public Cloud (M-03) depuis
// le catalogue de commande public, sans authentification :
// GET /order/catalog/public/cloud?ovhSubsidiary=FR.
//
// Seules les offres à la consommation (« .consumption ») de la zone de
// référence sont importées ; les déclinaisons 3AZ et Local Zones (suffixes
// .3AZ, .LZ…) et les forfaits mensuels sont ignorés. Les prix du catalogue
// sont exprimés en 10⁻⁸ de la devise.
type PriceImporter struct {
	BaseURL    string // défaut : https://eu.api.ovh.com/1.0
	Subsidiary string // défaut : FR (prix en EUR)
}

func init() { catalogs.RegisterImporter(PriceImporter{}) }

// Provider implémente catalogs.Importer.
func (PriceImporter) Provider() string { return "ovh" }

type ovhCatalog struct {
	CatalogID json.Number `json:"catalogId"`
	Locale    struct {
		CurrencyCode string `json:"currencyCode"`
		Subsidiary   string `json:"subsidiary"`
	} `json:"locale"`
	Addons []ovhAddon `json:"addons"`
}

type ovhAddon struct {
	PlanCode string `json:"planCode"`
	Product  string `json:"product"`
	Pricings []struct {
		Price        json.Number `json:"price"`
		Capacities   []string    `json:"capacities"`
		IntervalUnit string      `json:"intervalUnit"`
	} `json:"pricings"`
	Blobs *struct {
		Commercial *struct {
			BrickSubtype string `json:"brickSubtype"`
		} `json:"commercial"`
		Technical *struct {
			Name string `json:"name"`
			CPU  *struct {
				Cores json.Number `json:"cores"`
			} `json:"cpu"`
			Memory *struct {
				Size json.Number `json:"size"`
			} `json:"memory"`
		} `json:"technical"`
	} `json:"blobs"`
}

// consumptionPrice renvoie le prix unitaire à la consommation (premier palier).
func (a ovhAddon) consumptionPrice() (decimal.Decimal, bool) {
	for _, p := range a.Pricings {
		for _, c := range p.Capacities {
			if c != "consumption" {
				continue
			}
			n, err := strconv.ParseInt(p.Price.String(), 10, 64)
			if err != nil || n < 0 {
				return decimal.Zero, false
			}
			return decimal.New(n, -8), true
		}
	}
	return decimal.Zero, false
}

// Fetch implémente catalogs.Importer.
func (p PriceImporter) Fetch(ctx context.Context, hc *http.Client) (catalogs.File, error) {
	base := strings.TrimRight(p.BaseURL, "/")
	if base == "" {
		base = endpoints["ovh-eu"]
	}
	sub := p.Subsidiary
	if sub == "" {
		sub = "FR"
	}
	url := base + "/order/catalog/public/cloud?ovhSubsidiary=" + sub
	var cat ovhCatalog
	if err := catalogs.FetchJSON(ctx, hc, http.MethodGet, url, nil, &cat); err != nil {
		return catalogs.File{}, err
	}
	if cat.Locale.CurrencyCode == "" {
		return catalogs.File{}, fmt.Errorf("ovh catalog: missing currency")
	}
	f := catalogs.File{
		Provider: "ovh",
		Source:   "ovh-order-catalog:" + sub + ":" + cat.CatalogID.String(),
		Currency: cat.Locale.CurrencyCode,
		Note:     "Grille publique OVHcloud Public Cloud importée du catalogue de commande (" + sub + "), prix HT à la consommation.",
	}
	for _, a := range cat.Addons {
		if it, ok := ovhItem(a); ok {
			f.Items = append(f.Items, it)
		}
	}
	return f, nil
}

// ovhItem convertit une offre du catalogue en article de grille Kairn.
func ovhItem(a ovhAddon) (catalogs.Item, bool) {
	code := a.PlanCode
	price, ok := a.consumptionPrice()
	if !ok {
		return catalogs.Item{}, false
	}
	var base string
	switch {
	case strings.HasSuffix(code, ".hour.consumption"):
		base = strings.TrimSuffix(code, ".hour.consumption")
	case strings.HasSuffix(code, ".consumption"):
		base = strings.TrimSuffix(code, ".consumption")
	default:
		return catalogs.Item{}, false // forfaits mensuels, déclinaisons 3AZ / Local Zones
	}
	hour := func(sku string, attrs map[string]string) (catalogs.Item, bool) {
		return catalogs.Item{SKU: sku, Unit: model.UnitHour, Price: price, Attributes: attrs}, true
	}
	gbHour := func(sku string) (catalogs.Item, bool) {
		return catalogs.Item{SKU: sku, Unit: model.UnitGBMonth, Price: catalogs.PerGBMonth(price)}, true
	}
	switch {
	case a.Product == "publiccloud-instance" && !strings.Contains(base, "."):
		attrs := map[string]string{}
		if b := a.Blobs; b != nil {
			if b.Technical != nil && b.Technical.CPU != nil {
				attrs["vcpus"] = b.Technical.CPU.Cores.String()
			}
			if b.Technical != nil && b.Technical.Memory != nil {
				attrs["ram_gb"] = b.Technical.Memory.Size.String()
			}
			if b.Commercial != nil && b.Commercial.BrickSubtype != "" {
				attrs["family"] = b.Commercial.BrickSubtype
			}
		}
		return hour("compute.flavor."+base, attrs)
	case base == "volume.snapshot":
		return gbHour("storage.snapshot")
	case base == "snapshot":
		return gbHour("storage.instance_backup")
	case strings.HasPrefix(base, "volume.") && strings.Count(base, ".") == 1:
		return gbHour("storage.volume." + strings.TrimPrefix(base, "volume."))
	case a.Product == "publiccloud-storage" && strings.HasPrefix(base, "storage-"):
		return gbHour("storage.object." + strings.TrimPrefix(base, "storage-"))
	case base == "floatingip.floatingip":
		return hour("network.ip.floating", nil)
	case base == "publicip.ip":
		return hour("network.ip.public", nil)
	case strings.HasPrefix(base, "octavia-loadbalancer.loadbalancer-"):
		name := strings.TrimPrefix(base, "octavia-loadbalancer.loadbalancer-")
		if a.Blobs != nil && a.Blobs.Technical != nil && a.Blobs.Technical.Name != "" {
			name = a.Blobs.Technical.Name // nom de flavor Octavia (small, medium…)
		}
		return hour("network.lb."+name, nil)
	case strings.HasPrefix(base, "mks."):
		return hour("k8s.control_plane."+strings.TrimPrefix(base, "mks."), nil)
	case strings.HasPrefix(base, "gateway."):
		return hour("network.gateway."+strings.TrimPrefix(base, "gateway."), nil)
	}
	return catalogs.Item{}, false
}
