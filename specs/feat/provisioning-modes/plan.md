# Implementation Plan: Provisioning Modes

**Branch**: `feat/provisioning-modes` | **Date**: 2026-02-28 | **Spec**: `specs/feat/provisioning-modes/spec.md`

## Summary

Add three provisioning modes to Odin WS: (1) embedded SQLite as default database, (2) YAML config file mode for static deployments, (3) a CLI tool for tenant management. Decouple gateway and ws-server from direct database access by replacing polling-based DB queries with gRPC server-side streaming from the provisioning service. The provisioning service exposes two interfaces: gRPC (internal, port 9090) for real-time data push to gateway/ws-server, and REST (external, port 8080) for CLI and admin UI.

## Technical Context

**Language**: Go 1.22+
**Services**: ws-server, ws-gateway, provisioning
**Infrastructure**: Kubernetes (GKE Standard), Helm, Terraform
**Messaging**: Redpanda/Kafka (franz-go), NATS (broadcast bus)
**Storage**: SQLite (new default), PostgreSQL (opt-in), YAML (config mode)
**Monitoring**: Prometheus, Grafana, Loki
**Build/Deploy**: Docker (CGO_ENABLED=0), Taskfile, Artifact Registry

**New Dependencies**:
- `modernc.org/sqlite` — Pure Go SQLite driver (no CGO)
- `google.golang.org/grpc` — gRPC framework
- `google.golang.org/protobuf` — Protobuf runtime
- `buf.build/gen/go/...` — Generated protobuf/gRPC code
- `spf13/cobra` — CLI framework
- `gopkg.in/yaml.v3` — YAML parsing (promote from indirect)

## Constitution Compliance

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Configuration | PASS | All new config via `env:` struct tags: `PROVISIONING_MODE`, `DATABASE_DRIVER`, `DATABASE_PATH`, `GRPC_PORT`, `PROVISIONING_GRPC_ADDR`. Validated at startup. |
| II. Defense in Depth | PASS | Config file validated at startup. gRPC responses validated by consumers. Stream snapshots validated before cache swap. |
| III. Error Handling | PASS | All errors wrapped: `fmt.Errorf("operation: %w", err)`. Sentinel errors: `ErrReadOnlyMode`, `ErrUnsupportedDriver`, `ErrConfigValidation`. gRPC errors use `status.Errorf()`. |
| IV. Graceful Degradation | PASS | Config mode: writes return `ErrReadOnlyMode`. Stream disconnect: serve stale cache. SIGHUP reload failure: keep previous config. SQLite unavailable: startup failure (not silent). |
| V. Structured Logging | PASS | All new code uses zerolog. Log modes at startup: `"provisioning_mode": "config"`, `"database_driver": "sqlite"`, `"grpc_port": 9090`. |
| VI. Observability | PASS | Metrics: `provisioning_stream_state`, `provisioning_stream_reconnects_total`, `provisioning_event_bus_events_total`, `provisioning_config_reload_total`, `provisioning_admin_auth_total`. gRPC interceptors: latency histograms, call counters. |
| VII. Concurrency Safety | PASS | Event bus: channel-based pub/sub, non-blocking fan-out. gRPC streams: per-client goroutine with `ctx` + `wg`. Config reload: atomic pointer swap under `sync.RWMutex`. Stream cache updates: write lock on cache swap only. |
| VIII. Testing | PASS | Tests for: proto client/server, event bus, stream reconnection, config parser/validator, in-memory stores, database factory, SQLite migrations, CLI commands. |
| IX. Security | PASS | gRPC internal API: no auth (network-isolated). REST external: opaque admin token via `PROVISIONING_ADMIN_TOKEN` (constant-time comparison, rate-limited auth failures, min 16-char in non-dev, redacted in logs). Existing JWT auth retained as fallback. No secrets in logs. |
| X. Shared Code Consolidation | PASS | gRPC stream registries in `shared/provapi/` (used by gateway + ws-server). Admin REST client in `cmd/cli/client/` (CLI-only, not shared). Reuses existing interfaces. Service-specific types stay in service packages. |
| XI. Prior Art Research | PASS | gRPC streaming: standard pattern (Kubernetes watch API, etcd). SQLite embedding: Grafana, Gitea, Drone CI. CLI: cobra (kubectl, docker, gh). |
| XII. API Design | PASS | REST: `/api/v1/` versioned, `httputil.WriteJSON/WriteError`. gRPC: proto in `ws/proto/`, buf codegen, streaming for watch, proper status codes, interceptors for recovery/logging/metrics. |

---

## Architecture Overview

### Data Flow — API Mode (SQLite or PostgreSQL)

```
CLI ──REST──→ Provisioning API (/api/v1/) ──→ Service Layer ──→ Repository ──→ SQLite/PostgreSQL
                                                    │
                                              EventBus (in-process)
                                                    │
                                              gRPC Streams (port 9090)
                                               ╱            ╲
                                        Gateway              ws-server
                                    (WatchKeys,           (WatchTopics)
                                   WatchTenantConfig)
```

### Data Flow — Config Mode

```
CLI ──REST──→ Provisioning API (/api/v1/) ──→ Service Layer ──→ ConfigStore (in-memory)
                                                    │                    ↑
                                              EventBus (in-process)   YAML File
                                                    │              (SIGHUP reload)
                                              gRPC Streams (port 9090)
                                               ╱            ╲
                                        Gateway              ws-server
```

### Key Design Principles

1. **Only the provisioning service touches the database.** Gateway and ws-server receive data via gRPC streaming.
2. **Interfaces don't change.** `auth.KeyRegistry`, `gateway.TenantRegistry`, `kafka.TenantRegistry` retain their signatures. Implementations change (DB-backed → gRPC stream-backed).
3. **Real-time push replaces polling.** gRPC server-side streaming with in-process event bus eliminates all 5 polling patterns.
4. **The provisioning service always runs.** In both API and config mode, it exposes gRPC + REST. The data source differs (database vs. YAML file).
5. **Two protocols, two ports.** gRPC on 9090 (internal), REST on 8080 (external). Each with graceful shutdown.

---

## Phase 1: Proto + gRPC Foundation

**Goal**: Define protobuf contracts, set up code generation, and create gRPC server skeleton.

### 1.1 Buf Configuration

**New file**: `ws/buf.yaml`
```yaml
version: v2
modules:
  - path: proto
lint:
  use:
    - STANDARD
breaking:
  use:
    - FILE
```

**New file**: `ws/buf.gen.yaml`
```yaml
version: v2
plugins:
  - remote: buf.build/protocolbuffers/go
    out: gen/proto
    opt: paths=source_relative
  - remote: buf.build/grpc/go
    out: gen/proto
    opt: paths=source_relative
```

### 1.2 Proto Definitions

**New file**: `ws/proto/odin/provisioning/v1/provisioning.proto`

```protobuf
syntax = "proto3";

package odin.provisioning.v1;

option go_package = "github.com/Toniq-Labs/odin-ws/gen/proto/odin/provisioning/v1;provisioningv1";

// Internal service — streaming provisioning data to gateway and ws-server.
service ProvisioningInternal {
  // Stream active keys — snapshot on connect, deltas on change.
  rpc WatchKeys(WatchKeysRequest) returns (stream KeysUpdate);

  // Stream tenant config (OIDC, channel rules) — snapshot on connect, deltas on change.
  rpc WatchTenantConfig(WatchTenantConfigRequest) returns (stream TenantConfigUpdate);

  // Stream topic discovery — snapshot on connect, deltas on change.
  rpc WatchTopics(WatchTopicsRequest) returns (stream TopicsUpdate);
}

message WatchKeysRequest {}

message KeysUpdate {
  bool is_snapshot = 1;           // true = full state, false = delta
  repeated KeyInfo keys = 2;      // All active keys (snapshot) or changed keys (delta)
  repeated string removed_key_ids = 3; // Key IDs removed (delta only)
}

message KeyInfo {
  string key_id = 1;
  string tenant_id = 2;
  string algorithm = 3;           // ES256, RS256, EdDSA
  string public_key_pem = 4;
  bool is_active = 5;
  int64 expires_at_unix = 6;      // 0 = no expiry
}

message WatchTenantConfigRequest {}

message TenantConfigUpdate {
  bool is_snapshot = 1;
  repeated TenantConfig tenants = 2;
  repeated string removed_tenant_ids = 3;
}

message TenantConfig {
  string tenant_id = 1;
  OIDCConfig oidc = 2;            // nil = no OIDC
  ChannelRules channel_rules = 3; // nil = no rules
}

message OIDCConfig {
  string issuer_url = 1;
  string jwks_url = 2;
  string audience = 3;
  bool enabled = 4;
}

message ChannelRules {
  repeated string public_channels = 1;
  map<string, GroupChannels> group_mappings = 2;
  repeated string default_channels = 3;
}

message GroupChannels {
  repeated string channels = 1;
}

message WatchTopicsRequest {
  string namespace = 1;           // Topic namespace (e.g., "prod")
}

message TopicsUpdate {
  bool is_snapshot = 1;
  repeated string shared_topics = 2;
  repeated DedicatedTenant dedicated_tenants = 3;
}

message DedicatedTenant {
  string tenant_id = 1;
  repeated string topics = 2;
}
```

### 1.3 Generated Code

Run `buf generate` to produce:
- `ws/gen/proto/odin/provisioning/v1/provisioning.pb.go`
- `ws/gen/proto/odin/provisioning/v1/provisioning_grpc.pb.go`

**New directory**: `ws/gen/proto/odin/provisioning/v1/` — committed to repo.

### 1.4 gRPC Server on Provisioning Service

**New file**: `ws/internal/provisioning/grpcserver/server.go`

```go
type Server struct {
    provisioningv1.UnimplementedProvisioningInternalServer
    eventBus *eventbus.Bus
    service  *provisioning.Service
    logger   zerolog.Logger
}

func NewServer(bus *eventbus.Bus, svc *provisioning.Service, logger zerolog.Logger) *Server
func (s *Server) WatchKeys(req *pb.WatchKeysRequest, stream pb.ProvisioningInternal_WatchKeysServer) error
func (s *Server) WatchTenantConfig(req *pb.WatchTenantConfigRequest, stream pb.ProvisioningInternal_WatchTenantConfigServer) error
func (s *Server) WatchTopics(req *pb.WatchTopicsRequest, stream pb.ProvisioningInternal_WatchTopicsServer) error
```

Each `Watch*` method:
1. Loads current state from service layer → sends snapshot (`is_snapshot=true`)
2. Subscribes to event bus for changes
3. Blocks in loop: receives event → loads affected data → sends delta (`is_snapshot=false`)
4. Returns when `stream.Context().Done()` fires

### 1.5 gRPC Interceptors

**New file**: `ws/internal/provisioning/grpcserver/interceptors.go`

- **Recovery interceptor** (first): catches panics via `logging.RecoverPanic()`, returns `codes.Internal`
- **Logging interceptor**: zerolog with method, duration, status
- **Metrics interceptor**: Prometheus histograms for latency, counters for calls by method + status

### 1.6 Admin Token Middleware

**New file**: `ws/internal/provisioning/api/admin_auth.go`

Opaque admin token authentication for operator access to the REST API (separate from tenant JWT auth).

Implemented as a struct with proper goroutine lifecycle (Constitution VII):

```go
type AdminAuth struct {
    adminToken string
    rateLimits sync.Map        // IP → {count, resetAt}
    logger     zerolog.Logger
    cancel     context.CancelFunc
    wg         *sync.WaitGroup  // shared with main shutdown path
}

func NewAdminAuth(ctx context.Context, wg *sync.WaitGroup, adminToken string, logger zerolog.Logger) *AdminAuth
func (a *AdminAuth) Middleware() func(http.Handler) http.Handler
func (a *AdminAuth) Close()  // cancels ctx, cleanup goroutine exits via ctx.Done()
```

Constructor starts a background cleanup goroutine following Constitution VII:
1. `wg.Add(1)` BEFORE `go` statement
2. `defer logging.RecoverPanic(...)` first defer
3. `defer wg.Done()` second defer
4. `select` on `ctx.Done()` (shutdown) and cleanup ticker (every 5 min)

Middleware logic:
- If `adminToken` is empty: middleware is disabled (pass-through)
- Extracts `Authorization: Bearer <token>` from header
- Compares using `crypto/subtle.ConstantTimeCompare` (constant-time, no timing leaks)
- On match: sets admin context (bypass tenant checks), calls next handler
- On mismatch: falls through to existing JWT middleware (does NOT reject)
- Per-IP rate limiting: `sync.Map` of IP → `{count, resetAt}`, 10 failures/min → HTTP 429 for 60s
- Background cleanup goroutine sweeps stale entries older than 2 min every 5 minutes
- Metrics: `provisioning_admin_auth_total` (by result), `provisioning_admin_auth_rate_limited_total`

**Modified file**: `ws/internal/provisioning/api/router.go`

Insert `adminAuth.Middleware()` before existing `AuthMiddleware` in the `/api/v1/` middleware chain.

**Modified file**: `ws/cmd/provisioning/main.go`

Wire `adminAuth.Close()` into the shutdown path (called before `wg.Wait()`).

### 1.7 Provisioning Config Changes

**Modified file**: `ws/internal/shared/platform/provisioning_config.go`

Add:
```go
GRPCPort   int    `env:"GRPC_PORT" envDefault:"9090"`
AdminToken string `env:"PROVISIONING_ADMIN_TOKEN"` // Opaque admin API key, redacted in logs
```

Validation:
- If `AdminToken` set and `len < 16` and `Environment != "dev"`: startup error
- If `AdminToken` set and `len < 16` and `Environment == "dev"`: log WARNING
- `LogConfig()` prints `"admin_token": "[REDACTED]"` when set

### 1.8 Provisioning Main — gRPC Listener

**Modified file**: `ws/cmd/provisioning/main.go`

Add gRPC server startup alongside HTTP:
```go
// Create gRPC server
grpcServer := grpc.NewServer(
    grpc.ChainStreamInterceptor(recoveryInterceptor, loggingInterceptor, metricsInterceptor),
)
provisioningv1.RegisterProvisioningInternalServer(grpcServer, grpcSrv)

// Start gRPC listener
lis, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.GRPCPort))
wg.Add(1)
go func() {
    defer logging.RecoverPanic(logger, "grpc-server")
    defer wg.Done()
    grpcServer.Serve(lis)
}()

// Graceful shutdown: stop gRPC + HTTP
grpcServer.GracefulStop()
```

---

## Phase 2: Event Bus

**Goal**: Create an in-process pub/sub for change notifications.

### 2.1 Event Bus Package

**New file**: `ws/internal/provisioning/eventbus/bus.go`

```go
type EventType int

const (
    KeysChanged EventType = iota
    TenantConfigChanged
    TopicsChanged
)

type Event struct {
    Type EventType
}

type Bus struct {
    mu          sync.RWMutex
    subscribers map[int]chan Event
    nextID      int
    logger      zerolog.Logger
}

func New(logger zerolog.Logger) *Bus
func (b *Bus) Subscribe() (id int, ch <-chan Event)
func (b *Bus) Unsubscribe(id int)
func (b *Bus) Publish(event Event)
```

`Publish()` uses non-blocking `select` with `default` to skip slow subscribers (Constitution VII — fan-out sends). Dropped events are counted via Prometheus metrics.

Subscriber channels are buffered (capacity 16) to absorb bursts.

### 2.2 Wire Event Bus into Service Layer

**Modified file**: `ws/internal/provisioning/service.go`

Add `EventBus *eventbus.Bus` field to `ServiceConfig` and `Service`.

After each successful write operation, emit the appropriate event:
- `CreateTenant()`, `UpdateTenant()`, `SuspendTenant()`, `ReactivateTenant()` → `TopicsChanged` + `TenantConfigChanged`
- `CreateKey()`, `RevokeKey()` → `KeysChanged`
- `CreateTopics()`, `DeleteTopics()` → `TopicsChanged`
- `UpdateQuota()` → (no stream impact — quotas are API-only)
- `CreateOIDCConfig()`, `UpdateOIDCConfig()`, `DeleteOIDCConfig()` → `TenantConfigChanged`
- `SetChannelRules()`, `DeleteChannelRules()` → `TenantConfigChanged`

Pattern:
```go
func (s *Service) CreateKey(ctx context.Context, ...) (*TenantKey, error) {
    key, err := s.keyStore.Create(ctx, ...)
    if err != nil { return nil, fmt.Errorf("create key: %w", err) }
    s.emitEvent(eventbus.KeysChanged)
    return key, nil
}

func (s *Service) emitEvent(eventType eventbus.EventType) {
    if s.eventBus != nil {
        s.eventBus.Publish(eventbus.Event{Type: eventType})
    }
}
```

Nil-guarded: event bus is optional (existing tests pass without it).

---

## Phase 3: Gateway Refactor (DB → gRPC Streaming)

**Goal**: Remove the gateway's PostgreSQL dependency. Replace `PostgresKeyRegistry` and `PostgresTenantRegistry` with gRPC stream-backed implementations.

### 3.1 gRPC Stream Key Registry

**New file**: `ws/internal/shared/provapi/key_registry.go`

Implements `auth.KeyRegistry` and `auth.KeyRegistryWithRefresh`:

```go
type StreamKeyRegistry struct {
    conn       *grpc.ClientConn
    mu         sync.RWMutex
    keysByID   map[string]*auth.KeyInfo
    keysByTenant map[string][]*auth.KeyInfo
    streamState atomic.Int32  // 0=connected, 1=reconnecting, 2=disconnected
    reconnects atomic.Int64
    logger     zerolog.Logger
    cancel     context.CancelFunc
    wg         sync.WaitGroup
}

type StreamKeyRegistryConfig struct {
    GRPCAddr          string        `env:"PROVISIONING_GRPC_ADDR" envDefault:"localhost:9090"`
    ReconnectDelay    time.Duration `env:"PROVISIONING_GRPC_RECONNECT_DELAY" envDefault:"1s"`
    ReconnectMaxDelay time.Duration `env:"PROVISIONING_GRPC_RECONNECT_MAX_DELAY" envDefault:"30s"`
    MetricPrefix      string        // "gateway" or "ws" — ensures Constitution VI compliance
    Logger            zerolog.Logger
}
```

**Lifecycle**:
1. `NewStreamKeyRegistry()` — dials gRPC, starts stream goroutine
2. Stream goroutine: calls `WatchKeys()`, receives snapshots/deltas, updates maps under write lock
3. On disconnect: enters reconnect loop with exponential backoff + jitter
4. `GetKey()` / `GetKeysByTenant()` — read lock on maps (same as PostgresKeyRegistry)
5. `Close()` — cancels context, waits for goroutine

### 3.2 gRPC Stream Tenant Registry

**New file**: `ws/internal/shared/provapi/tenant_registry.go`

Implements `gateway.TenantRegistry`:

```go
type StreamTenantRegistry struct {
    conn             *grpc.ClientConn
    mu               sync.RWMutex
    issuerToTenant   map[string]string              // issuerURL → tenantID
    oidcConfigs      map[string]*types.TenantOIDCConfig // tenantID → config
    channelRules     map[string]*types.ChannelRules     // tenantID → rules
    streamState      atomic.Int32
    logger           zerolog.Logger
    cancel           context.CancelFunc
    wg               sync.WaitGroup
}
```

Same streaming + reconnection pattern as `StreamKeyRegistry`.

### 3.3 Gateway Config Changes

**Modified file**: `ws/internal/shared/platform/gateway_config.go`

Remove:
- `ProvisioningDBURL` (`PROVISIONING_DATABASE_URL`)
- `DBMaxOpenConns`, `DBMaxIdleConns`, `DBConnMaxLifetime`, `DBConnMaxIdleTime`, `DBPingTimeout`
- `KeyCacheRefreshInterval`, `KeyCacheQueryTimeout` (no longer polling)
- `IssuerCacheTTL`, `ChannelRulesCacheTTL` (no longer TTL-based)

Add:
- `ProvisioningGRPCAddr string` (`env:"PROVISIONING_GRPC_ADDR" envDefault:"localhost:9090"`)
- `GRPCReconnectDelay time.Duration` (`env:"PROVISIONING_GRPC_RECONNECT_DELAY" envDefault:"1s"`)
- `GRPCReconnectMaxDelay time.Duration` (`env:"PROVISIONING_GRPC_RECONNECT_MAX_DELAY" envDefault:"30s"`)

Retain (used by multi-issuer OIDC — not affected by gRPC migration):
- `OIDCKeyfuncCacheTTL`, `JWKSFetchTimeout`, `JWKSRefreshInterval`

### 3.4 Gateway Wiring Changes

**Modified file**: `ws/internal/gateway/gateway.go`

In `setupValidator()`:
- Remove: `sql.Open("postgres", ...)`, connection pool config, `db.PingContext()`
- Remove: `auth.NewPostgresKeyRegistry(...)`
- Remove: `NewPostgresTenantRegistry(...)`
- Add: `provapi.NewStreamKeyRegistry(provapi.StreamKeyRegistryConfig{GRPCAddr: gw.config.ProvisioningGRPCAddr, ...})`
- Add: `provapi.NewStreamTenantRegistry(provapi.StreamTenantRegistryConfig{GRPCAddr: gw.config.ProvisioningGRPCAddr, ...})`

In `Gateway` struct:
- Remove: `dbConn *sql.DB`
- Add: (key registry and tenant registry already stored, just different types)

In `Close()`:
- Remove: `gw.dbConn.Close()`
- gRPC registries close their own connections via `Close()`

### 3.5 Multi-Issuer OIDC

**No changes needed** to `gateway/multi_issuer_oidc.go`. It depends on `gateway.TenantRegistry` interface — the stream-backed implementation satisfies it transparently. The keyfunc cache (1hr TTL, per-issuer JWKS refresh) continues to work independently.

### 3.6 Health Endpoint Updates

**Modified file**: `ws/internal/gateway/gateway.go` (or health handler)

Add provisioning stream state to health response:
```json
{
  "checks": {
    "provisioning_keys_stream": {"status": "connected", "healthy": true},
    "provisioning_config_stream": {"status": "reconnecting", "healthy": false}
  }
}
```

---

## Phase 4: ws-server Refactor (DB → gRPC Streaming)

**Goal**: Remove ws-server's PostgreSQL dependency for topic discovery.

### 4.1 gRPC Stream Topic Registry

**New file**: `ws/internal/shared/provapi/topic_registry.go`

Implements `kafka.TenantRegistry`:

```go
type StreamTopicRegistry struct {
    conn           *grpc.ClientConn
    mu             sync.RWMutex
    sharedTopics   []string
    dedicatedTenants []kafka.TenantTopics
    namespace      string
    streamState    atomic.Int32
    logger         zerolog.Logger
    cancel         context.CancelFunc
    wg             sync.WaitGroup
    onUpdate       func() // Callback to notify MultiTenantConsumerPool
}
```

**Key difference from key/tenant registries**: The topic registry needs to notify the `MultiTenantConsumerPool` when topics change so it can add/remove Kafka consumers. An `onUpdate` callback is passed at construction.

`GetSharedTenantTopics()` and `GetDedicatedTenants()` simply return cached data under read lock.

### 4.2 ws-server Config Changes

**Modified file**: `ws/internal/shared/platform/server_config.go`

Remove:
- `ProvisioningDatabaseURL` (`PROVISIONING_DATABASE_URL`)
- `ProvisioningDBMaxOpenConns`, `ProvisioningDBMaxIdleConns`, `ProvisioningDBConnMaxLifetime`, `ProvisioningDBConnMaxIdleTime`

Add:
- `ProvisioningGRPCAddr string` (`env:"PROVISIONING_GRPC_ADDR" envDefault:"localhost:9090"`)
- `GRPCReconnectDelay time.Duration` (`env:"PROVISIONING_GRPC_RECONNECT_DELAY" envDefault:"1s"`)
- `GRPCReconnectMaxDelay time.Duration` (`env:"PROVISIONING_GRPC_RECONNECT_MAX_DELAY" envDefault:"30s"`)

### 4.3 ws-server Wiring Changes

**Modified file**: `ws/cmd/server/main.go`

Remove (lines ~185-210):
- `sql.Open("postgres", cfg.ProvisioningDatabaseURL)`
- Connection pool config
- `db.PingContext()`
- `provisioning.NewTopicRegistry(provisioningDB)`

Add:
- `provapi.NewStreamTopicRegistry(provapi.StreamTopicRegistryConfig{...})`

Pass `StreamTopicRegistry` to `MultiTenantConsumerPool` config as `Registry`.

### 4.4 MultiTenantConsumerPool Adaptation

**Modified file**: `ws/internal/server/orchestration/multitenant_pool.go`

The pool currently calls `registry.GetSharedTenantTopics()` and `registry.GetDedicatedTenants()` on a 60s ticker. With the stream-backed registry:

- The registry's `onUpdate` callback triggers `refreshTopics()` immediately when new data arrives
- The 60s ticker can remain as a safety net (in case an event was missed)
- Or remove the ticker entirely and rely on stream events + a longer-interval heartbeat (e.g., 5min)

**Recommendation**: Keep a reduced-frequency ticker (5min) as a safety net. Primary trigger is `onUpdate` callback from stream events.

---

## Phase 5: Embedded SQLite + Database Factory

**Goal**: Make the provisioning service work with embedded SQLite as default.

### 5.1 SQLite Migrations

**New directory**: `ws/internal/provisioning/repository/migrations/sqlite/`

**`001_initial.sql`**: Same schema as PostgreSQL but adapted:
- `CREATE TYPE ... AS ENUM` → `TEXT` columns with `CHECK(col IN (...))`
- `SERIAL` → `INTEGER PRIMARY KEY`
- `JSONB` → `TEXT`
- `INET` → `TEXT`
- Regex CHECK constraints → omit (validated in Go)
- `NOW()` → `datetime('now')`
- Triggers: inline body (no `CREATE FUNCTION`)

**`002_oidc_channel_rules.sql`**: Same adaptations.

### 5.2 Migration Reorganization

Move existing PostgreSQL migrations:
- `repository/migrations/001_initial.sql` → `repository/migrations/postgres/001_initial.sql`
- `repository/migrations/002_oidc_channel_rules.sql` → `repository/migrations/postgres/002_oidc_channel_rules.sql`
- `repository/migrations/atlas.sum` → `repository/migrations/postgres/atlas.sum`

### 5.3 Embedded Migration Runner

**New file**: `ws/internal/provisioning/repository/embed.go`
```go
//go:embed migrations/postgres/*.sql
var postgresMigrations embed.FS

//go:embed migrations/sqlite/*.sql
var sqliteMigrations embed.FS
```

**New file**: `ws/internal/provisioning/repository/migrator.go`

Lightweight runner (~80 lines):
- Creates `schema_migrations` table
- Reads embedded migration files (sorted by name)
- Skips already-applied migrations
- Applies pending in a transaction
- Logs each migration

### 5.4 Database Factory

**New file**: `ws/internal/provisioning/repository/factory.go`

```go
type DatabaseConfig struct {
    Driver      string        // "sqlite" or "postgres"
    URL         string        // PostgreSQL connection URL
    Path        string        // SQLite file path (default: "odin.db")
    AutoMigrate bool          // Run embedded migrations on startup
    MaxOpenConns   int
    MaxIdleConns   int
    ConnMaxLifetime time.Duration
}

func OpenDatabase(cfg DatabaseConfig) (*sql.DB, error)
```

SQLite-specific setup:
- `PRAGMA journal_mode=WAL`
- `PRAGMA busy_timeout=5000`
- `PRAGMA foreign_keys=ON`

### 5.5 Provisioning Config Changes

**Modified file**: `ws/internal/shared/platform/provisioning_config.go`

Change `DATABASE_URL` from `required` to optional. Add:
```go
DatabaseDriver string `env:"DATABASE_DRIVER" envDefault:"sqlite"`
DatabasePath   string `env:"DATABASE_PATH" envDefault:"odin.db"`
AutoMigrate    bool   `env:"AUTO_MIGRATE" envDefault:"true"`
```

**Note**: These env vars are auto-injected by Helm templates (see Phase 8.1). Developers never set `DATABASE_DRIVER` or `DATABASE_URL` directly — Helm derives them from high-level values (`postgresql.enabled`, `externalDatabase.*`). The Go config reads the env vars as normal; the automation is entirely in the Helm layer.

Validation:
- `DATABASE_DRIVER` must be in `{sqlite, postgres}`
- If `postgres`: `DATABASE_URL` required
- If `sqlite`: `DATABASE_URL` ignored

### 5.6 Provisioning Main Changes

**Modified file**: `ws/cmd/provisioning/main.go`

Replace `sql.Open("postgres", cfg.DatabaseURL)` with:
```go
db, err := repository.OpenDatabase(repository.DatabaseConfig{
    Driver:      cfg.DatabaseDriver,
    URL:         cfg.DatabaseURL,
    Path:        cfg.DatabasePath,
    AutoMigrate: cfg.AutoMigrate,
    MaxOpenConns: cfg.DBMaxOpenConns,
    ...
})
```

### 5.7 SQLite Driver Import

**Modified file**: `ws/go.mod`

Add `modernc.org/sqlite`. Blank import in `repository/factory.go`:
```go
import _ "modernc.org/sqlite"
```

---

## Phase 6: Config File Mode

**Goal**: Allow the provisioning service to load all data from a YAML file.

### 6.1 Config File Types

**New file**: `ws/internal/provisioning/configstore/types.go`

```go
type ConfigFile struct {
    Tenants []TenantConfig `yaml:"tenants"`
}

type TenantConfig struct {
    ID           string            `yaml:"id"`
    Name         string            `yaml:"name"`
    ConsumerType string            `yaml:"consumer_type"`
    Metadata     map[string]any    `yaml:"metadata"`
    Categories   []CategoryConfig  `yaml:"categories"`
    Keys         []KeyConfig       `yaml:"keys"`
    Quotas       *QuotaConfig      `yaml:"quotas"`
    OIDC         *OIDCConfig       `yaml:"oidc"`
    ChannelRules *ChannelRulesConfig `yaml:"channel_rules"`
}
// ... nested types for each entity
```

### 6.2 Config File Parser

**New file**: `ws/internal/provisioning/configstore/parser.go`

- `ParseFile(path string) (*ConfigFile, error)`
- `ParseBytes(data []byte) (*ConfigFile, error)` — for testing and SIGHUP reload

### 6.3 Config File Validator

**New file**: `ws/internal/provisioning/configstore/validator.go`

- `Validate(cfg *ConfigFile) error`
- Returns multi-error (`errors.Join`) with all validation failures
- Checks: tenant ID format, unique IDs, unique key IDs, at least one category per tenant, algorithm enum, PEM format, HTTPS issuer URLs, no conflicting issuers

### 6.4 In-Memory Store Implementations

**New file**: `ws/internal/provisioning/configstore/stores.go`

Implements all 7 Store interfaces backed by in-memory maps:

```go
type ConfigStores struct {
    mu           sync.RWMutex
    tenants      map[string]*provisioning.Tenant
    keys         map[string]*provisioning.TenantKey
    keysByTenant map[string][]*provisioning.TenantKey
    categories   map[string][]*provisioning.TenantTopic
    quotas       map[string]*provisioning.TenantQuota
    oidcConfigs  map[string]*provisioning.TenantOIDCConfig
    channelRules map[string]*provisioning.TenantChannelRules
}
```

Read methods: return from maps under `RLock()`.
Write methods: return `ErrReadOnlyMode`.
`Ping()`: always returns nil.

### 6.5 Config Loader with Atomic Reload

**New file**: `ws/internal/provisioning/configstore/loader.go`

```go
type Loader struct {
    path   string
    stores atomic.Pointer[ConfigStores]
    logger zerolog.Logger
}

func NewLoader(path string, logger zerolog.Logger) (*Loader, error)
func (l *Loader) Load() error
func (l *Loader) Reload() error
func (l *Loader) Stores() *ConfigStores
```

`Load()` / `Reload()`: ParseFile → Validate → build ConfigStores → atomic swap.

### 6.6 Provisioning Mode Config

**Modified file**: `ws/internal/shared/platform/provisioning_config.go`

Add:
```go
ProvisioningMode string `env:"PROVISIONING_MODE" envDefault:"api"`
ConfigFilePath   string `env:"PROVISIONING_CONFIG_PATH"`
```

Validation:
- `PROVISIONING_MODE` must be in `{api, config}`
- If `config`: `PROVISIONING_CONFIG_PATH` required
- If `config`: `DATABASE_*` fields ignored

### 6.7 Provisioning Main — Mode Branching

**Modified file**: `ws/cmd/provisioning/main.go`

```go
var stores provisioning.AllStores // interface embedding all 7 stores

switch cfg.ProvisioningMode {
case "api":
    db, err := repository.OpenDatabase(...)
    stores = repository.NewStores(db)
case "config":
    loader, err := configstore.NewLoader(cfg.ConfigFilePath, logger)
    if err := loader.Load(); err != nil { log.Fatal()... }
    stores = loader.Stores()
    registerSIGHUP(ctx, &wg, loader, eventBus, logger)
}

svc := provisioning.NewService(provisioning.ServiceConfig{
    TenantStore: stores, KeyStore: stores, TopicStore: stores,
    EventBus: eventBus,
    ...
})
```

### 6.8 SIGHUP Handler

Following Constitution VII goroutine lifecycle:

```go
func registerSIGHUP(ctx context.Context, wg *sync.WaitGroup, loader *configstore.Loader, bus *eventbus.Bus, logger zerolog.Logger) {
    sighup := make(chan os.Signal, 1)
    signal.Notify(sighup, syscall.SIGHUP)
    wg.Add(1) // Step 1: Add BEFORE go statement
    go func() {
        defer logging.RecoverPanic(logger, "sighup-handler") // Step 2: RecoverPanic first
        defer wg.Done()                                       // Step 3: Done second
        for {
            select {
            case <-ctx.Done(): // Step 4: Check ctx.Done in select
                signal.Stop(sighup)
                return
            case <-sighup:
                start := time.Now()
                configReloadTotal.Inc()
                if err := loader.Reload(); err != nil {
                    configReloadFailuresTotal.Inc()
                    logger.Error().Err(err).Msg("config reload failed")
                } else {
                    configReloadDuration.Observe(time.Since(start).Seconds())
                    logger.Info().Msg("config reloaded")
                    bus.Publish(eventbus.Event{Type: eventbus.KeysChanged})
                    bus.Publish(eventbus.Event{Type: eventbus.TenantConfigChanged})
                    bus.Publish(eventbus.Event{Type: eventbus.TopicsChanged})
                }
            }
        }
    }()
}
```

### 6.9 Read-Only Mode Error Handling

**Modified file**: `ws/internal/provisioning/api/handlers.go` (or error mapping)

Map `ErrReadOnlyMode` → HTTP 405 Method Not Allowed with message `"write operations are not available in config mode"`.

---

## Phase 7: CLI Tool

**Goal**: Create an `odin` CLI binary for tenant management via REST API.

### 7.1 CLI Structure

**New directory**: `ws/cmd/cli/`

```
ws/cmd/cli/
├── main.go
├── commands/
│   ├── root.go         # Root command, global flags
│   ├── tenant.go       # tenant create/get/list/update/suspend/reactivate/deprovision
│   ├── key.go          # key create/list/revoke
│   ├── category.go     # category create/list
│   ├── quota.go        # quota get/update
│   ├── oidc.go         # oidc get/create/update/delete
│   ├── rules.go        # rules get/set/delete/test
│   ├── config.go       # config init/validate/export
│   └── output.go       # JSON/table formatting
```

### 7.2 Admin REST Client

**New file**: `ws/cmd/cli/client/admin_client.go` (CLI-only — per Constitution X, not in `shared/`)

HTTP client for `/api/v1/` endpoints with admin token auth:
```go
type AdminClient struct {
    baseURL    string
    httpClient *http.Client
    token      string
    logger     zerolog.Logger
}

func NewAdminClient(cfg AdminClientConfig) *AdminClient
func (c *AdminClient) CreateTenant(ctx, req) (*Tenant, error)
func (c *AdminClient) GetTenant(ctx, id) (*Tenant, error)
// ... all CRUD operations
```

### 7.3 Build Configuration

**Modified file**: `ws/go.mod` — Add `github.com/spf13/cobra`

**New file**: `ws/build/cli/Dockerfile` — Multi-stage build producing `odin` binary

---

## Phase 8: Helm + Docker Updates

### 8.1 Provisioning Service Helm Chart

**Modified files**: `deployments/helm/odin/charts/provisioning/values.yaml`, `templates/deployment.yaml`

**Values structure** (developer-facing, high-level):
```yaml
config:
  grpcPort: 9090
  provisioningMode: "api"      # "api" or "config"
  configFilePath: ""            # Required when provisioningMode=config
  autoMigrate: true
  adminToken: ""                # Empty = disabled

# Database — auto-detected from these values. Developers never set DATABASE_DRIVER directly.
# Default: SQLite (zero config)
# Bundled PostgreSQL: set postgresql.enabled: true in parent chart
# External PostgreSQL: set externalDatabase.url or .existingSecret below
database:
  sqlitePath: "/data/odin.db"
  maxOpenConns: 25
  maxIdleConns: 5
  connMaxLifetime: "5m"

externalDatabase:
  url: ""                       # postgres://user:pass@host:5432/odin?sslmode=require
  existingSecret: ""            # K8s secret name containing "database-url" key
  existingSecretKey: "database-url"
```

**Helm template auto-derivation** (in `deployment.yaml`):
```yaml
{{- /* Auto-detect database driver from high-level values */ -}}
{{- $dbDriver := "sqlite" -}}
{{- if or .Values.externalDatabase.url .Values.externalDatabase.existingSecret -}}
  {{- $dbDriver = "postgres" -}}
{{- else if .Values.global.postgresql.enabled -}}
  {{- $dbDriver = "postgres" -}}
{{- end -}}

- name: DATABASE_DRIVER
  value: {{ $dbDriver | quote }}

{{- if eq $dbDriver "sqlite" }}
- name: DATABASE_PATH
  value: {{ .Values.database.sqlitePath | quote }}
{{- else }}
  {{- /* PostgreSQL — resolve URL from externalDatabase or bundled subchart */ -}}
  {{- if .Values.externalDatabase.existingSecret }}
- name: DATABASE_URL
  valueFrom:
    secretKeyRef:
      name: {{ .Values.externalDatabase.existingSecret }}
      key: {{ .Values.externalDatabase.existingSecretKey }}
  {{- else if .Values.externalDatabase.url }}
- name: DATABASE_URL
  value: {{ .Values.externalDatabase.url | quote }}
  {{- else }}
  {{- /* Bundled PostgreSQL subchart — auto-constructed from service discovery */ -}}
- name: DATABASE_URL
  valueFrom:
    secretKeyRef:
      name: {{ printf "%s-provisioning-db" .Release.Name }}
      key: database-url
  {{- end }}
{{- end }}
```

This ensures:
- **SQLite (default)**: Nothing to configure. `DATABASE_DRIVER=sqlite`, `DATABASE_PATH=/data/odin.db`.
- **Bundled PostgreSQL**: Developer sets `postgresql.enabled: true` in parent values. Helm auto-constructs `DATABASE_URL` from the subchart's service name and auto-generated password (same as today's flow in `taskfiles/k8s.yml`).
- **External PostgreSQL**: Developer provides `externalDatabase.url` or `.existingSecret`. Only scenario requiring a developer-provided value.

Additional changes:
- Database pool settings (`DB_MAX_OPEN_CONNS`, `DB_MAX_IDLE_CONNS`, `DB_CONN_MAX_LIFETIME`) now reference `database.*` values (moved from `config.*`). Only injected when `$dbDriver == postgres` — SQLite doesn't use pool settings.
- gRPC port in container ports and service
- Volume mount for SQLite: `/data/` backed by PVC (only when `$dbDriver == sqlite`)
- Config mode volume mount: when `config.provisioningMode == "config"` and `config.configFileConfigMap` is set, mount the ConfigMap as a volume at `config.configFilePath` (default `/etc/odin/tenants.yaml`)
- Conditional init containers: `wait-for-postgres` only when `$dbDriver == postgres` and not external; `wait-for-redpanda` always (Kafka still needed)

**New file**: `deployments/helm/odin/charts/provisioning/templates/pvc.yaml`
- PVC for SQLite storage (only when `$dbDriver == "sqlite"`)
- Default: 1Gi, `ReadWriteOnce`

**New file**: `deployments/helm/odin/charts/provisioning/templates/service-grpc.yaml`
- ClusterIP service exposing gRPC port (internal only)

**New file**: `deployments/helm/odin/charts/provisioning/templates/secret-admin-token.yaml`
- Only rendered when `config.adminToken` is set
- Stores admin token as a Kubernetes Secret for secure injection into deployment

**New file**: `deployments/helm/odin/charts/provisioning/templates/ingress.yaml`
- Only rendered when `ingress.enabled: true`
- Standard Ingress resource with configurable `className`, `host`, TLS, and annotations
- Values note: when Ingress is enabled, `config.adminToken` SHOULD be set for security

Additional values for `values.yaml`:
- `config.adminToken: ""` (empty = disabled)
- `config.configFileConfigMap: ""` (name of ConfigMap containing the YAML config file; required when `provisioningMode=config` in K8s)
- `ingress.enabled: false`, `ingress.className: ""`, `ingress.host: ""`, `ingress.tls: []`, `ingress.annotations: {}`

**Ingress guard (FR-A08)**: The ingress template MUST include a Helm template `fail` guard:
```yaml
{{- if and .Values.ingress.enabled (not .Values.config.adminToken) }}
{{ fail "config.adminToken is required when ingress is enabled (FR-A08)" }}
{{- end }}
```

**Modified file**: `deployments/helm/odin/values.yaml` (parent chart)

Add `global.postgresql.enabled` so provisioning subchart can detect bundled PostgreSQL:
```yaml
global:
  postgresql:
    enabled: false  # Default: SQLite. Set true for bundled PostgreSQL subchart.
```

Change `postgresql.enabled` default from implicit to explicit `false` (SQLite is now the default). When a developer sets `postgresql.enabled: true`, the provisioning Helm template detects it via `global.postgresql.enabled` and auto-derives `DATABASE_DRIVER=postgres`.

Update `provisioning:` section to include `externalDatabase` values:
```yaml
provisioning:
  externalDatabase:
    url: ""
    existingSecret: ""
    existingSecretKey: "database-url"
```

### 8.2 Gateway Helm Chart

**Modified files**: `deployments/helm/odin/charts/ws-gateway/values.yaml`, `templates/deployment.yaml`

Remove:
- `PROVISIONING_DATABASE_URL` env var and secret reference
- `DB_MAX_OPEN_CONNS`, `DB_MAX_IDLE_CONNS`, `DB_CONN_MAX_LIFETIME`, `DB_CONN_MAX_IDLE_TIME`, `DB_PING_TIMEOUT`
- `KEY_CACHE_REFRESH_INTERVAL`, `KEY_CACHE_QUERY_TIMEOUT`
- `GATEWAY_ISSUER_CACHE_TTL`, `GATEWAY_CHANNEL_RULES_CACHE_TTL` (no longer needed — gRPC stream replaces TTL-based lazy caches)
- `wait-for-postgres` init container

Add:
- `PROVISIONING_GRPC_ADDR` (default: `{{ .Release.Name }}-provisioning-grpc:9090`)
- `PROVISIONING_GRPC_RECONNECT_DELAY`, `PROVISIONING_GRPC_RECONNECT_MAX_DELAY`
- `wait-for-provisioning` init container (wait for provisioning gRPC health)

### 8.3 ws-server Helm Chart

**Modified files**: `deployments/helm/odin/charts/ws-server/values.yaml`, `templates/deployment.yaml`

Same pattern as gateway:
- Remove: `PROVISIONING_DATABASE_URL`, `PROVISIONING_DB_*`, `wait-for-postgres`
- Add: `PROVISIONING_GRPC_ADDR`, reconnect config, `wait-for-provisioning`

### 8.4 Provisioning Dockerfile

**Modified file**: `ws/build/provisioning/Dockerfile`

Add:
```dockerfile
EXPOSE 9090
VOLUME ["/data"]
```

### 8.5 CLI Dockerfile

**New file**: `ws/build/cli/Dockerfile`

Same multi-stage pattern. Builds `odin` binary. No server — just the CLI.

### 8.6 Taskfile Updates

**Modified file**: `taskfiles/k8s.yml`

The Taskfile currently unconditionally handles PostgreSQL (password generation, `wait-for-db`, `db:migrate`). These steps MUST be conditional on the database driver.

**Conditional deploy logic (T047b)**:
- Derive `K8S_DB_DRIVER` from the Helm values file: parse `global.postgresql.enabled` — if `true`, set `postgres`; else `sqlite`.
- Wrap PostgreSQL-specific steps (password handling lines 80-87, `wait-for-db`, `db:migrate`) in `if [ "$K8S_DB_DRIVER" = "postgres" ]`.
- When SQLite: skip password handling, skip `wait-for-db`, skip `db:migrate` (auto-migrated at startup).
- Auto-sync `global.postgresql.enabled`: when the environment values file has `postgresql.enabled: true`, pass `--set global.postgresql.enabled=true` to the `helm install/upgrade` command to keep both values in sync.
- Update `db:migrate` and `db:status` tasks to check `K8S_DB_DRIVER` and skip gracefully with a message when SQLite.

**External DB setup command (T047c)**:
- New task: `k8s:db:setup-external` accepting `URL` variable (required).
- Flow: validate URL format (`postgres://`), create K8s secret `provisioning-external-db` in `K8S_NAMESPACE`, print instruction to set `provisioning.externalDatabase.existingSecret` in Helm values.
- Usage: `task k8s:db:setup-external URL="postgres://user:pass@host:5432/odin" ENV=dev`.

---

## Phase 9: Testing

### 9.1 Proto + gRPC

**New file**: `ws/internal/provisioning/grpcserver/server_test.go`
- Test each stream RPC with mock service
- Test snapshot delivery on connect
- Test delta delivery on event bus publish
- Test stream cancellation

### 9.2 Event Bus

**New file**: `ws/internal/provisioning/eventbus/bus_test.go`
- Test publish/subscribe
- Test non-blocking fan-out (slow subscriber doesn't block)
- Test unsubscribe
- Test concurrent publish/subscribe

### 9.3 gRPC Stream Registries

**New files**:
- `ws/internal/shared/provapi/key_registry_test.go`
- `ws/internal/shared/provapi/tenant_registry_test.go`
- `ws/internal/shared/provapi/topic_registry_test.go`

Test with `grpc.NewServer()` + `bufconn` (in-memory gRPC):
- Test cache population from snapshot
- Test cache update from delta
- Test reconnection behavior
- Test concurrent reads during update

### 9.4 Config Store

**New files**:
- `ws/internal/provisioning/configstore/parser_test.go`
- `ws/internal/provisioning/configstore/validator_test.go`
- `ws/internal/provisioning/configstore/stores_test.go`
- `ws/internal/provisioning/configstore/loader_test.go`

### 9.5 Database Factory + SQLite

**New files**:
- `ws/internal/provisioning/repository/factory_test.go`
- `ws/internal/provisioning/repository/migrator_test.go`

### 9.6 CLI Commands

**New files**: `ws/cmd/cli/commands/*_test.go`
- Test flag parsing, output formatting, error handling

### 9.7 Admin Client

**New file**: `ws/cmd/cli/client/admin_client_test.go`
- Test against httptest server

### 9.8 Admin Token Middleware

**New file**: `ws/internal/provisioning/api/admin_auth_test.go`
- Valid admin token grants access
- Invalid token falls through (doesn't reject)
- Empty admin token disables middleware
- Rate limiting after 10 failures
- Rate limit resets after 60s
- Admin token not in response body or logs

---

## File Change Summary

### New Files (~39)

| File | Purpose |
|------|---------|
| `ws/buf.yaml` | Buf configuration |
| `ws/buf.gen.yaml` | Buf code generation config |
| `ws/proto/odin/provisioning/v1/provisioning.proto` | Protobuf definitions |
| `ws/gen/proto/odin/provisioning/v1/*.pb.go` | Generated protobuf/gRPC code |
| `ws/internal/provisioning/eventbus/bus.go` | In-process event bus |
| `ws/internal/provisioning/grpcserver/server.go` | gRPC stream handlers |
| `ws/internal/provisioning/grpcserver/interceptors.go` | gRPC interceptors |
| `ws/internal/shared/provapi/key_registry.go` | gRPC stream-backed KeyRegistry |
| `ws/internal/shared/provapi/tenant_registry.go` | gRPC stream-backed TenantRegistry |
| `ws/internal/shared/provapi/topic_registry.go` | gRPC stream-backed TopicRegistry |
| `ws/cmd/cli/client/admin_client.go` | REST admin client for CLI |
| `ws/internal/provisioning/api/admin_auth.go` | Admin token middleware |
| `ws/internal/provisioning/repository/migrations/sqlite/*.sql` | SQLite migrations |
| `ws/internal/provisioning/repository/embed.go` | Embedded migration files |
| `ws/internal/provisioning/repository/migrator.go` | Auto-migration runner |
| `ws/internal/provisioning/repository/factory.go` | Database factory |
| `ws/internal/provisioning/repository/stores.go` | `NewStores(db)` constructor for AllStores |
| `ws/internal/provisioning/configstore/types.go` | YAML config types |
| `ws/internal/provisioning/configstore/parser.go` | YAML parser |
| `ws/internal/provisioning/configstore/validator.go` | Config validator |
| `ws/internal/provisioning/configstore/stores.go` | In-memory stores |
| `ws/internal/provisioning/configstore/loader.go` | Config loader + reload |
| `ws/cmd/cli/main.go` | CLI entrypoint |
| `ws/cmd/cli/commands/*.go` | CLI commands (8 files) |
| `ws/build/cli/Dockerfile` | CLI Docker build |
| `deployments/helm/odin/charts/provisioning/templates/pvc.yaml` | SQLite PVC |
| `deployments/helm/odin/charts/provisioning/templates/service-grpc.yaml` | gRPC service |
| `deployments/helm/odin/charts/provisioning/templates/secret-admin-token.yaml` | Admin token Secret |
| `deployments/helm/odin/charts/provisioning/templates/ingress.yaml` | Optional Ingress |

### Modified Files (~17)

| File | Changes |
|------|---------|
| `ws/go.mod` | Add grpc, protobuf, sqlite, cobra deps |
| `ws/internal/shared/platform/provisioning_config.go` | Add PROVISIONING_MODE, DATABASE_DRIVER, GRPC_PORT, PROVISIONING_ADMIN_TOKEN, etc. |
| `ws/internal/provisioning/api/router.go` | Wire admin token middleware before JWT middleware |
| `ws/internal/shared/platform/gateway_config.go` | Remove DB fields, add PROVISIONING_GRPC_ADDR |
| `ws/internal/shared/platform/server_config.go` | Remove DB fields, add PROVISIONING_GRPC_ADDR |
| `ws/internal/provisioning/interfaces.go` | Add `AllStores` interface embedding all 7 store interfaces |
| `ws/internal/provisioning/service.go` | Add EventBus, emit events after writes |
| `ws/cmd/provisioning/main.go` | Database factory, config mode, gRPC server, SIGHUP |
| `ws/internal/gateway/gateway.go` | Remove DB, use gRPC stream registries |
| `ws/cmd/server/main.go` | Remove DB, use gRPC stream topic registry |
| `ws/internal/server/orchestration/multitenant_pool.go` | Add onUpdate callback support |
| `ws/build/provisioning/Dockerfile` | Add gRPC port, SQLite volume |
| `deployments/helm/odin/charts/provisioning/values.yaml` | gRPC, SQLite, config mode fields |
| `deployments/helm/odin/charts/provisioning/templates/deployment.yaml` | New env vars, ports, PVC |
| `deployments/helm/odin/charts/ws-gateway/values.yaml` | Remove DB, add gRPC |
| `deployments/helm/odin/charts/ws-gateway/templates/deployment.yaml` | Remove DB env, add gRPC env |
| `deployments/helm/odin/charts/ws-server/values.yaml` | Remove DB, add gRPC |
| `deployments/helm/odin/charts/ws-server/templates/deployment.yaml` | Remove DB env, add gRPC env |

### Test Files (~17)

| File | Coverage |
|------|----------|
| `ws/internal/provisioning/grpcserver/server_test.go` | gRPC stream handlers |
| `ws/internal/provisioning/eventbus/bus_test.go` | Event bus |
| `ws/internal/shared/provapi/key_registry_test.go` | Stream key registry |
| `ws/internal/shared/provapi/tenant_registry_test.go` | Stream tenant registry |
| `ws/internal/shared/provapi/topic_registry_test.go` | Stream topic registry |
| `ws/cmd/cli/client/admin_client_test.go` | REST admin client |
| `ws/internal/provisioning/api/admin_auth_test.go` | Admin token middleware |
| `ws/internal/provisioning/configstore/parser_test.go` | YAML parser |
| `ws/internal/provisioning/configstore/validator_test.go` | Config validator |
| `ws/internal/provisioning/configstore/stores_test.go` | In-memory stores |
| `ws/internal/provisioning/configstore/loader_test.go` | Config loader |
| `ws/internal/provisioning/repository/factory_test.go` | Database factory |
| `ws/internal/provisioning/repository/migrator_test.go` | Migration runner |
| `ws/cmd/cli/commands/*_test.go` | CLI commands |

**Total**: ~39 new + ~17 modified + ~17 test = **~73 files**

---

## Phase Dependencies

```
Phase 1 (Proto + gRPC Foundation)
    └──→ Phase 2 (Event Bus)
              ├──→ Phase 3 (Gateway Refactor)
              ├──→ Phase 4 (ws-server Refactor)
              ├──→ Phase 5 (SQLite) ──→ Phase 6 (Config Mode)
              └──→ Phase 7 (CLI Tool)

Phase 8 (Helm + Docker) — after Phases 3-7 code compiles
Phase 9 (Testing — iterative per phase)
```

Notes:
- Phases 3, 4, 5, and 7 can all be parallelized (they depend only on Phase 2, not each other)
- Phase 5 (SQLite) is provisioning-internal — no dependency on gateway/ws-server refactor
- Phase 6 (Config Mode) depends on Phase 5 (uses same store interfaces)
- Phase 9 (testing) should be done iteratively per phase

---

## Verification Steps

**Phase 1**: `grpcurl -plaintext localhost:9090 list` shows `odin.provisioning.v1.ProvisioningInternal`.

**Phase 2**: Create a tenant via REST → event bus logs `KeysChanged` and `TopicsChanged`.

**Phase 3**: Gateway starts with `PROVISIONING_GRPC_ADDR=localhost:9090`, no `PROVISIONING_DATABASE_URL`. JWT auth works. Health shows `provisioning_keys_stream: connected`.

**Phase 4**: ws-server starts with `PROVISIONING_GRPC_ADDR=localhost:9090`. `MultiTenantConsumerPool` discovers topics. Topic refresh logs successful.

**Phase 5**: Provisioning starts with no `DATABASE_URL` → creates `odin.db`. `PRAGMA journal_mode` returns `wal`. All API operations work.

**Phase 6**: `PROVISIONING_MODE=config PROVISIONING_CONFIG_PATH=tenants.yaml` → serves data from YAML. `kill -HUP $PID` reloads. Write operations return 405.

**Phase 7**: `odin tenant list --api-url http://localhost:8080` returns tenants. `odin config validate --file tenants.yaml` validates.

**Phase 8**: `helm install` deploys with SQLite by default. Gateway/ws-server connect via gRPC. No `wait-for-postgres`.

### Full Integration

```bash
# API mode with SQLite (default)
./odin-provisioning  # Creates odin.db, listens on :8080 (REST) + :9090 (gRPC)

# CLI creates tenant
odin tenant create --id acme --name "Acme Corp" --category trade

# Gateway connects via gRPC
PROVISIONING_GRPC_ADDR=localhost:9090 ./odin-gateway

# ws-server discovers topics via gRPC
PROVISIONING_GRPC_ADDR=localhost:9090 ./odin-ws-server

# Config mode
odin config export --file tenants.yaml
PROVISIONING_MODE=config PROVISIONING_CONFIG_PATH=tenants.yaml ./odin-provisioning
```
