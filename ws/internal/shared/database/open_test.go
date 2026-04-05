package database

import (
	"context"
	"embed"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
)

//go:embed testdata/migrations/sqlite/*.sql
var testSQLiteMigrations embed.FS

func TestOpen_SQLite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, err := Open(Config{
		Driver:        "sqlite",
		Path:          dbPath,
		AutoMigrate:   true,
		MigrationsFS:  testSQLiteMigrations,
		MigrationsDir: "testdata/migrations/sqlite",
		Logger:        zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	// Verify WAL mode
	var journalMode string
	if err := db.QueryRowContext(context.Background(), "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("journal_mode = %q, want %q", journalMode, "wal")
	}

	// Verify migrations were applied
	var count int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if count == 0 {
		t.Error("expected at least 1 migration applied")
	}

	// Verify test table was created
	var tableName string
	err = db.QueryRowContext(context.Background(), "SELECT name FROM sqlite_master WHERE type='table' AND name='test_items'").Scan(&tableName)
	if err != nil {
		t.Fatal("test_items table should have been created by migration")
	}
}

func TestOpen_SQLite_EmptyPath(t *testing.T) {
	t.Parallel()

	_, err := Open(Config{
		Driver: "sqlite",
		Path:   "",
		Logger: zerolog.Nop(),
	})
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestOpen_Postgres_EmptyURL(t *testing.T) {
	t.Parallel()

	_, err := Open(Config{
		Driver: "postgres",
		URL:    "",
		Logger: zerolog.Nop(),
	})
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestOpen_InvalidDriver(t *testing.T) {
	t.Parallel()

	_, err := Open(Config{
		Driver: "invalid",
		Logger: zerolog.Nop(),
	})
	if err == nil {
		t.Fatal("expected error for invalid driver")
	}
}

func TestOpen_SQLite_NoAutoMigrate(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test-nomigrate.db")

	db, err := Open(Config{
		Driver:      "sqlite",
		Path:        dbPath,
		AutoMigrate: false,
		Logger:      zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	// schema_migrations should not exist
	var tableName string
	err = db.QueryRowContext(context.Background(), "SELECT name FROM sqlite_master WHERE type='table' AND name='schema_migrations'").Scan(&tableName)
	if err == nil {
		t.Error("schema_migrations should not exist when AutoMigrate=false")
	}
}
