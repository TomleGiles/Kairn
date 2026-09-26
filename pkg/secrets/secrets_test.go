package secrets

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func keyring(t *testing.T) *Keyring {
	t.Helper()
	kek, err := GenerateKEK()
	if err != nil {
		t.Fatal(err)
	}
	w, err := NewLocalKEK(kek)
	if err != nil {
		t.Fatal(err)
	}
	return NewKeyring(w)
}

func TestRoundTripAndAADBinding(t *testing.T) {
	ctx := context.Background()
	k := keyring(t)
	aad := AAD("org-a", "connector", "c1")
	blob, err := k.EncryptMap(ctx, map[string]string{"password": "s3cr3t"}, aad)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(blob, []byte("s3cr3t")) {
		t.Fatal("plaintext leaked in ciphertext")
	}
	got, err := k.DecryptMap(ctx, blob, aad)
	if err != nil || got["password"] != "s3cr3t" {
		t.Fatalf("round trip: %v %v", got, err)
	}
	if _, err := k.DecryptMap(ctx, blob, AAD("org-b", "connector", "c1")); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("ciphertext moved to another org must not decrypt: %v", err)
	}
	tampered := append([]byte(nil), blob...)
	tampered[len(tampered)-1] ^= 0xff
	if _, err := k.Decrypt(ctx, tampered, aad); !errors.Is(err, ErrDecrypt) {
		t.Fatal("tampered ciphertext must fail")
	}
}

func TestEmptyMap(t *testing.T) {
	k := keyring(t)
	blob, err := k.EncryptMap(context.Background(), nil, nil)
	if err != nil || blob != nil {
		t.Fatalf("empty map → nil blob: %v %v", blob, err)
	}
	m, err := k.DecryptMap(context.Background(), nil, nil)
	if err != nil || len(m) != 0 {
		t.Fatal("nil blob → empty map")
	}
}

func TestInvalidKEK(t *testing.T) {
	if _, err := NewLocalKEK("not-base64!"); err == nil {
		t.Fatal("invalid base64 must fail")
	}
	if _, err := NewLocalKEK(base64.StdEncoding.EncodeToString([]byte("short"))); err == nil {
		t.Fatal("short key must fail")
	}
}

func TestVaultTransit(t *testing.T) {
	// Faux Vault : « chiffre » en préfixant le contexte, vérifie le contexte au déchiffrement.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != "tok" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch {
		case strings.Contains(r.URL.Path, "/encrypt/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"ciphertext": "vault:v1:" + body["context"] + ":" + body["plaintext"]}})
		case strings.Contains(r.URL.Path, "/decrypt/"):
			parts := strings.SplitN(strings.TrimPrefix(body["ciphertext"], "vault:v1:"), ":", 2)
			if parts[0] != body["context"] {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"plaintext": parts[1]}})
		}
	}))
	defer srv.Close()
	k := NewKeyring(&VaultTransit{Addr: srv.URL, Token: "tok", KeyName: "kairn"})
	ctx := context.Background()
	blob, err := k.Encrypt(ctx, []byte("hello"), AAD("org-a"))
	if err != nil {
		t.Fatal(err)
	}
	pt, err := k.Decrypt(ctx, blob, AAD("org-a"))
	if err != nil || string(pt) != "hello" {
		t.Fatalf("vault round trip: %q %v", pt, err)
	}
	if _, err := k.Decrypt(ctx, blob, AAD("org-b")); err == nil {
		t.Fatal("wrong context must fail")
	}
}
