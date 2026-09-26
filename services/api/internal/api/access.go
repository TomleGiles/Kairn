package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/kairn-io/kairn/pkg/allocation"
	"github.com/kairn-io/kairn/pkg/auth"
	"github.com/kairn-io/kairn/pkg/ids"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/plans"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/tenancy"
)

// SessionCookie est le cookie de session de l'application web.
const SessionCookie = "kairn_session"

// ServiceTokenHeader porte le jeton des services internes.
const ServiceTokenHeader = "X-Kairn-Service-Token" //nolint:gosec // G101 : nom d'en-tête, pas un secret

func isTenancyErr(err error) bool {
	return errors.Is(err, tenancy.ErrCrossOrg) || errors.Is(err, tenancy.ErrNoOrg)
}

// bearer extrait le jeton de la requête (en-tête Authorization ou cookie de session).
func bearer(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if t, ok := strings.CutPrefix(h, "Bearer "); ok {
			return strings.TrimSpace(t)
		}
	}
	if c, err := r.Cookie(SessionCookie); err == nil {
		return c.Value
	}
	return ""
}

// authenticate identifie le principal. Une requête sans jeton reste anonyme
// (les routes protégées refusent ensuite) ; un jeton invalide est refusé.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if st := r.Header.Get(ServiceTokenHeader); st != "" {
			if !auth.ServiceTokenValid(s.Config.ServiceToken, st) {
				writeProblem(w, http.StatusUnauthorized, "Unauthorized", "invalid service token")
				return
			}
			next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(ctx, auth.Principal{Kind: auth.KindService, Role: model.RoleOwner})))
			return
		}
		tok := bearer(r)
		if tok == "" {
			next.ServeHTTP(w, r)
			return
		}
		p, err := s.principalFor(ctx, tok)
		if err != nil {
			writeProblem(w, http.StatusUnauthorized, "Unauthorized", "invalid or expired credentials")
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(ctx, p)))
	})
}

func (s *Server) principalFor(ctx context.Context, tok string) (auth.Principal, error) {
	now := s.now()
	if strings.HasPrefix(tok, auth.TokenPrefix) {
		prefix, secret, err := auth.ParseAPIToken(tok)
		if err != nil {
			return auth.Principal{}, err
		}
		t, err := s.Store.System().FindAPIToken(ctx, prefix)
		if err != nil {
			return auth.Principal{}, auth.ErrInvalidToken
		}
		if err := auth.VerifyAPIToken(t, secret, now); err != nil {
			return auth.Principal{}, err
		}
		if t.LastUsedAt == nil || now.Sub(*t.LastUsedAt) > 5*time.Minute {
			_ = s.Store.Tokens().Touch(tenancy.WithOrg(ctx, t.OrgID), t.ID, now)
		}
		return auth.Principal{Kind: auth.KindToken, TokenID: t.ID, OrgID: t.OrgID, Role: t.Role, Scopes: t.Scopes}, nil
	}
	if p, err := s.Sessions.Verify(tok, now); err == nil {
		return p, nil
	}
	if s.OIDC != nil {
		return s.OIDC.VerifyAccessToken(ctx, s.Store, tok)
	}
	return auth.Principal{}, auth.ErrInvalidToken
}

// ------------------------------------------------------------------ accès aux organisations

// access décrit les droits effectifs du principal sur une organisation.
type access struct {
	P      auth.Principal
	Org    model.Organization
	Role   model.Role
	Scopes []string
	Limits plans.Limits
}

type accessKey struct{}

// principal renvoie le principal authentifié ou une erreur 401.
func principal(ctx context.Context) (auth.Principal, error) {
	p, ok := auth.FromContext(ctx)
	if !ok {
		return auth.Principal{}, huma.Error401Unauthorized("authentication required")
	}
	return p, nil
}

// userPrincipal exige un utilisateur (pas un jeton ni un service).
func userPrincipal(ctx context.Context) (auth.Principal, error) {
	p, err := principal(ctx)
	if err != nil {
		return p, err
	}
	if p.Kind != auth.KindUser {
		return p, huma.Error403Forbidden("this operation requires a user session")
	}
	return p, nil
}

// enter vérifie l'accès du principal à l'organisation, la permission et la
// fonctionnalité du plan, puis renvoie un contexte porteur de l'organisation.
// Toute absence de droit sur l'organisation renvoie 404 (pas de fuite d'existence).
func (s *Server) enter(ctx context.Context, orgID string, perm auth.Permission, feature plans.Feature) (context.Context, access, error) {
	p, err := principal(ctx)
	if err != nil {
		return ctx, access{}, err
	}
	notFound := huma.Error404NotFound("organization not found")
	if !ids.Valid(orgID) {
		return ctx, access{}, notFound
	}
	octx := tenancy.WithOrg(ctx, orgID)
	if p.UserID != "" {
		octx = tenancy.WithUser(octx, p.UserID)
	}
	a := access{P: p}
	switch p.Kind {
	case auth.KindService:
		a.Role = model.RoleOwner
	case auth.KindToken:
		if p.OrgID != orgID {
			return ctx, access{}, notFound
		}
		a.Role, a.Scopes = p.Role, nil
	case auth.KindUser:
		m, err := s.Store.Memberships().Get(octx, p.UserID)
		switch {
		case err == nil:
			a.Role, a.Scopes = m.Role, m.Scopes
		case errors.Is(err, store.ErrNotFound):
			role, ok := s.mspRole(octx, orgID, p.UserID)
			if !ok {
				return ctx, access{}, notFound
			}
			a.Role = role
		default:
			return ctx, access{}, s.fail(ctx, err)
		}
	default:
		return ctx, access{}, notFound
	}
	org, err := s.Store.Orgs().Get(octx, orgID)
	if err != nil {
		return ctx, access{}, notFound
	}
	a.Org = org
	a.Limits = plans.For(org, s.now())
	if perm != "" {
		if !auth.RoleCan(a.Role, perm) || !p.TokenAllows(perm) {
			return ctx, access{}, huma.Error403Forbidden("missing permission " + string(perm))
		}
	}
	if !a.Limits.Allows(feature) {
		return ctx, access{}, huma.NewError(http.StatusPaymentRequired, "feature "+string(feature)+" is not included in plan "+string(org.Plan))
	}
	return context.WithValue(octx, accessKey{}, a), a, nil
}

func contextWithAccess(ctx context.Context, a access) context.Context {
	return context.WithValue(ctx, accessKey{}, a)
}

func accessFrom(ctx context.Context) access {
	a, _ := ctx.Value(accessKey{}).(access)
	return a
}

// mspRole accorde l'accès à une organisation cliente aux administrateurs de l'organisation MSP parente.
func (s *Server) mspRole(ctx context.Context, orgID, userID string) (model.Role, bool) {
	org, err := s.Store.Orgs().Get(ctx, orgID)
	if err != nil || org.ParentOrgID == nil {
		return "", false
	}
	pctx := tenancy.WithUser(tenancy.WithOrg(ctx, *org.ParentOrgID), userID)
	parent, err := s.Store.Orgs().Get(pctx, *org.ParentOrgID)
	if err != nil || !plans.For(parent, s.now()).Allows(plans.FeatureMSP) {
		return "", false
	}
	m, err := s.Store.Memberships().Get(pctx, userID)
	if err != nil || auth.RoleRank(m.Role) > auth.RoleRank(model.RoleAdmin) {
		return "", false
	}
	return m.Role, true
}

// allowedNodes calcule les nœuds d'allocation visibles (scopes RBAC) ; nil = tous.
func (s *Server) allowedNodes(ctx context.Context, a access) ([]string, error) {
	if len(a.Scopes) == 0 {
		return nil, nil
	}
	nodes, err := store.ListAll(ctx, s.Store.AllocationNodes(), func(n model.AllocationNode) string { return n.ID }, nil)
	if err != nil {
		return nil, err
	}
	return allocation.NewTree(nodes).AllowedNodes(a.Scopes), nil
}

// audit ajoute une entrée au journal d'audit de l'organisation courante.
func (s *Server) audit(ctx context.Context, a access, action, targetType, targetID string, details map[string]any) {
	m := metaFrom(ctx)
	if details == nil {
		details = map[string]any{}
	}
	e := model.AuditEvent{
		OrgID: a.Org.ID, ActorType: a.P.Kind, ActorID: a.P.ActorID(), Action: action,
		TargetType: targetType, TargetID: targetID, IP: m.IP, UserAgent: truncate(m.UserAgent, 200),
		Details: details, At: s.now(),
	}
	if err := s.Store.Audit().Append(ctx, &e); err != nil {
		s.Log.Error("audit append failed", "action", action, "err", err)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// ------------------------------------------------------------------ limitation de débit

func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := "ip:" + r.RemoteAddr
		if p, ok := auth.FromContext(r.Context()); ok {
			if p.Kind == auth.KindService {
				next.ServeHTTP(w, r)
				return
			}
			key = p.Kind + ":" + p.ActorID()
		}
		if !s.limiter.Allow(r.Context(), key) {
			w.Header().Set("Retry-After", "1")
			writeProblem(w, http.StatusTooManyRequests, "Too Many Requests", "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}
