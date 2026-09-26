// Package provider implémente le provider Terraform / OpenTofu de Kairn.
package provider

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ provider.Provider = (*kairnProvider)(nil)

type kairnProvider struct {
	version string
	// httpClient remplace le client HTTP (tests).
	httpClient *http.Client
}

type providerModel struct {
	URL            types.String `tfsdk:"url"`
	Token          types.String `tfsdk:"token"`
	OrganizationID types.String `tfsdk:"organization_id"`
}

// New renvoie le constructeur du provider.
func New(version string) func() provider.Provider {
	return func() provider.Provider { return &kairnProvider{version: version} }
}

func (p *kairnProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "kairn"
	resp.Version = p.version
}

func (p *kairnProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Configuration « as code » d'une organisation Kairn (connecteurs, allocation, budgets) via l'API publique.",
		Attributes: map[string]schema.Attribute{
			"url": schema.StringAttribute{
				Optional:    true,
				Description: "URL de Kairn (ex. https://kairn.example.com). Variable d'environnement : KAIRN_URL.",
			},
			"token": schema.StringAttribute{
				Optional: true, Sensitive: true,
				Description: "Jeton d'API Kairn (kairn_…), rôle Admin recommandé. Variable d'environnement : KAIRN_TOKEN.",
			},
			"organization_id": schema.StringAttribute{
				Optional:    true,
				Description: "Organisation gérée. Par défaut : l'organisation du jeton. Variable d'environnement : KAIRN_ORG.",
			},
		},
	}
}

func valueOrEnv(v types.String, env string) string {
	if !v.IsNull() && !v.IsUnknown() && v.ValueString() != "" {
		return v.ValueString()
	}
	return os.Getenv(env)
}

func (p *kairnProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	c := &Client{
		BaseURL:   valueOrEnv(cfg.URL, "KAIRN_URL"),
		Token:     valueOrEnv(cfg.Token, "KAIRN_TOKEN"),
		OrgID:     valueOrEnv(cfg.OrganizationID, "KAIRN_ORG"),
		UserAgent: "terraform-provider-kairn/" + p.version,
		HTTP:      p.httpClient,
	}
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: time.Minute}
	}
	if c.BaseURL == "" {
		resp.Diagnostics.AddAttributeError(path.Root("url"), "Missing Kairn URL", "Set url or KAIRN_URL.")
	}
	if c.Token == "" {
		resp.Diagnostics.AddAttributeError(path.Root("token"), "Missing Kairn API token", "Set token or KAIRN_TOKEN (never commit it).")
	}
	if resp.Diagnostics.HasError() {
		return
	}
	if err := c.resolveOrg(ctx); err != nil {
		resp.Diagnostics.AddError("Cannot determine the Kairn organization", err.Error())
		return
	}
	resp.ResourceData = c
	resp.DataSourceData = c
}

func (p *kairnProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewOrganizationResource,
		NewConnectorResource,
		NewAllocationNodeResource,
		NewAllocationRuleResource,
		NewBudgetResource,
	}
}

func (p *kairnProvider) DataSources(context.Context) []func() datasource.DataSource {
	return nil
}
