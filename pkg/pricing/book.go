// Package pricing indexe les grilles tarifaires versionnées pour la recherche
// de prix par SKU, région et date. Une grille négociée propre à
// l'organisation est prioritaire sur la grille publique du même fournisseur.
package pricing

import (
	"sort"
	"time"

	"github.com/kairn-io/kairn/pkg/model"
)

// Catalog associe une grille et ses articles.
type Catalog struct {
	Catalog model.PriceCatalog
	Items   []model.PriceItem
}

type itemKey struct{ sku, region string }

type indexed struct {
	cat   model.PriceCatalog
	items map[itemKey]model.PriceItem
}

// Book est un index immuable de grilles tarifaires.
type Book struct {
	byProvider map[string][]indexed // trié : grilles org d'abord, puis ValidFrom décroissant
}

// NewBook construit l'index.
func NewBook(cats []Catalog) *Book {
	b := &Book{byProvider: map[string][]indexed{}}
	for _, c := range cats {
		ix := indexed{cat: c.Catalog, items: make(map[itemKey]model.PriceItem, len(c.Items))}
		for _, it := range c.Items {
			ix.items[itemKey{it.SKU, it.Region}] = it
		}
		b.byProvider[c.Catalog.Provider] = append(b.byProvider[c.Catalog.Provider], ix)
	}
	for p := range b.byProvider {
		list := b.byProvider[p]
		sort.SliceStable(list, func(i, j int) bool {
			oi, oj := list[i].cat.OrgID != nil, list[j].cat.OrgID != nil
			if oi != oj {
				return oi // grilles négociées d'abord
			}
			if !list[i].cat.ValidFrom.Equal(list[j].cat.ValidFrom) {
				return list[i].cat.ValidFrom.After(list[j].cat.ValidFrom)
			}
			return list[i].cat.Version > list[j].cat.Version
		})
	}
	return b
}

// Match est un résultat de recherche de prix.
type Match struct {
	Item    model.PriceItem
	Catalog model.PriceCatalog
}

// eligible renvoie les grilles d'un fournisseur valides à la date, dans
// l'ordre de priorité. Les grilles d'exemple sont écartées dès qu'une grille
// publique importée est valide : un prix indicatif ne doit jamais compléter
// une grille officielle.
func (b *Book) eligible(provider string, at time.Time) []indexed {
	list := b.byProvider[provider]
	official := false
	for _, ix := range list {
		if ix.cat.OrgID == nil && ix.cat.Source != model.CatalogSourceSample && !ix.cat.ValidFrom.After(at) {
			official = true
			break
		}
	}
	out := make([]indexed, 0, len(list))
	for _, ix := range list {
		if ix.cat.ValidFrom.After(at) || (official && ix.cat.OrgID == nil && ix.cat.Source == model.CatalogSourceSample) {
			continue
		}
		out = append(out, ix)
	}
	return out
}

// Lookup cherche un SKU pour un fournisseur, une région et une date.
// Ordre : grille négociée puis publique ; version la plus récente valide à la
// date ; article de la région exacte puis article toutes régions.
func (b *Book) Lookup(provider, sku, region string, at time.Time) (Match, bool) {
	for _, ix := range b.eligible(provider, at) {
		if it, ok := ix.items[itemKey{sku, region}]; ok {
			return Match{Item: it, Catalog: ix.cat}, true
		}
		if region != "" {
			if it, ok := ix.items[itemKey{sku, ""}]; ok {
				return Match{Item: it, Catalog: ix.cat}, true
			}
		}
	}
	return Match{}, false
}

// Items renvoie les articles de la grille la plus récente valide à la date,
// utile aux recommandations (recherche de flavor équivalente moins chère).
func (b *Book) Items(provider string, at time.Time) []Match {
	seen := map[itemKey]bool{}
	var out []Match
	for _, ix := range b.eligible(provider, at) {
		keys := make([]itemKey, 0, len(ix.items))
		for k := range ix.items {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i].sku != keys[j].sku {
				return keys[i].sku < keys[j].sku
			}
			return keys[i].region < keys[j].region
		})
		for _, k := range keys {
			if !seen[k] {
				seen[k] = true
				out = append(out, Match{Item: ix.items[k], Catalog: ix.cat})
			}
		}
	}
	return out
}

// Providers liste les fournisseurs indexés.
func (b *Book) Providers() []string {
	out := make([]string, 0, len(b.byProvider))
	for p := range b.byProvider {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
