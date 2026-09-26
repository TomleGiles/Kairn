package chtsdb

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client parle à ClickHouse via son interface HTTP. Les valeurs ne sont jamais
// concaténées dans le SQL : elles passent en paramètres de requête typés
// ({nom:Type} côté SQL, param_nom dans l'URL).
type Client struct {
	URL      string // ex. http://clickhouse:8123
	Database string
	User     string
	Password string
	HTTP     *http.Client
}

// Params sont les paramètres nommés d'une requête.
type Params map[string]any

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

func formatParam(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case time.Time:
		return x.UTC().Format("2006-01-02 15:04:05.000"), nil
	case int:
		return strconv.Itoa(x), nil
	case int64:
		return strconv.FormatInt(x, 10), nil
	case uint32:
		return strconv.FormatUint(uint64(x), 10), nil
	case []string:
		// Format littéral ClickHouse Array(String) : ['a','b'] avec échappement.
		parts := make([]string, len(x))
		for i, s := range x {
			parts[i] = "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
		}
		return "[" + strings.Join(parts, ",") + "]", nil
	}
	return "", fmt.Errorf("chtsdb: unsupported parameter type %T", v)
}

func (c *Client) request(ctx context.Context, query string, params Params, body io.Reader, settings map[string]string) (*http.Response, error) {
	u, err := url.Parse(strings.TrimRight(c.URL, "/") + "/")
	if err != nil {
		return nil, fmt.Errorf("chtsdb: url: %w", err)
	}
	q := u.Query()
	if c.Database != "" {
		q.Set("database", c.Database)
	}
	for k, v := range settings {
		q.Set(k, v)
	}
	for k, v := range params {
		s, err := formatParam(v)
		if err != nil {
			return nil, err
		}
		q.Set("param_"+k, s)
	}
	var reader io.Reader = strings.NewReader(query)
	if body != nil {
		// Requête dans l'URL, données dans le corps (INSERT … FORMAT JSONEachRow).
		q.Set("query", query)
		reader = body
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), reader)
	if err != nil {
		return nil, err
	}
	if c.User != "" {
		req.Header.Set("X-ClickHouse-User", c.User)
		req.Header.Set("X-ClickHouse-Key", c.Password)
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("chtsdb: request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("chtsdb: clickhouse %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return resp, nil
}

// Exec exécute une instruction sans résultat.
func (c *Client) Exec(ctx context.Context, query string, params Params) error {
	resp, err := c.request(ctx, query, params, nil, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// Insert envoie des lignes au format JSONEachRow.
func (c *Client) Insert(ctx context.Context, table string, rows []map[string]any) error {
	if len(rows) == 0 {
		return nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			return fmt.Errorf("chtsdb: encode: %w", err)
		}
	}
	resp, err := c.request(ctx, "INSERT INTO "+table+" FORMAT JSONEachRow", nil, &buf,
		map[string]string{"date_time_input_format": "best_effort", "input_format_skip_unknown_fields": "0"})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// Query exécute une requête et décode chaque ligne JSONEachRow via fn.
func (c *Client) Query(ctx context.Context, query string, params Params, fn func(row map[string]json.RawMessage) error) error {
	resp, err := c.request(ctx, query+" FORMAT JSONEachRow", params, nil,
		map[string]string{"output_format_json_quote_64bit_integers": "0", "output_format_json_quote_decimals": "1"})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		row := map[string]json.RawMessage{}
		if err := json.Unmarshal(line, &row); err != nil {
			return fmt.Errorf("chtsdb: decode row: %w", err)
		}
		if err := fn(row); err != nil {
			return err
		}
	}
	return sc.Err()
}

// Ping vérifie la disponibilité du serveur.
func (c *Client) Ping(ctx context.Context) error { return c.Exec(ctx, "SELECT 1", nil) }
