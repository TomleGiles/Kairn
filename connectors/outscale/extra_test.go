package outscale

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kairn-io/kairn/pkg/connector"
	"github.com/kairn-io/kairn/pkg/model"
)

func TestHelpersAndHealth(t *testing.T) {
	for in, want := range map[[2]string]string{
		{"compute", "TinaOS-FCU"}: model.CostCompute, {"storage", "BSU"}: model.CostStorage, {"network", "LBU"}: model.CostNetwork,
		{"", "Licence Windows"}: model.CostCompute, {"support", "x"}: model.CostOther,
	} {
		if got := category(in[0], in[1]); got != want {
			t.Fatalf("category(%v) = %s", in, got)
		}
	}
	if _, err := New(connector.Config{Settings: map[string]string{"access_key": "a", "region": "eu west"}, Secrets: map[string]string{"secret_key": "s"}}); err == nil {
		t.Fatal("invalid region accepted")
	}
	cc, _ := New(connector.Config{Settings: map[string]string{"access_key": "AK"}, Secrets: map[string]string{"secret_key": "SK"}})
	c := cc.(*Conn)
	c.endpoint = "http://127.0.0.1:1"
	c.now = func() time.Time { return time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC) }
	if c.Type() != Type || len(c.RequiredPermissions()) == 0 {
		t.Fatal("type/permissions")
	}
	if h := c.Health(context.Background()); h.Status != connector.HealthDown {
		t.Fatalf("health: %+v", h)
	}
	if _, err := c.SyncMetrics(context.Background(), connector.TimeWindow{}); !errors.Is(err, connector.ErrNotSupported) {
		t.Fatal("metrics unsupported")
	}
	// Signature déterministe : même entrée, même en-tête ; corps différent, signature différente.
	a1, s1 := c.signature("api.eu-west-2.outscale.com", "/api/v1/ReadVms", []byte("{}"), c.now())
	a2, _ := c.signature("api.eu-west-2.outscale.com", "/api/v1/ReadVms", []byte("{}"), c.now())
	a3, _ := c.signature("api.eu-west-2.outscale.com", "/api/v1/ReadVms", []byte(`{"x":1}`), c.now())
	if a1 != a2 || a1 == a3 || s1 != "20260901T000000Z" {
		t.Fatal("signature")
	}
}
