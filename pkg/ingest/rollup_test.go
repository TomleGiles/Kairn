package ingest_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/kairn-io/kairn/connectors/demo"
	"github.com/kairn-io/kairn/pkg/ingest"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/store/memstore"
	"github.com/kairn-io/kairn/pkg/tenancy"
	"github.com/kairn-io/kairn/pkg/tsdb"
	"github.com/kairn-io/kairn/pkg/tsdb/memtsdb"
)

// rollupRecorder enregistre les fenêtres agrégées.
type rollupRecorder struct {
	tsdb.TSDB
	mu      sync.Mutex
	windows [][2]time.Time
}

func (r *rollupRecorder) Rollup(ctx context.Context, from, to time.Time) error {
	r.mu.Lock()
	r.windows = append(r.windows, [2]time.Time{from, to})
	r.mu.Unlock()
	return r.TSDB.Rollup(ctx, from, to)
}

// Un backfill agrège chaque jour de métriques rejoué : sans cela, l'historique
// de plus de 15 jours disparaîtrait avec l'expiration des points bruts.
func TestBackfillRollsUpMetrics(t *testing.T) {
	now := time.Date(2026, 9, 20, 6, 0, 0, 0, time.UTC)
	epoch := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	st := memstore.New()
	db := &rollupRecorder{TSDB: memtsdb.New()}
	ctx := tenancy.WithOrg(context.Background(), "org-r")
	if err := st.Orgs().Create(ctx, &model.Organization{ID: "org-r", Name: "R", Slug: "r", Plan: model.PlanTeam, Currency: "EUR"}); err != nil {
		t.Fatal(err)
	}
	c := model.Connector{ID: "c-k8s", Type: demo.TypeKubernetes, Name: "prod", Enabled: true, IntervalSeconds: 900,
		Settings: map[string]string{"epoch": epoch.Format(time.RFC3339), "seed": "rollup"}}
	if err := st.Connectors().Create(ctx, &c); err != nil {
		t.Fatal(err)
	}
	sy := &ingest.Syncer{Store: st, TSDB: db, Now: func() time.Time { return now }}
	rep, err := sy.Backfill(ctx, c, epoch.AddDate(0, 0, -3))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Metrics == 0 {
		t.Fatal("backfill produced no metrics")
	}
	days := map[time.Time]bool{}
	for _, w := range db.windows {
		days[tsdb.TruncDay(w[0])] = true
	}
	for d := epoch.AddDate(0, 0, -3); d.Before(epoch); d = d.AddDate(0, 0, 1) {
		if !days[d] {
			t.Errorf("day %s not rolled up (windows: %v)", d.Format("2006-01-02"), db.windows)
		}
	}
}
