package ovh

import (
	"context"
	"errors"
	"testing"

	"github.com/kairn-io/kairn/pkg/connector"
	"github.com/kairn-io/kairn/pkg/model"
)

func TestHelpers(t *testing.T) {
	for in, want := range map[string]string{"octavia-loadbalancer": model.CostNetwork, "gateway": model.CostNetwork, "volume-backup": model.CostStorage,
		"storage-standard": model.CostStorage, "rancher": model.CostCompute} {
		if costTypeOf(in) != want {
			t.Fatalf("costTypeOf(%s)", in)
		}
	}
	for in, want := range map[string]string{"Hour": model.UnitHour, "GiB": model.UnitGBMonth, "x": model.UnitUnit} {
		if unitOf(in) != want {
			t.Fatalf("unitOf(%s)", in)
		}
	}
	if _, err := New(connector.Config{Settings: map[string]string{"application_key": "a", "endpoint": "ftp://x"}, Secrets: map[string]string{"application_secret": "s", "consumer_key": "c"}}); err == nil {
		t.Fatal("non-https endpoint accepted")
	}
	c, err := New(connector.Config{Settings: map[string]string{"application_key": "a", "endpoint": "ovh-ca", "project_ids": "p1, p2"},
		Secrets: map[string]string{"application_secret": "s", "consumer_key": "c"}})
	if err != nil {
		t.Fatal(err)
	}
	conn := c.(*Conn)
	if conn.c.base != endpoints["ovh-ca"] || len(conn.projects) != 2 || conn.Type() != Type || len(conn.RequiredPermissions()) == 0 {
		t.Fatalf("config: %+v", conn)
	}
	if _, err := conn.SyncMetrics(context.Background(), connector.TimeWindow{}); !errors.Is(err, connector.ErrNotSupported) {
		t.Fatal("metrics unsupported")
	}
}
