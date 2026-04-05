package repository

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/klurvio/sukko/internal/provisioning"
	"github.com/klurvio/sukko/internal/shared/types"
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
	defer func() { _ = db.Close() }()

	// Verify WAL mode
	var journalMode string
	if err := db.QueryRowContext(context.Background(), "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("journal_mode = %q, want %q", journalMode, "wal")
	}

	// Verify schema_migrations table exists
	var tableName string
	err = db.QueryRowContext(context.Background(), "SELECT name FROM sqlite_master WHERE type='table' AND name='schema_migrations'").Scan(&tableName)
	if err != nil {
		t.Fatalf("schema_migrations table not found: %v", err)
	}

	// Verify migrations were applied
	var count int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
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

func TestOpenDatabase_SQLite_TimestampRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test-timestamps.db")

	db, err := OpenDatabase(DatabaseConfig{
		Driver:      "sqlite",
		Path:        dbPath,
		AutoMigrate: true,
		Logger:      zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("OpenDatabase() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	// --- tenants table: non-nullable + nullable timestamps ---

	// Insert a tenant using datetime('now') defaults
	_, err = db.ExecContext(context.Background(), `INSERT INTO tenants (id, name, status, consumer_type, metadata) VALUES ('t1', 'Test', 'active', 'shared', '{}')`)
	if err != nil {
		t.Fatalf("insert tenant: %v", err)
	}

	// Scan back and verify non-nullable timestamps
	var createdAt, updatedAt time.Time
	var suspendedAt sql.NullTime
	err = db.QueryRowContext(context.Background(), `SELECT created_at, updated_at, suspended_at FROM tenants WHERE id = 't1'`).
		Scan(&createdAt, &updatedAt, &suspendedAt)
	if err != nil {
		t.Fatalf("scan tenant timestamps: %v", err)
	}

	if createdAt.IsZero() {
		t.Error("created_at should not be zero")
	}
	if updatedAt.IsZero() {
		t.Error("updated_at should not be zero")
	}
	if suspendedAt.Valid {
		t.Error("suspended_at should be nil/invalid when NULL")
	}

	// Update suspended_at to a non-NULL value and verify it scans
	_, err = db.ExecContext(context.Background(), `UPDATE tenants SET suspended_at = datetime('now') WHERE id = 't1'`)
	if err != nil {
		t.Fatalf("update suspended_at: %v", err)
	}

	err = db.QueryRowContext(context.Background(), `SELECT suspended_at FROM tenants WHERE id = 't1'`).Scan(&suspendedAt)
	if err != nil {
		t.Fatalf("scan suspended_at after update: %v", err)
	}
	if !suspendedAt.Valid {
		t.Error("suspended_at should be valid after setting a value")
	}
	if suspendedAt.Valid && suspendedAt.Time.IsZero() {
		t.Error("suspended_at should not be zero time")
	}

	// --- tenant_keys table: NullTime for expires_at/revoked_at ---

	// Insert a key with NULL nullable timestamps
	_, err = db.ExecContext(context.Background(), `INSERT INTO tenant_keys (key_id, tenant_id, algorithm, public_key, is_active) VALUES ('k1', 't1', 'ES256', 'testkey', 1)`)
	if err != nil {
		t.Fatalf("insert tenant_key: %v", err)
	}

	var keyCreatedAt time.Time
	var expiresAt, revokedAt sql.NullTime
	err = db.QueryRowContext(context.Background(), `SELECT created_at, expires_at, revoked_at FROM tenant_keys WHERE key_id = 'k1'`).
		Scan(&keyCreatedAt, &expiresAt, &revokedAt)
	if err != nil {
		t.Fatalf("scan tenant_key timestamps: %v", err)
	}

	if keyCreatedAt.IsZero() {
		t.Error("tenant_keys.created_at should not be zero")
	}
	if expiresAt.Valid {
		t.Error("expires_at should be nil/invalid when NULL")
	}
	if revokedAt.Valid {
		t.Error("revoked_at should be nil/invalid when NULL")
	}
}

func TestOpenDatabase_SQLite_RepositoryWriteOps(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test-writeops.db")

	db, err := OpenDatabase(DatabaseConfig{
		Driver:      "sqlite",
		Path:        dbPath,
		AutoMigrate: true,
		Logger:      zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("OpenDatabase() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()

	// --- Tenant: Create + Update ---
	tenantRepo := NewTenantRepository(db)

	tenant := &provisioning.Tenant{
		ID:           "test-tenant",
		Name:         "Test Tenant",
		Status:       provisioning.StatusActive,
		ConsumerType: provisioning.ConsumerShared,
		Metadata:     provisioning.Metadata{"env": "test"},
	}
	if err := tenantRepo.Create(ctx, tenant); err != nil {
		t.Fatalf("tenant Create: %v", err)
	}

	tenant.Name = "Updated Tenant"
	if err := tenantRepo.Update(ctx, tenant); err != nil {
		t.Fatalf("tenant Update: %v", err)
	}

	got, err := tenantRepo.Get(ctx, "test-tenant")
	if err != nil {
		t.Fatalf("tenant Get after Update: %v", err)
	}
	if got.Name != "Updated Tenant" {
		t.Errorf("tenant name = %q, want %q", got.Name, "Updated Tenant")
	}
	if got.UpdatedAt.IsZero() {
		t.Error("tenant updated_at should not be zero after Update")
	}

	// --- Key: Create + Revoke ---
	keyRepo := NewKeyRepository(db)

	key := &provisioning.TenantKey{
		KeyID:     "test-key-001",
		TenantID:  "test-tenant",
		Algorithm: provisioning.AlgorithmES256,
		PublicKey: "-----BEGIN PUBLIC KEY-----\nMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEtest...\n-----END PUBLIC KEY-----",
	}
	if err := keyRepo.Create(ctx, key); err != nil {
		t.Fatalf("key Create: %v", err)
	}

	if err := keyRepo.Revoke(ctx, "test-key-001"); err != nil {
		t.Fatalf("key Revoke: %v", err)
	}

	revokedKey, err := keyRepo.Get(ctx, "test-key-001")
	if err != nil {
		t.Fatalf("key Get after Revoke: %v", err)
	}
	if revokedKey.IsActive {
		t.Error("key should be inactive after Revoke")
	}
	if revokedKey.RevokedAt == nil || revokedKey.RevokedAt.IsZero() {
		t.Error("key revoked_at should be non-zero after Revoke")
	}

	// --- Channel Rules: Create + Update (upsert) ---
	rulesRepo := NewChannelRulesRepository(db)

	rules := &types.ChannelRules{
		Public:        []string{"*.trade"},
		GroupMappings: map[string][]string{},
		Default:       []string{},
	}
	if err := rulesRepo.Create(ctx, "test-tenant", rules); err != nil {
		t.Fatalf("channel rules Create: %v", err)
	}

	gotRules, err := rulesRepo.Get(ctx, "test-tenant")
	if err != nil {
		t.Fatalf("channel rules Get after Create: %v", err)
	}
	if gotRules.CreatedAt.IsZero() {
		t.Error("channel rules created_at should not be zero")
	}
	if gotRules.UpdatedAt.IsZero() {
		t.Error("channel rules updated_at should not be zero")
	}

	// Update via upsert
	updatedRules := &types.ChannelRules{
		Public:        []string{"*.trade", "*.liquidity"},
		GroupMappings: map[string][]string{"traders": {"*.realtime"}},
		Default:       []string{},
	}
	if err := rulesRepo.Update(ctx, "test-tenant", updatedRules); err != nil {
		t.Fatalf("channel rules Update: %v", err)
	}

	gotUpdated, err := rulesRepo.Get(ctx, "test-tenant")
	if err != nil {
		t.Fatalf("channel rules Get after Update: %v", err)
	}
	if len(gotUpdated.Rules.Public) != 2 {
		t.Errorf("channel rules public count = %d, want 2", len(gotUpdated.Rules.Public))
	}
	if gotUpdated.UpdatedAt.IsZero() {
		t.Error("channel rules updated_at should not be zero after Update")
	}
}

func TestOpenDatabase_SQLite_APIKeyWriteOps(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test-apikey-ops.db")

	db, err := OpenDatabase(DatabaseConfig{
		Driver:      "sqlite",
		Path:        dbPath,
		AutoMigrate: true,
		Logger:      zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("OpenDatabase() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()

	// Create a tenant first (required FK)
	tenantRepo := NewTenantRepository(db)
	tenant := &provisioning.Tenant{
		ID:           "apikey-tenant",
		Name:         "API Key Test Tenant",
		Status:       provisioning.StatusActive,
		ConsumerType: provisioning.ConsumerShared,
		Metadata:     provisioning.Metadata{"env": "test"},
	}
	if err := tenantRepo.Create(ctx, tenant); err != nil {
		t.Fatalf("tenant Create: %v", err)
	}

	store := NewAPIKeyStore(db)

	// --- Create ---
	apiKey := &provisioning.APIKey{
		KeyID:    "pk_live_test001",
		TenantID: "apikey-tenant",
		Name:     "Test Key 1",
	}
	if err := store.Create(ctx, apiKey); err != nil {
		t.Fatalf("API key Create: %v", err)
	}

	// --- Get: verify fields ---
	got, err := store.Get(ctx, "pk_live_test001")
	if err != nil {
		t.Fatalf("API key Get: %v", err)
	}
	if got.KeyID != "pk_live_test001" {
		t.Errorf("KeyID = %q, want %q", got.KeyID, "pk_live_test001")
	}
	if got.TenantID != "apikey-tenant" {
		t.Errorf("TenantID = %q, want %q", got.TenantID, "apikey-tenant")
	}
	if got.Name != "Test Key 1" {
		t.Errorf("Name = %q, want %q", got.Name, "Test Key 1")
	}
	if !got.IsActive {
		t.Error("IsActive should be true after Create")
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
	if got.RevokedAt != nil {
		t.Error("RevokedAt should be nil after Create")
	}

	// --- ListByTenant with pagination ---
	// Create 2 more keys (3 total)
	for _, k := range []*provisioning.APIKey{
		{KeyID: "pk_live_test002", TenantID: "apikey-tenant", Name: "Test Key 2"},
		{KeyID: "pk_live_test003", TenantID: "apikey-tenant", Name: "Test Key 3"},
	} {
		if err := store.Create(ctx, k); err != nil {
			t.Fatalf("API key Create(%s): %v", k.KeyID, err)
		}
	}

	// Page 1: limit=2, offset=0 → 2 items, total=3
	keys, total, err := store.ListByTenant(ctx, "apikey-tenant", provisioning.ListOptions{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("ListByTenant page 1: %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("ListByTenant page 1 items = %d, want 2", len(keys))
	}
	if total != 3 {
		t.Errorf("ListByTenant page 1 total = %d, want 3", total)
	}

	// Page 2: limit=2, offset=2 → 1 item, total=3
	keys, total, err = store.ListByTenant(ctx, "apikey-tenant", provisioning.ListOptions{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("ListByTenant page 2: %v", err)
	}
	if len(keys) != 1 {
		t.Errorf("ListByTenant page 2 items = %d, want 1", len(keys))
	}
	if total != 3 {
		t.Errorf("ListByTenant page 2 total = %d, want 3", total)
	}

	// --- Revoke ---
	if err := store.Revoke(ctx, "pk_live_test001"); err != nil {
		t.Fatalf("API key Revoke: %v", err)
	}

	revoked, err := store.Get(ctx, "pk_live_test001")
	if err != nil {
		t.Fatalf("API key Get after Revoke: %v", err)
	}
	if revoked.IsActive {
		t.Error("IsActive should be false after Revoke")
	}
	if revoked.RevokedAt == nil {
		t.Fatal("RevokedAt should be non-nil after Revoke")
	}
	if revoked.RevokedAt.IsZero() {
		t.Error("RevokedAt should not be zero after Revoke")
	}

	// --- GetActiveAPIKeys: only active keys returned ---
	activeKeys, err := store.GetActiveAPIKeys(ctx)
	if err != nil {
		t.Fatalf("GetActiveAPIKeys: %v", err)
	}
	// pk_live_test001 was revoked, so only test002 and test003 should remain
	if len(activeKeys) != 2 {
		t.Errorf("GetActiveAPIKeys count = %d, want 2", len(activeKeys))
	}
	for _, k := range activeKeys {
		if k.KeyID == "pk_live_test001" {
			t.Error("GetActiveAPIKeys should not return revoked key pk_live_test001")
		}
	}

	// --- Get not found ---
	_, err = store.Get(ctx, "pk_live_nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent key")
	}
	if !errors.Is(err, provisioning.ErrAPIKeyNotFound) {
		t.Errorf("Get non-existent error = %v, want %v", err, provisioning.ErrAPIKeyNotFound)
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
	defer func() { _ = db.Close() }()

	// schema_migrations should not exist
	var tableName string
	err = db.QueryRowContext(context.Background(), "SELECT name FROM sqlite_master WHERE type='table' AND name='schema_migrations'").Scan(&tableName)
	if err == nil {
		t.Error("schema_migrations should not exist when AutoMigrate=false")
	}
}
