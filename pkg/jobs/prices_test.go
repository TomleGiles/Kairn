package jobs

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/bus"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/pricing/catalogs"
)

type testImporter struct{ price string }

func (testImporter) Provider() string { return "jobs-test" }
func (i *testImporter) Fetch(context.Context, *http.Client) (catalogs.File, error) {
	return catalogs.File{Currency: "EUR", Source: "test", Items: []catalogs.Item{
		{SKU: "compute.flavor.x", Unit: model.UnitHour, Price: decimal.RequireFromString(i.price)},
	}}, nil
}

var jobsImporter = &testImporter{price: "0.1"}

func init() { catalogs.RegisterImporter(jobsImporter) }

func TestSchedulerImportsPrices(t *testing.T) {
	e := newEnv(t)
	rec := &recorder{}
	e.w.Bus = rec
	e.w.PriceImport = []string{"jobs-test"}
	sched := &Scheduler{Worker: e.w}
	orgs := []string{"org-a", "org-b"}

	// Première grille officielle : elle remplace l'exemple sur le passé → recalcul par organisation.
	res := sched.ImportPrices(context.Background(), e.now, orgs)
	if len(res) != 1 || !res[0].Created || !res[0].ValidFrom.Equal(catalogs.FirstValidFrom) {
		t.Fatalf("first import: %+v", res)
	}
	if rec.count(bus.SubjectCostRequested) != len(orgs) {
		t.Fatalf("backdated catalog must trigger a recompute per org: %v", rec.subs)
	}
	// Nouvelle version : effet le lendemain, le recalcul horaire suffit.
	jobsImporter.price = "0.2"
	before := len(rec.subs)
	res = sched.ImportPrices(context.Background(), e.now.Add(time.Hour), orgs)
	if len(res) != 1 || !res[0].Created || len(rec.subs) != before {
		t.Fatalf("new version must not trigger an extra recompute: %+v %v", res, rec.subs[before:])
	}
	// Import désactivé.
	e.w.PriceImport = nil
	if res := sched.ImportPrices(context.Background(), e.now, orgs); res != nil {
		t.Fatalf("disabled import: %+v", res)
	}
}

func TestReportBrand(t *testing.T) {
	for in, want := range map[any]string{nil: "Kairn", "": "Kairn", "  Infogérance  ": "Infogérance", "A\r\nBcc: x": "ABcc: x", 42: "Kairn"} {
		if got := reportBrand(map[string]any{"brand": in}); got != want {
			t.Errorf("%v: got %q, want %q", in, got, want)
		}
	}
}
