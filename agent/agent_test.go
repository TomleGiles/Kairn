package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	colmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/protobuf/proto"

	"github.com/kairn-io/kairn/pkg/gateway"
	"github.com/kairn-io/kairn/pkg/ids"
	"github.com/kairn-io/kairn/pkg/model"
)

func TestParsers(t *testing.T) {
	prev, err := parseCPU(strings.NewReader("cpu  100 0 100 700 100 0 0 0 0 0\ncpu0 1 2 3 4\n"))
	if err != nil {
		t.Fatal(err)
	}
	cur, _ := parseCPU(strings.NewReader("cpu  150 0 150 1400 100 0 0 0 0 0\n"))
	// 800 jiffies écoulés dont 700 d'inactivité (idle + iowait) → 12,5 % d'utilisation.
	if u, ok := cpuUtil(prev, cur); !ok || u != 0.125 {
		t.Fatalf("cpu util: %v", u)
	}
	total, avail, err := parseMeminfo(strings.NewReader("MemTotal:       16303700 kB\nMemFree:  1000 kB\nMemAvailable:    8151850 kB\n"))
	if err != nil || total != 16303700*1024 || avail != 8151850*1024 {
		t.Fatalf("meminfo: %d %d %v", total, avail, err)
	}
	d0 := parseDiskstats(strings.NewReader("   8       0 sda 1000 0 0 0 500 0 0 0 0 0 0\n   8       1 sda1 900 0 0 0 400 0 0 0 0 0 0\n 259 0 nvme0n1 10 0 0 0 10 0 0 0\n 259 1 nvme0n1p1 5 0 0 0 5 0 0 0\n   7 0 loop0 5 0 0 0 5 0 0 0\n"))
	if len(d0) != 2 || d0["sda"] != [2]uint64{1000, 500} {
		t.Fatalf("diskstats (partitions and loop devices excluded): %v", d0)
	}
	d1 := counters{"sda": {1300, 800}, "nvme0n1": {10, 10}}
	if r, w := rate(d0, d1, 10); r != 30 || w != 30 {
		t.Fatalf("disk rate: %v %v", r, w)
	}
	n := parseNetDev(strings.NewReader("Inter-|   Receive\n face |bytes packets\n    lo: 999 1 0 0 0 0 0 0 999 1 0 0 0 0 0 0\n  eth0: 1000 10 0 0 0 0 0 0 2000 20 0 0 0 0 0 0\nveth12: 5 1 0 0 0 0 0 0 5 1 0 0 0 0 0 0\n"))
	if len(n) != 1 || n["eth0"] != [2]uint64{1000, 2000} {
		t.Fatalf("net dev: %v", n)
	}
	// Compteur remis à zéro (redémarrage) : pas de taux négatif.
	if r, _ := rate(counters{"eth0": {5000, 0}}, counters{"eth0": {10, 0}}, 1); r != 0 {
		t.Fatalf("counter reset: %v", r)
	}
	if parseOSRelease(strings.NewReader("NAME=Ubuntu\nPRETTY_NAME=\"Ubuntu 24.04 LTS\"\n")) != "Ubuntu 24.04 LTS" {
		t.Fatal("os-release")
	}
}

func TestOTLPRoundTripThroughGateway(t *testing.T) {
	var got []*colmetrics.ExportMetricsServiceRequest
	var inventory string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/ingest/v1/otlp/v1/metrics":
			var req colmetrics.ExportMetricsServiceRequest
			if err := proto.Unmarshal(body, &req); err != nil {
				t.Errorf("protobuf: %v", err)
			}
			got = append(got, &req)
		case "/ingest/v1/agent/inventory":
			inventory = string(body)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	a := &Agent{gateway: srv.URL, token: "tok", http: srv.Client(), log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		host: HostInfo{HostID: "machine-1", Hostname: "db-1", Labels: map[string]string{"site": "par1"}}}
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	a.buffer = []point{{name: "system.cpu.utilization", ts: now, value: 0.42}, {name: "kairn.memory.usage_bytes", ts: now, value: 8e9}, {name: "unknown.metric", ts: now, value: 1}}
	if err := a.sendInventory(context.Background()); err != nil || !strings.Contains(inventory, `"site":"par1"`) {
		t.Fatalf("inventory: %v %s", err, inventory)
	}
	if err := a.flush(context.Background()); err != nil || len(a.buffer) != 0 || len(got) != 1 {
		t.Fatalf("flush: %v", err)
	}
	// La passerelle convertit la requête : les métriques connues sont rattachées à l'hôte.
	org, conn := ids.New(), ids.New()
	pts := gateway.ConvertOTLP(org, conn, got[0])
	if len(pts) != 2 || pts[0].ResourceID != ids.Resource(org, conn, model.TypeHost, "machine-1") || pts[0].Metric != model.MetricCPUUtil || pts[0].Value != 0.42 {
		t.Fatalf("converted: %+v", pts)
	}
	// Passerelle indisponible : le tampon est conservé.
	a.gateway = "http://127.0.0.1:1"
	a.buffer = []point{{name: "system.cpu.utilization", ts: now, value: 0.1}}
	if err := a.flush(context.Background()); err == nil || len(a.buffer) != 1 {
		t.Fatal("buffer must be kept when the gateway is unreachable")
	}
}

func TestRunRequiresSecureGateway(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	t.Setenv("KAIRN_AGENT_TOKEN", "tok")
	if err := run(log, []string{"--gateway", "http://ingest.example.com", "--once"}); err == nil {
		t.Fatal("plain http gateway accepted")
	}
	t.Setenv("KAIRN_AGENT_TOKEN", "")
	if err := run(log, []string{"--gateway", "https://ingest.example.com"}); err == nil {
		t.Fatal("missing token accepted")
	}
}
