# Multi-Tenant Authentication & OIDC Integration

## Overview

Sukko supports two authentication flows for WebSocket connections:

| Flow | Token Issuer | Use Case |
|------|--------------|----------|
| **Tenant-Signed JWT** | Tenant's backend (using keys from provisioning) | Sukko internal, full control |
| **OIDC** | Tenant's Identity Provider (Auth0, Okta, Azure AD) | Third-party tenants |

## Feature Flags

Both multi-issuer OIDC and per-tenant channel rules are gated behind feature flags for safe rollout:

| Flag | Environment Variable | Default | Description |
|------|---------------------|---------|-------------|
| Multi-Issuer OIDC | `GATEWAY_MULTI_ISSUER_OIDC_ENABLED` | `false` | Enables per-tenant OIDC issuer registration |
| Per-Tenant Channel Rules | `GATEWAY_PER_TENANT_CHANNEL_RULES` | `false` | Enables per-tenant channel authorization rules |

When disabled, the gateway uses:
- Single-issuer OIDC (configured via `OIDC_ISSUER_URL`, `OIDC_JWKS_URL`)
- Global channel patterns (configured via `GATEWAY_PUBLIC_PATTERNS`, etc.)

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│  Tenant's Infrastructure                                                 │
│                                                                          │
│  ┌────────────────┐         ┌────────────────┐                          │
│  │  Active        │  sync   │  Identity      │                          │
│  │  Directory     │────────►│  Provider      │                          │
│  │  (Groups)      │         │  (Auth0/Okta)  │                          │
│  └────────────────┘         └────────────────┘                          │
│                                     │                                    │
│                                     │ Issues JWT with groups             │
│                                     ▼                                    │
└─────────────────────────────────────│────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  Sukko Platform                                                           │
│                                                                          │
│  ┌────────────────┐         ┌────────────────┐         ┌──────────────┐ │
│  │  Provisioning  │         │  Gateway       │         │  WS Server   │ │
│  │  API           │────────►│                │────────►│              │ │
│  │                │ channel │  - Validates   │  proxy  │  - Broadcast │ │
│  │  - Tenants     │  rules  │    JWT         │         │  - Channels  │ │
│  │  - OIDC config │         │  - Maps groups │         │              │ │
│  │  - Channel     │         │    to channels │         │              │ │
│  │    rules       │         │  - Authorizes  │         │              │ │
│  └────────────────┘         └────────────────┘         └──────────────┘ │
│          │                                                               │
│          ▼                                                               │
│  ┌────────────────┐                                                      │
│  │  PostgreSQL    │                                                      │
│  │  - tenant_oidc │                                                      │
│  │  - channel_rules│                                                     │
│  └────────────────┘                                                      │
└─────────────────────────────────────────────────────────────────────────┘
```

## Authentication Flows

### Flow 1: Sukko Internal (Tenant-Signed JWT)

For Sukko's own use, where `sukko-api` issues tokens with full control over claims.

```
User                    sukko-api                    Gateway
─────────────────────────────────────────────────────────────
  │                         │                          │
  │──── Authenticate ──────►│                          │
  │                         │                          │
  │                         │ Generate JWT:            │
  │                         │ {                        │
  │                         │   "sub": "user123",      │
  │                         │   "tenant_id": "sukko",   │
  │                         │   "channels": ["BTC.*"], │ ◄── Dynamic claims
  │                         │   "tier": "premium"      │
  │                         │ }                        │
  │                         │                          │
  │◄─── JWT (signed with ───│                          │
  │     tenant key)         │                          │
  │                         │                          │
  │──────────────── Connect with JWT ─────────────────►│
  │                                                    │
  │                                    Validate signature
  │                                    (using tenant's public key)
  │                                    Read claims directly
  │                                    Authorize channels from claims
```

**Characteristics:**
- Full control over claims (channels, tiers, permissions)
- No external IdP dependency
- Claims set dynamically per user by sukko-api

### Flow 2: Third-Party Tenants (OIDC)

For external tenants using their own Identity Provider.

```
User                    Tenant's IdP              Gateway              Provisioning DB
───────────────────────────────────────────────────────────────────────────────────────
  │                         │                        │                       │
  │──── Login ─────────────►│                        │                       │
  │                         │                        │                       │
  │                         │ Generate JWT:          │                       │
  │                         │ {                      │                       │
  │                         │   "iss": "https://     │                       │
  │                         │         acme.auth0.com"│                       │
  │                         │   "sub": "alice",      │                       │
  │                         │   "groups": ["traders"]│ ◄── From AD/LDAP sync │
  │                         │ }                      │                       │
  │                         │                        │                       │
  │◄─── JWT (signed by IdP)─│                        │                       │
  │                         │                        │                       │
  │──────────────── Connect with JWT ───────────────►│                       │
  │                                                  │                       │
  │                                   Validate signature (JWKS)              │
  │                                                  │                       │
  │                                   Lookup: issuer → tenant_id ───────────►│
  │                                                  │◄──── "acme-corp" ─────│
  │                                                  │                       │
  │                                   Lookup: tenant's channel rules ───────►│
  │                                                  │◄── group_mappings ────│
  │                                                  │                       │
  │                                   Map: "traders" → ["*.trade"]           │
  │                                   Authorize channel subscriptions        │
```

**Characteristics:**
- Tenant uses existing IdP (no new system to manage)
- Groups come from tenant's directory (AD, LDAP, HR system)
- Channel access configured via provisioning API

## Channel Rules

### Concept

Channel rules map IdP groups to allowed channel patterns:

```
IdP Group              Channel Pattern           Example Channels
─────────────────────────────────────────────────────────────────
"traders"         →    ["*.trade"]          →    BTC.trade, ETH.trade
"crypto-team"     →    ["BTC.*", "ETH.*"]   →    BTC.trade, BTC.liquidity
"premium"         →    ["*.realtime"]       →    BTC.realtime, ETH.realtime
(public)          →    ["*.metadata"]       →    BTC.metadata (no group needed)
```

### How Authorization Works

```
User JWT:
{
  "sub": "alice@acme.com",
  "groups": ["traders", "premium"]
}

Tenant's Channel Rules:
{
  "public": ["*.metadata"],
  "group_mappings": {
    "traders": ["*.trade", "*.liquidity"],
    "premium": ["*.realtime"]
  }
}

Gateway computes allowed channels:
  From "traders" → ["*.trade", "*.liquidity"]
  From "premium" → ["*.realtime"]
  From "public"  → ["*.metadata"]

  Combined: ["*.trade", "*.liquidity", "*.realtime", "*.metadata"]

Subscription requests:
  "BTC.trade"     → matches "*.trade"     → ALLOWED
  "ETH.realtime"  → matches "*.realtime"  → ALLOWED
  "SOL.orderbook" → no match              → DENIED
```

### API for Channel Rules

```bash
# Set channel rules for a tenant (requires admin role)
PUT /api/v1/tenants/{tenant_id}/channel-rules
Content-Type: application/json
Authorization: Bearer <admin-token>

{
  "public": ["*.metadata", "*.analytics"],
  "group_mappings": {
    "traders": ["*.trade", "*.liquidity"],
    "market-makers": ["*.orderbook", "*.depth"],
    "premium": ["*.realtime.*"],
    "admins": ["*"]
  },
  "default": ["*.basic"]
}

# Get current rules
GET /api/v1/tenants/{tenant_id}/channel-rules
Authorization: Bearer <token>

# Delete channel rules (requires admin role)
DELETE /api/v1/tenants/{tenant_id}/channel-rules
Authorization: Bearer <admin-token>

# Test what groups would allow (for debugging)
POST /api/v1/tenants/{tenant_id}/test-access
Content-Type: application/json
Authorization: Bearer <token>

{
  "groups": ["traders", "premium"]
}

Response:
{
  "allowed_patterns": ["*.trade", "*.liquidity", "*.realtime.*", "*.metadata", "*.analytics"]
}
```

## Where Groups Come From

**Groups are NOT managed by Sukko.** They come from the tenant's existing identity infrastructure.

### Enterprise Tenants

```
Source of Truth              Syncs to IdP              Appears in JWT
──────────────────────────────────────────────────────────────────────
Active Directory     ──────►  Auth0/Okta    ──────►   groups: [...]
  └─ "Trading Team"
  └─ "Crypto Desk"
  └─ "Premium Users"

HR System (Workday)  ──────►  SCIM Sync     ──────►   groups: [...]
  └─ Department
  └─ Job Title
```

### How Groups Get Into Tokens Automatically

Tenants configure their IdP **once** to include groups in tokens:

#### Auth0

```javascript
// Auth0 Action (one-time setup)
exports.onExecutePostLogin = async (event, api) => {
  api.idToken.setCustomClaim('groups', event.user.groups);
};
```

Or via Dashboard:
```
Auth0 Dashboard → Applications → [App] → Settings → Enable "groups" claim
```

#### Okta

```
Okta Admin → Applications → [App] → Sign On → OpenID Connect ID Token

Add claim:
  Name: groups
  Value: getFilteredGroups(app_group_whitelist, 50, "STARTS_WITH")
  Include in: ID Token ✓
```

#### Azure AD

```
Azure Portal → App Registration → Token Configuration → Add groups claim

Select: Security groups ✓
```

### Automatic User Access Flow

```
Day 1: IT Admin Setup (one-time, ~10 minutes)
────────────────────────────────────────────────────────────────────────
1. Add Sukko as application in IdP                              (5 min)
2. Enable "groups" claim for Sukko app                          (1 min)
3. Configure group→channel mapping in Sukko Provisioning API    (5 min)

Day 2+: User Access (fully automatic)
────────────────────────────────────────────────────────────────────────
Alice is added to "Trading Team" in Active Directory
  ↓ (automatic AD→IdP sync)
Alice logs into Sukko via company SSO
  ↓ (automatic)
IdP issues JWT with groups: ["Trading Team"]
  ↓ (automatic)
Gateway maps "Trading Team" → ["*.trade", "*.liquidity"]
  ↓ (automatic)
Alice can subscribe to trade and liquidity channels

NO PER-USER CONFIGURATION IN SUKKO.
```

## Tenant Onboarding

### Step 1: Register Tenant

```bash
POST /api/v1/tenants
{
  "name": "Acme Corporation",
  "contact_email": "admin@acme.com"
}

Response:
{
  "id": "acme-corp",
  "name": "Acme Corporation",
  "status": "active"
}
```

### Step 2: Register OIDC Configuration

```bash
# Create OIDC config (requires admin role)
POST /api/v1/tenants/acme-corp/oidc
Content-Type: application/json
Authorization: Bearer <admin-token>

{
  "issuer_url": "https://acme.auth0.com/",
  "jwks_url": "",                          # Optional, defaults to {issuer}/.well-known/jwks.json
  "audience": "sukko",                   # Optional, validates "aud" claim
  "enabled": true
}

# Get OIDC config
GET /api/v1/tenants/acme-corp/oidc

# Update OIDC config (requires admin role)
PUT /api/v1/tenants/acme-corp/oidc
Content-Type: application/json
Authorization: Bearer <admin-token>

{
  "issuer_url": "https://acme.auth0.com/",
  "audience": "sukko-v2",
  "enabled": true
}

# Delete OIDC config (requires admin role)
DELETE /api/v1/tenants/acme-corp/oidc
Authorization: Bearer <admin-token>
```

### Step 3: Configure Channel Rules

```bash
# Set channel rules (requires admin role)
PUT /api/v1/tenants/acme-corp/channel-rules
Content-Type: application/json
Authorization: Bearer <admin-token>

{
  "public": ["*.metadata"],
  "group_mappings": {
    "Trading Team": ["*.trade", "*.liquidity"],
    "Premium Subscription": ["*.realtime"]
  },
  "default": ["*.basic"]                   # Optional, used when no groups match
}

# Get current rules
GET /api/v1/tenants/acme-corp/channel-rules

# Delete channel rules (requires admin role)
DELETE /api/v1/tenants/acme-corp/channel-rules
Authorization: Bearer <admin-token>
```

### Step 4: Tenant Configures Their IdP

Tenant's IT admin (one-time):
1. Add Sukko as an application in their IdP
2. Configure to include `groups` claim in tokens
3. Set audience to match registered audience

### Step 5: Users Connect

Users authenticate via their company SSO and connect to Sukko. No further configuration needed.

## Database Schema

**Migration file:** `internal/provisioning/repository/migrations/002_oidc_channel_rules.sql`

```sql
-- OIDC configuration per tenant
CREATE TABLE tenant_oidc_config (
    tenant_id       TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    issuer_url      TEXT NOT NULL,
    jwks_url        TEXT,                       -- Optional override, defaults to {issuer}/.well-known/jwks.json
    audience        TEXT,                       -- Expected audience claim
    enabled         BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Each issuer can only belong to one tenant
    CONSTRAINT tenant_oidc_issuer_unique UNIQUE (issuer_url),
    -- Issuer URL must be HTTPS
    CONSTRAINT issuer_url_https CHECK (issuer_url LIKE 'https://%'),
    -- Issuer URL max length
    CONSTRAINT issuer_url_max_length CHECK (char_length(issuer_url) <= 512),
    -- JWKS URL must be HTTPS if provided
    CONSTRAINT jwks_url_https CHECK (jwks_url IS NULL OR jwks_url LIKE 'https://%'),
    -- Audience max length
    CONSTRAINT audience_max_length CHECK (audience IS NULL OR char_length(audience) <= 256)
);

-- Index for issuer lookup (gateway hot path)
CREATE UNIQUE INDEX idx_tenant_oidc_issuer_enabled ON tenant_oidc_config(issuer_url)
    WHERE enabled = true;

-- Channel access rules per tenant
CREATE TABLE tenant_channel_rules (
    tenant_id       TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    rules           JSONB NOT NULL DEFAULT '{"public": [], "group_mappings": {}, "default": []}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Example rules JSONB:
-- {
--   "public": ["*.metadata", "*.analytics"],
--   "group_mappings": {
--     "traders": ["*.trade", "*.liquidity"],
--     "premium": ["*.realtime.*"]
--   },
--   "default": ["*.basic"]  -- Fallback when no groups match
-- }
```

## Configuration

### Gateway Environment Variables

```bash
# Feature flags
GATEWAY_MULTI_ISSUER_OIDC_ENABLED=true      # Enable per-tenant OIDC
GATEWAY_PER_TENANT_CHANNEL_RULES=true       # Enable per-tenant channel rules

# Cache TTL settings
GATEWAY_ISSUER_CACHE_TTL=5m                 # How long to cache issuer→tenant mappings
GATEWAY_CHANNEL_RULES_CACHE_TTL=1m          # How long to cache channel rules
GATEWAY_OIDC_KEYFUNC_CACHE_TTL=1h           # How long to cache JWKS keyfuncs

# JWKS fetch settings
GATEWAY_JWKS_FETCH_TIMEOUT=10s              # Timeout for fetching JWKS endpoints
GATEWAY_JWKS_REFRESH_INTERVAL=1h            # Background JWKS refresh interval

# Fallback channel rules (when tenant has none configured)
GATEWAY_FALLBACK_PUBLIC_CHANNELS=*.metadata # Comma-separated patterns
```

### Validation Constants

Defined in `internal/shared/types/constants.go`:

| Constant | Value | Description |
|----------|-------|-------------|
| `MaxAudienceLength` | 256 | Maximum OIDC audience length |
| `MaxGroupNameLength` | 128 | Maximum group name length |
| `MaxChannelPatternLength` | 256 | Maximum channel pattern length |
| `MaxIssuerURLLength` | 512 | Maximum issuer URL length |
| `MaxGroupsPerMapping` | 100 | Maximum groups in channel rules |
| `MaxPatternsPerGroup` | 50 | Maximum patterns per group |
| `MaxPublicPatterns` | 50 | Maximum public channel patterns |

## Gateway Implementation

### Key Components

| Component | File | Purpose |
|-----------|------|---------|
| `TenantRegistry` | `internal/gateway/interfaces.go` | Interface for tenant lookup |
| `PostgresTenantRegistry` | `internal/gateway/tenant_registry_postgres.go` | PostgreSQL-backed implementation with caching |
| `NoopTenantRegistry` | `internal/gateway/tenant_registry_noop.go` | Graceful degradation when DB unavailable |
| `MultiIssuerOIDC` | `internal/gateway/multi_issuer_oidc.go` | Dynamic JWKS management per issuer |
| `TenantPermissionChecker` | `internal/gateway/permissions_tenant.go` | Per-tenant channel authorization |

### TenantRegistry Interface

```go
// Defined in internal/gateway/interfaces.go
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

    // Close releases resources held by the registry.
    Close() error
}
```

### Channel Authorization

```go
// Defined in internal/shared/types/channel_rules.go
func (r *ChannelRules) ComputeAllowedPatterns(groups []string) []string {
    allowed := make([]string, 0, len(r.Public)+len(groups)*2)

    // Add public channels (always allowed)
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
```

### Permission Checker

```go
// Defined in internal/gateway/permissions_tenant.go
func (pc *TenantPermissionChecker) CanSubscribe(ctx context.Context, claims *auth.Claims, channel string) bool {
    rules := pc.getRulesForTenant(ctx, claims.TenantID)

    // Compute allowed patterns for this user's groups
    allowedPatterns := rules.ComputeAllowedPatterns(claims.Groups)

    // Check if channel matches any allowed pattern
    allowed := auth.MatchAnyWildcard(allowedPatterns, channel)

    // Record metric
    result := "denied"
    if allowed {
        result = "allowed"
    }
    RecordChannelAuthorization(claims.TenantID, result)

    return allowed
}
```

## Security Considerations

### Token Validation

| Check | Purpose |
|-------|---------|
| Signature verification | Ensure token wasn't tampered with |
| Issuer validation | Only accept tokens from registered IdPs |
| Audience validation | Ensure token was issued for Sukko |
| Expiration check | Reject expired tokens |

### Channel Access

| Control | Purpose |
|---------|---------|
| Tenant isolation | Users can only access their tenant's channels |
| Group-based access | Fine-grained channel permissions |
| Pattern matching | Flexible channel authorization |

### Caching

| Data | Default TTL | Environment Variable | Invalidation |
|------|-------------|---------------------|--------------|
| OIDC JWKS (keyfunc) | 1 hour | `GATEWAY_OIDC_KEYFUNC_CACHE_TTL` | Background refresh via keyfunc library |
| Issuer → Tenant mapping | 5 minutes | `GATEWAY_ISSUER_CACHE_TTL` | TTL expiry, `InvalidateIssuerCache()` |
| OIDC config | 5 minutes | `GATEWAY_ISSUER_CACHE_TTL` | TTL expiry, `InvalidateTenantCache()` |
| Channel rules | 1 minute | `GATEWAY_CHANNEL_RULES_CACHE_TTL` | TTL expiry, `InvalidateTenantCache()` |

## Graceful Degradation

The implementation follows the coding guidelines for graceful degradation. Optional dependencies have defined degraded behavior:

| Component | On Failure | Degraded Behavior |
|-----------|------------|-------------------|
| `PostgresTenantRegistry` | DB connection fails | Falls back to `NoopTenantRegistry` |
| `NoopTenantRegistry` | N/A (always works) | Returns `ErrIssuerNotFound`, `ErrChannelRulesNotFound` |
| Multi-issuer OIDC | Setup fails | Gateway continues with single-issuer or tenant keys only |
| OIDC JWKS fetch | Timeout/error | That issuer's tokens rejected, metric recorded |
| Channel rules lookup | Not found | Falls back to `FallbackPublicChannels` patterns |

### NoopTenantRegistry

When the database is unavailable, `NoopTenantRegistry` ensures the gateway stays operational:

```go
// From internal/gateway/tenant_registry_noop.go
func (n *NoopTenantRegistry) GetTenantByIssuer(_ context.Context, issuerURL string) (string, error) {
    return "", types.ErrIssuerNotFound // Forces fallback to tenant-signed JWT validation
}

func (n *NoopTenantRegistry) GetChannelRules(_ context.Context, tenantID string) (*types.ChannelRules, error) {
    return nil, types.ErrChannelRulesNotFound // Forces fallback to default rules
}
```

## Metrics

All metrics are defined in `internal/gateway/metrics.go`:

### OIDC Validation

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `gateway_oidc_validation_total` | Counter | `tenant_id`, `result` | Total OIDC token validations |
| `gateway_oidc_validation_latency_seconds` | Histogram | `tenant_id` | OIDC validation latency |

### Issuer Cache

| Metric | Type | Description |
|--------|------|-------------|
| `gateway_issuer_cache_hits_total` | Counter | Cache hits for issuer→tenant lookup |
| `gateway_issuer_cache_misses_total` | Counter | Cache misses (database query required) |

### Channel Rules

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `gateway_channel_rules_lookup_total` | Counter | `tenant_id`, `source` | Rules lookups (source: cache, database, fallback) |
| `gateway_channel_authorization_total` | Counter | `tenant_id`, `result` | Authorization decisions (result: allowed, denied) |

### JWKS Fetch

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `gateway_jwks_fetch_total` | Counter | `issuer`, `result` | JWKS endpoint fetches |
| `gateway_jwks_fetch_latency_seconds` | Histogram | `issuer` | JWKS fetch latency |

## Comparison: Sukko vs Third-Party

| Aspect | Sukko (Internal) | Third-Party (OIDC) |
|--------|-----------------|-------------------|
| Token issuer | sukko-api | Tenant's IdP |
| Signing keys | From provisioning | IdP's keys (JWKS) |
| Claims control | Full (dynamic) | IdP provides groups |
| Channel authorization | Claims in token | Group→Channel mapping |
| User management | sukko-api | Tenant's IdP |
| Setup effort | None | One-time IdP config |

## Troubleshooting

### Token Not Accepted

```bash
# Check if issuer is registered
GET /api/v1/tenants?oidc_issuer=https://acme.auth0.com

# Verify JWKS is accessible
curl https://acme.auth0.com/.well-known/jwks.json
```

### User Can't Access Expected Channels

```bash
# Check tenant's channel rules
GET /api/v1/tenants/acme-corp/channel-rules

# Test access with specific groups
POST /api/v1/tenants/acme-corp/test-access
{
  "groups": ["traders"]
}

# Verify groups claim is in token (decode JWT)
```

### Groups Not Appearing in Token

1. Check IdP configuration includes groups claim
2. Verify user is assigned to groups in IdP
3. Check IdP's group claim name matches expected format

## Implementation Files

### Shared Types (`internal/shared/types/`)

| File | Contents |
|------|----------|
| `oidc.go` | `TenantOIDCConfig` struct with `Validate()`, `GetJWKSURL()` |
| `channel_rules.go` | `ChannelRules` struct with `Validate()`, `ComputeAllowedPatterns()` |
| `errors.go` | Sentinel errors (`ErrIssuerNotFound`, `ErrChannelRulesNotFound`, etc.) |
| `constants.go` | Validation constants (`MaxAudienceLength`, `MaxGroupNameLength`, etc.) |

### Gateway (`internal/gateway/`)

| File | Contents |
|------|----------|
| `interfaces.go` | `TenantRegistry`, `MultiIssuerValidator` interfaces |
| `tenant_registry_postgres.go` | `PostgresTenantRegistry` with TTL-based caching |
| `tenant_registry_noop.go` | `NoopTenantRegistry` for graceful degradation |
| `multi_issuer_oidc.go` | `MultiIssuerOIDC` for dynamic JWKS management |
| `permissions_tenant.go` | `TenantPermissionChecker` for per-tenant authorization |
| `metrics.go` | Prometheus metrics for OIDC/channel rules |
| `gateway.go` | Integration (`setupMultiIssuerOIDC`, `Close`) |

### Provisioning (`internal/provisioning/`)

| File | Contents |
|------|----------|
| `api/handlers_oidc.go` | OIDC config API handlers |
| `api/handlers_channel_rules.go` | Channel rules API handlers |
| `api/router.go` | Routes for `/oidc` and `/channel-rules` |
| `repository/oidc.go` | `PostgresOIDCConfigRepository` |
| `repository/channel_rules.go` | `PostgresChannelRulesRepository` |
| `repository/migrations/002_oidc_channel_rules.sql` | Database schema |

### Configuration (`internal/shared/platform/`)

| File | Contents |
|------|----------|
| `gateway_config.go` | Feature flags, TTL settings, validation |

## References

- [Auth0: Add Groups to ID Token](https://auth0.com/docs/manage-users/access-control)
- [Okta: Customize Tokens](https://developer.okta.com/docs/guides/customize-tokens-returned-from-okta/)
- [Azure AD: Configure Group Claims](https://docs.microsoft.com/en-us/azure/active-directory/hybrid/how-to-connect-fed-group-claims)
- [Implementation Plan](./PLAN_AUTH_OIDC_CHANNEL_RULES*.md) - Original implementation plan
- [Coding Guidelines](./CODING_GUIDELINES.md) - Project coding standards
