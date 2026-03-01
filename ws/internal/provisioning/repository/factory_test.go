package repository

import (
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
)

func TestOpenDatabase_SQLite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, err := OpenDatabase(DatabaseConfig{
		Driver:      "sqlite",
		Path:        dbPath,
		AutoMigrate: true,
		Logger:      zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("OpenDatabase() error = %v", err)
	}
	defer db.Close()

	// Verify WAL mode
	var journalMode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("journal_mode = %q, want %q", journalMode, "wal")
	}

	// Verify schema_migrations table exists
	var tableName string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='schema_migrations'").Scan(&tableName)
	if err != nil {
		t.Fatalf("schema_migrations table not found: %v", err)
	}

	// Verify migrations were applied
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if count == 0 {
		t.Error("expected at least 1 migration applied")
	}
}

func TestOpenDatabase_SQLite_EmptyPath(t *testing.T) {
	t.Parallel()

	_, err := OpenDatabase(DatabaseConfig{
		Driver: "sqlite",
		Path:   "",
		Logger: zerolog.Nop(),
	})
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestOpenDatabase_Postgres_EmptyURL(t *testing.T) {
	t.Parallel()

	_, err := OpenDatabase(DatabaseConfig{
		Driver: "postgres",
		URL:    "",
		Logger: zerolog.Nop(),
	})
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestOpenDatabase_InvalidDriver(t *testing.T) {
	t.Parallel()

	_, err := OpenDatabase(DatabaseConfig{
		Driver: "invalid",
		Logger: zerolog.Nop(),
	})
	if err == nil {
		t.Fatal("expected error for invalid driver")
	}
}

func TestOpenDatabase_SQLite_NoAutoMigrate(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test-nomigrate.db")

	db, err := OpenDatabase(DatabaseConfig{
		Driver:      "sqlite",
		Path:        dbPath,
		AutoMigrate: false,
		Logger:      zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("OpenDatabase() error = %v", err)
	}
	defer db.Close()

	// schema_migrations should not exist
	var tableName string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='schema_migrations'").Scan(&tableName)
	if err == nil {
		t.Error("schema_migrations should not exist when AutoMigrate=false")
	}
}
