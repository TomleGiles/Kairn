package pgstore

import (
	"context"
	"os"
	"testing"

	"github.com/kairn-io/kairn/pkg/store/storetest"
)

// TestContract exécute la suite de contrat contre PostgreSQL (make test-int).
// KAIRN_TEST_PG_DSN doit désigner une base migrée, avec un rôle SANS BYPASSRLS
// pour que la Row-Level Security soit réellement exercée.
func TestContract(t *testing.T) {
	dsn := os.Getenv("KAIRN_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("KAIRN_TEST_PG_DSN not set (integration test)")
	}
	st, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var bypass bool
	if err := st.pool.QueryRow(context.Background(), "SELECT rolbypassrls OR rolsuper FROM pg_roles WHERE rolname = current_user").Scan(&bypass); err != nil {
		t.Fatal(err)
	}
	if bypass {
		t.Fatal("the test role bypasses RLS: use an application role (NOSUPERUSER NOBYPASSRLS)")
	}
	storetest.Run(t, st, storetest.Options{Rollback: true})
}
