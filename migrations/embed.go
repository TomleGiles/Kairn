// Package migrations embarque les migrations SQL versionnées (PostgreSQL et
// ClickHouse) pour qu'un binaire unique puisse les appliquer (`kairn-api migrate`).
package migrations

import "embed"

// Postgres contient les migrations golang-migrate de PostgreSQL (répertoire postgres/).
//
//go:embed postgres/*.sql
var Postgres embed.FS

// ClickHouse contient les migrations ClickHouse (répertoire clickhouse/).
//
//go:embed clickhouse/*.sql
var ClickHouse embed.FS
