// Package catalogs embarque des grilles tarifaires d'exemple (indicatives)
// utilisées par le mode démo, les tests et l'installation initiale. En
// production, les grilles publiques sont importées depuis les API des
// fournisseurs (voir importer.go et connectors/*/pricing.go) et versionnées.
package catalogs

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/pricing"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/tenancy"
)

//go:embed data/*.json
var data embed.FS

// File est le format d'échange d'une grille tarifaire (import/export JSON).
type File struct {
	Provider  string    `json:"provider"`
	Version   string    `json:"version"`
	Source    string    `json:"source"`
	Currency  string    `json:"currency"`
	ValidFrom time.Time `json:"valid_from"`
	Note      string    `json:"note,omitempty"`
	Items     []Item    `json:"items"`
}

// Item est un article d'une grille au format d'échange.
type Item struct {
	SKU        string            `json:"sku"`
	Region     string            `json:"region,omitempty"`
	Unit       string            `json:"unit"`
	Price      decimal.Decimal   `json:"price"`
	Currency   string            `json:"currency,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// ToCatalog convertit le fichier en grille et articles.
func (f File) ToCatalog() (model.PriceCatalog, []model.PriceItem, error) {
	if f.Provider == "" || f.Version == "" || f.Currency == "" {
		return model.PriceCatalog{}, nil, errors.New("catalogs: provider, version and currency are required")
	}
	c := model.PriceCatalog{Provider: f.Provider, Version: f.Version, Source: f.Source, Currency: f.Currency, ValidFrom: f.ValidFrom.UTC()}
	items := make([]model.PriceItem, 0, len(f.Items))
	for _, it := range f.Items {
		if it.SKU == "" || it.Unit == "" || it.Price.IsNegative() {
			return c, nil, fmt.Errorf("catalogs: invalid item %q", it.SKU)
		}
		cur := it.Currency
		if cur == "" {
			cur = f.Currency
		}
		attrs := it.Attributes
		if attrs == nil {
			attrs = map[string]string{}
		}
		items = append(items, model.PriceItem{SKU: it.SKU, Region: it.Region, Unit: it.Unit, Price: it.Price, Currency: cur, Attributes: attrs})
	}
	return c, items, nil
}

// Samples renvoie les grilles d'exemple embarquées.
func Samples() ([]File, error) {
	entries, err := data.ReadDir("data")
	if err != nil {
		return nil, err
	}
	var out []File
	for _, e := range entries {
		raw, err := data.ReadFile(path.Join("data", e.Name()))
		if err != nil {
			return nil, err
		}
		var f File
		if err := json.Unmarshal(raw, &f); err != nil {
			return nil, fmt.Errorf("catalogs: %s: %w", e.Name(), err)
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Provider < out[j].Provider })
	return out, nil
}

// SampleBook construit un index à partir des grilles d'exemple (tests, simulations).
func SampleBook() (*pricing.Book, error) {
	files, err := Samples()
	if err != nil {
		return nil, err
	}
	var cats []pricing.Catalog
	for i, f := range files {
		c, items, err := f.ToCatalog()
		if err != nil {
			return nil, err
		}
		c.ID = fmt.Sprintf("sample-%d", i)
		cats = append(cats, pricing.Catalog{Catalog: c, Items: items})
	}
	return pricing.NewBook(cats), nil
}

// InstallSamples importe les grilles d'exemple comme grilles publiques si
// elles sont absentes. Nécessite un contexte système.
func InstallSamples(ctx context.Context, st store.Store) (int, error) {
	files, err := Samples()
	if err != nil {
		return 0, err
	}
	ctx = tenancy.WithSystem(ctx)
	n := 0
	for _, f := range files {
		c, items, err := f.ToCatalog()
		if err != nil {
			return n, err
		}
		if err := st.Pricing().CreateCatalog(ctx, &c, items); err != nil {
			if errors.Is(err, store.ErrConflict) {
				continue
			}
			return n, fmt.Errorf("install %s: %w", f.Provider, err)
		}
		n++
	}
	return n, nil
}
