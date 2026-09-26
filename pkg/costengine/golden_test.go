package costengine

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/pricing"
)

// Tests « golden » (CLAUDE.md §10) : chaque fichier testdata/golden/<cas>.input.json
// est calculé et comparé ligne à ligne, au centime et à 6 décimales, avec
// <cas>.golden.json. Toute modification de résultat doit être justifiée dans la PR :
//
//	go test ./pkg/costengine -run TestGolden -update
var update = flag.Bool("update", false, "réécrit les fichiers golden")

type goldenInput struct {
	Day         time.Time                        `json:"day"`
	Currency    string                           `json:"currency"`
	Settings    model.OrgSettings                `json:"settings"`
	VATRate     *decimal.Decimal                 `json:"vat_rate"`
	Resources   []model.Resource                 `json:"resources"`
	Edges       []model.ResourceEdge             `json:"edges"`
	Catalogs    []pricing.Catalog                `json:"catalogs"`
	OnPrem      []model.OnPremCostModel          `json:"onprem"`
	Adjustments []model.PricingAdjustment        `json:"adjustments"`
	Rules       []model.AllocationRule           `json:"rules"`
	SharedRules []model.SharedCostRule           `json:"shared_rules"`
	Billing     []model.BillingLine              `json:"billing"`
	Rates       map[string]decimal.Decimal       `json:"rates"` // "USD/EUR" → taux
	Metrics     map[string]map[string][]*float64 `json:"metrics"`
	PriorCredit map[string]decimal.Decimal       `json:"prior_credit"`
}

type goldenMetrics map[string]map[string][]*float64

func (g goldenMetrics) Hourly(id, metric string) []float64 {
	out := make([]float64, 24)
	vals := g[id][metric]
	for i := range out {
		if i < len(vals) && vals[i] != nil {
			out[i] = *vals[i]
		} else {
			out[i] = math.NaN()
		}
	}
	return out
}

func (g goldenMetrics) DailyAvg(id, metric string) (float64, bool) {
	vals := g[id][metric]
	sum, n := 0.0, 0
	for _, v := range vals {
		if v != nil {
			sum += *v
			n++
		}
	}
	if n == 0 {
		return 0, false
	}
	return sum / float64(n), true
}

type goldenLine struct {
	ResourceID       string `json:"resource_id"`
	CostType         string `json:"cost_type"`
	SKU              string `json:"sku"`
	Source           string `json:"source"`
	AllocationNodeID string `json:"allocation_node_id"`
	SourceRef        string `json:"source_ref,omitempty"`
	CatalogVersion   string `json:"catalog_version"`
	Quantity         string `json:"quantity"`
	Amount           string `json:"amount"`
	Currency         string `json:"currency"`
}

type goldenOutput struct {
	Total    string       `json:"total"`
	Lines    []goldenLine `json:"lines"`
	Warnings []string     `json:"warnings"`
}

func runGolden(t *testing.T, path string) goldenOutput {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var gi goldenInput
	if err := json.Unmarshal(raw, &gi); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	in := Input{
		OrgID: "org-golden", Currency: gi.Currency, Settings: gi.Settings, VATRate: gi.VATRate, Day: gi.Day,
		Resources: gi.Resources, Edges: gi.Edges, Book: pricing.NewBook(gi.Catalogs), OnPrem: gi.OnPrem,
		Adjustments: gi.Adjustments, Rules: gi.Rules, SharedRules: gi.SharedRules, Billing: gi.Billing,
		Metrics: goldenMetrics(gi.Metrics), PriorCredit: gi.PriorCredit,
		Rates: func(from, to string, _ time.Time) (decimal.Decimal, error) {
			if r, ok := gi.Rates[from+"/"+to]; ok {
				return r, nil
			}
			return decimal.Zero, fmt.Errorf("no rate %s/%s", from, to)
		},
	}
	out, err := Compute(in)
	if err != nil {
		t.Fatal(err)
	}
	g := goldenOutput{Total: Total(out.Lines).StringFixed(6), Warnings: out.Warnings}
	for _, l := range out.Lines {
		g.Lines = append(g.Lines, goldenLine{
			ResourceID: l.ResourceID, CostType: l.CostType, SKU: l.SKU, Source: l.Source,
			AllocationNodeID: l.AllocationNodeID, SourceRef: l.SourceRef, CatalogVersion: l.CatalogVersion,
			Quantity: l.Quantity.StringFixed(6), Amount: l.Amount.StringFixed(6), Currency: l.Currency,
		})
	}
	return g
}

func TestGolden(t *testing.T) {
	inputs, err := filepath.Glob(filepath.Join("testdata", "golden", "*.input.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) == 0 {
		t.Fatal("no golden fixtures found")
	}
	for _, in := range inputs {
		name := strings.TrimSuffix(filepath.Base(in), ".input.json")
		t.Run(name, func(t *testing.T) {
			got := runGolden(t, in)
			goldenPath := strings.TrimSuffix(in, ".input.json") + ".golden.json"
			if *update {
				b, _ := json.MarshalIndent(got, "", "  ")
				if err := os.WriteFile(goldenPath, append(b, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			raw, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("missing golden file (run with -update): %v", err)
			}
			var want goldenOutput
			if err := json.Unmarshal(raw, &want); err != nil {
				t.Fatal(err)
			}
			if got.Total != want.Total {
				t.Errorf("total: got %s, want %s", got.Total, want.Total)
			}
			if len(got.Lines) != len(want.Lines) {
				t.Fatalf("line count: got %d, want %d", len(got.Lines), len(want.Lines))
			}
			for i := range want.Lines {
				if got.Lines[i] != want.Lines[i] {
					t.Errorf("line %d:\n got  %+v\n want %+v", i, got.Lines[i], want.Lines[i])
				}
			}
		})
	}
}
