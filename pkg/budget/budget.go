// Package budget calcule l'état des budgets (M-08) : consommation de la
// période courante et prévision de fin de période.
package budget

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/allocation"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/tsdb"
)

// Périodes de budget.
const (
	Monthly   = "monthly"
	Quarterly = "quarterly"
	Yearly    = "yearly"
)

// Bounds renvoie [début, fin) de la période contenant now (UTC).
func Bounds(period string, now time.Time) (time.Time, time.Time, error) {
	now = now.UTC()
	switch period {
	case Monthly:
		s := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		return s, s.AddDate(0, 1, 0), nil
	case Quarterly:
		q := (int(now.Month()) - 1) / 3
		s := time.Date(now.Year(), time.Month(q*3+1), 1, 0, 0, 0, 0, time.UTC)
		return s, s.AddDate(0, 3, 0), nil
	case Yearly:
		s := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
		return s, s.AddDate(1, 0, 0), nil
	}
	return time.Time{}, time.Time{}, fmt.Errorf("budget: unknown period %q", period)
}

// Forecast projette le coût de fin de période : réel à date + moyenne des
// derniers jours complets × jours restants. daily contient les coûts journaliers
// de la période (jours complets uniquement), dans l'ordre.
func Forecast(actual decimal.Decimal, daily []decimal.Decimal, today, end time.Time) decimal.Decimal {
	remaining := int(end.Sub(tsdb.TruncDay(today)) / (24 * time.Hour))
	if remaining <= 0 || len(daily) == 0 {
		return actual
	}
	window := daily
	if len(window) > 7 {
		window = window[len(window)-7:]
	}
	sum := decimal.Zero
	for _, d := range window {
		sum = sum.Add(d)
	}
	avg := sum.Div(decimal.NewFromInt(int64(len(window))))
	return actual.Add(avg.Mul(decimal.NewFromInt(int64(remaining))))
}

// Evaluate calcule l'état d'un budget. tree sert à inclure les sous-nœuds.
func Evaluate(ctx context.Context, db tsdb.TSDB, b model.Budget, tree *allocation.Tree, now time.Time) (model.BudgetStatus, error) {
	start, end, err := Bounds(b.Period, now)
	if err != nil {
		return model.BudgetStatus{}, err
	}
	q := tsdb.CostQuery{From: start, To: end, Granularity: tsdb.GranDay}
	if b.NodeID != nil && *b.NodeID != "" {
		q.NodeIDs = tree.Subtree(*b.NodeID)
	}
	rows, err := db.QueryCosts(ctx, q)
	if err != nil {
		return model.BudgetStatus{}, err
	}
	today := tsdb.TruncDay(now)
	actual := decimal.Zero
	byDay := map[time.Time]decimal.Decimal{}
	for _, r := range rows {
		actual = actual.Add(r.Amount)
		byDay[r.Period] = byDay[r.Period].Add(r.Amount)
	}
	var daily []decimal.Decimal
	for d := start; d.Before(today) && d.Before(end); d = d.AddDate(0, 0, 1) {
		if v, ok := byDay[d]; ok {
			daily = append(daily, v)
		}
	}
	// Le jour courant, incomplet, est exclu de la base de prévision.
	complete := actual.Sub(byDay[today])
	forecast := Forecast(complete, daily, today, end)
	if forecast.LessThan(actual) {
		forecast = actual
	}
	st := model.BudgetStatus{
		Budget: b, PeriodStart: start, PeriodEnd: end, Currency: b.Currency,
		Actual: actual.Round(2), Forecast: forecast.Round(2),
	}
	if b.Amount.IsPositive() {
		hundred := decimal.NewFromInt(100)
		st.ActualPercent = actual.Div(b.Amount).Mul(hundred).Round(1)
		st.ForecastPercent = forecast.Div(b.Amount).Mul(hundred).Round(1)
	}
	return st, nil
}

// CrossedThresholds renvoie les seuils (en %) franchis par une valeur.
func CrossedThresholds(thresholds []int, percent decimal.Decimal) []int {
	var out []int
	for _, t := range thresholds {
		if percent.GreaterThanOrEqual(decimal.NewFromInt(int64(t))) {
			out = append(out, t)
		}
	}
	return out
}
