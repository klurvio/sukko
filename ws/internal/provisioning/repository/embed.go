package repository

import "embed"

// Embedded migration files for SQLite.
// PostgreSQL migrations live in internal/shared/migrations/postgres/
// and are applied externally via Atlas.
//
//go:embed migrations/sqlite/*.sql
var sqliteMigrations embed.FS
