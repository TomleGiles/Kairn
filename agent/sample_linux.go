//go:build linux

package main

import (
	"io"
	"os"
	"strings"
	"syscall"
	"time"
)

// sampler relève l'état de l'hôte Linux ; les taux sont calculés entre deux relevés.
type sampler struct {
	root string // préfixe des chemins (/host dans un conteneur DaemonSet)
	prev *snapshot
}

type snapshot struct {
	at        time.Time
	cpu       cpuTimes
	disks     counters
	nets      counters
	memTotal  uint64
	memAvail  uint64
	fsUsed    uint64
	fsTotal   uint64
	hasFS     bool
	hasMemory bool
}

func (s *sampler) path(p string) string { return s.root + p }

// withFile ouvre un fichier de l'hôte, le passe à fn puis le ferme.
func (s *sampler) withFile(p string, fn func(io.Reader) error) error {
	f, err := os.Open(s.path(p))
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return fn(f)
}

func (s *sampler) read() (*snapshot, error) {
	snap := &snapshot{at: time.Now().UTC()}
	if err := s.withFile("/proc/stat", func(r io.Reader) (err error) {
		snap.cpu, err = parseCPU(r)
		return err
	}); err != nil {
		return nil, err
	}
	_ = s.withFile("/proc/meminfo", func(r io.Reader) (err error) {
		snap.memTotal, snap.memAvail, err = parseMeminfo(r)
		snap.hasMemory = err == nil
		return err
	})
	_ = s.withFile("/proc/diskstats", func(r io.Reader) error { snap.disks = parseDiskstats(r); return nil })
	_ = s.withFile("/proc/net/dev", func(r io.Reader) error { snap.nets = parseNetDev(r); return nil })
	var st syscall.Statfs_t
	root := s.root
	if root == "" {
		root = "/"
	}
	if syscall.Statfs(root, &st) == nil {
		bs := uint64(st.Bsize) //nolint:gosec // taille de bloc positive
		snap.fsTotal = st.Blocks * bs
		snap.fsUsed = (st.Blocks - st.Bfree) * bs
		snap.hasFS = true
	}
	return snap, nil
}

// Sample renvoie les métriques de l'intervalle écoulé depuis le relevé précédent.
func (s *sampler) Sample() ([]point, error) {
	cur, err := s.read()
	if err != nil {
		return nil, err
	}
	prev := s.prev
	s.prev = cur
	var pts []point
	add := func(name string, v float64) { pts = append(pts, point{name: name, ts: cur.at, value: v}) }
	if cur.hasMemory {
		used := cur.memTotal - cur.memAvail
		add("system.memory.utilization", clamp(float64(used)/float64(cur.memTotal)))
		add("kairn.memory.usage_bytes", float64(used))
	}
	if cur.hasFS {
		add("system.filesystem.usage", float64(cur.fsUsed))
	}
	if prev == nil {
		return pts, nil // les taux nécessitent deux relevés
	}
	dt := cur.at.Sub(prev.at).Seconds()
	if u, ok := cpuUtil(prev.cpu, cur.cpu); ok {
		add("system.cpu.utilization", u)
		add("kairn.cpu.usage_cores", u*float64(numCPU()))
	}
	r, w := rate(prev.disks, cur.disks, dt)
	add("system.disk.operations.rate", r+w)
	rx, tx := rate(prev.nets, cur.nets, dt)
	add("system.network.io.receive.rate", rx)
	add("system.network.io.transmit.rate", tx)
	return pts, nil
}

// Host décrit l'hôte pour l'inventaire.
func (s *sampler) Host() (HostInfo, error) {
	h := HostInfo{OS: "linux"}
	if b, err := os.ReadFile(s.path("/etc/machine-id")); err == nil {
		h.HostID = strings.TrimSpace(string(b))
	}
	_ = s.withFile("/etc/os-release", func(r io.Reader) error {
		if name := parseOSRelease(r); name != "" {
			h.OS = name
		}
		return nil
	})
	snap, err := s.read()
	if err != nil {
		return h, err
	}
	h.RAMGB = float64(snap.memTotal) / (1 << 30)
	h.DiskGB = float64(snap.fsTotal) / (1 << 30)
	return h, nil
}
