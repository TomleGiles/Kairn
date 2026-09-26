// Package ovh est le connecteur OVHcloud Public Cloud (M-01, R2) :
// inventaire des projets (instances, volumes, instantanés, IP flottantes,
// répartiteurs, stockage objet, Kubernetes managé) et consommation facturée
// (API /cloud/project/{id}/usage), rapprochée des coûts estimés.
//
// Accès en lecture seule : clé d'application et consumer key limitée aux
// méthodes GET sur /cloud/project/* (docs/connectors/ovh.md).
package ovh

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/connectors/internal/rest"
	"github.com/kairn-io/kairn/pkg/connector"
	"github.com/kairn-io/kairn/pkg/model"
)

// Type est l'identifiant du connecteur.
const Type = "ovh"

var permissions = []connector.Permission{
	{Scope: "GET /cloud/project/*", Description: "Inventaire des projets Public Cloud et consommation (usage/current, usage/history)."},
	{Scope: "GET /auth/time", Description: "Horloge de l'API pour la signature des requêtes."},
}

func init() {
	connector.Register(connector.TypeInfo{
		Type: Type, DisplayName: "OVHcloud Public Cloud", Category: connector.CategoryCloud, Provider: "ovh",
		Resources: []string{model.TypeProject, model.TypeInstance, model.TypeVolume, model.TypeSnapshot, model.TypeIP, model.TypeLoadBalancer,
			model.TypeBucket, model.TypeK8sCluster},
		DefaultInterval: time.Hour, Billing: true, DocsURL: "/docs/connectors/ovh", Permissions: permissions,
		Fields: []connector.Field{
			{Name: "endpoint", Label: "Point d'accès", Default: "ovh-eu", Help: "ovh-eu, ovh-ca ou ovh-us"},
			{Name: "application_key", Label: "Application key", Required: true},
			{Name: "application_secret", Label: "Application secret", Secret: true, Required: true},
			{Name: "consumer_key", Label: "Consumer key (GET /cloud/project/*)", Secret: true, Required: true},
			{Name: "project_ids", Label: "Projets", Help: "Identifiants séparés par des virgules ; vide = tous les projets accessibles"},
		},
	}, New)
}

// Conn est une instance du connecteur.
type Conn struct {
	c        *client
	projects []string
	now      func() time.Time
}

// New construit le connecteur.
func New(cfg connector.Config) (connector.Connector, error) {
	if err := cfg.Require("application_key", "application_secret", "consumer_key"); err != nil {
		return nil, err
	}
	base, ok := endpoints[cfg.Setting("endpoint", "ovh-eu")]
	if !ok {
		base = strings.TrimRight(cfg.Setting("endpoint", ""), "/")
		if !strings.HasPrefix(base, "https://") {
			return nil, fmt.Errorf("unknown endpoint %q", cfg.Setting("endpoint", ""))
		}
	}
	hc, err := rest.NewHTTP(rest.Options{})
	if err != nil {
		return nil, err
	}
	now := func() time.Time { return time.Now().UTC() }
	conn := &Conn{now: now, c: &client{base: base, appKey: cfg.Setting("application_key", ""), appSecret: cfg.Secret("application_secret"),
		consumerKey: cfg.Secret("consumer_key"), http: hc, now: now}}
	for _, p := range strings.Split(cfg.Setting("project_ids", ""), ",") {
		if p = strings.TrimSpace(p); p != "" {
			conn.projects = append(conn.projects, p)
		}
	}
	return conn, nil
}

func (c *Conn) Type() string                                { return Type }
func (c *Conn) RequiredPermissions() []connector.Permission { return permissions }

func (c *Conn) projectIDs(ctx context.Context) ([]string, error) {
	if len(c.projects) > 0 {
		return c.projects, nil
	}
	var ids []string
	if err := c.c.get(ctx, "/cloud/project", &ids); err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	sort.Strings(ids)
	return ids, nil
}

func (c *Conn) Validate(ctx context.Context, _ connector.Config) error {
	ids, err := c.projectIDs(ctx)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return fmt.Errorf("no Public Cloud project is accessible with this consumer key")
	}
	var usage json.RawMessage
	return c.c.get(ctx, "/cloud/project/"+url.PathEscape(ids[0])+"/usage/current", &usage)
}

func (c *Conn) Health(ctx context.Context) connector.HealthStatus {
	h := connector.HealthStatus{Status: connector.HealthOK, CheckedAt: c.now()}
	if _, err := c.projectIDs(ctx); err != nil {
		h.Status, h.Message = connector.HealthDown, err.Error()
	}
	return h
}

// SyncMetrics : l'API OVHcloud n'expose pas d'utilisation ; utiliser Prometheus (node_exporter).
func (c *Conn) SyncMetrics(context.Context, connector.TimeWindow) (<-chan connector.MetricPoint, error) {
	return nil, connector.ErrNotSupported
}

// ------------------------------------------------------------------ inventaire

func p(project string, parts ...string) string {
	s := "/cloud/project/" + url.PathEscape(project)
	for _, x := range parts {
		s += "/" + url.PathEscape(x)
	}
	return s
}

func ts(s string) *time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05Z0700"} {
		if t, err := time.Parse(layout, s); err == nil {
			t = t.UTC()
			return &t
		}
	}
	return nil
}

func (c *Conn) SyncInventory(ctx context.Context, _ time.Time) (<-chan connector.Resource, error) {
	projects, err := c.projectIDs(ctx)
	if err != nil {
		return nil, err
	}
	return connector.Stream(ctx, 256, func(ctx context.Context, emit func(connector.Resource) bool) error {
		for _, proj := range projects {
			if err := c.project(ctx, proj, emit); err != nil {
				return fmt.Errorf("project %s: %w", proj, err)
			}
		}
		return nil
	}), nil
}

func (c *Conn) project(ctx context.Context, proj string, emit func(connector.Resource) bool) error {
	var info struct {
		Description  string `json:"description"`
		Status       string `json:"status"`
		CreationDate string `json:"creationDate"`
	}
	if err := c.c.get(ctx, p(proj), &info); err != nil {
		return err
	}
	name := info.Description
	if name == "" {
		name = proj
	}
	if !emit(connector.Resource{Type: model.TypeProject, ExternalID: proj, Name: name, Attributes: map[string]any{"project_id": proj, "status": info.Status},
		Labels: map[string]string{}, CreatedAt: ts(info.CreationDate)}) {
		return nil
	}
	inProject := connector.Edge{Relation: model.RelContains, ParentType: model.TypeProject, ParentExternalID: proj}

	var flavors []struct {
		ID    string      `json:"id"`
		Name  string      `json:"name"`
		VCPUs json.Number `json:"vcpus"`
		RAM   json.Number `json:"ram"` // Go
		Disk  json.Number `json:"disk"`
	}
	if err := c.c.get(ctx, p(proj, "flavor"), &flavors); err != nil {
		return fmt.Errorf("flavors: %w", err)
	}
	type flavor struct {
		name       string
		vcpus, ram float64
	}
	fl := map[string]flavor{}
	for _, f := range flavors {
		v, _ := f.VCPUs.Float64()
		r, _ := f.RAM.Float64()
		fl[f.ID] = flavor{f.Name, v, r}
	}
	var instances []struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		FlavorID       string `json:"flavorId"`
		Region         string `json:"region"`
		Status         string `json:"status"`
		Created        string `json:"created"`
		MonthlyBilling *struct {
			Status string `json:"status"`
		} `json:"monthlyBilling"`
	}
	if err := c.c.get(ctx, p(proj, "instance"), &instances); err != nil {
		return fmt.Errorf("instances: %w", err)
	}
	alive := map[string]bool{}
	for _, in := range instances {
		alive[in.ID] = true
		f := fl[in.FlavorID]
		billing := "running"
		switch strings.ToUpper(in.Status) {
		case "SHUTOFF", "STOPPED", "PAUSED", "SUSPENDED":
			billing = "stopped_billed" // OVHcloud facture les instances arrêtées
		case "SHELVED", "SHELVED_OFFLOADED":
			billing = "stopped_unbilled"
		}
		attrs := map[string]any{"flavor": f.name, "vcpus": f.vcpus, "ram_gb": f.ram, "status": strings.ToUpper(in.Status), "billing_state": billing,
			"project_id": proj, "billing_mode": "hourly"}
		if in.MonthlyBilling != nil && in.MonthlyBilling.Status == "ok" {
			attrs["billing_mode"] = "monthly"
		}
		if !emit(connector.Resource{Type: model.TypeInstance, ExternalID: in.ID, Name: in.Name, Region: in.Region, Attributes: attrs,
			Labels: map[string]string{}, Parents: []connector.Edge{inProject}, CreatedAt: ts(in.Created)}) {
			return nil
		}
	}
	var volumes []struct {
		ID           string      `json:"id"`
		Name         string      `json:"name"`
		Size         json.Number `json:"size"`
		Type         string      `json:"type"`
		Region       string      `json:"region"`
		Status       string      `json:"status"`
		AttachedTo   []string    `json:"attachedTo"`
		CreationDate string      `json:"creationDate"`
	}
	if err := c.c.get(ctx, p(proj, "volume"), &volumes); err != nil {
		return fmt.Errorf("volumes: %w", err)
	}
	for _, v := range volumes {
		size, _ := v.Size.Float64()
		attrs := map[string]any{"size_gb": size, "volume_type": v.Type, "status": v.Status, "project_id": proj}
		parents := []connector.Edge{inProject}
		for _, a := range v.AttachedTo {
			if alive[a] {
				attrs["attached_to"] = a
				parents = append(parents, connector.Edge{Relation: model.RelAttachedTo, ParentType: model.TypeInstance, ParentExternalID: a})
				break
			}
		}
		if !emit(connector.Resource{Type: model.TypeVolume, ExternalID: v.ID, Name: v.Name, Region: v.Region, Attributes: attrs,
			Labels: map[string]string{}, Parents: parents, CreatedAt: ts(v.CreationDate)}) {
			return nil
		}
	}
	var snaps []struct {
		ID           string      `json:"id"`
		Name         string      `json:"name"`
		Size         json.Number `json:"size"`
		VolumeID     string      `json:"volumeId"`
		Region       string      `json:"region"`
		CreationDate string      `json:"creationDate"`
	}
	if err := c.c.get(ctx, p(proj, "volume", "snapshot"), &snaps); err != nil {
		return fmt.Errorf("snapshots: %w", err)
	}
	for _, s := range snaps {
		size, _ := s.Size.Float64()
		created := ts(s.CreationDate)
		attrs := map[string]any{"size_gb": size, "volume_id": s.VolumeID, "project_id": proj}
		if created != nil {
			attrs["created_at"] = created.Format(time.RFC3339)
		}
		if !emit(connector.Resource{Type: model.TypeSnapshot, ExternalID: s.ID, Name: s.Name, Region: s.Region, Attributes: attrs,
			Labels: map[string]string{}, Parents: []connector.Edge{inProject}, CreatedAt: created}) {
			return nil
		}
	}
	var regions []string
	if err := c.c.get(ctx, p(proj, "region"), &regions); err != nil {
		return fmt.Errorf("regions: %w", err)
	}
	for _, region := range regions {
		if err := c.regional(ctx, proj, region, inProject, alive, emit); err != nil {
			return fmt.Errorf("region %s: %w", region, err)
		}
	}
	var kubes []string
	if err := c.c.get(ctx, p(proj, "kube"), &kubes); err != nil {
		return fmt.Errorf("kube: %w", err)
	}
	for _, id := range kubes {
		var k struct {
			ID, Name, Region, Version, Status, Plan string
			CreatedAt                               string `json:"createdAt"`
		}
		if err := c.c.get(ctx, p(proj, "kube", id), &k); err != nil {
			return fmt.Errorf("kube %s: %w", id, err)
		}
		tier := strings.ToLower(k.Plan)
		if tier == "" {
			tier = "free"
		}
		if !emit(connector.Resource{Type: model.TypeK8sCluster, ExternalID: k.ID, Name: k.Name, Region: k.Region,
			Attributes: map[string]any{"version": k.Version, "status": k.Status, "control_plane_tier": tier, "project_id": proj, "k8s.cluster": k.Name},
			Labels:     map[string]string{}, Parents: []connector.Edge{inProject}, CreatedAt: ts(k.CreatedAt)}) {
			return nil
		}
	}
	return nil
}

// regional émet les ressources exposées par région : IP flottantes, répartiteurs, stockage objet S3.
func (c *Conn) regional(ctx context.Context, proj, region string, inProject connector.Edge, alive map[string]bool, emit func(connector.Resource) bool) error {
	var fips []struct {
		ID               string `json:"id"`
		IP               string `json:"ip"`
		Status           string `json:"status"`
		AssociatedEntity *struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"associatedEntity"`
	}
	if err := c.c.get(ctx, p(proj, "region", region, "floatingip"), &fips); err != nil && !rest.IsNotFound(err) {
		return fmt.Errorf("floating ips: %w", err)
	}
	for _, f := range fips {
		attrs := map[string]any{"ip_kind": "floating", "address": f.IP, "status": f.Status, "project_id": proj}
		parents := []connector.Edge{inProject}
		if f.AssociatedEntity != nil && alive[f.AssociatedEntity.ID] {
			attrs["attached_to"] = f.AssociatedEntity.ID
			parents = append(parents, connector.Edge{Relation: model.RelAttachedTo, ParentType: model.TypeInstance, ParentExternalID: f.AssociatedEntity.ID})
		}
		if !emit(connector.Resource{Type: model.TypeIP, ExternalID: f.ID, Name: f.IP, Region: region, Attributes: attrs, Labels: map[string]string{}, Parents: parents}) {
			return nil
		}
	}
	lbFlavors := map[string]string{}
	var lbfl []struct{ ID, Name string }
	if err := c.c.get(ctx, p(proj, "region", region, "loadbalancing", "flavor"), &lbfl); err == nil {
		for _, f := range lbfl {
			lbFlavors[f.ID] = f.Name
		}
	}
	var lbs []struct {
		ID                 string `json:"id"`
		Name               string `json:"name"`
		FlavorID           string `json:"flavorId"`
		OperatingStatus    string `json:"operatingStatus"`
		ProvisioningStatus string `json:"provisioningStatus"`
		VIPAddress         string `json:"vipAddress"`
		CreatedAt          string `json:"createdAt"`
	}
	if err := c.c.get(ctx, p(proj, "region", region, "loadbalancing", "loadbalancer"), &lbs); err != nil && !rest.IsNotFound(err) {
		return fmt.Errorf("load balancers: %w", err)
	}
	for _, lb := range lbs {
		if !emit(connector.Resource{Type: model.TypeLoadBalancer, ExternalID: lb.ID, Name: lb.Name, Region: region,
			Attributes: map[string]any{"flavor": lbFlavors[lb.FlavorID], "status": lb.ProvisioningStatus, "operating_status": lb.OperatingStatus,
				"vip_address": lb.VIPAddress, "project_id": proj},
			Labels: map[string]string{}, Parents: []connector.Edge{inProject}, CreatedAt: ts(lb.CreatedAt)}) {
			return nil
		}
	}
	var buckets []struct {
		Name         string      `json:"name"`
		ObjectsCount json.Number `json:"objectsCount"`
		ObjectsSize  json.Number `json:"objectsSize"`
		CreatedAt    string      `json:"createdAt"`
	}
	if err := c.c.get(ctx, p(proj, "region", region, "storage"), &buckets); err != nil && !rest.IsNotFound(err) {
		return fmt.Errorf("object storage: %w", err)
	}
	for _, b := range buckets {
		size, _ := b.ObjectsSize.Float64()
		objects, _ := b.ObjectsCount.Float64()
		if !emit(connector.Resource{Type: model.TypeBucket, ExternalID: region + "/" + b.Name, Name: b.Name, Region: region,
			Attributes: map[string]any{"storage_class": "standard", "size_gb": size / 1e9, "objects": objects, "project_id": proj},
			Labels:     map[string]string{}, Parents: []connector.Edge{inProject}, CreatedAt: ts(b.CreatedAt)}) {
			return nil
		}
	}
	return nil
}

// ------------------------------------------------------------------ facturation

type qty struct {
	Unit  string      `json:"unit"`
	Value json.Number `json:"value"`
}

type usage struct {
	Period struct {
		From string `json:"from"`
		To   string `json:"to"`
	} `json:"period"`
	LastUpdate  string `json:"lastUpdate"`
	HourlyUsage struct {
		Instance []struct {
			Reference string `json:"reference"`
			Region    string `json:"region"`
			Details   []struct {
				InstanceID string      `json:"instanceId"`
				Quantity   qty         `json:"quantity"`
				TotalPrice json.Number `json:"totalPrice"`
			} `json:"details"`
		} `json:"instance"`
		Volume []struct {
			Type    string `json:"type"`
			Region  string `json:"region"`
			Details []struct {
				VolumeID   string      `json:"volumeId"`
				Quantity   qty         `json:"quantity"`
				TotalPrice json.Number `json:"totalPrice"`
			} `json:"details"`
		} `json:"volume"`
		Snapshot []struct {
			Region     string      `json:"region"`
			TotalPrice json.Number `json:"totalPrice"`
		} `json:"snapshot"`
		Storage []struct {
			Region     string      `json:"region"`
			BucketName string      `json:"bucketName"`
			Type       string      `json:"type"`
			TotalPrice json.Number `json:"totalPrice"`
		} `json:"storage"`
	} `json:"hourlyUsage"`
	MonthlyUsage struct {
		Instance []struct {
			Reference string `json:"reference"`
			Details   []struct {
				InstanceID string      `json:"instanceId"`
				TotalPrice json.Number `json:"totalPrice"`
			} `json:"details"`
		} `json:"instance"`
	} `json:"monthlyUsage"`
	ResourcesUsage []struct {
		Type       string      `json:"type"`
		TotalPrice json.Number `json:"totalPrice"`
	} `json:"resourcesUsage"`
}

// SyncBilling lit la consommation des périodes chevauchant [From, To) et la
// répartit par jour, au centime près (le dernier jour reçoit l'arrondi).
func (c *Conn) SyncBilling(ctx context.Context, period connector.Period) (<-chan connector.CostLine, error) {
	projects, err := c.projectIDs(ctx)
	if err != nil {
		return nil, err
	}
	return connector.Stream(ctx, 512, func(ctx context.Context, emit func(connector.CostLine) bool) error {
		for _, proj := range projects {
			usages, err := c.usages(ctx, proj, period)
			if err != nil {
				return fmt.Errorf("project %s usage: %w", proj, err)
			}
			for id, u := range usages {
				for _, l := range c.lines(proj, id, u, period) {
					if !emit(l) {
						return nil
					}
				}
			}
		}
		return nil
	}), nil
}

// usages renvoie les consommations (historique + mois courant) chevauchant la période.
func (c *Conn) usages(ctx context.Context, proj string, period connector.Period) (map[string]usage, error) {
	out := map[string]usage{}
	var hist []struct {
		ID     string `json:"id"`
		Period struct {
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"period"`
	}
	q := url.Values{"from": {period.From.Format(time.RFC3339)}, "to": {period.To.Format(time.RFC3339)}}
	if err := c.c.get(ctx, p(proj, "usage", "history")+"?"+q.Encode(), &hist); err != nil {
		return nil, err
	}
	for _, h := range hist {
		var u usage
		if err := c.c.get(ctx, p(proj, "usage", "history", h.ID), &u); err != nil {
			return nil, err
		}
		out["hist-"+h.ID] = u
	}
	var cur usage
	if err := c.c.get(ctx, p(proj, "usage", "current"), &cur); err != nil {
		return nil, err
	}
	out["current"] = cur
	return out, nil
}

// spread répartit un montant sur les jours [from, to) ∩ période, au centime près.
func spread(total decimal.Decimal, from, to time.Time, period connector.Period) map[time.Time]decimal.Decimal {
	out := map[time.Time]decimal.Decimal{}
	start := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	var days []time.Time
	for d := start; d.Before(to); d = d.AddDate(0, 0, 1) {
		days = append(days, d)
	}
	if len(days) == 0 || total.IsZero() {
		return out
	}
	share := total.Div(decimal.NewFromInt(int64(len(days)))).Truncate(2)
	rest := total.Sub(share.Mul(decimal.NewFromInt(int64(len(days)))))
	for i, d := range days {
		v := share
		if i == len(days)-1 {
			v = v.Add(rest)
		}
		if !d.Before(period.From) && d.Before(period.To) {
			out[d] = v
		}
	}
	return out
}

func (c *Conn) lines(proj, usageID string, u usage, period connector.Period) []connector.CostLine {
	from, to := ts(u.Period.From), ts(u.Period.To)
	if from == nil || to == nil {
		return nil
	}
	end := *to
	if last := ts(u.LastUpdate); last != nil && last.Before(end) {
		end = *last // mois en cours : consommation connue jusqu'à la dernière mise à jour
	}
	invoice := proj + ":" + usageID
	var out []connector.CostLine
	add := func(rtype, ext, service, sku, costType string, q qty, amount json.Number) {
		total, err := decimal.NewFromString(string(amount))
		if err != nil || total.IsZero() {
			return
		}
		quantity := decimal.Zero
		if q.Value != "" {
			quantity, _ = decimal.NewFromString(string(q.Value))
		}
		days := spread(total, *from, end, period)
		qdays := spread(quantity, *from, end, period)
		keys := make([]time.Time, 0, len(days))
		for d := range days {
			keys = append(keys, d)
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i].Before(keys[j]) })
		for _, d := range keys {
			out = append(out, connector.CostLine{ResourceType: rtype, ResourceExternalID: ext, Day: d, Service: service, SKU: sku, CostType: costType,
				Quantity: qdays[d], Unit: unitOf(q.Unit), Amount: days[d], Currency: "EUR", InvoiceID: invoice})
		}
	}
	for _, in := range u.HourlyUsage.Instance {
		for _, d := range in.Details {
			add(model.TypeInstance, d.InstanceID, "instance", "compute.flavor."+strings.TrimSuffix(in.Reference, ".consumption"), model.CostCompute, d.Quantity, d.TotalPrice)
		}
	}
	for _, in := range u.MonthlyUsage.Instance {
		for _, d := range in.Details {
			add(model.TypeInstance, d.InstanceID, "instance", "compute.flavor."+strings.TrimSuffix(in.Reference, ".monthly"), model.CostCompute, qty{}, d.TotalPrice)
		}
	}
	for _, v := range u.HourlyUsage.Volume {
		for _, d := range v.Details {
			add(model.TypeVolume, d.VolumeID, "volume", "storage.volume."+v.Type, model.CostStorage, d.Quantity, d.TotalPrice)
		}
	}
	for _, s := range u.HourlyUsage.Snapshot {
		add("", "", "snapshot", "storage.snapshot", model.CostStorage, qty{}, s.TotalPrice)
	}
	for _, s := range u.HourlyUsage.Storage {
		ext := ""
		if s.BucketName != "" {
			ext = s.Region + "/" + s.BucketName
		}
		rtype := ""
		if ext != "" {
			rtype = model.TypeBucket
		}
		add(rtype, ext, "object-storage", "storage.object.standard", model.CostStorage, qty{}, s.TotalPrice)
	}
	for _, r := range u.ResourcesUsage {
		add("", "", r.Type, r.Type, costTypeOf(r.Type), qty{}, r.TotalPrice)
	}
	return out
}

func unitOf(u string) string {
	switch strings.ToLower(u) {
	case "hour":
		return model.UnitHour
	case "gib", "gb":
		return model.UnitGBMonth
	}
	return model.UnitUnit
}

func costTypeOf(t string) string {
	switch {
	case strings.Contains(t, "loadbalancer"), strings.Contains(t, "floatingip"), strings.Contains(t, "gateway"), strings.Contains(t, "bandwidth"):
		return model.CostNetwork
	case strings.Contains(t, "storage"), strings.Contains(t, "volume"), strings.Contains(t, "backup"):
		return model.CostStorage
	}
	return model.CostCompute
}
