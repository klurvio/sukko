package gateway

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog"

	pkgmetrics "github.com/klurvio/sukko/internal/shared/metrics"
	"github.com/klurvio/sukko/internal/shared/types"
)

// MultiIssuerOIDCConfig configures the multi-issuer OIDC validator.
type MultiIssuerOIDCConfig struct {
	// Registry provides issuer→tenant mappings and OIDC configs.
	Registry TenantRegistry

	// KeyfuncCacheTTL is how long to cache keyfuncs for each issuer.
	KeyfuncCacheTTL time.Duration

	// JWKSFetchTimeout is the timeout for fetching JWKS endpoints.
	JWKSFetchTimeout time.Duration

	// Logger for structured logging.
	Logger zerolog.Logger
}

// MultiIssuerOIDC manages JWKS keyfuncs for multiple OIDC issuers.
// Each tenant can have their own IdP, and we cache keyfuncs per issuer.
type MultiIssuerOIDC struct {
	registry         TenantRegistry
	keyfuncCacheTTL  time.Duration
	jwksFetchTimeout time.Duration
	logger           zerolog.Logger

	// keyfuncCache maps issuer URL → cached keyfunc
	keyfuncCache   map[string]*keyfuncCacheEntry
	keyfuncCacheMu sync.RWMutex

	// Context for managing keyfunc lifecycle
	ctx    context.Context
	cancel context.CancelFunc
}

type keyfuncCacheEntry struct {
	keyfunc       jwt.Keyfunc
	cancelRefresh context.CancelFunc
	expiresAt     time.Time
}

// NewMultiIssuerOIDC creates a new multi-issuer OIDC validator.
func NewMultiIssuerOIDC(cfg MultiIssuerOIDCConfig) (*MultiIssuerOIDC, error) {
	if cfg.Registry == nil {
		return nil, errors.New("registry is required")
	}
	if cfg.KeyfuncCacheTTL <= 0 {
		return nil, errors.New("KeyfuncCacheTTL must be > 0")
	}
	if cfg.JWKSFetchTimeout <= 0 {
		return nil, errors.New("JWKSFetchTimeout must be > 0")
	}

	ctx, cancel := context.WithCancel(context.Background())

	m := &MultiIssuerOIDC{
		registry:         cfg.Registry,
		keyfuncCacheTTL:  cfg.KeyfuncCacheTTL,
		jwksFetchTimeout: cfg.JWKSFetchTimeout,
		logger:           cfg.Logger,
		keyfuncCache:     make(map[string]*keyfuncCacheEntry),
		ctx:              ctx,
		cancel:           cancel,
	}

	cfg.Logger.Info().
		Dur("keyfunc_cache_ttl", cfg.KeyfuncCacheTTL).
		Dur("jwks_fetch_timeout", cfg.JWKSFetchTimeout).
		Msg("MultiIssuerOIDC initialized")

	return m, nil
}

// GetKeyfunc returns a jwt.Keyfunc for the given issuer URL.
// Creates and caches the keyfunc if not already cached.
func (m *MultiIssuerOIDC) GetKeyfunc(ctx context.Context, issuerURL string) (jwt.Keyfunc, error) {
	// Check cache first
	m.keyfuncCacheMu.RLock()
	entry, ok := m.keyfuncCache[issuerURL]
	m.keyfuncCacheMu.RUnlock()

	if ok && time.Now().Before(entry.expiresAt) {
		return entry.keyfunc, nil
	}

	// Need to create or refresh keyfunc
	return m.createKeyfunc(ctx, issuerURL)
}

// createKeyfunc creates a new keyfunc for the issuer.
func (m *MultiIssuerOIDC) createKeyfunc(ctx context.Context, issuerURL string) (jwt.Keyfunc, error) {
	// Get OIDC config from registry to get JWKS URL
	tenantID, err := m.registry.GetTenantByIssuer(ctx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("get tenant for issuer %s: %w", issuerURL, err)
	}

	config, err := m.registry.GetOIDCConfig(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("get OIDC config for tenant %s: %w", tenantID, err)
	}

	if !config.Enabled {
		return nil, types.ErrOIDCNotConfigured
	}

	jwksURL := config.GetJWKSURL()

	m.logger.Debug().
		Str("issuer_url", issuerURL).
		Str("jwks_url", jwksURL).
		Str("tenant_id", tenantID).
		Msg("Creating keyfunc for issuer")

	// Create keyfunc with refresh context derived from m.ctx
	refreshCtx, cancelRefresh := context.WithCancel(m.ctx)
	fetchStart := time.Now()

	//nolint:contextcheck // refreshCtx is derived from m.ctx via WithCancel above
	kf, err := keyfunc.NewDefaultCtx(refreshCtx, []string{jwksURL})
	if err != nil {
		cancelRefresh()
		RecordJWKSFetch(issuerURL, pkgmetrics.ResultError, time.Since(fetchStart))
		return nil, fmt.Errorf("create keyfunc for %s: %w", jwksURL, err)
	}

	RecordJWKSFetch(issuerURL, pkgmetrics.ResultSuccess, time.Since(fetchStart))

	// Lock for write and check again (double-checked locking)
	m.keyfuncCacheMu.Lock()
	defer m.keyfuncCacheMu.Unlock()

	// Close any existing keyfunc for this issuer
	if existing, ok := m.keyfuncCache[issuerURL]; ok {
		existing.cancelRefresh()
	}

	// Cache the new keyfunc
	m.keyfuncCache[issuerURL] = &keyfuncCacheEntry{
		keyfunc:       kf.Keyfunc,
		cancelRefresh: cancelRefresh,
		expiresAt:     time.Now().Add(m.keyfuncCacheTTL),
	}

	m.logger.Info().
		Str("issuer_url", issuerURL).
		Str("tenant_id", tenantID).
		Msg("Keyfunc created and cached for issuer")

	return kf.Keyfunc, nil
}

// GetTenantByIssuer returns the tenant ID for an issuer URL.
// This is a pass-through to the registry for convenience.
func (m *MultiIssuerOIDC) GetTenantByIssuer(ctx context.Context, issuerURL string) (string, error) {
	tenantID, err := m.registry.GetTenantByIssuer(ctx, issuerURL)
	if err != nil {
		return "", fmt.Errorf("get tenant by issuer: %w", err)
	}
	return tenantID, nil
}

// InvalidateIssuer removes a cached keyfunc for an issuer.
// Useful when OIDC config is updated.
func (m *MultiIssuerOIDC) InvalidateIssuer(issuerURL string) {
	m.keyfuncCacheMu.Lock()
	defer m.keyfuncCacheMu.Unlock()

	if entry, ok := m.keyfuncCache[issuerURL]; ok {
		entry.cancelRefresh()
		delete(m.keyfuncCache, issuerURL)
		m.logger.Debug().
			Str("issuer_url", issuerURL).
			Msg("Invalidated keyfunc cache for issuer")
	}
}

// Close stops all background refresh goroutines and releases resources.
func (m *MultiIssuerOIDC) Close() error {
	m.cancel() // Cancel all keyfunc refresh goroutines

	m.keyfuncCacheMu.Lock()
	defer m.keyfuncCacheMu.Unlock()

	// Clear the cache
	for issuer, entry := range m.keyfuncCache {
		entry.cancelRefresh()
		delete(m.keyfuncCache, issuer)
	}

	m.logger.Debug().Msg("MultiIssuerOIDC closed")
	return nil
}

// Compile-time check that MultiIssuerOIDC implements MultiIssuerValidator.
var _ MultiIssuerValidator = (*MultiIssuerOIDC)(nil)
