// Package secrets chiffre les credentials clients par enveloppe : chaque
// valeur est chiffrée (AES-256-GCM) avec une clé de données aléatoire, elle-même
// chiffrée par une clé maîtresse (KEK) locale ou par Vault Transit.
// Les données associées (AAD) lient le chiffré à son organisation et à son
// objet : un chiffré copié vers une autre organisation ne se déchiffre pas.
package secrets

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrDecrypt est renvoyée quand un chiffré est invalide ou ne correspond pas à son AAD.
var ErrDecrypt = errors.New("secrets: decryption failed")

// KeyWrapper chiffre et déchiffre les clés de données.
type KeyWrapper interface {
	Wrap(ctx context.Context, dataKey, aad []byte) ([]byte, error)
	Unwrap(ctx context.Context, wrapped, aad []byte) ([]byte, error)
	Name() string
}

// Keyring chiffre des valeurs par enveloppe.
type Keyring struct {
	wrapper KeyWrapper
}

// NewKeyring crée un trousseau à partir d'un KeyWrapper.
func NewKeyring(w KeyWrapper) *Keyring { return &Keyring{wrapper: w} }

const formatV1 byte = 1

// AAD construit les données associées à partir d'éléments d'identification.
func AAD(parts ...string) []byte { return []byte(strings.Join(parts, "|")) }

// Encrypt chiffre plaintext. Format : v1 | len(wrapped) uint16 | wrapped | nonce | ciphertext.
func (k *Keyring) Encrypt(ctx context.Context, plaintext, aad []byte) ([]byte, error) {
	dataKey := make([]byte, 32)
	if _, err := rand.Read(dataKey); err != nil {
		return nil, fmt.Errorf("secrets: data key: %w", err)
	}
	wrapped, err := k.wrapper.Wrap(ctx, dataKey, aad)
	if err != nil {
		return nil, fmt.Errorf("secrets: wrap: %w", err)
	}
	if len(wrapped) > 0xffff {
		return nil, errors.New("secrets: wrapped key too large")
	}
	sealed, nonce, err := seal(dataKey, plaintext, aad)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteByte(formatV1)
	_ = binary.Write(&buf, binary.BigEndian, uint16(len(wrapped)))
	buf.Write(wrapped)
	buf.Write(nonce)
	buf.Write(sealed)
	return buf.Bytes(), nil
}

// Decrypt déchiffre un chiffré produit par Encrypt avec le même AAD.
func (k *Keyring) Decrypt(ctx context.Context, blob, aad []byte) ([]byte, error) {
	if len(blob) < 3 || blob[0] != formatV1 {
		return nil, ErrDecrypt
	}
	n := int(binary.BigEndian.Uint16(blob[1:3]))
	if len(blob) < 3+n+12 {
		return nil, ErrDecrypt
	}
	wrapped := blob[3 : 3+n]
	nonce := blob[3+n : 3+n+12]
	sealed := blob[3+n+12:]
	dataKey, err := k.wrapper.Unwrap(ctx, wrapped, aad)
	if err != nil {
		return nil, ErrDecrypt
	}
	return open(dataKey, nonce, sealed, aad)
}

// EncryptMap chiffre une table de secrets (JSON). Une table vide donne nil.
func (k *Keyring) EncryptMap(ctx context.Context, m map[string]string, aad []byte) ([]byte, error) {
	if len(m) == 0 {
		return nil, nil
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return k.Encrypt(ctx, raw, aad)
}

// DecryptMap déchiffre une table de secrets. Un chiffré vide donne une table vide.
func (k *Keyring) DecryptMap(ctx context.Context, blob, aad []byte) (map[string]string, error) {
	out := map[string]string{}
	if len(blob) == 0 {
		return out, nil
	}
	raw, err := k.Decrypt(ctx, blob, aad)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, ErrDecrypt
	}
	return out, nil
}

func seal(key, plaintext, aad []byte) (sealed, nonce []byte, err error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	return gcm.Seal(nil, nonce, plaintext, aad), nonce, nil
}

func open(key, nonce, sealed, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrDecrypt
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrDecrypt
	}
	out, err := gcm.Open(nil, nonce, sealed, aad)
	if err != nil {
		return nil, ErrDecrypt
	}
	return out, nil
}

// LocalKEK enveloppe les clés de données avec une clé maîtresse locale (32 octets).
// Adapté au mode self-hosted sans Vault ; la KEK doit venir d'un secret Kubernetes.
type LocalKEK struct {
	key []byte
}

// NewLocalKEK décode une KEK en base64 (32 octets).
func NewLocalKEK(b64 string) (*LocalKEK, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return nil, fmt.Errorf("secrets: invalid KEK encoding: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("secrets: KEK must be 32 bytes, got %d", len(key))
	}
	return &LocalKEK{key: key}, nil
}

// Name identifie le mécanisme.
func (l *LocalKEK) Name() string { return "local" }

// Wrap chiffre la clé de données.
func (l *LocalKEK) Wrap(_ context.Context, dataKey, aad []byte) ([]byte, error) {
	sealed, nonce, err := seal(l.key, dataKey, aad)
	if err != nil {
		return nil, err
	}
	return append(nonce, sealed...), nil
}

// Unwrap déchiffre la clé de données.
func (l *LocalKEK) Unwrap(_ context.Context, wrapped, aad []byte) ([]byte, error) {
	if len(wrapped) < 12 {
		return nil, ErrDecrypt
	}
	return open(l.key, wrapped[:12], wrapped[12:], aad)
}

// VaultTransit enveloppe les clés de données via le moteur Transit de Vault.
type VaultTransit struct {
	Addr    string
	Token   string
	KeyName string
	Client  *http.Client
}

// Name identifie le mécanisme.
func (v *VaultTransit) Name() string { return "vault-transit" }

func (v *VaultTransit) call(ctx context.Context, op string, body map[string]string) (map[string]any, error) {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(v.Addr, "/")+"/v1/transit/"+op+"/"+v.KeyName, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Vault-Token", v.Token)
	req.Header.Set("Content-Type", "application/json")
	client := v.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vault %s: %w", op, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("vault %s: status %d: %s", op, resp.StatusCode, b)
	}
	var out struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("vault %s: decode: %w", op, err)
	}
	return out.Data, nil
}

// Wrap chiffre la clé de données avec Vault (contexte = AAD, clé dérivée).
func (v *VaultTransit) Wrap(ctx context.Context, dataKey, aad []byte) ([]byte, error) {
	data, err := v.call(ctx, "encrypt", map[string]string{
		"plaintext": base64.StdEncoding.EncodeToString(dataKey),
		"context":   base64.StdEncoding.EncodeToString(aad),
	})
	if err != nil {
		return nil, err
	}
	ct, _ := data["ciphertext"].(string)
	if ct == "" {
		return nil, errors.New("vault encrypt: empty ciphertext")
	}
	return []byte(ct), nil
}

// Unwrap déchiffre la clé de données avec Vault.
func (v *VaultTransit) Unwrap(ctx context.Context, wrapped, aad []byte) ([]byte, error) {
	data, err := v.call(ctx, "decrypt", map[string]string{
		"ciphertext": string(wrapped),
		"context":    base64.StdEncoding.EncodeToString(aad),
	})
	if err != nil {
		return nil, err
	}
	pt, _ := data["plaintext"].(string)
	return base64.StdEncoding.DecodeString(pt)
}

// GenerateKEK produit une KEK aléatoire encodée en base64 (outil d'installation).
func GenerateKEK() (string, error) {
	k := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, k); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(k), nil
}
