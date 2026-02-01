# Plan: OIDC Multi-Issuer Support & Per-Tenant Channel Rules

## Summary

Extend the authentication system to support multiple OIDC issuers (one per tenant) and per-tenant channel access rules. This enables third-party tenants to use their own Identity Providers while we control channel authorization.

## References

- **Coding Guidelines**: `docs/architecture/CODING_GUIDELINES.md`
- **Auth Documentation**: `docs/architecture/MULTI_TENANT_AUTH_OIDC.md`

---

## Current State

### What Exists

| Component | Current Implementation | Location |
|-----------|----------------------|----------|
| Claims struct | Has `TenantID`, `Groups`, `Roles` | `internal/shared/auth/jwt.go` |
| MultiTenantValidator | Single OIDC issuer, tenant keys via KeyRegistry | `internal/shared/auth/jwt_multitenant.go` |
| OIDC support | Single issuer, single JWKS URL | `internal/shared/auth/oidc.go` |
| PermissionChecker | Global patterns (same for all tenants) | `internal/gateway/permissions.go` |
| Tenant model | ID, Name, Status, Metadata | `internal/provisioning/types.go` |
| KeyRegistry | Loads tenant signing keys from PostgreSQL | `internal/shared/auth/keys_postgres.go` |

### What's Missing

| Feature | Description |
|---------|-------------|
| Multiple OIDC issuers | Each tenant can register their own IdP |
| Issuer → Tenant mapping | Lookup tenant from OIDC token issuer |
| Per-tenant channel rules | Group → channel pattern mapping per tenant |
| Channel rules API | CRUD for tenant channel access configuration |
| OIDC config API | CRUD for tenant OIDC issuer registration |

---

## Architecture Changes

### Before (Current)

```
                           ┌─────────────────────┐
                           │  Gateway Config     │
                           │  (single OIDC       │
                           │   issuer)           │
                           └─────────────────────┘
                                    │
OIDC Token ──────────────────────────────────────► Gateway
(from single IdP)                   │              - Validate via single JWKS
                                    │              - Global channel patterns
                                    ▼
                           ┌─────────────────────┐
                           │  PermissionChecker  │
                           │  (global patterns)  │
                           └─────────────────────┘
```

### After (New)

```
                           ┌─────────────────────┐
                           │  TenantRegistry     │
                           │  - OIDC configs     │
                           │  - Channel rules    │
                           └─────────────────────┘
                                    │
                                    │ Cache
                                    ▼
OIDC Token ──────────────────────────────────────► Gateway
(from any registered IdP)          │              - Lookup issuer → tenant
                                   │              - Validate via tenant's JWKS
                                   │              - Load tenant's channel rules
                                   ▼
                           ┌─────────────────────┐
                           │  PermissionChecker  │
                           │  (per-tenant rules) │
                           └─────────────────────┘
```

---

## Configuration (per Coding Guidelines)

All values MUST be configurable via environment variables. No hardcoded values.

### Gateway Config Additions

**File: `internal/shared/platform/gateway_config.go`**

```go
type GatewayConfig struct {
    // ... existing fields ...

    // Multi-Issuer OIDC (Feature Flag)
    MultiIssuerOIDCEnabled bool `env:"GATEWAY_MULTI_ISSUER_OIDC_ENABLED" envDefault:"false"`

    // Per-Tenant Channel Rules (Feature Flag)
    PerTenantChannelRulesEnabled bool `env:"GATEWAY_PER_TENANT_CHANNEL_RULES" envDefault:"false"`

    // TenantRegistry Cache Settings
    IssuerCacheTTL       time.Duration `env:"GATEWAY_ISSUER_CACHE_TTL" envDefault:"5m"`
    ChannelRulesCacheTTL time.Duration `env:"GATEWAY_CHANNEL_RULES_CACHE_TTL" envDefault:"1m"`
    OIDCKeyfuncCacheTTL  time.Duration `env:"GATEWAY_OIDC_KEYFUNC_CACHE_TTL" envDefault:"1h"`

    // JWKS Fetch Settings
    JWKSFetchTimeout     time.Duration `env:"GATEWAY_JWKS_FETCH_TIMEOUT" envDefault:"10s"`
    JWKSRefreshInterval  time.Duration `env:"GATEWAY_JWKS_REFRESH_INTERVAL" envDefault:"1h"`

    // Fallback Channel Rules (when tenant has none configured)
    FallbackPublicChannels []string `env:"GATEWAY_FALLBACK_PUBLIC_CHANNELS" envDefault:"*.metadata"`
}

// Validate checks configuration. Called at startup.
func (c *GatewayConfig) Validate() error {
    // ... existing validation ...

    if c.MultiIssuerOIDCEnabled {
        if c.IssuerCacheTTL < time.Second {
            return fmt.Errorf("GATEWAY_ISSUER_CACHE_TTL must be >= 1s, got %v", c.IssuerCacheTTL)
        }
        if c.JWKSFetchTimeout < time.Second {
            return fmt.Errorf("GATEWAY_JWKS_FETCH_TIMEOUT must be >= 1s, got %v", c.JWKSFetchTimeout)
        }
    }

    return nil
}
```

---

## Database Schema Changes

### New Tables

```sql
-- Tenant OIDC configuration
-- Allows each tenant to register their Identity Provider
CREATE TABLE tenant_oidc_config (
    tenant_id TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    issuer_url TEXT NOT NULL,
    jwks_url TEXT,                     -- Optional override, defaults to {issuer}/.well-known/jwks.json
    audience TEXT,                     -- Expected audience claim
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT tenant_oidc_issuer_unique UNIQUE (issuer_url)
);

-- Index for issuer lookup (gateway hot path)
CREATE UNIQUE INDEX idx_tenant_oidc_issuer_enabled ON tenant_oidc_config(issuer_url) WHERE enabled = true;

-- Tenant channel access rules
-- Maps IdP groups to allowed channel patterns
CREATE TABLE tenant_channel_rules (
    tenant_id TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    rules JSONB NOT NULL DEFAULT '{"public": [], "group_mappings": {}, "default": []}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

---

## Input Validation (per Coding Guidelines: Defense in Depth)

Every input MUST be validated at the boundary. Never assume upstream validation.

Validation is implemented in `internal/shared/types/` and called by both:
- **Provisioning API** - on create/update operations
- **Gateway** - defense in depth (re-validate even though provisioning validated)

### Validation Location

```
internal/shared/types/
├── oidc.go           # TenantOIDCConfig.Validate()
├── channel_rules.go  # ChannelRules.Validate(), IsValidChannelPattern()
├── errors.go         # Sentinel errors (ErrInvalidIssuerURL, etc.)
└── constants.go      # MaxAudienceLength, MaxGroupNameLength, etc.
```

### Usage Pattern

```go
// Provisioning API - validate on create
func (h *Handler) CreateOIDCConfig(w http.ResponseWriter, r *http.Request) {
    var config types.TenantOIDCConfig
    if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
        http.Error(w, "invalid request body", http.StatusBadRequest)
        return
    }

    // Validation using shared types
    if err := config.Validate(); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    // ...
}

// Gateway - defense in depth (re-validate cached data)
func (r *CachedTenantRegistry) GetOIDCConfig(ctx context.Context, tenantID string) (*types.TenantOIDCConfig, error) {
    config, err := r.cache.Get(tenantID)
    if err != nil {
        return nil, fmt.Errorf("cache get: %w", err)
    }

    // Defense in depth: validate even though provisioning validated
    if err := config.Validate(); err != nil {
        r.logger.Error().Err(err).Str("tenant_id", tenantID).Msg("Cached OIDC config failed validation")
        return nil, fmt.Errorf("validate cached config: %w", err)
    }

    return config, nil
}
```

---

## Graceful Degradation (per Coding Guidelines)

Optional dependencies must have defined degraded behavior. Use noop implementations.

### Dependency Classification

| Dependency | Classification | On Failure | Degraded Behavior |
|------------|---------------|------------|-------------------|
| TenantRegistry (DB) | Optional | Log warning, use fallback | Tenant-signed JWTs only, no OIDC |
| OIDC JWKS endpoint | Optional (per issuer) | Log warning, skip issuer | That issuer's tokens rejected |
| Channel Rules lookup | Optional | Log warning, use fallback | Fallback public patterns only |
| Issuer cache | Non-critical | Refresh from DB | Increased DB load |

### Noop Implementations

**File: `internal/gateway/tenant_registry_noop.go`**

```go
package gateway

import (
    "context"

    "github.com/rs/zerolog"

    "github.com/Toniq-Labs/odin-ws/internal/shared/types"
)

// NoopTenantRegistry provides fallback when database is unavailable.
// Used for graceful degradation - OIDC disabled, tenant-signed JWTs only.
type NoopTenantRegistry struct {
    logger zerolog.Logger
}

func NewNoopTenantRegistry(logger zerolog.Logger) *NoopTenantRegistry {
    logger.Warn().Msg("Using NoopTenantRegistry - OIDC multi-issuer disabled")
    return &NoopTenantRegistry{logger: logger}
}

func (n *NoopTenantRegistry) GetTenantByIssuer(ctx context.Context, issuerURL string) (string, error) {
    return "", types.ErrIssuerNotFound // Forces fallback to tenant-signed JWT validation
}

func (n *NoopTenantRegistry) GetOIDCConfig(ctx context.Context, tenantID string) (*types.TenantOIDCConfig, error) {
    return nil, types.ErrOIDCNotConfigured
}

func (n *NoopTenantRegistry) GetChannelRules(ctx context.Context, tenantID string) (*types.ChannelRules, error) {
    return nil, types.ErrChannelRulesNotFound // Forces fallback to default rules
}
```

### Fallback Channel Rules

```go
// DefaultChannelRules returns fallback rules when tenant has none configured.
func DefaultChannelRules(config *platform.GatewayConfig) *types.ChannelRules {
    return &types.ChannelRules{
        Public:        config.FallbackPublicChannels,
        GroupMappings: map[string][]string{},
        Default:       []string{},
    }
}
```

### Gateway Initialization with Graceful Degradation

```go
func (gw *Gateway) setupTenantRegistry() {
    if !gw.config.MultiIssuerOIDCEnabled {
        gw.tenantRegistry = NewNoopTenantRegistry(gw.logger)
        return
    }

    // Try to create PostgreSQL-backed registry
    registry, err := NewPostgresTenantRegistry(PostgresTenantRegistryConfig{
        DB:                   gw.dbConn,
        IssuerCacheTTL:       gw.config.IssuerCacheTTL,
        ChannelRulesCacheTTL: gw.config.ChannelRulesCacheTTL,
        Logger:               gw.logger,
    })
    if err != nil {
        // Graceful degradation: log warning and use noop
        gw.logger.Warn().
            Err(err).
            Msg("Failed to create TenantRegistry, using noop (OIDC multi-issuer disabled)")
        gw.tenantRegistry = NewNoopTenantRegistry(gw.logger)
        return
    }

    gw.tenantRegistry = registry
    gw.logger.Info().Msg("TenantRegistry initialized with PostgreSQL backend")
}
```

---

## Metrics (per Coding Guidelines: Observable by Default)

Every significant operation must be metriced. Silent failures are forbidden.

### New Metrics

```go
var (
    // OIDC validation metrics
    oidcValidationTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "gateway_oidc_validation_total",
        Help: "Total OIDC token validations",
    }, []string{"tenant_id", "result"}) // result: success, invalid_signature, unknown_issuer, expired

    oidcValidationLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
        Name:    "gateway_oidc_validation_latency_seconds",
        Help:    "OIDC token validation latency",
        Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0},
    }, []string{"tenant_id"})

    // Issuer cache metrics
    issuerCacheHits = prometheus.NewCounter(prometheus.CounterOpts{
        Name: "gateway_issuer_cache_hits_total",
        Help: "Issuer cache hits",
    })
    issuerCacheMisses = prometheus.NewCounter(prometheus.CounterOpts{
        Name: "gateway_issuer_cache_misses_total",
        Help: "Issuer cache misses",
    })

    // Channel rules metrics
    channelRulesLookupTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "gateway_channel_rules_lookup_total",
        Help: "Channel rules lookups",
    }, []string{"tenant_id", "source"}) // source: cache, database, fallback

    channelAuthorizationTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "gateway_channel_authorization_total",
        Help: "Channel authorization decisions",
    }, []string{"tenant_id", "result"}) // result: allowed, denied

    // JWKS fetch metrics
    jwksFetchTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "gateway_jwks_fetch_total",
        Help: "JWKS endpoint fetches",
    }, []string{"issuer", "result"}) // result: success, timeout, error

    jwksFetchLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
        Name:    "gateway_jwks_fetch_latency_seconds",
        Help:    "JWKS fetch latency",
        Buckets: []float64{0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0},
    }, []string{"issuer"})
)

func init() {
    prometheus.MustRegister(
        oidcValidationTotal,
        oidcValidationLatency,
        issuerCacheHits,
        issuerCacheMisses,
        channelRulesLookupTotal,
        channelAuthorizationTotal,
        jwksFetchTotal,
        jwksFetchLatency,
    )
}
```

---

## Audit Logging (per Coding Guidelines)

Security-sensitive operations must use audit logging.

### Audit Events

```go
// Audit action constants for OIDC and channel rules
const (
    ActionCreateOIDCConfig = "create_oidc_config"
    ActionUpdateOIDCConfig = "update_oidc_config"
    ActionDeleteOIDCConfig = "delete_oidc_config"
    ActionSetChannelRules  = "set_channel_rules"
    ActionDeleteChannelRules = "delete_channel_rules"
)

// Example usage in handler
func (h *Handler) CreateOIDCConfig(w http.ResponseWriter, r *http.Request) {
    // ... validation and creation ...

    // Audit log the security-sensitive action
    h.audit.Info(ActionCreateOIDCConfig, "OIDC configuration created", map[string]any{
        "tenant_id":   tenantID,
        "issuer_url":  config.IssuerURL,
        "actor":       claims.Subject,
        "ip_address":  r.RemoteAddr,
    })
}

func (h *Handler) SetChannelRules(w http.ResponseWriter, r *http.Request) {
    // ... validation and update ...

    // Audit log with before/after for compliance
    h.audit.Info(ActionSetChannelRules, "Channel rules updated", map[string]any{
        "tenant_id":     tenantID,
        "groups_added":  groupsAdded,
        "groups_removed": groupsRemoved,
        "actor":         claims.Subject,
        "ip_address":    r.RemoteAddr,
    })
}
```

---

## Error Handling (per Coding Guidelines)

All errors must be wrapped with context using `fmt.Errorf` and `%w`.

### Sentinel Errors (in `internal/shared/types/errors.go`)

```go
package types

import "errors"

// Sentinel errors for OIDC configuration.
var (
    ErrIssuerNotFound      = errors.New("issuer not found")
    ErrIssuerAlreadyExists = errors.New("issuer already registered to another tenant")
    ErrOIDCNotConfigured   = errors.New("OIDC not configured for tenant")
    ErrIssuerURLRequired   = errors.New("issuer URL is required")
    ErrInvalidIssuerURL    = errors.New("invalid issuer URL")
    ErrInvalidJWKSURL      = errors.New("invalid JWKS URL")
)

// Sentinel errors for channel rules.
var (
    ErrChannelRulesNotFound  = errors.New("channel rules not found")
    ErrInvalidChannelPattern = errors.New("invalid channel pattern")
    ErrEmptyGroupName        = errors.New("group name cannot be empty")
)
```

### Error Wrapping Examples

```go
func (r *PostgresOIDCRepository) GetByIssuer(ctx context.Context, issuerURL string) (*types.TenantOIDCConfig, error) {
    var config types.TenantOIDCConfig
    err := r.db.QueryRowContext(ctx, `
        SELECT tenant_id, issuer_url, jwks_url, audience, enabled, created_at, updated_at
        FROM tenant_oidc_config
        WHERE issuer_url = $1 AND enabled = true
    `, issuerURL).Scan(
        &config.TenantID, &config.IssuerURL, &config.JWKSURL,
        &config.Audience, &config.Enabled, &config.CreatedAt, &config.UpdatedAt,
    )
    if err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return nil, types.ErrIssuerNotFound
        }
        return nil, fmt.Errorf("query oidc config by issuer %s: %w", issuerURL, err)
    }
    return &config, nil
}
```

---

## Code Changes

### Shared Package Organization

Components used by multiple services (gateway, provisioning) go in `internal/shared/`.

#### Shared Types Location Decision

| Component | Used By | Location | Rationale |
|-----------|---------|----------|-----------|
| `TenantOIDCConfig` | provisioning (write), gateway (read) | `internal/shared/types/` | Shared data structure |
| `ChannelRules` | provisioning (write), gateway (read) | `internal/shared/types/` | Shared data structure |
| `ValidateOIDCConfig()` | provisioning, gateway (defense in depth) | `internal/shared/types/` | Shared validation |
| `ValidateChannelRules()` | provisioning, gateway (defense in depth) | `internal/shared/types/` | Shared validation |
| Sentinel errors | provisioning, gateway | `internal/shared/types/` | Shared error handling |
| `TenantRegistry` interface | gateway (consumer) | `internal/gateway/` | Interface at point of use |
| Audit action constants | provisioning only | `internal/provisioning/` | Service-specific |
| Repository interfaces | provisioning only | `internal/provisioning/` | Service-specific |

---

### 1. Shared Types Package

**File: `internal/shared/types/oidc.go`**

```go
package types

import (
    "errors"
    "fmt"
    "net/url"
    "time"
)

// TenantOIDCConfig represents OIDC configuration for a tenant.
// Used by provisioning (storage) and gateway (validation).
type TenantOIDCConfig struct {
    TenantID  string    `json:"tenant_id"`
    IssuerURL string    `json:"issuer_url"`
    JWKSURL   string    `json:"jwks_url,omitempty"`
    Audience  string    `json:"audience,omitempty"`
    Enabled   bool      `json:"enabled"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

// Validate validates OIDC config. Defense in depth.
func (c *TenantOIDCConfig) Validate() error {
    // Issuer URL validation
    if c.IssuerURL == "" {
        return ErrIssuerURLRequired
    }
    issuerURL, err := url.Parse(c.IssuerURL)
    if err != nil {
        return fmt.Errorf("%w: %v", ErrInvalidIssuerURL, err)
    }
    if issuerURL.Scheme != "https" {
        return fmt.Errorf("%w: must use HTTPS", ErrInvalidIssuerURL)
    }
    if issuerURL.Host == "" {
        return fmt.Errorf("%w: must have a host", ErrInvalidIssuerURL)
    }

    // JWKS URL validation (if provided)
    if c.JWKSURL != "" {
        jwksURL, err := url.Parse(c.JWKSURL)
        if err != nil {
            return fmt.Errorf("%w: %v", ErrInvalidJWKSURL, err)
        }
        if jwksURL.Scheme != "https" {
            return fmt.Errorf("%w: must use HTTPS", ErrInvalidJWKSURL)
        }
    }

    // Audience validation
    if c.Audience != "" && len(c.Audience) > MaxAudienceLength {
        return fmt.Errorf("audience must be <= %d characters", MaxAudienceLength)
    }

    return nil
}

// JWKSURL returns the JWKS URL, defaulting to standard OIDC discovery path.
func (c *TenantOIDCConfig) GetJWKSURL() string {
    if c.JWKSURL != "" {
        return c.JWKSURL
    }
    // Default to standard OIDC discovery path
    return c.IssuerURL + "/.well-known/jwks.json"
}
```

**File: `internal/shared/types/channel_rules.go`**

```go
package types

import (
    "fmt"
    "regexp"
    "time"
)

// ChannelRules represents per-tenant channel access rules.
// Used by provisioning (storage) and gateway (authorization).
type ChannelRules struct {
    Public        []string            `json:"public"`
    GroupMappings map[string][]string `json:"group_mappings"`
    Default       []string            `json:"default,omitempty"`
}

// TenantChannelRules represents stored channel rules for a tenant.
type TenantChannelRules struct {
    TenantID  string       `json:"tenant_id"`
    Rules     ChannelRules `json:"rules"`
    CreatedAt time.Time    `json:"created_at"`
    UpdatedAt time.Time    `json:"updated_at"`
}

// channelPatternRegex validates channel patterns.
// Allows: alphanumeric, dots, hyphens, underscores, and wildcards (*)
var channelPatternRegex = regexp.MustCompile(`^[a-zA-Z0-9.*_-]{1,256}$`)

// Validate validates channel rules. Defense in depth.
func (r *ChannelRules) Validate() error {
    // Validate public patterns
    for _, pattern := range r.Public {
        if !channelPatternRegex.MatchString(pattern) {
            return fmt.Errorf("%w: %q", ErrInvalidChannelPattern, pattern)
        }
    }

    // Validate group mappings
    for group, patterns := range r.GroupMappings {
        if group == "" {
            return ErrEmptyGroupName
        }
        if len(group) > MaxGroupNameLength {
            return fmt.Errorf("group name too long: %q (max %d)", group, MaxGroupNameLength)
        }
        for _, pattern := range patterns {
            if !channelPatternRegex.MatchString(pattern) {
                return fmt.Errorf("%w for group %q: %q", ErrInvalidChannelPattern, group, pattern)
            }
        }
    }

    // Validate default patterns
    for _, pattern := range r.Default {
        if !channelPatternRegex.MatchString(pattern) {
            return fmt.Errorf("%w in default: %q", ErrInvalidChannelPattern, pattern)
        }
    }

    return nil
}

// ComputeAllowedPatterns returns all channel patterns a user can access.
func (r *ChannelRules) ComputeAllowedPatterns(groups []string) []string {
    // Pre-allocate with estimated capacity
    allowed := make([]string, 0, len(r.Public)+len(groups)*2)

    // Add public channels
    allowed = append(allowed, r.Public...)

    // Add channels for each group
    matched := false
    for _, group := range groups {
        if patterns, ok := r.GroupMappings[group]; ok {
            allowed = append(allowed, patterns...)
            matched = true
        }
    }

    // Add default if no groups matched
    if !matched && len(r.Default) > 0 {
        allowed = append(allowed, r.Default...)
    }

    return deduplicate(allowed)
}

// deduplicate removes duplicate strings from a slice.
func deduplicate(s []string) []string {
    if len(s) <= 1 {
        return s
    }
    seen := make(map[string]struct{}, len(s))
    result := make([]string, 0, len(s))
    for _, v := range s {
        if _, ok := seen[v]; !ok {
            seen[v] = struct{}{}
            result = append(result, v)
        }
    }
    return result
}

// IsValidChannelPattern checks if a pattern is valid.
// Exported for use in tests and other packages.
func IsValidChannelPattern(pattern string) bool {
    return channelPatternRegex.MatchString(pattern)
}
```

**File: `internal/shared/types/errors.go`**

```go
package types

import "errors"

// Sentinel errors for OIDC configuration.
var (
    ErrIssuerNotFound      = errors.New("issuer not found")
    ErrIssuerAlreadyExists = errors.New("issuer already registered to another tenant")
    ErrOIDCNotConfigured   = errors.New("OIDC not configured for tenant")
    ErrIssuerURLRequired   = errors.New("issuer URL is required")
    ErrInvalidIssuerURL    = errors.New("invalid issuer URL")
    ErrInvalidJWKSURL      = errors.New("invalid JWKS URL")
)

// Sentinel errors for channel rules.
var (
    ErrChannelRulesNotFound  = errors.New("channel rules not found")
    ErrInvalidChannelPattern = errors.New("invalid channel pattern")
    ErrEmptyGroupName        = errors.New("group name cannot be empty")
)
```

**File: `internal/shared/types/constants.go`**

```go
package types

// Validation constants for OIDC and channel rules.
// Configurable limits are in platform config; these are fixed protocol limits.
const (
    // MaxAudienceLength is the maximum length of an OIDC audience claim.
    MaxAudienceLength = 256

    // MaxGroupNameLength is the maximum length of a group name in channel rules.
    MaxGroupNameLength = 128

    // MaxChannelPatternLength is the maximum length of a channel pattern.
    MaxChannelPatternLength = 256
)
```

---

### 2. Interfaces - Defined at Point of Use (per Coding Guidelines)

**File: `internal/gateway/interfaces.go`** (gateway defines what it needs)

```go
package gateway

import (
    "context"

    "github.com/Toniq-Labs/odin-ws/internal/shared/types"
)

// TenantRegistry provides tenant lookup for gateway authentication.
// Defined here (consumer) per coding guidelines: "accept interfaces, return concrete types"
type TenantRegistry interface {
    // GetTenantByIssuer returns the tenant ID for an OIDC issuer.
    // Returns types.ErrIssuerNotFound if issuer is not registered.
    GetTenantByIssuer(ctx context.Context, issuerURL string) (string, error)

    // GetOIDCConfig returns the OIDC configuration for a tenant.
    // Returns types.ErrOIDCNotConfigured if not configured.
    GetOIDCConfig(ctx context.Context, tenantID string) (*types.TenantOIDCConfig, error)

    // GetChannelRules returns the channel rules for a tenant.
    // Returns types.ErrChannelRulesNotFound if not configured.
    GetChannelRules(ctx context.Context, tenantID string) (*types.ChannelRules, error)
}
```

### 3. Gateway Permission Checker with Fallback

**File: `internal/gateway/permissions_tenant.go`**

```go
package gateway

import (
    "context"
    "errors"

    "github.com/rs/zerolog"

    "github.com/Toniq-Labs/odin-ws/internal/shared/auth"
    "github.com/Toniq-Labs/odin-ws/internal/shared/types"
)

// TenantPermissionChecker validates channel permissions using per-tenant rules.
type TenantPermissionChecker struct {
    registry      TenantRegistry
    fallbackRules *types.ChannelRules
    logger        zerolog.Logger
}

// NewTenantPermissionChecker creates a permission checker with fallback rules.
func NewTenantPermissionChecker(registry TenantRegistry, fallback *types.ChannelRules, logger zerolog.Logger) *TenantPermissionChecker {
    return &TenantPermissionChecker{
        registry:      registry,
        fallbackRules: fallback,
        logger:        logger,
    }
}

// CanSubscribe checks if the claims allow subscription to the channel.
func (pc *TenantPermissionChecker) CanSubscribe(ctx context.Context, claims *auth.Claims, channel string) bool {
    rules, err := pc.registry.GetChannelRules(ctx, claims.TenantID)
    if err != nil {
        if errors.Is(err, types.ErrChannelRulesNotFound) {
            // Expected: tenant has no custom rules, use fallback
            channelRulesLookupTotal.WithLabelValues(claims.TenantID, "fallback").Inc()
            rules = pc.fallbackRules
        } else {
            // Unexpected error: log and use fallback (graceful degradation)
            pc.logger.Warn().
                Err(err).
                Str("tenant_id", claims.TenantID).
                Msg("Failed to load channel rules, using fallback")
            channelRulesLookupTotal.WithLabelValues(claims.TenantID, "fallback").Inc()
            rules = pc.fallbackRules
        }
    } else {
        channelRulesLookupTotal.WithLabelValues(claims.TenantID, "cache").Inc()
    }

    // Compute allowed patterns for this user's groups (method on shared type)
    allowedPatterns := rules.ComputeAllowedPatterns(claims.Groups)

    // Check if channel matches any allowed pattern
    allowed := auth.MatchAnyWildcard(allowedPatterns, channel)

    // Record metric
    result := "denied"
    if allowed {
        result = "allowed"
    }
    channelAuthorizationTotal.WithLabelValues(claims.TenantID, result).Inc()

    return allowed
}
```

---

## Implementation Order

### Phase 1: Shared Types & Database

1. Create shared types in `internal/shared/types/`:
   - `oidc.go` - TenantOIDCConfig with Validate()
   - `channel_rules.go` - ChannelRules with Validate() and ComputeAllowedPatterns()
   - `errors.go` - Sentinel errors
   - `constants.go` - Validation constants
2. Add unit tests for shared types (`internal/shared/types/*_test.go`)
3. Create database migration for new tables
4. Implement repository interfaces for OIDC config and channel rules
5. Add unit tests for repositories

### Phase 2: Provisioning API

1. Add OIDC config handlers with audit logging
2. Add channel rules handlers with audit logging
3. Add test access endpoint
4. Update router with new endpoints
5. Add integration tests

### Phase 3: Gateway Auth Changes

1. Implement TenantRegistry interface in gateway package
2. Implement CachedTenantRegistry with PostgreSQL backend
3. Add NoopTenantRegistry for graceful degradation
4. Implement MultiIssuerOIDC for dynamic JWKS management
5. Update MultiTenantValidator for multi-issuer support
6. Add metrics for all operations
7. Add unit tests

### Phase 4: Gateway Permission Changes

1. Implement TenantPermissionChecker
2. Add fallback rules support
3. Update gateway to use TenantPermissionChecker
4. Add integration tests

### Phase 5: Testing & Documentation

1. End-to-end tests with mock IdP
2. Update API documentation
3. Add IdP configuration guides (Auth0, Okta, Azure AD)
4. Update MULTI_TENANT_AUTH_OIDC.md with implementation details

---

## File Changes Summary

### Shared Package (used by gateway + provisioning)

| File | Action | Description |
|------|--------|-------------|
| `internal/shared/types/oidc.go` | Create | TenantOIDCConfig type with validation |
| `internal/shared/types/channel_rules.go` | Create | ChannelRules type with validation and ComputeAllowedPatterns |
| `internal/shared/types/errors.go` | Create | Shared sentinel errors |
| `internal/shared/types/constants.go` | Create | Validation constants (MaxAudienceLength, etc.) |

### Provisioning Service

| File | Action | Description |
|------|--------|-------------|
| `migrations/XXXXXX_add_oidc_channel_rules.sql` | Create | New database tables |
| `internal/provisioning/repository/oidc.go` | Create | OIDC config repository |
| `internal/provisioning/repository/channel_rules.go` | Create | Channel rules repository |
| `internal/provisioning/api/handlers_oidc.go` | Create | OIDC API handlers with audit logging |
| `internal/provisioning/api/handlers_channel_rules.go` | Create | Channel rules API handlers |
| `internal/provisioning/api/router.go` | Modify | Add new routes |
| `internal/provisioning/types.go` | Modify | Add audit action constants |

### Gateway Service

| File | Action | Description |
|------|--------|-------------|
| `internal/shared/platform/gateway_config.go` | Modify | Add new config fields |
| `internal/gateway/interfaces.go` | Create | TenantRegistry interface (at point of use) |
| `internal/gateway/tenant_registry_postgres.go` | Create | PostgresTenantRegistry implementation |
| `internal/gateway/tenant_registry_noop.go` | Create | NoopTenantRegistry for graceful degradation |
| `internal/gateway/oidc_multi.go` | Create | Multi-issuer OIDC support |
| `internal/gateway/permissions_tenant.go` | Create | Per-tenant permission checker |
| `internal/gateway/metrics.go` | Modify | Add new metrics |
| `internal/gateway/gateway.go` | Modify | Use new components |

---

## Testing Strategy

### Unit Tests

- Repository CRUD operations
- Validation functions (URLs, patterns)
- ChannelRules.ComputeAllowedPatterns()
- Pattern matching logic
- Cache hit/miss behavior
- Graceful degradation (noop fallback)

### Integration Tests

- Full OIDC flow with mock IdP
- Channel subscription with different group combinations
- Multi-tenant isolation
- Cache invalidation on config change
- Fallback behavior when registry unavailable

### End-to-End Tests

- Tenant onboarding flow
- OIDC configuration + channel rules setup
- User connection with OIDC token
- Channel authorization

---

## Rollback Plan

All changes are additive:
1. New tables don't affect existing functionality
2. Feature flags gate new functionality (disabled by default)
3. Gateway falls back to existing behavior if TenantRegistry unavailable
4. NoopTenantRegistry ensures service stays operational

```go
// Feature flags in GatewayConfig - both default to false
MultiIssuerOIDCEnabled       bool `env:"GATEWAY_MULTI_ISSUER_OIDC_ENABLED" envDefault:"false"`
PerTenantChannelRulesEnabled bool `env:"GATEWAY_PER_TENANT_CHANNEL_RULES" envDefault:"false"`
```

---

## Security Considerations

1. **Issuer uniqueness**: Each issuer URL can only belong to one tenant (enforced by DB constraint)
2. **HTTPS only**: Validate JWKS URLs and issuer URLs are HTTPS
3. **Rate limiting**: Rate limit OIDC config changes via existing API rate limiter
4. **Audit logging**: Log all OIDC config and channel rules changes
5. **Cache invalidation**: Ensure cache invalidation on config changes
6. **Input validation**: Validate all inputs at API boundary (defense in depth)
7. **No secrets in logs**: Never log JWKS content or token values

---

## Code Review Checklist (from Coding Guidelines)

Before merging, verify:

- [ ] No hardcoded values - all configurable via env vars
- [ ] All errors wrapped with context (`fmt.Errorf("...: %w", err)`)
- [ ] Sentinel errors defined for expected error conditions
- [ ] Input validation at ALL boundaries (defense in depth)
- [ ] Graceful degradation with noop implementations
- [ ] Metrics for key operations
- [ ] Audit logging for security events
- [ ] Structured logging with appropriate fields
- [ ] Panic recovery on all goroutines
- [ ] Feature flags fully gate optional capabilities
- [ ] Table-driven tests for new code
- [ ] No `TODO`/`HACK`/`FIXME` without linked ticket
