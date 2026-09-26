package api

import (
	"net/http"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/kairn-io/kairn/connectors/demo"
)

var pathParam = regexp.MustCompile(`\{([a-z_]+)\}`)

// TestTenantIsolationAllEndpoints appelle chaque opération d'organisation avec
// les identifiants d'une autre organisation (session utilisateur et jeton
// d'API) : aucune ne doit réussir ni échouer en erreur serveur, et aucune
// réponse ne doit contenir d'identifiant de l'organisation visée.
func TestTenantIsolationAllEndpoints(t *testing.T) {
	e := newEnv(t)
	alice := e.login("alice@victim.example")
	victim := e.org(alice, "Victim Corp")
	// Données de l'organisation visée.
	r := e.do(http.MethodPost, "/api/v1/orgs/"+victim.ID+"/connectors", alice, map[string]any{
		"type": demo.TypeOpenStack, "name": "prod", "settings": map[string]string{"seed": "x"},
	})
	expect(t, r, http.StatusCreated)
	var conn connectorView
	r.JSON(t, &conn)

	mallory := e.login("mallory@attacker.example")
	attacker := e.org(mallory, "Attacker Inc")
	tokResp := e.do(http.MethodPost, "/api/v1/orgs/"+attacker.ID+"/tokens", mallory, map[string]any{"name": "t", "role": "admin"})
	expect(t, tokResp, http.StatusCreated)
	var tok createdToken
	tokResp.JSON(t, &tok)

	doc := e.srv.API.OpenAPI()
	paths := make([]string, 0, len(doc.Paths))
	for p := range doc.Paths {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	checked := 0
	for _, p := range paths {
		if !strings.Contains(p, "{org_id}") {
			continue
		}
		item := doc.Paths[p]
		ops := map[string]bool{
			http.MethodGet: item.Get != nil, http.MethodPost: item.Post != nil, http.MethodPut: item.Put != nil,
			http.MethodPatch: item.Patch != nil, http.MethodDelete: item.Delete != nil,
		}
		for method, ok := range ops {
			if !ok {
				continue
			}
			for _, scenario := range []struct {
				name, org, token string
			}{
				{"session-on-victim-org", victim.ID, mallory},
				{"token-on-victim-org", victim.ID, tok.Token},
				{"victim-ids-through-own-org", attacker.ID, mallory},
			} {
				url := pathParam.ReplaceAllStringFunc(p, func(m string) string {
					switch m {
					case "{org_id}":
						return scenario.org
					case "{period}":
						return "2026-08"
					case "{slug}":
						return "victim"
					default:
						return conn.ID // identifiant réel appartenant à la victime
					}
				})
				res := e.do(method, url, scenario.token, map[string]any{})
				if res.Code < 400 && !(scenario.name == "victim-ids-through-own-org" && !strings.Contains(p, "{id}")) {
					t.Errorf("%s %s [%s]: expected refusal, got %d: %s", method, p, scenario.name, res.Code, truncate(string(res.Body), 200))
				}
				if res.Code >= 500 {
					t.Errorf("%s %s [%s]: server error %d: %s", method, p, scenario.name, res.Code, truncate(string(res.Body), 200))
				}
				if strings.Contains(string(res.Body), victim.ID) || strings.Contains(string(res.Body), conn.ID) && res.Code < 400 {
					t.Errorf("%s %s [%s]: response leaks victim identifiers", method, p, scenario.name)
				}
				checked++
			}
		}
	}
	if checked < 150 {
		t.Fatalf("too few checks (%d): routes not registered?", checked)
	}
	t.Logf("%d appels d'isolation vérifiés", checked)
}
