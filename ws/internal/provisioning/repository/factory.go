package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog"

	// SQLite driver (pure Go, no CGO)
	_ "modernc.org/sqlite"

	// PostgreSQL driver
	_ "github.com/lib/pq"
)

// DatabaseConfig holds database connection configuration.
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
// If AutoMigrate is true, embedded migrations are applied.
func OpenDatabase(cfg DatabaseConfig) (*sql.DB, error) {
	var db *sql.DB
	var err error

	switch cfg.Driver {
	case "sqlite":
		db, err = openSQLite(cfg)
	case "postgres":
		db, err = openPostgres(cfg)
	default:
		return nil, fmt.Errorf("unsupported database driver: %q (must be sqlite or postgres)", cfg.Driver)
	}
	if err != nil {
		return nil, err
	}

	// Run migrations if enabled
	if cfg.AutoMigrate {
		migrator := NewMigrator(db, cfg.Driver, cfg.Logger)
		if err := migrator.Migrate(); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("run migrations: %w", err)
		}
	}

	return db, nil
}

func openSQLite(cfg DatabaseConfig) (*sql.DB, error) {
	if cfg.Path == "" {
		return nil, errors.New("DATABASE_PATH is required for sqlite driver")
	}

	// DSN parameters:
	// _time_format=sqlite  → write time.Time as "YYYY-MM-DD HH:MM:SS±HH:MM"
	// _texttotime          → scan DATETIME/DATE/TIMESTAMP TEXT columns back to time.Time
	dsn := fmt.Sprintf("file:%s?_time_format=sqlite&_texttotime=1", cfg.Path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	// SQLite pragmas for production use
	pragmas := []string{
		"PRAGMA journal_mode=WAL",   // Write-Ahead Logging for concurrent reads
		"PRAGMA busy_timeout=5000",  // Wait 5s on lock contention
		"PRAGMA foreign_keys=ON",    // Enforce FK constraints
		"PRAGMA synchronous=NORMAL", // Good durability with WAL
		"PRAGMA cache_size=-20000",  // 20MB page cache
	}
	for _, pragma := range pragmas {
		if _, err := db.ExecContext(context.Background(), pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("set %s: %w", pragma, err)
		}
	}

	// SQLite is single-writer; limit pool to 1 writer + readers
	db.SetMaxOpenConns(1)

	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite database: %w", err)
	}

	return db, nil
}

func openPostgres(cfg DatabaseConfig) (*sql.DB, error) {
	if cfg.URL == "" {
		return nil, errors.New("DATABASE_URL is required for postgres driver")
	}

	db, err := sql.Open("postgres", cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}

	// Configure connection pool
	if cfg.MaxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		db.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}
	if cfg.ConnMaxIdleTime > 0 {
		db.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
	}

	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres database: %w", err)
	}

	return db, nil
}
