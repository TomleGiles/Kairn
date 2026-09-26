package all

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kairn-io/kairn/pkg/connector"
)

// settings minimaux par type, pointant vers un serveur local qui refuse tout
// (aucun appel réseau externe pendant les tests).
func minimal(typ, down string) (map[string]string, map[string]string, bool) {
	switch typ {
	case "openstack":
		return map[string]string{"auth_url": down + "/v3", "application_credential_id": "id"}, map[string]string{"application_credential_secret": "s"}, true
	case "kubernetes":
		return map[string]string{"cluster_name": "c", "api_server": down}, map[string]string{"token": "t"}, true
	case "prometheus":
		return map[string]string{"url": down, "cluster_name": "c"}, nil, true
	case "ovh":
		return map[string]string{"endpoint": strings.Replace(down, "http://", "https://", 1), "application_key": "ak"},
			map[string]string{"application_secret": "as", "consumer_key": "ck"}, true
	case "focus":
		return map[string]string{"source": strings.Replace(down, "http://", "https://", 1) + "/export.csv"}, nil, true
	case "gitlab", "github", "argocd", "flux", "alertmanager", "pagerduty", "opsgenie", "agent":
		return map[string]string{}, nil, true
	}
	return nil, nil, false // connecteurs sans point d'accès configurable (Scaleway, Outscale) : testés dans leur paquet
}

// TestConnectorConformance vérifie le contrat de chaque connecteur enregistré :
// description complète, lecture seule documentée, configuration obligatoire
// contrôlée, méthodes optionnelles signalées par ErrNotSupported, santé sans panique.
func TestConnectorConformance(t *testing.T) {
	refuse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer refuse.Close()
	types := connector.Types()
	if len(types) < 15 {
		t.Fatalf("registered connectors: %d", len(types))
	}
	for _, info := range types {
		t.Run(info.Type, func(t *testing.T) {
			if info.DisplayName == "" || info.Category == "" || info.Provider == "" {
				t.Fatalf("incomplete type info: %+v", info)
			}
			if !strings.HasPrefix(info.Type, "demo-") && len(info.Permissions) == 0 {
				t.Fatal("required permissions must be documented")
			}
			for _, f := range info.Fields {
				if f.Name == "" || f.Label == "" {
					t.Fatalf("field without name or label: %+v", f)
				}
			}
			// Les secrets ne sont jamais des paramètres en clair : un champ secret doit être marqué comme tel.
			for _, f := range info.Fields {
				n := strings.ToLower(f.Name)
				if (strings.Contains(n, "password") || strings.Contains(n, "secret") || n == "token") && !f.Secret {
					t.Fatalf("field %s must be secret", f.Name)
				}
			}
			settings, secrets, ok := minimal(info.Type, refuse.URL)
			if !ok {
				return
			}
			c, err := connector.New(info.Type, connector.Config{OrgID: "o", ConnectorID: "c", Settings: settings, Secrets: secrets})
			if err != nil {
				t.Fatalf("new: %v", err)
			}
			if c.Type() != info.Type || c.RequiredPermissions() == nil && info.Type != "agent" && !strings.HasPrefix(info.Type, "demo-") && info.Category != connector.CategoryEvents {
				t.Fatalf("type/permissions: %s %v", c.Type(), c.RequiredPermissions())
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			h := c.Health(ctx)
			if h.Status == "" || h.CheckedAt.IsZero() {
				t.Fatalf("health: %+v", h)
			}
			if _, isPush := c.(connector.PushOnly); isPush || info.Category == connector.CategoryEvents {
				if _, err := c.SyncInventory(ctx, time.Time{}); !errors.Is(err, connector.ErrNotSupported) {
					t.Fatalf("push-only connector must not pull inventory: %v", err)
				}
				return
			}
			if !info.Billing {
				if _, err := c.SyncBilling(ctx, connector.Period{}); !errors.Is(err, connector.ErrNotSupported) {
					t.Fatalf("billing must be ErrNotSupported: %v", err)
				}
			}
			if !info.Metrics {
				if _, err := c.SyncMetrics(ctx, connector.TimeWindow{}); !errors.Is(err, connector.ErrNotSupported) {
					t.Fatalf("metrics must be ErrNotSupported: %v", err)
				}
			}
			// Contre un serveur qui refuse tout, la validation échoue proprement (jamais de panique).
			if err := c.Validate(ctx, connector.Config{}); err == nil {
				t.Fatal("validation against a refusing server must fail")
			}
			if h.Status == connector.HealthOK && info.Category != connector.CategoryEvents {
				t.Fatalf("health must be down against a refusing server: %+v", h)
			}
		})
	}
	// Configuration obligatoire manquante.
	for _, typ := range []string{"openstack", "kubernetes", "prometheus", "ovh", "scaleway", "outscale", "focus"} {
		if _, err := connector.New(typ, connector.Config{}); err == nil {
			t.Fatalf("%s: empty configuration accepted", typ)
		}
	}
}
