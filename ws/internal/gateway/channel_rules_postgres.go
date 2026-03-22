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

// maxChannelRulesCacheSize is the maximum number of tenants cached. When exceeded,
// the cache is cleared and refilled on demand — simple eviction without LRU overhead.
const maxChannelRulesCacheSize = 10000

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
	if rules, ok := r.getCachedRules(tenantID); ok {
		RecordChannelRulesLookup(LookupSourceCache)
		return rules, nil
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

	r.setCachedRules(tenantID, &rules)

	RecordChannelRulesLookup(LookupSourceDatabase)
	return &rules, nil
}

// getCachedRules returns cached channel rules if present and not expired.
func (r *PostgresChannelRulesProvider) getCachedRules(tenantID string) (*types.ChannelRules, bool) {
	r.channelRulesCacheMu.RLock()
	defer r.channelRulesCacheMu.RUnlock()

	entry, ok := r.channelRulesCache[tenantID]
	if ok && time.Now().Before(entry.expiresAt) {
		return entry.rules, true
	}
	return nil, false
}

// setCachedRules stores channel rules in the cache, evicting all entries if the cache is full.
func (r *PostgresChannelRulesProvider) setCachedRules(tenantID string, rules *types.ChannelRules) {
	r.channelRulesCacheMu.Lock()
	defer r.channelRulesCacheMu.Unlock()

	if len(r.channelRulesCache) >= maxChannelRulesCacheSize {
		r.channelRulesCache = make(map[string]*channelRulesCacheEntry)
		r.logger.Warn().Int("max_size", maxChannelRulesCacheSize).Msg("Channel rules cache evicted due to size limit")
	}
	r.channelRulesCache[tenantID] = &channelRulesCacheEntry{
		rules:     rules,
		expiresAt: time.Now().Add(r.channelRulesCacheTTL),
	}
}

// Close clears the cache. The database connection is managed externally.
func (r *PostgresChannelRulesProvider) Close() error {
	r.channelRulesCacheMu.Lock()
	defer r.channelRulesCacheMu.Unlock()

	r.channelRulesCache = make(map[string]*channelRulesCacheEntry)
	r.logger.Debug().Msg("PostgresChannelRulesProvider closed")
	return nil
}

// InvalidateTenantCache removes all cached data for a tenant.
// Useful when tenant config is updated.
func (r *PostgresChannelRulesProvider) InvalidateTenantCache(tenantID string) {
	r.channelRulesCacheMu.Lock()
	defer r.channelRulesCacheMu.Unlock()

	delete(r.channelRulesCache, tenantID)
}

// Compile-time check that PostgresChannelRulesProvider implements ChannelRulesProvider.
var _ ChannelRulesProvider = (*PostgresChannelRulesProvider)(nil)
