package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIPOnlyTrustsDeclaredProxies(t *testing.T) {
	proxies := parseTrustedProxies([]string{"10.0.0.0/8", "192.168.1.5", "garbage", ""})
	req := func(remote, xff string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = remote
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		return r
	}
	cases := []struct{ remote, xff, want string }{
		// Client direct : l'en-tête est ignoré (sinon l'IP serait falsifiable).
		{"203.0.113.7:51234", "1.2.3.4", "203.0.113.7"},
		// Derrière l'ingress de confiance : première adresse non fiable en partant de la droite.
		{"10.2.3.4:443", "1.2.3.4, 198.51.100.9", "198.51.100.9"},
		{"10.2.3.4:443", "198.51.100.9, 10.9.9.9", "198.51.100.9"},
		{"192.168.1.5:80", "198.51.100.10", "198.51.100.10"},
		// Proxy de confiance sans en-tête : l'adresse du proxy.
		{"10.2.3.4:443", "", "10.2.3.4"},
		{"[::ffff:203.0.113.7]:1", "", "203.0.113.7"},
		{"[2001:db8::1]:443", "", "2001:db8::1"},
	}
	for _, c := range cases {
		if got := proxies.clientIP(req(c.remote, c.xff)); got != c.want {
			t.Errorf("remote=%s xff=%q: got %s, want %s", c.remote, c.xff, got, c.want)
		}
	}
	// Sans proxy déclaré, le port est retiré : la limitation de débit se fait par IP, pas par connexion.
	if got := parseTrustedProxies(nil).clientIP(req("203.0.113.7:40000", "9.9.9.9")); got != "203.0.113.7" {
		t.Fatalf("no proxies: %s", got)
	}
}
