// Package memtsdb implémente tsdb.TSDB en mémoire (tests et mode démo).
package memtsdb

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/tenancy"
	"github.com/kairn-io/kairn/pkg/tsdb"
)

type seriesKey struct{ resource, metric string }

// DB est la base mémoire.
type DB struct {
	mu      sync.RWMutex
	metrics map[string]map[seriesKey][]tsdb.Point     // org → série → points triés
	costs   map[string]map[time.Time][]model.CostLine // org → jour → lignes
	billing map[string][]model.BillingLine            // org → lignes
	events  map[string][]model.Event                  // org → événements triés
	uptime  map[string][]model.UptimeResult           // org → résultats
	units   map[string]map[string]map[time.Time]decimal.Decimal
}

// New crée une base vide.
func New() *DB {
	return &DB{
		metrics: map[string]map[seriesKey][]tsdb.Point{},
		costs:   map[string]map[time.Time][]model.CostLine{},
		billing: map[string][]model.BillingLine{},
		events:  map[string][]model.Event{},
		uptime:  map[string][]model.UptimeResult{},
		units:   map[string]map[string]map[time.Time]decimal.Decimal{},
	}
}

var _ tsdb.TSDB = (*DB)(nil)

func (d *DB) WriteMetrics(ctx context.Context, pts []model.MetricPoint) error {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	m := d.metrics[org]
	if m == nil {
		m = map[seriesKey][]tsdb.Point{}
		d.metrics[org] = m
	}
	touched := map[seriesKey]bool{}
	for _, p := range pts {
		k := seriesKey{p.ResourceID, p.Metric}
		m[k] = append(m[k], tsdb.Point{TS: p.TS.UTC(), Value: p.Value})
		touched[k] = true
	}
	// Tri et déduplication (dernier écrit gagne) : ingestion idempotente.
	for k := range touched {
		s := m[k]
		sort.SliceStable(s, func(i, j int) bool { return s[i].TS.Before(s[j].TS) })
		out := s[:0]
		for i, p := range s {
			if i+1 < len(s) && s[i+1].TS.Equal(p.TS) {
				continue
			}
			out = append(out, p)
		}
		m[k] = out
	}
	return nil
}

func (d *DB) QueryMetrics(ctx context.Context, q tsdb.MetricQuery) ([]tsdb.Series, error) {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return nil, err
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	var out []tsdb.Series
	for k, pts := range d.metrics[org] {
		if len(q.ResourceIDs) > 0 && !has(q.ResourceIDs, k.resource) {
			continue
		}
		if len(q.Metrics) > 0 && !has(q.Metrics, k.metric) {
			continue
		}
		lo := sort.Search(len(pts), func(i int) bool { return !pts[i].TS.Before(q.From) })
		var sel []tsdb.Point
		for i := lo; i < len(pts) && pts[i].TS.Before(q.To); i++ {
			sel = append(sel, pts[i])
		}
		if len(sel) == 0 {
			continue
		}
		if q.Step > 0 {
			sel = bucket(sel, q.Step, q.Agg)
		}
		out = append(out, tsdb.Series{ResourceID: k.resource, Metric: k.metric, Points: sel})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ResourceID != out[j].ResourceID {
			return out[i].ResourceID < out[j].ResourceID
		}
		return out[i].Metric < out[j].Metric
	})
	return out, nil
}

func bucket(pts []tsdb.Point, step time.Duration, agg string) []tsdb.Point {
	var out []tsdb.Point
	var cur time.Time
	var vals []float64
	flush := func() {
		if len(vals) > 0 {
			out = append(out, tsdb.Point{TS: cur, Value: tsdb.Aggregate(vals, agg)})
		}
	}
	for _, p := range pts {
		b := p.TS.Truncate(step)
		if !b.Equal(cur) {
			flush()
			cur, vals = b, vals[:0]
		}
		vals = append(vals, p.Value)
	}
	flush()
	return out
}

func has(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Rollup est sans objet en mémoire : les agrégats sont calculés à la volée.
func (d *DB) Rollup(context.Context, time.Time, time.Time) error { return nil }

func (d *DB) ReplaceCostLines(ctx context.Context, day time.Time, lines []model.CostLine) error {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return err
	}
	day = tsdb.TruncDay(day)
	cp := make([]model.CostLine, 0, len(lines))
	for _, l := range lines {
		if l.OrgID == "" {
			l.OrgID = org
		}
		if l.OrgID != org {
			return tenancy.ErrCrossOrg
		}
		l.Day = day
		cp = append(cp, l)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.costs[org] == nil {
		d.costs[org] = map[time.Time][]model.CostLine{}
	}
	d.costs[org][day] = cp
	return nil
}

func matchLine(l model.CostLine, q tsdb.CostQuery, nodeSet map[string]bool) bool {
	if nodeSet != nil && !nodeSet[l.AllocationNodeID] {
		return false
	}
	for dim, vals := range q.Filters {
		if len(vals) > 0 && !has(vals, tsdb.DimValue(l, dim)) {
			return false
		}
	}
	return true
}

func (d *DB) scan(ctx context.Context, q tsdb.CostQuery, fn func(model.CostLine)) error {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return err
	}
	if err := q.Validate(); err != nil {
		return err
	}
	var nodeSet map[string]bool
	if q.NodeIDs != nil {
		nodeSet = map[string]bool{}
		for _, n := range q.NodeIDs {
			nodeSet[n] = true
		}
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	days := make([]time.Time, 0, len(d.costs[org]))
	for day := range d.costs[org] {
		if !day.Before(tsdb.TruncDay(q.From)) && day.Before(q.To) {
			days = append(days, day)
		}
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })
	for _, day := range days {
		for _, l := range d.costs[org][day] {
			if matchLine(l, q, nodeSet) {
				fn(l)
			}
		}
	}
	return nil
}

func (d *DB) QueryCosts(ctx context.Context, q tsdb.CostQuery) ([]tsdb.CostRow, error) {
	type acc struct {
		row tsdb.CostRow
	}
	groups := map[string]*acc{}
	var order []string
	err := d.scan(ctx, q, func(l model.CostLine) {
		period := tsdb.PeriodStart(l.Day, q.Granularity)
		keys := map[string]string{}
		parts := []string{period.Format(time.RFC3339), l.Currency}
		for _, dim := range q.GroupBy {
			v := tsdb.DimValue(l, dim)
			keys[dim] = v
			parts = append(parts, dim+"="+v)
		}
		k := strings.Join(parts, "\x1f")
		g, ok := groups[k]
		if !ok {
			g = &acc{row: tsdb.CostRow{Period: period, Keys: keys, Amount: decimal.Zero, Currency: l.Currency}}
			groups[k] = g
			order = append(order, k)
		}
		g.row.Amount = g.row.Amount.Add(l.Amount)
	})
	if err != nil {
		return nil, err
	}
	out := make([]tsdb.CostRow, 0, len(order))
	for _, k := range order {
		out = append(out, groups[k].row)
	}
	sortRows(out, q.GroupBy)
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

// sortRows ordonne par période puis montant décroissant puis clés (déterministe).
func sortRows(rows []tsdb.CostRow, dims []string) {
	sort.SliceStable(rows, func(i, j int) bool {
		if !rows[i].Period.Equal(rows[j].Period) {
			return rows[i].Period.Before(rows[j].Period)
		}
		if c := rows[i].Amount.Cmp(rows[j].Amount); c != 0 {
			return c > 0
		}
		for _, dim := range dims {
			if rows[i].Keys[dim] != rows[j].Keys[dim] {
				return rows[i].Keys[dim] < rows[j].Keys[dim]
			}
		}
		return false
	})
}

func (d *DB) CostLines(ctx context.Context, q tsdb.CostQuery) ([]model.CostLine, error) {
	var out []model.CostLine
	err := d.scan(ctx, q, func(l model.CostLine) {
		if q.Limit <= 0 || len(out) < q.Limit {
			out = append(out, l)
		}
	})
	return out, err
}

func (d *DB) ReplaceBillingLines(ctx context.Context, connectorID string, from, to time.Time, lines []model.BillingLine) error {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	kept := d.billing[org][:0:0]
	for _, l := range d.billing[org] {
		if l.ConnectorID == connectorID && !l.Day.Before(from) && l.Day.Before(to) {
			continue
		}
		kept = append(kept, l)
	}
	for _, l := range lines {
		l.OrgID, l.ConnectorID = org, connectorID
		l.Day = tsdb.TruncDay(l.Day)
		kept = append(kept, l)
	}
	d.billing[org] = kept
	return nil
}

func (d *DB) BillingLines(ctx context.Context, connectorID string, from, to time.Time) ([]model.BillingLine, error) {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return nil, err
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	var out []model.BillingLine
	for _, l := range d.billing[org] {
		if (connectorID == "" || l.ConnectorID == connectorID) && !l.Day.Before(from) && l.Day.Before(to) {
			out = append(out, l)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Day.Before(out[j].Day) })
	return out, nil
}

func eventKey(e model.Event) string {
	return e.TS.UTC().Format(time.RFC3339Nano) + "|" + e.Kind + "|" + e.Source + "|" + e.ResourceID + "|" + e.Title
}

func (d *DB) WriteEvents(ctx context.Context, evs []model.Event) error {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	seen := map[string]bool{}
	for _, e := range d.events[org] {
		seen[eventKey(e)] = true
	}
	for _, e := range evs {
		e.OrgID = org
		e.TS = e.TS.UTC()
		if k := eventKey(e); !seen[k] {
			seen[k] = true
			d.events[org] = append(d.events[org], e)
		}
	}
	list := d.events[org]
	sort.SliceStable(list, func(i, j int) bool { return list[i].TS.Before(list[j].TS) })
	return nil
}

func (d *DB) QueryEvents(ctx context.Context, q tsdb.EventQuery) ([]model.Event, error) {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return nil, err
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	var out []model.Event
	for _, e := range d.events[org] {
		if !q.From.IsZero() && e.TS.Before(q.From) {
			continue
		}
		if !q.To.IsZero() && !e.TS.Before(q.To) {
			continue
		}
		if len(q.Kinds) > 0 && !has(q.Kinds, e.Kind) {
			continue
		}
		if len(q.ResourceIDs) > 0 && !has(q.ResourceIDs, e.ResourceID) {
			continue
		}
		out = append(out, e)
	}
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[len(out)-q.Limit:]
	}
	return out, nil
}

func (d *DB) WriteUptime(ctx context.Context, rs []model.UptimeResult) error {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, r := range rs {
		r.OrgID = org
		d.uptime[org] = append(d.uptime[org], r)
	}
	list := d.uptime[org]
	sort.SliceStable(list, func(i, j int) bool { return list[i].TS.Before(list[j].TS) })
	return nil
}

func (d *DB) QueryUptime(ctx context.Context, q tsdb.UptimeQuery) ([]model.UptimeResult, error) {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return nil, err
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	var out []model.UptimeResult
	for _, r := range d.uptime[org] {
		if len(q.CheckIDs) > 0 && !has(q.CheckIDs, r.CheckID) {
			continue
		}
		if !q.From.IsZero() && r.TS.Before(q.From) {
			continue
		}
		if !q.To.IsZero() && !r.TS.Before(q.To) {
			continue
		}
		if q.Region != "" && r.Region != q.Region {
			continue
		}
		out = append(out, r)
	}
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[len(out)-q.Limit:]
	}
	return out, nil
}

func (d *DB) WriteUnitValues(ctx context.Context, metricID string, values map[time.Time]decimal.Decimal) error {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.units[org] == nil {
		d.units[org] = map[string]map[time.Time]decimal.Decimal{}
	}
	if d.units[org][metricID] == nil {
		d.units[org][metricID] = map[time.Time]decimal.Decimal{}
	}
	for day, v := range values {
		d.units[org][metricID][tsdb.TruncDay(day)] = v
	}
	return nil
}

func (d *DB) UnitValues(ctx context.Context, metricID string, from, to time.Time) (map[time.Time]decimal.Decimal, error) {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return nil, err
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := map[time.Time]decimal.Decimal{}
	for day, v := range d.units[org][metricID] {
		if !day.Before(from) && day.Before(to) {
			out[day] = v
		}
	}
	return out, nil
}

// PurgeOrg supprime toutes les données de l'organisation courante.
func (d *DB) PurgeOrg(ctx context.Context) error {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.metrics, org)
	delete(d.costs, org)
	delete(d.billing, org)
	delete(d.events, org)
	delete(d.uptime, org)
	delete(d.units, org)
	return nil
}
