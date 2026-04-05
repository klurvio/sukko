package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// openPushCredentialsTestDB opens a SQLite database and creates the push_credentials
// table with DATETIME column type (not TEXT) so that the modernc/sqlite _texttotime
// driver parameter correctly scans timestamps into time.Time values.
func openPushCredentialsTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test-push-creds.db")

	db, err := OpenDatabase(DatabaseConfig{
		Driver:      "sqlite",
		Path:        dbPath,
		AutoMigrate: false,
		Logger:      zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("OpenDatabase() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Create table with DATETIME type so _texttotime scans work correctly.
	// The migration 007 uses TEXT, which prevents time.Time scanning.
	createSQL := `
		CREATE TABLE IF NOT EXISTS push_credentials (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id       TEXT NOT NULL,
			provider        TEXT NOT NULL,
			credential_data TEXT NOT NULL,
			created_at      DATETIME NOT NULL DEFAULT (datetime('now')),
			UNIQUE(tenant_id, provider)
		);
		CREATE INDEX IF NOT EXISTS idx_push_credentials_tenant ON push_credentials(tenant_id);
	`
	if _, err := db.ExecContext(context.Background(), createSQL); err != nil {
		t.Fatalf("create push_credentials table: %v", err)
	}

	return db
}

// randomHexKey generates a random 32-byte key and returns its 64-char hex encoding.
func randomHexKey(t *testing.T) string {
	t.Helper()

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate random key: %v", err)
	}
	return hex.EncodeToString(key)
}

func TestCredentialsRepo_CreateAndGet(t *testing.T) {
	t.Parallel()

	db := openPushCredentialsTestDB(t)
	hexKey := randomHexKey(t)
	ctx := context.Background()

	repo, err := NewCredentialsRepository(db, hexKey)
	if err != nil {
		t.Fatalf("NewCredentialsRepository() error = %v", err)
	}

	cred := &PushCredential{
		TenantID:       "acme",
		Provider:       "fcm",
		CredentialData: `{"project_id":"my-project","private_key":"..."}`,
	}

	if err := repo.Create(ctx, cred); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if cred.ID <= 0 {
		t.Errorf("Create() did not set ID, got %d", cred.ID)
	}

	got, err := repo.Get(ctx, "acme", "fcm")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.TenantID != "acme" {
		t.Errorf("TenantID = %q, want %q", got.TenantID, "acme")
	}
	if got.Provider != "fcm" {
		t.Errorf("Provider = %q, want %q", got.Provider, "fcm")
	}
	if got.CredentialData != cred.CredentialData {
		t.Errorf("CredentialData = %q, want %q", got.CredentialData, cred.CredentialData)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

func TestCredentialsRepo_CreateDuplicate(t *testing.T) {
	t.Parallel()

	db := openPushCredentialsTestDB(t)
	hexKey := randomHexKey(t)
	ctx := context.Background()

	repo, err := NewCredentialsRepository(db, hexKey)
	if err != nil {
		t.Fatalf("NewCredentialsRepository() error = %v", err)
	}

	cred := &PushCredential{
		TenantID:       "acme",
		Provider:       "fcm",
		CredentialData: `{"key":"value1"}`,
	}
	if err := repo.Create(ctx, cred); err != nil {
		t.Fatalf("Create() first error = %v", err)
	}

	dup := &PushCredential{
		TenantID:       "acme",
		Provider:       "fcm",
		CredentialData: `{"key":"value2"}`,
	}
	err = repo.Create(ctx, dup)
	if err == nil {
		t.Fatal("Create() duplicate should return an error")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %q, expected to contain 'already exists'", err.Error())
	}
}

func TestCredentialsRepo_ListByTenant(t *testing.T) {
	t.Parallel()

	db := openPushCredentialsTestDB(t)
	hexKey := randomHexKey(t)
	ctx := context.Background()

	repo, err := NewCredentialsRepository(db, hexKey)
	if err != nil {
		t.Fatalf("NewCredentialsRepository() error = %v", err)
	}

	for _, cred := range []*PushCredential{
		{TenantID: "acme", Provider: "vapid", CredentialData: `{"public":"BPe1...","private":"xyz"}`},
		{TenantID: "acme", Provider: "fcm", CredentialData: `{"project_id":"proj"}`},
	} {
		if err := repo.Create(ctx, cred); err != nil {
			t.Fatalf("Create(%s) error = %v", cred.Provider, err)
		}
	}

	creds, err := repo.ListByTenant(ctx, "acme")
	if err != nil {
		t.Fatalf("ListByTenant() error = %v", err)
	}
	if len(creds) != 2 {
		t.Errorf("ListByTenant() returned %d creds, want 2", len(creds))
	}

	// Verify decrypted data is readable
	for _, c := range creds {
		if c.CredentialData == "" {
			t.Errorf("CredentialData for provider %s is empty", c.Provider)
		}
	}
}

func TestCredentialsRepo_Delete(t *testing.T) {
	t.Parallel()

	db := openPushCredentialsTestDB(t)
	hexKey := randomHexKey(t)
	ctx := context.Background()

	repo, err := NewCredentialsRepository(db, hexKey)
	if err != nil {
		t.Fatalf("NewCredentialsRepository() error = %v", err)
	}

	cred := &PushCredential{
		TenantID:       "acme",
		Provider:       "fcm",
		CredentialData: `{"key":"val"}`,
	}
	if err := repo.Create(ctx, cred); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := repo.Delete(ctx, "acme", "fcm"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	_, err = repo.Get(ctx, "acme", "fcm")
	if err == nil {
		t.Fatal("Get() after Delete() should return an error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, expected to contain 'not found'", err.Error())
	}
}

func TestCredentialsRepo_WithoutEncryptionKey(t *testing.T) {
	t.Parallel()

	db := openPushCredentialsTestDB(t)
	ctx := context.Background()

	// Empty key = no encryption (plaintext storage)
	repo, err := NewCredentialsRepository(db, "")
	if err != nil {
		t.Fatalf("NewCredentialsRepository() error = %v", err)
	}

	plainData := `{"project_id":"my-project","api_key":"secret"}`
	cred := &PushCredential{
		TenantID:       "acme",
		Provider:       "fcm",
		CredentialData: plainData,
	}
	if err := repo.Create(ctx, cred); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := repo.Get(ctx, "acme", "fcm")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.CredentialData != plainData {
		t.Errorf("CredentialData = %q, want %q", got.CredentialData, plainData)
	}

	// Verify the data is stored as plaintext in the DB by reading directly
	var rawData string
	err = db.QueryRowContext(ctx, "SELECT credential_data FROM push_credentials WHERE tenant_id = 'acme' AND provider = 'fcm'").Scan(&rawData)
	if err != nil {
		t.Fatalf("direct query error = %v", err)
	}
	if rawData != plainData {
		t.Errorf("raw DB data = %q, want plaintext %q", rawData, plainData)
	}
}
