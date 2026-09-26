package migrate

import (
	"testing"

	"github.com/kairn-io/kairn/migrations"
)

func TestSplitStatements(t *testing.T) {
	got := splitStatements("-- commentaire\nCREATE TABLE a (x UInt8)\nENGINE = Memory;\n\nCREATE TABLE b (y String) ENGINE = Memory;\n")
	if len(got) != 2 || got[0] != "CREATE TABLE a (x UInt8)\nENGINE = Memory" {
		t.Fatalf("%q", got)
	}
}

func TestEmbeddedMigrations(t *testing.T) {
	ch, err := chMigrations(migrations.ClickHouse)
	if err != nil || len(ch) == 0 || ch[0].version != 1 {
		t.Fatalf("clickhouse migrations: %v %+v", err, ch)
	}
	if stmts := splitStatements(ch[0].sql); len(stmts) != 8 {
		t.Fatalf("clickhouse init statements: %d", len(stmts))
	}
	if _, err := pgxURL("postgres://u:p@h:5432/db?sslmode=disable"); err != nil {
		t.Fatal(err)
	}
	if _, err := pgxURL("mysql://x"); err == nil {
		t.Fatal("non-postgres DSN accepted")
	}
}
