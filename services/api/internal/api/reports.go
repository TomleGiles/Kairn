package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/kairn-io/kairn/pkg/auth"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/objstore"
	"github.com/kairn-io/kairn/pkg/plans"
	"github.com/kairn-io/kairn/pkg/secrets"
	"github.com/kairn-io/kairn/pkg/store"
)

// ExportSecretsAAD lie les secrets d'un export à son organisation.
func ExportSecretsAAD(orgID, id string) []byte { return secrets.AAD(orgID, "export", id) }

// WebhookSecretAAD lie le secret de signature d'un webhook à son organisation.
func WebhookSecretAAD(orgID, id string) []byte { return secrets.AAD(orgID, "webhook", id) }

// llmPolicy explique pourquoi l'assistant est indisponible pour l'organisation.
func llmPolicy(o model.Organization, l plans.Limits, usedTokens int) error {
	p := o.Settings.LLMProvider
	if p == "" {
		p = "anthropic"
	}
	switch {
	case p == "none":
		return huma.Error403Forbidden("AI features are disabled for this organization")
	case p == "anthropic" && !o.Settings.AllowExternalLLM:
		return huma.Error403Forbidden("the organization has not authorized LLM calls outside the EU; enable allow_external_llm or choose a sovereign provider (mistral, local)")
	case p == "local" && !l.Allows(plans.FeatureSovereignLLM):
		return huma.NewError(http.StatusPaymentRequired, "self-hosted LLM requires the Enterprise plan")
	}
	if l.LLMTokensMonth >= 0 && usedTokens >= l.LLMTokensMonth {
		return huma.NewError(http.StatusTooManyRequests, "monthly AI quota reached for this plan")
	}
	return nil
}

type exportBody struct {
	Name        string            `json:"name" required:"true" minLength:"1" maxLength:"120"`
	Format      string            `json:"format" required:"true" enum:"csv,parquet"`
	Destination map[string]string `json:"destination" required:"true" doc:"endpoint, bucket, prefix, region"`
	Secrets     map[string]string `json:"secrets,omitempty" doc:"access_key, secret_key (chiffrés)"`
	Schedule    string            `json:"schedule" required:"true" enum:"daily,monthly"`
	Enabled     *bool             `json:"enabled,omitempty"`
}

type webhookBody struct {
	URL     string   `json:"url" required:"true" format:"uri" maxLength:"2048"`
	Events  []string `json:"events" required:"true" minItems:"1" doc:"alert.fired, anomaly.detected, recommendation.created, budget.threshold, report.ready, connector.failed"`
	Secret  string   `json:"secret,omitempty" minLength:"16" maxLength:"256" doc:"Secret de signature HMAC-SHA256 (X-Kairn-Signature)"`
	Enabled *bool    `json:"enabled,omitempty"`
}

// WebhookEvents liste les événements pouvant être poussés vers les webhooks clients.
var WebhookEvents = []string{"alert.fired", "anomaly.detected", "recommendation.created", "budget.threshold", "report.ready", "connector.failed"}

// ReportPeriodBody désigne le mois d'un rapport (AAAA-MM).
type ReportPeriodBody struct {
	Period string `json:"period" required:"true" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"Mois du rapport (AAAA-MM)"`
}

func (s *Server) registerReports() {
	tag := "Rapports et IA"
	registerCRUD(s, crudSpec[model.Report, ReportPeriodBody]{
		Tag: tag, Path: "/reports", Name: "report", Plural: "reports", Summary: "rapports",
		Read: auth.PermReportsRead, Write: auth.PermReportsManage, Feature: plans.FeatureReports,
		Repo: func() store.CRUD[model.Report] { return s.Store.Reports() },
		ID:   func(r *model.Report) string { return r.ID },
		Apply: func(ctx context.Context, a access, b *ReportPeriodBody, r *model.Report, isNew bool) error {
			if !isNew {
				return huma.Error409Conflict("reports are immutable; generate a new one")
			}
			if _, err := s.Store.Reports().GetByPeriod(ctx, "monthly_exec", b.Period); err == nil {
				return huma.Error409Conflict("a report already exists for this period; use POST /reports/generate to regenerate it")
			}
			*r = model.Report{OrgID: a.Org.ID, Kind: "monthly_exec", Period: b.Period, Status: "pending", Summary: map[string]any{}}
			return nil
		},
		After: func(ctx context.Context, a access, r *model.Report, op string) {
			if op == "create" && s.Jobs != nil {
				_ = s.Jobs.RequestReport(ctx, a.Org.ID, r.Period)
			}
		},
	})
	huma.Register(s.API, huma.Operation{OperationID: "generate-report", Method: http.MethodPost, Path: Prefix + "/orgs/{org_id}/reports/generate", Tags: []string{tag},
		Summary: "Génère (ou régénère) le rapport mensuel exécutif", DefaultStatus: http.StatusAccepted},
		func(ctx context.Context, in *CreateIn[ReportPeriodBody]) (*Empty, error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermReportsManage, plans.FeatureReports)
			if err != nil {
				return nil, err
			}
			if _, err := time.Parse("2006-01", in.Body.Period); err != nil {
				return nil, invalid("invalid period")
			}
			if err := s.Jobs.RequestReport(ctx, a.Org.ID, in.Body.Period); err != nil {
				return nil, s.fail(ctx, err)
			}
			s.audit(ctx, a, "report.generate", "report", in.Body.Period, nil)
			return &Empty{}, nil
		})
	type fileOut struct {
		ContentType        string `header:"Content-Type"`
		ContentDisposition string `header:"Content-Disposition"`
		Body               []byte
	}
	huma.Register(s.API, huma.Operation{OperationID: "download-report", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/reports/{id}/download", Tags: []string{tag},
		Summary: "Télécharge le PDF d'un rapport"},
		func(ctx context.Context, in *IDPath) (*fileOut, error) {
			ctx, _, err := s.enter(ctx, in.OrgID, auth.PermReportsRead, plans.FeatureReports)
			if err != nil {
				return nil, err
			}
			r, err := s.Store.Reports().Get(ctx, in.ID)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			if r.ObjectKey == "" {
				return nil, huma.Error409Conflict("report is not ready")
			}
			if !strings.HasPrefix(r.ObjectKey, objstore.Key(r.OrgID)+"/") {
				return nil, huma.Error404NotFound("report file not found")
			}
			data, err := s.Objects.Get(ctx, r.ObjectKey)
			if err != nil {
				if errors.Is(err, objstore.ErrNotFound) {
					return nil, huma.Error404NotFound("report file not found")
				}
				return nil, s.fail(ctx, err)
			}
			return &fileOut{ContentType: "application/pdf", ContentDisposition: `attachment; filename="kairn-rapport-` + r.Period + `.pdf"`, Body: data}, nil
		})

	// ---- Assistant IA (M-10) : relais SSE vers ai-service avec les droits de l'utilisateur.
	type chatIn struct {
		OrgPath
		Authorization string `header:"Authorization"`
		Cookie        string `cookie:"kairn_session"`
		Body          struct {
			Messages []struct {
				Role    string `json:"role" enum:"user,assistant" required:"true"`
				Content string `json:"content" required:"true" maxLength:"20000"`
			} `json:"messages" required:"true" minItems:"1" maxItems:"50"`
			Locale string `json:"locale,omitempty" enum:"fr,en"`
		}
	}
	huma.Register(s.API, huma.Operation{OperationID: "assistant-chat", Method: http.MethodPost, Path: Prefix + "/orgs/{org_id}/assistant/chat", Tags: []string{tag},
		Summary: "Assistant conversationnel (flux SSE) — les chiffres proviennent uniquement des outils"},
		func(ctx context.Context, in *chatIn) (*huma.StreamResponse, error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermAssistantUse, plans.FeatureAssistant)
			if err != nil {
				return nil, err
			}
			now := s.now()
			month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
			tot, err := s.Store.LLMUsage().Totals(ctx, month, month.AddDate(0, 1, 0))
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			if err := llmPolicy(a.Org, a.Limits, tot.InputTokens+tot.OutputTokens); err != nil {
				return nil, err
			}
			if s.Config.AIServiceURL == "" {
				return nil, huma.NewError(http.StatusServiceUnavailable, "the AI service is not configured")
			}
			userToken := strings.TrimPrefix(in.Authorization, "Bearer ")
			if userToken == "" {
				userToken = in.Cookie
			}
			payload, _ := json.Marshal(map[string]any{"org_id": a.Org.ID, "messages": in.Body.Messages, "locale": in.Body.Locale,
				"llm_provider": a.Org.Settings.LLMProvider, "plan": a.Org.Plan})
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.Config.AIServiceURL, "/")+"/v1/assistant/chat", bytes.NewReader(payload))
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "text/event-stream")
			req.Header.Set(ServiceTokenHeader, s.Config.ServiceToken)
			req.Header.Set("X-Kairn-User-Token", userToken)
			resp, err := s.HTTP.Do(req) //nolint:bodyclose // fermé par la fonction de flux ci-dessous (ou sur erreur)
			if err != nil {
				return nil, huma.NewError(http.StatusBadGateway, "AI service unreachable")
			}
			if resp.StatusCode != http.StatusOK {
				b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
				resp.Body.Close()
				return nil, huma.NewError(http.StatusBadGateway, "AI service error: "+string(b))
			}
			s.audit(ctx, a, "assistant.chat", "assistant", "", map[string]any{"messages": len(in.Body.Messages)})
			return &huma.StreamResponse{Body: func(hctx huma.Context) {
				defer resp.Body.Close()
				hctx.SetHeader("Content-Type", "text/event-stream")
				hctx.SetHeader("Cache-Control", "no-cache")
				hctx.SetHeader("X-Accel-Buffering", "no")
				w := hctx.BodyWriter()
				flusher, _ := w.(http.Flusher)
				rd := bufio.NewReader(resp.Body)
				for {
					line, err := rd.ReadBytes('\n')
					if len(line) > 0 {
						_, _ = w.Write(line)
						if flusher != nil && len(bytes.TrimSpace(line)) == 0 {
							flusher.Flush()
						}
					}
					if err != nil {
						if flusher != nil {
							flusher.Flush()
						}
						return
					}
				}
			}}, nil
		})

	type llmOut struct {
		Month       string          `json:"month"`
		Totals      store.LLMTotals `json:"totals"`
		QuotaTokens int             `json:"quota_tokens"`
		Provider    string          `json:"provider"`
		External    bool            `json:"allow_external_llm"`
		Currency    string          `json:"currency"`
	}
	huma.Register(s.API, huma.Operation{OperationID: "get-llm-usage", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/llm-usage", Tags: []string{tag},
		Summary: "Consommation LLM du mois et quota du plan"},
		func(ctx context.Context, in *OrgPath) (*Out[llmOut], error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermOrgRead, "")
			if err != nil {
				return nil, err
			}
			now := s.now()
			month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
			tot, err := s.Store.LLMUsage().Totals(ctx, month, month.AddDate(0, 1, 0))
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			p := a.Org.Settings.LLMProvider
			if p == "" {
				p = "anthropic"
			}
			return out(llmOut{Month: month.Format("2006-01"), Totals: tot, QuotaTokens: a.Limits.LLMTokensMonth, Provider: p,
				External: a.Org.Settings.AllowExternalLLM, Currency: a.Org.Currency}), nil
		})

	// ---- Exports planifiés (M-12)
	etag := "Exports et webhooks"
	registerCRUD(s, crudSpec[model.ExportJob, exportBody]{
		Tag: etag, Path: "/exports", Name: "export-job", Plural: "export-jobs", Summary: "exports planifiés vers S3",
		Read: auth.PermExport, Write: auth.PermExport, Feature: plans.FeatureExports, Repo: func() store.CRUD[model.ExportJob] { return s.Store.Exports() },
		ID: func(e *model.ExportJob) string { return e.ID },
		Apply: func(ctx context.Context, a access, b *exportBody, e *model.ExportJob, isNew bool) error {
			if b.Destination["bucket"] == "" || b.Destination["endpoint"] == "" {
				return invalid("destination requires endpoint and bucket")
			}
			if isNew {
				e.ID = newID()
				if b.Secrets["access_key"] == "" || b.Secrets["secret_key"] == "" {
					return invalid("secrets access_key and secret_key are required")
				}
			}
			e.OrgID, e.Name, e.Format, e.Destination, e.Schedule = a.Org.ID, b.Name, b.Format, b.Destination, b.Schedule
			e.Enabled = b.Enabled == nil || *b.Enabled
			if len(b.Secrets) > 0 {
				enc, err := s.Keyring.EncryptMap(ctx, b.Secrets, ExportSecretsAAD(e.OrgID, e.ID))
				if err != nil {
					return err
				}
				e.SecretsEnc = enc
			}
			return nil
		},
	})

	registerCRUD(s, crudSpec[model.WebhookSubscription, webhookBody]{
		Tag: etag, Path: "/webhooks", Name: "webhook", Plural: "webhooks", Summary: "webhooks sortants",
		Read: auth.PermOrgRead, Write: auth.PermOrgManage, Feature: plans.FeatureAPI, Repo: func() store.CRUD[model.WebhookSubscription] { return s.Store.Webhooks() },
		ID: func(w *model.WebhookSubscription) string { return w.ID },
		Apply: func(ctx context.Context, a access, b *webhookBody, w *model.WebhookSubscription, isNew bool) error {
			u, err := url.Parse(b.URL)
			if err != nil || u.Scheme != "https" || u.Host == "" {
				return invalid("webhook url must be https")
			}
			for _, e := range b.Events {
				if !containsStr(WebhookEvents, e) {
					return invalid("unknown event " + e)
				}
			}
			if isNew {
				w.ID = newID()
				if b.Secret == "" {
					return invalid("a signing secret is required")
				}
			}
			w.OrgID, w.URL, w.Events = a.Org.ID, b.URL, b.Events
			w.Enabled = b.Enabled == nil || *b.Enabled
			if b.Secret != "" {
				enc, err := s.Keyring.Encrypt(ctx, []byte(b.Secret), WebhookSecretAAD(w.OrgID, w.ID))
				if err != nil {
					return err
				}
				w.SecretEnc = enc
			}
			return nil
		},
	})
}
