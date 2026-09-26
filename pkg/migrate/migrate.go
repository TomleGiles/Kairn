// Package migrate applique les migrations versionnées embarquées dans le
// binaire : golang-migrate pour PostgreSQL, un exécuteur minimal (HTTP)
// pour ClickHouse. Les migrations s'exécutent avec le rôle propriétaire du
// schéma, jamais avec le rôle applicatif (CLAUDE.md §5).
package migrate

import (
	"context"
	"errors"
	"fmt"
	"strings"

	gomigrate "github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // pilote pgx5://
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/kairn-io/kairn/migrations"
)

// pgxURL convertit un DSN postgres:// en URL du pilote golang-migrate.
func pgxURL(dsn string) (string, error) {
	for _, p := range []string{"postgres://", "postgresql://"} {
		if strings.HasPrefix(dsn, p) {
			return "pgx5://" + strings.TrimPrefix(dsn, p), nil
		}
	}
	return "", errors.New("migrate: postgres DSN must start with postgres://")
}

func postgres(dsn string) (*gomigrate.Migrate, error) {
	src, err := iofs.New(migrations.Postgres, "postgres")
	if err != nil {
		return nil, fmt.Errorf("migrate: source: %w", err)
	}
	url, err := pgxURL(dsn)
	if err != nil {
		return nil, err
	}
	m, err := gomigrate.NewWithSourceInstance("iofs", src, url)
	if err != nil {
		return nil, fmt.Errorf("migrate: open: %w", err)
	}
	return m, nil
}

// PostgresUp applique toutes les migrations PostgreSQL en attente.
func PostgresUp(_ context.Context, dsn string) (uint, error) {
	m, err := postgres(dsn)
	if err != nil {
		return 0, err
	}
	defer func() { _, _ = m.Close() }()
	if err := m.Up(); err != nil && !errors.Is(err, gomigrate.ErrNoChange) {
		return 0, fmt.Errorf("migrate: up: %w", err)
	}
	v, _, err := m.Version()
	if err != nil && !errors.Is(err, gomigrate.ErrNilVersion) {
		return 0, fmt.Errorf("migrate: version: %w", err)
	}
	return v, nil
}

// PostgresDown annule la dernière migration (usage : développement).
func PostgresDown(_ context.Context, dsn string, steps int) error {
	m, err := postgres(dsn)
	if err != nil {
		return err
	}
	defer func() { _, _ = m.Close() }()
	if err := m.Steps(-steps); err != nil && !errors.Is(err, gomigrate.ErrNoChange) {
		return fmt.Errorf("migrate: down: %w", err)
	}
	return nil
}
