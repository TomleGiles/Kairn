package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/kairn-io/kairn/pkg/auth"
	"github.com/kairn-io/kairn/pkg/connector"
	"github.com/kairn-io/kairn/pkg/ids"
	"github.com/kairn-io/kairn/pkg/ingest"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/plans"
	"github.com/kairn-io/kairn/pkg/store"
)

// connectorType est la vue publique d'un type de connecteur.
type connectorType struct {
	Type                   string                 `json:"type"`
	DisplayName            string                 `json:"display_name"`
	Category               string                 `json:"category"`
	Provider               string                 `json:"provider"`
	Resources              []string               `json:"resources"`
	DefaultIntervalSeconds int                    `json:"default_interval_seconds"`
	Fields                 []connector.Field      `json:"fields"`
	Permissions            []connector.Permission `json:"permissions"`
	Metrics                bool                   `json:"metrics"`
	Billing                bool                   `json:"billing"`
	Webhook                bool                   `json:"webhook"`
	DocsURL                string                 `json:"docs_url"`
}

func typeView(i connector.TypeInfo) connectorType {
	docs := i.DocsURL
	if docs == "" {
		docs = "/docs/connectors/" + i.Type
	}
	return connectorType{
		Type: i.Type, DisplayName: i.DisplayName, Category: i.Category, Provider: i.Provider, Resources: i.Resources,
		DefaultIntervalSeconds: int(i.DefaultInterval / time.Second), Fields: i.Fields, Permissions: i.Permissions,
		Metrics: i.Metrics, Billing: i.Billing, Webhook: i.Webhook, DocsURL: docs,
	}
}

// connectorView expose un connecteur sans ses secrets.
type connectorView struct {
	model.Connector
	SecretKeys []string `json:"secret_keys" doc:"Noms des secrets configurés (valeurs jamais renvoyées)"`
	WebhookURL string   `json:"webhook_url,omitempty"`
}

func (s *Server) connectorView(ctx context.Context, c model.Connector) connectorView {
	v := connectorView{Connector: c, SecretKeys: []string{}}
	if len(c.SecretsEnc) > 0 && s.Keyring != nil {
		if m, err := s.Keyring.DecryptMap(ctx, c.SecretsEnc, ingest.SecretsAAD(c.OrgID, c.ID)); err == nil {
			for k := range m {
				v.SecretKeys = append(v.SecretKeys, k)
			}
			sort.Strings(v.SecretKeys)
		}
	}
	if c.WebhookToken != nil {
		v.WebhookURL = strings.TrimRight(s.Config.APIURL, "/") + "/ingest/v1/webhooks/" + *c.WebhookToken
	}
	return v
}

type connectorBody struct {
	Type            string            `json:"type" required:"true"`
	Name            string            `json:"name" required:"true" minLength:"1" maxLength:"120"`
	Settings        map[string]string `json:"settings,omitempty"`
	Secrets         map[string]string `json:"secrets,omitempty" doc:"Valeurs secrètes, chiffrées par enveloppe à l'enregistrement"`
	IntervalSeconds int               `json:"interval_seconds,omitempty" minimum:"0" maximum:"86400"`
	Enabled         *bool             `json:"enabled,omitempty"`
	BackfillDays    int               `json:"backfill_days,omitempty" minimum:"0" maximum:"396" doc:"Historique à importer à la création (30 par défaut)"`
}

// checkFields vérifie les champs obligatoires du type.
// checkTarget vérifie que le connecteur cible (propriétaire des ressources) existe dans l'organisation.
func (s *Server) checkTarget(ctx context.Context, settings map[string]string) error {
	t := settings[ingest.TargetSetting]
	if t == "" {
		return nil
	}
	if _, err := s.Store.Connectors().Get(ctx, t); err != nil {
		return invalid("target_connector_id does not reference a connector of this organization")
	}
	return nil
}

func checkFields(info connector.TypeInfo, settings, secrets map[string]string) error {
	var missing []string
	for _, f := range info.Fields {
		if !f.Required {
			continue
		}
		if f.Secret && secrets[f.Name] == "" || !f.Secret && settings[f.Name] == "" {
			missing = append(missing, f.Name)
		}
	}
	if len(missing) > 0 {
		return invalid("missing required fields: " + strings.Join(missing, ", "))
	}
	return nil
}

// checkConnectorLimits applique les limites du plan (nombre de connecteurs et de fournisseurs cloud).
func (s *Server) checkConnectorLimits(ctx context.Context, a access, info connector.TypeInfo) error {
	conns, err := store.ListAll(ctx, s.Store.Connectors(), func(c model.Connector) string { return c.ID }, nil)
	if err != nil {
		return err
	}
	if !plans.Within(a.Limits.MaxConnectors, len(conns)+1) {
		return huma.NewError(http.StatusPaymentRequired, fmt.Sprintf("plan limit reached (%d connectors)", a.Limits.MaxConnectors))
	}
	if info.Category == connector.CategoryCloud {
		providers := map[string]bool{info.Provider: true}
		for _, c := range conns {
			if i, ok := connector.Info(c.Type); ok && i.Category == connector.CategoryCloud {
				providers[i.Provider] = true
			}
		}
		if !plans.Within(a.Limits.MaxProviders, len(providers)) {
			return huma.NewError(http.StatusPaymentRequired, fmt.Sprintf("plan limit reached (%d cloud provider)", a.Limits.MaxProviders))
		}
	}
	return nil
}

type validateResult struct {
	OK          bool                   `json:"ok"`
	Error       string                 `json:"error,omitempty"`
	Permissions []connector.Permission `json:"permissions"`
}

func (s *Server) validateConnector(ctx context.Context, typ string, cfg connector.Config) validateResult {
	info, _ := connector.Info(typ)
	res := validateResult{Permissions: info.Permissions}
	if res.Permissions == nil {
		res.Permissions = []connector.Permission{}
	}
	c, err := connector.New(typ, cfg)
	if err == nil {
		vctx, cancel := context.WithTimeout(ctx, 25*time.Second)
		defer cancel()
		err = c.Validate(vctx, cfg)
	}
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.OK = true
	return res
}

func (s *Server) registerConnectors() {
	tag := "Connecteurs"
	huma.Register(s.API, huma.Operation{OperationID: "list-connector-types", Method: http.MethodGet, Path: Prefix + "/connector-types", Tags: []string{tag},
		Summary: "Types de connecteurs : ressources couvertes, fréquence, permissions minimales"},
		func(ctx context.Context, _ *struct{}) (*Out[[]connectorType], error) {
			if _, err := principal(ctx); err != nil {
				return nil, err
			}
			var out_ []connectorType
			for _, i := range connector.Types() {
				out_ = append(out_, typeView(i))
			}
			return out(out_), nil
		})

	huma.Register(s.API, huma.Operation{OperationID: "list-connectors", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/connectors", Tags: []string{tag},
		Summary: "Connecteurs de l'organisation et leur santé"},
		func(ctx context.Context, in *OrgPath) (*Out[[]connectorView], error) {
			ctx, _, err := s.enter(ctx, in.OrgID, auth.PermConnectorsRead, "")
			if err != nil {
				return nil, err
			}
			conns, err := store.ListAll(ctx, s.Store.Connectors(), func(c model.Connector) string { return c.ID }, nil)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			views := make([]connectorView, 0, len(conns))
			for _, c := range conns {
				views = append(views, s.connectorView(ctx, c))
			}
			return out(views), nil
		})

	huma.Register(s.API, huma.Operation{OperationID: "get-connector", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/connectors/{id}", Tags: []string{tag},
		Summary: "Détail d'un connecteur"},
		func(ctx context.Context, in *IDPath) (*Out[connectorView], error) {
			ctx, _, err := s.enter(ctx, in.OrgID, auth.PermConnectorsRead, "")
			if err != nil {
				return nil, err
			}
			c, err := s.Store.Connectors().Get(ctx, in.ID)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			return out(s.connectorView(ctx, c)), nil
		})

	huma.Register(s.API, huma.Operation{OperationID: "validate-connector", Method: http.MethodPost, Path: Prefix + "/orgs/{org_id}/connectors/validate", Tags: []string{tag},
		Summary: "Vérifie l'accès et les permissions avant enregistrement"},
		func(ctx context.Context, in *CreateIn[connectorBody]) (*Out[validateResult], error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermConnectorsManage, "")
			if err != nil {
				return nil, err
			}
			if _, ok := connector.Info(in.Body.Type); !ok {
				return nil, invalid("unknown connector type")
			}
			res := s.validateConnector(ctx, in.Body.Type, connector.Config{OrgID: a.Org.ID, Settings: in.Body.Settings, Secrets: in.Body.Secrets})
			return out(res), nil
		})

	huma.Register(s.API, huma.Operation{OperationID: "create-connector", Method: http.MethodPost, Path: Prefix + "/orgs/{org_id}/connectors", Tags: []string{tag},
		Summary: "Ajoute un connecteur (lecture seule) et lance la première synchronisation", DefaultStatus: http.StatusCreated},
		func(ctx context.Context, in *CreateIn[connectorBody]) (*Out[connectorView], error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermConnectorsManage, "")
			if err != nil {
				return nil, err
			}
			info, ok := connector.Info(in.Body.Type)
			if !ok {
				return nil, invalid("unknown connector type")
			}
			if err := checkFields(info, in.Body.Settings, in.Body.Secrets); err != nil {
				return nil, err
			}
			if err := s.checkTarget(ctx, in.Body.Settings); err != nil {
				return nil, err
			}
			if err := s.checkConnectorLimits(ctx, a, info); err != nil {
				return nil, s.fail(ctx, err)
			}
			c := model.Connector{
				ID: ids.New(), OrgID: a.Org.ID, Type: in.Body.Type, Name: in.Body.Name, Settings: in.Body.Settings,
				Status: model.ConnectorPending, IntervalSeconds: in.Body.IntervalSeconds, Enabled: true,
			}
			if c.Settings == nil {
				c.Settings = map[string]string{}
			}
			if in.Body.Enabled != nil {
				c.Enabled = *in.Body.Enabled
			}
			if c.IntervalSeconds == 0 {
				c.IntervalSeconds = int(info.DefaultInterval / time.Second)
			}
			if c.IntervalSeconds < 60 {
				c.IntervalSeconds = 60
			}
			if len(in.Body.Secrets) > 0 {
				if s.Keyring == nil {
					return nil, huma.NewError(http.StatusServiceUnavailable, "secret storage is not configured")
				}
				enc, err := s.Keyring.EncryptMap(ctx, in.Body.Secrets, ingest.SecretsAAD(c.OrgID, c.ID))
				if err != nil {
					return nil, s.fail(ctx, err)
				}
				c.SecretsEnc = enc
			}
			if info.Webhook {
				tok, err := auth.RandomString(24)
				if err != nil {
					return nil, s.fail(ctx, err)
				}
				c.WebhookToken = &tok
			}
			if err := s.Store.Connectors().Create(ctx, &c); err != nil {
				return nil, s.fail(ctx, err)
			}
			s.audit(ctx, a, "connector.create", "connector", c.ID, map[string]any{"type": c.Type, "name": c.Name})
			days := in.Body.BackfillDays
			if days == 0 {
				days = 30
			}
			if c.Enabled && s.Jobs != nil {
				if err := s.Jobs.RequestSync(ctx, a.Org.ID, c.ID, days); err != nil {
					s.Log.Warn("initial sync request failed", "connector", c.ID, "err", err)
				}
			}
			return out(s.connectorView(ctx, c)), nil
		})

	type patchBody struct {
		Name            *string           `json:"name,omitempty" minLength:"1" maxLength:"120"`
		Settings        map[string]string `json:"settings,omitempty"`
		Secrets         map[string]string `json:"secrets,omitempty" doc:"Secrets à remplacer ; valeur vide = suppression"`
		IntervalSeconds *int              `json:"interval_seconds,omitempty" minimum:"60" maximum:"86400"`
		Enabled         *bool             `json:"enabled,omitempty"`
	}
	huma.Register(s.API, huma.Operation{OperationID: "update-connector", Method: http.MethodPatch, Path: Prefix + "/orgs/{org_id}/connectors/{id}", Tags: []string{tag},
		Summary: "Modifie un connecteur"},
		func(ctx context.Context, in *UpdateIn[patchBody]) (*Out[connectorView], error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermConnectorsManage, "")
			if err != nil {
				return nil, err
			}
			c, err := s.Store.Connectors().Get(ctx, in.ID)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			b := in.Body
			if b.Name != nil {
				c.Name = *b.Name
			}
			if err := s.checkTarget(ctx, b.Settings); err != nil {
				return nil, err
			}
			for k, v := range b.Settings {
				if v == "" {
					delete(c.Settings, k)
				} else {
					c.Settings[k] = v
				}
			}
			if len(b.Secrets) > 0 {
				aad := ingest.SecretsAAD(c.OrgID, c.ID)
				cur, err := s.Keyring.DecryptMap(ctx, c.SecretsEnc, aad)
				if err != nil {
					return nil, s.fail(ctx, err)
				}
				for k, v := range b.Secrets {
					if v == "" {
						delete(cur, k)
					} else {
						cur[k] = v
					}
				}
				if c.SecretsEnc, err = s.Keyring.EncryptMap(ctx, cur, aad); err != nil {
					return nil, s.fail(ctx, err)
				}
			}
			if b.IntervalSeconds != nil {
				c.IntervalSeconds = *b.IntervalSeconds
			}
			if b.Enabled != nil {
				c.Enabled = *b.Enabled
				if !c.Enabled {
					c.Status = model.ConnectorDisabled
				} else if c.Status == model.ConnectorDisabled {
					c.Status = model.ConnectorPending
				}
			}
			if err := s.Store.Connectors().Update(ctx, &c); err != nil {
				return nil, s.fail(ctx, err)
			}
			keys := make([]string, 0, len(b.Secrets))
			for k := range b.Secrets {
				keys = append(keys, k)
			}
			s.audit(ctx, a, "connector.update", "connector", c.ID, map[string]any{"secrets_changed": keys})
			return out(s.connectorView(ctx, c)), nil
		})

	huma.Register(s.API, huma.Operation{OperationID: "delete-connector", Method: http.MethodDelete, Path: Prefix + "/orgs/{org_id}/connectors/{id}", Tags: []string{tag},
		Summary: "Supprime un connecteur", DefaultStatus: http.StatusNoContent},
		func(ctx context.Context, in *IDPath) (*Empty, error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermConnectorsManage, "")
			if err != nil {
				return nil, err
			}
			if err := s.Store.Connectors().Delete(ctx, in.ID); err != nil {
				return nil, s.fail(ctx, err)
			}
			s.audit(ctx, a, "connector.delete", "connector", in.ID, nil)
			return &Empty{}, nil
		})

	huma.Register(s.API, huma.Operation{OperationID: "test-connector", Method: http.MethodPost, Path: Prefix + "/orgs/{org_id}/connectors/{id}/test", Tags: []string{tag},
		Summary: "Teste la configuration enregistrée"},
		func(ctx context.Context, in *IDPath) (*Out[validateResult], error) {
			ctx, _, err := s.enter(ctx, in.OrgID, auth.PermConnectorsManage, "")
			if err != nil {
				return nil, err
			}
			c, err := s.Store.Connectors().Get(ctx, in.ID)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			cfg, err := ingest.Config(ctx, s.Keyring, c)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			return out(s.validateConnector(ctx, c.Type, cfg)), nil
		})

	type syncIn struct {
		IDPath
		Body struct {
			BackfillDays int `json:"backfill_days,omitempty" minimum:"0" maximum:"396"`
		}
	}
	huma.Register(s.API, huma.Operation{OperationID: "sync-connector", Method: http.MethodPost, Path: Prefix + "/orgs/{org_id}/connectors/{id}/sync", Tags: []string{tag},
		Summary: "Déclenche une synchronisation (ou un backfill)", DefaultStatus: http.StatusAccepted},
		func(ctx context.Context, in *syncIn) (*Empty, error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermConnectorsManage, "")
			if err != nil {
				return nil, err
			}
			if _, err := s.Store.Connectors().Get(ctx, in.ID); err != nil {
				return nil, s.fail(ctx, err)
			}
			if err := s.Jobs.RequestSync(ctx, a.Org.ID, in.ID, in.Body.BackfillDays); err != nil {
				return nil, s.fail(ctx, err)
			}
			s.audit(ctx, a, "connector.sync", "connector", in.ID, map[string]any{"backfill_days": in.Body.BackfillDays})
			return &Empty{}, nil
		})

	huma.Register(s.API, huma.Operation{OperationID: "list-connector-runs", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/connectors/{id}/runs", Tags: []string{tag},
		Summary: "Historique des synchronisations"},
		func(ctx context.Context, in *IDPath) (*Out[[]model.ConnectorRun], error) {
			ctx, _, err := s.enter(ctx, in.OrgID, auth.PermConnectorsRead, "")
			if err != nil {
				return nil, err
			}
			if _, err := s.Store.Connectors().Get(ctx, in.ID); err != nil {
				return nil, s.fail(ctx, err)
			}
			runs, err := s.Store.Runs().ListByConnector(ctx, in.ID, 100)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			if runs == nil {
				runs = []model.ConnectorRun{}
			}
			return out(runs), nil
		})

	huma.Register(s.API, huma.Operation{OperationID: "rotate-connector-webhook", Method: http.MethodPost, Path: Prefix + "/orgs/{org_id}/connectors/{id}/webhook/rotate", Tags: []string{tag},
		Summary: "Régénère l'URL de webhook d'un connecteur d'événements"},
		func(ctx context.Context, in *IDPath) (*Out[connectorView], error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermConnectorsManage, "")
			if err != nil {
				return nil, err
			}
			c, err := s.Store.Connectors().Get(ctx, in.ID)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			if info, _ := connector.Info(c.Type); !info.Webhook {
				return nil, huma.Error409Conflict("this connector does not receive webhooks")
			}
			tok, err := auth.RandomString(24)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			c.WebhookToken = &tok
			if err := s.Store.Connectors().Update(ctx, &c); err != nil {
				return nil, s.fail(ctx, err)
			}
			s.audit(ctx, a, "connector.webhook.rotate", "connector", c.ID, nil)
			return out(s.connectorView(ctx, c)), nil
		})
}
