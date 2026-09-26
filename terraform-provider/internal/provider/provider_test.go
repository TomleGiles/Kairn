package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

var ctx = context.Background()

func noDiags(t *testing.T, d diag.Diagnostics) {
	t.Helper()
	if d.HasError() {
		t.Fatalf("diagnostics: %v", d)
	}
}

// ------------------------------------------------------------------ harnais

type harness struct {
	t *testing.T
	r resource.Resource
	s schema.Schema
}

func newHarness(t *testing.T, r resource.Resource, c *Client) *harness {
	t.Helper()
	r.(resource.ResourceWithConfigure).Configure(ctx, resource.ConfigureRequest{ProviderData: c}, &resource.ConfigureResponse{})
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	noDiags(t, sr.Diagnostics)
	return &harness{t: t, r: r, s: sr.Schema}
}

func (h *harness) null() tftypes.Value { return tftypes.NewValue(h.s.Type().TerraformType(ctx), nil) }

func (h *harness) plan(model any) tfsdk.Plan {
	h.t.Helper()
	p := tfsdk.Plan{Schema: h.s, Raw: h.null()}
	noDiags(h.t, p.Set(ctx, model))
	return p
}

func (h *harness) create(model, out any) tfsdk.State {
	h.t.Helper()
	resp := resource.CreateResponse{State: tfsdk.State{Schema: h.s, Raw: h.null()}}
	h.r.Create(ctx, resource.CreateRequest{Plan: h.plan(model)}, &resp)
	noDiags(h.t, resp.Diagnostics)
	noDiags(h.t, resp.State.Get(ctx, out))
	return resp.State
}

func (h *harness) read(st tfsdk.State, out any) (tfsdk.State, bool) {
	h.t.Helper()
	resp := resource.ReadResponse{State: st}
	h.r.Read(ctx, resource.ReadRequest{State: st}, &resp)
	noDiags(h.t, resp.Diagnostics)
	if resp.State.Raw.IsNull() {
		return resp.State, false
	}
	noDiags(h.t, resp.State.Get(ctx, out))
	return resp.State, true
}

func (h *harness) update(prior tfsdk.State, model, out any) tfsdk.State {
	h.t.Helper()
	resp := resource.UpdateResponse{State: prior}
	h.r.Update(ctx, resource.UpdateRequest{Plan: h.plan(model), State: prior}, &resp)
	noDiags(h.t, resp.Diagnostics)
	noDiags(h.t, resp.State.Get(ctx, out))
	return resp.State
}

func (h *harness) delete(st tfsdk.State) diag.Diagnostics {
	resp := resource.DeleteResponse{State: st}
	h.r.Delete(ctx, resource.DeleteRequest{State: st}, &resp)
	return resp.Diagnostics
}

func strMap(kv ...string) types.Map {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	v, _ := types.MapValueFrom(ctx, types.StringType, m)
	return v
}

func testClient(url string) *Client {
	return &Client{BaseURL: url, Token: "kairn_test", OrgID: testOrg, UserAgent: "test"}
}

// ------------------------------------------------------------------ provider

func TestProviderSchemaIsValid(t *testing.T) {
	srv, err := providerserver.NewProtocol6WithError(New("test")())()
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range resp.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Fatalf("schema error: %s: %s", d.Summary, d.Detail)
		}
	}
	for _, name := range []string{"kairn_organization", "kairn_connector", "kairn_allocation_node", "kairn_allocation_rule", "kairn_budget"} {
		if _, ok := resp.ResourceSchemas[name]; !ok {
			t.Errorf("missing resource %s", name)
		}
	}
	if !resp.Provider.Block.Attributes[findAttr(resp.Provider.Block.Attributes, "token")].Sensitive {
		t.Error("token must be sensitive")
	}
}

func findAttr(attrs []*tfprotov6.SchemaAttribute, name string) int {
	for i, a := range attrs {
		if a.Name == name {
			return i
		}
	}
	return -1
}

func TestProviderConfigure(t *testing.T) {
	_, srv := newFakeAPI(t)
	t.Setenv("KAIRN_URL", "")
	t.Setenv("KAIRN_TOKEN", "")
	t.Setenv("KAIRN_ORG", "")
	p := &kairnProvider{version: "test", httpClient: srv.Client()}
	var sr provider.SchemaResponse
	p.Schema(ctx, provider.SchemaRequest{}, &sr)
	cfg := func(url, token string) tfsdk.Config {
		typ := sr.Schema.Type().TerraformType(ctx)
		val := func(s string) tftypes.Value {
			if s == "" {
				return tftypes.NewValue(tftypes.String, nil)
			}
			return tftypes.NewValue(tftypes.String, s)
		}
		return tfsdk.Config{Schema: sr.Schema, Raw: tftypes.NewValue(typ, map[string]tftypes.Value{
			"url": val(url), "token": val(token), "organization_id": val(""),
		})}
	}
	var resp provider.ConfigureResponse
	p.Configure(ctx, provider.ConfigureRequest{Config: cfg(srv.URL, "kairn_test")}, &resp)
	noDiags(t, resp.Diagnostics)
	c, ok := resp.ResourceData.(*Client)
	if !ok || c.OrgID != testOrg {
		t.Fatalf("organization must be resolved from the token: %+v", resp.ResourceData)
	}
	// Jeton manquant : erreur explicite, aucun appel.
	resp = provider.ConfigureResponse{}
	p.Configure(ctx, provider.ConfigureRequest{Config: cfg(srv.URL, "")}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("missing token must fail")
	}
	// Variables d'environnement en repli ; mauvais jeton : erreur de l'API.
	t.Setenv("KAIRN_TOKEN", "wrong")
	resp = provider.ConfigureResponse{}
	p.Configure(ctx, provider.ConfigureRequest{Config: cfg(srv.URL, "")}, &resp)
	if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "401") {
		t.Fatalf("bad token must surface the API error: %v", resp.Diagnostics)
	}
}
