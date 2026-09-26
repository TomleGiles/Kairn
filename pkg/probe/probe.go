// Package probe exécute les sondes d'uptime (M-05) depuis une région : il
// récupère les contrôles à exécuter auprès de la passerelle d'ingestion, les
// exécute (HTTP, TCP, ICMP) et renvoie les résultats.
//
// Sécurité : les cibles sont fournies par les clients. Par défaut, toute
// connexion vers une adresse privée, de bouclage, lien-local ou multicast est
// refusée au moment de la connexion (après résolution DNS, donc sans contournement
// par rebinding), pour empêcher l'usage des sondes contre le réseau interne (SSRF).
package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"

	"github.com/kairn-io/kairn/pkg/gateway"
	"github.com/kairn-io/kairn/pkg/model"
)

// ErrForbiddenTarget signale une cible interdite (réseau privé).
var ErrForbiddenTarget = errors.New("probe: target address not allowed")

// Runner exécute les sondes d'une région.
type Runner struct {
	GatewayURL   string // ex. https://ingest.kairn.example
	ServiceToken string
	Region       string
	// AllowPrivate autorise les cibles privées (édition self-hosted sondant un réseau interne).
	AllowPrivate bool
	Concurrency  int
	Log          *slog.Logger
	Now          func() time.Time

	mu      sync.Mutex
	lastRun map[string]time.Time
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

func (r *Runner) log() *slog.Logger {
	if r.Log != nil {
		return r.Log
	}
	return slog.Default()
}

// allowedIP indique si une adresse peut être sondée.
func allowedIP(ip net.IP, allowPrivate bool) bool {
	if allowPrivate {
		return !ip.IsUnspecified()
	}
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() ||
		ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() || isCGNAT(ip))
}

func isCGNAT(ip net.IP) bool {
	v4 := ip.To4()
	return v4 != nil && v4[0] == 100 && v4[1]&0xc0 == 64 // 100.64.0.0/10
}

// dialer refuse les adresses interdites au moment de la connexion.
func (r *Runner) dialer(timeout time.Duration) *net.Dialer {
	return &net.Dialer{Timeout: timeout, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip := net.ParseIP(host)
		if ip == nil || !allowedIP(ip, r.AllowPrivate) {
			return ErrForbiddenTarget
		}
		return nil
	}}
}

// Check exécute une sonde et renvoie son résultat.
func (r *Runner) Check(ctx context.Context, org string, c model.UptimeCheck) model.UptimeResult {
	timeout := time.Duration(c.TimeoutMS) * time.Millisecond
	if timeout <= 0 || timeout > 60*time.Second {
		timeout = 10 * time.Second
	}
	res := model.UptimeResult{OrgID: org, CheckID: c.ID, Region: r.Region, TS: r.now()}
	start := time.Now()
	var err error
	switch c.Kind {
	case "http":
		res.StatusCode, err = r.httpCheck(ctx, c, timeout)
	case "tcp":
		err = r.tcpCheck(ctx, c.Target, timeout)
	case "icmp":
		err = r.icmpCheck(ctx, c.Target, timeout)
	default:
		err = fmt.Errorf("unknown check kind %q", c.Kind)
	}
	res.LatencyMS = float64(time.Since(start).Microseconds()) / 1000
	res.Up = err == nil
	if err != nil {
		res.Error = truncate(err.Error(), 300)
	}
	return res
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func (r *Runner) httpCheck(ctx context.Context, c model.UptimeCheck, timeout time.Duration) (int, error) {
	u, err := url.Parse(c.Target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return 0, errors.New("invalid http target")
	}
	tr := &http.Transport{DialContext: r.dialer(timeout).DialContext, TLSHandshakeTimeout: timeout, DisableKeepAlives: true,
		ResponseHeaderTimeout: timeout, Proxy: nil}
	client := &http.Client{Transport: tr, Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "Kairn-Uptime/1.0 (+https://kairn.io)")
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	want := c.ExpectedStatus
	if want == 0 {
		want = http.StatusOK
	}
	if resp.StatusCode != want {
		return resp.StatusCode, fmt.Errorf("unexpected status %d (expected %d)", resp.StatusCode, want)
	}
	if c.Keyword != "" && !bytes.Contains(body, []byte(c.Keyword)) {
		return resp.StatusCode, errors.New("keyword not found")
	}
	return resp.StatusCode, nil
}

func (r *Runner) tcpCheck(ctx context.Context, target string, timeout time.Duration) error {
	if _, _, err := net.SplitHostPort(target); err != nil {
		return errors.New("tcp target must be host:port")
	}
	conn, err := r.dialer(timeout).DialContext(ctx, "tcp", target)
	if err != nil {
		return err
	}
	return conn.Close()
}

// icmpCheck envoie un écho ICMP non privilégié (socket « udp4 » ; sous Linux,
// net.ipv4.ping_group_range doit inclure le groupe du processus).
func (r *Runner) icmpCheck(ctx context.Context, target string, timeout time.Duration) error {
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", target)
	if err != nil || len(ips) == 0 {
		return fmt.Errorf("resolve %s: %w", target, err)
	}
	ip := ips[0]
	if !allowedIP(ip, r.AllowPrivate) {
		return ErrForbiddenTarget
	}
	conn, err := icmp.ListenPacket("udp4", "0.0.0.0")
	if err != nil {
		return fmt.Errorf("icmp unavailable: %w", err)
	}
	defer func() { _ = conn.Close() }()
	msg := icmp.Message{Type: ipv4.ICMPTypeEcho, Code: 0, Body: &icmp.Echo{ID: 0, Seq: 1, Data: []byte("kairn-uptime")}}
	b, err := msg.Marshal(nil)
	if err != nil {
		return err
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.WriteTo(b, &net.UDPAddr{IP: ip}); err != nil {
		return err
	}
	buf := make([]byte, 1500)
	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			return err
		}
		reply, err := icmp.ParseMessage(1, buf[:n])
		if err == nil && reply.Type == ipv4.ICMPTypeEchoReply {
			return nil
		}
	}
}

// due indique si la sonde doit s'exécuter maintenant (selon son intervalle).
func (r *Runner) due(c model.UptimeCheck, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lastRun == nil {
		r.lastRun = map[string]time.Time{}
	}
	every := time.Duration(c.IntervalSeconds) * time.Second
	if every < 30*time.Second {
		every = 30 * time.Second
	}
	if now.Sub(r.lastRun[c.ID]) < every {
		return false
	}
	r.lastRun[c.ID] = now
	return true
}

func (r *Runner) call(ctx context.Context, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(r.GatewayURL, "/")+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("X-Kairn-Service-Token", r.ServiceToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("probe: gateway %s %d: %s", path, resp.StatusCode, msg)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// RunOnce récupère les sondes, exécute celles qui sont dues et publie les résultats.
func (r *Runner) RunOnce(ctx context.Context) (int, error) {
	var checks []gateway.ProbeCheck
	if err := r.call(ctx, http.MethodGet, "/ingest/v1/probes/checks?region="+url.QueryEscape(r.Region), nil, &checks); err != nil {
		return 0, err
	}
	now := r.now()
	var due []gateway.ProbeCheck
	for _, pc := range checks {
		if r.due(pc.Check, now) {
			due = append(due, pc)
		}
	}
	if len(due) == 0 {
		return 0, nil
	}
	conc := r.Concurrency
	if conc <= 0 {
		conc = 32
	}
	results := make([]model.UptimeResult, len(due))
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	for i, pc := range due {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, pc gateway.ProbeCheck) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = r.Check(ctx, pc.OrgID, pc.Check)
		}(i, pc)
	}
	wg.Wait()
	return len(results), r.call(ctx, http.MethodPost, "/ingest/v1/probes/results", results, nil)
}

// Run exécute la boucle de sondage jusqu'à l'annulation du contexte.
func (r *Runner) Run(ctx context.Context) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		if n, err := r.RunOnce(ctx); err != nil {
			r.log().Warn("probe round failed", "region", r.Region, "err", err)
		} else if n > 0 {
			r.log().Debug("probe round", "region", r.Region, "checks", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
