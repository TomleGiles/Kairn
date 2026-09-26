package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/auth"
	"github.com/kairn-io/kairn/pkg/connector"
	"github.com/kairn-io/kairn/pkg/ids"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/objstore"
	"github.com/kairn-io/kairn/pkg/plans"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/tenancy"
)

func slugify(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) < 2 {
		s = "org-" + strings.ReplaceAll(ids.New()[:8], "-", "")
	}
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

type orgBody struct {
	Name     string `json:"name" required:"true" minLength:"2" maxLength:"120"`
	Slug     string `json:"slug,omitempty" pattern:"^[a-z0-9][a-z0-9-]{1,62}$"`
	Currency string `json:"currency,omitempty" enum:"EUR,USD,GBP,CHF"`
	Locale   string `json:"locale,omitempty" enum:"fr,en"`
	Timezone string `json:"timezone,omitempty"`
}

// CreateOrganization crée une organisation, son propriétaire, son abonnement
// d'essai et la racine de l'arbre d'allocation.
func (s *Server) CreateOrganization(ctx context.Context, owner model.User, b orgBody, parent *string, plan model.Plan) (model.Organization, error) {
	o := model.Organization{
		ID: ids.New(), ParentOrgID: parent, Name: strings.TrimSpace(b.Name), Slug: b.Slug, Plan: plan,
		Currency: b.Currency, Locale: b.Locale, Timezone: b.Timezone,
		Settings: model.OrgSettings{K8sAllocationMethod: "max", K8sIdleMode: "keep", RightsizingPercentile: 95, RightsizingWindowDays: 14},
	}
	if o.Slug == "" {
		o.Slug = slugify(o.Name)
	}
	if o.Currency == "" {
		o.Currency = "EUR"
	}
	if o.Locale == "" {
		o.Locale = "fr"
	}
	if o.Timezone == "" {
		o.Timezone = "Europe/Paris"
	}
	if _, err := time.LoadLocation(o.Timezone); err != nil {
		return o, invalid("unknown timezone")
	}
	trial := s.now().AddDate(0, 0, plans.TrialDays)
	if plan == model.PlanStarter {
		o.TrialEndsAt = &trial
	}
	octx := tenancy.WithOrg(ctx, o.ID)
	if owner.ID != "" {
		octx = tenancy.WithUser(octx, owner.ID)
	}
	err := s.Store.InTx(octx, func(ctx context.Context) error {
		if err := s.Store.Orgs().Create(ctx, &o); err != nil {
			return err
		}
		if owner.ID != "" {
			if err := s.Store.Memberships().Upsert(ctx, &model.Membership{OrgID: o.ID, UserID: owner.ID, Role: model.RoleOwner}); err != nil {
				return err
			}
		}
		sub := model.Subscription{OrgID: o.ID, Plan: plan, Status: "trialing", TrialEndsAt: o.TrialEndsAt}
		if parent != nil {
			sub.Status = "managed"
		}
		if err := s.Store.Subscriptions().Upsert(ctx, &sub); err != nil {
			return err
		}
		root := model.AllocationNode{ID: ids.New(), Kind: model.NodeOrganization, Name: o.Name}
		root.Path = "/" + root.ID + "/"
		return s.Store.AllocationNodes().Create(ctx, &root)
	})
	return o, err
}

func (s *Server) registerOrgs() {
	tag := "Organisations"
	huma.Register(s.API, huma.Operation{OperationID: "list-orgs", Method: http.MethodGet, Path: Prefix + "/orgs", Tags: []string{tag},
		Summary: "Organisations accessibles"},
		func(ctx context.Context, _ *struct{}) (*Out[[]model.Organization], error) {
			p, err := principal(ctx)
			if err != nil {
				return nil, err
			}
			switch p.Kind {
			case auth.KindToken:
				o, err := s.Store.Orgs().Get(tenancy.WithOrg(ctx, p.OrgID), p.OrgID)
				if err != nil {
					return nil, s.fail(ctx, err)
				}
				return out([]model.Organization{o}), nil
			case auth.KindUser:
				orgs, err := s.Store.Orgs().ListForUser(tenancy.WithUser(ctx, p.UserID), p.UserID)
				if err != nil {
					return nil, s.fail(ctx, err)
				}
				if orgs == nil {
					orgs = []model.Organization{}
				}
				return out(orgs), nil
			}
			return nil, huma.Error403Forbidden("not available for services")
		})

	huma.Register(s.API, huma.Operation{OperationID: "create-org", Method: http.MethodPost, Path: Prefix + "/orgs", Tags: []string{tag},
		Summary: "Crée une organisation (essai Team de 14 jours)", DefaultStatus: http.StatusCreated},
		func(ctx context.Context, in *struct{ Body orgBody }) (*Out[model.Organization], error) {
			p, err := userPrincipal(ctx)
			if err != nil {
				return nil, err
			}
			u, err := s.Store.Users().Get(tenancy.WithUser(ctx, p.UserID), p.UserID)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			o, err := s.CreateOrganization(ctx, u, in.Body, nil, model.PlanStarter)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			octx := tenancy.WithOrg(ctx, o.ID)
			s.audit(octx, access{P: p, Org: o}, "org.create", "organization", o.ID, map[string]any{"name": o.Name})
			return out(o), nil
		})

	huma.Register(s.API, huma.Operation{OperationID: "get-org", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}", Tags: []string{tag},
		Summary: "Détail d'une organisation"},
		func(ctx context.Context, in *OrgPath) (*Out[model.Organization], error) {
			_, a, err := s.enter(ctx, in.OrgID, auth.PermOrgRead, "")
			if err != nil {
				return nil, err
			}
			return out(a.Org), nil
		})

	type patchOrg struct {
		OrgPath
		Body struct {
			Name     *string            `json:"name,omitempty" minLength:"2" maxLength:"120"`
			Currency *string            `json:"currency,omitempty" enum:"EUR,USD,GBP,CHF"`
			Locale   *string            `json:"locale,omitempty" enum:"fr,en"`
			Timezone *string            `json:"timezone,omitempty"`
			VATRate  *decimal.Decimal   `json:"vat_rate,omitempty"`
			Settings *model.OrgSettings `json:"settings,omitempty" doc:"Fusion partielle : seules les clés fournies sont modifiées"`
		}
		RawBody []byte
	}
	huma.Register(s.API, huma.Operation{OperationID: "update-org", Method: http.MethodPatch, Path: Prefix + "/orgs/{org_id}", Tags: []string{tag},
		Summary: "Met à jour les réglages de l'organisation"},
		func(ctx context.Context, in *patchOrg) (*Out[model.Organization], error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermOrgManage, "")
			if err != nil {
				return nil, err
			}
			o := a.Org
			b := in.Body
			if b.Name != nil {
				o.Name = strings.TrimSpace(*b.Name)
			}
			if b.Currency != nil {
				o.Currency = *b.Currency
			}
			if b.Locale != nil {
				o.Locale = *b.Locale
			}
			if b.Timezone != nil {
				if _, err := time.LoadLocation(*b.Timezone); err != nil {
					return nil, invalid("unknown timezone")
				}
				o.Timezone = *b.Timezone
			}
			if b.VATRate != nil {
				if b.VATRate.IsNegative() || b.VATRate.GreaterThan(decimal.NewFromInt(1)) {
					return nil, invalid("vat_rate must be between 0 and 1")
				}
				v := *b.VATRate
				o.VATRate = &v
			}
			if b.Settings != nil {
				st, err := mergeSettings(o.Settings, in.RawBody)
				if err != nil {
					return nil, invalid("invalid settings")
				}
				if st.SSOIdPAlias != "" && !a.Limits.Allows(plans.FeatureSSO) {
					return nil, huma.NewError(http.StatusPaymentRequired, "SSO is not included in this plan")
				}
				if st.WhiteLabel != nil && !a.Limits.Allows(plans.FeatureWhiteLabel) {
					return nil, huma.NewError(http.StatusPaymentRequired, "white label is not included in this plan")
				}
				switch st.LLMProvider {
				case "", "none", "anthropic", "mistral", "local":
				default:
					return nil, invalid("unknown llm_provider")
				}
				if (st.LLMProvider == "mistral" || st.LLMProvider == "local") && !a.Limits.Allows(plans.FeatureSovereignLLM) && st.LLMProvider != o.Settings.LLMProvider {
					return nil, huma.NewError(http.StatusPaymentRequired, "sovereign LLM is not included in this plan")
				}
				for _, r := range st.ReportRecipients {
					if _, err := mail.ParseAddress(r); err != nil {
						return nil, invalid("invalid report recipient " + r)
					}
				}
				o.Settings = st
			}
			if err := s.Store.Orgs().Update(ctx, &o); err != nil {
				return nil, s.fail(ctx, err)
			}
			s.audit(ctx, a, "org.update", "organization", o.ID, nil)
			return out(o), nil
		})

	// ---- Droit à l'effacement (RGPD) : suppression définitive de l'organisation et de toutes ses données.
	huma.Register(s.API, huma.Operation{OperationID: "delete-org", Method: http.MethodDelete, Path: Prefix + "/orgs/{org_id}", Tags: []string{tag},
		Summary:       "Supprime définitivement l'organisation et toutes ses données (propriétaire uniquement)",
		Description:   "Efface les données relationnelles, les séries (métriques, coûts, événements) et les fichiers (rapports, exports). Irréversible. Le corps doit reprendre le slug de l'organisation.",
		DefaultStatus: http.StatusNoContent},
		func(ctx context.Context, in *struct {
			OrgPath
			Body struct {
				Confirm string `json:"confirm" required:"true" doc:"Slug de l'organisation, pour confirmation"`
			}
		}) (*Empty, error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermOrgManage, "")
			if err != nil {
				return nil, err
			}
			if a.Role != model.RoleOwner || a.P.Kind != auth.KindUser {
				return nil, huma.Error403Forbidden("only an owner (interactive session) can delete the organization")
			}
			if in.Body.Confirm != a.Org.Slug {
				return nil, invalid("confirmation does not match the organization slug")
			}
			if kids, err := s.Store.Orgs().ListChildren(ctx); err != nil {
				return nil, s.fail(ctx, err)
			} else if len(kids) > 0 {
				return nil, huma.Error409Conflict("delete or detach the client organizations first")
			}
			// Séries et fichiers d'abord : si l'une de ces étapes échoue, l'organisation
			// existe encore et la suppression peut être relancée.
			if err := s.TSDB.PurgeOrg(ctx); err != nil {
				return nil, s.fail(ctx, err)
			}
			if err := s.Objects.DeletePrefix(ctx, objstore.Key(a.Org.ID)); err != nil {
				return nil, s.fail(ctx, err)
			}
			if err := s.Store.Orgs().Delete(ctx, a.Org.ID); err != nil {
				return nil, s.fail(ctx, err)
			}
			// Le journal d'audit de l'organisation disparaît avec elle : trace minimale dans les logs de la plateforme.
			s.Log.Info("organization deleted", "org", a.Org.ID, "actor", a.P.UserID)
			return &Empty{}, nil
		})

	// ---- MSP : organisations clientes
	huma.Register(s.API, huma.Operation{OperationID: "list-client-orgs", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/clients", Tags: []string{"MSP"},
		Summary: "Organisations clientes gérées"},
		func(ctx context.Context, in *OrgPath) (*Out[[]model.Organization], error) {
			ctx, _, err := s.enter(ctx, in.OrgID, auth.PermOrgRead, plans.FeatureMSP)
			if err != nil {
				return nil, err
			}
			orgs, err := s.Store.Orgs().ListChildren(ctx)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			if orgs == nil {
				orgs = []model.Organization{}
			}
			return out(orgs), nil
		})
	huma.Register(s.API, huma.Operation{OperationID: "create-client-org", Method: http.MethodPost, Path: Prefix + "/orgs/{org_id}/clients", Tags: []string{"MSP"},
		Summary: "Crée une organisation cliente", DefaultStatus: http.StatusCreated},
		func(ctx context.Context, in *struct {
			OrgPath
			Body orgBody
		}) (*Out[model.Organization], error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermOrgManage, plans.FeatureMSP)
			if err != nil {
				return nil, err
			}
			parent := a.Org.ID
			o, err := s.CreateOrganization(ctx, model.User{}, in.Body, &parent, model.PlanTeam)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			s.audit(ctx, a, "msp.client.create", "organization", o.ID, map[string]any{"name": o.Name})
			return out(o), nil
		})

	s.registerMembers()
	s.registerTokens()
	s.registerAudit()
	s.registerBilling()
}

// ------------------------------------------------------------------ membres

func (s *Server) registerMembers() {
	tag := "Membres"
	huma.Register(s.API, huma.Operation{OperationID: "list-members", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/members", Tags: []string{tag},
		Summary: "Membres de l'organisation"},
		func(ctx context.Context, in *OrgPath) (*Out[[]model.Membership], error) {
			ctx, _, err := s.enter(ctx, in.OrgID, auth.PermOrgRead, "")
			if err != nil {
				return nil, err
			}
			ms, err := s.Store.Memberships().List(ctx)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			if ms == nil {
				ms = []model.Membership{}
			}
			return out(ms), nil
		})

	type memberBody struct {
		Email  string     `json:"email,omitempty" format:"email"`
		Role   model.Role `json:"role" required:"true" enum:"owner,admin,finance,engineer,viewer"`
		Scopes []string   `json:"scopes,omitempty" doc:"Nœuds d'allocation visibles (vide = toute l'organisation)"`
	}
	checkGrant := func(a access, role model.Role) error {
		if role == model.RoleOwner && a.Role != model.RoleOwner {
			return huma.Error403Forbidden("only an owner can grant the owner role")
		}
		return nil
	}
	huma.Register(s.API, huma.Operation{OperationID: "add-member", Method: http.MethodPost, Path: Prefix + "/orgs/{org_id}/members", Tags: []string{tag},
		Summary: "Ajoute ou invite un membre", DefaultStatus: http.StatusCreated},
		func(ctx context.Context, in *struct {
			OrgPath
			Body memberBody
		}) (*Out[model.Membership], error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermMembersManage, "")
			if err != nil {
				return nil, err
			}
			if err := checkGrant(a, in.Body.Role); err != nil {
				return nil, err
			}
			if _, err := mail.ParseAddress(in.Body.Email); err != nil {
				return nil, invalid("invalid email")
			}
			ms, err := s.Store.Memberships().List(ctx)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			if !plans.Within(a.Limits.MaxUsers, len(ms)+1) {
				return nil, huma.NewError(http.StatusPaymentRequired, fmt.Sprintf("plan limit reached (%d users)", a.Limits.MaxUsers))
			}
			u, err := s.upsertUser(ctx, "", in.Body.Email, "")
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			m := model.Membership{OrgID: a.Org.ID, UserID: u.ID, Role: in.Body.Role, Scopes: in.Body.Scopes}
			if err := s.Store.Memberships().Upsert(ctx, &m); err != nil {
				return nil, s.fail(ctx, err)
			}
			m.Email, m.Name = u.Email, u.Name
			s.audit(ctx, a, "member.add", "user", u.ID, map[string]any{"role": m.Role})
			return out(m), nil
		})

	type memberPath struct {
		OrgID  string `path:"org_id"`
		UserID string `path:"user_id"`
	}
	ownersLeft := func(ctx context.Context, excluding string) (int, error) {
		ms, err := s.Store.Memberships().List(ctx)
		if err != nil {
			return 0, err
		}
		n := 0
		for _, m := range ms {
			if m.Role == model.RoleOwner && m.UserID != excluding {
				n++
			}
		}
		return n, nil
	}
	huma.Register(s.API, huma.Operation{OperationID: "update-member", Method: http.MethodPut, Path: Prefix + "/orgs/{org_id}/members/{user_id}", Tags: []string{tag},
		Summary: "Modifie le rôle ou les scopes d'un membre"},
		func(ctx context.Context, in *struct {
			memberPath
			Body memberBody
		}) (*Out[model.Membership], error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermMembersManage, "")
			if err != nil {
				return nil, err
			}
			if err := checkGrant(a, in.Body.Role); err != nil {
				return nil, err
			}
			m, err := s.Store.Memberships().Get(ctx, in.UserID)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			if m.Role == model.RoleOwner && in.Body.Role != model.RoleOwner {
				if a.Role != model.RoleOwner {
					return nil, huma.Error403Forbidden("only an owner can change an owner")
				}
				if n, err := ownersLeft(ctx, m.UserID); err != nil || n == 0 {
					return nil, huma.Error409Conflict("an organization must keep at least one owner")
				}
			}
			m.Role, m.Scopes = in.Body.Role, in.Body.Scopes
			if err := s.Store.Memberships().Upsert(ctx, &m); err != nil {
				return nil, s.fail(ctx, err)
			}
			s.audit(ctx, a, "member.update", "user", m.UserID, map[string]any{"role": m.Role, "scopes": m.Scopes})
			return out(m), nil
		})
	huma.Register(s.API, huma.Operation{OperationID: "remove-member", Method: http.MethodDelete, Path: Prefix + "/orgs/{org_id}/members/{user_id}", Tags: []string{tag},
		Summary: "Retire un membre", DefaultStatus: http.StatusNoContent},
		func(ctx context.Context, in *memberPath) (*Empty, error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermMembersManage, "")
			if err != nil {
				return nil, err
			}
			m, err := s.Store.Memberships().Get(ctx, in.UserID)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			if m.Role == model.RoleOwner {
				if a.Role != model.RoleOwner {
					return nil, huma.Error403Forbidden("only an owner can remove an owner")
				}
				if n, err := ownersLeft(ctx, m.UserID); err != nil || n == 0 {
					return nil, huma.Error409Conflict("an organization must keep at least one owner")
				}
			}
			if err := s.Store.Memberships().Delete(ctx, in.UserID); err != nil {
				return nil, s.fail(ctx, err)
			}
			s.audit(ctx, a, "member.remove", "user", in.UserID, nil)
			return &Empty{}, nil
		})
}

// ------------------------------------------------------------------ jetons d'API

type createdToken struct {
	model.APIToken
	Token string `json:"token" doc:"Valeur du jeton, affichée une seule fois"`
}

func (s *Server) registerTokens() {
	tag := "Jetons d'API"
	huma.Register(s.API, huma.Operation{OperationID: "list-tokens", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/tokens", Tags: []string{tag},
		Summary: "Jetons d'API"},
		func(ctx context.Context, in *OrgPath) (*Out[[]model.APIToken], error) {
			ctx, _, err := s.enter(ctx, in.OrgID, auth.PermTokensManage, plans.FeatureAPI)
			if err != nil {
				return nil, err
			}
			ts, err := s.Store.Tokens().List(ctx)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			if ts == nil {
				ts = []model.APIToken{}
			}
			return out(ts), nil
		})
	type tokenBody struct {
		Name          string     `json:"name" required:"true" minLength:"1" maxLength:"80"`
		Role          model.Role `json:"role" required:"true" enum:"admin,finance,engineer,viewer"`
		Scopes        []string   `json:"scopes,omitempty" doc:"Permissions accordées (vide = toutes celles du rôle)"`
		ExpiresInDays int        `json:"expires_in_days,omitempty" minimum:"0" maximum:"730"`
	}
	huma.Register(s.API, huma.Operation{OperationID: "create-token", Method: http.MethodPost, Path: Prefix + "/orgs/{org_id}/tokens", Tags: []string{tag},
		Summary: "Crée un jeton d'API scoppé", DefaultStatus: http.StatusCreated},
		func(ctx context.Context, in *struct {
			OrgPath
			Body tokenBody
		}) (*Out[createdToken], error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermTokensManage, plans.FeatureAPI)
			if err != nil {
				return nil, err
			}
			if auth.RoleRank(in.Body.Role) < auth.RoleRank(a.Role) {
				return nil, huma.Error403Forbidden("cannot create a token more privileged than yourself")
			}
			if !auth.ValidScopes(in.Body.Scopes) {
				return nil, invalid("unknown scope")
			}
			plain, prefix, hash, err := auth.NewAPIToken()
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			t := model.APIToken{OrgID: a.Org.ID, Name: in.Body.Name, Prefix: prefix, Hash: hash, Role: in.Body.Role, Scopes: in.Body.Scopes}
			if t.Scopes == nil {
				t.Scopes = []string{}
			}
			if a.P.Kind == auth.KindUser {
				uid := a.P.UserID
				t.CreatedBy = &uid
			}
			if in.Body.ExpiresInDays > 0 {
				exp := s.now().AddDate(0, 0, in.Body.ExpiresInDays)
				t.ExpiresAt = &exp
			}
			if err := s.Store.Tokens().Create(ctx, &t); err != nil {
				return nil, s.fail(ctx, err)
			}
			s.audit(ctx, a, "token.create", "api_token", t.ID, map[string]any{"name": t.Name, "role": t.Role})
			return out(createdToken{APIToken: t, Token: plain}), nil
		})
	huma.Register(s.API, huma.Operation{OperationID: "revoke-token", Method: http.MethodDelete, Path: Prefix + "/orgs/{org_id}/tokens/{id}", Tags: []string{tag},
		Summary: "Révoque un jeton", DefaultStatus: http.StatusNoContent},
		func(ctx context.Context, in *IDPath) (*Empty, error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermTokensManage, "")
			if err != nil {
				return nil, err
			}
			if err := s.Store.Tokens().Revoke(ctx, in.ID, s.now()); err != nil {
				return nil, s.fail(ctx, err)
			}
			s.audit(ctx, a, "token.revoke", "api_token", in.ID, nil)
			return &Empty{}, nil
		})
}

// ------------------------------------------------------------------ audit

type auditIn struct {
	OrgPath
	From   time.Time `query:"from"`
	To     time.Time `query:"to"`
	Action string    `query:"action"`
	Actor  string    `query:"actor"`
	Cursor string    `query:"cursor"`
	Limit  int       `query:"limit" minimum:"0" maximum:"500"`
}

func (s *Server) registerAudit() {
	tag := "Audit"
	huma.Register(s.API, huma.Operation{OperationID: "list-audit-events", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/audit", Tags: []string{tag},
		Summary: "Journal d'audit"},
		func(ctx context.Context, in *auditIn) (*Out[Page[model.AuditEvent]], error) {
			ctx, _, err := s.enter(ctx, in.OrgID, auth.PermAuditRead, "")
			if err != nil {
				return nil, err
			}
			lim := store.ListQuery{Limit: in.Limit}.Normalize().Limit
			evs, err := s.Store.Audit().List(ctx, store.AuditFilter{From: in.From, To: in.To, Action: in.Action, ActorID: in.Actor, Cursor: in.Cursor, Limit: lim})
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			return out(page(evs, lim, func(e model.AuditEvent) string { return e.ID })), nil
		})
	// Champs à plat (voir export-costs) : pas de structure imbriquée sur deux niveaux.
	type exportIn struct {
		OrgPath
		From   time.Time `query:"from"`
		To     time.Time `query:"to"`
		Action string    `query:"action"`
		Actor  string    `query:"actor"`
		Format string    `query:"format" enum:"csv,json" default:"csv"`
	}
	type fileOut struct {
		ContentType        string `header:"Content-Type"`
		ContentDisposition string `header:"Content-Disposition"`
		Body               []byte
	}
	huma.Register(s.API, huma.Operation{OperationID: "export-audit-events", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/audit/export", Tags: []string{tag},
		Summary: "Exporte le journal d'audit (CSV ou JSON)"},
		func(ctx context.Context, in *exportIn) (*fileOut, error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermAuditRead, "")
			if err != nil {
				return nil, err
			}
			var all []model.AuditEvent
			cursor := ""
			for {
				evs, err := s.Store.Audit().List(ctx, store.AuditFilter{From: in.From, To: in.To, Action: in.Action, ActorID: in.Actor, Cursor: cursor, Limit: store.MaxLimit})
				if err != nil {
					return nil, s.fail(ctx, err)
				}
				all = append(all, evs...)
				if len(evs) < store.MaxLimit || len(all) >= 100_000 {
					break
				}
				cursor = evs[len(evs)-1].ID
			}
			s.audit(ctx, a, "audit.export", "audit", "", map[string]any{"count": len(all), "format": in.Format})
			name := "kairn-audit-" + s.now().Format("20060102")
			if in.Format == "json" {
				b, _ := json.MarshalIndent(all, "", "  ")
				return &fileOut{ContentType: "application/json", ContentDisposition: `attachment; filename="` + name + `.json"`, Body: b}, nil
			}
			var buf bytes.Buffer
			w := csv.NewWriter(&buf)
			_ = w.Write([]string{"at", "actor_type", "actor_id", "action", "target_type", "target_id", "ip", "details"})
			for _, e := range all {
				d, _ := json.Marshal(e.Details)
				_ = w.Write([]string{e.At.Format(time.RFC3339), e.ActorType, e.ActorID, e.Action, e.TargetType, e.TargetID, e.IP, string(d)})
			}
			w.Flush()
			return &fileOut{ContentType: "text/csv; charset=utf-8", ContentDisposition: `attachment; filename="` + name + `.csv"`, Body: buf.Bytes()}, nil
		})
}

// ------------------------------------------------------------------ facturation SaaS (M-13)

type billingBody struct {
	Plan         model.Plan         `json:"plan"`
	Limits       plans.Limits       `json:"limits"`
	Subscription model.Subscription `json:"subscription"`
	Usage        struct {
		Users          int `json:"users"`
		Connectors     int `json:"connectors"`
		CloudProviders int `json:"cloud_providers"`
		LLMTokensMonth int `json:"llm_tokens_month"`
	} `json:"usage"`
	TrialEndsAt *time.Time `json:"trial_ends_at,omitempty"`
}

func (s *Server) billingState(ctx context.Context, a access) (billingBody, error) {
	b := billingBody{Plan: a.Org.Plan, Limits: a.Limits, TrialEndsAt: a.Org.TrialEndsAt}
	sub, err := s.Store.Subscriptions().Get(ctx)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return b, err
	}
	b.Subscription = sub
	ms, err := s.Store.Memberships().List(ctx)
	if err != nil {
		return b, err
	}
	b.Usage.Users = len(ms)
	conns, err := store.ListAll(ctx, s.Store.Connectors(), func(c model.Connector) string { return c.ID }, nil)
	if err != nil {
		return b, err
	}
	b.Usage.Connectors = len(conns)
	providers := map[string]bool{}
	for _, c := range conns {
		if info, ok := connector.Info(c.Type); ok && info.Category == connector.CategoryCloud {
			providers[info.Provider] = true
		}
	}
	b.Usage.CloudProviders = len(providers)
	now := s.now()
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	tot, err := s.Store.LLMUsage().Totals(ctx, month, month.AddDate(0, 1, 0))
	if err != nil {
		return b, err
	}
	b.Usage.LLMTokensMonth = tot.InputTokens + tot.OutputTokens
	return b, nil
}

func (s *Server) registerBilling() {
	tag := "Facturation"
	huma.Register(s.API, huma.Operation{OperationID: "get-billing", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/billing", Tags: []string{tag},
		Summary: "Plan, limites et consommation"},
		func(ctx context.Context, in *OrgPath) (*Out[billingBody], error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermOrgRead, "")
			if err != nil {
				return nil, err
			}
			b, err := s.billingState(ctx, a)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			return out(b), nil
		})
	type urlBody struct {
		URL string `json:"url"`
	}
	huma.Register(s.API, huma.Operation{OperationID: "create-checkout", Method: http.MethodPost, Path: Prefix + "/orgs/{org_id}/billing/checkout", Tags: []string{tag},
		Summary: "Crée une session de paiement Stripe pour changer de plan"},
		func(ctx context.Context, in *struct {
			OrgPath
			Body struct {
				Plan model.Plan `json:"plan" required:"true" enum:"starter,team"`
			}
		}) (*Out[urlBody], error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermBillingManage, "")
			if err != nil {
				return nil, err
			}
			price := s.Config.StripePrices[string(in.Body.Plan)]
			if s.Config.StripeSecretKey == "" || price == "" {
				return nil, huma.NewError(http.StatusServiceUnavailable, "online billing is not configured; contact sales")
			}
			form := url.Values{
				"mode": {"subscription"}, "line_items[0][price]": {price}, "line_items[0][quantity]": {"1"},
				"client_reference_id": {a.Org.ID}, "metadata[org_id]": {a.Org.ID}, "metadata[plan]": {string(in.Body.Plan)},
				"subscription_data[metadata][org_id]": {a.Org.ID}, "subscription_data[metadata][plan]": {string(in.Body.Plan)},
				"success_url": {s.Config.PublicURL + "/settings/billing?status=success"},
				"cancel_url":  {s.Config.PublicURL + "/settings/billing?status=cancel"},
			}
			if sub, err := s.Store.Subscriptions().Get(ctx); err == nil && sub.StripeCustomerID != nil {
				form.Set("customer", *sub.StripeCustomerID)
			}
			var res struct {
				URL string `json:"url"`
			}
			if err := s.stripe(ctx, "/v1/checkout/sessions", form, &res); err != nil {
				return nil, s.fail(ctx, err)
			}
			s.audit(ctx, a, "billing.checkout", "subscription", a.Org.ID, map[string]any{"plan": in.Body.Plan})
			return out(urlBody{URL: res.URL}), nil
		})
	huma.Register(s.API, huma.Operation{OperationID: "create-billing-portal", Method: http.MethodPost, Path: Prefix + "/orgs/{org_id}/billing/portal", Tags: []string{tag},
		Summary: "Ouvre le portail client Stripe (factures, moyen de paiement)"},
		func(ctx context.Context, in *OrgPath) (*Out[urlBody], error) {
			ctx, _, err := s.enter(ctx, in.OrgID, auth.PermBillingManage, "")
			if err != nil {
				return nil, err
			}
			sub, err := s.Store.Subscriptions().Get(ctx)
			if err != nil || sub.StripeCustomerID == nil || s.Config.StripeSecretKey == "" {
				return nil, huma.Error409Conflict("no billing account yet")
			}
			var res struct {
				URL string `json:"url"`
			}
			if err := s.stripe(ctx, "/v1/billing_portal/sessions", url.Values{"customer": {*sub.StripeCustomerID}, "return_url": {s.Config.PublicURL + "/settings/billing"}}, &res); err != nil {
				return nil, s.fail(ctx, err)
			}
			return out(urlBody{URL: res.URL}), nil
		})
	huma.Register(s.API, huma.Operation{OperationID: "stripe-webhook", Method: http.MethodPost, Path: Prefix + "/billing/stripe/webhook", Tags: []string{tag},
		Summary: "Webhook Stripe (signé)", Security: []map[string][]string{}, DefaultStatus: http.StatusNoContent},
		func(ctx context.Context, in *struct {
			Signature string `header:"Stripe-Signature"`
			RawBody   []byte
		}) (*Empty, error) {
			if err := verifyStripeSignature(in.RawBody, in.Signature, s.Config.StripeWebhookSecret, s.now()); err != nil {
				return nil, huma.Error400BadRequest("invalid signature")
			}
			if err := s.handleStripeEvent(ctx, in.RawBody); err != nil {
				return nil, s.fail(ctx, err)
			}
			return &Empty{}, nil
		})
}

func (s *Server) stripe(ctx context.Context, path string, form url.Values, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.stripe.com"+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.SetBasicAuth(s.Config.StripeSecretKey, "")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("stripe: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("stripe %s: status %d: %s", path, resp.StatusCode, b)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// verifyStripeSignature vérifie l'en-tête Stripe-Signature (t=…,v1=…) avec une tolérance de 5 minutes.
func verifyStripeSignature(payload []byte, header, secret string, now time.Time) error {
	if secret == "" {
		return errors.New("webhook secret not configured")
	}
	var ts string
	var sigs []string
	for _, part := range strings.Split(header, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch k {
		case "t":
			ts = v
		case "v1":
			sigs = append(sigs, v)
		}
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || len(sigs) == 0 {
		return errors.New("malformed signature header")
	}
	if d := now.Sub(time.Unix(sec, 0)); d > 5*time.Minute || d < -5*time.Minute {
		return errors.New("signature timestamp out of tolerance")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(payload)
	want := hex.EncodeToString(mac.Sum(nil))
	for _, s := range sigs {
		if hmac.Equal([]byte(s), []byte(want)) {
			return nil
		}
	}
	return errors.New("signature mismatch")
}

func (s *Server) handleStripeEvent(ctx context.Context, raw []byte) error {
	var ev struct {
		Type string `json:"type"`
		Data struct {
			Object struct {
				ID                string            `json:"id"`
				Customer          string            `json:"customer"`
				Subscription      string            `json:"subscription"`
				Status            string            `json:"status"`
				ClientReferenceID string            `json:"client_reference_id"`
				Metadata          map[string]string `json:"metadata"`
				CurrentPeriodEnd  int64             `json:"current_period_end"`
			} `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil {
		return huma.Error400BadRequest("invalid event")
	}
	obj := ev.Data.Object
	orgID := obj.Metadata["org_id"]
	if orgID == "" {
		orgID = obj.ClientReferenceID
	}
	if orgID == "" && obj.Customer != "" {
		orgID, _ = s.Store.System().FindOrgByStripeCustomer(ctx, obj.Customer)
	}
	if !ids.Valid(orgID) {
		return nil // événement sans rapport avec une organisation connue
	}
	octx := tenancy.WithOrg(ctx, orgID)
	org, err := s.Store.Orgs().Get(octx, orgID)
	if err != nil {
		return nil //nolint:nilerr // organisation supprimée entre-temps : l'événement Stripe est acquitté
	}
	sub, _ := s.Store.Subscriptions().Get(octx)
	sub.OrgID = orgID
	plan := model.Plan(obj.Metadata["plan"])
	switch ev.Type {
	case "checkout.session.completed":
		if obj.Customer != "" {
			c := obj.Customer
			sub.StripeCustomerID = &c
		}
		if obj.Subscription != "" {
			id := obj.Subscription
			sub.StripeSubscriptionID = &id
		}
		sub.Status = "active"
	case "customer.subscription.updated", "customer.subscription.created":
		sub.Status = obj.Status
		if obj.CurrentPeriodEnd > 0 {
			t := time.Unix(obj.CurrentPeriodEnd, 0).UTC()
			sub.CurrentPeriodEnd = &t
		}
	case "customer.subscription.deleted":
		sub.Status = "canceled"
		plan = model.PlanStarter
	default:
		return nil
	}
	if plan != "" {
		if _, ok := plans.Get(plan); ok {
			sub.Plan = plan
			org.Plan = plan
			org.TrialEndsAt = nil
			if err := s.Store.Orgs().Update(octx, &org); err != nil {
				return err
			}
		}
	}
	if err := s.Store.Subscriptions().Upsert(octx, &sub); err != nil {
		return err
	}
	s.audit(octx, access{P: auth.Principal{Kind: auth.KindService}, Org: org}, "billing.stripe."+ev.Type, "subscription", orgID, map[string]any{"plan": sub.Plan, "status": sub.Status})
	return nil
}

// mergeSettings applique une fusion partielle (JSON merge patch, RFC 7396, au
// premier niveau) des réglages envoyés sur les réglages courants : une clé
// absente conserve sa valeur.
func mergeSettings(cur model.OrgSettings, rawBody []byte) (model.OrgSettings, error) {
	var body struct {
		Settings map[string]json.RawMessage `json:"settings"`
	}
	if err := json.Unmarshal(rawBody, &body); err != nil {
		return cur, err
	}
	base, err := json.Marshal(cur)
	if err != nil {
		return cur, err
	}
	merged := map[string]json.RawMessage{}
	if err := json.Unmarshal(base, &merged); err != nil {
		return cur, err
	}
	for k, v := range body.Settings {
		if string(v) == "null" {
			delete(merged, k)
			continue
		}
		merged[k] = v
	}
	raw, err := json.Marshal(merged)
	if err != nil {
		return cur, err
	}
	var out model.OrgSettings
	if err := json.Unmarshal(raw, &out); err != nil {
		return cur, err
	}
	return out, nil
}
