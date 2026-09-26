package scaleway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kairn-io/kairn/pkg/connector"
	"github.com/kairn-io/kairn/pkg/model"
)

func TestHelpersAndHealth(t *testing.T) {
	for cat, want := range map[string]string{"Compute": model.CostCompute, "Storage": model.CostStorage, "Network": model.CostNetwork, "Serverless": model.CostCompute, "Support": model.CostOther} {
		if got := categoryCost(cat); got != want {
			t.Fatalf("categoryCost(%s) = %s", cat, got)
		}
	}
	if regionOf("fr-par-1") != "fr-par" || regionOf("x") != "x" {
		t.Fatal("regionOf")
	}
	if l := tagsToLabels([]string{"a=1", "b:2", "flag", ""}); l["a"] != "1" || l["b"] != "2" || l["flag"] != "true" || len(l) != 3 {
		t.Fatalf("labels: %v", l)
	}
	if (money{Units: 1, Nanos: 230000000}).decimal().String() != "1.23" {
		t.Fatal("money")
	}
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer down.Close()
	c := newConn(t, down)
	if c.Type() != Type || len(c.RequiredPermissions()) == 0 {
		t.Fatal("type/permissions")
	}
	if h := c.Health(context.Background()); h.Status != connector.HealthDown {
		t.Fatalf("health: %+v", h)
	}
	if _, err := c.SyncMetrics(context.Background(), connector.TimeWindow{}); !errors.Is(err, connector.ErrNotSupported) {
		t.Fatal("metrics unsupported")
	}
	if !isOptional(errors.Join(connector.ErrPermission)) {
		t.Fatal("permission errors are optional for secondary services")
	}
}
