package catalogs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/money"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/tenancy"
)

// Importer récupère la grille tarifaire publique d'un fournisseur depuis son
// API publique (sans authentification ni donnée client). Chaque fournisseur
// l'implémente dans connectors/<fournisseur>/pricing.go et l'enregistre via
// RegisterImporter.
type Importer interface {
	Provider() string
	// Fetch renvoie la grille courante. Version et ValidFrom sont fixés par Import.
	Fetch(ctx context.Context, hc *http.Client) (File, error)
}

var (
	impMu     sync.RWMutex
	importers = map[string]Importer{}
)

// RegisterImporter enregistre l'importeur d'un fournisseur (appelé depuis init()).
func RegisterImporter(imp Importer) {
	impMu.Lock()
	defer impMu.Unlock()
	if _, dup := importers[imp.Provider()]; dup {
		panic("catalogs: duplicate importer for " + imp.Provider())
	}
	importers[imp.Provider()] = imp
}

// Importers liste les importeurs enregistrés, triés par fournisseur.
func Importers() []Importer {
	impMu.RLock()
	defer impMu.RUnlock()
	out := make([]Importer, 0, len(importers))
	for _, imp := range importers {
		out = append(out, imp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Provider() < out[j].Provider() })
	return out
}

// FirstValidFrom est la date d'effet de la première grille importée d'un
// fournisseur : faute d'historique officiel, elle s'applique à tout le passé
// (backfill) à la place des grilles d'exemple. Les imports suivants prennent
// effet le lendemain de leur détection (voir ADR-0006).
var FirstValidFrom = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// priceScale est la précision stockée des prix (numeric(18,6)).
const priceScale = 6

// Result décrit le résultat de l'import d'une grille.
type Result struct {
	Provider  string    `json:"provider"`
	Version   string    `json:"version"`
	Items     int       `json:"items"`
	Created   bool      `json:"created"`
	ValidFrom time.Time `json:"valid_from"`
}

// Normalize arrondit les prix à la précision stockée, retire les doublons
// (premier article conservé pour un couple SKU/région) et trie les articles.
func (f *File) Normalize() {
	seen := map[[2]string]bool{}
	out := make([]Item, 0, len(f.Items))
	for _, it := range f.Items {
		k := [2]string{it.SKU, it.Region}
		if seen[k] {
			continue
		}
		seen[k] = true
		it.Price = it.Price.Round(priceScale)
		if it.Currency == f.Currency {
			it.Currency = ""
		}
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SKU != out[j].SKU {
			return out[i].SKU < out[j].SKU
		}
		return out[i].Region < out[j].Region
	})
	f.Items = out
}

// Digest est l'empreinte du contenu tarifaire (devise et articles), indépendante
// de la date d'import : deux imports identiques produisent la même version.
func (f File) Digest() string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\n", f.Currency)
	for _, it := range f.Items {
		keys := make([]string, 0, len(it.Attributes))
		for k := range it.Attributes {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		attrs := make([]string, 0, len(keys))
		for _, k := range keys {
			attrs = append(attrs, k+"="+it.Attributes[k])
		}
		fmt.Fprintf(h, "%s|%s|%s|%s|%s|%s\n", it.SKU, it.Region, it.Unit, it.Price.String(), it.Currency, strings.Join(attrs, ","))
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// Import récupère la grille publique d'un fournisseur et l'enregistre comme
// nouvelle version si son contenu a changé. La version est dérivée du contenu
// (« pub-<empreinte> ») : l'import est idempotent et rejouable.
func Import(ctx context.Context, st store.Store, imp Importer, hc *http.Client, now time.Time) (Result, error) {
	res := Result{Provider: imp.Provider()}
	f, err := imp.Fetch(ctx, hc)
	if err != nil {
		return res, fmt.Errorf("catalogs: fetch %s: %w", imp.Provider(), err)
	}
	f.Provider = imp.Provider()
	if len(f.Items) == 0 {
		return res, fmt.Errorf("catalogs: %s: empty catalog refused", f.Provider)
	}
	if f.Currency == "" {
		return res, fmt.Errorf("catalogs: %s: missing currency", f.Provider)
	}
	f.Normalize()
	f.Version = "pub-" + f.Digest()
	res.Version, res.Items = f.Version, len(f.Items)

	sys := tenancy.WithSystem(ctx)
	existing, err := st.Pricing().ListCatalogs(sys)
	if err != nil {
		return res, fmt.Errorf("catalogs: list: %w", err)
	}
	first := true
	for _, c := range existing {
		if c.OrgID != nil || c.Provider != f.Provider || c.Source == model.CatalogSourceSample {
			continue
		}
		if c.Version == f.Version {
			res.ValidFrom = c.ValidFrom
			return res, nil // contenu inchangé
		}
		first = false
	}
	f.ValidFrom = FirstValidFrom
	if !first {
		d := now.UTC()
		f.ValidFrom = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
	}
	res.ValidFrom = f.ValidFrom
	c, items, err := f.ToCatalog()
	if err != nil {
		return res, err
	}
	if err := st.Pricing().CreateCatalog(sys, &c, items); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return res, nil // importé en parallèle par une autre instance
		}
		return res, fmt.Errorf("catalogs: store %s: %w", f.Provider, err)
	}
	res.Created = true
	return res, nil
}

// ImportAll importe les grilles des fournisseurs demandés (tous si la liste
// est vide). Un échec n'interrompt pas les autres fournisseurs.
func ImportAll(ctx context.Context, st store.Store, hc *http.Client, now time.Time, providers []string, log *slog.Logger) ([]Result, error) {
	want := map[string]bool{}
	for _, p := range providers {
		if p = strings.TrimSpace(p); p != "" {
			want[p] = true
		}
	}
	var out []Result
	var errs []error
	for _, imp := range Importers() {
		if len(want) > 0 && !want[imp.Provider()] {
			continue
		}
		r, err := Import(ctx, st, imp, hc, now)
		if err != nil {
			errs = append(errs, err)
			if log != nil {
				log.Warn("price catalog import failed", "provider", imp.Provider(), "err", err)
			}
			continue
		}
		if log != nil {
			log.Info("price catalog import", "provider", r.Provider, "version", r.Version, "items", r.Items, "created", r.Created)
		}
		out = append(out, r)
	}
	return out, errors.Join(errs...)
}

// maxCatalogBytes borne la taille d'une réponse de catalogue public.
const maxCatalogBytes = 128 << 20

// FetchJSON exécute une requête vers une API tarifaire publique et décode la
// réponse JSON (taille bornée, statut vérifié, nombres conservés en json.Number).
func FetchJSON(ctx context.Context, hc *http.Client, method, url string, body io.Reader, v any) error {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", "kairn-price-importer")
	if hc == nil {
		hc = &http.Client{Timeout: 2 * time.Minute}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("%s %s: HTTP %d", method, url, resp.StatusCode)
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxCatalogBytes))
	dec.UseNumber()
	return dec.Decode(v)
}

// PerGBMonth convertit un prix par Go et par heure en prix par Go et par mois
// (730 h, convention du cost-engine) : les prix au Go-heure ont trop de
// décimales pour la précision stockée (6).
func PerGBMonth(perGBHour decimal.Decimal) decimal.Decimal {
	return perGBHour.Mul(money.HoursPerMonth)
}
