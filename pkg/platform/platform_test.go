package platform

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kairn-io/kairn/pkg/config"
	"github.com/kairn-io/kairn/pkg/secrets"
)

func TestKeyringSelection(t *testing.T) {
	if _, err := Keyring(config.Config{}); err == nil {
		t.Fatal("a key source is required")
	}
	kek, _ := secrets.GenerateKEK()
	kr, err := Keyring(config.Config{KEK: kek})
	if err != nil {
		t.Fatal(err)
	}
	enc, err := kr.Encrypt(context.Background(), []byte("s3cret"), []byte("org|x"))
	if err != nil {
		t.Fatal(err)
	}
	if plain, err := kr.Decrypt(context.Background(), enc, []byte("org|x")); err != nil || string(plain) != "s3cret" {
		t.Fatalf("roundtrip: %v", err)
	}
	if _, err := Keyring(config.Config{KEK: "not-base64!"}); err == nil {
		t.Fatal("invalid KEK accepted")
	}
	if kr, err := Keyring(config.Config{VaultAddr: "https://vault.example", VaultToken: "t", VaultKey: "kairn"}); err != nil || kr == nil {
		t.Fatalf("vault keyring: %v", err)
	}
}

func TestMemoryBackendsAndHealthServer(t *testing.T) {
	be, err := Memory(config.Config{LocalObjectDir: t.TempDir()}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer be.Close()
	if !be.InProcessBus || be.Store == nil || be.TSDB == nil || be.Ready(context.Background()) != nil {
		t.Fatalf("memory backends: %+v", be)
	}
	ready := true
	srv := httptest.NewServer(HealthServer("", func(context.Context) error {
		if !ready {
			return errors.New("down")
		}
		return nil
	}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })).Handler)
	defer srv.Close()
	code := func(path string) int {
		r, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r.StatusCode
	}
	if code("/healthz") != 200 || code("/readyz") != 200 || code("/other") != http.StatusTeapot {
		t.Fatal("health endpoints")
	}
	ready = false
	if code("/readyz") != http.StatusServiceUnavailable || code("/healthz") != 200 {
		t.Fatal("readiness must reflect dependencies")
	}
	if e := Email(config.Config{SMTPAddr: "smtp:587", SMTPFrom: "a@b"}); e.Addr != "smtp:587" || e.From != "a@b" {
		t.Fatalf("email: %+v", e)
	}
}
