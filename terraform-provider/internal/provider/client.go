package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client appelle l'API publique Kairn avec un jeton d'API.
type Client struct {
	BaseURL   string
	Token     string
	OrgID     string
	UserAgent string
	HTTP      *http.Client
}

// APIError est une erreur RFC 9457 renvoyée par l'API.
type APIError struct {
	Status int    `json:"status"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Errors []struct {
		Message  string `json:"message"`
		Location string `json:"location"`
	} `json:"errors"`
}

func (e *APIError) Error() string {
	msg := e.Title
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	for _, d := range e.Errors {
		msg += "; " + d.Location + ": " + d.Message
	}
	return fmt.Sprintf("Kairn API %d: %s", e.Status, msg)
}

// IsNotFound indique une ressource absente (supprimée hors de Terraform).
func IsNotFound(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == http.StatusNotFound
}

func (c *Client) orgPath(p string) string { return "/api/v1/orgs/" + c.OrgID + p }

// do exécute une requête JSON ; out peut être nil.
func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.UserAgent)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: time.Minute}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		ae := &APIError{Status: resp.StatusCode, Title: http.StatusText(resp.StatusCode)}
		_ = json.Unmarshal(data, ae)
		ae.Status = resp.StatusCode
		return ae
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode %s %s: %w", method, path, err)
	}
	return nil
}

type orgRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// resolveOrg détermine l'organisation du jeton quand elle n'est pas fournie.
func (c *Client) resolveOrg(ctx context.Context) error {
	if c.OrgID != "" {
		return nil
	}
	var orgs []orgRef // GET /orgs renvoie un tableau (pas de pagination)
	if err := c.do(ctx, http.MethodGet, "/api/v1/orgs", nil, &orgs); err != nil {
		return err
	}
	if len(orgs) != 1 {
		return fmt.Errorf("the token gives access to %d organizations: set organization_id", len(orgs))
	}
	c.OrgID = orgs[0].ID
	return nil
}
