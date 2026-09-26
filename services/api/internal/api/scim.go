package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/kairn-io/kairn/pkg/auth"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/plans"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/tenancy"
)

// SCIM 2.0 (RFC 7643/7644) : provisionnement des utilisateurs depuis l'annuaire
// de l'organisation. Authentification par jeton d'API portant le scope « scim ».
// Un utilisateur SCIM correspond à une appartenance ; « active: false » ou la
// suppression retirent l'appartenance (l'identité globale est conservée).

const (
	scimUserSchema = "urn:ietf:params:scim:schemas:core:2.0:User"
	scimListSchema = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	scimErrSchema  = "urn:ietf:params:scim:api:messages:2.0:Error"
	// Extension Kairn portant le rôle RBAC.
	scimKairnExt = "urn:ietf:params:scim:schemas:extension:kairn:2.0:User"
)

type scimUser struct {
	Schemas  []string       `json:"schemas"`
	ID       string         `json:"id,omitempty"`
	UserName string         `json:"userName"`
	Name     *scimName      `json:"name,omitempty"`
	Emails   []scimEmail    `json:"emails,omitempty"`
	Active   *bool          `json:"active,omitempty"`
	Meta     map[string]any `json:"meta,omitempty"`
	Kairn    *scimKairn     `json:"urn:ietf:params:scim:schemas:extension:kairn:2.0:User,omitempty"`
}

type scimName struct {
	Formatted string `json:"formatted,omitempty"`
}

type scimEmail struct {
	Value   string `json:"value"`
	Primary bool   `json:"primary"`
}

type scimKairn struct {
	Role string `json:"role,omitempty"`
}

func scimError(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/scim+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"schemas": []string{scimErrSchema}, "status": strconv.Itoa(status), "detail": detail})
}

func scimJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/scim+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) toSCIM(m model.Membership) scimUser {
	active := true
	return scimUser{Schemas: []string{scimUserSchema, scimKairnExt}, ID: m.UserID, UserName: m.Email,
		Name: &scimName{Formatted: m.Name}, Emails: []scimEmail{{Value: m.Email, Primary: true}}, Active: &active,
		Meta: map[string]any{"resourceType": "User", "created": m.CreatedAt}, Kairn: &scimKairn{Role: string(m.Role)}}
}

var scimFilterRe = regexp.MustCompile(`^userName\s+eq\s+"([^"]+)"$`)

func (s *Server) registerSCIM() {
	s.Router.Route("/scim/v2", func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				p, ok := auth.FromContext(req.Context())
				if !ok || p.Kind != auth.KindToken || !p.TokenAllows(auth.PermSCIM) {
					scimError(w, http.StatusUnauthorized, "a token with the scim scope is required")
					return
				}
				ctx := tenancy.WithOrg(req.Context(), p.OrgID)
				org, err := s.Store.Orgs().Get(ctx, p.OrgID)
				if err != nil || !plans.For(org, s.now()).Allows(plans.FeatureSCIM) {
					scimError(w, http.StatusForbidden, "SCIM is not included in this plan")
					return
				}
				ctx = contextWithAccess(ctx, access{P: p, Org: org, Role: p.Role, Limits: plans.For(org, s.now())})
				next.ServeHTTP(w, req.WithContext(ctx))
			})
		})
		r.Get("/ServiceProviderConfig", func(w http.ResponseWriter, _ *http.Request) {
			scimJSON(w, http.StatusOK, map[string]any{
				"schemas":               []string{"urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"},
				"patch":                 map[string]bool{"supported": true},
				"bulk":                  map[string]any{"supported": false, "maxOperations": 0, "maxPayloadSize": 0},
				"filter":                map[string]any{"supported": true, "maxResults": 500},
				"changePassword":        map[string]bool{"supported": false},
				"sort":                  map[string]bool{"supported": false},
				"etag":                  map[string]bool{"supported": false},
				"authenticationSchemes": []map[string]any{{"type": "oauthbearertoken", "name": "Bearer", "description": "Jeton d'API Kairn avec le scope scim"}},
			})
		})
		r.Get("/ResourceTypes", func(w http.ResponseWriter, _ *http.Request) {
			scimJSON(w, http.StatusOK, map[string]any{"schemas": []string{scimListSchema}, "totalResults": 1, "Resources": []map[string]any{
				{"id": "User", "name": "User", "endpoint": "/Users", "schema": scimUserSchema,
					"schemaExtensions": []map[string]any{{"schema": scimKairnExt, "required": false}}},
			}})
		})
		r.Get("/Users", func(w http.ResponseWriter, req *http.Request) {
			ctx := req.Context()
			ms, err := s.Store.Memberships().List(ctx)
			if err != nil {
				scimError(w, http.StatusInternalServerError, "internal error")
				return
			}
			if f := strings.TrimSpace(req.URL.Query().Get("filter")); f != "" {
				mt := scimFilterRe.FindStringSubmatch(f)
				if mt == nil {
					scimError(w, http.StatusBadRequest, "only userName eq filters are supported")
					return
				}
				var keep []model.Membership
				for _, m := range ms {
					if strings.EqualFold(m.Email, mt[1]) {
						keep = append(keep, m)
					}
				}
				ms = keep
			}
			start, _ := strconv.Atoi(req.URL.Query().Get("startIndex"))
			count, _ := strconv.Atoi(req.URL.Query().Get("count"))
			if start < 1 {
				start = 1
			}
			if count <= 0 || count > 500 {
				count = 100
			}
			total := len(ms)
			var pageItems []scimUser
			for i := start - 1; i < len(ms) && len(pageItems) < count; i++ {
				pageItems = append(pageItems, s.toSCIM(ms[i]))
			}
			if pageItems == nil {
				pageItems = []scimUser{}
			}
			scimJSON(w, http.StatusOK, map[string]any{"schemas": []string{scimListSchema}, "totalResults": total, "startIndex": start,
				"itemsPerPage": len(pageItems), "Resources": pageItems})
		})
		upsert := func(w http.ResponseWriter, req *http.Request, status int) {
			ctx := req.Context()
			a := accessFrom(ctx)
			var in scimUser
			if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 1<<20)).Decode(&in); err != nil {
				scimError(w, http.StatusBadRequest, "invalid json")
				return
			}
			email := in.UserName
			for _, e := range in.Emails {
				if e.Primary || email == "" {
					email = e.Value
				}
			}
			if email == "" || !strings.Contains(email, "@") {
				scimError(w, http.StatusBadRequest, "userName must be an email address")
				return
			}
			role := model.RoleViewer
			if in.Kairn != nil && in.Kairn.Role != "" {
				role = model.Role(in.Kairn.Role)
				if !role.Valid() || role == model.RoleOwner {
					scimError(w, http.StatusBadRequest, "invalid role")
					return
				}
			}
			name := ""
			if in.Name != nil {
				name = in.Name.Formatted
			}
			u, err := s.upsertUser(ctx, "", email, name)
			if err != nil {
				scimError(w, http.StatusInternalServerError, "internal error")
				return
			}
			if id := chi.URLParam(req, "id"); id != "" && id != u.ID {
				scimError(w, http.StatusConflict, "userName belongs to another user")
				return
			}
			if in.Active != nil && !*in.Active {
				_ = s.Store.Memberships().Delete(ctx, u.ID)
				s.audit(ctx, a, "scim.user.deactivate", "user", u.ID, nil)
				scimJSON(w, http.StatusOK, scimUser{Schemas: []string{scimUserSchema}, ID: u.ID, UserName: u.Email, Active: in.Active})
				return
			}
			if cur, err := s.Store.Memberships().Get(ctx, u.ID); err == nil && cur.Role == model.RoleOwner {
				role = model.RoleOwner // SCIM ne rétrograde jamais un propriétaire
			}
			m := model.Membership{UserID: u.ID, Role: role}
			if err := s.Store.Memberships().Upsert(ctx, &m); err != nil {
				scimError(w, http.StatusInternalServerError, "internal error")
				return
			}
			m.Email, m.Name = u.Email, u.Name
			s.audit(ctx, a, "scim.user.upsert", "user", u.ID, map[string]any{"role": role})
			scimJSON(w, status, s.toSCIM(m))
		}
		r.Post("/Users", func(w http.ResponseWriter, req *http.Request) { upsert(w, req, http.StatusCreated) })
		r.Put("/Users/{id}", func(w http.ResponseWriter, req *http.Request) { upsert(w, req, http.StatusOK) })
		r.Get("/Users/{id}", func(w http.ResponseWriter, req *http.Request) {
			m, err := s.Store.Memberships().Get(req.Context(), chi.URLParam(req, "id"))
			if err != nil {
				scimError(w, http.StatusNotFound, "user not found")
				return
			}
			scimJSON(w, http.StatusOK, s.toSCIM(m))
		})
		r.Patch("/Users/{id}", func(w http.ResponseWriter, req *http.Request) {
			ctx := req.Context()
			id := chi.URLParam(req, "id")
			m, err := s.Store.Memberships().Get(ctx, id)
			if err != nil {
				scimError(w, http.StatusNotFound, "user not found")
				return
			}
			var in struct {
				Operations []struct {
					Op    string          `json:"op"`
					Path  string          `json:"path"`
					Value json.RawMessage `json:"value"`
				} `json:"Operations"`
			}
			if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 1<<20)).Decode(&in); err != nil {
				scimError(w, http.StatusBadRequest, "invalid json")
				return
			}
			for _, op := range in.Operations {
				if !strings.EqualFold(op.Op, "replace") {
					continue
				}
				var active *bool
				switch op.Path {
				case "active":
					var b bool
					if json.Unmarshal(op.Value, &b) == nil {
						active = &b
					}
				case "":
					var v struct {
						Active *bool `json:"active"`
					}
					if json.Unmarshal(op.Value, &v) == nil {
						active = v.Active
					}
				}
				if active != nil && !*active {
					if m.Role == model.RoleOwner {
						scimError(w, http.StatusConflict, "owners cannot be deactivated through SCIM")
						return
					}
					if err := s.Store.Memberships().Delete(ctx, id); err != nil && !errors.Is(err, store.ErrNotFound) {
						scimError(w, http.StatusInternalServerError, "internal error")
						return
					}
					s.audit(ctx, accessFrom(ctx), "scim.user.deactivate", "user", id, nil)
					f := false
					scimJSON(w, http.StatusOK, scimUser{Schemas: []string{scimUserSchema}, ID: id, UserName: m.Email, Active: &f})
					return
				}
			}
			scimJSON(w, http.StatusOK, s.toSCIM(m))
		})
		r.Delete("/Users/{id}", func(w http.ResponseWriter, req *http.Request) {
			ctx := req.Context()
			id := chi.URLParam(req, "id")
			if m, err := s.Store.Memberships().Get(ctx, id); err == nil && m.Role == model.RoleOwner {
				scimError(w, http.StatusConflict, "owners cannot be removed through SCIM")
				return
			}
			if err := s.Store.Memberships().Delete(ctx, id); err != nil {
				scimError(w, http.StatusNotFound, "user not found")
				return
			}
			s.audit(ctx, accessFrom(ctx), "scim.user.delete", "user", id, nil)
			w.WriteHeader(http.StatusNoContent)
		})
	})
}
