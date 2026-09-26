package provider

import (
	"context"
	"net/http"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// connectorResource gère un connecteur. Les secrets sont en écriture seule :
// l'API ne renvoie que leurs noms ; l'état conserve les valeurs configurées
// (marquées sensibles) et détecte la suppression d'un secret hors Terraform.
type connectorResource struct{ base }

// NewConnectorResource construit kairn_connector.
func NewConnectorResource() resource.Resource { return &connectorResource{} }

type connectorModel struct {
	ID              types.String `tfsdk:"id"`
	Type            types.String `tfsdk:"type"`
	Name            types.String `tfsdk:"name"`
	Enabled         types.Bool   `tfsdk:"enabled"`
	IntervalSeconds types.Int64  `tfsdk:"interval_seconds"`
	Settings        types.Map    `tfsdk:"settings"`
	Secrets         types.Map    `tfsdk:"secrets"`
	BackfillDays    types.Int64  `tfsdk:"backfill_days"`
	Status          types.String `tfsdk:"status"`
	WebhookURL      types.String `tfsdk:"webhook_url"`
}

type connectorAPI struct {
	ID              string            `json:"id"`
	Type            string            `json:"type"`
	Name            string            `json:"name"`
	Enabled         bool              `json:"enabled"`
	IntervalSeconds int64             `json:"interval_seconds"`
	Settings        map[string]string `json:"settings"`
	SecretKeys      []string          `json:"secret_keys"`
	Status          string            `json:"status"`
	WebhookURL      string            `json:"webhook_url"`
}

type connectorCreate struct {
	Type            string            `json:"type"`
	Name            string            `json:"name"`
	Settings        map[string]string `json:"settings,omitempty"`
	Secrets         map[string]string `json:"secrets,omitempty"`
	IntervalSeconds int64             `json:"interval_seconds,omitempty"`
	Enabled         *bool             `json:"enabled,omitempty"`
	BackfillDays    int64             `json:"backfill_days,omitempty"`
}

type connectorPatch struct {
	Name            *string           `json:"name,omitempty"`
	Enabled         *bool             `json:"enabled,omitempty"`
	IntervalSeconds *int64            `json:"interval_seconds,omitempty"`
	Settings        map[string]string `json:"settings,omitempty"`
	Secrets         map[string]string `json:"secrets,omitempty"`
}

func (r *connectorResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_connector"
}

func (r *connectorResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Connecteur Kairn (lecture seule chez le fournisseur). Voir la documentation de chaque type pour les paramètres et permissions minimales.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"type": schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Description: "Type de connecteur (openstack, kubernetes, prometheus, ovh, scaleway, outscale, focus, agent, gitlab…)."},
			"name":    schema.StringAttribute{Required: true},
			"enabled": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(true)},
			"interval_seconds": schema.Int64Attribute{Optional: true, Computed: true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
				Description:   "Période de synchronisation (60 à 86400 s). Par défaut : celle du type de connecteur."},
			"settings": schema.MapAttribute{ElementType: types.StringType, Optional: true,
				Description: "Paramètres non secrets (URL, région, projet…)."},
			"secrets": schema.MapAttribute{ElementType: types.StringType, Optional: true, Sensitive: true,
				Description: "Secrets (mots de passe, clés, jetons), chiffrés par Kairn et jamais relus : fournir via des variables sensibles."},
			"backfill_days": schema.Int64Attribute{Optional: true,
				Description: "Historique à importer à la création (0 à 396 jours ; défaut Kairn : 30). Sans effet après la création."},
			"status": schema.StringAttribute{Computed: true, Description: "État de santé du connecteur."},
			"webhook_url": schema.StringAttribute{Computed: true, Sensitive: true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				Description:   "URL de réception des webhooks (sources d'événements, agent) ; contient un jeton secret."},
		},
	}
}

// fromConnectorAPI met à jour le modèle depuis l'API. Seules les clés de
// paramètres gérées par Terraform sont comparées (le serveur peut en ajouter).
func fromConnectorAPI(m *connectorModel, c connectorAPI, settings, secrets map[string]string) {
	m.ID, m.Type, m.Name = types.StringValue(c.ID), types.StringValue(c.Type), types.StringValue(c.Name)
	m.Enabled, m.IntervalSeconds = types.BoolValue(c.Enabled), types.Int64Value(c.IntervalSeconds)
	m.Status = types.StringValue(c.Status)
	if c.WebhookURL != "" {
		m.WebhookURL = types.StringValue(c.WebhookURL)
	} else {
		m.WebhookURL = types.StringNull()
	}
	if settings != nil {
		seen := map[string]string{}
		for k := range settings {
			if v, ok := c.Settings[k]; ok {
				seen[k] = v
			}
		}
		m.Settings = mapValue(seen, m.Settings.IsNull())
	}
	if secrets != nil {
		kept := map[string]string{}
		for k, v := range secrets {
			if slices.Contains(c.SecretKeys, k) {
				kept[k] = v // la valeur n'est jamais relue : on conserve la valeur connue
			}
		}
		m.Secrets = mapValue(kept, m.Secrets.IsNull())
	}
}

// mapValue construit une map Terraform ; vide et non configurée → nulle.
func mapValue(v map[string]string, wasNull bool) types.Map {
	if len(v) == 0 && wasNull {
		return types.MapNull(types.StringType)
	}
	elems := map[string]types.String{}
	for k, s := range v {
		elems[k] = types.StringValue(s)
	}
	out, _ := types.MapValueFrom(context.Background(), types.StringType, elems)
	return out
}

func (r *connectorResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m connectorModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	settings := stringMap(ctx, m.Settings, &resp.Diagnostics)
	secrets := stringMap(ctx, m.Secrets, &resp.Diagnostics)
	enabled := m.Enabled.ValueBool()
	body := connectorCreate{Type: m.Type.ValueString(), Name: m.Name.ValueString(), Settings: settings, Secrets: secrets,
		Enabled: &enabled, BackfillDays: m.BackfillDays.ValueInt64()}
	if !m.IntervalSeconds.IsUnknown() && !m.IntervalSeconds.IsNull() {
		body.IntervalSeconds = m.IntervalSeconds.ValueInt64()
	}
	var out connectorAPI
	if err := r.client.do(ctx, http.MethodPost, r.client.orgPath("/connectors"), body, &out); err != nil {
		apiError(&resp.Diagnostics, "create connector", err)
		return
	}
	fromConnectorAPI(&m, out, settings, secrets)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *connectorResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m connectorModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var out connectorAPI
	if err := r.client.do(ctx, http.MethodGet, r.client.orgPath("/connectors/"+m.ID.ValueString()), nil, &out); err != nil {
		if IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "read connector", err)
		return
	}
	settings := stringMap(ctx, m.Settings, &resp.Diagnostics)
	secrets := stringMap(ctx, m.Secrets, &resp.Diagnostics)
	if m.Settings.IsNull() && len(out.Settings) > 0 && m.Type.IsNull() {
		settings = out.Settings // import : on reprend les paramètres existants
	}
	fromConnectorAPI(&m, out, settings, secrets)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// diffMap produit le patch fusionné attendu par l'API : valeur à écrire, ou
// chaîne vide pour une clé retirée de la configuration.
func diffMap(prev, next map[string]string) map[string]string {
	patch := map[string]string{}
	for k, v := range next {
		if prev[k] != v {
			patch[k] = v
		}
	}
	for k := range prev {
		if _, ok := next[k]; !ok {
			patch[k] = ""
		}
	}
	return patch
}

func (r *connectorResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state connectorModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	settings := stringMap(ctx, plan.Settings, &resp.Diagnostics)
	secrets := stringMap(ctx, plan.Secrets, &resp.Diagnostics)
	body := connectorPatch{
		Name:     strPtr(plan.Name),
		Settings: diffMap(stringMap(ctx, state.Settings, &resp.Diagnostics), settings),
		Secrets:  diffMap(stringMap(ctx, state.Secrets, &resp.Diagnostics), secrets),
	}
	enabled := plan.Enabled.ValueBool()
	body.Enabled = &enabled
	if !plan.IntervalSeconds.IsUnknown() && !plan.IntervalSeconds.IsNull() {
		v := plan.IntervalSeconds.ValueInt64()
		body.IntervalSeconds = &v
	}
	var out connectorAPI
	if err := r.client.do(ctx, http.MethodPatch, r.client.orgPath("/connectors/"+state.ID.ValueString()), body, &out); err != nil {
		apiError(&resp.Diagnostics, "update connector", err)
		return
	}
	fromConnectorAPI(&plan, out, settings, secrets)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *connectorResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m connectorModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.do(ctx, http.MethodDelete, r.client.orgPath("/connectors/"+m.ID.ValueString()), nil, nil); err != nil && !IsNotFound(err) {
		apiError(&resp.Diagnostics, "delete connector", err)
	}
}

func (r *connectorResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
