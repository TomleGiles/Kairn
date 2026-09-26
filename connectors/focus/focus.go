// Package focus importe les exports de facturation au format FinOps FOCUS
// (FinOps Open Cost and Usage Specification, v1.x) : AWS (CUR 2.0 / Data
// Exports FOCUS), Azure (Cost Management, export FOCUS), Google Cloud
// (BigQuery → export FOCUS), ou tout fournisseur conforme. Connecteur
// secondaire pour les environnements hybrides (M-01).
//
// Les fichiers CSV (éventuellement gzip) sont lus sur un stockage
// S3-compatible (préfixe) ou à une URL HTTPS. Chaque ResourceId facturé
// devient une ressource d'inventaire (étiquettes FOCUS → labels) et chaque
// ligne une ligne de facture rattachée.
package focus

import (
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/connectors/internal/rest"
	"github.com/kairn-io/kairn/pkg/connector"
	"github.com/kairn-io/kairn/pkg/model"
)

// Type est l'identifiant du connecteur.
const Type = "focus"

var permissions = []connector.Permission{
	{Scope: "s3:ListBucket, s3:GetObject sur le préfixe d'export", Description: "Lecture des fichiers FOCUS déposés par le fournisseur."},
}

func init() {
	connector.Register(connector.TypeInfo{
		Type: Type, DisplayName: "Export FOCUS (AWS, Azure, Google Cloud…)", Category: connector.CategoryBilling, Provider: "focus",
		Resources: []string{model.TypeService}, DefaultInterval: 6 * time.Hour, Billing: true, DocsURL: "/docs/connectors/focus", Permissions: permissions,
		Fields: []connector.Field{
			{Name: "provider", Label: "Fournisseur", Default: "aws", Help: "aws, azure, gcp… (libellé des coûts importés)"},
			{Name: "source", Label: "Emplacement", Required: true, Help: "s3://bucket/prefix ou https://…/export.csv.gz"},
			{Name: "s3_endpoint", Label: "Point d'accès S3", Default: "s3.amazonaws.com", Help: "ex. s3.gra.io.cloud.ovh.net pour un bucket OVHcloud"},
			{Name: "s3_region", Label: "Région S3", Default: "eu-west-3"},
			{Name: "access_key", Label: "Access key (lecture seule)"},
			{Name: "secret_key", Label: "Secret key", Secret: true},
			{Name: "cost_column", Label: "Colonne de coût", Default: "EffectiveCost", Help: "EffectiveCost (remises et engagements amortis) ou BilledCost"},
		},
	}, New)
}

// Conn est une instance du connecteur.
type Conn struct {
	provider   string
	source     *url.URL
	costColumn string
	s3         *minio.Client
	http       *http.Client
	now        func() time.Time
}

// New construit le connecteur.
func New(cfg connector.Config) (connector.Connector, error) {
	src, err := url.Parse(cfg.Setting("source", ""))
	if err != nil || src.Host == "" || (src.Scheme != "s3" && src.Scheme != "https") {
		return nil, fmt.Errorf("%w: source (s3://bucket/prefix or https://…)", connector.ErrMissingConfig)
	}
	cost := cfg.Setting("cost_column", "EffectiveCost")
	if cost != "EffectiveCost" && cost != "BilledCost" {
		return nil, fmt.Errorf("cost_column must be EffectiveCost or BilledCost")
	}
	hc, err := rest.NewHTTP(rest.Options{Timeout: 10 * time.Minute})
	if err != nil {
		return nil, err
	}
	c := &Conn{provider: strings.ToLower(cfg.Setting("provider", "aws")), source: src, costColumn: cost, http: hc,
		now: func() time.Time { return time.Now().UTC() }}
	if src.Scheme == "s3" {
		if err := cfg.Require("access_key", "secret_key"); err != nil {
			return nil, err
		}
		c.s3, err = minio.New(cfg.Setting("s3_endpoint", "s3.amazonaws.com"), &minio.Options{
			Creds: credentials.NewStaticV4(cfg.Setting("access_key", ""), cfg.Secret("secret_key"), ""), Secure: true, Region: cfg.Setting("s3_region", "eu-west-3"),
		})
		if err != nil {
			return nil, fmt.Errorf("s3 client: %w", err)
		}
	}
	return c, nil
}

func (c *Conn) Type() string                                { return Type }
func (c *Conn) RequiredPermissions() []connector.Permission { return permissions }

func (c *Conn) SyncMetrics(context.Context, connector.TimeWindow) (<-chan connector.MetricPoint, error) {
	return nil, connector.ErrNotSupported
}

// Validate lit l'en-tête du premier fichier et vérifie qu'il s'agit d'un export FOCUS.
func (c *Conn) Validate(ctx context.Context, _ connector.Config) error {
	files, err := c.files(ctx)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return errors.New("no FOCUS file (.csv or .csv.gz) found at the configured location")
	}
	rc, err := c.open(ctx, files[0])
	if err != nil {
		return fmt.Errorf("%s: %w", files[0], err)
	}
	defer rc.Close()
	head, err := csv.NewReader(rc).Read()
	if err != nil {
		return fmt.Errorf("%s: %w", files[0], err)
	}
	cols := map[string]bool{}
	for _, h := range head {
		cols[strings.TrimPrefix(strings.TrimSpace(h), "\ufeff")] = true
	}
	for _, req := range []string{"ChargePeriodStart", c.costColumn, "BillingCurrency"} {
		if !cols[req] {
			return fmt.Errorf("not a FOCUS export: missing column %s", req)
		}
	}
	return nil
}

func (c *Conn) Health(ctx context.Context) connector.HealthStatus {
	h := connector.HealthStatus{Status: connector.HealthOK, CheckedAt: c.now()}
	if err := c.Validate(ctx, connector.Config{}); err != nil {
		h.Status, h.Message = connector.HealthDown, err.Error()
	}
	return h
}

// files liste les fichiers d'export à lire.
func (c *Conn) files(ctx context.Context) ([]string, error) {
	if c.source.Scheme == "https" {
		return []string{c.source.String()}, nil
	}
	prefix := strings.TrimPrefix(c.source.Path, "/")
	var out []string
	for obj := range c.s3.ListObjects(ctx, c.source.Host, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if obj.Err != nil {
			return nil, fmt.Errorf("list %s: %w", c.source, obj.Err)
		}
		if strings.HasSuffix(obj.Key, ".csv") || strings.HasSuffix(obj.Key, ".csv.gz") {
			out = append(out, obj.Key)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (c *Conn) open(ctx context.Context, name string) (io.ReadCloser, error) {
	var rc io.ReadCloser
	if c.source.Scheme == "https" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, name, nil)
		if err != nil {
			return nil, err
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
				return nil, fmt.Errorf("%w: HTTP %d", connector.ErrPermission, resp.StatusCode)
			}
			return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		rc = resp.Body
	} else {
		obj, err := c.s3.GetObject(ctx, c.source.Host, name, minio.GetObjectOptions{})
		if err != nil {
			return nil, err
		}
		rc = obj
	}
	if strings.HasSuffix(strings.SplitN(name, "?", 2)[0], ".gz") {
		gz, err := gzip.NewReader(rc)
		if err != nil {
			rc.Close()
			return nil, err
		}
		return struct {
			io.Reader
			io.Closer
		}{gz, rc}, nil
	}
	return rc, nil
}

// row est une ligne FOCUS normalisée.
type row struct {
	chargeStart, chargeEnd time.Time
	cost, quantity         decimal.Decimal
	currency, unit         string
	resourceID, resource   string
	service, category, sku string
	chargeCategory, region string
	tags                   map[string]string
}

// scan lit toutes les lignes des fichiers et appelle fn pour chacune.
func (c *Conn) scan(ctx context.Context, fn func(row) bool) error {
	files, err := c.files(ctx)
	if err != nil {
		return err
	}
	for _, f := range files {
		if err := c.scanFile(ctx, f, fn); err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
	}
	return nil
}

func (c *Conn) scanFile(ctx context.Context, name string, fn func(row) bool) error {
	rc, err := c.open(ctx, name)
	if err != nil {
		return err
	}
	defer rc.Close()
	r := csv.NewReader(rc)
	r.ReuseRecord = true
	head, err := r.Read()
	if err != nil {
		return err
	}
	col := map[string]int{}
	for i, h := range head {
		col[strings.TrimPrefix(strings.TrimSpace(h), "\ufeff")] = i
	}
	for _, req := range []string{"ChargePeriodStart", c.costColumn, "BillingCurrency"} {
		if _, ok := col[req]; !ok {
			return fmt.Errorf("not a FOCUS export: missing column %s", req)
		}
	}
	get := func(rec []string, k string) string {
		if i, ok := col[k]; ok && i < len(rec) {
			return rec[i]
		}
		return ""
	}
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		start, err := time.Parse(time.RFC3339, normTime(get(rec, "ChargePeriodStart")))
		if err != nil {
			continue
		}
		end, err := time.Parse(time.RFC3339, normTime(get(rec, "ChargePeriodEnd")))
		if err != nil {
			end = start.Add(24 * time.Hour)
		}
		cost, err := decimal.NewFromString(strings.TrimSpace(get(rec, c.costColumn)))
		if err != nil {
			continue
		}
		qty, _ := decimal.NewFromString(strings.TrimSpace(get(rec, "ConsumedQuantity")))
		x := row{chargeStart: start.UTC(), chargeEnd: end.UTC(), cost: cost, quantity: qty, currency: get(rec, "BillingCurrency"),
			unit: get(rec, "ConsumedUnit"), resourceID: get(rec, "ResourceId"), resource: get(rec, "ResourceName"), service: get(rec, "ServiceName"),
			category: get(rec, "ServiceCategory"), sku: get(rec, "SkuId"), chargeCategory: get(rec, "ChargeCategory"), region: get(rec, "RegionId")}
		if raw := get(rec, "Tags"); raw != "" {
			var tags map[string]any
			if json.Unmarshal([]byte(raw), &tags) == nil {
				x.tags = map[string]string{}
				for k, v := range tags {
					x.tags[k] = fmt.Sprint(v)
				}
			}
		}
		if !fn(x) {
			return nil
		}
	}
}

// normTime accepte les formats de date des exports (ISO 8601 avec ou sans « T »/fuseau).
func normTime(s string) string {
	s = strings.TrimSpace(s)
	if len(s) == 10 {
		return s + "T00:00:00Z"
	}
	s = strings.Replace(s, " ", "T", 1)
	if !strings.HasSuffix(s, "Z") && !strings.Contains(s[10:], "+") && strings.Count(s[10:], "-") == 0 {
		s += "Z"
	}
	return s
}

// resourceType déduit un type normalisé de la catégorie et du service FOCUS.
func resourceType(category, service string) string {
	switch category {
	case "Compute":
		return model.TypeInstance
	case "Storage":
		svc := strings.ToLower(service)
		for _, obj := range []string{"s3", "blob", "cloud storage", "object"} {
			if strings.Contains(svc, obj) {
				return model.TypeBucket
			}
		}
		return model.TypeVolume
	case "Databases":
		return model.TypeDatabase
	case "Networking":
		return model.TypeLoadBalancer
	}
	return model.TypeService
}

// SyncInventory déduit l'inventaire des ressources facturées sur les 30 derniers jours.
func (c *Conn) SyncInventory(ctx context.Context, _ time.Time) (<-chan connector.Resource, error) {
	since := c.now().AddDate(0, 0, -30)
	return connector.Stream(ctx, 256, func(ctx context.Context, emit func(connector.Resource) bool) error {
		seen := map[string]connector.Resource{}
		first := map[string]time.Time{}
		err := c.scan(ctx, func(x row) bool {
			if x.resourceID == "" || x.chargeStart.Before(since) {
				return true
			}
			r, ok := seen[x.resourceID]
			if !ok {
				name := x.resource
				if name == "" {
					name = x.resourceID[strings.LastIndexAny(x.resourceID, "/:")+1:]
				}
				r = connector.Resource{Type: resourceType(x.category, x.service), ExternalID: x.resourceID, Name: name, Region: x.region,
					Attributes: map[string]any{"service": x.service, "service_category": x.category, "sku": x.sku, "billed_by": c.provider},
					Labels:     map[string]string{}}
			}
			for k, v := range x.tags {
				r.Labels[k] = v
			}
			seen[x.resourceID] = r
			if t, ok := first[x.resourceID]; !ok || x.chargeStart.Before(t) {
				first[x.resourceID] = x.chargeStart
			}
			return true
		})
		if err != nil {
			return err
		}
		ids := make([]string, 0, len(seen))
		for id := range seen {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			r := seen[id]
			t := first[id]
			r.CreatedAt = &t
			if !emit(r) {
				return nil
			}
		}
		return nil
	}), nil
}

// SyncBilling émet les lignes de la période, par jour de début de facturation.
func (c *Conn) SyncBilling(ctx context.Context, period connector.Period) (<-chan connector.CostLine, error) {
	return connector.Stream(ctx, 1024, func(ctx context.Context, emit func(connector.CostLine) bool) error {
		return c.scan(ctx, func(x row) bool {
			day := time.Date(x.chargeStart.Year(), x.chargeStart.Month(), x.chargeStart.Day(), 0, 0, 0, 0, time.UTC)
			if day.Before(period.From) || !day.Before(period.To) || x.cost.IsZero() {
				return true
			}
			l := connector.CostLine{Day: day, Service: x.service, SKU: x.sku, CostType: costType(x.category, x.chargeCategory), Quantity: x.quantity,
				Unit: model.UnitUnit, Amount: x.cost, Currency: strings.ToUpper(x.currency), InvoiceID: c.provider + ":" + day.Format("2006-01")}
			if x.resourceID != "" {
				l.ResourceType, l.ResourceExternalID = resourceType(x.category, x.service), x.resourceID
			}
			return emit(l)
		})
	}), nil
}

func costType(category, charge string) string {
	switch charge {
	case "Credit":
		return model.CostCredit
	case "Tax":
		return model.CostTax
	case "Purchase":
		return model.CostCommitment
	}
	switch category {
	case "Compute", "Databases", "AI and Machine Learning":
		return model.CostCompute
	case "Storage":
		return model.CostStorage
	case "Networking":
		return model.CostNetwork
	}
	return model.CostOther
}
