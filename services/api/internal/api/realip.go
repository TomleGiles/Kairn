package api

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// trustedProxies contient les réseaux des proxys autorisés à transmettre l'IP
// cliente (ingress, répartiteur). Hors de ces réseaux, X-Forwarded-For est ignoré :
// sinon un client pourrait choisir son IP (journal d'audit, limitation de débit).
type trustedProxies []netip.Prefix

func parseTrustedProxies(cidrs []string) trustedProxies {
	var out trustedProxies
	for _, c := range cidrs {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if !strings.Contains(c, "/") {
			if a, err := netip.ParseAddr(c); err == nil {
				out = append(out, netip.PrefixFrom(a, a.BitLen()))
			}
			continue
		}
		if p, err := netip.ParsePrefix(c); err == nil {
			out = append(out, p.Masked())
		}
	}
	return out
}

func (t trustedProxies) contains(a netip.Addr) bool {
	for _, p := range t {
		if p.Contains(a.Unmap()) {
			return true
		}
	}
	return false
}

// clientIP renvoie l'IP du client (sans port). X-Forwarded-For n'est lu que si la
// connexion vient d'un proxy de confiance ; on le parcourt de droite à gauche et on
// retient la première adresse qui n'est pas elle-même un proxy de confiance.
func (t trustedProxies) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	remote, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	if !t.contains(remote) {
		return remote.Unmap().String()
	}
	hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break
		}
		if !t.contains(a) {
			return a.Unmap().String()
		}
	}
	return remote.Unmap().String()
}

// realIP remplace RemoteAddr par l'IP cliente résolue (utilisée par l'audit et la limitation de débit).
func (s *Server) realIP(next http.Handler) http.Handler {
	proxies := parseTrustedProxies(s.Config.TrustedProxies)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.RemoteAddr = proxies.clientIP(r)
		next.ServeHTTP(w, r)
	})
}
