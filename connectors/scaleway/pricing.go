package scaleway

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/pricing/catalogs"
)

// PriceImporter importe la grille publique Scaleway (M-03) depuis le catalogue
// de produits public, sans authentification :
// GET /product-catalog/v2alpha1/public-catalog/products (paginé).
//
// Les prix « par Go » du stockage sont horaires ; ils sont convertis en prix
// par Go et par mois (730 h). Les nœuds de load balancer et les IP sont
// facturés à l'heure.
type PriceImporter struct {
	BaseURL  string // défaut : https://api.scaleway.com
	PageSize int    // défaut : 1000
}

func init() { catalogs.RegisterImporter(PriceImporter{}) }

// Provider implémente catalogs.Importer.
func (PriceImporter) Provider() string { return "scaleway" }

type scwProduct struct {
	SKU             string `json:"sku"`
	ProductCategory string `json:"product_category"`
	Product         string `json:"product"`
	Status          string `json:"status"`
	Locality        struct {
		Zone   string `json:"zone"`
		Region string `json:"region"`
	} `json:"locality"`
	Price *struct {
		RetailPrice *struct {
			CurrencyCode string      `json:"currency_code"`
			Units        json.Number `json:"units"`
			Nanos        json.Number `json:"nanos"`
		} `json:"retail_price"`
	} `json:"price"`
	UnitOfMeasure struct {
		Unit string      `json:"unit"`
		Size json.Number `json:"size"`
	} `json:"unit_of_measure"`
	Properties struct {
		Hardware *struct {
			CPU struct {
				Virtual struct {
					Count json.Number `json:"count"`
				} `json:"virtual"`
			} `json:"cpu"`
			RAM struct {
				Size json.Number `json:"size"`
			} `json:"ram"`
		} `json:"hardware"`
		Instance *struct {
			Range   string `json:"range"`
			OfferID string `json:"offer_id"`
		} `json:"instance"`
		ObjectStorage *struct {
			Class *struct {
				StorageClass string `json:"storage_class"`
			} `json:"class"`
		} `json:"object_storage"`
		LoadBalancer *struct {
			Node *struct {
				OfferID string `json:"offer_id"`
			} `json:"node"`
		} `json:"load_balancer"`
	} `json:"properties"`
}

// unitPrice renvoie le prix d'une unité (units + nanos, divisé par la taille de l'unité).
func (p scwProduct) unitPrice() (decimal.Decimal, string, bool) {
	if p.Price == nil || p.Price.RetailPrice == nil || p.Price.RetailPrice.CurrencyCode == "" {
		return decimal.Zero, "", false
	}
	rp := p.Price.RetailPrice
	units, err1 := decimal.NewFromString(numOr0(rp.Units))
	nanos, err2 := strconv.ParseInt(numOr0(rp.Nanos), 10, 64)
	if err1 != nil || err2 != nil {
		return decimal.Zero, "", false
	}
	v := units.Add(decimal.New(nanos, -9))
	if size, err := decimal.NewFromString(numOr0(p.UnitOfMeasure.Size)); err == nil && size.GreaterThan(decimal.NewFromInt(1)) {
		v = v.Div(size)
	}
	if v.IsNegative() {
		return decimal.Zero, "", false
	}
	return v, rp.CurrencyCode, true
}

func numOr0(n json.Number) string {
	if n == "" {
		return "0"
	}
	return n.String()
}

// Fetch implémente catalogs.Importer.
func (p PriceImporter) Fetch(ctx context.Context, hc *http.Client) (catalogs.File, error) {
	base := strings.TrimRight(p.BaseURL, "/")
	if base == "" {
		base = "https://api.scaleway.com"
	}
	size := p.PageSize
	if size <= 0 {
		size = 1000
	}
	var all []scwProduct
	for page := 1; page <= 1000; page++ {
		var resp struct {
			Products   []scwProduct `json:"products"`
			TotalCount json.Number  `json:"total_count"`
		}
		url := fmt.Sprintf("%s/product-catalog/v2alpha1/public-catalog/products?page_size=%d&page=%d", base, size, page)
		if err := catalogs.FetchJSON(ctx, hc, http.MethodGet, url, nil, &resp); err != nil {
			return catalogs.File{}, err
		}
		all = append(all, resp.Products...)
		total, _ := strconv.Atoi(resp.TotalCount.String())
		if len(resp.Products) == 0 || len(all) >= total {
			break
		}
	}
	f := catalogs.File{
		Provider: "scaleway",
		Source:   "scaleway-public-catalog:v2alpha1",
		Note:     "Grille publique Scaleway importée du catalogue de produits public, prix HT.",
	}
	// Les offres disponibles passent avant les offres retirées pour un même SKU.
	for _, retired := range []bool{false, true} {
		for _, prod := range all {
			if (prod.Status == "retired") != retired {
				continue
			}
			it, cur, ok := scwItem(prod)
			if !ok {
				continue
			}
			if f.Currency == "" {
				f.Currency = cur
			}
			if cur != f.Currency {
				it.Currency = cur
			}
			f.Items = append(f.Items, it)
		}
	}
	return f, nil
}

var gib = decimal.NewFromBigInt(big.NewInt(1<<30), 0)

// scwItem convertit un produit du catalogue en article de grille Kairn.
func scwItem(p scwProduct) (catalogs.Item, string, bool) {
	price, cur, ok := p.unitPrice()
	if !ok {
		return catalogs.Item{}, "", false
	}
	unit := p.UnitOfMeasure.Unit
	zone, region := p.Locality.Zone, p.Locality.Region
	props := p.Properties
	segs := strings.Split(strings.Trim(p.SKU, "/"), "/")
	item := func(sku, where, u string, v decimal.Decimal, attrs map[string]string) (catalogs.Item, string, bool) {
		return catalogs.Item{SKU: sku, Region: where, Unit: u, Price: v, Attributes: attrs}, cur, true
	}
	storage := func(sku, where string) (catalogs.Item, string, bool) {
		if unit != "gigabyte" {
			return catalogs.Item{}, "", false
		}
		return item(sku, where, model.UnitGBMonth, catalogs.PerGBMonth(price), nil)
	}
	switch {
	case props.Instance != nil && props.Instance.OfferID != "" && unit == "hour" && zone != "":
		if p.Status == "retired" && price.IsZero() {
			return catalogs.Item{}, "", false
		}
		attrs := map[string]string{}
		if r := props.Instance.Range; r != "" {
			attrs["family"] = r
		}
		if hw := props.Hardware; hw != nil {
			if c := hw.CPU.Virtual.Count.String(); c != "" && c != "0" {
				attrs["vcpus"] = c
			}
			if b, err := decimal.NewFromString(numOr0(hw.RAM.Size)); err == nil && b.IsPositive() {
				attrs["ram_gb"] = b.Div(gib).Round(2).String()
			}
		}
		return item("compute.flavor."+props.Instance.OfferID, zone, model.UnitHour, price, attrs)
	case props.ObjectStorage != nil && props.ObjectStorage.Class != nil && region != "":
		return storage("storage.object."+props.ObjectStorage.Class.StorageClass, region)
	case props.LoadBalancer != nil && props.LoadBalancer.Node != nil && zone != "":
		return item("network.lb."+strings.ToUpper(props.LoadBalancer.Node.OfferID), zone, model.UnitHour, price, nil)
	case len(segs) >= 3 && segs[0] == "storage" && segs[1] == "block" && zone != "":
		switch segs[2] {
		case "volume-low-latency-5k":
			return storage("storage.volume.sbs_5k", zone)
		case "volume-low-latency-15k":
			return storage("storage.volume.sbs_15k", zone)
		case "volume-bssd":
			return storage("storage.volume.b_ssd", zone)
		case "snapshot":
			return storage("storage.snapshot", zone)
		}
	case len(segs) >= 3 && segs[0] == "instance" && segs[1] == "volume" && zone != "":
		return storage("storage.volume."+segs[2], zone) // l_ssd, scratch
	case len(segs) >= 3 && segs[0] == "k8s" && region != "" && unit == "hour":
		// /k8s/control-plane/<region> (Kapsule mutualisé), /k8s/control-plane/kapsule-dedicated-8/<region>,
		// /k8s/control-plane/multicloud-dedicated-4/<region>, /k8s/multicloud/control-plane/<region>.
		switch {
		case segs[1] == "control-plane" && len(segs) == 3:
			return item("k8s.control_plane.free", region, model.UnitHour, price, nil)
		case segs[1] == "control-plane" && len(segs) == 4:
			return item("k8s.control_plane."+strings.TrimPrefix(segs[2], "kapsule-"), region, model.UnitHour, price, nil)
		case segs[1] == "multicloud" && segs[2] == "control-plane":
			return item("k8s.control_plane.multicloud", region, model.UnitHour, price, nil)
		}
	}
	return catalogs.Item{}, "", false
}
