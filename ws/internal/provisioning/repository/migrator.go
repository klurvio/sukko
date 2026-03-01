package repository

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/rs/zerolog"
)

// Migrator applies embedded SQL migrations to a database.
type Migrator struct {
	db     *sql.DB
	driver string
	logger zerolog.Logger
}

// NewMigrator creates a new migration runner for the given driver.
func NewMigrator(db *sql.DB, driver string, logger zerolog.Logger) *Migrator {
	return &Migrator{
		db:     db,
		driver: driver,
		logger: logger.With().Str("component", "migrator").Logger(),
	}
}

// Migrate applies all pending migrations.
func (m *Migrator) Migrate() error {
	// Create schema_migrations table if not exists
	if err := m.ensureMigrationsTable(); err != nil {
		return fmt.Errorf("ensure migrations table: %w", err)
	}

	// Get applied migrations
	applied, err := m.getApplied()
	if err != nil {
		return fmt.Errorf("get applied migrations: %w", err)
	}

	// Read migration files
	migrations, err := m.readMigrations()
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}

	// Apply pending migrations
	for _, mig := range migrations {
		if applied[mig.name] {
			m.logger.Debug().Str("migration", mig.name).Msg("migration already applied, skipping")
			continue
		}

		if err := m.applyMigration(mig); err != nil {
			return fmt.Errorf("apply migration %s: %w", mig.name, err)
		}

		m.logger.Info().Str("migration", mig.name).Msg("migration applied")
	}

	return nil
}

type migration struct {
	name string
	sql  string
}

func (m *Migrator) ensureMigrationsTable() error {
	_, err := m.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		name       TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`)
	return err
}

func (m *Migrator) getApplied() (map[string]bool, error) {
	rows, err := m.db.Query("SELECT name FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("query schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan migration name: %w", err)
		}
		applied[name] = true
	}
	return applied, rows.Err()
}

func (m *Migrator) readMigrations() ([]migration, error) {
	var fsys embed.FS
	var dir string

	switch m.driver {
	case "postgres":
		fsys = postgresMigrations
		dir = "migrations/postgres"
	case "sqlite":
		fsys = sqliteMigrations
		dir = "migrations/sqlite"
	default:
		return nil, fmt.Errorf("unsupported driver: %s", m.driver)
	}

	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}

	// Sort entries by name to ensure correct order
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	var migrations []migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		content, err := fs.ReadFile(fsys, dir+"/"+entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}

		migrations = append(migrations, migration{
			name: entry.Name(),
			sql:  string(content),
		})
	}

	return migrations, nil
}

func (m *Migrator) applyMigration(mig migration) error {
	tx, err := m.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// Execute the migration SQL
	if _, err = tx.Exec(mig.sql); err != nil {
		return fmt.Errorf("execute SQL: %w", err)
	}

	// Record the migration
	if _, err = tx.Exec("INSERT INTO schema_migrations (name) VALUES (?)", mig.name); err != nil {
		return fmt.Errorf("record migration: %w", err)
	}

	return tx.Commit()
}
