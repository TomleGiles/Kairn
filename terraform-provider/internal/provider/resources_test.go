package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestConnectorLifecycle(t *testing.T) {
	f, srv := newFakeAPI(t)
	h := newHarness(t, NewConnectorResource(), testClient(srv.URL))

	plan := connectorModel{
		ID: types.StringUnknown(), Type: types.StringValue("openstack"), Name: types.StringValue("prod"),
		Enabled: types.BoolValue(true), IntervalSeconds: types.Int64Unknown(),
		Settings: strMap("auth_url", "https://keystone/v3", "region", "GRA11"),
		Secrets:  strMap("application_credential_secret", "s3cr3t"), BackfillDays: types.Int64Value(90),
		Status: types.StringUnknown(), WebhookURL: types.StringUnknown(),
	}
	var got connectorModel
	st := h.create(plan, &got)
	if got.ID.ValueString() == "" || got.IntervalSeconds.ValueInt64() != 3600 || got.Status.ValueString() != "pending" {
		t.Fatalf("computed values: %+v", got)
	}
	// Les paramètres ajoutés par le serveur n'entrent pas dans l'état (pas de faux écart).
	if len(got.Settings.Elements()) != 2 || got.WebhookURL.IsUnknown() {
		t.Fatalf("settings/webhook: %+v", got)
	}
	if f.secrets[got.ID.ValueString()]["application_credential_secret"] != "s3cr3t" {
		t.Fatal("secret must be sent at creation")
	}

	// Mise à jour : paramètre retiré, secret remplacé, intervalle modifié.
	plan = got
	plan.Settings = strMap("auth_url", "https://keystone/v3")
	plan.Secrets = strMap("application_credential_secret", "rotated")
	plan.IntervalSeconds = types.Int64Value(900)
	plan.Status = types.StringUnknown()
	st = h.update(st, plan, &got)
	c := f.connectors[got.ID.ValueString()]
	if _, still := c["settings"].(map[string]string)["region"]; still {
		t.Fatal("a setting removed from the configuration must be deleted (empty value in the merge patch)")
	}
	if f.secrets[got.ID.ValueString()]["application_credential_secret"] != "rotated" || got.IntervalSeconds.ValueInt64() != 900 {
		t.Fatalf("update not applied: %+v", got)
	}

	// Dérive : un secret supprimé hors Terraform disparaît de l'état (le plan le recréera).
	delete(f.secrets[got.ID.ValueString()], "application_credential_secret")
	st, ok := h.read(st, &got)
	if !ok || len(got.Secrets.Elements()) != 0 {
		t.Fatalf("secret drift must be detected: %+v", got.Secrets)
	}
	// Dérive : un paramètre modifié hors Terraform est relu.
	c["settings"].(map[string]string)["auth_url"] = "https://other/v3"
	st, _ = h.read(st, &got)
	if got.Settings.Elements()["auth_url"].(types.String).ValueString() != "https://other/v3" {
		t.Fatalf("settings drift: %+v", got.Settings)
	}

	noDiags(t, h.delete(st))
	if len(f.connectors) != 0 {
		t.Fatal("connector not deleted")
	}
	if _, ok := h.read(st, &got); ok {
		t.Fatal("a deleted connector must be removed from the state")
	}
	noDiags(t, h.delete(st)) // suppression idempotente
}

func TestConnectorWebhookURL(t *testing.T) {
	_, srv := newFakeAPI(t)
	h := newHarness(t, NewConnectorResource(), testClient(srv.URL))
	var got connectorModel
	h.create(connectorModel{
		ID: types.StringUnknown(), Type: types.StringValue("gitlab"), Name: types.StringValue("deploys"), Enabled: types.BoolValue(true),
		IntervalSeconds: types.Int64Unknown(), Settings: types.MapNull(types.StringType), Secrets: strMap("webhook_secret", "x"),
		BackfillDays: types.Int64Null(), Status: types.StringUnknown(), WebhookURL: types.StringUnknown(),
	}, &got)
	if got.WebhookURL.ValueString() == "" || !got.Settings.IsNull() {
		t.Fatalf("webhook URL must be exposed and unset settings stay null: %+v", got)
	}
}

func TestAllocationNodeAndRule(t *testing.T) {
	f, srv := newFakeAPI(t)
	c := testClient(srv.URL)
	nodes := newHarness(t, NewAllocationNodeResource(), c)
	var bu, team allocationNodeModel
	nodes.create(allocationNodeModel{ID: types.StringUnknown(), Kind: types.StringValue("business_unit"), Name: types.StringValue("Produit"),
		ParentID: types.StringNull(), Path: types.StringUnknown()}, &bu)
	teamState := nodes.create(allocationNodeModel{ID: types.StringUnknown(), Kind: types.StringValue("team"), Name: types.StringValue("Data"),
		ParentID: bu.ID, Path: types.StringUnknown()}, &team)
	if team.Path.ValueString() != "Produit/Data" || team.ParentID.ValueString() != bu.ID.ValueString() {
		t.Fatalf("node: %+v", team)
	}
	plan := team
	plan.Name, plan.Path = types.StringValue("Data Platform"), types.StringUnknown()
	nodes.update(teamState, plan, &team)
	if team.Path.ValueString() != "Produit/Data Platform" {
		t.Fatalf("renamed node: %+v", team)
	}

	rules := newHarness(t, NewAllocationRuleResource(), c)
	in := types.ListValueMust(types.StringType, []attr.Value{types.StringValue("data"), types.StringValue("analytics")})
	var rule allocationRuleModel
	ruleState := rules.create(allocationRuleModel{ID: types.StringUnknown(), NodeID: team.ID, Name: types.StringValue("équipe data"),
		Priority: types.Int64Value(10), Enabled: types.BoolValue(true), Conditions: []conditionModel{
			{Field: types.StringValue("label.team"), Op: types.StringValue("in"), Value: types.StringNull(), Values: in},
			{Field: types.StringValue("provider"), Op: types.StringValue("eq"), Value: types.StringValue("openstack"), Values: types.ListNull(types.StringType)},
		}}, &rule)
	if len(rule.Conditions) != 2 || len(rule.Conditions[0].Values.Elements()) != 2 || !rule.Conditions[0].Value.IsNull() ||
		rule.Conditions[1].Value.ValueString() != "openstack" || !rule.Conditions[1].Values.IsNull() {
		t.Fatalf("rule round-trip must preserve the configured shape: %+v", rule.Conditions)
	}
	// Dérive : priorité modifiée dans l'interface.
	f.rules[rule.ID.ValueString()]["priority"] = 42
	_, _ = rules.read(ruleState, &rule)
	if rule.Priority.ValueInt64() != 42 {
		t.Fatalf("priority drift: %d", rule.Priority.ValueInt64())
	}
	noDiags(t, rules.delete(ruleState))
	noDiags(t, nodes.delete(teamState))
	if len(f.rules) != 0 || len(f.nodes) != 1 {
		t.Fatalf("delete: rules=%d nodes=%d", len(f.rules), len(f.nodes))
	}
}

func TestBudgetLifecycle(t *testing.T) {
	f, srv := newFakeAPI(t)
	h := newHarness(t, NewBudgetResource(), testClient(srv.URL))
	var b budgetModel
	st := h.create(budgetModel{ID: types.StringUnknown(), Name: types.StringValue("Cloud 2026"), Period: types.StringValue("monthly"),
		Amount: types.StringValue("12000.00"), Currency: types.StringUnknown(), NodeID: types.StringNull(), Thresholds: types.ListUnknown(types.Int64Type),
		ForecastAlert: types.BoolValue(true), ChannelIDs: types.ListNull(types.StringType)}, &b)
	if b.Currency.ValueString() != "EUR" || len(b.Thresholds.Elements()) != 3 || b.Amount.ValueString() != "12000.00" {
		t.Fatalf("defaults: %+v", b)
	}
	// L'API peut renvoyer le montant normalisé ("12000") : l'écriture configurée est conservée.
	f.budgets[b.ID.ValueString()]["amount"] = "12000"
	st, _ = h.read(st, &b)
	if b.Amount.ValueString() != "12000.00" {
		t.Fatalf("equal decimals must not create drift: %s", b.Amount.ValueString())
	}
	plan := b
	plan.Amount = types.StringValue("15000")
	plan.Thresholds = types.ListValueMust(types.Int64Type, []attr.Value{types.Int64Value(90), types.Int64Value(100)})
	st = h.update(st, plan, &b)
	if b.Amount.ValueString() != "15000" || len(b.Thresholds.Elements()) != 2 {
		t.Fatalf("update: %+v", b)
	}
	noDiags(t, h.delete(st))
	if len(f.budgets) != 0 {
		t.Fatal("budget not deleted")
	}
}

func TestOrganizationSettings(t *testing.T) {
	f, srv := newFakeAPI(t)
	h := newHarness(t, NewOrganizationResource(), testClient(srv.URL))
	var o organizationModel
	st := h.create(organizationModel{ID: types.StringUnknown(), Name: types.StringValue("Acme SAS"), Slug: types.StringUnknown(), Plan: types.StringUnknown(),
		Currency: types.StringUnknown(), Locale: types.StringValue("en"), Timezone: types.StringUnknown(), VATRate: types.StringValue("20.0")}, &o)
	if o.ID.ValueString() != testOrg || f.org["name"] != "Acme SAS" || f.org["locale"] != "en" || o.Currency.ValueString() != "EUR" {
		t.Fatalf("organization: %+v / %+v", o, f.org)
	}
	f.org["vat_rate"] = "20"
	_, _ = h.read(st, &o)
	if o.VATRate.ValueString() != "20.0" {
		t.Fatalf("equal VAT rates must not drift: %s", o.VATRate.ValueString())
	}
	d := h.delete(st)
	if d.HasError() || d.WarningsCount() != 1 {
		t.Fatalf("destroying kairn_organization only warns: %v", d)
	}
	if _, ok := f.org["id"]; !ok {
		t.Fatal("the organization must never be deleted")
	}
}

func TestAPIErrors(t *testing.T) {
	_, srv := newFakeAPI(t)
	c := testClient(srv.URL)
	err := c.do(ctx, "GET", "/api/v1/orgs/other/connectors", nil, nil)
	if !IsNotFound(err) {
		t.Fatalf("404 problem+json must map to IsNotFound: %v", err)
	}
	c.Token = "bad"
	if err := c.do(ctx, "GET", c.orgPath(""), nil, nil); err == nil || IsNotFound(err) {
		t.Fatalf("401 must be an error, not a missing resource: %v", err)
	}
	if !decimalEqual("1.50", "1.5") || decimalEqual("1.5", "1.51") || decimalEqual("x", "1") {
		t.Fatal("decimalEqual")
	}
	if got := diffMap(map[string]string{"a": "1", "b": "2"}, map[string]string{"a": "1", "c": "3"}); len(got) != 2 || got["b"] != "" || got["c"] != "3" {
		t.Fatalf("diffMap: %v", got)
	}
}
