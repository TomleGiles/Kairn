package catalogs

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/pricing"
	"github.com/kairn-io/kairn/pkg/store/memstore"
	"github.com/kairn-io/kairn/pkg/tenancy"
)

type fakeImporter struct {
	provider string
	items    []Item
	err      error
}

func (f *fakeImporter) Provider() string { return f.provider }
func (f *fakeImporter) Fetch(context.Context, *http.Client) (File, error) {
	if f.err != nil {
		return File{}, f.err
	}
	return File{Currency: "EUR", Source: "fake-api", Items: append([]Item(nil), f.items...)}, nil
}

func it(sku, price string) Item {
	return Item{SKU: sku, Unit: model.UnitHour, Price: decimal.RequireFromString(price)}
}

func TestImportVersioning(t *testing.T) {
	ctx := context.Background()
	st := memstore.New()
	if _, err := InstallSamples(ctx, st); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 26, 10, 30, 0, 0, time.UTC)
	imp := &fakeImporter{provider: "ovh", items: []Item{it("compute.flavor.b2-7", "0.0709"), it("compute.flavor.b2-7", "9"), it("network.ip.floating", "0.00270004")}}

	first, err := Import(ctx, st, imp, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Created || !first.ValidFrom.Equal(FirstValidFrom) || !strings.HasPrefix(first.Version, "pub-") || first.Items != 2 {
		t.Fatalf("first import must be created, valid for the whole past, deduplicated: %+v", first)
	}
	again, err := Import(ctx, st, imp, nil, now.Add(24*time.Hour))
	if err != nil || again.Created || again.Version != first.Version {
		t.Fatalf("unchanged content must not create a version: %+v %v", again, err)
	}
	imp.items[0] = it("compute.flavor.b2-7", "0.0750")
	changed, err := Import(ctx, st, imp, nil, now)
	if err != nil || !changed.Created || changed.Version == first.Version {
		t.Fatalf("new prices must create a version: %+v %v", changed, err)
	}
	if want := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC); !changed.ValidFrom.Equal(want) {
		t.Fatalf("a new version takes effect the next UTC day, got %s", changed.ValidFrom)
	}

	// Les prix sont stockés à 6 décimales et l'index ignore la grille d'exemple.
	cats, err := st.Pricing().ListCatalogs(tenancy.WithSystem(ctx))
	if err != nil {
		t.Fatal(err)
	}
	var book []pricing.Catalog
	for _, c := range cats {
		items, err := st.Pricing().Items(tenancy.WithSystem(ctx), c.ID)
		if err != nil {
			t.Fatal(err)
		}
		book = append(book, pricing.Catalog{Catalog: c, Items: items})
	}
	b := pricing.NewBook(book)
	m, ok := b.Lookup("ovh", "network.ip.floating", "GRA11", now)
	if !ok || m.Item.Price.String() != "0.0027" || m.Catalog.Version != first.Version {
		t.Fatalf("lookup today: %+v %v", m, ok)
	}
	if m, ok := b.Lookup("ovh", "compute.flavor.b2-7", "", now.AddDate(0, 0, 2)); !ok || m.Item.Price.String() != "0.075" {
		t.Fatalf("lookup after the change: %+v %v", m, ok)
	}
	if _, ok := b.Lookup("ovh", "storage.volume.classic", "", now); ok {
		t.Fatal("a SKU missing from the official catalog must not fall back to the sample catalog")
	}
}

func TestImportRejectsInvalidCatalogs(t *testing.T) {
	ctx := context.Background()
	st := memstore.New()
	now := time.Now()
	if _, err := Import(ctx, st, &fakeImporter{provider: "empty"}, nil, now); err == nil {
		t.Fatal("empty catalog must be refused")
	}
	boom := errors.New("boom")
	if _, err := Import(ctx, st, &fakeImporter{provider: "down", err: boom}, nil, now); !errors.Is(err, boom) {
		t.Fatalf("fetch error must be wrapped: %v", err)
	}
	bad := &fakeImporter{provider: "neg", items: []Item{it("x", "-1")}}
	if _, err := Import(ctx, st, bad, nil, now); err == nil {
		t.Fatal("negative price must be refused")
	}
}

func TestImportAllContinuesOnError(t *testing.T) {
	RegisterImporter(&fakeImporter{provider: "zz-ok", items: []Item{it("a", "1")}})
	RegisterImporter(&fakeImporter{provider: "zz-down", err: errors.New("unreachable")})
	st := memstore.New()
	res, err := ImportAll(context.Background(), st, nil, time.Now(), []string{"zz-down", " zz-ok "}, nil)
	if err == nil || len(res) != 1 || res[0].Provider != "zz-ok" || !res[0].Created {
		t.Fatalf("one failure must not block the others: %+v %v", res, err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate importer must panic")
		}
	}()
	RegisterImporter(&fakeImporter{provider: "zz-ok"})
}

func TestFetchJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("unexpected request %s %s", r.Method, r.Header.Get("Content-Type"))
			}
			_, _ = w.Write([]byte(`{"price": 0.123456789}`))
		default:
			http.Error(w, "nope", http.StatusBadGateway)
		}
	}))
	defer srv.Close()
	var v struct{ Price any }
	if err := FetchJSON(context.Background(), srv.Client(), http.MethodPost, srv.URL+"/ok", strings.NewReader("{}"), &v); err != nil {
		t.Fatal(err)
	}
	if n, ok := v.Price.(interface{ String() string }); !ok || n.String() != "0.123456789" {
		t.Fatalf("numbers must be kept exact (json.Number), got %#v", v.Price)
	}
	if err := FetchJSON(context.Background(), srv.Client(), http.MethodGet, srv.URL+"/ko", nil, &v); err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("HTTP errors must be reported: %v", err)
	}
	if got := PerGBMonth(decimal.RequireFromString("0.0000972")); got.String() != "0.070956" {
		t.Fatalf("PerGBMonth: %s", got)
	}
}
