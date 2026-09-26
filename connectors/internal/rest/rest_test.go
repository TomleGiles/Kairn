package rest

import (
	"context"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/kairn-io/kairn/pkg/connector"
)

func TestClient(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			if r.Header.Get("X-Test") != "1" || r.Header.Get("Authorization") != "Bearer t" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"n": 12345678901234567890, "s": "x"}`))
		case "/forbidden":
			w.WriteHeader(http.StatusForbidden)
		case "/missing":
			w.WriteHeader(http.StatusNotFound)
		case "/bad":
			_, _ = w.Write([]byte(`{not json`))
		}
	}))
	defer srv.Close()
	// La CA du serveur de test est ajoutée explicitement (cloud privé à PKI interne).
	cert := srv.Certificate()
	pemCA := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}))
	hc, err := NewHTTP(Options{CAPEM: pemCA})
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{HTTP: hc, Headers: map[string]string{"X-Test": "1"}, Auth: func(r *http.Request) { r.Header.Set("Authorization", "Bearer t") }}
	var out map[string]any
	if err := c.Get(context.Background(), Join(srv.URL, "/ok", nil), &out); err != nil {
		t.Fatal(err)
	}
	if out["s"] != "x" || out["n"].(interface{ String() string }).String() != "12345678901234567890" {
		t.Fatalf("numbers must stay exact: %+v", out)
	}
	if err := c.Get(context.Background(), Join(srv.URL, "forbidden", url.Values{"token": {"secret"}}), nil); !errors.Is(err, connector.ErrPermission) {
		t.Fatalf("403 → ErrPermission: %v", err)
	} else if strings.Contains(err.Error(), "secret") {
		t.Fatal("query strings (tokens) must be redacted from errors")
	}
	if err := c.Get(context.Background(), Join(srv.URL, "missing", nil), nil); !IsNotFound(err) {
		t.Fatalf("404: %v", err)
	}
	if err := c.Get(context.Background(), Join(srv.URL, "bad", nil), &out); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	if _, err := NewHTTP(Options{CAPEM: "not a pem"}); err == nil {
		t.Fatal("invalid CA accepted")
	}
	// Sans la CA, le certificat du serveur de test est refusé.
	plain, _ := NewHTTP(Options{})
	if err := (&Client{HTTP: plain}).Get(context.Background(), srv.URL+"/ok", nil); err == nil {
		t.Fatal("untrusted certificate accepted")
	}
}
