// Package api implémente l'API REST publique /api/v1 (M-12) : OpenAPI 3.1
// générée depuis le code, erreurs RFC 9457 (problem+json), pagination par
// curseur, isolation stricte par organisation.
package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/auth"
	"github.com/kairn-io/kairn/pkg/bus"
	"github.com/kairn-io/kairn/pkg/config"
	"github.com/kairn-io/kairn/pkg/objstore"
	"github.com/kairn-io/kairn/pkg/ratelimit"
	"github.com/kairn-io/kairn/pkg/secrets"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/tsdb"
)

// Version est la version de l'API publique.
const Version = "1.0.0"

// Prefix est le préfixe des routes publiques.
const Prefix = "/api/v1"

// Jobs déclenche les traitements asynchrones (NATS en production, en processus en démo).
type Jobs interface {
	RequestSync(ctx context.Context, orgID, connectorID string, backfillDays int) error
	RequestRecompute(ctx context.Context, orgID string, from, to time.Time) error
	RequestAnalytics(ctx context.Context, orgID string) error
	RequestReport(ctx context.Context, orgID, period string) error
}

// ChannelTester envoie une notification de test sur un canal.
type ChannelTester interface {
	Test(ctx context.Context, channelID string) error
}

// Deps regroupe les dépendances du serveur.
type Deps struct {
	Store    store.Store
	TSDB     tsdb.TSDB
	Keyring  *secrets.Keyring
	Sessions auth.SessionIssuer
	OIDC     *OIDC
	Objects  objstore.Store
	Jobs     Jobs
	Bus      bus.Bus
	Channels ChannelTester
	Config   config.Config
	Log      *slog.Logger
	Now      func() time.Time
	HTTP     *http.Client
	// Limiter limite le débit par principal ou IP (Redis partagé entre
	// réplicas en production ; seau local par défaut).
	Limiter ratelimit.Limiter
	// Ready vérifie les dépendances externes (readiness).
	Ready func(ctx context.Context) error
}

// Limites de débit par principal (ou par IP pour les requêtes anonymes).
const (
	RateLimitPerSecond = 20
	RateLimitBurst     = 60
)

// Server est l'API.
type Server struct {
	Deps
	API     huma.API
	Router  chi.Router
	limiter ratelimit.Limiter
	// raw sert des préfixes hors des middlewares de l'API (voir Handle).
	raw []rawRoute
}

type rawRoute struct {
	prefix string
	h      http.Handler
}

// Handle sert un préfixe par un gestionnaire autonome, hors des middlewares
// de l'API (authentification par session ou jeton d'API, limitation de débit) :
// c'est le cas de la passerelle d'ingestion en mode nœud unique, qui
// authentifie elle-même l'agent et les webhooks, comme en mode distribué.
func (s *Server) Handle(prefix string, h http.Handler) {
	s.raw = append(s.raw, rawRoute{prefix: prefix, h: h})
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// New construit le serveur et enregistre toutes les routes.
func New(d Deps) *Server {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.HTTP == nil {
		d.HTTP = &http.Client{Timeout: 60 * time.Second}
	}
	if d.Limiter == nil {
		d.Limiter = ratelimit.NewMemory(RateLimitPerSecond, RateLimitBurst)
	}
	s := &Server{Deps: d, limiter: d.Limiter}
	r := chi.NewRouter()
	r.Use(middleware.RequestID, s.realIP, s.recoverer, securityHeaders, s.cors, s.logRequests, s.authenticate, s.rateLimit)
	s.Router = r

	cfg := huma.DefaultConfig("Kairn API", Version)
	cfg.CreateHooks = nil // pas de champ $schema dans les réponses
	cfg.OpenAPIPath = Prefix + "/openapi"
	cfg.DocsPath = Prefix + "/docs"
	cfg.SchemasPath = Prefix + "/schemas"
	cfg.FieldsOptionalByDefault = true
	cfg.Info.Description = "API publique de Kairn — observabilité orientée coûts pour les clouds souverains, OpenStack et Kubernetes. " +
		"Montants en chaînes décimales, dates en UTC (RFC 3339), erreurs au format RFC 9457."
	cfg.Info.Contact = &huma.Contact{Name: "Kairn", URL: "https://kairn.io"}
	cfg.Servers = []*huma.Server{{URL: d.Config.APIURL}}
	cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"bearer":  {Type: "http", Scheme: "bearer", Description: "Jeton d'API (kairn_…) ou session"},
		"session": {Type: "apiKey", In: "cookie", Name: SessionCookie},
	}
	cfg.Security = []map[string][]string{{"bearer": {}}, {"session": {}}}
	cfg.Components.Schemas.RegisterTypeAlias(reflect.TypeOf(decimal.Decimal{}), reflect.TypeOf(""))
	s.API = humachi.New(r, cfg)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	r.Get("/readyz", s.readyz)

	s.registerAuth()
	s.registerOrgs()
	s.registerConnectors()
	s.registerInventory()
	s.registerCosts()
	s.registerPricing()
	s.registerAllocation()
	s.registerOptimization()
	s.registerAlerting()
	s.registerUptime()
	s.registerReports()
	s.registerInternal()
	s.registerSCIM()
	return s
}

// ServeHTTP implémente http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	for _, rr := range s.raw {
		if strings.HasPrefix(r.URL.Path, rr.prefix) {
			rr.h.ServeHTTP(w, r)
			return
		}
	}
	s.Router.ServeHTTP(w, r)
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	if s.Ready != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := s.Ready(ctx); err != nil {
			s.Log.Warn("readiness failed", "err", err)
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready"))
}

// ------------------------------------------------------------------ middlewares

type requestMetaKey struct{}

type requestMeta struct {
	IP, UserAgent, RequestID string
}

func metaFrom(ctx context.Context) requestMeta {
	m, _ := ctx.Value(requestMetaKey{}).(requestMeta)
	return m
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		ctx := context.WithValue(r.Context(), requestMetaKey{}, requestMeta{
			IP: r.RemoteAddr, UserAgent: r.UserAgent(), RequestID: middleware.GetReqID(r.Context()),
		})
		next.ServeHTTP(ww, r.WithContext(ctx))
		// Jamais de corps, d'en-tête d'autorisation ni de paramètre sensible dans les journaux.
		s.Log.Info("http", "method", r.Method, "path", r.URL.Path, "status", ww.Status(),
			"duration_ms", time.Since(start).Milliseconds(), "request_id", middleware.GetReqID(r.Context()))
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil && rec != http.ErrAbortHandler {
				s.Log.Error("panic", "path", r.URL.Path, "panic", rec)
				writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "unexpected error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		if !strings.HasSuffix(r.URL.Path, "/docs") {
			h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) cors(next http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, o := range s.Config.CORSOrigins {
		allowed[o] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && allowed[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Add("Vary", "Origin")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func writeProblem(w http.ResponseWriter, status int, title, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"title":` + quote(title) + `,"status":` + itoa(status) + `,"detail":` + quote(detail) + `}`))
}

func quote(s string) string {
	b := strings.Builder{}
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}

// ------------------------------------------------------------------ erreurs

// fail convertit une erreur de domaine en erreur HTTP problem+json.
func (s *Server) fail(ctx context.Context, err error) error {
	var se huma.StatusError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &se):
		return err
	case errors.Is(err, store.ErrNotFound):
		return huma.Error404NotFound("resource not found")
	case errors.Is(err, store.ErrConflict):
		return huma.Error409Conflict("resource already exists or conflicts with another one")
	case isTenancyErr(err):
		// Jamais de fuite d'existence entre organisations.
		return huma.Error404NotFound("resource not found")
	case errors.Is(err, context.Canceled):
		return huma.NewError(499, "request cancelled")
	}
	s.Log.Error("internal error", "err", err, "request_id", metaFrom(ctx).RequestID)
	return huma.Error500InternalServerError("internal error")
}

// invalid renvoie une erreur de validation 422.
func invalid(msg string, errs ...error) error { return huma.Error422UnprocessableEntity(msg, errs...) }

// ------------------------------------------------------------------ helpers de sortie

// Page est une page de résultats.
type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// Out est une réponse JSON simple.
type Out[T any] struct {
	Body T
}

// Empty est une réponse sans corps (204).
type Empty struct{}

func out[T any](v T) *Out[T] { return &Out[T]{Body: v} }

func page[T any](items []T, limit int, id func(T) string) Page[T] {
	if items == nil {
		items = []T{}
	}
	p := Page[T]{Items: items}
	if limit > 0 && len(items) == limit {
		p.NextCursor = id(items[len(items)-1])
	}
	return p
}
