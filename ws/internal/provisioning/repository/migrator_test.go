package repository

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"

	_ "modernc.org/sqlite"
)

func openTestSQLite(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "migrator-test.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	// Set pragmas like the factory does
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	}
	for _, p := range pragmas {
		if _, err := db.ExecContext(context.Background(), p); err != nil {
			t.Fatalf("set %s: %v", p, err)
		}
	}

	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestMigrator_CreatesTable(t *testing.T) {
	t.Parallel()

	db := openTestSQLite(t)
	m := NewMigrator(db, "sqlite", zerolog.Nop())

	if err := m.ensureMigrationsTable(); err != nil {
		t.Fatalf("ensureMigrationsTable() error = %v", err)
	}

	// Verify table exists
	var name string
	err := db.QueryRowContext(context.Background(), "SELECT name FROM sqlite_master WHERE type='table' AND name='schema_migrations'").Scan(&name)
	if err != nil {
		t.Fatalf("schema_migrations table not created: %v", err)
	}
}

func TestMigrator_AppliesMigrations(t *testing.T) {
	t.Parallel()

	db := openTestSQLite(t)
	m := NewMigrator(db, "sqlite", zerolog.Nop())

	if err := m.Migrate(); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	// Verify migrations were recorded
	var count int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if count == 0 {
		t.Error("expected at least 1 migration applied")
	}

	// Verify tables were created by migration (e.g., tenants table)
	var tableName string
	err := db.QueryRowContext(context.Background(), "SELECT name FROM sqlite_master WHERE type='table' AND name='tenants'").Scan(&tableName)
	if err != nil {
		t.Fatal("tenants table should have been created by migration")
	}
}

func TestMigrator_Idempotent(t *testing.T) {
	t.Parallel()

	db := openTestSQLite(t)
	m := NewMigrator(db, "sqlite", zerolog.Nop())

	// First run
	if err := m.Migrate(); err != nil {
		t.Fatalf("first Migrate() error = %v", err)
	}

	var count1 int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM schema_migrations").Scan(&count1); err != nil {
		t.Fatalf("count migrations: %v", err)
	}

	// Second run — should be idempotent
	if err := m.Migrate(); err != nil {
		t.Fatalf("second Migrate() error = %v", err)
	}

	var count2 int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM schema_migrations").Scan(&count2); err != nil {
		t.Fatalf("count migrations: %v", err)
	}

	if count1 != count2 {
		t.Errorf("migration count changed: %d → %d (should be idempotent)", count1, count2)
	}
}

func TestMigrator_InvalidDriver(t *testing.T) {
	t.Parallel()

	db := openTestSQLite(t)
	m := NewMigrator(db, "invalid-driver", zerolog.Nop())

	if err := m.ensureMigrationsTable(); err != nil {
		t.Fatalf("ensureMigrationsTable() error = %v", err)
	}

	if err := m.Migrate(); err == nil {
		t.Fatal("expected error for invalid driver")
	}
}
