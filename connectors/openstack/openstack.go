// Package openstack est le connecteur OpenStack (M-01) : Keystone v3,
// Nova, Cinder, Neutron, Octavia, Swift et, en option, Gnocchi pour
// l'utilisation. Strictement en lecture seule ; un rôle « reader » sur le
// projet suffit (docs/connectors/openstack.md).
package openstack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kairn-io/kairn/connectors/internal/rest"
	"github.com/kairn-io/kairn/pkg/connector"
	"github.com/kairn-io/kairn/pkg/model"
)

// Type est l'identifiant du connecteur.
const Type = "openstack"

var permissions = []connector.Permission{
	{Scope: "keystone: role reader sur le projet", Description: "Lecture des serveurs, volumes, IP flottantes et du catalogue (rôle « reader » des politiques par défaut)."},
	{Scope: "octavia: role load-balancer_observer", Description: "Lecture des répartiteurs de charge.", Optional: true},
	{Scope: "swift: lecture du compte", Description: "Liste des conteneurs et volumétrie du stockage objet.", Optional: true},
	{Scope: "gnocchi: lecture des métriques", Description: "Utilisation CPU et mémoire des instances (sinon, utiliser le connecteur Prometheus).", Optional: true},
}

func init() {
	connector.Register(connector.TypeInfo{
		Type: Type, DisplayName: "OpenStack (privé, OVHcloud Public Cloud, Infomaniak…)", Category: connector.CategoryCloud, Provider: "openstack",
		Resources:       []string{model.TypeProject, model.TypeInstance, model.TypeVolume, model.TypeSnapshot, model.TypeIP, model.TypeLoadBalancer, model.TypeBucket},
		DefaultInterval: time.Hour, Metrics: true, DocsURL: "/docs/connectors/openstack",
		Permissions: permissions,
		Fields: []connector.Field{
			{Name: "auth_url", Label: "URL Keystone v3", Required: true, Help: "ex. https://auth.cloud.ovh.net/v3"},
			{Name: "region", Label: "Région", Help: "ex. GRA11 ; vide = première région du catalogue"},
			{Name: "application_credential_id", Label: "ID des identifiants d'application", Help: "Recommandé : identifiants d'application avec le rôle reader"},
			{Name: "application_credential_secret", Label: "Secret des identifiants d'application", Secret: true},
			{Name: "username", Label: "Utilisateur (si pas d'identifiants d'application)"},
			{Name: "password", Label: "Mot de passe", Secret: true},
			{Name: "user_domain_name", Label: "Domaine de l'utilisateur", Default: "Default"},
			{Name: "project_id", Label: "ID du projet"},
			{Name: "project_name", Label: "Nom du projet (si pas d'ID)"},
			{Name: "project_domain_name", Label: "Domaine du projet", Default: "Default"},
			{Name: "interface", Label: "Interface des points d'accès", Default: "public"},
			{Name: "stopped_billing", Label: "Facturation des instances arrêtées", Default: "billed", Help: "billed (OVHcloud, la plupart des clouds) ou unbilled"},
			{Name: "metrics", Label: "Source d'utilisation", Default: "auto", Help: "auto (Gnocchi si présent), gnocchi ou none"},
			{Name: "ca_cert", Label: "Certificat d'autorité (PEM)", Help: "Pour un cloud privé à PKI interne"},
			{Name: "pricing_provider", Label: "Grille tarifaire", Help: "ex. ovh pour appliquer la grille OVHcloud ; vide = openstack"},
		},
	}, New)
}

type settings struct {
	authURL, region, iface                string
	appCredID, appCredSecret              string
	username, password, userDomain        string
	projectID, projectName, projectDomain string
	stoppedBilled                         bool
	metrics                               string
	caCert                                string
	gnocchiGranularity                    int
}

// Conn est une instance du connecteur.
type Conn struct {
	s   *session
	now func() time.Time
}

// New construit le connecteur à partir de sa configuration.
func New(cfg connector.Config) (connector.Connector, error) {
	st := settings{
		authURL: strings.TrimRight(cfg.Setting("auth_url", ""), "/"), region: cfg.Setting("region", ""), iface: cfg.Setting("interface", "public"),
		appCredID: cfg.Setting("application_credential_id", ""), appCredSecret: cfg.Secret("application_credential_secret"),
		username: cfg.Setting("username", ""), password: cfg.Secret("password"), userDomain: cfg.Setting("user_domain_name", "Default"),
		projectID: cfg.Setting("project_id", ""), projectName: cfg.Setting("project_name", ""), projectDomain: cfg.Setting("project_domain_name", "Default"),
		stoppedBilled: cfg.Setting("stopped_billing", "billed") != "unbilled", metrics: cfg.Setting("metrics", "auto"),
		caCert: cfg.Setting("ca_cert", ""), gnocchiGranularity: 300,
	}
	if g, err := strconv.Atoi(cfg.Setting("gnocchi_granularity", "300")); err == nil && g > 0 {
		st.gnocchiGranularity = g
	}
	if st.authURL == "" {
		return nil, fmt.Errorf("%w: auth_url", connector.ErrMissingConfig)
	}
	if !strings.HasSuffix(st.authURL, "/v3") {
		st.authURL += "/v3"
	}
	switch {
	case st.appCredID != "":
		if st.appCredSecret == "" {
			return nil, fmt.Errorf("%w: application_credential_secret", connector.ErrMissingConfig)
		}
	case st.username != "":
		if st.password == "" || (st.projectID == "" && st.projectName == "") {
			return nil, fmt.Errorf("%w: password and project_id or project_name", connector.ErrMissingConfig)
		}
	default:
		return nil, fmt.Errorf("%w: application_credential_id or username", connector.ErrMissingConfig)
	}
	hc, err := rest.NewHTTP(rest.Options{CAPEM: st.caCert})
	if err != nil {
		return nil, err
	}
	return &Conn{s: &session{cfg: st, http: hc}, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (c *Conn) Type() string                                { return Type }
func (c *Conn) RequiredPermissions() []connector.Permission { return permissions }

// Validate vérifie l'authentification, le catalogue et la lecture des serveurs.
func (c *Conn) Validate(ctx context.Context, _ connector.Config) error {
	if err := c.s.authenticate(ctx); err != nil {
		return err
	}
	compute, ok := c.s.endpoint("compute")
	if !ok {
		return fmt.Errorf("no compute endpoint for region %q and interface %q in the service catalog", c.s.cfg.region, c.s.cfg.iface)
	}
	var out struct {
		Servers []json.RawMessage `json:"servers"`
	}
	return c.s.client(nil).Get(ctx, rest.Join(compute, "servers", url.Values{"limit": {"1"}}), &out)
}

// Health vérifie que l'authentification fonctionne.
func (c *Conn) Health(ctx context.Context) connector.HealthStatus {
	h := connector.HealthStatus{Status: connector.HealthOK, CheckedAt: c.now()}
	if err := c.s.authenticate(ctx); err != nil {
		h.Status, h.Message = connector.HealthDown, err.Error()
	}
	return h
}

// SyncBilling : OpenStack n'expose pas d'API de facturation standard (coûts estimés par la grille tarifaire).
func (c *Conn) SyncBilling(context.Context, connector.Period) (<-chan connector.CostLine, error) {
	return nil, connector.ErrNotSupported
}

// ------------------------------------------------------------------ inventaire

type flavorInfo struct {
	OriginalName string      `json:"original_name"`
	VCPUs        json.Number `json:"vcpus"`
	RAM          json.Number `json:"ram"` // Mo
	Disk         json.Number `json:"disk"`
}

type server struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Status   string            `json:"status"`
	Created  time.Time         `json:"created"`
	Flavor   flavorInfo        `json:"flavor"`
	Metadata map[string]string `json:"metadata"`
	Image    json.RawMessage   `json:"image"`
	AZ       string            `json:"OS-EXT-AZ:availability_zone"`
}

type link struct {
	Rel  string `json:"rel"`
	Href string `json:"href"`
}

func nextLink(links []link) string {
	for _, l := range links {
		if l.Rel == "next" {
			return l.Href
		}
	}
	return ""
}

func num(n json.Number) float64 {
	f, _ := n.Float64()
	return f
}

// billingState traduit un statut Nova en état de facturation normalisé.
func (c *Conn) billingState(status string) string {
	switch strings.ToUpper(status) {
	case "SHUTOFF", "SUSPENDED", "PAUSED", "STOPPED":
		if c.s.cfg.stoppedBilled {
			return "stopped_billed"
		}
		return "stopped_unbilled"
	case "SHELVED", "SHELVED_OFFLOADED", "DELETED", "SOFT_DELETED":
		return "stopped_unbilled"
	}
	return "running"
}

func (c *Conn) region() string {
	if c.s.cfg.region != "" {
		return c.s.cfg.region
	}
	return "default"
}

func labelsFrom(meta map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range meta {
		if len(k) <= 128 && len(v) <= 256 {
			out[k] = v
		}
	}
	return out
}

// SyncInventory émet le projet, les instances, volumes, instantanés, IP flottantes, répartiteurs et conteneurs.
func (c *Conn) SyncInventory(ctx context.Context, _ time.Time) (<-chan connector.Resource, error) {
	if err := c.s.authenticate(ctx); err != nil {
		return nil, err
	}
	return connector.Stream(ctx, 256, func(ctx context.Context, emit func(connector.Resource) bool) error {
		region := c.region()
		pid := c.s.projectID
		inProject := connector.Edge{Relation: model.RelContains, ParentType: model.TypeProject, ParentExternalID: pid}
		if !emit(connector.Resource{Type: model.TypeProject, ExternalID: pid, Name: c.s.project, Region: region,
			Attributes: map[string]any{"project_id": pid}, Labels: map[string]string{}}) {
			return nil
		}
		alive := map[string]bool{}
		if err := c.servers(ctx, func(sv server) bool {
			alive[sv.ID] = true
			created := sv.Created
			attrs := map[string]any{
				"flavor": sv.Flavor.OriginalName, "vcpus": num(sv.Flavor.VCPUs), "ram_gb": num(sv.Flavor.RAM) / 1024, "disk_gb": num(sv.Flavor.Disk),
				"status": strings.ToUpper(sv.Status), "billing_state": c.billingState(sv.Status), "project_id": pid,
			}
			if sv.AZ != "" {
				attrs["availability_zone"] = sv.AZ
			}
			if lic := sv.Metadata["license"]; lic != "" {
				attrs["license"] = lic
			}
			return emit(connector.Resource{Type: model.TypeInstance, ExternalID: sv.ID, Name: sv.Name, Region: region, Attributes: attrs,
				Labels: labelsFrom(sv.Metadata), Parents: []connector.Edge{inProject}, CreatedAt: &created})
		}); err != nil {
			return err
		}
		if err := c.volumes(ctx, region, inProject, alive, emit); err != nil {
			return err
		}
		if err := c.floatingIPs(ctx, region, inProject, alive, emit); err != nil {
			return err
		}
		if err := c.loadBalancers(ctx, region, inProject, emit); err != nil {
			return err
		}
		return c.containers(ctx, region, inProject, emit)
	}), nil
}

// servers parcourt toutes les pages de /servers/detail avec les détails de gabarit (microversion 2.47).
func (c *Conn) servers(ctx context.Context, fn func(server) bool) error {
	compute, ok := c.s.endpoint("compute")
	if !ok {
		return errors.New("no compute endpoint in the service catalog")
	}
	cl := c.s.client(map[string]string{"OpenStack-API-Version": "compute 2.47", "X-OpenStack-Nova-API-Version": "2.47"})
	next := rest.Join(compute, "servers/detail", url.Values{"limit": {"500"}})
	for next != "" {
		var page struct {
			Servers []server `json:"servers"`
			Links   []link   `json:"servers_links"`
		}
		if err := cl.Get(ctx, next, &page); err != nil {
			return fmt.Errorf("nova: %w", err)
		}
		for _, sv := range page.Servers {
			if !fn(sv) {
				return nil
			}
		}
		next = nextLink(page.Links)
	}
	return nil
}

func (c *Conn) volumes(ctx context.Context, region string, inProject connector.Edge, alive map[string]bool, emit func(connector.Resource) bool) error {
	base, ok := c.s.endpoint("volumev3", "block-storage", "volumev2")
	if !ok {
		return nil // pas de stockage bloc dans ce cloud
	}
	cl := c.s.client(nil)
	next := rest.Join(base, "volumes/detail", url.Values{"limit": {"500"}})
	for next != "" {
		var page struct {
			Volumes []struct {
				ID          string            `json:"id"`
				Name        string            `json:"name"`
				Size        json.Number       `json:"size"`
				VolumeType  string            `json:"volume_type"`
				Status      string            `json:"status"`
				Bootable    string            `json:"bootable"`
				CreatedAt   string            `json:"created_at"`
				Metadata    map[string]string `json:"metadata"`
				Attachments []struct {
					ServerID string `json:"server_id"`
				} `json:"attachments"`
			} `json:"volumes"`
			Links []link `json:"volumes_links"`
		}
		if err := cl.Get(ctx, next, &page); err != nil {
			return fmt.Errorf("cinder volumes: %w", err)
		}
		for _, v := range page.Volumes {
			attrs := map[string]any{"size_gb": num(v.Size), "volume_type": v.VolumeType, "status": v.Status, "project_id": inProject.ParentExternalID,
				"bootable": v.Bootable == "true"}
			parents := []connector.Edge{inProject}
			for _, a := range v.Attachments {
				if alive[a.ServerID] {
					attrs["attached_to"] = a.ServerID
					parents = append(parents, connector.Edge{Relation: model.RelAttachedTo, ParentType: model.TypeInstance, ParentExternalID: a.ServerID})
					break
				}
			}
			if !emit(connector.Resource{Type: model.TypeVolume, ExternalID: v.ID, Name: v.Name, Region: region, Attributes: attrs,
				Labels: labelsFrom(v.Metadata), Parents: parents, CreatedAt: parseTime(v.CreatedAt)}) {
				return nil
			}
		}
		next = nextLink(page.Links)
	}
	next = rest.Join(base, "snapshots/detail", url.Values{"limit": {"500"}})
	for next != "" {
		var page struct {
			Snapshots []struct {
				ID        string      `json:"id"`
				Name      string      `json:"name"`
				Size      json.Number `json:"size"`
				VolumeID  string      `json:"volume_id"`
				Status    string      `json:"status"`
				CreatedAt string      `json:"created_at"`
			} `json:"snapshots"`
			Links []link `json:"snapshots_links"`
		}
		if err := cl.Get(ctx, next, &page); err != nil {
			return fmt.Errorf("cinder snapshots: %w", err)
		}
		for _, s := range page.Snapshots {
			created := parseTime(s.CreatedAt)
			attrs := map[string]any{"size_gb": num(s.Size), "volume_id": s.VolumeID, "status": s.Status, "project_id": inProject.ParentExternalID}
			if created != nil {
				attrs["created_at"] = created.Format(time.RFC3339)
			}
			if !emit(connector.Resource{Type: model.TypeSnapshot, ExternalID: s.ID, Name: s.Name, Region: region, Attributes: attrs,
				Labels: map[string]string{}, Parents: []connector.Edge{inProject}, CreatedAt: created}) {
				return nil
			}
		}
		next = nextLink(page.Links)
	}
	return nil
}

func (c *Conn) floatingIPs(ctx context.Context, region string, inProject connector.Edge, alive map[string]bool, emit func(connector.Resource) bool) error {
	base, ok := c.s.endpoint("network")
	if !ok {
		return nil
	}
	cl := c.s.client(nil)
	var page struct {
		FloatingIPs []struct {
			ID          string `json:"id"`
			Address     string `json:"floating_ip_address"`
			Status      string `json:"status"`
			PortID      string `json:"port_id"`
			CreatedAt   string `json:"created_at"`
			PortDetails *struct {
				DeviceID    string `json:"device_id"`
				DeviceOwner string `json:"device_owner"`
			} `json:"port_details"`
		} `json:"floatingips"`
	}
	if err := cl.Get(ctx, rest.Join(base, "v2.0/floatingips", url.Values{"project_id": {inProject.ParentExternalID}}), &page); err != nil {
		return fmt.Errorf("neutron floating ips: %w", err)
	}
	for _, ip := range page.FloatingIPs {
		attrs := map[string]any{"ip_kind": "floating", "address": ip.Address, "status": ip.Status, "project_id": inProject.ParentExternalID}
		parents := []connector.Edge{inProject}
		if ip.PortDetails != nil && alive[ip.PortDetails.DeviceID] {
			attrs["attached_to"] = ip.PortDetails.DeviceID
			parents = append(parents, connector.Edge{Relation: model.RelAttachedTo, ParentType: model.TypeInstance, ParentExternalID: ip.PortDetails.DeviceID})
		}
		name := ip.Address
		if name == "" {
			name = ip.ID
		}
		if !emit(connector.Resource{Type: model.TypeIP, ExternalID: ip.ID, Name: name, Region: region, Attributes: attrs,
			Labels: map[string]string{}, Parents: parents, CreatedAt: parseTime(ip.CreatedAt)}) {
			return nil
		}
	}
	return nil
}

func (c *Conn) loadBalancers(ctx context.Context, region string, inProject connector.Edge, emit func(connector.Resource) bool) error {
	base, ok := c.s.endpoint("load-balancer")
	if !ok {
		return nil
	}
	cl := c.s.client(nil)
	flavors := map[string]string{}
	var fl struct {
		Flavors []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"flavors"`
	}
	if err := cl.Get(ctx, rest.Join(base, "v2/lbaas/flavors", nil), &fl); err == nil {
		for _, f := range fl.Flavors {
			flavors[f.ID] = f.Name
		}
	}
	var page struct {
		LBs []struct {
			ID                 string `json:"id"`
			Name               string `json:"name"`
			FlavorID           string `json:"flavor_id"`
			ProvisioningStatus string `json:"provisioning_status"`
			OperatingStatus    string `json:"operating_status"`
			VIP                string `json:"vip_address"`
			CreatedAt          string `json:"created_at"`
		} `json:"loadbalancers"`
	}
	if err := cl.Get(ctx, rest.Join(base, "v2/lbaas/loadbalancers", url.Values{"project_id": {inProject.ParentExternalID}}), &page); err != nil {
		if errors.Is(err, connector.ErrPermission) || rest.IsNotFound(err) {
			return nil // permission optionnelle
		}
		return fmt.Errorf("octavia: %w", err)
	}
	for _, lb := range page.LBs {
		attrs := map[string]any{"flavor": flavors[lb.FlavorID], "status": lb.ProvisioningStatus, "operating_status": lb.OperatingStatus,
			"vip_address": lb.VIP, "project_id": inProject.ParentExternalID}
		if !emit(connector.Resource{Type: model.TypeLoadBalancer, ExternalID: lb.ID, Name: lb.Name, Region: region, Attributes: attrs,
			Labels: map[string]string{}, Parents: []connector.Edge{inProject}, CreatedAt: parseTime(lb.CreatedAt)}) {
			return nil
		}
	}
	return nil
}

func (c *Conn) containers(ctx context.Context, region string, inProject connector.Edge, emit func(connector.Resource) bool) error {
	base, ok := c.s.endpoint("object-store")
	if !ok {
		return nil
	}
	var list []struct {
		Name  string      `json:"name"`
		Count json.Number `json:"count"`
		Bytes json.Number `json:"bytes"`
	}
	if err := c.s.client(nil).Get(ctx, base+"?"+url.Values{"format": {"json"}, "limit": {"10000"}}.Encode(), &list); err != nil {
		if errors.Is(err, connector.ErrPermission) || rest.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("swift: %w", err)
	}
	for _, ct := range list {
		attrs := map[string]any{"storage_class": "standard", "objects": num(ct.Count), "size_gb": num(ct.Bytes) / 1e9, "project_id": inProject.ParentExternalID}
		if !emit(connector.Resource{Type: model.TypeBucket, ExternalID: ct.Name, Name: ct.Name, Region: region, Attributes: attrs,
			Labels: map[string]string{}, Parents: []connector.Edge{inProject}}) {
			return nil
		}
	}
	return nil
}

func parseTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.000000", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			t = t.UTC()
			return &t
		}
	}
	return nil
}
