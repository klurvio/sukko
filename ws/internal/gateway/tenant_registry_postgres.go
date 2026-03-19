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

// PostgresTenantRegistryConfig configures the PostgreSQL-backed tenant registry.
type PostgresTenantRegistryConfig struct {
	// DB is the database connection pool.
	DB *sql.DB

	// IssuerCacheTTL is how long to cache issuer→tenant mappings.
	IssuerCacheTTL time.Duration

	// ChannelRulesCacheTTL is how long to cache channel rules.
	ChannelRulesCacheTTL time.Duration

	// QueryTimeout is the timeout for database queries.
	QueryTimeout time.Duration

	// Logger for structured logging.
	Logger zerolog.Logger
}

// PostgresTenantRegistry implements TenantRegistry with PostgreSQL backend and caching.
type PostgresTenantRegistry struct {
	db                   *sql.DB
	issuerCacheTTL       time.Duration
	channelRulesCacheTTL time.Duration
	queryTimeout         time.Duration
	logger               zerolog.Logger

	// issuerCache maps issuer URL → cached tenant info
	issuerCache   map[string]*issuerCacheEntry
	issuerCacheMu sync.RWMutex

	// oidcConfigCache maps tenant ID → cached OIDC config
	oidcConfigCache   map[string]*oidcConfigCacheEntry
	oidcConfigCacheMu sync.RWMutex

	// channelRulesCache maps tenant ID → cached channel rules
	channelRulesCache   map[string]*channelRulesCacheEntry
	channelRulesCacheMu sync.RWMutex
}

type issuerCacheEntry struct {
	tenantID  string
	expiresAt time.Time
}

type oidcConfigCacheEntry struct {
	config    *types.TenantOIDCConfig
	expiresAt time.Time
}

type channelRulesCacheEntry struct {
	rules     *types.ChannelRules
	expiresAt time.Time
}

// NewPostgresTenantRegistry creates a new PostgreSQL-backed tenant registry.
func NewPostgresTenantRegistry(cfg PostgresTenantRegistryConfig) (*PostgresTenantRegistry, error) {
	if cfg.DB == nil {
		return nil, errors.New("database connection is required")
	}
	if cfg.IssuerCacheTTL <= 0 {
		return nil, errors.New("IssuerCacheTTL must be > 0")
	}
	if cfg.ChannelRulesCacheTTL <= 0 {
		return nil, errors.New("ChannelRulesCacheTTL must be > 0")
	}
	if cfg.QueryTimeout <= 0 {
		return nil, errors.New("QueryTimeout must be > 0")
	}

	registry := &PostgresTenantRegistry{
		db:                   cfg.DB,
		issuerCacheTTL:       cfg.IssuerCacheTTL,
		channelRulesCacheTTL: cfg.ChannelRulesCacheTTL,
		queryTimeout:         cfg.QueryTimeout,
		logger:               cfg.Logger,
		issuerCache:          make(map[string]*issuerCacheEntry),
		oidcConfigCache:      make(map[string]*oidcConfigCacheEntry),
		channelRulesCache:    make(map[string]*channelRulesCacheEntry),
	}

	cfg.Logger.Info().
		Dur("issuer_cache_ttl", cfg.IssuerCacheTTL).
		Dur("channel_rules_cache_ttl", cfg.ChannelRulesCacheTTL).
		Msg("PostgresTenantRegistry initialized")

	return registry, nil
}

// GetTenantByIssuer returns the tenant ID for an OIDC issuer.
func (r *PostgresTenantRegistry) GetTenantByIssuer(ctx context.Context, issuerURL string) (string, error) {
	// Check cache first
	r.issuerCacheMu.RLock()
	entry, ok := r.issuerCache[issuerURL]
	r.issuerCacheMu.RUnlock()

	if ok && time.Now().Before(entry.expiresAt) {
		issuerCacheHits.Inc()
		return entry.tenantID, nil
	}
	issuerCacheMisses.Inc()

	// Query database
	queryCtx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	var tenantID string
	err := r.db.QueryRowContext(queryCtx, `
		SELECT tenant_id FROM tenant_oidc_config
		WHERE issuer_url = $1 AND enabled = true
	`, issuerURL).Scan(&tenantID)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", types.ErrIssuerNotFound
		}
		return "", fmt.Errorf("query tenant by issuer %s: %w", issuerURL, err)
	}

	// Update cache
	r.issuerCacheMu.Lock()
	r.issuerCache[issuerURL] = &issuerCacheEntry{
		tenantID:  tenantID,
		expiresAt: time.Now().Add(r.issuerCacheTTL),
	}
	r.issuerCacheMu.Unlock()

	return tenantID, nil
}

// GetOIDCConfig returns the OIDC configuration for a tenant.
func (r *PostgresTenantRegistry) GetOIDCConfig(ctx context.Context, tenantID string) (*types.TenantOIDCConfig, error) {
	// Check cache first
	r.oidcConfigCacheMu.RLock()
	entry, ok := r.oidcConfigCache[tenantID]
	r.oidcConfigCacheMu.RUnlock()

	if ok && time.Now().Before(entry.expiresAt) {
		return entry.config, nil
	}

	// Query database
	queryCtx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	var config types.TenantOIDCConfig
	var jwksURL, audience sql.NullString
	err := r.db.QueryRowContext(queryCtx, `
		SELECT tenant_id, issuer_url, jwks_url, audience, enabled, created_at, updated_at
		FROM tenant_oidc_config
		WHERE tenant_id = $1
	`, tenantID).Scan(
		&config.TenantID,
		&config.IssuerURL,
		&jwksURL,
		&audience,
		&config.Enabled,
		&config.CreatedAt,
		&config.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, types.ErrOIDCNotConfigured
		}
		return nil, fmt.Errorf("query OIDC config for tenant %s: %w", tenantID, err)
	}

	config.JWKSURL = jwksURL.String
	config.Audience = audience.String

	// Defense in depth: validate even though provisioning validated
	if err := config.Validate(); err != nil {
		r.logger.Error().
			Err(err).
			Str("tenant_id", tenantID).
			Msg("Cached OIDC config failed validation")
		return nil, fmt.Errorf("validate cached config: %w", err)
	}

	// Update cache (reuses issuer cache TTL — both are tenant config with similar staleness tolerance)
	r.oidcConfigCacheMu.Lock()
	r.oidcConfigCache[tenantID] = &oidcConfigCacheEntry{
		config:    &config,
		expiresAt: time.Now().Add(r.issuerCacheTTL),
	}
	r.oidcConfigCacheMu.Unlock()

	return &config, nil
}

// GetChannelRules returns the channel rules for a tenant.
func (r *PostgresTenantRegistry) GetChannelRules(ctx context.Context, tenantID string) (*types.ChannelRules, error) {
	// Check cache first
	r.channelRulesCacheMu.RLock()
	entry, ok := r.channelRulesCache[tenantID]
	r.channelRulesCacheMu.RUnlock()

	if ok && time.Now().Before(entry.expiresAt) {
		channelRulesLookupTotal.WithLabelValues(tenantID, LookupSourceCache).Inc()
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

	channelRulesLookupTotal.WithLabelValues(tenantID, LookupSourceDatabase).Inc()
	return &rules, nil
}

// Close clears the cache. The database connection is managed externally.
func (r *PostgresTenantRegistry) Close() error {
	r.issuerCacheMu.Lock()
	r.issuerCache = make(map[string]*issuerCacheEntry)
	r.issuerCacheMu.Unlock()

	r.oidcConfigCacheMu.Lock()
	r.oidcConfigCache = make(map[string]*oidcConfigCacheEntry)
	r.oidcConfigCacheMu.Unlock()

	r.channelRulesCacheMu.Lock()
	r.channelRulesCache = make(map[string]*channelRulesCacheEntry)
	r.channelRulesCacheMu.Unlock()

	r.logger.Debug().Msg("PostgresTenantRegistry closed")
	return nil
}

// InvalidateIssuerCache removes a specific issuer from the cache.
// Useful when OIDC config is updated.
func (r *PostgresTenantRegistry) InvalidateIssuerCache(issuerURL string) {
	r.issuerCacheMu.Lock()
	delete(r.issuerCache, issuerURL)
	r.issuerCacheMu.Unlock()
}

// InvalidateTenantCache removes all cached data for a tenant.
// Useful when tenant config is updated.
func (r *PostgresTenantRegistry) InvalidateTenantCache(tenantID string) {
	r.oidcConfigCacheMu.Lock()
	delete(r.oidcConfigCache, tenantID)
	r.oidcConfigCacheMu.Unlock()

	r.channelRulesCacheMu.Lock()
	delete(r.channelRulesCache, tenantID)
	r.channelRulesCacheMu.Unlock()
}

// Compile-time check that PostgresTenantRegistry implements TenantRegistry.
var _ TenantRegistry = (*PostgresTenantRegistry)(nil)
