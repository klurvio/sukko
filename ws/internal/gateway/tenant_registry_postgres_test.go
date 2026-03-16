package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/shared/types"
)

// newTestPostgresRegistry creates a PostgresTenantRegistry with short cache TTLs for testing.
func newTestPostgresRegistry(t *testing.T, db *sql.DB) *PostgresTenantRegistry {
	t.Helper()
	registry, err := NewPostgresTenantRegistry(PostgresTenantRegistryConfig{
		DB:                   db,
		IssuerCacheTTL:       100 * time.Millisecond,
		ChannelRulesCacheTTL: 100 * time.Millisecond,
		QueryTimeout:         1 * time.Second,
		Logger:               zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("NewPostgresTenantRegistry: %v", err)
	}
	return registry
}

func TestPostgresRegistry_New_NilDB(t *testing.T) {
	t.Parallel()
	_, err := NewPostgresTenantRegistry(PostgresTenantRegistryConfig{
		DB:     nil,
		Logger: zerolog.Nop(),
	})
	if err == nil {
		t.Fatal("Expected error for nil DB")
	}
}

func TestPostgresRegistry_GetTenantByIssuer_Found(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	registry := newTestPostgresRegistry(t, db)

	rows := sqlmock.NewRows([]string{"tenant_id"}).AddRow("acme")
	mock.ExpectQuery("SELECT tenant_id FROM tenant_oidc_config").
		WithArgs("https://auth.acme.com").
		WillReturnRows(rows)

	tenantID, err := registry.GetTenantByIssuer(context.Background(), "https://auth.acme.com")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if tenantID != "acme" {
		t.Errorf("Expected tenant ID 'acme', got %q", tenantID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unmet expectations: %v", err)
	}
}

func TestPostgresRegistry_GetTenantByIssuer_CacheHit(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	registry := newTestPostgresRegistry(t, db)

	// First call: query DB
	rows := sqlmock.NewRows([]string{"tenant_id"}).AddRow("acme")
	mock.ExpectQuery("SELECT tenant_id FROM tenant_oidc_config").
		WithArgs("https://auth.acme.com").
		WillReturnRows(rows)

	tenantID, err := registry.GetTenantByIssuer(context.Background(), "https://auth.acme.com")
	if err != nil {
		t.Fatalf("First call: %v", err)
	}
	if tenantID != "acme" {
		t.Errorf("First call: expected 'acme', got %q", tenantID)
	}

	// Second call: should use cache (no new DB expectation)
	tenantID, err = registry.GetTenantByIssuer(context.Background(), "https://auth.acme.com")
	if err != nil {
		t.Fatalf("Second call: %v", err)
	}
	if tenantID != "acme" {
		t.Errorf("Second call: expected 'acme', got %q", tenantID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unmet expectations: %v", err)
	}
}

func TestPostgresRegistry_GetTenantByIssuer_CacheExpired(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	registry := newTestPostgresRegistry(t, db)

	// First query
	rows1 := sqlmock.NewRows([]string{"tenant_id"}).AddRow("acme")
	mock.ExpectQuery("SELECT tenant_id FROM tenant_oidc_config").
		WithArgs("https://auth.acme.com").
		WillReturnRows(rows1)

	_, err = registry.GetTenantByIssuer(context.Background(), "https://auth.acme.com")
	if err != nil {
		t.Fatalf("First call: %v", err)
	}

	// Wait for cache expiry
	time.Sleep(150 * time.Millisecond)

	// Second query — should hit DB again
	rows2 := sqlmock.NewRows([]string{"tenant_id"}).AddRow("acme")
	mock.ExpectQuery("SELECT tenant_id FROM tenant_oidc_config").
		WithArgs("https://auth.acme.com").
		WillReturnRows(rows2)

	_, err = registry.GetTenantByIssuer(context.Background(), "https://auth.acme.com")
	if err != nil {
		t.Fatalf("Second call: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unmet expectations: %v", err)
	}
}

func TestPostgresRegistry_GetTenantByIssuer_NotFound(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	registry := newTestPostgresRegistry(t, db)

	mock.ExpectQuery("SELECT tenant_id FROM tenant_oidc_config").
		WithArgs("https://unknown.com").
		WillReturnError(sql.ErrNoRows)

	_, err = registry.GetTenantByIssuer(context.Background(), "https://unknown.com")
	if !errors.Is(err, types.ErrIssuerNotFound) {
		t.Errorf("Expected ErrIssuerNotFound, got %v", err)
	}
}

func TestPostgresRegistry_GetTenantByIssuer_DBError(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	registry := newTestPostgresRegistry(t, db)

	dbErr := errors.New("connection refused")
	mock.ExpectQuery("SELECT tenant_id FROM tenant_oidc_config").
		WithArgs("https://auth.acme.com").
		WillReturnError(dbErr)

	_, err = registry.GetTenantByIssuer(context.Background(), "https://auth.acme.com")
	if err == nil {
		t.Fatal("Expected error")
	}
	if errors.Is(err, types.ErrIssuerNotFound) {
		t.Error("Should not be ErrIssuerNotFound for DB errors")
	}
}

func TestPostgresRegistry_GetOIDCConfig_Found(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	registry := newTestPostgresRegistry(t, db)
	now := time.Now()

	rows := sqlmock.NewRows([]string{"tenant_id", "issuer_url", "jwks_url", "audience", "enabled", "created_at", "updated_at"}).
		AddRow("acme", "https://auth.acme.com/", "https://auth.acme.com/.well-known/jwks.json", "api.acme.com", true, now, now)

	mock.ExpectQuery("SELECT tenant_id, issuer_url, jwks_url, audience, enabled, created_at, updated_at FROM tenant_oidc_config").
		WithArgs("acme").
		WillReturnRows(rows)

	config, err := registry.GetOIDCConfig(context.Background(), "acme")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if config.TenantID != "acme" {
		t.Errorf("Expected tenant ID 'acme', got %q", config.TenantID)
	}
	if config.IssuerURL != "https://auth.acme.com/" {
		t.Errorf("Expected issuer URL 'https://auth.acme.com/', got %q", config.IssuerURL)
	}
}

func TestPostgresRegistry_GetOIDCConfig_NotFound(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	registry := newTestPostgresRegistry(t, db)

	mock.ExpectQuery("SELECT tenant_id, issuer_url").
		WithArgs("unknown").
		WillReturnError(sql.ErrNoRows)

	_, err = registry.GetOIDCConfig(context.Background(), "unknown")
	if !errors.Is(err, types.ErrOIDCNotConfigured) {
		t.Errorf("Expected ErrOIDCNotConfigured, got %v", err)
	}
}

func TestPostgresRegistry_GetOIDCConfig_ValidationFails(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	registry := newTestPostgresRegistry(t, db)
	now := time.Now()

	// Invalid config: empty issuer URL fails TenantOIDCConfig.Validate()
	rows := sqlmock.NewRows([]string{"tenant_id", "issuer_url", "jwks_url", "audience", "enabled", "created_at", "updated_at"}).
		AddRow("acme", "", "", "", true, now, now)

	mock.ExpectQuery("SELECT tenant_id, issuer_url").
		WithArgs("acme").
		WillReturnRows(rows)

	_, err = registry.GetOIDCConfig(context.Background(), "acme")
	if err == nil {
		t.Fatal("Expected validation error")
	}
}

func TestPostgresRegistry_GetOIDCConfig_DBError(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	registry := newTestPostgresRegistry(t, db)

	mock.ExpectQuery("SELECT tenant_id, issuer_url").
		WithArgs("acme").
		WillReturnError(errors.New("connection lost"))

	_, err = registry.GetOIDCConfig(context.Background(), "acme")
	if err == nil {
		t.Fatal("Expected error")
	}
	if errors.Is(err, types.ErrOIDCNotConfigured) {
		t.Error("Should not be ErrOIDCNotConfigured for DB errors")
	}
}

func TestPostgresRegistry_GetChannelRules_Found(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	registry := newTestPostgresRegistry(t, db)

	rules := types.ChannelRules{
		Public: []string{"*.metadata"},
		GroupMappings: map[string][]string{
			"traders": {"*.trade"},
		},
	}
	rulesJSON, _ := json.Marshal(rules)

	rows := sqlmock.NewRows([]string{"rules"}).AddRow(rulesJSON)
	mock.ExpectQuery("SELECT rules FROM tenant_channel_rules").
		WithArgs("acme").
		WillReturnRows(rows)

	result, err := registry.GetChannelRules(context.Background(), "acme")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if len(result.Public) != 1 || result.Public[0] != "*.metadata" {
		t.Errorf("Expected public pattern '*.metadata', got %v", result.Public)
	}
}

func TestPostgresRegistry_GetChannelRules_NotFound(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	registry := newTestPostgresRegistry(t, db)

	mock.ExpectQuery("SELECT rules FROM tenant_channel_rules").
		WithArgs("unknown").
		WillReturnError(sql.ErrNoRows)

	_, err = registry.GetChannelRules(context.Background(), "unknown")
	if !errors.Is(err, types.ErrChannelRulesNotFound) {
		t.Errorf("Expected ErrChannelRulesNotFound, got %v", err)
	}
}

func TestPostgresRegistry_GetChannelRules_InvalidJSON(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	registry := newTestPostgresRegistry(t, db)

	rows := sqlmock.NewRows([]string{"rules"}).AddRow([]byte(`{invalid json`))
	mock.ExpectQuery("SELECT rules FROM tenant_channel_rules").
		WithArgs("acme").
		WillReturnRows(rows)

	_, err = registry.GetChannelRules(context.Background(), "acme")
	if err == nil {
		t.Fatal("Expected unmarshal error")
	}
}

func TestPostgresRegistry_GetChannelRules_ValidationFails(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	registry := newTestPostgresRegistry(t, db)

	// Invalid rules: empty GroupMappings key fails Validate()
	rules := types.ChannelRules{
		Public: []string{"*.metadata"},
		GroupMappings: map[string][]string{
			"": {"*.trade"}, // empty group name should fail validation
		},
	}
	rulesJSON, _ := json.Marshal(rules)

	rows := sqlmock.NewRows([]string{"rules"}).AddRow(rulesJSON)
	mock.ExpectQuery("SELECT rules FROM tenant_channel_rules").
		WithArgs("acme").
		WillReturnRows(rows)

	_, err = registry.GetChannelRules(context.Background(), "acme")
	if err == nil {
		t.Fatal("Expected validation error")
	}
}

func TestPostgresRegistry_GetChannelRules_DBError(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	registry := newTestPostgresRegistry(t, db)

	mock.ExpectQuery("SELECT rules FROM tenant_channel_rules").
		WithArgs("acme").
		WillReturnError(errors.New("database down"))

	_, err = registry.GetChannelRules(context.Background(), "acme")
	if err == nil {
		t.Fatal("Expected error")
	}
	if errors.Is(err, types.ErrChannelRulesNotFound) {
		t.Error("Should not be ErrChannelRulesNotFound for DB errors")
	}
}

func TestPostgresRegistry_InvalidateTenantCache(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	registry := newTestPostgresRegistry(t, db)
	now := time.Now()

	// Populate OIDC config cache
	rows1 := sqlmock.NewRows([]string{"tenant_id", "issuer_url", "jwks_url", "audience", "enabled", "created_at", "updated_at"}).
		AddRow("acme", "https://auth.acme.com/", "https://auth.acme.com/.well-known/jwks.json", "api.acme.com", true, now, now)
	mock.ExpectQuery("SELECT tenant_id, issuer_url").
		WithArgs("acme").
		WillReturnRows(rows1)

	_, err = registry.GetOIDCConfig(context.Background(), "acme")
	if err != nil {
		t.Fatalf("GetOIDCConfig: %v", err)
	}

	// Invalidate
	registry.InvalidateTenantCache("acme")

	// Next call should hit DB again
	rows2 := sqlmock.NewRows([]string{"tenant_id", "issuer_url", "jwks_url", "audience", "enabled", "created_at", "updated_at"}).
		AddRow("acme", "https://auth.acme.com/", "https://auth.acme.com/.well-known/jwks.json", "api.acme.com", true, now, now)
	mock.ExpectQuery("SELECT tenant_id, issuer_url").
		WithArgs("acme").
		WillReturnRows(rows2)

	_, err = registry.GetOIDCConfig(context.Background(), "acme")
	if err != nil {
		t.Fatalf("GetOIDCConfig after invalidate: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unmet expectations: %v", err)
	}
}

func TestPostgresRegistry_InvalidateIssuerCache(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	registry := newTestPostgresRegistry(t, db)

	// Populate issuer cache
	rows1 := sqlmock.NewRows([]string{"tenant_id"}).AddRow("acme")
	mock.ExpectQuery("SELECT tenant_id FROM tenant_oidc_config").
		WithArgs("https://auth.acme.com").
		WillReturnRows(rows1)

	_, err = registry.GetTenantByIssuer(context.Background(), "https://auth.acme.com")
	if err != nil {
		t.Fatalf("GetTenantByIssuer: %v", err)
	}

	// Invalidate
	registry.InvalidateIssuerCache("https://auth.acme.com")

	// Next call should hit DB again
	rows2 := sqlmock.NewRows([]string{"tenant_id"}).AddRow("acme")
	mock.ExpectQuery("SELECT tenant_id FROM tenant_oidc_config").
		WithArgs("https://auth.acme.com").
		WillReturnRows(rows2)

	_, err = registry.GetTenantByIssuer(context.Background(), "https://auth.acme.com")
	if err != nil {
		t.Fatalf("GetTenantByIssuer after invalidate: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unmet expectations: %v", err)
	}
}

func TestPostgresRegistry_Close(t *testing.T) {
	t.Parallel()
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	registry := newTestPostgresRegistry(t, db)

	// Manually populate caches
	registry.issuerCacheMu.Lock()
	registry.issuerCache["test"] = &issuerCacheEntry{tenantID: "acme", expiresAt: time.Now().Add(time.Hour)}
	registry.issuerCacheMu.Unlock()

	registry.oidcConfigCacheMu.Lock()
	registry.oidcConfigCache["acme"] = &oidcConfigCacheEntry{config: &types.TenantOIDCConfig{}, expiresAt: time.Now().Add(time.Hour)}
	registry.oidcConfigCacheMu.Unlock()

	if err := registry.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Verify caches are cleared
	registry.issuerCacheMu.RLock()
	issuerCount := len(registry.issuerCache)
	registry.issuerCacheMu.RUnlock()

	registry.oidcConfigCacheMu.RLock()
	oidcCount := len(registry.oidcConfigCache)
	registry.oidcConfigCacheMu.RUnlock()

	registry.channelRulesCacheMu.RLock()
	rulesCount := len(registry.channelRulesCache)
	registry.channelRulesCacheMu.RUnlock()

	if issuerCount != 0 || oidcCount != 0 || rulesCount != 0 {
		t.Errorf("Caches not cleared: issuer=%d, oidc=%d, rules=%d", issuerCount, oidcCount, rulesCount)
	}
}
