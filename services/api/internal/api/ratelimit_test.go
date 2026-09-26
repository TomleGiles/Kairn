package api

import (
	"net/http"
	"testing"

	"github.com/kairn-io/kairn/pkg/ratelimit"
)

func TestRateLimitPerPrincipal(t *testing.T) {
	e := newEnv(t)
	alice := e.login("alice@example.com")
	bob := e.login("bob@example.com")
	e.srv.limiter = ratelimit.NewMemory(0.001, 2)
	for i := range 2 {
		if r := e.do(http.MethodGet, "/api/v1/me", alice, nil); r.Code != http.StatusOK {
			t.Fatalf("request %d: %d %s", i, r.Code, r.Body)
		}
	}
	r := e.do(http.MethodGet, "/api/v1/me", alice, nil)
	if r.Code != http.StatusTooManyRequests || r.Hdr.Get("Retry-After") == "" || r.Hdr.Get("Content-Type") != "application/problem+json" {
		t.Fatalf("third request must be limited with a problem+json 429: %d %v", r.Code, r.Hdr)
	}
	if r := e.do(http.MethodGet, "/api/v1/me", bob, nil); r.Code != http.StatusOK {
		t.Fatalf("another principal has its own bucket: %d", r.Code)
	}
}
