package repository

import (
	"database/sql"
	"time"

	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/shared/database"
)

// DatabaseConfig holds database connection configuration.
// This is a provisioning-specific wrapper that delegates to the shared database package.
type DatabaseConfig struct {
	// Driver is the database driver: "sqlite" or "postgres".
	Driver string

	// URL is the PostgreSQL connection string (required when Driver="postgres").
	URL string

	// Path is the SQLite database file path (required when Driver="sqlite").
	Path string

	// AutoMigrate runs embedded migrations on startup.
	AutoMigrate bool

	// PostgreSQL connection pool settings (ignored for SQLite).
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration

	// Logger for migration output.
	Logger zerolog.Logger
}

// OpenDatabase creates and configures a database connection.
// If AutoMigrate is true for SQLite, embedded provisioning migrations are applied.
// For PostgreSQL, migrations are applied externally via Atlas.
func OpenDatabase(cfg DatabaseConfig) (*sql.DB, error) {
	return database.Open(database.Config{
		Driver:          cfg.Driver,
		URL:             cfg.URL,
		Path:            cfg.Path,
		AutoMigrate:     cfg.AutoMigrate,
		MigrationsFS:    sqliteMigrations,
		MigrationsDir:   "migrations/sqlite",
		MaxOpenConns:    cfg.MaxOpenConns,
		MaxIdleConns:    cfg.MaxIdleConns,
		ConnMaxLifetime: cfg.ConnMaxLifetime,
		ConnMaxIdleTime: cfg.ConnMaxIdleTime,
		Logger:          cfg.Logger,
	})
}
