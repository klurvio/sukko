# Multi-Tenant Authentication System

**Date:** 2026-01-27 (Updated)

---

## Overview

A generic, secure, multi-tenant authentication and authorization system for WebSocket servers. Uses per-tenant asymmetric keys (ES256/RS256/EdDSA) for JWT validation.

---

## Goals

1. **Generic** - Not product-specific, reusable for any multi-tenant SaaS
2. **Secure** - Tenant isolation enforced, fail-secure defaults, audit logging
3. **Flexible** - Config-driven rules, custom placeholders, extensible claims
4. **Testable** - Unit testable policies, integration test patterns
5. **Observable** - Prometheus metrics, structured logging, audit trail

---

## Authentication Model: Per-Tenant Asymmetric Keys

Each tenant has their own key pair:
- **Private key**: Tenant keeps secret, signs tokens
- **Public key**: Gateway stores, verifies tokens

```
┌─────────────────┐                      ┌─────────────────┐
│  Tenant (Odin)  │                      │     Gateway     │
│                 │                      │                 │
│  Has: private   │                      │  Has: tenant's  │
│       key       │                      │       public key│
│                 │                      │                 │
│  1. User logs   │                      │                 │
│     into Odin   │                      │                 │
│                 │                      │                 │
│  2. Odin signs  │      JWT Token       │  3. Gateway     │
│     JWT with    │ ──────────────────►  │     validates   │
│     private key │                      │     with public │
│                 │                      │     key         │
│                 │                      │                 │
│                 │                      │  4. Extracts    │
│                 │                      │     tenant_id   │
│                 │                      │                 │
│                 │                      │  5. Enforces    │
│                 │                      │     isolation   │
└─────────────────┘                      └─────────────────┘
```

### JWT Header with Key ID

```json
{
  "alg": "ES256",
  "typ": "JWT",
  "kid": "odin-prod-2026"
}
```

### Supported Algorithms

| Algorithm | Type | Key Size | Recommended |
|-----------|------|----------|-------------|
| ES256 | ECDSA P-256 | 256-bit | Yes (smaller, faster) |
| RS256 | RSA | 2048-bit | Yes |
| EdDSA | Ed25519 | 256-bit | Yes (fastest) |

---

## Key Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| **Authentication** | Per-tenant asymmetric keys | Tenant signs, gateway verifies. Gateway cannot forge tokens. |
| **Key Storage** | PostgreSQL with cache | Durable storage, in-memory cache for performance |
| **Infrastructure** | Multi-tenant shared | Cost-effective, standard for SaaS |
| **Topic Naming** | `{env}.{tenant}.{resource}` | Environment + tenant prevents cross-contamination |
| **Channel Naming** | Tenant implicit | Client sees `BTC.trade`, server maps to `{tenant}.BTC.trade` |
| **Isolation Mode** | Always fail-secure | Topics/channels without tenant are rejected |

---

## Implementation Status

| Component | Status | Location |
|-----------|--------|----------|
| MultiTenantValidator | **Implemented** | `internal/auth/jwt_multitenant.go` |
| PostgresKeyRegistry | **Implemented** | `internal/auth/keys_postgres.go` |
| StaticKeyRegistry | **Implemented** | `internal/auth/keys.go` (for testing) |
| Claims (enhanced) | **Implemented** | `internal/auth/jwt.go` |
| PermissionChecker | **Implemented** | `internal/gateway/permissions.go` |
| Gateway auth flow | **Implemented** | `internal/gateway/gateway.go` |
| Subscribe filtering | **Implemented** | `internal/gateway/proxy.go` |
| Gateway config | **Implemented** | `internal/platform/gateway_config.go` |
| Provisioning Service | **Implemented** | `internal/provisioning/` |
| Database migrations | **Implemented** | `internal/provisioning/repository/migrations/` |

---

## Configuration

All configuration is via environment variables, managed through Helm charts.

### Gateway Configuration

**Helm Values** (`deployments/k8s/helm/odin/charts/ws-gateway/values.yaml`):

```yaml
config:
  # Authentication
  authEnabled: true
  requireTenantId: true

  # Key cache settings
  keyCacheRefreshInterval: "1m"
  keyCacheQueryTimeout: "5s"

  # Database connection pool
  dbMaxOpenConns: 10
  dbMaxIdleConns: 5
  dbConnMaxLifetime: "5m"
  dbConnMaxIdleTime: "1m"
  dbPingTimeout: "5s"
```

**Environment Variables** (set by Helm deployment.yaml):

| Variable | Description | Default |
|----------|-------------|---------|
| `AUTH_ENABLED` | Enable JWT validation | `false` |
| `REQUIRE_TENANT_ID` | Require tenant_id claim | `true` |
| `PROVISIONING_DATABASE_URL` | PostgreSQL connection string | (from secret) |
| `KEY_CACHE_REFRESH_INTERVAL` | Background cache refresh | `1m` |
| `KEY_CACHE_QUERY_TIMEOUT` | DB query timeout | `5s` |
| `DB_MAX_OPEN_CONNS` | Max open connections | `10` |
| `DB_MAX_IDLE_CONNS` | Max idle connections | `5` |
| `DB_CONN_MAX_LIFETIME` | Connection max lifetime | `5m` |
| `DB_CONN_MAX_IDLE_TIME` | Idle connection max time | `1m` |
| `DB_PING_TIMEOUT` | Connection ping timeout | `5s` |

### Enabling Authentication

```yaml
# values/production.yaml
ws-gateway:
  config:
    authEnabled: true
```

Create the database secret:
```bash
kubectl create secret generic odin-provisioning-db \
  --from-literal=database-url="postgres://user:pass@host:5432/provisioning?sslmode=require"
```

---

## Database Schema

The provisioning database stores tenant keys for JWT validation.

### Schema Location

`internal/provisioning/repository/migrations/001_initial.sql`

### Key Tables

```sql
-- Tenant public keys for JWT validation
CREATE TABLE tenant_keys (
    key_id          TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    algorithm       TEXT NOT NULL CHECK (algorithm IN ('ES256', 'RS256', 'EdDSA')),
    public_key      TEXT NOT NULL,
    is_active       BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ,
    revoked_at      TIMESTAMPTZ
);

-- Indexes for key lookup performance
CREATE INDEX idx_tenant_keys_lookup ON tenant_keys(key_id, is_active)
    WHERE is_active = true;
```

---

## Database Migrations

Migrations are managed with [Atlas](https://atlasgo.io/), a Go-native schema migration tool.

### Installation

```bash
# Install Atlas CLI
curl -sSf https://atlasgo.sh | sh
```

### Apply Migrations

```bash
# Apply all pending migrations
atlas migrate apply \
  --url "$DATABASE_URL" \
  --dir "file://ws/internal/provisioning/repository/migrations"
```

### Validate Schema

```bash
# Check for schema drift
atlas schema diff \
  --from "$DATABASE_URL" \
  --to "file://ws/internal/provisioning/repository/migrations"
```

### Taskfile Integration

```yaml
# Taskfile.yml
provisioning:migrate:
  desc: Apply database migrations
  dir: "{{.ROOT_DIR}}"
  cmds:
    - atlas migrate apply --url "$DATABASE_URL" --dir "file://ws/internal/provisioning/repository/migrations"

provisioning:validate:
  desc: Check for schema drift
  dir: "{{.ROOT_DIR}}"
  cmds:
    - atlas schema diff --from "$DATABASE_URL" --to "file://ws/internal/provisioning/repository/migrations"

provisioning:new-migration:
  desc: Create a new migration file
  dir: "{{.ROOT_DIR}}"
  cmds:
    - atlas migrate new {{.CLI_ARGS}} --dir "file://ws/internal/provisioning/repository/migrations"
```

### Kubernetes Init Container

For automatic migration on deployment:

```yaml
# In ws-gateway deployment.yaml or as a separate Job
initContainers:
  - name: migrate
    image: arigaio/atlas:latest
    command:
      - atlas
      - migrate
      - apply
      - --url
      - $(DATABASE_URL)
      - --dir
      - file:///migrations
    env:
      - name: DATABASE_URL
        valueFrom:
          secretKeyRef:
            name: {{ .Release.Name }}-provisioning-db
            key: database-url
    volumeMounts:
      - name: migrations
        mountPath: /migrations
volumes:
  - name: migrations
    configMap:
      name: {{ .Release.Name }}-migrations
```

---

## Observability

Authentication events are tracked via Prometheus metrics, structured logging, and audit logging.

### Prometheus Metrics

Metrics use **service-specific prefixes** to distinguish between components:
- `gateway_` - WebSocket gateway metrics (`internal/gateway/metrics.go`)
- `ws_` - Core WebSocket server metrics (`internal/monitoring/metrics.go`)
- `provisioning_` - Provisioning service metrics (`internal/provisioning/api/metrics.go`)

#### Gateway Auth Metrics

```go
// internal/gateway/metrics.go

// Authentication attempts by status (success, failed, skipped)
gateway_auth_validations_total{status="success|failed|skipped"}

// JWT validation latency
gateway_auth_latency_seconds

// Channel permission checks by result
gateway_channel_checks_total{result="allowed|denied"}

// Access denials with detailed reason
gateway_access_denials_total{resource_type="channel|topic", reason="unauthorized|tenant_mismatch|..."}

// Key cache performance
gateway_key_cache_hits_total
gateway_key_cache_misses_total
gateway_key_cache_size
gateway_key_cache_refreshes_total{result="success|error"}
```

### Structured Logging

Using zerolog with tenant context:

```go
// Successful authentication
logger.Info().
    Str("tenant_id", claims.TenantID).
    Str("user_id", claims.Subject).
    Str("key_id", keyID).
    Str("algorithm", claims.Algorithm).
    Dur("latency", duration).
    Msg("Token validated successfully")

// Authentication failure
logger.Warn().
    Str("remote_addr", remoteAddr).
    Str("key_id", keyID).
    Str("reason", "key_not_found").
    Msg("Authentication failed")

// Access denied
logger.Warn().
    Str("tenant_id", claims.TenantID).
    Str("user_id", claims.Subject).
    Str("channel", channel).
    Str("reason", "tenant_mismatch").
    Msg("Channel access denied")
```

### Audit Logging

Security-relevant events use the audit logger for compliance:

```go
// internal/monitoring/audit_logger.go pattern

// Successful authentication
auditLogger.Info("AuthSuccess",
    "User authenticated successfully",
    map[string]any{
        "tenant_id": claims.TenantID,
        "user_id":   claims.Subject,
        "key_id":    keyID,
        "algorithm": claims.Algorithm,
        "remote_addr": remoteAddr,
    })

// Authentication failure
auditLogger.Warning("AuthFailure",
    "Authentication attempt failed",
    map[string]any{
        "remote_addr": remoteAddr,
        "reason":      "invalid_signature",
        "key_id":      keyID,
    })

// Access denied
auditLogger.Warning("AccessDenied",
    "Tenant access denied",
    map[string]any{
        "tenant_id":       claims.TenantID,
        "user_id":         claims.Subject,
        "resource":        channel,
        "resource_tenant": resourceTenant,
        "reason":          "tenant_mismatch",
    })

// Cross-tenant access (admin)
auditLogger.Warning("CrossTenantAccess",
    "Admin accessed other tenant's resource",
    map[string]any{
        "admin_tenant":    claims.TenantID,
        "resource_tenant": resourceTenant,
        "resource":        resource,
        "granting_role":   role,
    })
```

### Grafana Dashboard Queries

Example PromQL queries for monitoring:

```promql
# Authentication success rate
sum(rate(gateway_auth_validations_total{status="success"}[5m]))
/ sum(rate(gateway_auth_validations_total[5m]))

# Authentication failures
rate(gateway_auth_validations_total{status="failed"}[5m])

# P99 authentication latency
histogram_quantile(0.99, rate(gateway_auth_latency_seconds_bucket[5m]))

# Key cache hit rate
sum(rate(gateway_key_cache_hits_total[5m]))
/ (sum(rate(gateway_key_cache_hits_total[5m])) + sum(rate(gateway_key_cache_misses_total[5m])))

# Access denials by reason
sum by (resource_type, reason) (rate(gateway_access_denials_total[5m]))

# Channel check deny rate
sum(rate(gateway_channel_checks_total{result="denied"}[5m]))
/ sum(rate(gateway_channel_checks_total[5m]))
```

---

## Architecture

### Component Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                         Gateway                                  │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐ │
│  │ MultiTenant     │  │ Postgres        │  │ Permission      │ │
│  │ Validator       │──│ KeyRegistry     │  │ Checker         │ │
│  └─────────────────┘  └─────────────────┘  └─────────────────┘ │
│         │                      │                    │           │
│         ▼                      ▼                    ▼           │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │                  Authorization Flow                          ││
│  │  1. Extract token → 2. Validate JWT → 3. Check permissions  ││
│  └─────────────────────────────────────────────────────────────┘│
│                              │                                   │
│                              ▼                                   │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │                     Observability                            ││
│  │  Metrics (Prometheus) │ Logs (zerolog) │ Audit (AuditLogger)││
│  └─────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────┘
```

### Key Registry Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    PostgresKeyRegistry                           │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────────┐    ┌─────────────────┐                     │
│  │  In-Memory      │◄───│  Background     │                     │
│  │  Cache          │    │  Refresh        │                     │
│  │  (sync.RWMutex) │    │  (ticker)       │                     │
│  └────────┬────────┘    └─────────────────┘                     │
│           │                      │                               │
│           │ cache miss           │ periodic refresh              │
│           ▼                      ▼                               │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │                    PostgreSQL                                ││
│  │                  (tenant_keys table)                         ││
│  └─────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────┘
```

---

## Claims Structure

```go
type Claims struct {
    jwt.RegisteredClaims

    // Tenant isolation (REQUIRED when auth enabled)
    TenantID string `json:"tenant_id"`

    // Identity attributes
    Attributes map[string]string `json:"attrs,omitempty"`

    // RBAC
    Roles []string `json:"roles,omitempty"`

    // Group memberships
    Groups []string `json:"groups,omitempty"`

    // Permission scopes
    Scopes []string `json:"scopes,omitempty"`
}

// Helper methods
func (c *Claims) HasRole(role string) bool
func (c *Claims) HasScope(scope string) bool
func (c *Claims) HasGroup(group string) bool
func (c *Claims) GetAttribute(key string) string
```

---

## Channel Permission Patterns

Configure via Helm values:

```yaml
permissions:
  # Public channels - any authenticated user can subscribe
  public:
    - "*.trade"
    - "*.liquidity"
    - "*.metadata"
    - "all.trade"
    - "all.liquidity"

  # User-scoped channels - JWT.sub must match {principal}
  userScoped:
    - "balances.{principal}"
    - "notifications.{principal}"

  # Group-scoped channels - JWT.groups must contain {group_id}
  groupScoped:
    - "community.{group_id}"
    - "social.{group_id}"
```

### Channel Naming (Tenant Implicit)

```
┌──────────────────────────────┬───────────────────────────────┐
│ Client API (Simple)          │ Server Internal (Tenant-Aware)│
├──────────────────────────────┼───────────────────────────────┤
│ subscribe("BTC.trade")       │ → acme.BTC.trade              │
│ subscribe("ETH.liquidity")   │ → acme.ETH.liquidity          │
│ subscribe("balances")        │ → acme.user123.balances       │
└──────────────────────────────┴───────────────────────────────┘
        Client sees                    Server maps (from JWT)
```

---

## Tenant Isolation

**Design Principle:** Auth implies isolation. Single switch controls everything.

```
┌─────────────────────────────────────────────────────────────────┐
│                      AUTH_ENABLED=false                          │
├─────────────────────────────────────────────────────────────────┤
│  - No JWT validation                                             │
│  - No tenant isolation                                           │
│  - Open access (for development/POC)                             │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                      AUTH_ENABLED=true                           │
├─────────────────────────────────────────────────────────────────┤
│  - JWT validation enforced                                       │
│  - Tenant isolation enforced                                     │
│  - Fail-secure mode (default deny)                               │
└─────────────────────────────────────────────────────────────────┘
```

---

## Testing

### Unit Tests

```go
// Test JWT validation
func TestMultiTenantValidator_ValidateToken_ES256(t *testing.T)
func TestMultiTenantValidator_ValidateToken_ExpiredToken(t *testing.T)
func TestMultiTenantValidator_ValidateToken_KeyNotFound(t *testing.T)
func TestMultiTenantValidator_ValidateToken_RevokedKey(t *testing.T)

// Test key registry
func TestPostgresKeyRegistry_GetKey(t *testing.T)
func TestPostgresKeyRegistry_CacheRefresh(t *testing.T)

// Test permissions
func TestPermissionChecker_PublicChannel(t *testing.T)
func TestPermissionChecker_UserScopedChannel(t *testing.T)
func TestPermissionChecker_GroupScopedChannel(t *testing.T)
```

### Integration Tests

```go
func TestGateway_AuthEnabled_ValidToken(t *testing.T)
func TestGateway_AuthEnabled_InvalidToken(t *testing.T)
func TestGateway_AuthEnabled_ExpiredToken(t *testing.T)
func TestGateway_ChannelPermission_Denied(t *testing.T)
```

Run tests:
```bash
go test ./internal/auth/... ./internal/gateway/... -v
```

---

## Security Checklist

### Authentication
- [x] ES256/RS256/EdDSA asymmetric keys
- [x] Key ID (kid) lookup from JWT header
- [x] Tenant ID in claims must match key owner
- [x] Only allowed algorithms accepted
- [x] Token expiration enforced

### Authorization
- [x] Default effect is deny (fail-secure)
- [x] Channel tenant isolation checked
- [x] All authorization decisions logged

### Key Management
- [x] Keys stored in PostgreSQL (durable)
- [x] In-memory cache for performance
- [x] Background cache refresh
- [x] Key revocation support

### Observability
- [x] Prometheus metrics for auth events
- [x] Structured logging with tenant context
- [x] Audit logging for security events

---

## Rate Limiting & Protection

The system includes multiple layers of protection against abuse:

### Current Implementation

| Protection | Scope | Location |
|------------|-------|----------|
| Per-IP connection rate limit | 10 burst, 1 conn/sec | `internal/limits/connection_rate_limiter.go` |
| Global connection rate limit | 300 burst, 50 conn/sec | `internal/limits/connection_rate_limiter.go` |
| Max connections (system-wide) | Configurable | `internal/limits/resource_guard.go` |
| CPU/Memory emergency brakes | System-wide | `internal/limits/resource_guard.go` |
| Message rate limiting | Per-connection | `internal/server/client.go` |

### How It Works

```
Client Connection
       │
       ▼
┌─────────────────────┐
│  Per-IP Rate Limit  │ ─── Reject if > 10 burst or > 1/sec from same IP
└─────────────────────┘
       │
       ▼
┌─────────────────────┐
│ Global Rate Limit   │ ─── Reject if > 300 burst or > 50/sec total
└─────────────────────┘
       │
       ▼
┌─────────────────────┐
│  Resource Guard     │ ─── Reject if max connections reached
└─────────────────────┘     or CPU/memory thresholds exceeded
       │
       ▼
┌─────────────────────┐
│  JWT Validation     │ ─── Reject if invalid/expired token
└─────────────────────┘
       │
       ▼
    Connected
```

---

## Future Enhancements

The following features are planned for future phases:

### Phase 2: Scale Features

| Feature | Description | Status |
|---------|-------------|--------|
| Per-tenant connection limits | Enforce max connections per tenant from quotas | **Implemented** |
| Tenant deletion background job | Periodic cleanup of deprovisioned tenants | **Implemented** |
| Per-tenant rate limiting | API rate limits based on tenant tier | Planned |
| Per-tenant metrics | Add `tenant_id` label to key metrics | **Implemented** |

### Phase 3: Enterprise Features

| Feature | Description | Priority |
|---------|-------------|----------|
| Tenant tiers/plans | Free, starter, pro, enterprise quota templates | Medium |
| Webhook notifications | Notify external systems of tenant events | Low |
| Allowed origins (CORS) | Per-tenant origin restrictions | Low |
| gRPC API | Alternative to REST for internal services | Low |
| Key usage tracking | `last_used_at`, `last_used_ip` for API keys | Low |

### Implementation Details

**Per-tenant connection limits** (`internal/gateway/tenant_connections.go`):
- `TenantConnectionTracker` tracks active connections per tenant
- Thread-safe with atomic operations
- Configurable default limit via `DEFAULT_TENANT_CONNECTION_LIMIT` (default: 1000)
- Enable/disable via `TENANT_CONNECTION_LIMIT_ENABLED` (default: true)
- Metrics: `gateway_tenant_connections_active`, `gateway_tenant_connections_rejected_total`

**Tenant deletion job** (`internal/provisioning/lifecycle.go`):
- `LifecycleManager` runs background goroutine
- Configurable interval via `LIFECYCLE_CHECK_INTERVAL` (default: 1h)
- Enable/disable via `LIFECYCLE_MANAGER_ENABLED` (default: true)
- Deletes Kafka topics and ACLs for deprovisioned tenants
- Updates tenant status to `deleted` after cleanup

---

## Related Documentation

- [Provisioning Service](./PROVISIONING_SERVICE.md) - Tenant onboarding API
- [Client Guide](../CLIENT_GUIDE.md) - WebSocket client integration
