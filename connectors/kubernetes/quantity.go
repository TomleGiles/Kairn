package kubernetes

import (
	"strconv"
	"strings"
)

var suffixes = []struct {
	s string
	m float64
}{
	{"Ki", 1 << 10}, {"Mi", 1 << 20}, {"Gi", 1 << 30}, {"Ti", 1 << 40}, {"Pi", 1 << 50}, {"Ei", 1 << 60},
	{"n", 1e-9}, {"u", 1e-6}, {"m", 1e-3}, {"k", 1e3}, {"K", 1e3}, {"M", 1e6}, {"G", 1e9}, {"T", 1e12}, {"P", 1e15}, {"E", 1e18},
}

// ParseQuantity convertit une quantité Kubernetes (« 250m », « 1.5 », « 512Mi », « 1e3 ») en nombre.
// Une valeur illisible vaut 0. Les quantités mesurées (cœurs, octets) ne sont
// pas monétaires : un flottant convient.
func ParseQuantity(q string) float64 {
	q = strings.TrimSpace(q)
	if q == "" {
		return 0
	}
	mult := 1.0
	for _, sfx := range suffixes {
		if strings.HasSuffix(q, sfx.s) {
			// « 1e3 » est une notation exponentielle, pas le suffixe « E ».
			rest := strings.TrimSuffix(q, sfx.s)
			if _, err := strconv.ParseFloat(rest, 64); err == nil {
				q, mult = rest, sfx.m
				break
			}
		}
	}
	v, err := strconv.ParseFloat(q, 64)
	if err != nil {
		return 0
	}
	return v * mult
}
