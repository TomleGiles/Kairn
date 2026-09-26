package openstack

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kairn-io/kairn/connectors/internal/rest"
	"github.com/kairn-io/kairn/pkg/connector"
)

type endpoint struct {
	Interface string `json:"interface"`
	Region    string `json:"region"`
	RegionID  string `json:"region_id"`
	URL       string `json:"url"`
}

type catalogEntry struct {
	Type      string     `json:"type"`
	Name      string     `json:"name"`
	Endpoints []endpoint `json:"endpoints"`
}

type tokenBody struct {
	Token struct {
		ExpiresAt time.Time      `json:"expires_at"`
		Catalog   []catalogEntry `json:"catalog"`
		Project   struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Domain struct {
				Name string `json:"name"`
			} `json:"domain"`
		} `json:"project"`
		Roles []struct {
			Name string `json:"name"`
		} `json:"roles"`
	} `json:"token"`
}

// session porte le jeton Keystone et le catalogue de services.
type session struct {
	cfg  settings
	http *http.Client

	mu        sync.Mutex
	token     string
	expires   time.Time
	catalog   []catalogEntry
	projectID string
	project   string
	roles     []string
}

func (s *session) authBody() map[string]any {
	c := s.cfg
	if c.appCredID != "" {
		return map[string]any{"auth": map[string]any{"identity": map[string]any{
			"methods":                []string{"application_credential"},
			"application_credential": map[string]any{"id": c.appCredID, "secret": c.appCredSecret},
		}}}
	}
	user := map[string]any{"name": c.username, "password": c.password, "domain": map[string]any{"name": c.userDomain}}
	scope := map[string]any{}
	if c.projectID != "" {
		scope["project"] = map[string]any{"id": c.projectID}
	} else {
		scope["project"] = map[string]any{"name": c.projectName, "domain": map[string]any{"name": c.projectDomain}}
	}
	return map[string]any{"auth": map[string]any{
		"identity": map[string]any{"methods": []string{"password"}, "password": map[string]any{"user": user}},
		"scope":    scope,
	}}
}

// authenticate obtient (ou renouvelle) un jeton Keystone v3.
func (s *session) authenticate(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && time.Until(s.expires) > 5*time.Minute {
		return nil
	}
	var body tokenBody
	cl := &rest.Client{HTTP: s.http}
	hdr, err := cl.Do(ctx, http.MethodPost, rest.Join(s.cfg.authURL, "auth/tokens", nil), s.authBody(), &body)
	if err != nil {
		return fmt.Errorf("keystone authentication: %w", err)
	}
	tok := hdr.Get("X-Subject-Token")
	if tok == "" {
		return errors.New("keystone authentication: no token returned")
	}
	s.token, s.expires, s.catalog = tok, body.Token.ExpiresAt, body.Token.Catalog
	s.projectID, s.project = body.Token.Project.ID, body.Token.Project.Name
	s.roles = s.roles[:0]
	for _, r := range body.Token.Roles {
		s.roles = append(s.roles, r.Name)
	}
	if s.projectID == "" {
		return fmt.Errorf("%w: the credential is not scoped to a project", connector.ErrPermission)
	}
	return nil
}

// endpoint renvoie l'URL d'un service du catalogue pour la région et l'interface configurées.
func (s *session) endpoint(types ...string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, typ := range types {
		for _, e := range s.catalog {
			if e.Type != typ {
				continue
			}
			for _, ep := range e.Endpoints {
				if ep.Interface != s.cfg.iface {
					continue
				}
				if s.cfg.region != "" && ep.Region != s.cfg.region && ep.RegionID != s.cfg.region {
					continue
				}
				return strings.TrimRight(ep.URL, "/"), true
			}
		}
	}
	return "", false
}

// client renvoie un client REST authentifié par le jeton courant.
func (s *session) client(headers map[string]string) *rest.Client {
	return &rest.Client{HTTP: s.http, Headers: headers, Auth: func(req *http.Request) {
		s.mu.Lock()
		req.Header.Set("X-Auth-Token", s.token)
		s.mu.Unlock()
	}}
}
