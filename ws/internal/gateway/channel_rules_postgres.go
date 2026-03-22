package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/shared/types"
)

// PostgresChannelRulesProviderConfig configures the PostgreSQL-backed channel rules provider.
type PostgresChannelRulesProviderConfig struct {
	// DB is the database connection pool.
	DB *sql.DB

	// ChannelRulesCacheTTL is how long to cache channel rules.
	ChannelRulesCacheTTL time.Duration

	// QueryTimeout is the timeout for database queries.
	QueryTimeout time.Duration

	// Logger for structured logging.
	Logger zerolog.Logger
}

// PostgresChannelRulesProvider implements ChannelRulesProvider with PostgreSQL backend and caching.
type PostgresChannelRulesProvider struct {
	db                   *sql.DB
	channelRulesCacheTTL time.Duration
	queryTimeout         time.Duration
	logger               zerolog.Logger

	// channelRulesCache maps tenant ID → cached channel rules
	channelRulesCache   map[string]*channelRulesCacheEntry
	channelRulesCacheMu sync.RWMutex
}

type channelRulesCacheEntry struct {
	rules     *types.ChannelRules
	expiresAt time.Time
}

// NewPostgresChannelRulesProvider creates a new PostgreSQL-backed channel rules provider.
func NewPostgresChannelRulesProvider(cfg PostgresChannelRulesProviderConfig) (*PostgresChannelRulesProvider, error) {
	if cfg.DB == nil {
		return nil, errors.New("database connection is required")
	}
	if cfg.ChannelRulesCacheTTL <= 0 {
		return nil, errors.New("ChannelRulesCacheTTL must be > 0")
	}
	if cfg.QueryTimeout <= 0 {
		return nil, errors.New("QueryTimeout must be > 0")
	}

	provider := &PostgresChannelRulesProvider{
		db:                   cfg.DB,
		channelRulesCacheTTL: cfg.ChannelRulesCacheTTL,
		queryTimeout:         cfg.QueryTimeout,
		logger:               cfg.Logger,
		channelRulesCache:    make(map[string]*channelRulesCacheEntry),
	}

	cfg.Logger.Info().
		Dur("channel_rules_cache_ttl", cfg.ChannelRulesCacheTTL).
		Msg("PostgresChannelRulesProvider initialized")

	return provider, nil
}

// GetChannelRules returns the channel rules for a tenant.
func (r *PostgresChannelRulesProvider) GetChannelRules(ctx context.Context, tenantID string) (*types.ChannelRules, error) {
	// Check cache first
	r.channelRulesCacheMu.RLock()
	entry, ok := r.channelRulesCache[tenantID]
	r.channelRulesCacheMu.RUnlock()

	if ok && time.Now().Before(entry.expiresAt) {
		RecordChannelRulesLookup(tenantID, LookupSourceCache)
		return entry.rules, nil
	}

	// Query database
	queryCtx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	var rulesJSON []byte
	err := r.db.QueryRowContext(queryCtx, `
		SELECT rules FROM tenant_channel_rules
		WHERE tenant_id = $1
	`, tenantID).Scan(&rulesJSON)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, types.ErrChannelRulesNotFound
		}
		return nil, fmt.Errorf("query channel rules for tenant %s: %w", tenantID, err)
	}

	var rules types.ChannelRules
	if err := json.Unmarshal(rulesJSON, &rules); err != nil {
		return nil, fmt.Errorf("unmarshal channel rules for tenant %s: %w", tenantID, err)
	}

	// Defense in depth: validate even though provisioning validated
	if err := rules.Validate(); err != nil {
		r.logger.Error().
			Err(err).
			Str("tenant_id", tenantID).
			Msg("Cached channel rules failed validation")
		return nil, fmt.Errorf("validate cached rules: %w", err)
	}

	// Update cache
	r.channelRulesCacheMu.Lock()
	r.channelRulesCache[tenantID] = &channelRulesCacheEntry{
		rules:     &rules,
		expiresAt: time.Now().Add(r.channelRulesCacheTTL),
	}
	r.channelRulesCacheMu.Unlock()

	RecordChannelRulesLookup(tenantID, LookupSourceDatabase)
	return &rules, nil
}

// Close clears the cache. The database connection is managed externally.
func (r *PostgresChannelRulesProvider) Close() error {
	r.channelRulesCacheMu.Lock()
	r.channelRulesCache = make(map[string]*channelRulesCacheEntry)
	r.channelRulesCacheMu.Unlock()

	r.logger.Debug().Msg("PostgresChannelRulesProvider closed")
	return nil
}

// InvalidateTenantCache removes all cached data for a tenant.
// Useful when tenant config is updated.
func (r *PostgresChannelRulesProvider) InvalidateTenantCache(tenantID string) {
	r.channelRulesCacheMu.Lock()
	delete(r.channelRulesCache, tenantID)
	r.channelRulesCacheMu.Unlock()
}

// Compile-time check that PostgresChannelRulesProvider implements ChannelRulesProvider.
var _ ChannelRulesProvider = (*PostgresChannelRulesProvider)(nil)
