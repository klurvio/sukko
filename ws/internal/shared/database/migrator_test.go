package database

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
	m := NewMigrator(db, "sqlite", testSQLiteMigrations, "testdata/migrations/sqlite", zerolog.Nop())

	if err := m.ensureMigrationsTable(); err != nil {
		t.Fatalf("ensureMigrationsTable() error = %v", err)
	}

	var name string
	err := db.QueryRowContext(context.Background(), "SELECT name FROM sqlite_master WHERE type='table' AND name='schema_migrations'").Scan(&name)
	if err != nil {
		t.Fatalf("schema_migrations table not created: %v", err)
	}
}

func TestMigrator_AppliesMigrations(t *testing.T) {
	t.Parallel()

	db := openTestSQLite(t)
	m := NewMigrator(db, "sqlite", testSQLiteMigrations, "testdata/migrations/sqlite", zerolog.Nop())

	if err := m.Migrate(); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	var count int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if count == 0 {
		t.Error("expected at least 1 migration applied")
	}

	var tableName string
	err := db.QueryRowContext(context.Background(), "SELECT name FROM sqlite_master WHERE type='table' AND name='test_items'").Scan(&tableName)
	if err != nil {
		t.Fatal("test_items table should have been created by migration")
	}
}

func TestMigrator_Idempotent(t *testing.T) {
	t.Parallel()

	db := openTestSQLite(t)
	m := NewMigrator(db, "sqlite", testSQLiteMigrations, "testdata/migrations/sqlite", zerolog.Nop())

	if err := m.Migrate(); err != nil {
		t.Fatalf("first Migrate() error = %v", err)
	}

	var count1 int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM schema_migrations").Scan(&count1); err != nil {
		t.Fatalf("count migrations: %v", err)
	}

	if err := m.Migrate(); err != nil {
		t.Fatalf("second Migrate() error = %v", err)
	}

	var count2 int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM schema_migrations").Scan(&count2); err != nil {
		t.Fatalf("count migrations: %v", err)
	}

	if count1 != count2 {
		t.Errorf("migration count changed: %d -> %d (should be idempotent)", count1, count2)
	}
}

func TestMigrator_EmptyMigrationsDir(t *testing.T) {
	t.Parallel()

	db := openTestSQLite(t)
	// Use an invalid migrations dir to trigger read error
	m := NewMigrator(db, "sqlite", testSQLiteMigrations, "nonexistent/dir", zerolog.Nop())

	if err := m.Migrate(); err == nil {
		t.Fatal("expected error for nonexistent migrations dir")
	}
}
