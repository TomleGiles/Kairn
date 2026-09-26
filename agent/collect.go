package main

import (
	"bufio"
	"errors"
	"io"
	"strconv"
	"strings"
)

// ErrUnsupported signale une plateforme sans collecte de métriques (hors Linux).
var ErrUnsupported = errors.New("metrics collection is only supported on Linux")

// cpuTimes est une ligne « cpu » de /proc/stat (jiffies cumulés).
type cpuTimes struct{ idle, total uint64 }

// parseCPU lit la ligne agrégée « cpu » de /proc/stat.
func parseCPU(r io.Reader) (cpuTimes, error) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 5 || f[0] != "cpu" {
			continue
		}
		var t cpuTimes
		for i, v := range f[1:] {
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				return t, err
			}
			t.total += n
			if i == 3 || i == 4 { // idle, iowait
				t.idle += n
			}
		}
		return t, nil
	}
	return cpuTimes{}, errors.New("/proc/stat: no cpu line")
}

// cpuUtil calcule l'utilisation entre deux relevés (0..1).
func cpuUtil(prev, cur cpuTimes) (float64, bool) {
	dt := float64(cur.total - prev.total)
	if cur.total <= prev.total || dt == 0 {
		return 0, false
	}
	busy := dt - float64(cur.idle-prev.idle)
	return clamp(busy / dt), true
}

// parseMeminfo renvoie MemTotal et MemAvailable en octets.
func parseMeminfo(r io.Reader) (total, available uint64, err error) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 2 {
			continue
		}
		v, perr := strconv.ParseUint(f[1], 10, 64)
		if perr != nil {
			continue
		}
		switch f[0] {
		case "MemTotal:":
			total = v * 1024
		case "MemAvailable:":
			available = v * 1024
		}
	}
	if total == 0 {
		return 0, 0, errors.New("/proc/meminfo: MemTotal missing")
	}
	return total, available, sc.Err()
}

// counters regroupe des compteurs cumulés par nom (disques, interfaces).
type counters map[string][2]uint64

// physicalDisk garde les disques entiers (pas les partitions ni les périphériques virtuels).
func physicalDisk(name string) bool {
	switch {
	case strings.HasPrefix(name, "nvme"):
		return strings.Contains(name, "n") && !strings.Contains(name[strings.Index(name, "n")+1:], "p")
	case strings.HasPrefix(name, "sd"), strings.HasPrefix(name, "vd"), strings.HasPrefix(name, "xvd"):
		last := name[len(name)-1]
		return last < '0' || last > '9'
	}
	return false
}

// parseDiskstats renvoie, par disque physique, les opérations de lecture et d'écriture terminées.
func parseDiskstats(r io.Reader) counters {
	out := counters{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 8 || !physicalDisk(f[2]) {
			continue
		}
		reads, _ := strconv.ParseUint(f[3], 10, 64)
		writes, _ := strconv.ParseUint(f[7], 10, 64)
		out[f[2]] = [2]uint64{reads, writes}
	}
	return out
}

// virtualIface exclut les interfaces locales et virtuelles (conteneurs, ponts).
func virtualIface(name string) bool {
	for _, p := range []string{"lo", "veth", "docker", "br-", "cali", "flannel", "cni", "virbr", "tun", "kube"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// parseNetDev renvoie, par interface physique, les octets reçus et émis.
func parseNetDev(r io.Reader) counters {
	out := counters{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		f := strings.Fields(rest)
		if len(f) < 9 || virtualIface(name) {
			continue
		}
		rx, _ := strconv.ParseUint(f[0], 10, 64)
		tx, _ := strconv.ParseUint(f[8], 10, 64)
		out[name] = [2]uint64{rx, tx}
	}
	return out
}

// rate additionne les deltas positifs de deux relevés, divisés par la durée (compteurs remis à zéro ignorés).
func rate(prev, cur counters, seconds float64) (a, b float64) {
	if seconds <= 0 {
		return 0, 0
	}
	for k, c := range cur {
		p, ok := prev[k]
		if !ok {
			continue
		}
		if c[0] >= p[0] {
			a += float64(c[0] - p[0])
		}
		if c[1] >= p[1] {
			b += float64(c[1] - p[1])
		}
	}
	return a / seconds, b / seconds
}

func clamp(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	}
	return v
}

// parseOSRelease renvoie PRETTY_NAME de /etc/os-release.
func parseOSRelease(r io.Reader) string {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "PRETTY_NAME="); ok {
			return strings.Trim(v, `"`)
		}
	}
	return ""
}
