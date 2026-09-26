// Package scaleway est le connecteur Scaleway (M-01, R2) : inventaire des
// zones configurées (instances, volumes, instantanés, IP flexibles,
// répartiteurs, clusters Kapsule) et consommation facturée (Billing API).
// Accès en lecture seule : clé d'API d'une application IAM disposant des
// ensembles de permissions « …ReadOnly » et « BillingReadOnly ».
package scaleway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/connectors/internal/rest"
	"github.com/kairn-io/kairn/pkg/connector"
	"github.com/kairn-io/kairn/pkg/model"
)

// Type est l'identifiant du connecteur.
const Type = "scaleway"

var permissions = []connector.Permission{
	{Scope: "InstancesReadOnly, BlockStorageReadOnly", Description: "Instances, volumes, instantanés et IP flexibles."},
	{Scope: "LoadBalancersReadOnly, KubernetesReadOnly", Description: "Répartiteurs de charge et clusters Kapsule.", Optional: true},
	{Scope: "BillingReadOnly", Description: "Consommation facturée (rapprochement estimé / facturé).", Optional: true},
}

func init() {
	connector.Register(connector.TypeInfo{
		Type: Type, DisplayName: "Scaleway", Category: connector.CategoryCloud, Provider: "scaleway",
		Resources:       []string{model.TypeInstance, model.TypeVolume, model.TypeSnapshot, model.TypeIP, model.TypeLoadBalancer, model.TypeK8sCluster},
		DefaultInterval: time.Hour, Billing: true, DocsURL: "/docs/connectors/scaleway", Permissions: permissions,
		Fields: []connector.Field{
			{Name: "secret_key", Label: "Clé secrète d'API", Secret: true, Required: true},
			{Name: "organization_id", Label: "ID d'organisation", Required: true},
			{Name: "zones", Label: "Zones", Default: "fr-par-1,fr-par-2,nl-ams-1,pl-waw-1"},
		},
	}, New)
}

// Conn est une instance du connecteur.
type Conn struct {
	base  string
	org   string
	zones []string
	cl    *rest.Client
	now   func() time.Time
}

// New construit le connecteur.
func New(cfg connector.Config) (connector.Connector, error) {
	if err := cfg.Require("secret_key", "organization_id"); err != nil {
		return nil, err
	}
	hc, err := rest.NewHTTP(rest.Options{})
	if err != nil {
		return nil, err
	}
	key := cfg.Secret("secret_key")
	c := &Conn{base: "https://api.scaleway.com", org: cfg.Setting("organization_id", ""), now: func() time.Time { return time.Now().UTC() },
		cl: &rest.Client{HTTP: hc, Auth: func(r *http.Request) { r.Header.Set("X-Auth-Token", key) }}}
	for _, z := range strings.Split(cfg.Setting("zones", "fr-par-1,fr-par-2,nl-ams-1,pl-waw-1"), ",") {
		if z = strings.TrimSpace(z); z != "" {
			c.zones = append(c.zones, z)
		}
	}
	return c, nil
}

func (c *Conn) Type() string                                { return Type }
func (c *Conn) RequiredPermissions() []connector.Permission { return permissions }

func (c *Conn) SyncMetrics(context.Context, connector.TimeWindow) (<-chan connector.MetricPoint, error) {
	return nil, connector.ErrNotSupported
}

func (c *Conn) Validate(ctx context.Context, _ connector.Config) error {
	var out json.RawMessage
	return c.cl.Get(ctx, c.url("instance/v1/zones/"+c.zones[0]+"/servers", url.Values{"per_page": {"1"}}), &out)
}

func (c *Conn) Health(ctx context.Context) connector.HealthStatus {
	h := connector.HealthStatus{Status: connector.HealthOK, CheckedAt: c.now()}
	if err := c.Validate(ctx, connector.Config{}); err != nil {
		h.Status, h.Message = connector.HealthDown, err.Error()
	}
	return h
}

func (c *Conn) url(path string, q url.Values) string { return rest.Join(c.base, path, q) }

// pages parcourt une liste paginée jusqu'à total_count. La taille de page est
// passée sous ses deux noms (per_page pour Instance, page_size pour les API
// plus récentes : Block, Kubernetes, Load Balancer).
func (c *Conn) pages(ctx context.Context, path, key string, fn func(json.RawMessage) bool) error {
	for page := 1; ; page++ {
		var raw map[string]json.RawMessage
		q := url.Values{"page": {strconv.Itoa(page)}, "per_page": {"100"}, "page_size": {"100"}}
		if err := c.cl.Get(ctx, c.url(path, q), &raw); err != nil {
			return err
		}
		var items []json.RawMessage
		if err := json.Unmarshal(raw[key], &items); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		for _, it := range items {
			if !fn(it) {
				return nil
			}
		}
		var total int
		_ = json.Unmarshal(raw["total_count"], &total)
		if len(items) < 100 || page*100 >= total {
			return nil
		}
	}
}

func tsp(s string) *time.Time {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		t = t.UTC()
		return &t
	}
	return nil
}

func tagsToLabels(tags []string) map[string]string {
	out := map[string]string{}
	for _, t := range tags {
		if k, v, ok := strings.Cut(t, "="); ok {
			out[k] = v
		} else if k, v, ok := strings.Cut(t, ":"); ok {
			out[k] = v
		} else if t != "" {
			out[t] = "true"
		}
	}
	return out
}

func regionOf(zone string) string {
	if i := strings.LastIndex(zone, "-"); i > 0 {
		return zone[:i]
	}
	return zone
}

// SyncInventory parcourt chaque zone configurée.
func (c *Conn) SyncInventory(ctx context.Context, _ time.Time) (<-chan connector.Resource, error) {
	return connector.Stream(ctx, 256, func(ctx context.Context, emit func(connector.Resource) bool) error {
		regions := map[string]bool{}
		for _, zone := range c.zones {
			if err := c.zone(ctx, zone, emit); err != nil {
				return fmt.Errorf("zone %s: %w", zone, err)
			}
			regions[regionOf(zone)] = true
		}
		names := make([]string, 0, len(regions))
		for r := range regions {
			names = append(names, r)
		}
		sort.Strings(names)
		for _, region := range names {
			err := c.pages(ctx, "k8s/v1/regions/"+region+"/clusters", "clusters", func(raw json.RawMessage) bool {
				var k struct {
					ID, Name, Version, Type, Status string
					CreatedAt                       string `json:"created_at"`
					Tags                            []string
				}
				if json.Unmarshal(raw, &k) != nil {
					return true
				}
				tier := controlPlaneTier(k.Type)
				return emit(connector.Resource{Type: model.TypeK8sCluster, ExternalID: k.ID, Name: k.Name, Region: region,
					Attributes: map[string]any{"version": k.Version, "status": k.Status, "control_plane_tier": tier, "k8s.cluster": k.Name},
					Labels:     tagsToLabels(k.Tags), CreatedAt: tsp(k.CreatedAt)})
			})
			if err != nil && !isOptional(err) {
				return fmt.Errorf("kapsule %s: %w", region, err)
			}
		}
		return nil
	}), nil
}

// controlPlaneTier traduit le type de cluster Kapsule/Kosmos en offre de plan
// de contrôle, alignée sur les SKU de la grille publique (k8s.control_plane.<offre>) :
// kapsule → free (mutualisé), kapsule-dedicated-8 → dedicated-8,
// multicloud → multicloud, multicloud-dedicated-4 → multicloud-dedicated-4.
func controlPlaneTier(typ string) string {
	t := strings.ToLower(strings.TrimSpace(typ))
	switch {
	case t == "" || t == "kapsule":
		return "free"
	case strings.HasPrefix(t, "kapsule-"):
		return strings.TrimPrefix(t, "kapsule-")
	}
	return t
}

func isOptional(err error) bool {
	return rest.IsNotFound(err) || errors.Is(err, connector.ErrPermission)
}

func (c *Conn) zone(ctx context.Context, zone string, emit func(connector.Resource) bool) error {
	alive := map[string]bool{}
	err := c.pages(ctx, "instance/v1/zones/"+zone+"/servers", "servers", func(raw json.RawMessage) bool {
		var s struct {
			ID, Name       string
			CommercialType string `json:"commercial_type"`
			State          string
			CreationDate   string `json:"creation_date"`
			Tags           []string
			Project        string
		}
		if json.Unmarshal(raw, &s) != nil {
			return true
		}
		alive[s.ID] = true
		billing := "running"
		switch s.State {
		case "stopped":
			billing = "stopped_unbilled" // arrêt complet : l'instance n'est plus facturée (les volumes le restent)
		case "stopped in place":
			billing = "stopped_billed"
		}
		return emit(connector.Resource{Type: model.TypeInstance, ExternalID: s.ID, Name: s.Name, Region: zone,
			Attributes: map[string]any{"flavor": s.CommercialType, "status": s.State, "billing_state": billing, "project_id": s.Project},
			Labels:     tagsToLabels(s.Tags), CreatedAt: tsp(s.CreationDate)})
	})
	if err != nil {
		return fmt.Errorf("servers: %w", err)
	}
	// Volumes Block Storage (SBS) : l'API Block donne l'offre (sbs_5k, sbs_15k),
	// que l'API Instance résume en « sbs_volume ». Facultatif (BlockStorageReadOnly).
	sbs := map[string]bool{}
	err = c.pages(ctx, "block/v1alpha1/zones/"+zone+"/volumes", "volumes", func(raw json.RawMessage) bool {
		var v struct {
			ID, Name, Type, Status string
			Size                   json.Number
			CreatedAt              string `json:"created_at"`
			Tags                   []string
			Specs                  *struct {
				PerfIOPS int `json:"perf_iops"`
			}
			References []struct {
				ProductResourceType string `json:"product_resource_type"`
				ProductResourceID   string `json:"product_resource_id"`
			}
		}
		if json.Unmarshal(raw, &v) != nil || v.ID == "" {
			return true
		}
		sbs[v.ID] = true
		typ := v.Type
		if !strings.HasPrefix(typ, "sbs_") && v.Specs != nil && v.Specs.PerfIOPS > 0 {
			typ = fmt.Sprintf("sbs_%dk", v.Specs.PerfIOPS/1000)
		}
		size, _ := v.Size.Float64()
		attrs := map[string]any{"size_gb": size / 1e9, "volume_type": typ, "status": v.Status}
		var parents []connector.Edge
		for _, ref := range v.References {
			if ref.ProductResourceType == "instance_server" && alive[ref.ProductResourceID] {
				attrs["attached_to"] = ref.ProductResourceID
				parents = append(parents, connector.Edge{Relation: model.RelAttachedTo, ParentType: model.TypeInstance, ParentExternalID: ref.ProductResourceID})
				break
			}
		}
		return emit(connector.Resource{Type: model.TypeVolume, ExternalID: v.ID, Name: v.Name, Region: zone, Attributes: attrs,
			Labels: tagsToLabels(v.Tags), Parents: parents, CreatedAt: tsp(v.CreatedAt)})
	})
	if err != nil && !isOptional(err) {
		return fmt.Errorf("block volumes: %w", err)
	}
	err = c.pages(ctx, "instance/v1/zones/"+zone+"/volumes", "volumes", func(raw json.RawMessage) bool {
		var v struct {
			ID, Name     string
			Size         json.Number
			VolumeType   string `json:"volume_type"`
			State        string
			CreationDate string `json:"creation_date"`
			Server       *struct{ ID string }
			Tags         []string
		}
		if json.Unmarshal(raw, &v) != nil || sbs[v.ID] {
			return true // volume SBS déjà inventorié via l'API Block
		}
		size, _ := v.Size.Float64()
		attrs := map[string]any{"size_gb": size / 1e9, "volume_type": v.VolumeType, "status": v.State}
		var parents []connector.Edge
		if v.Server != nil && alive[v.Server.ID] {
			attrs["attached_to"] = v.Server.ID
			parents = append(parents, connector.Edge{Relation: model.RelAttachedTo, ParentType: model.TypeInstance, ParentExternalID: v.Server.ID})
		}
		return emit(connector.Resource{Type: model.TypeVolume, ExternalID: v.ID, Name: v.Name, Region: zone, Attributes: attrs,
			Labels: tagsToLabels(v.Tags), Parents: parents, CreatedAt: tsp(v.CreationDate)})
	})
	if err != nil {
		return fmt.Errorf("volumes: %w", err)
	}
	err = c.pages(ctx, "instance/v1/zones/"+zone+"/snapshots", "snapshots", func(raw json.RawMessage) bool {
		var s struct {
			ID, Name     string
			Size         json.Number
			CreationDate string `json:"creation_date"`
		}
		if json.Unmarshal(raw, &s) != nil {
			return true
		}
		size, _ := s.Size.Float64()
		created := tsp(s.CreationDate)
		attrs := map[string]any{"size_gb": size / 1e9}
		if created != nil {
			attrs["created_at"] = created.Format(time.RFC3339)
		}
		return emit(connector.Resource{Type: model.TypeSnapshot, ExternalID: s.ID, Name: s.Name, Region: zone, Attributes: attrs,
			Labels: map[string]string{}, CreatedAt: created})
	})
	if err != nil {
		return fmt.Errorf("snapshots: %w", err)
	}
	err = c.pages(ctx, "instance/v1/zones/"+zone+"/ips", "ips", func(raw json.RawMessage) bool {
		var ip struct {
			ID, Address string
			Server      *struct{ ID string }
			Tags        []string
		}
		if json.Unmarshal(raw, &ip) != nil {
			return true
		}
		attrs := map[string]any{"ip_kind": "floating", "address": ip.Address}
		var parents []connector.Edge
		if ip.Server != nil && alive[ip.Server.ID] {
			attrs["attached_to"] = ip.Server.ID
			parents = append(parents, connector.Edge{Relation: model.RelAttachedTo, ParentType: model.TypeInstance, ParentExternalID: ip.Server.ID})
		}
		return emit(connector.Resource{Type: model.TypeIP, ExternalID: ip.ID, Name: ip.Address, Region: zone, Attributes: attrs,
			Labels: tagsToLabels(ip.Tags), Parents: parents})
	})
	if err != nil {
		return fmt.Errorf("ips: %w", err)
	}
	err = c.pages(ctx, "lb/v1/zones/"+zone+"/lbs", "lbs", func(raw json.RawMessage) bool {
		var lb struct {
			ID, Name, Type, Status string
			CreatedAt              string `json:"created_at"`
			Tags                   []string
		}
		if json.Unmarshal(raw, &lb) != nil {
			return true
		}
		return emit(connector.Resource{Type: model.TypeLoadBalancer, ExternalID: lb.ID, Name: lb.Name, Region: zone,
			Attributes: map[string]any{"flavor": lb.Type, "status": lb.Status}, Labels: tagsToLabels(lb.Tags), CreatedAt: tsp(lb.CreatedAt)})
	})
	if err != nil && !isOptional(err) {
		return fmt.Errorf("load balancers: %w", err)
	}
	return nil
}

// ------------------------------------------------------------------ facturation

// money convertit un montant Scaleway {units, nanos} en décimal exact.
type money struct {
	CurrencyCode string `json:"currency_code"`
	Units        int64  `json:"units"`
	Nanos        int64  `json:"nanos"`
}

func (m money) decimal() decimal.Decimal {
	return decimal.NewFromInt(m.Units).Add(decimal.New(m.Nanos, -9))
}

// SyncBilling lit la consommation mensuelle (Billing API v2) et la répartit par jour, au centime près.
func (c *Conn) SyncBilling(ctx context.Context, period connector.Period) (<-chan connector.CostLine, error) {
	return connector.Stream(ctx, 256, func(ctx context.Context, emit func(connector.CostLine) bool) error {
		now := c.now()
		for m := time.Date(period.From.Year(), period.From.Month(), 1, 0, 0, 0, 0, time.UTC); m.Before(period.To); m = m.AddDate(0, 1, 0) {
			end := m.AddDate(0, 1, 0)
			if now.Before(end) {
				end = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
			}
			if !end.After(m) {
				continue
			}
			q := url.Values{"organization_id": {c.org}, "billing_period": {m.Format("2006-01")}}
			var out struct {
				Consumptions []struct {
					Value          money  `json:"value"`
					ProductName    string `json:"product_name"`
					ResourceName   string `json:"resource_name"`
					SKU            string `json:"sku"`
					CategoryName   string `json:"category_name"`
					Unit           string `json:"unit"`
					BilledQuantity string `json:"billed_quantity"`
				} `json:"consumptions"`
			}
			if err := c.cl.Get(ctx, c.url("billing/v2beta1/consumptions", q), &out); err != nil {
				return fmt.Errorf("billing %s: %w", m.Format("2006-01"), err)
			}
			for _, cons := range out.Consumptions {
				total := cons.Value.decimal()
				if total.IsZero() {
					continue
				}
				qty, _ := decimal.NewFromString(cons.BilledQuantity)
				days := daysBetween(m, end)
				share := total.Div(decimal.NewFromInt(int64(len(days)))).Truncate(2)
				rest := total.Sub(share.Mul(decimal.NewFromInt(int64(len(days)))))
				qshare := qty.Div(decimal.NewFromInt(int64(len(days)))).Truncate(6)
				for i, d := range days {
					amount := share
					if i == len(days)-1 {
						amount = amount.Add(rest)
					}
					if d.Before(period.From) || !d.Before(period.To) {
						continue
					}
					if !emit(connector.CostLine{Day: d, Service: cons.ProductName, SKU: cons.SKU, CostType: categoryCost(cons.CategoryName), Quantity: qshare,
						Unit: model.UnitUnit, Amount: amount, Currency: strings.ToUpper(cons.Value.CurrencyCode), InvoiceID: "scaleway:" + m.Format("2006-01")}) {
						return nil
					}
				}
			}
		}
		return nil
	}), nil
}

func daysBetween(from, to time.Time) []time.Time {
	var out []time.Time
	for d := from; d.Before(to); d = d.AddDate(0, 0, 1) {
		out = append(out, d)
	}
	return out
}

func categoryCost(cat string) string {
	switch strings.ToLower(cat) {
	case "compute", "containers", "serverless":
		return model.CostCompute
	case "storage":
		return model.CostStorage
	case "network":
		return model.CostNetwork
	}
	return model.CostOther
}
