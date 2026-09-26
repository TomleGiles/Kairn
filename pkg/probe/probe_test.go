package probe

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kairn-io/kairn/pkg/gateway"
	"github.com/kairn-io/kairn/pkg/ids"
	"github.com/kairn-io/kairn/pkg/model"
)

func TestPrivateTargetsBlockedByDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	defer srv.Close()
	r := &Runner{Region: "eu-west"}
	res := r.Check(context.Background(), ids.New(), model.UptimeCheck{ID: ids.New(), Kind: "http", Target: srv.URL, ExpectedStatus: 200})
	if res.Up || !strings.Contains(res.Error, ErrForbiddenTarget.Error()) {
		t.Fatalf("loopback target must be refused: %+v", res)
	}
	for _, ip := range []string{"10.0.0.1", "192.168.1.1", "172.16.0.1", "169.254.169.254", "100.64.0.1", "::1", "fd00::1"} {
		if allowedIP(net.ParseIP(ip), false) {
			t.Fatalf("%s must be blocked", ip)
		}
	}
	if !allowedIP(net.ParseIP("51.68.1.1"), false) {
		t.Fatal("public address blocked")
	}
}

func TestHTTPAndTCPChecks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/down" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("<h1>Boutique OK</h1>"))
	}))
	defer srv.Close()
	r := &Runner{Region: "eu-west", AllowPrivate: true}
	ok := r.Check(context.Background(), "o", model.UptimeCheck{ID: "c", Kind: "http", Target: srv.URL, ExpectedStatus: 200, Keyword: "Boutique"})
	if !ok.Up || ok.StatusCode != 200 || ok.Region != "eu-west" {
		t.Fatalf("http ok: %+v", ok)
	}
	kw := r.Check(context.Background(), "o", model.UptimeCheck{ID: "c", Kind: "http", Target: srv.URL, Keyword: "absent"})
	if kw.Up || kw.Error != "keyword not found" {
		t.Fatalf("keyword: %+v", kw)
	}
	down := r.Check(context.Background(), "o", model.UptimeCheck{ID: "c", Kind: "http", Target: srv.URL + "/down"})
	if down.Up || down.StatusCode != 503 {
		t.Fatalf("down: %+v", down)
	}
	tcp := r.Check(context.Background(), "o", model.UptimeCheck{ID: "c", Kind: "tcp", Target: strings.TrimPrefix(srv.URL, "http://")})
	if !tcp.Up {
		t.Fatalf("tcp: %+v", tcp)
	}
	bad := r.Check(context.Background(), "o", model.UptimeCheck{ID: "c", Kind: "http", Target: "file:///etc/passwd"})
	if bad.Up {
		t.Fatal("non-http scheme accepted")
	}
}

func TestRunOnceHonoursIntervalsAndPostsResults(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	defer target.Close()
	org, check := ids.New(), ids.New()
	var posted []model.UptimeResult
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Kairn-Service-Token") != "svc" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/ingest/v1/probes/checks":
			_ = json.NewEncoder(w).Encode([]gateway.ProbeCheck{{OrgID: org, Check: model.UptimeCheck{ID: check, Kind: "http", Target: target.URL, IntervalSeconds: 60}}})
		case "/ingest/v1/probes/results":
			_ = json.NewDecoder(r.Body).Decode(&posted)
		}
	}))
	defer gw.Close()
	r := &Runner{GatewayURL: gw.URL, ServiceToken: "svc", Region: "eu-west", AllowPrivate: true}
	n, err := r.RunOnce(context.Background())
	if err != nil || n != 1 || len(posted) != 1 || !posted[0].Up || posted[0].OrgID != org {
		t.Fatalf("first round: %d %v %+v", n, err, posted)
	}
	if n, _ := r.RunOnce(context.Background()); n != 0 {
		t.Fatalf("check re-run before its interval")
	}
	r.ServiceToken = "wrong"
	r.lastRun = nil
	if _, err := r.RunOnce(context.Background()); err == nil || errors.Is(err, context.Canceled) {
		t.Fatal("gateway authentication error expected")
	}
}
