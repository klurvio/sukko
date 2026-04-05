package auth_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/shared/auth"
	"github.com/klurvio/sukko/internal/shared/testutil"
)

// generateTestPEM generates an ECDSA P-256 key pair and returns the PEM-encoded public key.
func generateTestPEM(t *testing.T) string {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate EC key: %v", err)
	}
	pubBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubBytes,
	}))
}

func TestDBKeyRegistry_GetKey(t *testing.T) {
	t.Parallel()
	pool := testutil.NewTestPool(t)
	ctx := context.Background()

	pubPEM := generateTestPEM(t)

	// Seed a tenant and key
	_, err := pool.Exec(ctx,
		`INSERT INTO tenants (id, name, status, consumer_type) VALUES ($1, $2, 'active', 'shared')`,
		"test-tenant", "Test Tenant")
	if err != nil {
		t.Fatalf("insert tenant: %v", err)
	}

	_, err = pool.Exec(ctx,
		`INSERT INTO tenant_keys (key_id, tenant_id, algorithm, public_key, is_active) VALUES ($1, $2, $3, $4, $5)`,
		"test-key-001", "test-tenant", "ES256", pubPEM, true)
	if err != nil {
		t.Fatalf("insert key: %v", err)
	}

	registry, err := auth.NewKeyRegistry(auth.KeyRegistryConfig{
		Pool:            pool,
		RefreshInterval: time.Minute,
		QueryTimeout:    5 * time.Second,
		Logger:          zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("NewKeyRegistry() error = %v", err)
	}
	t.Cleanup(func() { _ = registry.Close() })

	key, err := registry.GetKey(ctx, "test-key-001")
	if err != nil {
		t.Fatalf("GetKey() error = %v", err)
	}
	if key.KeyID != "test-key-001" {
		t.Errorf("KeyID = %q, want %q", key.KeyID, "test-key-001")
	}
	if key.TenantID != "test-tenant" {
		t.Errorf("TenantID = %q, want %q", key.TenantID, "test-tenant")
	}
	if key.PublicKey == nil {
		t.Error("PublicKey should be parsed")
	}
}

func TestDBKeyRegistry_GetKey_NotFound(t *testing.T) {
	t.Parallel()
	pool := testutil.NewTestPool(t)
	ctx := context.Background()

	registry, err := auth.NewKeyRegistry(auth.KeyRegistryConfig{
		Pool:            pool,
		RefreshInterval: time.Minute,
		QueryTimeout:    5 * time.Second,
		Logger:          zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("NewKeyRegistry() error = %v", err)
	}
	t.Cleanup(func() { _ = registry.Close() })

	_, err = registry.GetKey(ctx, "nonexistent-key")
	if err == nil {
		t.Fatal("GetKey() should return error for nonexistent key")
	}
}

func TestDBKeyRegistry_GetKeysByTenant(t *testing.T) {
	t.Parallel()
	pool := testutil.NewTestPool(t)
	ctx := context.Background()

	pubPEM := generateTestPEM(t)

	// Seed tenant with multiple keys
	_, err := pool.Exec(ctx,
		`INSERT INTO tenants (id, name, status, consumer_type) VALUES ($1, $2, 'active', 'shared')`,
		"multi-key-tenant", "Multi Key Tenant")
	if err != nil {
		t.Fatalf("insert tenant: %v", err)
	}

	for _, kid := range []string{"key-aaa-001", "key-aaa-002"} {
		_, err = pool.Exec(ctx,
			`INSERT INTO tenant_keys (key_id, tenant_id, algorithm, public_key, is_active) VALUES ($1, $2, $3, $4, $5)`,
			kid, "multi-key-tenant", "ES256", pubPEM, true)
		if err != nil {
			t.Fatalf("insert key %s: %v", kid, err)
		}
	}

	registry, err := auth.NewKeyRegistry(auth.KeyRegistryConfig{
		Pool:            pool,
		RefreshInterval: time.Minute,
		QueryTimeout:    5 * time.Second,
		Logger:          zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("NewKeyRegistry() error = %v", err)
	}
	t.Cleanup(func() { _ = registry.Close() })

	keys, err := registry.GetKeysByTenant(ctx, "multi-key-tenant")
	if err != nil {
		t.Fatalf("GetKeysByTenant() error = %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("GetKeysByTenant() returned %d keys, want 2", len(keys))
	}
}

func TestDBKeyRegistry_RevokedKeyNotReturned(t *testing.T) {
	t.Parallel()
	pool := testutil.NewTestPool(t)
	ctx := context.Background()

	pubPEM := generateTestPEM(t)
	past := time.Now().Add(-time.Hour)

	_, err := pool.Exec(ctx,
		`INSERT INTO tenants (id, name, status, consumer_type) VALUES ($1, $2, 'active', 'shared')`,
		"revoked-tenant", "Revoked Tenant")
	if err != nil {
		t.Fatalf("insert tenant: %v", err)
	}

	_, err = pool.Exec(ctx,
		`INSERT INTO tenant_keys (key_id, tenant_id, algorithm, public_key, is_active, revoked_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		"revoked-key-01", "revoked-tenant", "ES256", pubPEM, true, past)
	if err != nil {
		t.Fatalf("insert revoked key: %v", err)
	}

	registry, err := auth.NewKeyRegistry(auth.KeyRegistryConfig{
		Pool:            pool,
		RefreshInterval: time.Minute,
		QueryTimeout:    5 * time.Second,
		Logger:          zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("NewKeyRegistry() error = %v", err)
	}
	t.Cleanup(func() { _ = registry.Close() })

	_, err = registry.GetKey(ctx, "revoked-key-01")
	if err == nil {
		t.Fatal("GetKey() should return error for revoked key")
	}
}

func TestDBKeyRegistry_Refresh(t *testing.T) {
	t.Parallel()
	pool := testutil.NewTestPool(t)
	ctx := context.Background()

	registry, err := auth.NewKeyRegistry(auth.KeyRegistryConfig{
		Pool:            pool,
		RefreshInterval: time.Minute,
		QueryTimeout:    5 * time.Second,
		Logger:          zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("NewKeyRegistry() error = %v", err)
	}
	t.Cleanup(func() { _ = registry.Close() })

	// Refresh should succeed even with empty database
	if err := registry.Refresh(ctx); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	stats := registry.Stats()
	if stats.TotalKeys != 0 {
		t.Errorf("TotalKeys = %d, want 0", stats.TotalKeys)
	}
}
