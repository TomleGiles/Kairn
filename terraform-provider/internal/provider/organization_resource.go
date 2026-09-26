package provider

import (
	"context"
	"math/big"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// organizationResource gère les paramètres de l'organisation du jeton. Une
// organisation ne se crée ni ne se supprime avec un jeton d'API (session d'un
// propriétaire requise) : la ressource « adopte » l'organisation existante.
type organizationResource struct{ base }

// NewOrganizationResource construit kairn_organization.
func NewOrganizationResource() resource.Resource { return &organizationResource{} }

type organizationModel struct {
	ID       types.String `tfsdk:"id"`
	Name     types.String `tfsdk:"name"`
	Slug     types.String `tfsdk:"slug"`
	Plan     types.String `tfsdk:"plan"`
	Currency types.String `tfsdk:"currency"`
	Locale   types.String `tfsdk:"locale"`
	Timezone types.String `tfsdk:"timezone"`
	VATRate  types.String `tfsdk:"vat_rate"`
}

type orgAPI struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	Plan     string `json:"plan"`
	Currency string `json:"currency"`
	Locale   string `json:"locale"`
	Timezone string `json:"timezone"`
	VATRate  string `json:"vat_rate"`
}

type orgPatch struct {
	Name     *string `json:"name,omitempty"`
	Currency *string `json:"currency,omitempty"`
	Locale   *string `json:"locale,omitempty"`
	Timezone *string `json:"timezone,omitempty"`
	VATRate  *string `json:"vat_rate,omitempty"`
}

func (r *organizationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization"
}

func (r *organizationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		Description: "Paramètres de l'organisation gérée par le provider (celle du jeton). La détruire ne supprime pas l'organisation.",
		Attributes: map[string]schema.Attribute{
			"id":   schema.StringAttribute{Computed: true, PlanModifiers: keep, Description: "Identifiant de l'organisation."},
			"slug": schema.StringAttribute{Computed: true, PlanModifiers: keep},
			"plan": schema.StringAttribute{Computed: true, PlanModifiers: keep, Description: "Plan commercial (starter, team, enterprise, msp)."},
			"name": schema.StringAttribute{Optional: true, Computed: true, PlanModifiers: keep},
			"currency": schema.StringAttribute{Optional: true, Computed: true, PlanModifiers: keep,
				Description: "Devise de restitution des coûts.", Validators: []validator.String{oneOf("EUR", "USD", "GBP", "CHF")}},
			"locale": schema.StringAttribute{Optional: true, Computed: true, PlanModifiers: keep,
				Validators: []validator.String{oneOf("fr", "en")}},
			"timezone": schema.StringAttribute{Optional: true, Computed: true, PlanModifiers: keep, Description: "Fuseau IANA (ex. Europe/Paris)."},
			"vat_rate": schema.StringAttribute{Optional: true, Computed: true, PlanModifiers: keep,
				Description: "Taux de TVA en pourcentage, en chaîne décimale (ex. \"20\"). Vide : coûts hors taxes."},
		},
	}
}

func (r *organizationResource) apply(ctx context.Context, m *organizationModel) error {
	body := orgPatch{Name: strPtr(m.Name), Currency: strPtr(m.Currency), Locale: strPtr(m.Locale), Timezone: strPtr(m.Timezone), VATRate: strPtr(m.VATRate)}
	var out orgAPI
	if err := r.client.do(ctx, http.MethodPatch, r.client.orgPath(""), body, &out); err != nil {
		return err
	}
	fromOrgAPI(m, out)
	return nil
}

// fromOrgAPI recopie la réponse ; une valeur décimale égale à celle planifiée garde son écriture.
func fromOrgAPI(m *organizationModel, o orgAPI) {
	m.ID, m.Name, m.Slug, m.Plan = types.StringValue(o.ID), types.StringValue(o.Name), types.StringValue(o.Slug), types.StringValue(o.Plan)
	m.Currency, m.Locale, m.Timezone = types.StringValue(o.Currency), types.StringValue(o.Locale), types.StringValue(o.Timezone)
	if !(m.VATRate.IsNull() || m.VATRate.IsUnknown()) && decimalEqual(m.VATRate.ValueString(), o.VATRate) {
		return
	}
	m.VATRate = types.StringValue(o.VATRate)
}

// decimalEqual compare deux décimaux écrits différemment ("20" et "20.00").
func decimalEqual(a, b string) bool {
	x, ok1 := new(big.Rat).SetString(a)
	y, ok2 := new(big.Rat).SetString(b)
	if !ok1 || !ok2 {
		return a == b
	}
	return x.Cmp(y) == 0
}

func (r *organizationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m organizationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.apply(ctx, &m); err != nil {
		apiError(&resp.Diagnostics, "update organization", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *organizationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m organizationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var out orgAPI
	if err := r.client.do(ctx, http.MethodGet, r.client.orgPath(""), nil, &out); err != nil {
		if IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "read organization", err)
		return
	}
	fromOrgAPI(&m, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *organizationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m organizationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.apply(ctx, &m); err != nil {
		apiError(&resp.Diagnostics, "update organization", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *organizationResource) Delete(_ context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.AddWarning("Organization not deleted",
		"kairn_organization only manages settings: the organization itself can only be deleted by an owner from the Kairn interface.")
}

func (r *organizationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != r.client.OrgID {
		resp.Diagnostics.AddError("Wrong organization", "kairn_organization can only import the provider's organization ("+r.client.OrgID+").")
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
