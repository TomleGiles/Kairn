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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ------------------------------------------------------------------ nœuds

type allocationNodeResource struct{ base }

// NewAllocationNodeResource construit kairn_allocation_node.
func NewAllocationNodeResource() resource.Resource { return &allocationNodeResource{} }

type allocationNodeModel struct {
	ID       types.String `tfsdk:"id"`
	Kind     types.String `tfsdk:"kind"`
	Name     types.String `tfsdk:"name"`
	ParentID types.String `tfsdk:"parent_id"`
	Path     types.String `tfsdk:"path"`
}

type nodeAPI struct {
	ID       string  `json:"id"`
	Kind     string  `json:"kind"`
	Name     string  `json:"name"`
	ParentID *string `json:"parent_id,omitempty"`
	Path     string  `json:"path"`
}

// nodeBody est le corps accepté par l'API (champs inconnus refusés).
type nodeBody struct {
	Kind     string  `json:"kind"`
	Name     string  `json:"name"`
	ParentID *string `json:"parent_id,omitempty"`
}

func (r *allocationNodeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_allocation_node"
}

func (r *allocationNodeResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Nœud de la hiérarchie d'allocation (organisation → business unit → équipe → service → environnement).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"kind": schema.StringAttribute{Required: true,
				Validators: []validator.String{oneOf("organization", "business_unit", "team", "service", "environment")}},
			"name":      schema.StringAttribute{Required: true},
			"parent_id": schema.StringAttribute{Optional: true, Description: "Nœud parent (vide = racine)."},
			"path":      schema.StringAttribute{Computed: true, Description: "Chemin complet (ex. Produit/Équipe Data)."},
		},
	}
}

func (r *allocationNodeResource) body(m allocationNodeModel) nodeBody {
	return nodeBody{Kind: m.Kind.ValueString(), Name: m.Name.ValueString(), ParentID: strPtr(m.ParentID)}
}

func fromNodeAPI(m *allocationNodeModel, n nodeAPI) {
	m.ID, m.Kind, m.Name, m.Path = types.StringValue(n.ID), types.StringValue(n.Kind), types.StringValue(n.Name), types.StringValue(n.Path)
	m.ParentID = optString(n.ParentID)
}

func (r *allocationNodeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m allocationNodeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var out nodeAPI
	if err := r.client.do(ctx, http.MethodPost, r.client.orgPath("/allocation/nodes"), r.body(m), &out); err != nil {
		apiError(&resp.Diagnostics, "create allocation node", err)
		return
	}
	fromNodeAPI(&m, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *allocationNodeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m allocationNodeModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var out nodeAPI
	if err := r.client.do(ctx, http.MethodGet, r.client.orgPath("/allocation/nodes/"+m.ID.ValueString()), nil, &out); err != nil {
		if IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "read allocation node", err)
		return
	}
	fromNodeAPI(&m, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *allocationNodeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m allocationNodeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var out nodeAPI
	if err := r.client.do(ctx, http.MethodPut, r.client.orgPath("/allocation/nodes/"+m.ID.ValueString()), r.body(m), &out); err != nil {
		apiError(&resp.Diagnostics, "update allocation node", err)
		return
	}
	fromNodeAPI(&m, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *allocationNodeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m allocationNodeModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.do(ctx, http.MethodDelete, r.client.orgPath("/allocation/nodes/"+m.ID.ValueString()), nil, nil); err != nil && !IsNotFound(err) {
		apiError(&resp.Diagnostics, "delete allocation node", err)
	}
}

func (r *allocationNodeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ------------------------------------------------------------------ règles

type allocationRuleResource struct{ base }

// NewAllocationRuleResource construit kairn_allocation_rule.
func NewAllocationRuleResource() resource.Resource { return &allocationRuleResource{} }

type allocationRuleModel struct {
	ID         types.String     `tfsdk:"id"`
	NodeID     types.String     `tfsdk:"node_id"`
	Name       types.String     `tfsdk:"name"`
	Priority   types.Int64      `tfsdk:"priority"`
	Enabled    types.Bool       `tfsdk:"enabled"`
	Conditions []conditionModel `tfsdk:"conditions"`
}

type conditionModel struct {
	Field  types.String `tfsdk:"field"`
	Op     types.String `tfsdk:"op"`
	Value  types.String `tfsdk:"value"`
	Values types.List   `tfsdk:"values"`
}

type conditionAPI struct {
	Field  string   `json:"field"`
	Op     string   `json:"op"`
	Value  string   `json:"value,omitempty"`
	Values []string `json:"values,omitempty"`
}

type ruleAPI struct {
	ID         string         `json:"id,omitempty"`
	NodeID     string         `json:"node_id"`
	Name       string         `json:"name"`
	Priority   int64          `json:"priority"`
	Enabled    bool           `json:"enabled"`
	Conditions []conditionAPI `json:"conditions"`
}

func (r *allocationRuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_allocation_rule"
}

func (r *allocationRuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Règle d'allocation : les ressources qui satisfont toutes les conditions sont attribuées au nœud. Évaluation par priorité croissante, la première qui correspond gagne.",
		Attributes: map[string]schema.Attribute{
			"id":       schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"node_id":  schema.StringAttribute{Required: true},
			"name":     schema.StringAttribute{Required: true},
			"priority": schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(100)},
			"enabled":  schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(true)},
			"conditions": schema.ListNestedAttribute{
				Required:    true,
				Description: "Conditions (toutes requises).",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"field": schema.StringAttribute{Required: true,
						Description: "provider, type, name, region, connector_id, external_id, resource_id, label.<clé> ou attr.<clé>."},
					"op": schema.StringAttribute{Required: true,
						Validators: []validator.String{oneOf("eq", "neq", "in", "regex", "exists", "prefix")}},
					"value":  schema.StringAttribute{Optional: true},
					"values": schema.ListAttribute{ElementType: types.StringType, Optional: true, Description: "Valeurs de l'opérateur in."},
				}},
			},
		},
	}
}

func (r *allocationRuleResource) body(ctx context.Context, m allocationRuleModel, diags *diag.Diagnostics) ruleAPI {
	b := ruleAPI{NodeID: m.NodeID.ValueString(), Name: m.Name.ValueString(), Priority: m.Priority.ValueInt64(), Enabled: m.Enabled.ValueBool()}
	for _, c := range m.Conditions {
		b.Conditions = append(b.Conditions, conditionAPI{Field: c.Field.ValueString(), Op: c.Op.ValueString(), Value: c.Value.ValueString(),
			Values: stringList(ctx, c.Values, diags)})
	}
	return b
}

func fromRuleAPI(m *allocationRuleModel, o ruleAPI) {
	m.ID, m.NodeID, m.Name = types.StringValue(o.ID), types.StringValue(o.NodeID), types.StringValue(o.Name)
	m.Priority, m.Enabled = types.Int64Value(o.Priority), types.BoolValue(o.Enabled)
	conds := make([]conditionModel, 0, len(o.Conditions))
	for i, c := range o.Conditions {
		cm := conditionModel{Field: types.StringValue(c.Field), Op: types.StringValue(c.Op), Value: types.StringNull(), Values: types.ListNull(types.StringType)}
		if c.Value != "" {
			cm.Value = types.StringValue(c.Value)
		}
		// Conserver la forme configurée (null vs liste vide) quand le contenu est identique.
		if i < len(m.Conditions) && !m.Conditions[i].Value.IsNull() && m.Conditions[i].Value.ValueString() == c.Value {
			cm.Value = m.Conditions[i].Value
		}
		if len(c.Values) > 0 {
			vals := make([]attr.Value, 0, len(c.Values))
			for _, v := range c.Values {
				vals = append(vals, types.StringValue(v))
			}
			cm.Values = types.ListValueMust(types.StringType, vals)
		} else if i < len(m.Conditions) && !m.Conditions[i].Values.IsNull() {
			cm.Values = types.ListValueMust(types.StringType, []attr.Value{})
		}
		conds = append(conds, cm)
	}
	m.Conditions = conds
}

func (r *allocationRuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m allocationRuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var out ruleAPI
	if err := r.client.do(ctx, http.MethodPost, r.client.orgPath("/allocation/rules"), r.body(ctx, m, &resp.Diagnostics), &out); err != nil {
		apiError(&resp.Diagnostics, "create allocation rule", err)
		return
	}
	fromRuleAPI(&m, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *allocationRuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m allocationRuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var out ruleAPI
	if err := r.client.do(ctx, http.MethodGet, r.client.orgPath("/allocation/rules/"+m.ID.ValueString()), nil, &out); err != nil {
		if IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "read allocation rule", err)
		return
	}
	fromRuleAPI(&m, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *allocationRuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m allocationRuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var out ruleAPI
	if err := r.client.do(ctx, http.MethodPut, r.client.orgPath("/allocation/rules/"+m.ID.ValueString()), r.body(ctx, m, &resp.Diagnostics), &out); err != nil {
		apiError(&resp.Diagnostics, "update allocation rule", err)
		return
	}
	fromRuleAPI(&m, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *allocationRuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m allocationRuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.do(ctx, http.MethodDelete, r.client.orgPath("/allocation/rules/"+m.ID.ValueString()), nil, nil); err != nil && !IsNotFound(err) {
		apiError(&resp.Diagnostics, "delete allocation rule", err)
	}
}

func (r *allocationRuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
