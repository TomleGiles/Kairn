package outscale

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/connector"
	"github.com/kairn-io/kairn/pkg/model"
)

func fake(t *testing.T, c *Conn) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		ts, _ := time.Parse("20060102T150405Z", r.Header.Get("X-Osc-Date"))
		want, _ := c.signature(r.Host, r.URL.Path, body, ts)
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != want {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		write := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		switch strings.TrimPrefix(r.URL.Path, "/api/v1/") {
		case "ReadVms":
			write(map[string]any{"Vms": []any{
				map[string]any{"VmId": "i-1", "VmType": "tinav6.c2r4p2", "State": "running", "CreationDate": "2026-06-01T08:00:00.000Z",
					"Tags": []any{map[string]any{"Key": "Name", "Value": "web"}, map[string]any{"Key": "team", "Value": "shop"}}, "Placement": map[string]any{"SubregionName": "eu-west-2a"}},
				map[string]any{"VmId": "i-2", "VmType": "tinav6.c4r8p2", "State": "terminated"},
			}})
		case "ReadVolumes":
			write(map[string]any{"Volumes": []any{map[string]any{"VolumeId": "vol-1", "Size": 100, "VolumeType": "gp2", "State": "in-use",
				"LinkedVolumes": []any{map[string]any{"VmId": "i-1"}}}}})
		case "ReadSnapshots":
			write(map[string]any{"Snapshots": []any{}})
		case "ReadPublicIps":
			write(map[string]any{"PublicIps": []any{map[string]any{"PublicIpId": "eipalloc-1", "PublicIp": "171.33.1.1", "VmId": "i-1"}}})
		case "ReadLoadBalancers":
			write(map[string]any{"LoadBalancers": []any{}})
		case "ReadConsumptionAccount":
			var in map[string]any
			_ = json.Unmarshal(body, &in)
			if in["ShowPrice"] != true {
				t.Errorf("prices must be requested")
			}
			write(map[string]any{"Currency": "EUR", "ConsumptionEntries": []any{
				map[string]any{"Category": "compute", "Service": "TinaOS-FCU", "Type": "BoxUsage:tinav6.c2r4p2", "Value": 24, "Price": 1.2345},
				map[string]any{"Category": "storage", "Service": "TinaOS-FCU", "Type": "BSU:VolumeUsage:gp2", "Value": 2400, "Price": 0.0},
			}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestOutscale(t *testing.T) {
	cc, err := New(connector.Config{Settings: map[string]string{"access_key": "AK"}, Secrets: map[string]string{"secret_key": "SK"}})
	if err != nil {
		t.Fatal(err)
	}
	c := cc.(*Conn)
	c.now = func() time.Time { return time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC) }
	srv := fake(t, c)
	c.endpoint = srv.URL
	if err := c.Validate(context.Background(), connector.Config{}); err != nil {
		t.Fatal(err)
	}
	ctx, sink := connector.WithErrorSink(context.Background())
	ch, _ := c.SyncInventory(ctx, time.Time{})
	by := map[string]connector.Resource{}
	for _, r := range connector.Collect(ch) {
		by[r.ExternalID] = r
	}
	if err := sink.Err(); err != nil {
		t.Fatal(err)
	}
	if len(by) != 3 || by["i-1"].Name != "web" || by["i-1"].Attributes["vcpus"] != 2 || by["i-1"].Attributes["ram_gb"] != 4 || by["i-1"].Labels["team"] != "shop" {
		t.Fatalf("inventory (terminated VM excluded): %+v", by)
	}
	if by["vol-1"].Attributes["attached_to"] != "i-1" || by["eipalloc-1"].Attributes["attached_to"] != "i-1" {
		t.Fatalf("attachments: %+v", by)
	}
	bctx, bsink := connector.WithErrorSink(context.Background())
	bch, _ := c.SyncBilling(bctx, connector.Period{From: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)})
	lines := connector.Collect(bch)
	if err := bsink.Err(); err != nil {
		t.Fatal(err)
	}
	// Jours 1 et 2 (aujourd'hui), lignes à prix nul ignorées.
	if len(lines) != 2 || !lines[0].Amount.Equal(decimal.RequireFromString("1.2345")) || lines[0].CostType != model.CostCompute {
		t.Fatalf("billing: %+v", lines)
	}
	if err := c.call(context.Background(), "DeleteVms", nil, nil); err == nil {
		t.Fatal("non-read actions must be refused")
	}
}
