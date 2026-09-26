// Package chtsdb implémente tsdb.TSDB sur ClickHouse (≥ 24.3 : DELETE léger).
//
// Isolation : chaque requête filtre explicitement org_id = {org:UUID} ; la
// valeur vient toujours de tenancy.OrgID(ctx), jamais de l'appelant.
// Montants : Decimal(18,6), écrits en littéraux numériques exacts et relus
// en chaînes (output_format_json_quote_decimals), jamais en flottant.
package chtsdb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/ids"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/tenancy"
	"github.com/kairn-io/kairn/pkg/tsdb"
)

// Rétentions (M-05) : brut 15 jours, 5 min 90 jours, 1 h 25 mois.
const (
	RawRetention = 15 * 24 * time.Hour
	M5Retention  = 90 * 24 * time.Hour
	insertBatch  = 50_000
)

// DB est l'implémentation ClickHouse.
type DB struct {
	C   *Client
	Now func() time.Time
}

// New crée une base à partir d'un client HTTP ClickHouse.
func New(c *Client) *DB { return &DB{C: c, Now: func() time.Time { return time.Now().UTC() }} }

var _ tsdb.TSDB = (*DB)(nil)

func orgOf(ctx context.Context) (string, error) {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return "", err
	}
	if !ids.Valid(org) {
		return "", fmt.Errorf("chtsdb: invalid organization id")
	}
	return org, nil
}

func ts(t time.Time) string  { return t.UTC().Format("2006-01-02 15:04:05.000") }
func day(t time.Time) string { return tsdb.TruncDay(t).Format("2006-01-02") }
func num(d decimal.Decimal) json.Number {
	return json.Number(d.String())
}

func (d *DB) insertChunks(ctx context.Context, table string, rows []map[string]any) error {
	for start := 0; start < len(rows); start += insertBatch {
		if err := d.C.Insert(ctx, table, rows[start:min(start+insertBatch, len(rows))]); err != nil {
			return err
		}
	}
	return nil
}

// ------------------------------------------------------------------ métriques

func (d *DB) WriteMetrics(ctx context.Context, pts []model.MetricPoint) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}
	rows := make([]map[string]any, 0, len(pts))
	for _, p := range pts {
		if !ids.Valid(p.ResourceID) {
			return fmt.Errorf("chtsdb: invalid resource id %q", p.ResourceID)
		}
		rows = append(rows, map[string]any{"org_id": org, "resource_id": p.ResourceID, "metric": p.Metric, "ts": ts(p.TS), "value": p.Value})
	}
	return d.insertChunks(ctx, "metrics_raw", rows)
}

// resolution choisit la table la plus fine couvrant la fenêtre demandée.
func (d *DB) resolution(from time.Time) string {
	age := d.Now().Sub(from)
	switch {
	case age <= RawRetention:
		return "metrics_raw"
	case age <= M5Retention:
		return "metrics_5m"
	default:
		return "metrics_1h"
	}
}

// aggExpr renvoie l'expression d'agrégation pour la table choisie.
func aggExpr(table, agg string) string {
	if table == "metrics_raw" {
		switch agg {
		case tsdb.AggMax:
			return "max(value)"
		case tsdb.AggMin:
			return "min(value)"
		case tsdb.AggP95:
			return "quantileExactInclusive(0.95)(value)"
		case tsdb.AggSum:
			return "sum(value)"
		case tsdb.AggLast:
			return "argMax(value, ts)"
		default:
			return "avg(value)"
		}
	}
	// Agrégats pré-calculés : moyenne pondérée par le nombre d'échantillons ;
	// le P95 d'un intervalle est approché par le maximum des P95 (prudent pour le dimensionnement).
	switch agg {
	case tsdb.AggMax:
		return "max(max)"
	case tsdb.AggMin:
		return "min(min)"
	case tsdb.AggP95:
		return "max(p95)"
	case tsdb.AggSum:
		return "sum(avg * samples)"
	case tsdb.AggLast:
		return "argMax(avg, ts)"
	default:
		return "sum(avg * samples) / sum(samples)"
	}
}

func (d *DB) QueryMetrics(ctx context.Context, q tsdb.MetricQuery) ([]tsdb.Series, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}
	table := d.resolution(q.From)
	p := Params{"org": org, "from": ts(q.From), "to": ts(q.To)}
	where := "org_id = {org:UUID} AND ts >= {from:DateTime64(3, 'UTC')} AND ts < {to:DateTime64(3, 'UTC')}"
	if len(q.ResourceIDs) > 0 {
		valid := make([]string, 0, len(q.ResourceIDs))
		for _, id := range q.ResourceIDs {
			if ids.Valid(id) {
				valid = append(valid, id)
			}
		}
		if len(valid) == 0 {
			return nil, nil
		}
		p["rids"] = valid
		where += " AND resource_id IN {rids:Array(UUID)}"
	}
	if len(q.Metrics) > 0 {
		p["metrics"] = q.Metrics
		where += " AND metric IN {metrics:Array(String)}"
	}
	var sql string
	switch {
	case q.Step > 0:
		p["step"] = uint32(q.Step / time.Second)
		sql = "SELECT toString(resource_id) AS rid, metric, toStartOfInterval(ts, toIntervalSecond({step:UInt32}), 'UTC') AS b, " + aggExpr(table, q.Agg) +
			" AS v FROM " + table + " FINAL WHERE " + where + " GROUP BY rid, metric, b ORDER BY rid, metric, b"
	case table == "metrics_raw":
		sql = "SELECT toString(resource_id) AS rid, metric, ts AS b, value AS v FROM metrics_raw FINAL WHERE " + where + " ORDER BY rid, metric, b"
	default:
		col := "avg"
		switch q.Agg {
		case tsdb.AggMax, tsdb.AggMin, tsdb.AggP95:
			col = q.Agg
		}
		sql = "SELECT toString(resource_id) AS rid, metric, ts AS b, " + col + " AS v FROM " + table + " FINAL WHERE " + where + " ORDER BY rid, metric, b"
	}
	var out []tsdb.Series
	err = d.C.Query(ctx, sql, p, func(r map[string]json.RawMessage) error {
		var rid, metric, b string
		var v float64
		if err := decode(r, "rid", &rid, "metric", &metric, "b", &b, "v", &v); err != nil {
			return err
		}
		t, err := parseTime(b)
		if err != nil {
			return err
		}
		if n := len(out); n == 0 || out[n-1].ResourceID != rid || out[n-1].Metric != metric {
			out = append(out, tsdb.Series{ResourceID: rid, Metric: metric})
		}
		out[len(out)-1].Points = append(out[len(out)-1].Points, tsdb.Point{TS: t, Value: v})
		return nil
	})
	return out, err
}

// Rollup calcule les agrégats 5 min et 1 h des heures terminées de [from, to)
// pour l'organisation du contexte. Idempotent : ReplacingMergeTree(version)
// conserve le dernier calcul.
func (d *DB) Rollup(ctx context.Context, from, to time.Time) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}
	p := Params{"org": org, "from": ts(from.Truncate(time.Hour)), "to": ts(to.Truncate(time.Hour))}
	scope := " AND org_id = {org:UUID}"
	for _, t := range []struct{ table, bucket string }{{"metrics_5m", "toStartOfFiveMinutes(ts)"}, {"metrics_1h", "toStartOfHour(ts)"}} {
		sql := "INSERT INTO " + t.table + " (org_id, resource_id, metric, ts, avg, min, max, p95, samples, version) " +
			"SELECT org_id, resource_id, metric, toDateTime(" + t.bucket + ", 'UTC') AS b, avg(value), min(value), max(value), " +
			"quantileExactInclusive(0.95)(value), toUInt32(count()), now64(3) FROM metrics_raw FINAL " +
			"WHERE ts >= {from:DateTime64(3, 'UTC')} AND ts < {to:DateTime64(3, 'UTC')}" + scope + " GROUP BY org_id, resource_id, metric, b"
		if err := d.C.Exec(ctx, sql, p); err != nil {
			return fmt.Errorf("chtsdb: rollup %s: %w", t.table, err)
		}
	}
	return nil
}

// ------------------------------------------------------------------ coûts

func costRow(org string, l model.CostLine, computedAt string) map[string]any {
	labels := l.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	return map[string]any{
		"org_id": org, "day": day(l.Day), "resource_id": l.ResourceID, "connector_id": l.ConnectorID, "provider": l.Provider,
		"resource_type": l.ResourceType, "region": l.Region, "cost_type": l.CostType, "sku": l.SKU, "quantity": num(l.Quantity), "unit": l.Unit,
		"amount": num(l.Amount), "currency": l.Currency, "source": l.Source, "catalog_version": l.CatalogVersion,
		"allocation_node_id": l.AllocationNodeID, "source_ref": l.SourceRef, "labels": labels, "computed_at": computedAt,
	}
}

// ReplaceCostLines remplace toutes les lignes de l'organisation pour le jour donné
// (DELETE léger puis insertion : rejouable, déterministe).
func (d *DB) ReplaceCostLines(ctx context.Context, dayT time.Time, lines []model.CostLine) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}
	rows := make([]map[string]any, 0, len(lines))
	now := ts(d.Now())
	for _, l := range lines {
		if l.OrgID != "" && l.OrgID != org {
			return tenancy.ErrCrossOrg
		}
		l.Day = tsdb.TruncDay(dayT)
		rows = append(rows, costRow(org, l, now))
	}
	if err := d.C.Exec(ctx, "DELETE FROM cost_lines WHERE org_id = {org:UUID} AND day = {day:Date}", Params{"org": org, "day": day(dayT)}); err != nil {
		return fmt.Errorf("chtsdb: delete cost lines: %w", err)
	}
	return d.insertChunks(ctx, "cost_lines", rows)
}

// dimExpr traduit une dimension en expression SQL (validée par CostQuery.Validate).
func dimExpr(dim string, p Params, i int) string {
	if strings.HasPrefix(dim, "label:") {
		name := "lk" + strconv.Itoa(i)
		p[name] = strings.TrimPrefix(dim, "label:")
		return "labels[{" + name + ":String}]"
	}
	return dim // nom de colonne, validé par liste blanche (tsdb.CostDims)
}

func (d *DB) costWhere(org string, q tsdb.CostQuery, p Params) string {
	p["org"], p["from"], p["to"] = org, day(q.From), ts(q.To)
	// Même sémantique que memtsdb : jours J tels que J >= jour(From) et minuit(J) < To.
	where := "org_id = {org:UUID} AND day >= {from:Date} AND toDateTime64(day, 3, 'UTC') < {to:DateTime64(3, 'UTC')}"
	dims := make([]string, 0, len(q.Filters))
	for dim := range q.Filters {
		dims = append(dims, dim)
	}
	sort.Strings(dims)
	for i, dim := range dims {
		vals := q.Filters[dim]
		if len(vals) == 0 {
			continue
		}
		name := "f" + strconv.Itoa(i)
		p[name] = vals
		where += " AND " + dimExpr(dim, p, 100+i) + " IN {" + name + ":Array(String)}"
	}
	if q.NodeIDs != nil {
		p["nodes"] = q.NodeIDs
		where += " AND allocation_node_id IN {nodes:Array(String)}"
	}
	return where
}

func periodExpr(gran string) string {
	switch gran {
	case tsdb.GranWeek:
		return "toStartOfWeek(day, 1)"
	case tsdb.GranMonth:
		return "toStartOfMonth(day)"
	case tsdb.GranTotal:
		return "toDate('1970-01-01')"
	default:
		return "day"
	}
}

func (d *DB) QueryCosts(ctx context.Context, q tsdb.CostQuery) ([]tsdb.CostRow, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}
	if err := q.Validate(); err != nil {
		return nil, err
	}
	p := Params{}
	where := d.costWhere(org, q, p)
	sel := []string{periodExpr(q.Granularity) + " AS period", "currency"}
	group := []string{"period", "currency"}
	for i, dim := range q.GroupBy {
		alias := "g" + strconv.Itoa(i)
		sel = append(sel, dimExpr(dim, p, i)+" AS "+alias)
		group = append(group, alias)
	}
	sql := "SELECT " + strings.Join(sel, ", ") + ", toString(sum(amount)) AS total FROM cost_lines FINAL WHERE " + where +
		" GROUP BY " + strings.Join(group, ", ") + " ORDER BY period, sum(amount) DESC"
	for i := range q.GroupBy {
		sql += ", g" + strconv.Itoa(i)
	}
	if q.Limit > 0 {
		sql += " LIMIT " + strconv.Itoa(q.Limit)
	}
	var out []tsdb.CostRow
	err = d.C.Query(ctx, sql, p, func(r map[string]json.RawMessage) error {
		var period, currency, total string
		if err := decode(r, "period", &period, "currency", &currency, "total", &total); err != nil {
			return err
		}
		row := tsdb.CostRow{Keys: map[string]string{}, Currency: currency}
		if q.Granularity != tsdb.GranTotal {
			t, err := time.Parse("2006-01-02", period)
			if err != nil {
				return err
			}
			row.Period = t
		}
		amount, err := decimal.NewFromString(total)
		if err != nil {
			return err
		}
		row.Amount = amount
		for i, dim := range q.GroupBy {
			var v string
			if err := decode(r, "g"+strconv.Itoa(i), &v); err != nil {
				return err
			}
			row.Keys[dim] = v
		}
		out = append(out, row)
		return nil
	})
	return out, err
}

func (d *DB) CostLines(ctx context.Context, q tsdb.CostQuery) ([]model.CostLine, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}
	if err := q.Validate(); err != nil {
		return nil, err
	}
	p := Params{}
	sql := `SELECT toString(day) AS day, resource_id, connector_id, provider, resource_type, region, cost_type, sku, toString(quantity) AS quantity, unit,
		toString(amount) AS amount, currency, source, catalog_version, allocation_node_id, source_ref, labels
		FROM cost_lines FINAL WHERE ` + d.costWhere(org, q, p) + " ORDER BY day, resource_id, cost_type, sku, source, allocation_node_id, source_ref"
	if q.Limit > 0 {
		sql += " LIMIT " + strconv.Itoa(q.Limit)
	}
	var out []model.CostLine
	err = d.C.Query(ctx, sql, p, func(r map[string]json.RawMessage) error {
		l := model.CostLine{OrgID: org}
		var dayS, qty, amount string
		if err := decode(r, "day", &dayS, "resource_id", &l.ResourceID, "connector_id", &l.ConnectorID, "provider", &l.Provider,
			"resource_type", &l.ResourceType, "region", &l.Region, "cost_type", &l.CostType, "sku", &l.SKU, "quantity", &qty, "unit", &l.Unit,
			"amount", &amount, "currency", &l.Currency, "source", &l.Source, "catalog_version", &l.CatalogVersion,
			"allocation_node_id", &l.AllocationNodeID, "source_ref", &l.SourceRef, "labels", &l.Labels); err != nil {
			return err
		}
		var err error
		if l.Day, err = time.Parse("2006-01-02", dayS); err != nil {
			return err
		}
		if l.Quantity, err = decimal.NewFromString(qty); err != nil {
			return err
		}
		if l.Amount, err = decimal.NewFromString(amount); err != nil {
			return err
		}
		if len(l.Labels) == 0 {
			l.Labels = nil
		}
		out = append(out, l)
		return nil
	})
	return out, err
}

// ------------------------------------------------------------------ facturation

func (d *DB) ReplaceBillingLines(ctx context.Context, connectorID string, from, to time.Time, lines []model.BillingLine) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}
	if err := d.C.Exec(ctx, "DELETE FROM billing_lines WHERE org_id = {org:UUID} AND connector_id = {conn:String} AND day >= {from:Date} AND day < {to:Date}",
		Params{"org": org, "conn": connectorID, "from": day(from), "to": day(to)}); err != nil {
		return fmt.Errorf("chtsdb: delete billing lines: %w", err)
	}
	now := ts(d.Now())
	rows := make([]map[string]any, 0, len(lines))
	for _, l := range lines {
		rows = append(rows, map[string]any{
			"org_id": org, "connector_id": connectorID, "provider": l.Provider, "day": day(l.Day), "resource_id": l.ResourceID, "service": l.Service,
			"sku": l.SKU, "cost_type": l.CostType, "quantity": num(l.Quantity), "unit": l.Unit, "amount": num(l.Amount), "currency": l.Currency,
			"invoice_id": l.InvoiceID, "imported_at": now,
		})
	}
	return d.insertChunks(ctx, "billing_lines", rows)
}

func (d *DB) BillingLines(ctx context.Context, connectorID string, from, to time.Time) ([]model.BillingLine, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}
	p := Params{"org": org, "from": day(from), "to": day(to)}
	where := "org_id = {org:UUID} AND day >= {from:Date} AND day < {to:Date}"
	if connectorID != "" {
		p["conn"] = connectorID
		where += " AND connector_id = {conn:String}"
	}
	sql := `SELECT connector_id, provider, toString(day) AS day, resource_id, service, sku, cost_type, toString(quantity) AS quantity, unit,
		toString(amount) AS amount, currency, invoice_id FROM billing_lines FINAL WHERE ` + where + " ORDER BY day, resource_id, service, sku, invoice_id"
	var out []model.BillingLine
	err = d.C.Query(ctx, sql, p, func(r map[string]json.RawMessage) error {
		l := model.BillingLine{OrgID: org}
		var dayS, qty, amount string
		if err := decode(r, "connector_id", &l.ConnectorID, "provider", &l.Provider, "day", &dayS, "resource_id", &l.ResourceID, "service", &l.Service,
			"sku", &l.SKU, "cost_type", &l.CostType, "quantity", &qty, "unit", &l.Unit, "amount", &amount, "currency", &l.Currency, "invoice_id", &l.InvoiceID); err != nil {
			return err
		}
		var err error
		if l.Day, err = time.Parse("2006-01-02", dayS); err != nil {
			return err
		}
		if l.Quantity, err = decimal.NewFromString(qty); err != nil {
			return err
		}
		if l.Amount, err = decimal.NewFromString(amount); err != nil {
			return err
		}
		out = append(out, l)
		return nil
	})
	return out, err
}

// ------------------------------------------------------------------ événements

// eventID est l'empreinte d'un événement : la réécriture d'un même événement est idempotente.
func eventID(e model.Event) string {
	h := sha256.Sum256([]byte(e.TS.UTC().Format(time.RFC3339Nano) + "|" + e.Kind + "|" + e.Source + "|" + e.ResourceID + "|" + e.Title))
	return hex.EncodeToString(h[:16])
}

func (d *DB) WriteEvents(ctx context.Context, evs []model.Event) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}
	rows := make([]map[string]any, 0, len(evs))
	for _, e := range evs {
		payload := "{}"
		if len(e.Payload) > 0 {
			b, err := json.Marshal(e.Payload)
			if err != nil {
				return fmt.Errorf("chtsdb: encode event payload: %w", err)
			}
			payload = string(b)
		}
		rows = append(rows, map[string]any{"org_id": org, "ts": ts(e.TS), "event_id": eventID(e), "kind": e.Kind, "source": e.Source,
			"resource_id": e.ResourceID, "title": e.Title, "payload": payload})
	}
	return d.insertChunks(ctx, "events", rows)
}

func (d *DB) QueryEvents(ctx context.Context, q tsdb.EventQuery) ([]model.Event, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}
	p := Params{"org": org}
	where := "org_id = {org:UUID}"
	if !q.From.IsZero() {
		p["from"] = ts(q.From)
		where += " AND ts >= {from:DateTime64(3, 'UTC')}"
	}
	if !q.To.IsZero() {
		p["to"] = ts(q.To)
		where += " AND ts < {to:DateTime64(3, 'UTC')}"
	}
	if len(q.Kinds) > 0 {
		p["kinds"] = q.Kinds
		where += " AND kind IN {kinds:Array(String)}"
	}
	if len(q.ResourceIDs) > 0 {
		p["rids"] = q.ResourceIDs
		where += " AND resource_id IN {rids:Array(String)}"
	}
	// Les N plus récents, rendus dans l'ordre chronologique.
	sql := "SELECT toString(ts) AS ts, kind, source, resource_id, title, payload FROM events FINAL WHERE " + where + " ORDER BY ts DESC, event_id DESC"
	if q.Limit > 0 {
		sql += " LIMIT " + strconv.Itoa(q.Limit)
	}
	var out []model.Event
	err = d.C.Query(ctx, sql, p, func(r map[string]json.RawMessage) error {
		e := model.Event{OrgID: org}
		var tsS, payload string
		if err := decode(r, "ts", &tsS, "kind", &e.Kind, "source", &e.Source, "resource_id", &e.ResourceID, "title", &e.Title, "payload", &payload); err != nil {
			return err
		}
		t, err := parseTime(tsS)
		if err != nil {
			return err
		}
		e.TS = t
		if payload != "" && payload != "{}" {
			if err := json.Unmarshal([]byte(payload), &e.Payload); err != nil {
				return fmt.Errorf("chtsdb: decode payload: %w", err)
			}
		}
		out = append(out, e)
		return nil
	})
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, err
}

// ------------------------------------------------------------------ uptime

func (d *DB) WriteUptime(ctx context.Context, rs []model.UptimeResult) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}
	rows := make([]map[string]any, 0, len(rs))
	for _, r := range rs {
		up := 0
		if r.Up {
			up = 1
		}
		rows = append(rows, map[string]any{"org_id": org, "check_id": r.CheckID, "region": r.Region, "ts": ts(r.TS), "up": up,
			"latency_ms": r.LatencyMS, "status_code": r.StatusCode, "error": r.Error})
	}
	return d.insertChunks(ctx, "uptime_results", rows)
}

func (d *DB) QueryUptime(ctx context.Context, q tsdb.UptimeQuery) ([]model.UptimeResult, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}
	p := Params{"org": org}
	where := "org_id = {org:UUID}"
	if len(q.CheckIDs) > 0 {
		p["checks"] = q.CheckIDs
		where += " AND toString(check_id) IN {checks:Array(String)}"
	}
	if !q.From.IsZero() {
		p["from"] = ts(q.From)
		where += " AND ts >= {from:DateTime64(3, 'UTC')}"
	}
	if !q.To.IsZero() {
		p["to"] = ts(q.To)
		where += " AND ts < {to:DateTime64(3, 'UTC')}"
	}
	if q.Region != "" {
		p["region"] = q.Region
		where += " AND region = {region:String}"
	}
	sql := "SELECT toString(check_id) AS check_id, region, toString(ts) AS ts, up, latency_ms, status_code, error FROM uptime_results WHERE " + where +
		" ORDER BY ts DESC"
	if q.Limit > 0 {
		sql += " LIMIT " + strconv.Itoa(q.Limit)
	}
	var out []model.UptimeResult
	err = d.C.Query(ctx, sql, p, func(r map[string]json.RawMessage) error {
		x := model.UptimeResult{OrgID: org}
		var tsS string
		var up int
		if err := decode(r, "check_id", &x.CheckID, "region", &x.Region, "ts", &tsS, "up", &up, "latency_ms", &x.LatencyMS,
			"status_code", &x.StatusCode, "error", &x.Error); err != nil {
			return err
		}
		t, err := parseTime(tsS)
		if err != nil {
			return err
		}
		x.TS, x.Up = t, up == 1
		out = append(out, x)
		return nil
	})
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, err
}

// ------------------------------------------------------------------ métriques métier

func (d *DB) WriteUnitValues(ctx context.Context, metricID string, values map[time.Time]decimal.Decimal) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}
	if !ids.Valid(metricID) {
		return fmt.Errorf("chtsdb: invalid metric id")
	}
	now := ts(d.Now())
	rows := make([]map[string]any, 0, len(values))
	for dt, v := range values {
		rows = append(rows, map[string]any{"org_id": org, "metric_id": metricID, "day": day(dt), "value": num(v), "version": now})
	}
	return d.insertChunks(ctx, "unit_metric_values", rows)
}

func (d *DB) UnitValues(ctx context.Context, metricID string, from, to time.Time) (map[time.Time]decimal.Decimal, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}
	out := map[time.Time]decimal.Decimal{}
	if !ids.Valid(metricID) {
		return out, nil
	}
	err = d.C.Query(ctx, `SELECT toString(day) AS day, toString(value) AS value FROM unit_metric_values FINAL
		WHERE org_id = {org:UUID} AND metric_id = {mid:UUID} AND day >= {from:Date} AND day < {to:Date}`,
		Params{"org": org, "mid": metricID, "from": day(from), "to": day(to)}, func(r map[string]json.RawMessage) error {
			var dayS, v string
			if err := decode(r, "day", &dayS, "value", &v); err != nil {
				return err
			}
			t, err := time.Parse("2006-01-02", dayS)
			if err != nil {
				return err
			}
			val, err := decimal.NewFromString(v)
			if err != nil {
				return err
			}
			out[t] = val
			return nil
		})
	return out, err
}

// PurgeOrg supprime toutes les données de l'organisation courante (droit à l'effacement).
func (d *DB) PurgeOrg(ctx context.Context) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}
	for _, t := range Tables {
		if err := d.C.Exec(ctx, "DELETE FROM "+t+" WHERE org_id = {org:UUID}", Params{"org": org}); err != nil {
			return fmt.Errorf("chtsdb: purge %s: %w", t, err)
		}
	}
	return nil
}

// Tables liste les tables portant des données client.
var Tables = []string{"metrics_raw", "metrics_5m", "metrics_1h", "cost_lines", "billing_lines", "events", "uptime_results", "unit_metric_values"}

// ------------------------------------------------------------------ décodage

// decode lit des paires (colonne, cible) d'une ligne JSONEachRow.
func decode(r map[string]json.RawMessage, pairs ...any) error {
	for i := 0; i+1 < len(pairs); i += 2 {
		key := pairs[i].(string)
		raw, ok := r[key]
		if !ok {
			return fmt.Errorf("chtsdb: missing column %s", key)
		}
		if err := json.Unmarshal(raw, pairs[i+1]); err != nil {
			// Les nombres peuvent être rendus entre guillemets selon les réglages serveur.
			var s string
			if json.Unmarshal(raw, &s) == nil {
				if err2 := json.Unmarshal([]byte(s), pairs[i+1]); err2 == nil {
					continue
				}
			}
			return fmt.Errorf("chtsdb: decode %s: %w", key, err)
		}
	}
	return nil
}

func parseTime(s string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02 15:04:05.000", "2006-01-02 15:04:05", time.RFC3339Nano} {
		if t, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, errors.New("chtsdb: invalid time " + s)
}
