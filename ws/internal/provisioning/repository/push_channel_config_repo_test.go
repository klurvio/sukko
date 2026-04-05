package repository

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// openChannelConfigTestDB opens a SQLite database and creates the push_channel_configs
// table matching the schema expected by ChannelConfigRepository.
// The table name and columns are aligned with the Go code (push_channel_configs with
// created_at/updated_at columns).
func openChannelConfigTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test-channel-config.db")

	db, err := OpenDatabase(DatabaseConfig{
		Driver:      "sqlite",
		Path:        dbPath,
		AutoMigrate: false, // We create the table manually to match Go code expectations
		Logger:      zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("OpenDatabase() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Create table matching the Go code's queries (push_channel_configs, with timestamps)
	createSQL := `
		CREATE TABLE IF NOT EXISTS push_channel_configs (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id       TEXT NOT NULL UNIQUE,
			patterns        TEXT NOT NULL,
			default_ttl     INTEGER NOT NULL DEFAULT 2419200,
			default_urgency TEXT NOT NULL DEFAULT 'normal',
			created_at      DATETIME NOT NULL DEFAULT (datetime('now')),
			updated_at      DATETIME NOT NULL DEFAULT (datetime('now'))
		)
	`
	if _, err := db.ExecContext(context.Background(), createSQL); err != nil {
		t.Fatalf("create push_channel_configs table: %v", err)
	}

	return db
}

func TestChannelConfigRepo_UpsertAndGet(t *testing.T) {
	t.Parallel()

	db := openChannelConfigTestDB(t)
	repo := NewChannelConfigRepository(db)
	ctx := context.Background()

	config := &PushChannelConfig{
		TenantID:       "acme",
		Patterns:       []string{"acme.alerts.*"},
		DefaultTTL:     3600,
		DefaultUrgency: "high",
	}

	if err := repo.Upsert(ctx, config); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	got, err := repo.Get(ctx, "acme")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.TenantID != "acme" {
		t.Errorf("TenantID = %q, want %q", got.TenantID, "acme")
	}
	if len(got.Patterns) != 1 || got.Patterns[0] != "acme.alerts.*" {
		t.Errorf("Patterns = %v, want [acme.alerts.*]", got.Patterns)
	}
	if got.DefaultTTL != 3600 {
		t.Errorf("DefaultTTL = %d, want 3600", got.DefaultTTL)
	}
	if got.DefaultUrgency != "high" {
		t.Errorf("DefaultUrgency = %q, want %q", got.DefaultUrgency, "high")
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
	if got.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should not be zero")
	}
}

func TestChannelConfigRepo_UpsertUpdate(t *testing.T) {
	t.Parallel()

	db := openChannelConfigTestDB(t)
	repo := NewChannelConfigRepository(db)
	ctx := context.Background()

	// Initial insert
	config := &PushChannelConfig{
		TenantID:       "acme",
		Patterns:       []string{"acme.alerts.*"},
		DefaultTTL:     3600,
		DefaultUrgency: "high",
	}
	if err := repo.Upsert(ctx, config); err != nil {
		t.Fatalf("Upsert() initial error = %v", err)
	}

	// Update with different patterns
	updated := &PushChannelConfig{
		TenantID:       "acme",
		Patterns:       []string{"acme.alerts.*", "acme.notifications.*"},
		DefaultTTL:     7200,
		DefaultUrgency: "normal",
	}
	if err := repo.Upsert(ctx, updated); err != nil {
		t.Fatalf("Upsert() update error = %v", err)
	}

	got, err := repo.Get(ctx, "acme")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(got.Patterns) != 2 {
		t.Fatalf("Patterns length = %d, want 2", len(got.Patterns))
	}
	if got.Patterns[0] != "acme.alerts.*" || got.Patterns[1] != "acme.notifications.*" {
		t.Errorf("Patterns = %v, want [acme.alerts.* acme.notifications.*]", got.Patterns)
	}
	if got.DefaultTTL != 7200 {
		t.Errorf("DefaultTTL = %d, want 7200", got.DefaultTTL)
	}
	if got.DefaultUrgency != "normal" {
		t.Errorf("DefaultUrgency = %q, want %q", got.DefaultUrgency, "normal")
	}
}

func TestChannelConfigRepo_ListAll(t *testing.T) {
	t.Parallel()

	db := openChannelConfigTestDB(t)
	repo := NewChannelConfigRepository(db)
	ctx := context.Background()

	for _, cfg := range []*PushChannelConfig{
		{TenantID: "acme", Patterns: []string{"acme.*"}, DefaultTTL: 3600, DefaultUrgency: "high"},
		{TenantID: "beta", Patterns: []string{"beta.*"}, DefaultTTL: 7200, DefaultUrgency: "normal"},
	} {
		if err := repo.Upsert(ctx, cfg); err != nil {
			t.Fatalf("Upsert(%s) error = %v", cfg.TenantID, err)
		}
	}

	configs, err := repo.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll() error = %v", err)
	}
	if len(configs) != 2 {
		t.Errorf("ListAll() returned %d configs, want 2", len(configs))
	}
}

func TestChannelConfigRepo_Delete(t *testing.T) {
	t.Parallel()

	db := openChannelConfigTestDB(t)
	repo := NewChannelConfigRepository(db)
	ctx := context.Background()

	// Insert 2 configs
	for _, cfg := range []*PushChannelConfig{
		{TenantID: "acme", Patterns: []string{"acme.*"}, DefaultTTL: 3600, DefaultUrgency: "high"},
		{TenantID: "beta", Patterns: []string{"beta.*"}, DefaultTTL: 7200, DefaultUrgency: "normal"},
	} {
		if err := repo.Upsert(ctx, cfg); err != nil {
			t.Fatalf("Upsert(%s) error = %v", cfg.TenantID, err)
		}
	}

	// Delete one
	if err := repo.Delete(ctx, "acme"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// ListAll should return 1
	configs, err := repo.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll() error = %v", err)
	}
	if len(configs) != 1 {
		t.Errorf("ListAll() after delete returned %d configs, want 1", len(configs))
	}

	// Get deleted should return error
	_, err = repo.Get(ctx, "acme")
	if err == nil {
		t.Fatal("Get() after Delete() should return an error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, expected to contain 'not found'", err.Error())
	}
}

func TestChannelConfigRepo_PatternsRoundtrip(t *testing.T) {
	t.Parallel()

	db := openChannelConfigTestDB(t)
	repo := NewChannelConfigRepository(db)
	ctx := context.Background()

	patterns := []string{"acme.alerts.*", "acme.notifications.>", "acme.trades.btc-usd"}
	config := &PushChannelConfig{
		TenantID:       "acme",
		Patterns:       patterns,
		DefaultTTL:     86400,
		DefaultUrgency: "normal",
	}

	if err := repo.Upsert(ctx, config); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	got, err := repo.Get(ctx, "acme")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if len(got.Patterns) != len(patterns) {
		t.Fatalf("Patterns length = %d, want %d", len(got.Patterns), len(patterns))
	}
	for i, p := range patterns {
		if got.Patterns[i] != p {
			t.Errorf("Patterns[%d] = %q, want %q", i, got.Patterns[i], p)
		}
	}
}
