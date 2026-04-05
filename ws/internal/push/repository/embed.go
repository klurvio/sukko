package repository

import "embed"

// Embedded migration files for both SQLite and PostgreSQL.
// The Go migrator applies these at startup when AutoMigrate is enabled.

//go:embed migrations/sqlite/*.sql
var sqliteMigrations embed.FS

// PostgreSQL migrations are embedded from the shared migrations directory.
// Note: This requires a go:generate or build-time copy step, OR the push service
// can embed directly from the shared path. Since go:embed cannot cross module
// boundaries, we use the shared database migrator with the FS passed from main.go.
// See OpenDatabase() in factory.go — for postgres, the caller passes the shared FS.
