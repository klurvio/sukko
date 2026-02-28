# Implementation Plan: Provisioning Modes

**Branch**: `feat/provisioning-modes` | **Date**: 2026-02-27 | **Spec**: `specs/feat/provisioning-modes/spec.md`

## Summary

Add three provisioning modes to Odin WS: (1) embedded SQLite as default database (replacing mandatory PostgreSQL), (2) YAML config file mode for static deployments, and (3) a CLI tool for tenant management. This requires decoupling the gateway and ws-server from direct database access by migrating them to provisioning API calls, adding internal API endpoints, creating a shared API client library, and building the CLI.

## Technical Context

**Language**: Go 1.22+
**Services**: ws-server, ws-gateway, provisioning
**Infrastructure**: Kubernetes (GKE Standard), Helm, Terraform
**Messaging**: Redpanda/Kafka (franz-go), NATS (broadcast bus)
**Storage**: PostgreSQL (current), SQLite (new default), YAML (config mode)
**Monitoring**: Prometheus, Grafana, Loki
**Build/Deploy**: Docker (CGO_ENABLED=0), Taskfile, Artifact Registry

**New Dependencies**:
- `modernc.org/sqlite` — Pure Go SQLite driver (no CGO)
- `spf13/cobra` — CLI framework
- `gopkg.in/yaml.v3` — YAML parsing (promote from indirect to direct)

## Constitution Compliance

| Principle | Status | Notes |
|-----------|--------|-------|
| I. No Hardcoded Values | PASS | All new config via `env:` struct tags: `PROVISIONING_MODE`, `DATABASE_DRIVER`, `DATABASE_PATH`, `PROVISIONING_API_URL`, `AUTO_MIGRATE` |
| II. Defense in Depth | PASS | Config file validated at startup using same rules as API. API-backed registries re-validate responses. Internal API validates params. |
| III. Error Handling | PASS | All errors wrapped: `fmt.Errorf("operation: %w", err)`. Sentinel errors: `ErrReadOnlyMode`, `ErrUnsupportedDriver`, `ErrConfigValidation`. |
| IV. Graceful Degradation | PASS | Config mode: write operations return `ErrReadOnlyMode`. SIGHUP reload: rejects invalid config, keeps previous. API client: circuit breaker on provisioning API unavailability. |
| V. Structured Logging | PASS | All new code uses zerolog. Log modes at startup: `"provisioning_mode": "config"`, `"database_driver": "sqlite"`. All goroutines have `defer logging.RecoverPanic()`. |
| VI. Observability | PASS | Metrics: `provisioning_api_requests_total`, `provisioning_api_request_duration_seconds`, `provisioning_config_reloads_total`, `provisioning_config_reload_errors_total`. |
| VII. Concurrency Safety | PASS | Config hot-reload: atomic pointer swap under `sync.RWMutex`. API client caches: same patterns as existing PostgreSQL caches. Background goroutines: `context.Context` + `sync.WaitGroup`. |
| VIII. Configuration Validation | PASS | All new config validated at startup. `PROVISIONING_MODE` validated against `{api, config}`. `DATABASE_DRIVER` validated against `{sqlite, postgres}`. Startup fails fast on invalid config. |
| IX. Testing | PASS | Tests required for all new code. Config file parser, validator, in-memory stores, API client, API-backed registries, internal handlers, CLI commands, database factory, SQLite migrations. |
| X. Security | PASS | Internal API on separate route group (network-isolated). No secrets in logs — API URL logged without tokens. PEM keys in config files: plaintext (documented in spec as out of scope for encryption). |
| XI. Shared Code | PASS | API client in `shared/provapi/`. Reuses existing interfaces (`auth.KeyRegistry`, `gateway.TenantRegistry`, `kafka.TenantRegistry`). Validation logic shared between config parser and service layer. |

## Architecture Overview

### Data Flow — API Mode (SQLite or PostgreSQL)

```
CLI ──→ Provisioning API (/api/v1/) ──→ Service Layer ──→ Repository ──→ SQLite/PostgreSQL
                                              ↓
Gateway ──→ Internal API (/internal/v1/) ──→ Service Layer ──→ Repository ──→ SQLite/PostgreSQL
ws-server ──→ Internal API (/internal/v1/) ──→ Service Layer ──→ Repository ──→ SQLite/PostgreSQL
```

### Data Flow — Config Mode

```
CLI ──→ Provisioning API (/api/v1/) ──→ Service Layer ──→ ConfigStore (in-memory, read-only)
                                              ↓                    ↑
Gateway ──→ Internal API (/internal/v1/) ──→ Service Layer    YAML File (SIGHUP reload)
ws-server ──→ Internal API (/internal/v1/) ──→ Service Layer
```

### Key Design Principles

1. **Only the provisioning service touches the database.** Gateway and ws-server query the provisioning API — never the database.
2. **Interfaces don't change.** `auth.KeyRegistry`, `gateway.TenantRegistry`, `kafka.TenantRegistry` retain their signatures. Only implementations change (PostgreSQL-backed → API-backed).
3. **Caching layers are preserved.** The API-backed implementations use the same caching patterns (background ticker, TTL lazy caches) as the current PostgreSQL implementations. The data source changes; the caching behavior does not.
4. **The provisioning service always runs.** In both API and config mode, it exposes the REST API. The difference is the data source (database vs. YAML file).

---

## Phase 1: Internal API Endpoints + Client Library

**Goal**: Create the foundation that all subsequent phases depend on — internal API endpoints on the provisioning service and a shared HTTP client library.

### 1.1 Internal API Endpoints

Add a `/internal/v1/` route group to the provisioning service router (`provisioning/api/router.go`). No authentication middleware — these endpoints are for service-to-service communication within the Kubernetes cluster.

**New file**: `ws/internal/provisioning/api/internal_handlers.go`

| Endpoint | Method | Handler | Data Source |
|----------|--------|---------|-------------|
| `/internal/v1/keys/active` | GET | `GetActiveKeysInternal` | `KeyStore.GetActiveKeys()` |
| `/internal/v1/tenants/by-issuer` | GET | `GetTenantByIssuer` | `OIDCConfigStore.GetByIssuer()` |
| `/internal/v1/tenants/{tenantID}/oidc` | GET | `GetOIDCConfigInternal` | `OIDCConfigStore.Get()` |
| `/internal/v1/tenants/{tenantID}/channel-rules` | GET | `GetChannelRulesInternal` | `ChannelRulesStore.GetRules()` |
| `/internal/v1/topics/discovery` | GET | `GetTopicDiscovery` | `TopicStore` + `TenantStore` |
| `/internal/v1/health` | GET | `HealthInternal` | Ping database |

**Query parameters**:
- `/tenants/by-issuer?issuer_url=https://...` — URL-encoded issuer
- `/topics/discovery?namespace=prod` — topic namespace for `BuildTopicName()`

**Response format for `/topics/discovery`**:
```json
{
  "shared_topics": ["prod.acme.trade", "prod.acme.analytics"],
  "dedicated_tenants": [
    {"tenant_id": "bigco", "topics": ["prod.bigco.trade"]}
  ]
}
```

**Router changes** (`provisioning/api/router.go`):
```go
// Internal API — no auth, service-to-service only
r.Route("/internal/v1", func(r chi.Router) {
    r.Get("/keys/active", h.GetActiveKeysInternal)
    r.Get("/tenants/by-issuer", h.GetTenantByIssuer)
    r.Get("/tenants/{tenantID}/oidc", h.GetOIDCConfigInternal)
    r.Get("/tenants/{tenantID}/channel-rules", h.GetChannelRulesInternal)
    r.Get("/topics/discovery", h.GetTopicDiscovery)
    r.Get("/health", h.HealthInternal)
})
```

### 1.2 Provisioning API Client Library

**New package**: `ws/internal/shared/provapi/`

**`client.go`** — Core HTTP client:
```go
type Client struct {
    baseURL    string
    httpClient *http.Client
    logger     zerolog.Logger
}

type ClientConfig struct {
    BaseURL        string        `env:"PROVISIONING_API_URL" envDefault:"http://localhost:8080"`
    Timeout        time.Duration `env:"PROVISIONING_API_TIMEOUT" envDefault:"5s"`
    RetryAttempts  int           `env:"PROVISIONING_API_RETRIES" envDefault:"3"`
    RetryDelay     time.Duration `env:"PROVISIONING_API_RETRY_DELAY" envDefault:"500ms"`
    Logger         zerolog.Logger
}
```

Methods:
- `NewClient(cfg ClientConfig) *Client`
- `GetActiveKeys(ctx) ([]*auth.KeyInfo, error)`
- `GetTenantByIssuer(ctx, issuerURL) (string, error)`
- `GetOIDCConfig(ctx, tenantID) (*types.TenantOIDCConfig, error)`
- `GetChannelRules(ctx, tenantID) (*types.ChannelRules, error)`
- `GetTopicDiscovery(ctx, namespace) (*TopicDiscoveryResponse, error)`
- `Health(ctx) error`
- `Close() error`

Error handling: Wraps HTTP errors with context. Maps HTTP status codes to sentinel errors. Retries with exponential backoff (capped) on 5xx and connection errors.

### 1.3 Response Types

**New file**: `ws/internal/shared/provapi/types.go`

Shared response types that map provisioning API JSON responses to Go structs. Reuses existing types where possible (`auth.KeyInfo`, `types.TenantOIDCConfig`, `types.ChannelRules`).

---

## Phase 2: Gateway Refactor (Direct DB → Provisioning API)

**Goal**: Remove the gateway's PostgreSQL dependency. Replace `PostgresKeyRegistry` and `PostgresTenantRegistry` with API-backed implementations.

### 2.1 API-Backed Key Registry

**New file**: `ws/internal/shared/provapi/key_registry.go`

Implements `auth.KeyRegistry` and `auth.KeyRegistryWithRefresh`. Preserves the same caching pattern as `PostgresKeyRegistry`:

- Background ticker refresh (configurable interval, default 1min)
- `sync.RWMutex` + in-memory maps (`keysByID`, `keysByTenant`)
- Cache miss fallback: no individual key fetch via API (rely on cache refresh)
- Goroutine lifecycle: `context.Context` + `sync.WaitGroup`, `defer logging.RecoverPanic()`
- Stats: `cacheHits`, `cacheMisses`, `refreshErrors` (atomic counters)

**Difference from PostgresKeyRegistry**: Data source is `client.GetActiveKeys()` HTTP call instead of SQL query. Everything else identical.

### 2.2 API-Backed Tenant Registry

**New file**: `ws/internal/shared/provapi/tenant_registry.go`

Implements `gateway.TenantRegistry`. Preserves the same TTL-based lazy caching:

- `issuerCache map[string]*issuerCacheEntry` (TTL: 5min default)
- `oidcConfigCache map[string]*oidcConfigCacheEntry` (TTL: 5min default)
- `channelRulesCache map[string]*channelRulesCacheEntry` (TTL: 1min default)
- Each cache protected by `sync.RWMutex`
- Cache miss: calls `client.GetTenantByIssuer()`, `client.GetOIDCConfig()`, `client.GetChannelRules()`

### 2.3 Gateway Config Changes

**Modified file**: `ws/internal/shared/platform/gateway_config.go`

Remove:
- `ProvisioningDBURL` (`PROVISIONING_DATABASE_URL`)
- `DBMaxOpenConns`, `DBMaxIdleConns`, `DBConnMaxLifetime`, `DBConnMaxIdleTime`, `DBPingTimeout`

Add:
- `ProvisioningAPIURL string` (`env:"PROVISIONING_API_URL" envDefault:"http://localhost:8080"`)
- `ProvisioningAPITimeout time.Duration` (`env:"PROVISIONING_API_TIMEOUT" envDefault:"5s"`)

Retain (used by API-backed registries):
- `KeyCacheRefreshInterval`, `KeyCacheQueryTimeout`
- `IssuerCacheTTL`, `ChannelRulesCacheTTL`
- `OIDCKeyfuncCacheTTL`, `JWKSFetchTimeout`

### 2.4 Gateway Wiring Changes

**Modified file**: `ws/internal/gateway/gateway.go`

In `setupValidator()`:
- Remove: `sql.Open("postgres", ...)`, connection pool config, `db.PingContext()`
- Add: `provapi.NewClient(provapi.ClientConfig{BaseURL: gw.config.ProvisioningAPIURL, ...})`
- Replace: `auth.NewPostgresKeyRegistry(...)` → `provapi.NewAPIKeyRegistry(client, ...)`
- Replace: `NewPostgresTenantRegistry(...)` → `provapi.NewAPITenantRegistry(client, ...)`

In `Close()`:
- Remove: `gw.dbConn.Close()`
- Add: `gw.apiClient.Close()`

### 2.5 Multi-Issuer OIDC

**No changes needed** to `gateway/multi_issuer_oidc.go`. It depends on `gateway.TenantRegistry` interface — the API-backed implementation satisfies it transparently.

---

## Phase 3: ws-server Refactor (Direct DB → Provisioning API)

**Goal**: Remove the ws-server's PostgreSQL dependency for topic discovery.

### 3.1 API-Backed Topic Registry

**New file**: `ws/internal/shared/provapi/topic_registry.go`

Implements `kafka.TenantRegistry`:

```go
type APITopicRegistry struct {
    client *Client
}

func (r *APITopicRegistry) GetSharedTenantTopics(ctx context.Context, namespace string) ([]string, error) {
    resp, err := r.client.GetTopicDiscovery(ctx, namespace)
    // ... return resp.SharedTopics
}

func (r *APITopicRegistry) GetDedicatedTenants(ctx context.Context, namespace string) ([]kafka.TenantTopics, error) {
    resp, err := r.client.GetTopicDiscovery(ctx, namespace)
    // ... return resp.DedicatedTenants
}
```

No additional caching — the `MultiTenantConsumerPool` already has its own refresh loop (60s ticker). The API call replaces the SQL query 1:1.

### 3.2 ws-server Config Changes

**Modified file**: `ws/internal/shared/platform/server_config.go`

Remove:
- `ProvisioningDatabaseURL` (`PROVISIONING_DATABASE_URL`)
- `ProvisioningDBMaxOpenConns`, `ProvisioningDBMaxIdleConns`, `ProvisioningDBConnMaxLifetime`, `ProvisioningDBConnMaxIdleTime`

Add:
- `ProvisioningAPIURL string` (`env:"PROVISIONING_API_URL" envDefault:"http://localhost:8080"`)
- `ProvisioningAPITimeout time.Duration` (`env:"PROVISIONING_API_TIMEOUT" envDefault:"5s"`)

### 3.3 ws-server Wiring Changes

**Modified file**: `ws/cmd/server/main.go`

Remove (lines ~185-210):
- `sql.Open("postgres", cfg.ProvisioningDatabaseURL)`
- Connection pool config
- `db.PingContext()`
- `provisioning.NewTopicRegistry(provisioningDB)`

Add:
- `provapi.NewClient(provapi.ClientConfig{BaseURL: cfg.ProvisioningAPIURL, ...})`
- `provapi.NewAPITopicRegistry(client)`

Pass `APITopicRegistry` to `MultiTenantConsumerPool` config as `Registry`.

### 3.4 Kafka Producer Topic Validation

**Modified file**: `ws/internal/shared/kafka/producer.go`

The producer's `tenantRegistry` field is optional (currently nil in ws-server). If future code passes a registry, it will work with the API-backed implementation transparently. No changes needed.

---

## Phase 4: Embedded SQLite + Database Factory

**Goal**: Make the provisioning service work with embedded SQLite as default, PostgreSQL as opt-in.

### 4.1 SQLite Migrations

**New directory**: `ws/internal/provisioning/repository/migrations/sqlite/`

Create SQLite-equivalent migration files:

**`001_initial.sql`**: Same schema as PostgreSQL but adapted:
- `CREATE TYPE ... AS ENUM` → `TEXT` columns with `CHECK(col IN (...))`
- `SERIAL` → `INTEGER PRIMARY KEY` (SQLite auto-increments rowid)
- `BIGSERIAL` → `INTEGER PRIMARY KEY`
- `JSONB` → `TEXT`
- `INET` → `TEXT`
- `id ~ '^[a-z]...'` regex CHECK → omit (validated in Go service layer)
- `NOW()` → `datetime('now')`
- Trigger syntax: `CREATE TRIGGER ... BEGIN ... END;` (no `CREATE FUNCTION`)

**`002_oidc_channel_rules.sql`**: Same adaptations.

### 4.2 Migration Reorganization

Move existing PostgreSQL migrations:
- `repository/migrations/001_initial.sql` → `repository/migrations/postgres/001_initial.sql`
- `repository/migrations/002_oidc_channel_rules.sql` → `repository/migrations/postgres/002_oidc_channel_rules.sql`
- `repository/migrations/atlas.sum` → `repository/migrations/postgres/atlas.sum`

### 4.3 Embedded Migration Runner

**New file**: `ws/internal/provisioning/repository/embed.go`
```go
//go:embed migrations/postgres/*.sql
var postgresMigrations embed.FS

//go:embed migrations/sqlite/*.sql
var sqliteMigrations embed.FS
```

**New file**: `ws/internal/provisioning/repository/migrator.go`

Lightweight migration runner (~80 lines):
- Creates `schema_migrations` table if not exists
- Reads migration files from embedded FS (sorted by name)
- Skips already-applied migrations (tracked by version number)
- Applies pending migrations in a transaction
- Logs each migration applied

### 4.4 Database Factory

**New file**: `ws/internal/provisioning/repository/factory.go`

```go
type DatabaseConfig struct {
    Driver         string        // "sqlite" or "postgres"
    URL            string        // PostgreSQL connection URL
    Path           string        // SQLite file path (default: "odin.db")
    AutoMigrate    bool          // Run embedded migrations on startup
    MaxOpenConns   int
    MaxIdleConns   int
    ConnMaxLifetime time.Duration
}

func OpenDatabase(cfg DatabaseConfig) (*sql.DB, error) {
    switch cfg.Driver {
    case "sqlite":
        db, err := sql.Open("sqlite", cfg.Path)
        // PRAGMA journal_mode=WAL
        // PRAGMA busy_timeout=5000
        // PRAGMA foreign_keys=ON
        if cfg.AutoMigrate { RunMigrations(db, sqliteMigrations) }
        return db, nil
    case "postgres":
        db, err := sql.Open("postgres", cfg.URL)
        // Connection pool settings
        if cfg.AutoMigrate { RunMigrations(db, postgresMigrations) }
        return db, nil
    default:
        return nil, fmt.Errorf("unsupported database driver: %s", cfg.Driver)
    }
}
```

### 4.5 Provisioning Config Changes

**Modified file**: `ws/internal/shared/platform/provisioning_config.go`

Change `DATABASE_URL` from `required` to optional. Add new fields:

```go
// Database
DatabaseDriver  string `env:"DATABASE_DRIVER" envDefault:"sqlite"`
DatabaseURL     string `env:"DATABASE_URL"`                          // PostgreSQL only
DatabasePath    string `env:"DATABASE_PATH" envDefault:"odin.db"`    // SQLite only
AutoMigrate     bool   `env:"AUTO_MIGRATE" envDefault:"true"`
```

Validation logic:
- If `DATABASE_DRIVER=postgres` and `DATABASE_URL` is empty → error
- If `DATABASE_DRIVER=sqlite` → `DATABASE_URL` is ignored
- `DATABASE_DRIVER` must be in `{sqlite, postgres}`

### 4.6 Provisioning Main Changes

**Modified file**: `ws/cmd/provisioning/main.go`

Replace:
```go
db, err := sql.Open("postgres", cfg.DatabaseURL)
```

With:
```go
db, err := repository.OpenDatabase(repository.DatabaseConfig{
    Driver:          cfg.DatabaseDriver,
    URL:             cfg.DatabaseURL,
    Path:            cfg.DatabasePath,
    AutoMigrate:     cfg.AutoMigrate,
    MaxOpenConns:    cfg.DBMaxOpenConns,
    MaxIdleConns:    cfg.DBMaxIdleConns,
    ConnMaxLifetime: cfg.DBConnMaxLifetime,
})
```

The rest of `main.go` (repository creation, service creation, HTTP setup) remains unchanged — all repositories accept `*sql.DB` regardless of driver.

### 4.7 SQLite Driver Import

**Modified file**: `ws/go.mod`

Add `modernc.org/sqlite` dependency. The driver is registered via blank import in `repository/factory.go`:
```go
import _ "modernc.org/sqlite"
```

---

## Phase 5: Config File Mode

**Goal**: Allow the provisioning service to load all data from a YAML file with no database.

### 5.1 Config File Types

**New file**: `ws/internal/provisioning/configstore/types.go`

Go structs for YAML deserialization matching the schema from R8:

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
```

### 5.2 Config File Parser

**New file**: `ws/internal/provisioning/configstore/parser.go`

- `ParseFile(path string) (*ConfigFile, error)` — reads and deserializes YAML
- `ParseBytes(data []byte) (*ConfigFile, error)` — for testing and SIGHUP reload

### 5.3 Config File Validator

**New file**: `ws/internal/provisioning/configstore/validator.go`

- `Validate(cfg *ConfigFile) error` — validates entire config
- Reuses validation rules from the service layer where possible
- Checks: tenant ID format, unique tenant IDs, unique key IDs across all tenants, at least one category per tenant, algorithm enum, PEM format, HTTPS issuer URLs, no conflicting OIDC issuers
- Returns multi-error with all validation failures (not just the first)

### 5.4 In-Memory Store Implementations

**New file**: `ws/internal/provisioning/configstore/stores.go`

Implements all 7 Store interfaces backed by in-memory maps:

```go
type ConfigStores struct {
    mu            sync.RWMutex
    tenants       map[string]*provisioning.Tenant
    keys          map[string]*provisioning.TenantKey
    keysByTenant  map[string][]*provisioning.TenantKey
    categories    map[string][]*provisioning.TenantTopic
    quotas        map[string]*provisioning.TenantQuota
    oidcConfigs   map[string]*provisioning.TenantOIDCConfig
    channelRules  map[string]*provisioning.TenantChannelRules
    // ... audit is append-only in-memory slice (limited size)
}
```

Read methods: Look up from maps under `RLock()`.
Write methods: Return `ErrReadOnlyMode` sentinel error.
`Ping()`: Always returns nil (no external dependency).

Each store method (`TenantStore`, `KeyStore`, etc.) is exposed as a method that returns the appropriate interface type, or `ConfigStores` itself implements all interfaces.

### 5.5 Config Loader with Atomic Reload

**New file**: `ws/internal/provisioning/configstore/loader.go`

```go
type Loader struct {
    path   string
    stores atomic.Pointer[ConfigStores]
    logger zerolog.Logger
}

func NewLoader(path string, logger zerolog.Logger) (*Loader, error)
func (l *Loader) Load() error            // Initial load (startup)
func (l *Loader) Reload() error           // Hot-reload (SIGHUP)
func (l *Loader) Stores() *ConfigStores   // Current stores
```

`Load()` and `Reload()` flow:
1. `ParseFile(path)`
2. `Validate(cfg)`
3. Build new `ConfigStores` from validated config
4. Atomically swap: `l.stores.Store(newStores)`

On reload failure: log errors, return error, previous stores remain active.

### 5.6 Provisioning Mode Config

**Modified file**: `ws/internal/shared/platform/provisioning_config.go`

Add:
```go
ProvisioningMode   string `env:"PROVISIONING_MODE" envDefault:"api"`
ConfigFilePath     string `env:"PROVISIONING_CONFIG_PATH"`
```

Validation:
- `PROVISIONING_MODE` must be in `{api, config}`
- If `config`: `PROVISIONING_CONFIG_PATH` is required
- If `config`: `DATABASE_*` fields are ignored

### 5.7 Provisioning Main — Mode Branching

**Modified file**: `ws/cmd/provisioning/main.go`

```go
switch cfg.ProvisioningMode {
case "api":
    // Existing path: open database, create repositories, create service
    db, err := repository.OpenDatabase(...)
    tenantRepo := repository.NewPostgresTenantRepository(db)
    // ...
    svc := provisioning.NewService(...)

case "config":
    // New path: load YAML, create in-memory stores, create service
    loader, err := configstore.NewLoader(cfg.ConfigFilePath, logger)
    if err := loader.Load(); err != nil { log.Fatal()... }
    stores := loader.Stores()
    svc := provisioning.NewService(provisioning.ServiceConfig{
        TenantStore:  stores,
        KeyStore:     stores,
        TopicStore:   stores,
        // ... all stores from ConfigStores
        KafkaAdmin:   kafka.NewNoopAdmin(), // No Kafka ops in config mode
    })
    // Register SIGHUP handler for hot-reload
    registerSIGHUP(loader)
}
```

### 5.8 SIGHUP Handler

Add to `cmd/provisioning/main.go` (config mode only):

```go
func registerSIGHUP(loader *configstore.Loader) {
    sighup := make(chan os.Signal, 1)
    signal.Notify(sighup, syscall.SIGHUP)
    go func() {
        defer logging.RecoverPanic(logger, "sighup-handler")
        for range sighup {
            logger.Info().Msg("received SIGHUP, reloading config")
            if err := loader.Reload(); err != nil {
                logger.Error().Err(err).Msg("config reload failed, keeping previous config")
            } else {
                logger.Info().Msg("config reloaded successfully")
            }
        }
    }()
}
```

### 5.9 Service Layer — Read-Only Mode Awareness

**Modified file**: `ws/internal/provisioning/service.go`

No changes needed. The service calls Store methods. Write methods in ConfigStores return `ErrReadOnlyMode`. The service propagates this error to the HTTP handler, which returns HTTP 405 Method Not Allowed (or 409 Conflict) with a clear message.

**New file**: `ws/internal/provisioning/api/errors.go` (or modify existing error handling)

Map `ErrReadOnlyMode` to HTTP 405 with message: `"write operations are not available in config mode"`.

---

## Phase 6: CLI Tool

**Goal**: Create an `odin` CLI binary for managing tenants via the provisioning API.

### 6.1 CLI Structure

**New directory**: `ws/cmd/cli/`

```
ws/cmd/cli/
├── main.go                 # Entrypoint, root command
├── commands/
│   ├── root.go             # Root command, global flags (--api-url, --token, --output)
│   ├── tenant.go           # tenant create/get/list/update/suspend/reactivate/deprovision
│   ├── key.go              # key create/list/revoke
│   ├── category.go         # category create/list
│   ├── quota.go            # quota get/update
│   ├── oidc.go             # oidc get/create/update/delete
│   ├── rules.go            # rules get/set/delete/test
│   ├── config.go           # config init/validate/export
│   └── output.go           # JSON/table output formatting
```

### 6.2 Global Flags

```go
// root.go
var (
    apiURL    string // --api-url or ODIN_API_URL
    token     string // --token or ODIN_TOKEN
    outputFmt string // --output json|table (default: table)
)
```

### 6.3 API Client

The CLI uses the same `provapi.Client` from Phase 1 for internal endpoints, plus a new authenticated client for the external `/api/v1/` endpoints:

**New file**: `ws/internal/shared/provapi/admin_client.go`

Extends `Client` with:
- Bearer token authentication (from `--token` or `ODIN_TOKEN`)
- External API endpoints (`/api/v1/tenants/...`, etc.)
- All CRUD operations: create/get/update/delete for tenants, keys, categories, quotas, OIDC, channel rules

### 6.4 Command Implementations

Each command file follows the pattern:
```go
func newTenantCreateCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "create",
        Short: "Create a new tenant",
        RunE: func(cmd *cobra.Command, args []string) error {
            client := getClient()
            req := provisioning.CreateTenantRequest{...}
            tenant, err := client.CreateTenant(ctx, req)
            return printOutput(tenant)
        },
    }
    cmd.Flags().StringVar(&id, "id", "", "Tenant ID (required)")
    cmd.Flags().StringVar(&name, "name", "", "Tenant display name")
    cmd.Flags().StringSliceVar(&categories, "category", nil, "Categories to provision")
    cmd.MarkFlagRequired("id")
    return cmd
}
```

### 6.5 Config File Commands

**`odin config init --tenant my-tenant`**: Generates a well-commented YAML config file template with the specified tenant ID and sensible defaults.

**`odin config validate --file tenants.yaml`**: Loads the file, runs the same validator from Phase 5, reports errors with context. Exit code 0 on success, 1 on failure.

**`odin config export --file tenants.yaml`**: Calls the provisioning API to list all tenants with their full configuration, converts to YAML format, writes to file.

### 6.6 Output Formatting

**`output.go`** — Shared output formatter:
- `--output json`: `json.MarshalIndent()` to stdout
- `--output table`: Tabular format using `text/tabwriter`
- Default: `table` for interactive use

### 6.7 Build Configuration

**Modified file**: `ws/go.mod` — Add `github.com/spf13/cobra`

**New file**: `ws/build/cli/Dockerfile` — Multi-stage build producing `odin` binary

**Taskfile**: Add `build:cli` and `k8s:build:push:cli` targets

---

## Phase 7: Helm + Docker Updates

**Goal**: Update deployment manifests for the new provisioning modes.

### 7.1 Provisioning Service Helm Chart

**Modified file**: `deployments/helm/odin/charts/provisioning/values.yaml`

Add:
```yaml
config:
  provisioningMode: "api"           # api or config
  databaseDriver: "sqlite"          # sqlite or postgres
  databasePath: "/data/odin.db"     # SQLite file path
  autoMigrate: true
  configFilePath: ""                # Path to YAML config (config mode)
```

**Modified file**: `deployments/helm/odin/charts/provisioning/templates/deployment.yaml`

Add:
- `PROVISIONING_MODE`, `DATABASE_DRIVER`, `DATABASE_PATH`, `AUTO_MIGRATE` env vars
- `PROVISIONING_CONFIG_PATH` env var (config mode)
- Conditional `DATABASE_URL` (only when `databaseDriver: postgres`)
- Volume mount for SQLite persistent storage: `/data/` backed by a PVC
- Volume mount for config file (if config mode): ConfigMap or Secret mount
- Remove `wait-for-postgres` init container when `databaseDriver: sqlite`

**New file**: `deployments/helm/odin/charts/provisioning/templates/pvc.yaml`
- PersistentVolumeClaim for SQLite storage (only when `databaseDriver: sqlite`)
- Default: 1Gi, `ReadWriteOnce`

### 7.2 Gateway Helm Chart

**Modified files**: `deployments/helm/odin/charts/ws-gateway/values.yaml`, `templates/deployment.yaml`

Remove:
- `PROVISIONING_DATABASE_URL` env var and secret reference
- `DB_MAX_OPEN_CONNS`, `DB_MAX_IDLE_CONNS`, `DB_CONN_MAX_LIFETIME`, `DB_CONN_MAX_IDLE_TIME` env vars
- `wait-for-postgres` init container

Add:
- `PROVISIONING_API_URL` env var (default: `http://{{ .Release.Name }}-provisioning:8080`)
- `PROVISIONING_API_TIMEOUT` env var
- `wait-for-provisioning` init container (wait for provisioning service health endpoint)

### 7.3 ws-server Helm Chart

**Modified files**: `deployments/helm/odin/charts/ws-server/values.yaml`, `templates/deployment.yaml`

Same changes as gateway:
- Remove: `PROVISIONING_DATABASE_URL`, `PROVISIONING_DB_*` env vars, `wait-for-postgres`
- Add: `PROVISIONING_API_URL`, `PROVISIONING_API_TIMEOUT`, `wait-for-provisioning`

### 7.4 Provisioning Dockerfile

**Modified file**: `ws/build/provisioning/Dockerfile`

No CGO changes needed (`modernc.org/sqlite` is pure Go, CGO_ENABLED=0 works).

Add volume for SQLite data:
```dockerfile
VOLUME ["/data"]
```

### 7.5 CLI Dockerfile

**New file**: `ws/build/cli/Dockerfile`

Same multi-stage pattern as other services. Builds `odin` binary. No server — just the CLI binary for use as a tool image or local install.

---

## Phase 8: Testing

**Goal**: Comprehensive test coverage for all new code.

### 8.1 Internal API Handlers

**New file**: `ws/internal/provisioning/api/internal_handlers_test.go`

- Test each endpoint with mock stores
- Verify JSON response format
- Test error cases (tenant not found, invalid params)
- Test query parameter parsing

### 8.2 API Client

**New file**: `ws/internal/shared/provapi/client_test.go`

- Test HTTP client against httptest server
- Test retry logic
- Test error mapping
- Test timeout handling

### 8.3 API-Backed Registries

**New files**:
- `ws/internal/shared/provapi/key_registry_test.go`
- `ws/internal/shared/provapi/tenant_registry_test.go`
- `ws/internal/shared/provapi/topic_registry_test.go`

- Test cache hit/miss behavior
- Test background refresh
- Test TTL expiration
- Test concurrent access

### 8.4 Config Store

**New files**:
- `ws/internal/provisioning/configstore/parser_test.go`
- `ws/internal/provisioning/configstore/validator_test.go`
- `ws/internal/provisioning/configstore/stores_test.go`
- `ws/internal/provisioning/configstore/loader_test.go`

- Test YAML parsing (valid, invalid, edge cases)
- Test all validation rules
- Test read operations on in-memory stores
- Test write operations return ErrReadOnlyMode
- Test atomic reload

### 8.5 Database Factory + SQLite Migrations

**New files**:
- `ws/internal/provisioning/repository/factory_test.go`
- `ws/internal/provisioning/repository/migrator_test.go`

- Test SQLite database creation and WAL mode
- Test PostgreSQL database creation (if available)
- Test migration runner (apply, skip already applied, version tracking)
- Test migration rollback on error

### 8.6 SQLite Repository Compatibility

**New file**: `ws/internal/provisioning/repository/sqlite_compat_test.go`

Run the same test suite against SQLite that runs against PostgreSQL. Verify all 7 repository implementations work correctly with both drivers. Use build tags or test flags to select driver.

### 8.7 CLI Commands

**New files**: `ws/cmd/cli/commands/*_test.go`

- Test command flag parsing
- Test output formatting (JSON, table)
- Test error handling for invalid inputs

### 8.8 Integration Tests

**New file**: `ws/internal/provisioning/integration_test.go`

- Full roundtrip: provisioning service (SQLite) → internal API → API client → registries
- Config mode: YAML → provisioning service → internal API → API client
- Hot-reload: modify YAML → SIGHUP → verify new data served

---

## File Change Summary

### New Files (~25)

| File | Purpose |
|------|---------|
| `ws/internal/provisioning/api/internal_handlers.go` | Internal API endpoint handlers |
| `ws/internal/shared/provapi/client.go` | Provisioning API HTTP client |
| `ws/internal/shared/provapi/types.go` | API response types |
| `ws/internal/shared/provapi/key_registry.go` | API-backed auth.KeyRegistry |
| `ws/internal/shared/provapi/tenant_registry.go` | API-backed gateway.TenantRegistry |
| `ws/internal/shared/provapi/topic_registry.go` | API-backed kafka.TenantRegistry |
| `ws/internal/shared/provapi/admin_client.go` | Authenticated client for CLI |
| `ws/internal/provisioning/repository/migrations/sqlite/001_initial.sql` | SQLite initial schema |
| `ws/internal/provisioning/repository/migrations/sqlite/002_oidc_channel_rules.sql` | SQLite OIDC/rules schema |
| `ws/internal/provisioning/repository/embed.go` | Embedded migration files |
| `ws/internal/provisioning/repository/migrator.go` | Auto-migration runner |
| `ws/internal/provisioning/repository/factory.go` | Database factory (driver selection) |
| `ws/internal/provisioning/configstore/types.go` | YAML config file types |
| `ws/internal/provisioning/configstore/parser.go` | YAML parser |
| `ws/internal/provisioning/configstore/validator.go` | Config file validator |
| `ws/internal/provisioning/configstore/stores.go` | In-memory store implementations |
| `ws/internal/provisioning/configstore/loader.go` | Config loader with atomic reload |
| `ws/cmd/cli/main.go` | CLI entrypoint |
| `ws/cmd/cli/commands/root.go` | CLI root command |
| `ws/cmd/cli/commands/tenant.go` | Tenant commands |
| `ws/cmd/cli/commands/key.go` | Key commands |
| `ws/cmd/cli/commands/category.go` | Category commands |
| `ws/cmd/cli/commands/quota.go` | Quota commands |
| `ws/cmd/cli/commands/oidc.go` | OIDC commands |
| `ws/cmd/cli/commands/rules.go` | Channel rules commands |
| `ws/cmd/cli/commands/config.go` | Config file commands |
| `ws/cmd/cli/commands/output.go` | Output formatting |
| `ws/build/cli/Dockerfile` | CLI Docker build |
| `deployments/helm/odin/charts/provisioning/templates/pvc.yaml` | SQLite PVC |

### Modified Files (~15)

| File | Changes |
|------|---------|
| `ws/go.mod` | Add modernc.org/sqlite, spf13/cobra, promote yaml.v3 |
| `ws/internal/shared/platform/provisioning_config.go` | Add PROVISIONING_MODE, DATABASE_DRIVER, DATABASE_PATH, AUTO_MIGRATE, PROVISIONING_CONFIG_PATH |
| `ws/internal/shared/platform/gateway_config.go` | Remove DB fields, add PROVISIONING_API_URL |
| `ws/internal/shared/platform/server_config.go` | Remove DB fields, add PROVISIONING_API_URL |
| `ws/cmd/provisioning/main.go` | Database factory, config mode branching, SIGHUP handler |
| `ws/cmd/gateway/main.go` | Remove DB import (if gateway has its own main) |
| `ws/internal/gateway/gateway.go` | Remove DB setup, use API-backed registries |
| `ws/cmd/server/main.go` | Remove DB setup, use API-backed topic registry |
| `ws/internal/provisioning/api/router.go` | Add /internal/v1/ route group |
| `ws/build/provisioning/Dockerfile` | Add VOLUME for SQLite |
| `deployments/helm/odin/charts/provisioning/values.yaml` | New config fields |
| `deployments/helm/odin/charts/provisioning/templates/deployment.yaml` | New env vars, conditional DB, PVC |
| `deployments/helm/odin/charts/ws-gateway/values.yaml` | Remove DB, add API URL |
| `deployments/helm/odin/charts/ws-gateway/templates/deployment.yaml` | Remove DB env, add API URL env |
| `deployments/helm/odin/charts/ws-server/values.yaml` | Remove DB, add API URL |
| `deployments/helm/odin/charts/ws-server/templates/deployment.yaml` | Remove DB env, add API URL env |

### Test Files (~12)

| File | Coverage |
|------|----------|
| `ws/internal/provisioning/api/internal_handlers_test.go` | Internal API handlers |
| `ws/internal/shared/provapi/client_test.go` | HTTP client |
| `ws/internal/shared/provapi/key_registry_test.go` | API-backed key registry |
| `ws/internal/shared/provapi/tenant_registry_test.go` | API-backed tenant registry |
| `ws/internal/shared/provapi/topic_registry_test.go` | API-backed topic registry |
| `ws/internal/provisioning/configstore/parser_test.go` | YAML parser |
| `ws/internal/provisioning/configstore/validator_test.go` | Config validator |
| `ws/internal/provisioning/configstore/stores_test.go` | In-memory stores |
| `ws/internal/provisioning/configstore/loader_test.go` | Config loader + reload |
| `ws/internal/provisioning/repository/factory_test.go` | Database factory |
| `ws/internal/provisioning/repository/migrator_test.go` | Migration runner |
| `ws/internal/provisioning/repository/sqlite_compat_test.go` | SQLite repo compat |
| `ws/cmd/cli/commands/*_test.go` | CLI commands |

**Total**: ~25 new files + ~15 modified files + ~12 test files = **~52 files**

---

## Phase Dependencies

```
Phase 1 (Internal API + Client)
    ├──→ Phase 2 (Gateway Refactor)
    ├──→ Phase 3 (ws-server Refactor)
    └──→ Phase 6 (CLI Tool)

Phase 2 + Phase 3 (DB decoupling complete)
    └──→ Phase 4 (Embedded SQLite)

Phase 4 (SQLite ready)
    └──→ Phase 5 (Config Mode)

Phase 1-6 (all code complete)
    └──→ Phase 7 (Helm + Docker)

Phase 1-7 (all implementation)
    └──→ Phase 8 (Testing — iterative throughout)
```

Notes:
- Phases 2 and 3 can be parallelized (gateway and ws-server are independent)
- Phase 6 (CLI) can be parallelized with Phases 2-5 (depends only on Phase 1)
- Phase 8 (testing) is listed last but should be done iteratively per phase

---

## Verification Steps

### Per-Phase Verification

**Phase 1**: `curl http://localhost:8080/internal/v1/keys/active` returns active keys JSON. `curl http://localhost:8080/internal/v1/topics/discovery?namespace=local` returns discovery response.

**Phase 2**: Gateway starts without `PROVISIONING_DATABASE_URL`. Gateway authenticates connections using keys from provisioning API. OIDC validation works via API-backed tenant registry.

**Phase 3**: ws-server starts without `PROVISIONING_DATABASE_URL`. `MultiTenantConsumerPool` discovers topics via provisioning API. Topic refresh loop logs successful refreshes.

**Phase 4**: Provisioning service starts with no `DATABASE_URL` → creates `odin.db` SQLite file. All provisioning API operations work. `PRAGMA journal_mode` returns `wal`.

**Phase 5**: Provisioning service starts with `PROVISIONING_MODE=config` and `PROVISIONING_CONFIG_PATH=tenants.yaml`. API serves data from YAML. `kill -HUP $PID` reloads config. Write operations return 405.

**Phase 6**: `odin tenant list --api-url http://localhost:8080` returns tenant table. `odin config validate --file tenants.yaml` validates file. `odin config init --tenant test` generates template.

**Phase 7**: `helm install` deploys with SQLite by default. Gateway and ws-server connect to provisioning API. No `wait-for-postgres` init containers when using SQLite.

### Full Integration Verification

```bash
# Start provisioning with SQLite (default)
PROVISIONING_MODE=api ./odin-provisioning

# Create tenant via CLI
odin tenant create --id acme --name "Acme Corp" --category trade

# Verify gateway connects via API
PROVISIONING_API_URL=http://localhost:8080 ./odin-gateway

# Verify ws-server discovers topics via API
PROVISIONING_API_URL=http://localhost:8080 ./odin-ws-server

# Test config mode
odin config export --file tenants.yaml
PROVISIONING_MODE=config PROVISIONING_CONFIG_PATH=tenants.yaml ./odin-provisioning
```
