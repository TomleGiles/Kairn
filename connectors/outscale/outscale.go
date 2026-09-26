// Package outscale est le connecteur 3DS OUTSCALE (M-01, R4) : inventaire
// (VM, volumes BSU, instantanés, IP publiques, répartiteurs) et consommation
// facturée journalière (ReadConsumptionAccount avec prix). Seuls des appels
// « Read* » sont émis ; une paire de clés d'accès rattachée à un utilisateur
// EIM en lecture seule suffit (docs/connectors/outscale.md).
package outscale

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/connectors/internal/rest"
	"github.com/kairn-io/kairn/pkg/connector"
	"github.com/kairn-io/kairn/pkg/model"
)

// Type est l'identifiant du connecteur.
const Type = "outscale"

var permissions = []connector.Permission{
	{Scope: "api:Read*", Description: "Lecture des VM, volumes, instantanés, IP publiques et répartiteurs (politique EIM en lecture seule)."},
	{Scope: "api:ReadConsumptionAccount", Description: "Consommation et prix (rapprochement estimé / facturé).", Optional: true},
}

func init() {
	connector.Register(connector.TypeInfo{
		Type: Type, DisplayName: "3DS OUTSCALE", Category: connector.CategoryCloud, Provider: "outscale",
		Resources:       []string{model.TypeInstance, model.TypeVolume, model.TypeSnapshot, model.TypeIP, model.TypeLoadBalancer},
		DefaultInterval: time.Hour, Billing: true, DocsURL: "/docs/connectors/outscale", Permissions: permissions,
		Fields: []connector.Field{
			{Name: "region", Label: "Région", Default: "eu-west-2", Help: "eu-west-2, cloudgouv-eu-west-1 (SecNumCloud), us-east-2…"},
			{Name: "access_key", Label: "Access key", Required: true},
			{Name: "secret_key", Label: "Secret key", Secret: true, Required: true},
		},
	}, New)
}

// Conn est une instance du connecteur.
type Conn struct {
	region, ak, sk string
	endpoint       string
	http           *http.Client
	now            func() time.Time
}

// New construit le connecteur.
func New(cfg connector.Config) (connector.Connector, error) {
	if err := cfg.Require("access_key", "secret_key"); err != nil {
		return nil, err
	}
	region := cfg.Setting("region", "eu-west-2")
	if !regexp.MustCompile(`^[a-z0-9-]+$`).MatchString(region) {
		return nil, fmt.Errorf("invalid region %q", region)
	}
	hc, err := rest.NewHTTP(rest.Options{})
	if err != nil {
		return nil, err
	}
	return &Conn{region: region, ak: cfg.Setting("access_key", ""), sk: cfg.Secret("secret_key"), http: hc,
		endpoint: "https://api." + region + ".outscale.com", now: func() time.Time { return time.Now().UTC() }}, nil
}

func (c *Conn) Type() string                                { return Type }
func (c *Conn) RequiredPermissions() []connector.Permission { return permissions }

func (c *Conn) SyncMetrics(context.Context, connector.TimeWindow) (<-chan connector.MetricPoint, error) {
	return nil, connector.ErrNotSupported
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// signature calcule l'en-tête Authorization OSC4-HMAC-SHA256 (dérivé de SigV4, service « api »).
func (c *Conn) signature(host, path string, body []byte, t time.Time) (auth, stamp string) {
	stamp = t.UTC().Format("20060102T150405Z")
	date := stamp[:8]
	canonical := strings.Join([]string{http.MethodPost, path, "",
		"content-type:application/json", "host:" + host, "x-osc-date:" + stamp, "",
		"content-type;host;x-osc-date", sha256Hex(body)}, "\n")
	scope := date + "/" + c.region + "/api/osc4_request"
	toSign := "OSC4-HMAC-SHA256\n" + stamp + "\n" + scope + "\n" + sha256Hex([]byte(canonical))
	k := hmacSHA256([]byte("OSC4"+c.sk), date)
	k = hmacSHA256(k, c.region)
	k = hmacSHA256(k, "api")
	k = hmacSHA256(k, "osc4_request")
	sig := hex.EncodeToString(hmacSHA256(k, toSign))
	return "OSC4-HMAC-SHA256 Credential=" + c.ak + "/" + scope + ", SignedHeaders=content-type;host;x-osc-date, Signature=" + sig, stamp
}

// call invoque une action de lecture de l'API OSC.
func (c *Conn) call(ctx context.Context, action string, in, out any) error {
	if !strings.HasPrefix(action, "Read") {
		return fmt.Errorf("outscale: refusing non-read action %s", action) // garde-fou : connecteur en lecture seule
	}
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	path := "/api/v1/" + action
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	auth, stamp := c.signature(req.URL.Host, path, body, c.now())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Osc-Date", stamp)
	req.Header.Set("Authorization", auth)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("outscale %s: %w", action, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%w: outscale %s: HTTP %d", connector.ErrPermission, action, resp.StatusCode)
	case resp.StatusCode >= 300:
		return fmt.Errorf("outscale %s: HTTP %d", action, resp.StatusCode)
	}
	dec := json.NewDecoder(resp.Body)
	dec.UseNumber()
	return dec.Decode(out)
}

func (c *Conn) Validate(ctx context.Context, _ connector.Config) error {
	var out json.RawMessage
	return c.call(ctx, "ReadVms", map[string]any{"ResultsPerPage": 1}, &out)
}

func (c *Conn) Health(ctx context.Context) connector.HealthStatus {
	h := connector.HealthStatus{Status: connector.HealthOK, CheckedAt: c.now()}
	if err := c.Validate(ctx, connector.Config{}); err != nil {
		h.Status, h.Message = connector.HealthDown, err.Error()
	}
	return h
}

type tag struct{ Key, Value string }

func labels(tags []tag) map[string]string {
	out := map[string]string{}
	for _, t := range tags {
		out[t.Key] = t.Value
	}
	return out
}

func nameOf(tags []tag, def string) string {
	for _, t := range tags {
		if t.Key == "Name" && t.Value != "" {
			return t.Value
		}
	}
	return def
}

func created(s string) *time.Time {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		t = t.UTC()
		return &t
	}
	return nil
}

var shape = regexp.MustCompile(`c(\d+)r(\d+)`)

// tinaClass extrait génération et performance d'un type tinavG.cXrYpZ.
var tinaClass = regexp.MustCompile(`^tinav(\d+)\.c\d+r\d+p(\d+)$`)

// SyncInventory émet VM, volumes, instantanés, IP publiques et répartiteurs.
func (c *Conn) SyncInventory(ctx context.Context, _ time.Time) (<-chan connector.Resource, error) {
	return connector.Stream(ctx, 256, func(ctx context.Context, emit func(connector.Resource) bool) error {
		var vms struct {
			Vms []struct {
				VmID         string `json:"VmId"`
				VmType       string
				State        string
				CreationDate string
				Tags         []tag
				Placement    struct{ SubregionName string }
			}
		}
		if err := c.call(ctx, "ReadVms", map[string]any{}, &vms); err != nil {
			return err
		}
		alive := map[string]bool{}
		for _, vm := range vms.Vms {
			if vm.State == "terminated" {
				continue
			}
			alive[vm.VmID] = true
			attrs := map[string]any{"flavor": vm.VmType, "status": vm.State, "billing_state": "running", "availability_zone": vm.Placement.SubregionName}
			if vm.State == "stopped" {
				attrs["billing_state"] = "stopped_unbilled" // vCPU et RAM ne sont plus facturés, les volumes si
			}
			if m := shape.FindStringSubmatch(vm.VmType); m != nil {
				v, _ := strconv.Atoi(m[1])
				r, _ := strconv.Atoi(m[2])
				attrs["vcpus"], attrs["ram_gb"] = v, r
			}
			if m := tinaClass.FindStringSubmatch(vm.VmType); m != nil {
				// Tarification par vCore selon la génération et la performance (grille publique compute.vcpu.v6-p2).
				attrs["cpu_class"] = "v" + m[1] + "-p" + m[2]
			}
			if !emit(connector.Resource{Type: model.TypeInstance, ExternalID: vm.VmID, Name: nameOf(vm.Tags, vm.VmID), Region: c.region,
				Attributes: attrs, Labels: labels(vm.Tags), CreatedAt: created(vm.CreationDate)}) {
				return nil
			}
		}
		var vols struct {
			Volumes []struct {
				VolumeID      string `json:"VolumeId"`
				Size          json.Number
				VolumeType    string
				State         string
				CreationDate  string
				LinkedVolumes []struct {
					VmID string `json:"VmId"`
				}
				Tags []tag
			}
		}
		if err := c.call(ctx, "ReadVolumes", map[string]any{}, &vols); err != nil {
			return err
		}
		for _, v := range vols.Volumes {
			size, _ := v.Size.Float64()
			attrs := map[string]any{"size_gb": size, "volume_type": v.VolumeType, "status": v.State}
			var parents []connector.Edge
			for _, l := range v.LinkedVolumes {
				if alive[l.VmID] {
					attrs["attached_to"] = l.VmID
					parents = append(parents, connector.Edge{Relation: model.RelAttachedTo, ParentType: model.TypeInstance, ParentExternalID: l.VmID})
					break
				}
			}
			if !emit(connector.Resource{Type: model.TypeVolume, ExternalID: v.VolumeID, Name: nameOf(v.Tags, v.VolumeID), Region: c.region,
				Attributes: attrs, Labels: labels(v.Tags), Parents: parents, CreatedAt: created(v.CreationDate)}) {
				return nil
			}
		}
		var snaps struct {
			Snapshots []struct {
				SnapshotID   string `json:"SnapshotId"`
				VolumeSize   json.Number
				VolumeID     string `json:"VolumeId"`
				CreationDate string
				Description  string
				Tags         []tag
			}
		}
		if err := c.call(ctx, "ReadSnapshots", map[string]any{}, &snaps); err != nil {
			return err
		}
		for _, s := range snaps.Snapshots {
			size, _ := s.VolumeSize.Float64()
			t := created(s.CreationDate)
			attrs := map[string]any{"size_gb": size, "volume_id": s.VolumeID}
			if t != nil {
				attrs["created_at"] = t.Format(time.RFC3339)
			}
			if !emit(connector.Resource{Type: model.TypeSnapshot, ExternalID: s.SnapshotID, Name: nameOf(s.Tags, s.SnapshotID), Region: c.region,
				Attributes: attrs, Labels: labels(s.Tags), CreatedAt: t}) {
				return nil
			}
		}
		var ips struct {
			PublicIps []struct {
				PublicIPID string `json:"PublicIpId"`
				PublicIP   string `json:"PublicIp"`
				VMID       string `json:"VmId"`
				Tags       []tag
			}
		}
		if err := c.call(ctx, "ReadPublicIps", map[string]any{}, &ips); err != nil {
			return err
		}
		for _, ip := range ips.PublicIps {
			attrs := map[string]any{"ip_kind": "floating", "address": ip.PublicIP}
			var parents []connector.Edge
			if alive[ip.VMID] {
				attrs["attached_to"] = ip.VMID
				parents = append(parents, connector.Edge{Relation: model.RelAttachedTo, ParentType: model.TypeInstance, ParentExternalID: ip.VMID})
			}
			if !emit(connector.Resource{Type: model.TypeIP, ExternalID: ip.PublicIPID, Name: ip.PublicIP, Region: c.region, Attributes: attrs,
				Labels: labels(ip.Tags), Parents: parents}) {
				return nil
			}
		}
		var lbs struct {
			LoadBalancers []struct {
				LoadBalancerName string
				LoadBalancerType string
				Tags             []tag
			}
		}
		if err := c.call(ctx, "ReadLoadBalancers", map[string]any{}, &lbs); err != nil {
			return err
		}
		for _, lb := range lbs.LoadBalancers {
			if !emit(connector.Resource{Type: model.TypeLoadBalancer, ExternalID: lb.LoadBalancerName, Name: lb.LoadBalancerName, Region: c.region,
				Attributes: map[string]any{"scheme": lb.LoadBalancerType}, Labels: labels(lb.Tags)}) {
				return nil
			}
		}
		return nil
	}), nil
}

// SyncBilling lit la consommation jour par jour, prix inclus (ShowPrice).
func (c *Conn) SyncBilling(ctx context.Context, period connector.Period) (<-chan connector.CostLine, error) {
	return connector.Stream(ctx, 256, func(ctx context.Context, emit func(connector.CostLine) bool) error {
		today := time.Date(c.now().Year(), c.now().Month(), c.now().Day(), 0, 0, 0, 0, time.UTC)
		for d := period.From; d.Before(period.To) && !d.After(today); d = d.AddDate(0, 0, 1) {
			var out struct {
				Currency           string
				ConsumptionEntries []struct {
					Category  string
					Service   string
					Operation string
					Type      string
					Title     string
					Value     json.Number
					Price     json.Number
				}
			}
			in := map[string]any{"FromDate": d.Format("2006-01-02"), "ToDate": d.AddDate(0, 0, 1).Format("2006-01-02"), "ShowPrice": true}
			if err := c.call(ctx, "ReadConsumptionAccount", in, &out); err != nil {
				return err
			}
			for _, e := range out.ConsumptionEntries {
				amount, err := decimal.NewFromString(string(e.Price))
				if err != nil || amount.IsZero() {
					continue
				}
				qty, _ := decimal.NewFromString(string(e.Value))
				if !emit(connector.CostLine{Day: d, Service: e.Service, SKU: e.Type, CostType: category(e.Category, e.Service), Quantity: qty,
					Unit: model.UnitUnit, Amount: amount, Currency: out.Currency, InvoiceID: "outscale:" + d.Format("2006-01")}) {
					return nil
				}
			}
		}
		return nil
	}), nil
}

func category(cat, service string) string {
	s := strings.ToLower(cat + " " + service)
	switch {
	case strings.Contains(s, "compute"), strings.Contains(s, "vm"), strings.Contains(s, "licen"):
		return model.CostCompute
	case strings.Contains(s, "storage"), strings.Contains(s, "bsu"), strings.Contains(s, "snapshot"), strings.Contains(s, "oos"):
		return model.CostStorage
	case strings.Contains(s, "network"), strings.Contains(s, "ip"), strings.Contains(s, "lbu"), strings.Contains(s, "bandwidth"):
		return model.CostNetwork
	}
	return model.CostOther
}
