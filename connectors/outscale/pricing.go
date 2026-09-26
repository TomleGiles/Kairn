package outscale

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/pricing/catalogs"
)

// PriceImporter importe la grille publique OUTSCALE (M-03) de chaque région
// via l'appel public ReadPublicCatalog (sans authentification).
//
// OUTSCALE tarifie les instances Tina par vCore (selon la génération et la
// performance) et par Go de RAM : la grille contient compute.vcpu.<vG-pP> et
// compute.ram_gb, que le cost-engine combine avec l'attribut cpu_class posé
// par le connecteur. Les prix sont en EUR, exprimés tels que publiés.
type PriceImporter struct {
	// Regions : régions importées (défaut : eu-west-2 et cloudgouv-eu-west-1).
	Regions []string
	// Endpoint : modèle d'URL, %s = région (défaut : https://api.%s.outscale.com).
	Endpoint string
}

func init() { catalogs.RegisterImporter(PriceImporter{}) }

// Provider implémente catalogs.Importer.
func (PriceImporter) Provider() string { return "outscale" }

type oscEntry struct {
	Category      string      `json:"Category"`
	Service       string      `json:"Service"`
	Operation     string      `json:"Operation"`
	Type          string      `json:"Type"`
	Title         string      `json:"Title"`
	SubregionName string      `json:"SubregionName"`
	UnitPrice     json.Number `json:"UnitPrice"`
}

// Fetch implémente catalogs.Importer.
func (p PriceImporter) Fetch(ctx context.Context, hc *http.Client) (catalogs.File, error) {
	regions := p.Regions
	if len(regions) == 0 {
		regions = []string{"eu-west-2", "cloudgouv-eu-west-1"}
	}
	tmpl := p.Endpoint
	if tmpl == "" {
		tmpl = "https://api.%s.outscale.com"
	}
	f := catalogs.File{
		Provider: "outscale",
		Source:   "outscale-read-public-catalog:" + strings.Join(regions, ","),
		Currency: "EUR",
		Note:     "Grille publique OUTSCALE importée de ReadPublicCatalog, prix HT à la demande.",
	}
	for _, region := range regions {
		var resp struct {
			Catalog struct {
				Entries []oscEntry `json:"Entries"`
			} `json:"Catalog"`
		}
		url := fmt.Sprintf(tmpl, region) + "/api/v1/ReadPublicCatalog"
		if err := catalogs.FetchJSON(ctx, hc, http.MethodPost, url, strings.NewReader("{}"), &resp); err != nil {
			return catalogs.File{}, fmt.Errorf("%s: %w", region, err)
		}
		for _, e := range resp.Catalog.Entries {
			if it, ok := oscItem(e, region); ok {
				f.Items = append(f.Items, it)
			}
		}
	}
	return f, nil
}

// oscItem convertit une entrée du catalogue en article de grille Kairn.
func oscItem(e oscEntry, region string) (catalogs.Item, bool) {
	price, err := decimal.NewFromString(e.UnitPrice.String())
	if err != nil || price.IsNegative() {
		return catalogs.Item{}, false
	}
	item := func(sku, unit string, attrs map[string]string) (catalogs.Item, bool) {
		return catalogs.Item{SKU: sku, Region: region, Unit: unit, Price: price, Attributes: attrs}, true
	}
	t := e.Type
	switch {
	case e.Operation == "RunInstances-OD" && strings.HasPrefix(t, "CustomCore:"):
		return item("compute.vcpu."+strings.TrimPrefix(t, "CustomCore:"), model.UnitVCPUHour, nil)
	case e.Operation == "RunInstances-OD" && t == "CustomRam":
		return item("compute.ram_gb", model.UnitGBHour, nil)
	case strings.HasPrefix(t, "BSU:VolumeUsage:"):
		return item("storage.volume."+strings.TrimPrefix(t, "BSU:VolumeUsage:"), model.UnitGBMonth, nil)
	case t == "Snapshot:Usage":
		return item("storage.snapshot", model.UnitGBMonth, nil)
	case e.Operation == "OOSStorage":
		// Une seule classe publiée (« enterprise ») : c'est la classe par défaut des buckets.
		return item("storage.object.standard", model.UnitGBMonth, map[string]string{"offer": t})
	case e.Operation == "AssociateAddress" && t == "ElasticIP:IdleAddress":
		return item("network.ip.floating", model.UnitHour, nil)
	case t == "LBU:Usage":
		return item("network.lb", model.UnitHour, nil)
	case t == "NatGatewayUsage":
		return item("network.nat_gateway", model.UnitHour, nil)
	case e.Operation == "CreateVpnConnection" && t == "ConnectionUsage":
		return item("network.vpn", model.UnitHour, nil)
	case e.Service == "OKS" && e.Operation == "ControlPlane":
		return item("k8s.control_plane."+strings.TrimPrefix(t, "dedicated:"), model.UnitHour, nil)
	case strings.HasPrefix(t, "Gpu:attach:"):
		return item("gpu."+strings.TrimPrefix(t, "Gpu:attach:"), model.UnitHour, nil)
	case e.Category == "licence" && strings.Contains(strings.ToLower(e.Title), "windows"):
		return item("license.windows", model.UnitHour, nil)
	}
	return catalogs.Item{}, false
}
