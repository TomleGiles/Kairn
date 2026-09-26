package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/kairn-io/kairn/pkg/model"
)

func TestRoles(t *testing.T) {
	cases := []struct {
		role model.Role
		perm Permission
		want bool
	}{
		{model.RoleOwner, PermBillingManage, true},
		{model.RoleAdmin, PermBillingManage, false},
		{model.RoleAdmin, PermConnectorsManage, true},
		{model.RoleFinance, PermBudgetsManage, true},
		{model.RoleFinance, PermConnectorsManage, false},
		{model.RoleEngineer, PermRecoManage, true},
		{model.RoleEngineer, PermAllocationManage, false},
		{model.RoleViewer, PermCostsRead, true},
		{model.RoleViewer, PermExport, false},
		{model.RoleViewer, PermMembersManage, false},
	}
	for _, c := range cases {
		if got := RoleCan(c.role, c.perm); got != c.want {
			t.Errorf("%s can %s = %v, want %v", c.role, c.perm, got, c.want)
		}
	}
}

func TestSession(t *testing.T) {
	s := SessionIssuer{Secret: []byte(strings.Repeat("k", 32)), TTL: time.Hour}
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	tok, _, err := s.Issue(model.User{ID: "u1", Email: "a@b.c"}, now)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Verify(tok, now.Add(30*time.Minute))
	if err != nil || p.UserID != "u1" || p.Kind != KindUser {
		t.Fatalf("verify: %+v %v", p, err)
	}
	if _, err := s.Verify(tok, now.Add(2*time.Hour)); err == nil {
		t.Fatal("expired session must fail")
	}
	other := SessionIssuer{Secret: []byte(strings.Repeat("x", 32))}
	if _, err := other.Verify(tok, now); err == nil {
		t.Fatal("wrong secret must fail")
	}
	if _, _, err := (SessionIssuer{Secret: []byte("short")}).Issue(model.User{ID: "u"}, now); err == nil {
		t.Fatal("short secret must be refused")
	}
}

func TestAPIToken(t *testing.T) {
	plain, prefix, hash, err := NewAPIToken()
	if err != nil {
		t.Fatal(err)
	}
	p, secret, err := ParseAPIToken(plain)
	if err != nil || p != prefix {
		t.Fatalf("parse: %q %q %v", p, prefix, err)
	}
	now := time.Now()
	tok := model.APIToken{Hash: hash}
	if err := VerifyAPIToken(tok, secret, now); err != nil {
		t.Fatal(err)
	}
	if err := VerifyAPIToken(tok, secret+"x", now); err == nil {
		t.Fatal("wrong secret")
	}
	past := now.Add(-time.Minute)
	tok.ExpiresAt = &past
	if err := VerifyAPIToken(tok, secret, now); err == nil {
		t.Fatal("expired token")
	}
	for _, bad := range []string{"", "kairn_", "ghp_xxx", "kairn_short"} {
		if _, _, err := ParseAPIToken(bad); err == nil {
			t.Errorf("%q must not parse", bad)
		}
	}
}

func TestTokenScopes(t *testing.T) {
	p := Principal{Kind: KindToken, Scopes: []string{string(PermCostsRead)}}
	if !p.TokenAllows(PermCostsRead) || p.TokenAllows(PermExport) {
		t.Fatal("scoped token")
	}
	full := Principal{Kind: KindToken}
	if !full.TokenAllows(PermExport) || full.TokenAllows(PermSCIM) {
		t.Fatal("unscoped token has role permissions except SCIM")
	}
}
