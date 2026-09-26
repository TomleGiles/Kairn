package migrate

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/kairn-io/kairn/migrations"
	"github.com/kairn-io/kairn/pkg/tsdb/chtsdb"
)

// chMigration est un fichier NNNNNN_nom.up.sql.
type chMigration struct {
	version uint32
	name    string
	sql     string
}

func chMigrations(fsys fs.FS) ([]chMigration, error) {
	entries, err := fs.ReadDir(fsys, "clickhouse")
	if err != nil {
		return nil, err
	}
	var out []chMigration
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".up.sql") {
			continue
		}
		num, _, ok := strings.Cut(e.Name(), "_")
		if !ok {
			return nil, fmt.Errorf("migrate: invalid clickhouse migration name %q", e.Name())
		}
		v, err := strconv.ParseUint(num, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("migrate: invalid clickhouse migration version %q", e.Name())
		}
		b, err := fs.ReadFile(fsys, "clickhouse/"+e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, chMigration{version: uint32(v), name: e.Name(), sql: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

// splitStatements découpe un script SQL en instructions (« ; » en fin de ligne),
// en ignorant les commentaires de ligne.
func splitStatements(script string) []string {
	var out []string
	var cur strings.Builder
	for _, line := range strings.Split(script, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") || trimmed == "" {
			continue
		}
		cur.WriteString(line)
		cur.WriteString("\n")
		if strings.HasSuffix(trimmed, ";") {
			stmt := strings.TrimSuffix(strings.TrimSpace(cur.String()), ";")
			if stmt != "" {
				out = append(out, stmt)
			}
			cur.Reset()
		}
	}
	if rest := strings.TrimSpace(cur.String()); rest != "" {
		out = append(out, rest)
	}
	return out
}

// ClickHouseUp applique les migrations ClickHouse en attente et renvoie la version courante.
func ClickHouseUp(ctx context.Context, c *chtsdb.Client) (uint32, error) {
	if err := c.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version UInt32, name String, applied_at DateTime DEFAULT now())
		ENGINE = MergeTree ORDER BY version`, nil); err != nil {
		return 0, fmt.Errorf("migrate: clickhouse: %w", err)
	}
	var current uint32
	if err := c.Query(ctx, "SELECT max(version) AS v FROM schema_migrations", nil, func(r map[string]json.RawMessage) error {
		return json.Unmarshal(r["v"], &current)
	}); err != nil {
		return 0, fmt.Errorf("migrate: clickhouse version: %w", err)
	}
	all, err := chMigrations(migrations.ClickHouse)
	if err != nil {
		return 0, err
	}
	for _, m := range all {
		if m.version <= current {
			continue
		}
		for _, stmt := range splitStatements(m.sql) {
			if err := c.Exec(ctx, stmt, nil); err != nil {
				return current, fmt.Errorf("migrate: clickhouse %s: %w", m.name, err)
			}
		}
		if err := c.Exec(ctx, "INSERT INTO schema_migrations (version, name) VALUES ({v:UInt32}, {n:String})",
			chtsdb.Params{"v": m.version, "n": m.name}); err != nil {
			return current, fmt.Errorf("migrate: clickhouse record %s: %w", m.name, err)
		}
		current = m.version
	}
	return current, nil
}
