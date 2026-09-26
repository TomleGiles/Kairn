package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/kairn-io/kairn/pkg/model"
)

// Types de principal.
const (
	KindUser    = "user"
	KindToken   = "token"
	KindService = "service"
)

// Principal est l'identité authentifiée d'une requête.
type Principal struct {
	Kind   string
	UserID string
	Email  string
	Name   string
	// Jetons d'API : organisation, rôle et scopes fixés par le jeton.
	TokenID string
	OrgID   string
	Role    model.Role
	Scopes  []string
}

// ActorID identifie le principal dans le journal d'audit.
func (p Principal) ActorID() string {
	switch p.Kind {
	case KindToken:
		return p.TokenID
	case KindService:
		return "service"
	}
	return p.UserID
}

// TokenAllows applique les scopes d'un jeton (vide = toutes les permissions du rôle).
func (p Principal) TokenAllows(perm Permission) bool {
	if p.Kind != KindToken || len(p.Scopes) == 0 {
		return perm != PermSCIM
	}
	return slices.Contains(p.Scopes, string(perm))
}

type principalKey struct{}

// WithPrincipal attache le principal au contexte.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// FromContext renvoie le principal du contexte.
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// ------------------------------------------------------------------ sessions

// ErrInvalidToken est renvoyée pour toute session ou jeton invalide.
var ErrInvalidToken = errors.New("auth: invalid token")

// SessionIssuer signe et vérifie les sessions Kairn (JWT HS256).
type SessionIssuer struct {
	Secret []byte
	TTL    time.Duration
	Issuer string
}

type sessionClaims struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
	jwt.RegisteredClaims
}

// Issue crée une session pour un utilisateur.
func (s SessionIssuer) Issue(u model.User, now time.Time) (string, time.Time, error) {
	if len(s.Secret) < 32 {
		return "", time.Time{}, errors.New("auth: session secret must be at least 32 bytes")
	}
	ttl := s.TTL
	if ttl == 0 {
		ttl = 12 * time.Hour
	}
	exp := now.Add(ttl)
	claims := sessionClaims{
		Email: u.Email, Name: u.Name,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: u.ID, Issuer: s.issuer(), IssuedAt: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp), Audience: jwt.ClaimStrings{"kairn-api"},
		},
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.Secret)
	return tok, exp, err
}

func (s SessionIssuer) issuer() string {
	if s.Issuer != "" {
		return s.Issuer
	}
	return "kairn"
}

// Verify vérifie une session et renvoie le principal utilisateur.
func (s SessionIssuer) Verify(token string, now time.Time) (Principal, error) {
	var claims sessionClaims
	_, err := jwt.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, ErrInvalidToken
		}
		return s.Secret, nil
	}, jwt.WithIssuer(s.issuer()), jwt.WithAudience("kairn-api"), jwt.WithTimeFunc(func() time.Time { return now }),
		jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || claims.Subject == "" {
		return Principal{}, ErrInvalidToken
	}
	return Principal{Kind: KindUser, UserID: claims.Subject, Email: claims.Email, Name: claims.Name}, nil
}

// ------------------------------------------------------------------ jetons d'API

// TokenPrefix préfixe tous les jetons d'API Kairn (détection de fuite par les scanners de secrets).
const TokenPrefix = "kairn_"

// NewAPIToken génère un jeton : kairn_<id public 12>_<secret 43>. Seul le hash du secret est stocké.
func NewAPIToken() (plain, prefix, hash string, err error) {
	pub := make([]byte, 9)
	sec := make([]byte, 32)
	if _, err = rand.Read(pub); err != nil {
		return
	}
	if _, err = rand.Read(sec); err != nil {
		return
	}
	prefix = base64.RawURLEncoding.EncodeToString(pub)
	secret := base64.RawURLEncoding.EncodeToString(sec)
	plain = TokenPrefix + prefix + "_" + secret
	return plain, prefix, HashSecret(secret), nil
}

// HashSecret hache un secret de jeton (SHA-256 : secrets à haute entropie).
func HashSecret(secret string) string {
	h := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(h[:])
}

// ParseAPIToken découpe un jeton en préfixe (12 caractères base64url, qui
// peuvent contenir « _ ») et secret.
func ParseAPIToken(plain string) (prefix, secret string, err error) {
	rest, ok := strings.CutPrefix(plain, TokenPrefix)
	if !ok || len(rest) < 14 || rest[12] != '_' {
		return "", "", ErrInvalidToken
	}
	return rest[:12], rest[13:], nil
}

// VerifyAPIToken compare un secret au hash stocké, en temps constant, et vérifie la validité.
func VerifyAPIToken(t model.APIToken, secret string, now time.Time) error {
	if t.RevokedAt != nil || (t.ExpiresAt != nil && !now.Before(*t.ExpiresAt)) {
		return ErrInvalidToken
	}
	if subtle.ConstantTimeCompare([]byte(HashSecret(secret)), []byte(t.Hash)) != 1 {
		return ErrInvalidToken
	}
	return nil
}

// ServiceTokenValid compare un jeton de service partagé en temps constant.
func ServiceTokenValid(expected, got string) bool {
	if expected == "" || got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(got)) == 1
}

// RandomString renvoie une chaîne aléatoire base64url de n octets.
func RandomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
