package chtsdb_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/kairn-io/kairn/pkg/migrate"
	"github.com/kairn-io/kairn/pkg/tsdb/chtsdb"
	"github.com/kairn-io/kairn/pkg/tsdb/tsdbtest"
)

// TestContract exécute la suite de contrat contre un ClickHouse réel (make test-int).
// Ex. : KAIRN_TEST_CLICKHOUSE_URL=http://localhost:8123 (base « default »).
func TestContract(t *testing.T) {
	u := os.Getenv("KAIRN_TEST_CLICKHOUSE_URL")
	if u == "" {
		t.Skip("KAIRN_TEST_CLICKHOUSE_URL not set (integration test)")
	}
	c := &chtsdb.Client{URL: u, Database: os.Getenv("KAIRN_TEST_CLICKHOUSE_DB"), User: os.Getenv("KAIRN_TEST_CLICKHOUSE_USER"),
		Password: os.Getenv("KAIRN_TEST_CLICKHOUSE_PASSWORD")}
	if _, err := migrate.ClickHouseUp(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	tsdbtest.Run(t, chtsdb.New(c), time.Now())
}
