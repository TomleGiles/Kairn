package api

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/auth"
	"github.com/kairn-io/kairn/pkg/budget"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/plans"
	"github.com/kairn-io/kairn/pkg/secrets"
	"github.com/kairn-io/kairn/pkg/store"
)

// ChannelSecretsAAD lie les secrets d'un canal à son organisation.
func ChannelSecretsAAD(orgID, channelID string) []byte {
	return secrets.AAD(orgID, "channel", channelID)
}

type budgetBody struct {
	NodeID        *string         `json:"node_id,omitempty" doc:"Nœud d'allocation (vide = toute l'organisation)"`
	Name          string          `json:"name" required:"true" minLength:"1" maxLength:"120"`
	Period        string          `json:"period" required:"true" enum:"monthly,quarterly,yearly"`
	Amount        decimal.Decimal `json:"amount" required:"true"`
	Currency      string          `json:"currency,omitempty" enum:"EUR,USD,GBP,CHF"`
	Thresholds    []int           `json:"thresholds,omitempty" doc:"Seuils d'alerte en % (défaut 50, 80, 100)"`
	ForecastAlert *bool           `json:"forecast_alert,omitempty"`
	ChannelIDs    []string        `json:"channel_ids,omitempty"`
}

type alertRuleBody struct {
	Name               string               `json:"name" required:"true" minLength:"1" maxLength:"120"`
	Kind               string               `json:"kind" required:"true" enum:"budget_actual,budget_forecast,anomaly,uptime,recommendation,connector"`
	Config             map[string]string    `json:"config,omitempty" doc:"Paramètres (min_severity, min_savings…)"`
	ChannelIDs         []string             `json:"channel_ids" required:"true" minItems:"1"`
	BusinessHours      *model.BusinessHours `json:"business_hours,omitempty"`
	GroupWindowSeconds int                  `json:"group_window_seconds,omitempty" minimum:"0" maximum:"86400"`
	Enabled            *bool                `json:"enabled,omitempty"`
}

type channelBody struct {
	Kind     string            `json:"kind" required:"true" enum:"email,slack,teams,mattermost,webhook,pagerduty"`
	Name     string            `json:"name" required:"true" minLength:"1" maxLength:"120"`
	Settings map[string]string `json:"settings,omitempty" doc:"Ex. recipients (e-mail), channel (Slack)"`
	Secrets  map[string]string `json:"secrets,omitempty" doc:"Ex. webhook_url, routing_key, signing_secret (chiffrés)"`
	Enabled  *bool             `json:"enabled,omitempty"`
}

type silenceBody struct {
	Matchers map[string]string `json:"matchers" required:"true" doc:"kind, severity, fingerprint, rule_id"`
	StartsAt time.Time         `json:"starts_at" required:"true"`
	EndsAt   time.Time         `json:"ends_at" required:"true"`
	Reason   string            `json:"reason" required:"true" minLength:"1" maxLength:"500"`
}

func (s *Server) checkChannels(ctx context.Context, idsList []string) error {
	for _, id := range idsList {
		if _, err := s.Store.Channels().Get(ctx, id); err != nil {
			return invalid("unknown channel " + id)
		}
	}
	return nil
}

func validateBusinessHours(b *model.BusinessHours) error {
	if b == nil {
		return nil
	}
	if _, err := time.LoadLocation(b.Timezone); err != nil {
		return invalid("business_hours.timezone is invalid")
	}
	for _, t := range []string{b.Start, b.End} {
		if _, err := time.Parse("15:04", t); err != nil {
			return invalid("business_hours start/end must be HH:MM")
		}
	}
	for _, d := range b.Days {
		if d < 1 || d > 7 {
			return invalid("business_hours.days must be 1..7")
		}
	}
	return nil
}

func validateChannel(kind string, settings, sec map[string]string, isNew bool) error {
	switch kind {
	case model.ChannelEmail:
		if settings["recipients"] == "" {
			return invalid("email channel requires settings.recipients")
		}
	case model.ChannelSlack, model.ChannelTeams, model.ChannelMattermost, model.ChannelWebhook:
		if isNew && sec["webhook_url"] == "" && sec["url"] == "" {
			return invalid(kind + " channel requires secrets.webhook_url")
		}
		for _, k := range []string{"webhook_url", "url"} {
			if v := sec[k]; v != "" {
				u, err := url.Parse(v)
				if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
					return invalid("invalid webhook url")
				}
			}
		}
	case model.ChannelPagerDuty:
		if isNew && sec["routing_key"] == "" {
			return invalid("pagerduty channel requires secrets.routing_key")
		}
	}
	return nil
}

func (s *Server) registerAlerting() {
	tag := "Budgets et alertes"
	registerCRUD(s, crudSpec[model.Budget, budgetBody]{
		Tag: tag, Path: "/budgets", Name: "budget", Plural: "budgets", Summary: "budgets",
		Read: auth.PermCostsRead, Write: auth.PermBudgetsManage, Feature: plans.FeatureBudgets, Repo: func() store.CRUD[model.Budget] { return s.Store.Budgets() },
		ID: func(b *model.Budget) string { return b.ID }, Filters: []string{"node_id"},
		Apply: func(ctx context.Context, a access, b *budgetBody, m *model.Budget, isNew bool) error {
			if !b.Amount.IsPositive() {
				return invalid("amount must be positive")
			}
			if b.NodeID != nil && *b.NodeID != "" {
				if err := s.nodeExists(ctx, *b.NodeID); err != nil {
					return err
				}
			}
			if err := s.checkChannels(ctx, b.ChannelIDs); err != nil {
				return err
			}
			th := b.Thresholds
			if len(th) == 0 {
				th = []int{50, 80, 100}
			}
			for _, t := range th {
				if t <= 0 || t > 1000 {
					return invalid("thresholds must be between 1 and 1000")
				}
			}
			*m = model.Budget{ID: m.ID, OrgID: a.Org.ID, NodeID: b.NodeID, Name: b.Name, Period: b.Period, Amount: b.Amount,
				Currency: b.Currency, Thresholds: th, ForecastAlert: b.ForecastAlert == nil || *b.ForecastAlert, ChannelIDs: b.ChannelIDs, CreatedAt: m.CreatedAt}
			if m.Currency == "" {
				m.Currency = a.Org.Currency
			}
			if m.ChannelIDs == nil {
				m.ChannelIDs = []string{}
			}
			return nil
		},
	})
	huma.Register(s.API, huma.Operation{OperationID: "list-budget-statuses", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/budgets-status", Tags: []string{tag},
		Summary: "État des budgets : réel et prévision de fin de période"},
		func(ctx context.Context, in *OrgPath) (*Out[[]model.BudgetStatus], error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermCostsRead, plans.FeatureBudgets)
			if err != nil {
				return nil, err
			}
			bs, err := store.ListAll(ctx, s.Store.Budgets(), func(b model.Budget) string { return b.ID }, nil)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			tree, _, err := s.tree(ctx)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			allowed, err := s.allowedNodes(ctx, a)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			allowedSet := map[string]bool{}
			for _, n := range allowed {
				allowedSet[n] = true
			}
			out_ := []model.BudgetStatus{}
			for _, b := range bs {
				if allowed != nil && (b.NodeID == nil || !allowedSet[*b.NodeID]) {
					continue
				}
				st, err := budget.Evaluate(ctx, s.TSDB, b, tree, s.now())
				if err != nil {
					return nil, s.fail(ctx, err)
				}
				out_ = append(out_, st)
			}
			return out(out_), nil
		})

	registerCRUD(s, crudSpec[model.AlertRule, alertRuleBody]{
		Tag: tag, Path: "/alert-rules", Name: "alert-rule", Plural: "alert-rules", Summary: "règles d'alerte",
		Read: auth.PermCostsRead, Write: auth.PermAlertsManage, Repo: func() store.CRUD[model.AlertRule] { return s.Store.AlertRules() }, Filters: []string{"kind"},
		ID: func(r *model.AlertRule) string { return r.ID },
		Apply: func(ctx context.Context, a access, b *alertRuleBody, r *model.AlertRule, isNew bool) error {
			if err := s.checkChannels(ctx, b.ChannelIDs); err != nil {
				return err
			}
			if err := validateBusinessHours(b.BusinessHours); err != nil {
				return err
			}
			if b.Kind == model.AlertAnomaly && !a.Limits.Allows(plans.FeatureAnomalies) {
				return huma.NewError(http.StatusPaymentRequired, "anomalies are not included in this plan")
			}
			if b.Kind == model.AlertUptime && !a.Limits.Allows(plans.FeatureUptime) {
				return huma.NewError(http.StatusPaymentRequired, "uptime checks are not included in this plan")
			}
			*r = model.AlertRule{ID: r.ID, OrgID: a.Org.ID, Name: b.Name, Kind: b.Kind, Config: b.Config, ChannelIDs: b.ChannelIDs,
				BusinessHours: b.BusinessHours, GroupWindowSeconds: b.GroupWindowSeconds, Enabled: b.Enabled == nil || *b.Enabled, CreatedAt: r.CreatedAt}
			if r.Config == nil {
				r.Config = map[string]string{}
			}
			if r.GroupWindowSeconds == 0 {
				r.GroupWindowSeconds = 3600
			}
			return nil
		},
	})

	registerCRUD(s, crudSpec[model.NotificationChannel, channelBody]{
		Tag: tag, Path: "/channels", Name: "notification-channel", Plural: "notification-channels", Summary: "canaux de notification",
		Read: auth.PermCostsRead, Write: auth.PermAlertsManage, Repo: func() store.CRUD[model.NotificationChannel] { return s.Store.Channels() }, Filters: []string{"kind"},
		ID: func(c *model.NotificationChannel) string { return c.ID },
		Apply: func(ctx context.Context, a access, b *channelBody, c *model.NotificationChannel, isNew bool) error {
			if err := validateChannel(b.Kind, b.Settings, b.Secrets, isNew); err != nil {
				return err
			}
			if isNew {
				c.ID = newID()
			}
			c.OrgID, c.Kind, c.Name, c.Settings = a.Org.ID, b.Kind, b.Name, b.Settings
			if c.Settings == nil {
				c.Settings = map[string]string{}
			}
			c.Enabled = b.Enabled == nil || *b.Enabled
			if len(b.Secrets) > 0 {
				if s.Keyring == nil {
					return huma.NewError(http.StatusServiceUnavailable, "secret storage is not configured")
				}
				aad := ChannelSecretsAAD(c.OrgID, c.ID)
				cur, err := s.Keyring.DecryptMap(ctx, c.SecretsEnc, aad)
				if err != nil {
					return err
				}
				for k, v := range b.Secrets {
					if v == "" {
						delete(cur, k)
					} else {
						cur[k] = v
					}
				}
				if c.SecretsEnc, err = s.Keyring.EncryptMap(ctx, cur, aad); err != nil {
					return err
				}
			}
			return nil
		},
	})
	huma.Register(s.API, huma.Operation{OperationID: "test-notification-channel", Method: http.MethodPost, Path: Prefix + "/orgs/{org_id}/channels/{id}/test", Tags: []string{tag},
		Summary: "Envoie une notification de test", DefaultStatus: http.StatusNoContent},
		func(ctx context.Context, in *IDPath) (*Empty, error) {
			ctx, _, err := s.enter(ctx, in.OrgID, auth.PermAlertsManage, "")
			if err != nil {
				return nil, err
			}
			if _, err := s.Store.Channels().Get(ctx, in.ID); err != nil {
				return nil, s.fail(ctx, err)
			}
			if s.Channels == nil {
				return nil, huma.NewError(http.StatusServiceUnavailable, "notifier unavailable")
			}
			if err := s.Channels.Test(ctx, in.ID); err != nil {
				return nil, huma.NewError(http.StatusBadGateway, "delivery failed: "+err.Error())
			}
			return &Empty{}, nil
		})

	registerCRUD(s, crudSpec[model.Silence, silenceBody]{
		Tag: tag, Path: "/silences", Name: "silence", Plural: "silences", Summary: "silences",
		Read: auth.PermCostsRead, Write: auth.PermAlertsManage, Repo: func() store.CRUD[model.Silence] { return s.Store.Silences() },
		ID: func(x *model.Silence) string { return x.ID },
		Apply: func(ctx context.Context, a access, b *silenceBody, x *model.Silence, isNew bool) error {
			if !b.EndsAt.After(b.StartsAt) {
				return invalid("ends_at must be after starts_at")
			}
			if len(b.Matchers) == 0 {
				return invalid("at least one matcher is required")
			}
			for k := range b.Matchers {
				switch k {
				case "kind", "severity", "fingerprint", "rule_id":
				default:
					return invalid("unknown matcher " + k)
				}
			}
			*x = model.Silence{ID: x.ID, OrgID: a.Org.ID, Matchers: b.Matchers, StartsAt: b.StartsAt, EndsAt: b.EndsAt,
				Reason: b.Reason, CreatedBy: a.P.ActorID(), CreatedAt: x.CreatedAt}
			return nil
		},
	})

	type alertsIn struct {
		OrgPath
		Status string `query:"status" enum:"firing,resolved,silenced,suppressed"`
		Kind   string `query:"kind"`
		Cursor string `query:"cursor"`
		Limit  int    `query:"limit" minimum:"0" maximum:"500"`
	}
	huma.Register(s.API, huma.Operation{OperationID: "list-alert-events", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/alerts", Tags: []string{tag},
		Summary: "Alertes déclenchées (dédupliquées)"},
		func(ctx context.Context, in *alertsIn) (*Out[Page[model.AlertEvent]], error) {
			ctx, _, err := s.enter(ctx, in.OrgID, auth.PermCostsRead, "")
			if err != nil {
				return nil, err
			}
			f := map[string]string{}
			if in.Status != "" {
				f["status"] = in.Status
			}
			if in.Kind != "" {
				f["kind"] = in.Kind
			}
			q := store.ListQuery{Cursor: in.Cursor, Limit: in.Limit, Filters: f}.Normalize()
			evs, err := s.Store.AlertEvents().List(ctx, q)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			return out(page(evs, q.Limit, func(e model.AlertEvent) string { return e.ID })), nil
		})
	huma.Register(s.API, huma.Operation{OperationID: "resolve-alert-event", Method: http.MethodPost, Path: Prefix + "/orgs/{org_id}/alerts/{id}/resolve", Tags: []string{tag},
		Summary: "Résout manuellement une alerte"},
		func(ctx context.Context, in *IDPath) (*Out[model.AlertEvent], error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermAlertsManage, "")
			if err != nil {
				return nil, err
			}
			e, err := s.Store.AlertEvents().Get(ctx, in.ID)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			e.Status = model.AlertResolved
			if err := s.Store.AlertEvents().Update(ctx, &e); err != nil {
				return nil, s.fail(ctx, err)
			}
			s.audit(ctx, a, "alert.resolve", "alert_event", e.ID, nil)
			return out(e), nil
		})
}
