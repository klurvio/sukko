package repository

import (
	"database/sql"

	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/shared/database"
)

// Migrator applies embedded SQL migrations to a database.
// This is a thin wrapper around the shared database.Migrator that provides
// the provisioning-specific embedded migration files.
type Migrator = database.Migrator

// NewMigrator creates a new migration runner for the given driver.
// It uses the provisioning-specific SQLite migrations embedded in this package.
func NewMigrator(db *sql.DB, driver string, logger zerolog.Logger) *Migrator {
	return database.NewMigrator(db, driver, sqliteMigrations, "migrations/sqlite", logger)
}
