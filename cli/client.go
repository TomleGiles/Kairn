package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config est la configuration locale de la CLI (~/.config/kairn/config.json, droits 0600).
type Config struct {
	URL   string `json:"url"`
	Token string `json:"token"`
	Org   string `json:"org"`
}

func configPath() (string, error) {
	if p := os.Getenv("KAIRN_CONFIG"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "kairn", "config.json"), nil
}

// loadConfig lit le fichier puis applique les variables d'environnement (prioritaires).
func loadConfig() (Config, error) {
	var c Config
	if p, err := configPath(); err == nil {
		if b, err := os.ReadFile(p); err == nil {
			if err := json.Unmarshal(b, &c); err != nil {
				return c, fmt.Errorf("read %s: %w", p, err)
			}
		}
	}
	if v := os.Getenv("KAIRN_URL"); v != "" {
		c.URL = v
	}
	if v := os.Getenv("KAIRN_TOKEN"); v != "" {
		c.Token = v
	}
	if v := os.Getenv("KAIRN_ORG"); v != "" {
		c.Org = v
	}
	c.URL = strings.TrimRight(c.URL, "/")
	return c, nil
}

func saveConfig(c Config) error {
	p, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(p, b, 0o600)
}

// Client appelle l'API publique Kairn.
type Client struct {
	cfg  Config
	http *http.Client
}

// APIError est une erreur RFC 9457 (problem+json).
type APIError struct {
	Status int    `json:"status"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

func (e *APIError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("%s (HTTP %d): %s", e.Title, e.Status, e.Detail)
	}
	return fmt.Sprintf("%s (HTTP %d)", e.Title, e.Status)
}

func newClient(cfg Config) (*Client, error) {
	if cfg.URL == "" || cfg.Token == "" {
		return nil, errors.New("not logged in: run `kairn login --url https://kairn.example --token kairn_…` or set KAIRN_URL and KAIRN_TOKEN")
	}
	return &Client{cfg: cfg, http: &http.Client{Timeout: 5 * time.Minute}}, nil
}

// orgPath remplace {org} par l'organisation courante.
func (c *Client) orgPath(p string) (string, error) {
	if strings.Contains(p, "{org}") {
		if c.cfg.Org == "" {
			return "", errors.New("no organization selected: run `kairn orgs` then `kairn use <id>`")
		}
		p = strings.ReplaceAll(p, "{org}", url.PathEscape(c.cfg.Org))
	}
	return p, nil
}

func (c *Client) do(ctx context.Context, method, path string, q url.Values, body any, accept string) (*http.Response, error) {
	p, err := c.orgPath(path)
	if err != nil {
		return nil, err
	}
	u := c.cfg.URL + "/api/v1" + p
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
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
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	req.Header.Set("User-Agent", "kairn-cli/"+version)
	if accept == "" {
		accept = "application/json"
	}
	req.Header.Set("Accept", accept)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		e := &APIError{Status: resp.StatusCode, Title: http.StatusText(resp.StatusCode)}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(e)
		return nil, e
	}
	return resp, nil
}

// JSON exécute une requête et décode la réponse.
func (c *Client) JSON(ctx context.Context, method, path string, q url.Values, body, out any) error {
	resp, err := c.do(ctx, method, path, q, body, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	dec := json.NewDecoder(resp.Body)
	dec.UseNumber()
	return dec.Decode(out)
}

// Raw copie la réponse brute (exports CSV).
func (c *Client) Raw(ctx context.Context, path string, q url.Values, w io.Writer) error {
	resp, err := c.do(ctx, http.MethodGet, path, q, nil, "text/csv")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, err = io.Copy(w, resp.Body)
	return err
}

// Stream lit un flux SSE et appelle fn pour chaque événement JSON.
func (c *Client) Stream(ctx context.Context, path string, body any, fn func(map[string]any)) error {
	resp, err := c.do(ctx, http.MethodPost, path, nil, body, "text/event-stream")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var ev map[string]any
		if json.Unmarshal([]byte(strings.TrimSpace(line[5:])), &ev) == nil {
			fn(ev)
		}
	}
	return sc.Err()
}
