// Package migrations provides embedded PostgreSQL migration files.
// These are the shared migrations for all services (provisioning + push).
package migrations

import "embed"

// PostgresFS contains all PostgreSQL migration SQL files.
// Used by services that auto-migrate at startup via the Go-based migrator.
//
//go:embed postgres/*.sql
var PostgresFS embed.FS
