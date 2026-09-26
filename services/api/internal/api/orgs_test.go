package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/kairn-io/kairn/pkg/model"
)

func TestOpenAPIServed(t *testing.T) {
	e := newEnv(t)
	r := e.do(http.MethodGet, "/api/v1/openapi.json", "", nil)
	expect(t, r, http.StatusOK)
	var doc map[string]any
	r.JSON(t, &doc)
	if doc["openapi"] != "3.1.0" {
		t.Fatalf("openapi version: %v", doc["openapi"])
	}
}

func TestOrgLifecycleAndMembers(t *testing.T) {
	e := newEnv(t)
	alice := e.login("alice@example.com")
	expect(t, e.do(http.MethodGet, "/api/v1/me", "", nil), http.StatusUnauthorized)
	expect(t, e.do(http.MethodGet, "/api/v1/me", "garbage", nil), http.StatusUnauthorized)
	o := e.org(alice, "Acme Cloud")
	if o.Slug != "acme-cloud" || o.Plan != model.PlanStarter || o.TrialEndsAt == nil {
		t.Fatalf("org defaults: %+v", o)
	}
	var me meBody
	e.do(http.MethodGet, "/api/v1/me", alice, nil).JSON(t, &me)
	if len(me.Memberships) != 1 || me.Memberships[0].Role != model.RoleOwner {
		t.Fatalf("me: %+v", me)
	}
	// Invitation d'un membre finance.
	r := e.do(http.MethodPost, "/api/v1/orgs/"+o.ID+"/members", alice, map[string]any{"email": "bob@example.com", "role": "finance"})
	expect(t, r, http.StatusCreated)
	bob := e.login("bob@example.com")
	expect(t, e.do(http.MethodGet, "/api/v1/orgs/"+o.ID, bob, nil), http.StatusOK)
	// Bob (finance) ne peut pas gérer les membres.
	expect(t, e.do(http.MethodPost, "/api/v1/orgs/"+o.ID+"/members", bob, map[string]any{"email": "eve@example.com", "role": "owner"}), http.StatusForbidden)
	// Dernier propriétaire protégé.
	var ms []model.Membership
	e.do(http.MethodGet, "/api/v1/orgs/"+o.ID+"/members", alice, nil).JSON(t, &ms)
	var aliceID string
	for _, m := range ms {
		if m.Email == "alice@example.com" {
			aliceID = m.UserID
		}
	}
	expect(t, e.do(http.MethodDelete, "/api/v1/orgs/"+o.ID+"/members/"+aliceID, alice, nil), http.StatusConflict)
	// Isolation : un étranger ne voit pas l'organisation (404, pas 403).
	eve := e.login("eve@example.com")
	expect(t, e.do(http.MethodGet, "/api/v1/orgs/"+o.ID, eve, nil), http.StatusNotFound)
	// Audit.
	var audit Page[model.AuditEvent]
	e.do(http.MethodGet, "/api/v1/orgs/"+o.ID+"/audit", alice, nil).JSON(t, &audit)
	if len(audit.Items) < 2 {
		t.Fatalf("audit events: %+v", audit.Items)
	}
}

func TestAPITokens(t *testing.T) {
	e := newEnv(t)
	alice := e.login("alice@example.com")
	o := e.org(alice, "Acme")
	r := e.do(http.MethodPost, "/api/v1/orgs/"+o.ID+"/tokens", alice, map[string]any{"name": "ci", "role": "admin", "scopes": []string{"costs:read", "org:read"}})
	expect(t, r, http.StatusCreated)
	var tok createdToken
	r.JSON(t, &tok)
	expect(t, e.do(http.MethodGet, "/api/v1/orgs/"+o.ID, tok.Token, nil), http.StatusOK)
	// Le rôle admin autorise l'audit, mais pas les scopes du jeton → 403 ; autre organisation → 404.
	expect(t, e.do(http.MethodGet, "/api/v1/orgs/"+o.ID+"/audit", tok.Token, nil), http.StatusForbidden)
	other := e.org(alice, "Other")
	expect(t, e.do(http.MethodGet, "/api/v1/orgs/"+other.ID, tok.Token, nil), http.StatusNotFound)
	// Révocation.
	expect(t, e.do(http.MethodDelete, "/api/v1/orgs/"+o.ID+"/tokens/"+tok.ID, alice, nil), http.StatusNoContent)
	expect(t, e.do(http.MethodGet, "/api/v1/orgs/"+o.ID, tok.Token, nil), http.StatusUnauthorized)
}

func TestStripeWebhookSignature(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	payload := []byte(`{"type":"x"}`)
	ts := strconv.FormatInt(now.Unix(), 10)
	mac := hmac.New(sha256.New, []byte("whsec"))
	mac.Write([]byte(ts + "."))
	mac.Write(payload)
	header := "t=" + ts + ",v1=" + hex.EncodeToString(mac.Sum(nil))
	if err := verifyStripeSignature(payload, header, "whsec", now); err != nil {
		t.Fatal(err)
	}
	if err := verifyStripeSignature(payload, header, "other", now); err == nil {
		t.Fatal("wrong secret")
	}
	if err := verifyStripeSignature(payload, header, "whsec", now.Add(10*time.Minute)); err == nil {
		t.Fatal("replay outside tolerance")
	}
}

func TestPatchOrgSettingsMerges(t *testing.T) {
	e := newEnv(t)
	alice := e.login("alice@example.com")
	o := e.org(alice, "Merge Cloud")
	r := e.do(http.MethodPatch, "/api/v1/orgs/"+o.ID, alice, map[string]any{"settings": map[string]any{
		"report_recipients": []string{"cfo@example.com"}, "rightsizing_percentile": 90, "allow_external_llm": true}})
	expect(t, r, http.StatusOK)
	// Une mise à jour partielle ne touche que les clés envoyées.
	r = e.do(http.MethodPatch, "/api/v1/orgs/"+o.ID, alice, map[string]any{"settings": map[string]any{"allow_external_llm": false}})
	expect(t, r, http.StatusOK)
	var got model.Organization
	r.JSON(t, &got)
	st := got.Settings
	if st.AllowExternalLLM || st.RightsizingPercentile != 90 || len(st.ReportRecipients) != 1 || st.ReportRecipients[0] != "cfo@example.com" {
		t.Fatalf("settings not merged: %+v", st)
	}
	// null supprime une clé ; une valeur invalide est refusée.
	r = e.do(http.MethodPatch, "/api/v1/orgs/"+o.ID, alice, map[string]any{"settings": map[string]any{"report_recipients": nil}})
	expect(t, r, http.StatusOK)
	var cleared model.Organization
	r.JSON(t, &cleared)
	if len(cleared.Settings.ReportRecipients) != 0 || cleared.Settings.RightsizingPercentile != 90 {
		t.Fatalf("null should clear only that key: %+v", cleared.Settings)
	}
	expect(t, e.do(http.MethodPatch, "/api/v1/orgs/"+o.ID, alice, map[string]any{"settings": map[string]any{"report_recipients": []string{"nope"}}}),
		http.StatusUnprocessableEntity)
}

func TestDeleteOrganization(t *testing.T) {
	e := newEnv(t)
	alice := e.login("alice@example.com")
	o := e.org(alice, "Erase Me")
	expect(t, e.do(http.MethodPost, "/api/v1/orgs/"+o.ID+"/members", alice, map[string]any{"email": "bob@example.com", "role": "admin"}), http.StatusCreated)
	bob := e.login("bob@example.com")
	path := "/api/v1/orgs/" + o.ID
	// Un administrateur n'est pas propriétaire ; la confirmation doit reprendre le slug.
	expect(t, e.do(http.MethodDelete, path, bob, map[string]any{"confirm": o.Slug}), http.StatusForbidden)
	expect(t, e.do(http.MethodDelete, path, alice, map[string]any{"confirm": "wrong"}), http.StatusUnprocessableEntity)
	eve := e.login("eve@example.com")
	expect(t, e.do(http.MethodDelete, path, eve, map[string]any{"confirm": o.Slug}), http.StatusNotFound)
	expect(t, e.do(http.MethodDelete, path, alice, map[string]any{"confirm": o.Slug}), http.StatusNoContent)
	expect(t, e.do(http.MethodGet, path, alice, nil), http.StatusNotFound)
	var me meBody
	e.do(http.MethodGet, "/api/v1/me", bob, nil).JSON(t, &me)
	if len(me.Memberships) != 0 {
		t.Fatalf("memberships survived deletion: %+v", me.Memberships)
	}
}
