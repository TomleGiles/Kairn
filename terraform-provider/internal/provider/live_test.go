package provider

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// TestLiveAPI rejoue les cycles de vie des ressources contre une vraie API
// Kairn (instance de démo ou de pré-production) :
//
//	KAIRN_TF_LIVE_URL=http://localhost:8080 KAIRN_TF_LIVE_TOKEN=kairn_… go test ./... -run TestLiveAPI
func TestLiveAPI(t *testing.T) {
	url, token := os.Getenv("KAIRN_TF_LIVE_URL"), os.Getenv("KAIRN_TF_LIVE_TOKEN")
	if url == "" || token == "" {
		t.Skip("KAIRN_TF_LIVE_URL / KAIRN_TF_LIVE_TOKEN not set")
	}
	c := &Client{BaseURL: url, Token: token, UserAgent: "terraform-provider-kairn/live-test"}
	if err := c.resolveOrg(ctx); err != nil {
		t.Fatal(err)
	}

	nodes := newHarness(t, NewAllocationNodeResource(), c)
	var node allocationNodeModel
	nodeState := nodes.create(allocationNodeModel{ID: types.StringUnknown(), Kind: types.StringValue("team"), Name: types.StringValue("TF live"),
		ParentID: types.StringNull(), Path: types.StringUnknown()}, &node)
	defer func() { noDiags(t, nodes.delete(nodeState)) }()
	if node.Path.ValueString() == "" {
		t.Fatalf("node: %+v", node)
	}

	rules := newHarness(t, NewAllocationRuleResource(), c)
	var rule allocationRuleModel
	ruleState := rules.create(allocationRuleModel{ID: types.StringUnknown(), NodeID: node.ID, Name: types.StringValue("TF live"),
		Priority: types.Int64Value(9000), Enabled: types.BoolValue(false), Conditions: []conditionModel{
			{Field: types.StringValue("label.team"), Op: types.StringValue("in"), Value: types.StringNull(),
				Values: types.ListValueMust(types.StringType, []attr.Value{types.StringValue("tf-live")})},
		}}, &rule)
	if _, ok := rules.read(ruleState, &rule); !ok || rule.Priority.ValueInt64() != 9000 || rule.Enabled.ValueBool() {
		t.Fatalf("rule: %+v", rule)
	}
	noDiags(t, rules.delete(ruleState))

	budgets := newHarness(t, NewBudgetResource(), c)
	var b budgetModel
	bState := budgets.create(budgetModel{ID: types.StringUnknown(), Name: types.StringValue("TF live"), Period: types.StringValue("quarterly"),
		Amount: types.StringValue("1234.50"), Currency: types.StringUnknown(), NodeID: node.ID, Thresholds: types.ListUnknown(types.Int64Type),
		ForecastAlert: types.BoolValue(false), ChannelIDs: types.ListNull(types.StringType)}, &b)
	if _, ok := budgets.read(bState, &b); !ok || b.Amount.ValueString() != "1234.50" || b.Currency.ValueString() == "" || b.ForecastAlert.ValueBool() {
		t.Fatalf("budget: %+v", b)
	}
	noDiags(t, budgets.delete(bState))

	conns := newHarness(t, NewConnectorResource(), c)
	var conn connectorModel
	connState := conns.create(connectorModel{ID: types.StringUnknown(), Type: types.StringValue("demo-kubernetes"), Name: types.StringValue("TF live"),
		Enabled: types.BoolValue(false), IntervalSeconds: types.Int64Value(3600), Settings: strMap("seed", "tf-live"), Secrets: types.MapNull(types.StringType),
		BackfillDays: types.Int64Value(1), Status: types.StringUnknown(), WebhookURL: types.StringUnknown()}, &conn)
	plan := conn
	plan.Name, plan.Status = types.StringValue("TF live renamed"), types.StringUnknown()
	connState = conns.update(connState, plan, &conn)
	if _, ok := conns.read(connState, &conn); !ok || conn.Name.ValueString() != "TF live renamed" || conn.Settings.Elements()["seed"] == nil {
		t.Fatalf("connector: %+v", conn)
	}
	noDiags(t, conns.delete(connState))

	orgs := newHarness(t, NewOrganizationResource(), c)
	var o organizationModel
	orgs.create(organizationModel{ID: types.StringUnknown(), Name: types.StringUnknown(), Slug: types.StringUnknown(), Plan: types.StringUnknown(),
		Currency: types.StringUnknown(), Locale: types.StringUnknown(), Timezone: types.StringUnknown(), VATRate: types.StringUnknown()}, &o)
	if o.ID.ValueString() != c.OrgID || o.Currency.ValueString() == "" {
		t.Fatalf("organization: %+v", o)
	}
}
