// Package whatif simule l'impact mensuel de changements d'infrastructure
// (M-09) : ajout de nodes, changement de gamme, migration d'un cloud à un autre.
// Les montants sont décimaux et calculés à partir des grilles versionnées.
package whatif

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/costengine"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/money"
	"github.com/kairn-io/kairn/pkg/pricing"
)

// Types de scénario.
const (
	KindAddNodes     = "add_nodes"
	KindChangeFlavor = "change_flavor"
	KindMigrate      = "migrate"
	KindRemove       = "remove"
)

// Scenario décrit une hypothèse.
type Scenario struct {
	Kind           string   `json:"kind" enum:"add_nodes,change_flavor,migrate,remove"`
	Provider       string   `json:"provider,omitempty" doc:"Fournisseur (add_nodes) ou source (migrate)"`
	Region         string   `json:"region,omitempty"`
	Flavor         string   `json:"flavor,omitempty" doc:"Gamme ajoutée ou cible"`
	Count          int      `json:"count,omitempty" minimum:"0" maximum:"10000"`
	ResourceIDs    []string `json:"resource_ids,omitempty" doc:"Ressources concernées (change_flavor, remove, migrate)"`
	TargetProvider string   `json:"target_provider,omitempty" doc:"Fournisseur cible (migrate)"`
}

// Line détaille l'impact sur une ressource.
type Line struct {
	ResourceID string          `json:"resource_id,omitempty"`
	Name       string          `json:"name"`
	From       string          `json:"from"`
	To         string          `json:"to"`
	Current    decimal.Decimal `json:"current_monthly"`
	Projected  decimal.Decimal `json:"projected_monthly"`
	Delta      decimal.Decimal `json:"delta_monthly"`
}

// Result est le résultat d'une simulation.
type Result struct {
	Currency         string          `json:"currency"`
	CurrentMonthly   decimal.Decimal `json:"current_monthly"`
	ProjectedMonthly decimal.Decimal `json:"projected_monthly"`
	DeltaMonthly     decimal.Decimal `json:"delta_monthly"`
	DeltaPercent     decimal.Decimal `json:"delta_percent"`
	Lines            []Line          `json:"lines"`
	Unmapped         []string        `json:"unmapped"`
	Assumptions      []string        `json:"assumptions"`
}

// Input regroupe le contexte de simulation.
type Input struct {
	Book      *pricing.Book
	Resources []model.Resource // état courant
	Currency  string
	Rates     costengine.RateFunc
	Now       time.Time
	// BaselineMonthly est le coût mensuel courant de l'organisation (prévision ou dernier mois).
	BaselineMonthly decimal.Decimal
}

func monthly(h decimal.Decimal) decimal.Decimal { return h.Mul(money.HoursPerMonth) }

// Run applique les scénarios dans l'ordre.
func Run(in Input, scenarios []Scenario) (Result, error) {
	res := Result{Currency: in.Currency, CurrentMonthly: in.BaselineMonthly.Round(2), Lines: []Line{}, Unmapped: []string{},
		Assumptions: []string{"730 heures par mois", "prix des grilles valides à la date de simulation", "hors remises et engagements"}}
	byID := map[string]model.Resource{}
	for _, r := range in.Resources {
		byID[r.ID] = r
	}
	delta := decimal.Zero
	for _, sc := range scenarios {
		switch sc.Kind {
		case KindAddNodes:
			if sc.Count <= 0 || sc.Flavor == "" || sc.Provider == "" {
				return res, fmt.Errorf("add_nodes requires provider, flavor and count")
			}
			r := model.Resource{Provider: sc.Provider, Region: sc.Region, Type: model.TypeInstance, Attributes: map[string]any{"flavor": sc.Flavor}}
			h, ok := costengine.EstimateHourly(in.Book, r, in.Currency, in.Rates, in.Now)
			if !ok {
				return res, fmt.Errorf("no price for %s flavor %s", sc.Provider, sc.Flavor)
			}
			add := monthly(h).Mul(decimal.NewFromInt(int64(sc.Count)))
			res.Lines = append(res.Lines, Line{Name: fmt.Sprintf("%d × %s", sc.Count, sc.Flavor), From: "", To: sc.Flavor, Projected: add.Round(2), Delta: add.Round(2)})
			delta = delta.Add(add)
		case KindChangeFlavor, KindRemove:
			for _, id := range sc.ResourceIDs {
				r, ok := byID[id]
				if !ok {
					res.Unmapped = append(res.Unmapped, id)
					continue
				}
				cur, ok := costengine.EstimateHourly(in.Book, r, in.Currency, in.Rates, in.Now)
				if !ok {
					res.Unmapped = append(res.Unmapped, r.Name)
					continue
				}
				next := decimal.Zero
				to := "supprimée"
				if sc.Kind == KindChangeFlavor {
					nr := r
					nr.Attributes = cloneAttrs(r.Attributes)
					nr.Attributes["flavor"] = sc.Flavor
					delete(nr.Attributes, "instance_type")
					h, ok := costengine.EstimateHourly(in.Book, nr, in.Currency, in.Rates, in.Now)
					if !ok {
						return res, fmt.Errorf("no price for %s flavor %s", r.Provider, sc.Flavor)
					}
					next, to = h, sc.Flavor
				}
				l := Line{ResourceID: id, Name: r.Name, From: r.Attr("flavor"), To: to, Current: monthly(cur).Round(2), Projected: monthly(next).Round(2)}
				l.Delta = l.Projected.Sub(l.Current)
				res.Lines = append(res.Lines, l)
				delta = delta.Add(monthly(next.Sub(cur)))
			}
		case KindMigrate:
			if sc.TargetProvider == "" {
				return res, fmt.Errorf("migrate requires target_provider")
			}
			targets := candidates(in.Book, sc.TargetProvider, in.Now)
			for _, r := range in.Resources {
				if sc.Provider != "" && r.Provider != sc.Provider {
					continue
				}
				if len(sc.ResourceIDs) > 0 && !contains(sc.ResourceIDs, r.ID) {
					continue
				}
				cur, ok := costengine.EstimateHourly(in.Book, r, in.Currency, in.Rates, in.Now)
				if !ok {
					continue
				}
				nr, label, ok := equivalent(r, sc.TargetProvider, targets)
				if !ok {
					res.Unmapped = append(res.Unmapped, r.Name)
					continue
				}
				next, ok := costengine.EstimateHourly(in.Book, nr, in.Currency, in.Rates, in.Now)
				if !ok {
					res.Unmapped = append(res.Unmapped, r.Name)
					continue
				}
				l := Line{ResourceID: r.ID, Name: r.Name, From: r.Provider + " " + describe(r), To: sc.TargetProvider + " " + label,
					Current: monthly(cur).Round(2), Projected: monthly(next).Round(2)}
				l.Delta = l.Projected.Sub(l.Current)
				res.Lines = append(res.Lines, l)
				delta = delta.Add(monthly(next.Sub(cur)))
			}
			res.Assumptions = append(res.Assumptions, "équivalence : gamme la moins chère offrant au moins les mêmes vCPU et la même RAM ; stockage de même classe")
		default:
			return res, fmt.Errorf("unknown scenario %q", sc.Kind)
		}
	}
	sort.SliceStable(res.Lines, func(i, j int) bool { return res.Lines[i].Delta.Abs().GreaterThan(res.Lines[j].Delta.Abs()) })
	if len(res.Lines) > 200 {
		res.Lines = res.Lines[:200]
	}
	res.DeltaMonthly = delta.Round(2)
	res.ProjectedMonthly = in.BaselineMonthly.Add(delta).Round(2)
	if in.BaselineMonthly.IsPositive() {
		res.DeltaPercent = delta.Div(in.BaselineMonthly).Mul(decimal.NewFromInt(100)).Round(1)
	}
	return res, nil
}

func cloneAttrs(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func describe(r model.Resource) string {
	switch r.Type {
	case model.TypeInstance:
		if f := r.Attr("flavor"); f != "" {
			return f
		}
		return r.Attr("instance_type")
	case model.TypeVolume:
		return r.Attr("size_gb") + " Go " + r.Attr("volume_type")
	}
	return r.Type
}

type flavorCandidate struct {
	sku          string
	vcpus, ramGB decimal.Decimal
	price        decimal.Decimal
}

// candidates liste les gammes d'un fournisseur, triées par prix croissant.
func candidates(book *pricing.Book, provider string, at time.Time) []flavorCandidate {
	var out []flavorCandidate
	for _, m := range book.Items(provider, at) {
		if !strings.HasPrefix(m.Item.SKU, "compute.flavor.") {
			continue
		}
		v, err1 := decimal.NewFromString(m.Item.Attributes["vcpus"])
		ram, err2 := decimal.NewFromString(m.Item.Attributes["ram_gb"])
		if err1 != nil || err2 != nil {
			continue
		}
		out = append(out, flavorCandidate{sku: strings.TrimPrefix(m.Item.SKU, "compute.flavor."), vcpus: v, ramGB: ram, price: m.Item.Price})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if c := out[i].price.Cmp(out[j].price); c != 0 {
			return c < 0
		}
		return out[i].sku < out[j].sku
	})
	return out
}

// equivalent construit la ressource équivalente chez le fournisseur cible.
func equivalent(r model.Resource, target string, flavors []flavorCandidate) (model.Resource, string, bool) {
	nr := r
	nr.Provider, nr.Region = target, ""
	nr.Attributes = cloneAttrs(r.Attributes)
	switch r.Type {
	case model.TypeInstance:
		v, ram := r.AttrDecimal("vcpus"), r.AttrDecimal("ram_gb")
		for _, f := range flavors {
			if f.vcpus.GreaterThanOrEqual(v) && f.ramGB.GreaterThanOrEqual(ram) {
				nr.Attributes["flavor"] = f.sku
				delete(nr.Attributes, "instance_type")
				return nr, f.sku, true
			}
		}
		return nr, "", false
	case model.TypeVolume:
		delete(nr.Attributes, "volume_type") // classe par défaut du fournisseur cible
		return nr, r.Attr("size_gb") + " Go", true
	case model.TypeSnapshot, model.TypeIP, model.TypeLoadBalancer, model.TypeBucket:
		return nr, r.Type, true
	}
	return nr, "", false
}
