package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/danielgtaylor/huma/v2"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"

	"github.com/kairn-io/kairn/pkg/auth"
	"github.com/kairn-io/kairn/pkg/ids"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/tenancy"
)

// OIDC gère l'authentification déléguée (Keycloak, ou tout IdP OIDC).
// SAML, MFA et fédération d'annuaires sont portés par Keycloak.
type OIDC struct {
	provider *oidc.Provider
	idVerif  *oidc.IDTokenVerifier
	atVerif  *oidc.IDTokenVerifier
	oauth    oauth2.Config
}

// NewOIDC découvre le fournisseur OIDC.
func NewOIDC(ctx context.Context, issuer, clientID, clientSecret, redirectURL string) (*OIDC, error) {
	p, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	return &OIDC{
		provider: p,
		idVerif:  p.Verifier(&oidc.Config{ClientID: clientID}),
		atVerif:  p.Verifier(&oidc.Config{SkipClientIDCheck: true}),
		oauth: oauth2.Config{
			ClientID: clientID, ClientSecret: clientSecret, RedirectURL: redirectURL,
			Endpoint: p.Endpoint(), Scopes: []string{oidc.ScopeOpenID, "profile", "email"},
		},
	}, nil
}

type oidcClaims struct {
	Email    string `json:"email"`
	Verified bool   `json:"email_verified"`
	Name     string `json:"name"`
	Nonce    string `json:"nonce"`
}

// VerifyAccessToken authentifie un jeton d'accès OIDC (clients API, CLI).
func (o *OIDC) VerifyAccessToken(ctx context.Context, st store.Store, raw string) (auth.Principal, error) {
	tok, err := o.atVerif.Verify(ctx, raw)
	if err != nil {
		return auth.Principal{}, auth.ErrInvalidToken
	}
	var c oidcClaims
	_ = tok.Claims(&c)
	u, err := st.System().FindUser(ctx, tok.Subject, "")
	if err != nil {
		return auth.Principal{}, auth.ErrInvalidToken
	}
	return auth.Principal{Kind: auth.KindUser, UserID: u.ID, Email: u.Email, Name: u.Name}, nil
}

type flowClaims struct {
	State    string `json:"st"`
	Nonce    string `json:"nn"`
	Verifier string `json:"cv"`
	Redirect string `json:"rd"`
	jwt.RegisteredClaims
}

const flowCookie = "kairn_oidc"

// safeRedirect n'accepte que des chemins relatifs à l'application.
func safeRedirect(p string) string {
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.Contains(p, "\\") {
		return "/"
	}
	return p
}

func (s *Server) sessionCookie(tok string, exp time.Time) http.Cookie {
	//nolint:gosec // G124 : HttpOnly et SameSite fixés ; Secure dès que l'URL publique est en HTTPS (HTTP seulement en développement local)
	return http.Cookie{
		Name: SessionCookie, Value: tok, Path: "/", Expires: exp, HttpOnly: true,
		Secure: strings.HasPrefix(s.Config.PublicURL, "https://"), SameSite: http.SameSiteLaxMode,
	}
}

// upsertUser retrouve ou crée l'utilisateur correspondant à une identité.
func (s *Server) upsertUser(ctx context.Context, subject, email, name string) (model.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	u, err := s.Store.System().FindUser(ctx, subject, email)
	switch {
	case err == nil:
		uctx := tenancy.WithUser(ctx, u.ID)
		changed := false
		if subject != "" && (u.OIDCSubject == nil || *u.OIDCSubject != subject) {
			u.OIDCSubject, changed = &subject, true
		}
		if name != "" && u.Name != name {
			u.Name, changed = name, true
		}
		if changed {
			if err := s.Store.Users().Update(uctx, &u); err != nil {
				return u, err
			}
		}
		return u, nil
	case errors.Is(err, store.ErrNotFound):
		u = model.User{ID: ids.New(), Email: email, Name: name, Locale: "fr", CreatedAt: s.now()}
		if subject != "" {
			u.OIDCSubject = &subject
		}
		if u.Name == "" {
			u.Name = strings.Split(email, "@")[0]
		}
		return u, s.Store.Users().Create(tenancy.WithUser(ctx, u.ID), &u)
	}
	return model.User{}, err
}

// DemoOrgHook est appelé après une connexion en mode démo (rattachement à l'organisation de démo).
var DemoOrgHook func(ctx context.Context, u model.User) error

type meBody struct {
	User        model.User          `json:"user"`
	Memberships []membershipWithOrg `json:"memberships"`
	Permissions map[string][]string `json:"permissions"`
}

type membershipWithOrg struct {
	model.Membership
	OrgName string     `json:"org_name"`
	OrgSlug string     `json:"org_slug"`
	Plan    model.Plan `json:"plan"`
	Parent  *string    `json:"parent_org_id,omitempty"`
}

func (s *Server) registerAuth() {
	type loginIn struct {
		Body struct {
			Email string `json:"email" required:"true" format:"email" maxLength:"254"`
			Name  string `json:"name,omitempty" maxLength:"120"`
		}
	}
	type sessionOut struct {
		SetCookie http.Cookie `header:"Set-Cookie"`
		Body      struct {
			Token     string     `json:"token"`
			ExpiresAt time.Time  `json:"expires_at"`
			User      model.User `json:"user"`
		}
	}
	huma.Register(s.API, huma.Operation{
		OperationID: "dev-login", Method: http.MethodPost, Path: Prefix + "/auth/login", Tags: []string{"Authentification"},
		Summary:     "Connexion par e-mail (démo et développement uniquement)",
		Description: "Désactivée en production : l'authentification passe par OIDC (Keycloak, SSO).",
		Security:    []map[string][]string{},
	}, func(ctx context.Context, in *loginIn) (*sessionOut, error) {
		if !s.Config.DevLogin {
			return nil, huma.Error404NotFound("not available")
		}
		if _, err := mail.ParseAddress(in.Body.Email); err != nil {
			return nil, invalid("invalid email")
		}
		u, err := s.upsertUser(ctx, "", in.Body.Email, in.Body.Name)
		if err != nil {
			return nil, s.fail(ctx, err)
		}
		if DemoOrgHook != nil {
			if err := DemoOrgHook(ctx, u); err != nil {
				return nil, s.fail(ctx, err)
			}
		}
		tok, exp, err := s.Sessions.Issue(u, s.now())
		if err != nil {
			return nil, s.fail(ctx, err)
		}
		o := &sessionOut{SetCookie: s.sessionCookie(tok, exp)}
		o.Body.Token, o.Body.ExpiresAt, o.Body.User = tok, exp, u
		return o, nil
	})

	type logoutOut struct {
		SetCookie http.Cookie `header:"Set-Cookie"`
	}
	huma.Register(s.API, huma.Operation{
		OperationID: "logout", Method: http.MethodPost, Path: Prefix + "/auth/logout", Tags: []string{"Authentification"},
		Summary: "Déconnexion", DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, _ *struct{}) (*logoutOut, error) {
		c := s.sessionCookie("", time.Unix(0, 0)) //nolint:gosec // G124 : mêmes attributs que le cookie de session (HttpOnly, SameSite, Secure)
		c.MaxAge = -1
		return &logoutOut{SetCookie: c}, nil
	})

	type redirectOut struct {
		Status    int
		Location  string      `header:"Location"`
		SetCookie http.Cookie `header:"Set-Cookie"`
	}
	type oidcLoginIn struct {
		Redirect string `query:"redirect"`
		IdP      string `query:"idp" doc:"Alias du fournisseur d'identité Keycloak (SSO de l'organisation)"`
	}
	huma.Register(s.API, huma.Operation{
		OperationID: "oidc-login", Method: http.MethodGet, Path: Prefix + "/auth/oidc/login", Tags: []string{"Authentification"},
		Summary: "Démarre la connexion OIDC (code + PKCE)", Security: []map[string][]string{},
	}, func(ctx context.Context, in *oidcLoginIn) (*redirectOut, error) {
		if s.OIDC == nil {
			return nil, huma.Error404NotFound("OIDC is not configured")
		}
		state, _ := auth.RandomString(24)
		nonce, _ := auth.RandomString(24)
		verifier := oauth2.GenerateVerifier()
		exp := s.now().Add(10 * time.Minute)
		flow, err := jwt.NewWithClaims(jwt.SigningMethodHS256, flowClaims{
			State: state, Nonce: nonce, Verifier: verifier, Redirect: safeRedirect(in.Redirect),
			RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(exp)},
		}).SignedString(s.Sessions.Secret)
		if err != nil {
			return nil, s.fail(ctx, err)
		}
		opts := []oauth2.AuthCodeOption{oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)}
		if in.IdP != "" {
			opts = append(opts, oauth2.SetAuthURLParam("kc_idp_hint", in.IdP))
		}
		return &redirectOut{
			Status: http.StatusFound, Location: s.OIDC.oauth.AuthCodeURL(state, opts...),
			//nolint:gosec // G124 : HttpOnly, SameSite=Lax, Secure dès que l'API est en HTTPS
			SetCookie: http.Cookie{Name: flowCookie, Value: flow, Path: Prefix + "/auth/oidc", Expires: exp, HttpOnly: true,
				Secure: strings.HasPrefix(s.Config.APIURL, "https://"), SameSite: http.SameSiteLaxMode},
		}, nil
	})

	type callbackIn struct {
		Code  string `query:"code"`
		State string `query:"state"`
		Flow  string `cookie:"kairn_oidc"`
	}
	huma.Register(s.API, huma.Operation{
		OperationID: "oidc-callback", Method: http.MethodGet, Path: Prefix + "/auth/oidc/callback", Tags: []string{"Authentification"},
		Summary: "Retour du fournisseur OIDC", Security: []map[string][]string{},
	}, func(ctx context.Context, in *callbackIn) (*redirectOut, error) {
		if s.OIDC == nil {
			return nil, huma.Error404NotFound("OIDC is not configured")
		}
		var fc flowClaims
		if _, err := jwt.ParseWithClaims(in.Flow, &fc, func(*jwt.Token) (any, error) { return s.Sessions.Secret, nil },
			jwt.WithValidMethods([]string{"HS256"})); err != nil || fc.State == "" || fc.State != in.State {
			return nil, huma.Error400BadRequest("invalid login state")
		}
		tok, err := s.OIDC.oauth.Exchange(ctx, in.Code, oauth2.VerifierOption(fc.Verifier))
		if err != nil {
			return nil, huma.Error401Unauthorized("code exchange failed")
		}
		rawID, _ := tok.Extra("id_token").(string)
		idt, err := s.OIDC.idVerif.Verify(ctx, rawID)
		if err != nil {
			return nil, huma.Error401Unauthorized("invalid id token")
		}
		var c oidcClaims
		if err := idt.Claims(&c); err != nil || c.Nonce != fc.Nonce || c.Email == "" {
			return nil, huma.Error401Unauthorized("invalid id token claims")
		}
		u, err := s.upsertUser(ctx, idt.Subject, c.Email, c.Name)
		if err != nil {
			return nil, s.fail(ctx, err)
		}
		session, exp, err := s.Sessions.Issue(u, s.now())
		if err != nil {
			return nil, s.fail(ctx, err)
		}
		return &redirectOut{Status: http.StatusFound, Location: strings.TrimRight(s.Config.PublicURL, "/") + safeRedirect(fc.Redirect),
			SetCookie: s.sessionCookie(session, exp)}, nil
	})

	huma.Register(s.API, huma.Operation{
		OperationID: "get-me", Method: http.MethodGet, Path: Prefix + "/me", Tags: []string{"Authentification"},
		Summary: "Utilisateur courant, ses organisations et permissions",
	}, func(ctx context.Context, _ *struct{}) (*Out[meBody], error) {
		p, err := principal(ctx)
		if err != nil {
			return nil, err
		}
		if p.Kind == auth.KindToken {
			perms := map[string][]string{}
			for _, x := range auth.RolePermissions(p.Role) {
				if p.TokenAllows(x) {
					perms[p.OrgID] = append(perms[p.OrgID], string(x))
				}
			}
			return out(meBody{User: model.User{ID: p.TokenID, Name: "API token"}, Memberships: []membershipWithOrg{}, Permissions: perms}), nil
		}
		if p.Kind != auth.KindUser {
			return nil, huma.Error403Forbidden("user session required")
		}
		uctx := tenancy.WithUser(ctx, p.UserID)
		u, err := s.Store.Users().Get(uctx, p.UserID)
		if err != nil {
			return nil, huma.Error401Unauthorized("unknown user")
		}
		ms, err := s.Store.Memberships().ForUser(uctx, p.UserID)
		if err != nil {
			return nil, s.fail(ctx, err)
		}
		body := meBody{User: u, Memberships: []membershipWithOrg{}, Permissions: map[string][]string{}}
		for _, m := range ms {
			o, err := s.Store.Orgs().Get(tenancy.WithUser(tenancy.WithOrg(ctx, m.OrgID), p.UserID), m.OrgID)
			if err != nil {
				continue
			}
			body.Memberships = append(body.Memberships, membershipWithOrg{Membership: m, OrgName: o.Name, OrgSlug: o.Slug, Plan: o.Plan, Parent: o.ParentOrgID})
			for _, x := range auth.RolePermissions(m.Role) {
				body.Permissions[m.OrgID] = append(body.Permissions[m.OrgID], string(x))
			}
		}
		return out(body), nil
	})

	type patchMeIn struct {
		Body struct {
			Name   *string `json:"name,omitempty" maxLength:"120"`
			Locale *string `json:"locale,omitempty" enum:"fr,en"`
		}
	}
	huma.Register(s.API, huma.Operation{
		OperationID: "update-me", Method: http.MethodPatch, Path: Prefix + "/me", Tags: []string{"Authentification"},
		Summary: "Met à jour le profil",
	}, func(ctx context.Context, in *patchMeIn) (*Out[model.User], error) {
		p, err := userPrincipal(ctx)
		if err != nil {
			return nil, err
		}
		uctx := tenancy.WithUser(ctx, p.UserID)
		u, err := s.Store.Users().Get(uctx, p.UserID)
		if err != nil {
			return nil, s.fail(ctx, err)
		}
		if in.Body.Name != nil {
			u.Name = strings.TrimSpace(*in.Body.Name)
		}
		if in.Body.Locale != nil {
			u.Locale = *in.Body.Locale
		}
		if err := s.Store.Users().Update(uctx, &u); err != nil {
			return nil, s.fail(ctx, err)
		}
		return out(u), nil
	})
}
