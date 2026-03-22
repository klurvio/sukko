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

// newTestPostgresProvider creates a PostgresChannelRulesProvider with short cache TTLs for testing.
func newTestPostgresProvider(t *testing.T, db *sql.DB) *PostgresChannelRulesProvider {
	t.Helper()
	provider, err := NewPostgresChannelRulesProvider(PostgresChannelRulesProviderConfig{
		DB:                   db,
		ChannelRulesCacheTTL: 100 * time.Millisecond,
		QueryTimeout:         1 * time.Second,
		Logger:               zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("NewPostgresChannelRulesProvider: %v", err)
	}
	return provider
}

func TestPostgresProvider_New_NilDB(t *testing.T) {
	t.Parallel()
	_, err := NewPostgresChannelRulesProvider(PostgresChannelRulesProviderConfig{
		DB:     nil,
		Logger: zerolog.Nop(),
	})
	if err == nil {
		t.Fatal("Expected error for nil DB")
	}
}

func TestPostgresProvider_GetChannelRules_Found(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	provider := newTestPostgresProvider(t, db)

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

	result, err := provider.GetChannelRules(context.Background(), "acme")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if len(result.Public) != 1 || result.Public[0] != "*.metadata" {
		t.Errorf("Expected public pattern '*.metadata', got %v", result.Public)
	}
}

func TestPostgresProvider_GetChannelRules_NotFound(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	provider := newTestPostgresProvider(t, db)

	mock.ExpectQuery("SELECT rules FROM tenant_channel_rules").
		WithArgs("unknown").
		WillReturnError(sql.ErrNoRows)

	_, err = provider.GetChannelRules(context.Background(), "unknown")
	if !errors.Is(err, types.ErrChannelRulesNotFound) {
		t.Errorf("Expected ErrChannelRulesNotFound, got %v", err)
	}
}

func TestPostgresProvider_GetChannelRules_InvalidJSON(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	provider := newTestPostgresProvider(t, db)

	rows := sqlmock.NewRows([]string{"rules"}).AddRow([]byte(`{invalid json`))
	mock.ExpectQuery("SELECT rules FROM tenant_channel_rules").
		WithArgs("acme").
		WillReturnRows(rows)

	_, err = provider.GetChannelRules(context.Background(), "acme")
	if err == nil {
		t.Fatal("Expected unmarshal error")
	}
}

func TestPostgresProvider_GetChannelRules_ValidationFails(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	provider := newTestPostgresProvider(t, db)

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

	_, err = provider.GetChannelRules(context.Background(), "acme")
	if err == nil {
		t.Fatal("Expected validation error")
	}
}

func TestPostgresProvider_GetChannelRules_DBError(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	provider := newTestPostgresProvider(t, db)

	mock.ExpectQuery("SELECT rules FROM tenant_channel_rules").
		WithArgs("acme").
		WillReturnError(errors.New("database down"))

	_, err = provider.GetChannelRules(context.Background(), "acme")
	if err == nil {
		t.Fatal("Expected error")
	}
	if errors.Is(err, types.ErrChannelRulesNotFound) {
		t.Error("Should not be ErrChannelRulesNotFound for DB errors")
	}
}

func TestPostgresProvider_GetChannelRules_CacheHit(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	provider := newTestPostgresProvider(t, db)

	rules := types.ChannelRules{
		Public:        []string{"*.metadata"},
		GroupMappings: map[string][]string{},
	}
	rulesJSON, _ := json.Marshal(rules)

	// First call: query DB
	rows := sqlmock.NewRows([]string{"rules"}).AddRow(rulesJSON)
	mock.ExpectQuery("SELECT rules FROM tenant_channel_rules").
		WithArgs("acme").
		WillReturnRows(rows)

	result, err := provider.GetChannelRules(context.Background(), "acme")
	if err != nil {
		t.Fatalf("First call: %v", err)
	}
	if len(result.Public) != 1 {
		t.Errorf("First call: expected 1 public pattern, got %d", len(result.Public))
	}

	// Second call: should use cache (no new DB expectation)
	result, err = provider.GetChannelRules(context.Background(), "acme")
	if err != nil {
		t.Fatalf("Second call: %v", err)
	}
	if len(result.Public) != 1 {
		t.Errorf("Second call: expected 1 public pattern, got %d", len(result.Public))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unmet expectations: %v", err)
	}
}

func TestPostgresProvider_GetChannelRules_CacheExpired(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	provider := newTestPostgresProvider(t, db)

	rules := types.ChannelRules{
		Public:        []string{"*.metadata"},
		GroupMappings: map[string][]string{},
	}
	rulesJSON, _ := json.Marshal(rules)

	// First query
	rows1 := sqlmock.NewRows([]string{"rules"}).AddRow(rulesJSON)
	mock.ExpectQuery("SELECT rules FROM tenant_channel_rules").
		WithArgs("acme").
		WillReturnRows(rows1)

	_, err = provider.GetChannelRules(context.Background(), "acme")
	if err != nil {
		t.Fatalf("First call: %v", err)
	}

	// Wait for cache expiry
	time.Sleep(150 * time.Millisecond)

	// Second query — should hit DB again
	rows2 := sqlmock.NewRows([]string{"rules"}).AddRow(rulesJSON)
	mock.ExpectQuery("SELECT rules FROM tenant_channel_rules").
		WithArgs("acme").
		WillReturnRows(rows2)

	_, err = provider.GetChannelRules(context.Background(), "acme")
	if err != nil {
		t.Fatalf("Second call: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unmet expectations: %v", err)
	}
}

func TestPostgresProvider_InvalidateTenantCache(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	provider := newTestPostgresProvider(t, db)

	rules := types.ChannelRules{
		Public:        []string{"*.metadata"},
		GroupMappings: map[string][]string{},
	}
	rulesJSON, _ := json.Marshal(rules)

	// Populate cache
	rows1 := sqlmock.NewRows([]string{"rules"}).AddRow(rulesJSON)
	mock.ExpectQuery("SELECT rules FROM tenant_channel_rules").
		WithArgs("acme").
		WillReturnRows(rows1)

	_, err = provider.GetChannelRules(context.Background(), "acme")
	if err != nil {
		t.Fatalf("GetChannelRules: %v", err)
	}

	// Invalidate
	provider.InvalidateTenantCache("acme")

	// Next call should hit DB again
	rows2 := sqlmock.NewRows([]string{"rules"}).AddRow(rulesJSON)
	mock.ExpectQuery("SELECT rules FROM tenant_channel_rules").
		WithArgs("acme").
		WillReturnRows(rows2)

	_, err = provider.GetChannelRules(context.Background(), "acme")
	if err != nil {
		t.Fatalf("GetChannelRules after invalidate: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unmet expectations: %v", err)
	}
}

func TestPostgresProvider_Close(t *testing.T) {
	t.Parallel()
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	provider := newTestPostgresProvider(t, db)

	// Manually populate cache
	provider.channelRulesCacheMu.Lock()
	provider.channelRulesCache["acme"] = &channelRulesCacheEntry{
		rules:     &types.ChannelRules{Public: []string{"*.test"}},
		expiresAt: time.Now().Add(time.Hour),
	}
	provider.channelRulesCacheMu.Unlock()

	if err := provider.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Verify cache is cleared
	provider.channelRulesCacheMu.RLock()
	rulesCount := len(provider.channelRulesCache)
	provider.channelRulesCacheMu.RUnlock()

	if rulesCount != 0 {
		t.Errorf("Cache not cleared: rules=%d", rulesCount)
	}
}
