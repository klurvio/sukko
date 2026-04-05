package repository

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
)

// openTestDB creates a SQLite database with the push_subscriptions table for testing.
// The table is created manually with DATETIME column types (matching the provisioning
// convention) so that the modernc/sqlite _texttotime driver parameter correctly scans
// timestamps into time.Time values.
func openTestDB(t *testing.T) *testDB {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test-push.db")

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

	// Create the table with DATETIME types so _texttotime scans work correctly.
	createSQL := `
		CREATE TABLE IF NOT EXISTS push_subscriptions (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id       TEXT NOT NULL,
			principal       TEXT NOT NULL,
			platform        TEXT NOT NULL,
			token           TEXT,
			endpoint        TEXT,
			p256dh_key      TEXT,
			auth_secret     TEXT,
			channels        TEXT NOT NULL,
			created_at      DATETIME NOT NULL DEFAULT (datetime('now')),
			last_success_at DATETIME
		);
		CREATE INDEX IF NOT EXISTS idx_push_subs_tenant ON push_subscriptions(tenant_id);
		CREATE INDEX IF NOT EXISTS idx_push_subs_tenant_principal ON push_subscriptions(tenant_id, principal);
	`
	if _, err := db.ExecContext(context.Background(), createSQL); err != nil {
		t.Fatalf("create push_subscriptions table: %v", err)
	}

	return &testDB{
		repo: NewSQLiteSubscriptionRepository(db),
	}
}

type testDB struct {
	repo SubscriptionRepository
}

func TestSubscriptionRepo_CreateWebPush(t *testing.T) {
	t.Parallel()

	tdb := openTestDB(t)
	ctx := context.Background()

	sub := &PushSubscription{
		TenantID:   "acme",
		Principal:  "user1",
		Platform:   "web",
		Endpoint:   "https://push.example.com/sub/abc",
		P256dhKey:  "BPe1...",
		AuthSecret: "secret123",
		Channels:   []string{"acme.alerts.*"},
	}

	id, err := tdb.repo.Create(ctx, sub)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if id <= 0 {
		t.Errorf("Create() returned id = %d, want > 0", id)
	}
}

func TestSubscriptionRepo_CreateFCM(t *testing.T) {
	t.Parallel()

	tdb := openTestDB(t)
	ctx := context.Background()

	sub := &PushSubscription{
		TenantID:  "acme",
		Principal: "user2",
		Platform:  "android",
		Token:     "fcm-token-xyz",
		Channels:  []string{"acme.notifications.*"},
	}

	id, err := tdb.repo.Create(ctx, sub)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if id <= 0 {
		t.Errorf("Create() returned id = %d, want > 0", id)
	}

	// Verify no endpoint or web push fields set
	subs, err := tdb.repo.FindByTenant(ctx, "acme")
	if err != nil {
		t.Fatalf("FindByTenant() error = %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("FindByTenant() returned %d subs, want 1", len(subs))
	}
	if subs[0].Token != "fcm-token-xyz" {
		t.Errorf("Token = %q, want %q", subs[0].Token, "fcm-token-xyz")
	}
	if subs[0].Endpoint != "" {
		t.Errorf("Endpoint = %q, want empty", subs[0].Endpoint)
	}
}

func TestSubscriptionRepo_FindByTenant(t *testing.T) {
	t.Parallel()

	tdb := openTestDB(t)
	ctx := context.Background()

	// Insert 3 subs for "acme" and 1 for "other"
	for _, s := range []*PushSubscription{
		{TenantID: "acme", Principal: "u1", Platform: "web", Endpoint: "https://a.com/1", Channels: []string{"acme.a"}},
		{TenantID: "acme", Principal: "u2", Platform: "web", Endpoint: "https://a.com/2", Channels: []string{"acme.b"}},
		{TenantID: "acme", Principal: "u3", Platform: "android", Token: "tok", Channels: []string{"acme.c"}},
		{TenantID: "other", Principal: "u4", Platform: "ios", Token: "ios-tok", Channels: []string{"other.x"}},
	} {
		if _, err := tdb.repo.Create(ctx, s); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	subs, err := tdb.repo.FindByTenant(ctx, "acme")
	if err != nil {
		t.Fatalf("FindByTenant(acme) error = %v", err)
	}
	if len(subs) != 3 {
		t.Errorf("FindByTenant(acme) returned %d subs, want 3", len(subs))
	}

	otherSubs, err := tdb.repo.FindByTenant(ctx, "other")
	if err != nil {
		t.Fatalf("FindByTenant(other) error = %v", err)
	}
	if len(otherSubs) != 1 {
		t.Errorf("FindByTenant(other) returned %d subs, want 1", len(otherSubs))
	}
}

func TestSubscriptionRepo_FindByTenantEmpty(t *testing.T) {
	t.Parallel()

	tdb := openTestDB(t)
	ctx := context.Background()

	subs, err := tdb.repo.FindByTenant(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("FindByTenant() error = %v", err)
	}
	if subs != nil {
		t.Errorf("FindByTenant() returned %v, want nil", subs)
	}
}

func TestSubscriptionRepo_Delete(t *testing.T) {
	t.Parallel()

	tdb := openTestDB(t)
	ctx := context.Background()

	sub := &PushSubscription{
		TenantID:  "acme",
		Principal: "u1",
		Platform:  "web",
		Endpoint:  "https://push.example.com/sub/del",
		Channels:  []string{"acme.alerts.*"},
	}
	id, err := tdb.repo.Create(ctx, sub)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := tdb.repo.Delete(ctx, id, "acme"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	subs, err := tdb.repo.FindByTenant(ctx, "acme")
	if err != nil {
		t.Fatalf("FindByTenant() error = %v", err)
	}
	if len(subs) != 0 {
		t.Errorf("FindByTenant() returned %d subs after delete, want 0", len(subs))
	}
}

func TestSubscriptionRepo_DeleteByToken(t *testing.T) {
	t.Parallel()

	tdb := openTestDB(t)
	ctx := context.Background()

	sub := &PushSubscription{
		TenantID:  "acme",
		Principal: "u1",
		Platform:  "android",
		Token:     "abc123",
		Channels:  []string{"acme.alerts.*"},
	}
	if _, err := tdb.repo.Create(ctx, sub); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := tdb.repo.DeleteByToken(ctx, "acme", "abc123"); err != nil {
		t.Fatalf("DeleteByToken() error = %v", err)
	}

	subs, err := tdb.repo.FindByTenant(ctx, "acme")
	if err != nil {
		t.Fatalf("FindByTenant() error = %v", err)
	}
	if len(subs) != 0 {
		t.Errorf("FindByTenant() returned %d subs after DeleteByToken, want 0", len(subs))
	}
}

func TestSubscriptionRepo_UpdateLastSuccess(t *testing.T) {
	t.Parallel()

	tdb := openTestDB(t)
	ctx := context.Background()

	sub := &PushSubscription{
		TenantID:  "acme",
		Principal: "u1",
		Platform:  "web",
		Endpoint:  "https://push.example.com/sub/upd",
		Channels:  []string{"acme.alerts.*"},
	}
	id, err := tdb.repo.Create(ctx, sub)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Verify LastSuccessAt is nil initially
	subs, err := tdb.repo.FindByTenant(ctx, "acme")
	if err != nil {
		t.Fatalf("FindByTenant() error = %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("expected 1 sub, got %d", len(subs))
	}
	if subs[0].LastSuccessAt != nil {
		t.Error("LastSuccessAt should be nil initially")
	}

	// Update and verify
	if err := tdb.repo.UpdateLastSuccess(ctx, id); err != nil {
		t.Fatalf("UpdateLastSuccess() error = %v", err)
	}

	subs, err = tdb.repo.FindByTenant(ctx, "acme")
	if err != nil {
		t.Fatalf("FindByTenant() error = %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("expected 1 sub, got %d", len(subs))
	}
	if subs[0].LastSuccessAt == nil {
		t.Fatal("LastSuccessAt should be non-nil after UpdateLastSuccess")
	}
	if subs[0].LastSuccessAt.IsZero() {
		t.Error("LastSuccessAt should not be zero after UpdateLastSuccess")
	}
}

func TestSubscriptionRepo_ChannelsRoundtrip(t *testing.T) {
	t.Parallel()

	tdb := openTestDB(t)
	ctx := context.Background()

	channels := []string{"acme.alerts.*", "acme.notifications.*"}
	sub := &PushSubscription{
		TenantID:  "acme",
		Principal: "u1",
		Platform:  "web",
		Endpoint:  "https://push.example.com/sub/ch",
		Channels:  channels,
	}

	if _, err := tdb.repo.Create(ctx, sub); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	subs, err := tdb.repo.FindByTenant(ctx, "acme")
	if err != nil {
		t.Fatalf("FindByTenant() error = %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("expected 1 sub, got %d", len(subs))
	}

	got := subs[0].Channels
	if len(got) != len(channels) {
		t.Fatalf("Channels length = %d, want %d", len(got), len(channels))
	}
	for i, ch := range channels {
		if got[i] != ch {
			t.Errorf("Channels[%d] = %q, want %q", i, got[i], ch)
		}
	}
}
