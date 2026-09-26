// Package rest est le client HTTP JSON commun aux connecteurs : délais,
// taille de réponse bornée, CA personnalisée, traduction des 401/403 en
// connector.ErrPermission. Les connecteurs n'émettent que des lectures (GET),
// hormis l'authentification (POST sur les points d'entrée d'identité).
package rest

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kairn-io/kairn/pkg/connector"
)

// MaxBody borne la taille d'une réponse (protection mémoire).
const MaxBody = 64 << 20

// Client est un client JSON.
type Client struct {
	HTTP    *http.Client
	Headers map[string]string
	// Auth est appelé avant chaque requête pour positionner l'authentification.
	Auth func(req *http.Request)
}

// Options de transport.
type Options struct {
	CAPEM              string // certificats d'autorité supplémentaires (PEM)
	InsecureSkipVerify bool   // déconseillé ; uniquement pour des laboratoires
	Timeout            time.Duration
}

// NewHTTP construit un client HTTP selon les options.
func NewHTTP(o Options) (*http.Client, error) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if o.CAPEM != "" {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM([]byte(o.CAPEM)) {
			return nil, errors.New("rest: invalid CA certificate (PEM expected)")
		}
		tlsCfg.RootCAs = pool
	}
	if o.InsecureSkipVerify {
		tlsCfg.InsecureSkipVerify = true //nolint:gosec // option explicite de l'utilisateur, affichée dans l'UI
	}
	tr.TLSClientConfig = tlsCfg
	timeout := o.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	return &http.Client{Transport: tr, Timeout: timeout}, nil
}

// StatusError décrit une réponse HTTP en erreur (sans corps : il peut contenir des données sensibles).
type StatusError struct {
	Method, URL string
	Status      int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s %s: HTTP %d", e.Method, redact(e.URL), e.Status)
}

// redact retire la chaîne de requête (jetons éventuels) d'une URL pour les messages d'erreur.
func redact(u string) string {
	if i := strings.IndexByte(u, '?'); i >= 0 {
		return u[:i]
	}
	return u
}

// Do exécute une requête et décode la réponse JSON dans out (si non nil).
func (c *Client) Do(ctx context.Context, method, u string, body any, out any) (http.Header, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", "Kairn-Connector/1.0")
	for k, v := range c.Headers {
		req.Header.Set(k, v)
	}
	if c.Auth != nil {
		c.Auth(req)
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, redact(u), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return resp.Header, fmt.Errorf("%w: %s", connector.ErrPermission, (&StatusError{method, u, resp.StatusCode}).Error())
	}
	if resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return resp.Header, &StatusError{method, u, resp.StatusCode}
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, MaxBody))
		return resp.Header, nil
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, MaxBody))
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		return resp.Header, fmt.Errorf("%s %s: decode: %w", method, redact(u), err)
	}
	return resp.Header, nil
}

// Get lit une ressource JSON.
func (c *Client) Get(ctx context.Context, u string, out any) error {
	_, err := c.Do(ctx, http.MethodGet, u, nil, out)
	return err
}

// Join construit une URL à partir d'une base et d'un chemin, avec paramètres.
func Join(base, path string, q url.Values) string {
	u := strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

// IsNotFound indique une réponse 404 (service optionnel absent).
func IsNotFound(err error) bool {
	var se *StatusError
	return errors.As(err, &se) && (se.Status == http.StatusNotFound || se.Status == http.StatusNotImplemented)
}
