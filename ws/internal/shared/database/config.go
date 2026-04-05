// Package database provides shared database connection and migration utilities
// for services that need SQLite or PostgreSQL storage.
package database

import (
	"embed"
	"time"

	"github.com/rs/zerolog"
)

// Config holds database connection configuration.
type Config struct {
	// Driver is the database driver: "sqlite" or "postgres".
	Driver string

	// URL is the PostgreSQL connection string (required when Driver="postgres").
	URL string

	// Path is the SQLite database file path (required when Driver="sqlite").
	Path string

	// AutoMigrate runs embedded migrations on startup (SQLite only;
	// PostgreSQL migrations are applied externally via Atlas).
	AutoMigrate bool

	// MigrationsFS is the embedded filesystem containing migration SQL files.
	// Required when AutoMigrate is true.
	MigrationsFS embed.FS

	// MigrationsDir is the directory path within MigrationsFS for the current driver.
	// For example: "migrations/sqlite" for SQLite migrations embedded under that path.
	// Required when AutoMigrate is true.
	MigrationsDir string

	// PostgreSQL connection pool settings (ignored for SQLite).
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration

	// Logger for migration and connection output.
	Logger zerolog.Logger
}
