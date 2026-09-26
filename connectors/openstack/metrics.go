package openstack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/kairn-io/kairn/connectors/internal/rest"
	"github.com/kairn-io/kairn/pkg/connector"
	"github.com/kairn-io/kairn/pkg/model"
)

// SyncMetrics lit l'utilisation des instances dans Gnocchi (CPU, mémoire)
// et la volumétrie des conteneurs Swift. Sans Gnocchi, l'utilisation vient
// du connecteur Prometheus (node_exporter).
func (c *Conn) SyncMetrics(ctx context.Context, w connector.TimeWindow) (<-chan connector.MetricPoint, error) {
	if c.s.cfg.metrics == "none" {
		return nil, connector.ErrNotSupported
	}
	if err := c.s.authenticate(ctx); err != nil {
		return nil, err
	}
	gnocchi, hasGnocchi := c.s.endpoint("metric")
	if c.s.cfg.metrics == "gnocchi" && !hasGnocchi {
		return nil, errors.New("gnocchi requested but no metric endpoint in the service catalog")
	}
	return connector.Stream(ctx, 1024, func(ctx context.Context, emit func(connector.MetricPoint) bool) error {
		if hasGnocchi {
			var servers []server
			if err := c.servers(ctx, func(sv server) bool { servers = append(servers, sv); return true }); err != nil {
				return err
			}
			for _, sv := range servers {
				if err := c.instanceMetrics(ctx, gnocchi, sv, w, emit); err != nil {
					return err
				}
			}
		}
		return c.containerMetrics(ctx, w, emit)
	}), nil
}

type measure [3]json.RawMessage // [horodatage, granularité, valeur]

func (c *Conn) measures(ctx context.Context, base, metricID, aggregation string, w connector.TimeWindow) ([]measure, error) {
	q := url.Values{"start": {w.From.UTC().Format(time.RFC3339)}, "stop": {w.To.UTC().Format(time.RFC3339)},
		"granularity": {strconv.Itoa(c.s.cfg.gnocchiGranularity)}, "aggregation": {aggregation}}
	var out []measure
	if err := c.s.client(nil).Get(ctx, rest.Join(base, "v1/metric/"+url.PathEscape(metricID)+"/measures", q), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func decodeMeasure(m measure) (time.Time, float64, bool) {
	var ts string
	var v float64
	if json.Unmarshal(m[0], &ts) != nil || json.Unmarshal(m[2], &v) != nil {
		return time.Time{}, 0, false
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return time.Time{}, 0, false
	}
	return t.UTC(), v, true
}

func (c *Conn) instanceMetrics(ctx context.Context, base string, sv server, w connector.TimeWindow, emit func(connector.MetricPoint) bool) error {
	var res struct {
		Metrics map[string]string `json:"metrics"`
	}
	if err := c.s.client(nil).Get(ctx, rest.Join(base, "v1/resource/instance/"+url.PathEscape(sv.ID), nil), &res); err != nil {
		if rest.IsNotFound(err) {
			return nil // instance non suivie par Ceilometer
		}
		return fmt.Errorf("gnocchi resource %s: %w", sv.ID, err)
	}
	vcpus, ramMB := num(sv.Flavor.VCPUs), num(sv.Flavor.RAM)
	gran := float64(c.s.cfg.gnocchiGranularity)
	point := func(metric string, t time.Time, v float64) bool {
		return emit(connector.MetricPoint{ResourceType: model.TypeInstance, ResourceExternalID: sv.ID, Metric: metric, TS: t, Value: v})
	}
	if id := res.Metrics["cpu"]; id != "" {
		// « cpu » est un compteur cumulatif en nanosecondes : son taux par période donne des cœurs utilisés.
		ms, err := c.measures(ctx, base, id, "rate:mean", w)
		if err != nil && !rest.IsNotFound(err) {
			return fmt.Errorf("gnocchi cpu %s: %w", sv.ID, err)
		}
		for _, m := range ms {
			t, v, ok := decodeMeasure(m)
			if !ok || v < 0 {
				continue
			}
			cores := v / (gran * 1e9)
			if !point(model.MetricCPUUsageCores, t, round4(cores)) {
				return nil
			}
			if vcpus > 0 && !point(model.MetricCPUUtil, t, round4(clamp01(cores/vcpus))) {
				return nil
			}
		}
	}
	if id := res.Metrics["memory.usage"]; id != "" {
		ms, err := c.measures(ctx, base, id, "mean", w)
		if err != nil && !rest.IsNotFound(err) {
			return fmt.Errorf("gnocchi memory %s: %w", sv.ID, err)
		}
		for _, m := range ms {
			t, v, ok := decodeMeasure(m)
			if !ok {
				continue
			}
			if !point(model.MetricMemUsageBytes, t, v*1024*1024) {
				return nil
			}
			if ramMB > 0 && !point(model.MetricMemUtil, t, round4(clamp01(v/ramMB))) {
				return nil
			}
		}
	}
	return nil
}

// containerMetrics émet la volumétrie courante des conteneurs Swift.
func (c *Conn) containerMetrics(ctx context.Context, w connector.TimeWindow, emit func(connector.MetricPoint) bool) error {
	base, ok := c.s.endpoint("object-store")
	if !ok {
		return nil
	}
	ts := c.now()
	if ts.After(w.To) || ts.Before(w.From) {
		return nil // la volumétrie Swift n'est connue qu'au présent
	}
	var list []struct {
		Name  string      `json:"name"`
		Bytes json.Number `json:"bytes"`
	}
	if err := c.s.client(nil).Get(ctx, base+"?"+url.Values{"format": {"json"}, "limit": {"10000"}}.Encode(), &list); err != nil {
		if errors.Is(err, connector.ErrPermission) || rest.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("swift: %w", err)
	}
	for _, ct := range list {
		if !emit(connector.MetricPoint{ResourceType: model.TypeBucket, ResourceExternalID: ct.Name, Metric: model.MetricStorageBytes, TS: ts, Value: num(ct.Bytes)}) {
			return nil
		}
	}
	return nil
}

func clamp01(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	}
	return v
}

func round4(v float64) float64 {
	return float64(int64(v*10000+0.5)) / 10000
}
