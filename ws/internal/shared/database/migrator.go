package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/rs/zerolog"
)

// Migrator applies embedded SQL migrations to a database.
type Migrator struct {
	db            *sql.DB
	driver        string
	migrationsFS  embed.FS
	migrationsDir string
	logger        zerolog.Logger
}

// NewMigrator creates a new migration runner for the given driver.
// migrationsFS is the embedded filesystem containing SQL migration files.
// migrationsDir is the path within migrationsFS where the SQL files reside.
func NewMigrator(db *sql.DB, driver string, migrationsFS embed.FS, migrationsDir string, logger zerolog.Logger) *Migrator {
	return &Migrator{
		db:            db,
		driver:        driver,
		migrationsFS:  migrationsFS,
		migrationsDir: migrationsDir,
		logger:        logger.With().Str("component", "migrator").Logger(),
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
	// CURRENT_TIMESTAMP works on both SQLite and PostgreSQL.
	_, err := m.db.ExecContext(context.Background(), `CREATE TABLE IF NOT EXISTS schema_migrations (
		name       TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}
	return nil
}

func (m *Migrator) getApplied() (map[string]bool, error) {
	rows, err := m.db.QueryContext(context.Background(), "SELECT name FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("query schema_migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	applied := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan migration name: %w", err)
		}
		applied[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schema_migrations rows: %w", err)
	}
	return applied, nil
}

func (m *Migrator) readMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(m.migrationsFS, m.migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}

	// Sort entries by name to ensure correct order
	slices.SortFunc(entries, func(a, b fs.DirEntry) int {
		return strings.Compare(a.Name(), b.Name())
	})

	var migrations []migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		content, err := fs.ReadFile(m.migrationsFS, m.migrationsDir+"/"+entry.Name())
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
	tx, err := m.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// Execute the migration SQL
	if _, err = tx.ExecContext(context.Background(), mig.sql); err != nil {
		return fmt.Errorf("execute SQL: %w", err)
	}

	// Record the migration
	// $1 placeholder works on both SQLite (modernc.org/sqlite) and PostgreSQL (lib/pq).
	if _, err = tx.ExecContext(context.Background(), "INSERT INTO schema_migrations (name) VALUES ($1)", mig.name); err != nil {
		return fmt.Errorf("record migration: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}
	return nil
}
