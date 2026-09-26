package provider

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type budgetResource struct{ base }

// NewBudgetResource construit kairn_budget.
func NewBudgetResource() resource.Resource { return &budgetResource{} }

type budgetModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	Period        types.String `tfsdk:"period"`
	Amount        types.String `tfsdk:"amount"`
	Currency      types.String `tfsdk:"currency"`
	NodeID        types.String `tfsdk:"node_id"`
	Thresholds    types.List   `tfsdk:"thresholds"`
	ForecastAlert types.Bool   `tfsdk:"forecast_alert"`
	ChannelIDs    types.List   `tfsdk:"channel_ids"`
}

type budgetAPI struct {
	ID            string   `json:"id,omitempty"`
	Name          string   `json:"name"`
	Period        string   `json:"period"`
	Amount        string   `json:"amount"`
	Currency      string   `json:"currency,omitempty"`
	NodeID        *string  `json:"node_id,omitempty"`
	Thresholds    []int64  `json:"thresholds,omitempty"`
	ForecastAlert *bool    `json:"forecast_alert,omitempty"`
	ChannelIDs    []string `json:"channel_ids,omitempty"`
}

func (r *budgetResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_budget"
}

func (r *budgetResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Budget sur l'organisation ou un nœud d'allocation, avec alertes sur le réel et la prévision de fin de période.",
		Attributes: map[string]schema.Attribute{
			"id":     schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name":   schema.StringAttribute{Required: true},
			"period": schema.StringAttribute{Required: true, Validators: []validator.String{oneOf("monthly", "quarterly", "yearly")}},
			"amount": schema.StringAttribute{Required: true,
				Description: "Montant en chaîne décimale (ex. \"12000.00\") : jamais de flottant pour l'argent."},
			"currency": schema.StringAttribute{Optional: true, Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				Validators: []validator.String{oneOf("EUR", "USD", "GBP", "CHF")}, Description: "Défaut : devise de l'organisation."},
			"node_id": schema.StringAttribute{Optional: true, Description: "Nœud d'allocation ; vide = toute l'organisation."},
			"thresholds": schema.ListAttribute{ElementType: types.Int64Type, Optional: true, Computed: true,
				PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
				Description:   "Seuils d'alerte en % du budget (défaut Kairn : 50, 80, 100)."},
			"forecast_alert": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				Description: "Alerter si la prévision de fin de période dépasse le budget."},
			"channel_ids": schema.ListAttribute{ElementType: types.StringType, Optional: true, Description: "Canaux de notification."},
		},
	}
}

func toBudgetAPI(ctx context.Context, m budgetModel, diags *diag.Diagnostics) budgetAPI {
	b := budgetAPI{Name: m.Name.ValueString(), Period: m.Period.ValueString(), Amount: m.Amount.ValueString(),
		NodeID: strPtr(m.NodeID)}
	if v := strPtr(m.Currency); v != nil {
		b.Currency = *v
	}
	fa := m.ForecastAlert.ValueBool()
	b.ForecastAlert = &fa
	if !m.Thresholds.IsNull() && !m.Thresholds.IsUnknown() {
		diags.Append(m.Thresholds.ElementsAs(ctx, &b.Thresholds, false)...)
	}
	b.ChannelIDs = stringList(ctx, m.ChannelIDs, diags)
	return b
}

func fromBudgetAPI(m *budgetModel, o budgetAPI) {
	m.ID, m.Name, m.Period, m.Currency = types.StringValue(o.ID), types.StringValue(o.Name), types.StringValue(o.Period), types.StringValue(o.Currency)
	if m.Amount.IsNull() || m.Amount.IsUnknown() || !decimalEqual(m.Amount.ValueString(), o.Amount) {
		m.Amount = types.StringValue(o.Amount)
	}
	m.NodeID = optString(o.NodeID)
	th := make([]attr.Value, 0, len(o.Thresholds))
	for _, t := range o.Thresholds {
		th = append(th, types.Int64Value(t))
	}
	m.Thresholds = types.ListValueMust(types.Int64Type, th)
	if o.ForecastAlert != nil {
		m.ForecastAlert = types.BoolValue(*o.ForecastAlert)
	}
	if len(o.ChannelIDs) > 0 {
		ch := make([]attr.Value, 0, len(o.ChannelIDs))
		for _, c := range o.ChannelIDs {
			ch = append(ch, types.StringValue(c))
		}
		m.ChannelIDs = types.ListValueMust(types.StringType, ch)
	} else if !m.ChannelIDs.IsNull() {
		m.ChannelIDs = types.ListValueMust(types.StringType, []attr.Value{})
	}
}

func (r *budgetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m budgetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := toBudgetAPI(ctx, m, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	var out budgetAPI
	if err := r.client.do(ctx, http.MethodPost, r.client.orgPath("/budgets"), body, &out); err != nil {
		apiError(&resp.Diagnostics, "create budget", err)
		return
	}
	fromBudgetAPI(&m, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *budgetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m budgetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var out budgetAPI
	if err := r.client.do(ctx, http.MethodGet, r.client.orgPath("/budgets/"+m.ID.ValueString()), nil, &out); err != nil {
		if IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "read budget", err)
		return
	}
	fromBudgetAPI(&m, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *budgetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m budgetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := toBudgetAPI(ctx, m, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	var out budgetAPI
	if err := r.client.do(ctx, http.MethodPut, r.client.orgPath("/budgets/"+m.ID.ValueString()), body, &out); err != nil {
		apiError(&resp.Diagnostics, "update budget", err)
		return
	}
	fromBudgetAPI(&m, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *budgetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m budgetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.do(ctx, http.MethodDelete, r.client.orgPath("/budgets/"+m.ID.ValueString()), nil, nil); err != nil && !IsNotFound(err) {
		apiError(&resp.Diagnostics, "delete budget", err)
	}
}

func (r *budgetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
