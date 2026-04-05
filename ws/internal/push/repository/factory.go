package repository

import (
	"database/sql"
	"embed"
	"fmt"
	"time"

	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/shared/database"
)

// DatabaseConfig holds database connection configuration for the push service.
// This is a push-specific wrapper that delegates to the shared database package.
type DatabaseConfig struct {
	// Driver is the database driver: "sqlite" or "postgres".
	Driver string

	// URL is the PostgreSQL connection string (required when Driver="postgres").
	URL string

	// Path is the SQLite database file path (required when Driver="sqlite").
	Path string

	// AutoMigrate runs embedded migrations on startup.
	AutoMigrate bool

	// PostgresMigrationsFS is the embedded FS containing shared postgres migrations.
	// Required when Driver="postgres" and AutoMigrate=true.
	// Passed from cmd/push/main.go which embeds ws/internal/shared/migrations/postgres/.
	PostgresMigrationsFS embed.FS

	// PostgresMigrationsDir is the directory within PostgresMigrationsFS.
	PostgresMigrationsDir string

	// PostgreSQL connection pool settings (ignored for SQLite).
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration

	// Logger for migration output.
	Logger zerolog.Logger
}

// OpenDatabase creates and configures a database connection for the push service.
// If AutoMigrate is true, embedded migrations are applied for the configured driver.
func OpenDatabase(cfg DatabaseConfig) (*sql.DB, error) {
	var migrationsFS embed.FS
	var migrationsDir string

	switch cfg.Driver {
	case "postgres":
		migrationsFS = cfg.PostgresMigrationsFS
		migrationsDir = cfg.PostgresMigrationsDir
	default:
		migrationsFS = sqliteMigrations
		migrationsDir = "migrations/sqlite"
	}

	db, err := database.Open(database.Config{
		Driver:          cfg.Driver,
		URL:             cfg.URL,
		Path:            cfg.Path,
		AutoMigrate:     cfg.AutoMigrate,
		MigrationsFS:    migrationsFS,
		MigrationsDir:   migrationsDir,
		MaxOpenConns:    cfg.MaxOpenConns,
		MaxIdleConns:    cfg.MaxIdleConns,
		ConnMaxLifetime: cfg.ConnMaxLifetime,
		ConnMaxIdleTime: cfg.ConnMaxIdleTime,
		Logger:          cfg.Logger,
	})
	if err != nil {
		return nil, fmt.Errorf("open push database: %w", err)
	}
	return db, nil
}

// NewSubscriptionRepository creates a SubscriptionRepository for the given database driver.
// Defaults to SQLite for any unrecognized driver.
func NewSubscriptionRepository(db *sql.DB, driver string) SubscriptionRepository {
	switch driver {
	case "postgres":
		return NewPostgresSubscriptionRepository(db)
	default:
		return NewSQLiteSubscriptionRepository(db)
	}
}
