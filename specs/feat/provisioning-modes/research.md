# Research: Provisioning Modes

**Branch**: `feat/provisioning-modes` | **Date**: 2026-02-27

## R1. Current Repository Layer and Database Abstraction

**Question**: What database abstractions exist, and can the repository code be shared between PostgreSQL and SQLite?

**Finding**: The provisioning service uses a clean interface-based repository pattern. Seven store interfaces in `provisioning/interfaces.go` (283 lines):

| Interface | Methods | PostgreSQL Implementation |
|-----------|---------|--------------------------|
| `TenantStore` | Ping, Create, Get, Update, List, UpdateStatus, SetDeprovisionAt, GetTenantsForDeletion | `repository/tenant.go` (364 lines) |
| `KeyStore` | Create, Get, ListByTenant, Revoke, RevokeAllForTenant, GetActiveKeys | `repository/key.go` (235 lines) |
| `TopicStore` | Create, ListByTenant, MarkDeleted, CountByTenant, CountPartitionsByTenant | `repository/topic.go` (163 lines) |
| `QuotaStore` | Get, Create, Update | `repository/quota.go` (114 lines) |
| `AuditStore` | Log, ListByTenant | `repository/audit.go` (139 lines) |
| `OIDCConfigStore` | Create, Get, GetByIssuer, Update, Delete, ListEnabled | `repository/oidc.go` (258 lines) |
| `ChannelRulesStore` | Create, Get, GetRules, Update, Delete, List | `repository/channel_rules.go` (173 lines) |

All implementations use `*sql.DB` with `$1` parametrized queries via `database/sql`. The service layer (`service.go`, 710 lines) depends on interfaces, not implementations. Constructor pattern: `NewPostgresXxxRepository(db *sql.DB)`.

**Decision**: **Shared repository code with a dialect adapter.** The existing repositories use standard SQL through `database/sql`. `modernc.org/sqlite` supports `$1` parameter style. Most queries are portable — only DDL (migrations) and a few database functions differ. A thin dialect layer handles migration files and database-specific functions. The repositories themselves remain unchanged.

**Alternative considered**: Separate SQLite repository implementations. Rejected — would double the maintenance burden for nearly identical SQL.

## R2. PostgreSQL-Specific SQL and SQLite Compatibility

**Question**: What PostgreSQL-specific features are used in the schema and queries, and how do they map to SQLite?

**Finding**: Two migration files (`001_initial.sql`, `002_oidc_channel_rules.sql`) use these PostgreSQL features:

| PostgreSQL Feature | Usage | SQLite Equivalent |
|-------------------|-------|-------------------|
| `CREATE TYPE ... AS ENUM` | `tenant_status`, `consumer_type` | `TEXT` + `CHECK(col IN (...))` |
| `SERIAL` / `BIGSERIAL` | Auto-increment PKs (`tenant_categories.id`, `provisioning_audit.id`) | `INTEGER PRIMARY KEY` (auto-increments) |
| `JSONB` | `metadata`, `details`, `rules` columns | `TEXT` (store JSON string, marshal/unmarshal in Go) |
| `INET` | `ip_address` in audit table | `TEXT` |
| `id ~ '^[a-z]...'` | Regex CHECK constraints on IDs | Omit from DDL, validate in Go (already validated at service layer) |
| `NOW()` | Default timestamps, trigger functions | `datetime('now')` in DDL; pass `time.Now()` as parameter in queries |
| `ON CONFLICT ... DO NOTHING RETURNING id` | Upsert in `topic.go` | Supported (SQLite 3.35.0+ via modernc.org/sqlite) |
| `ON CONFLICT ... DO UPDATE SET` | Upsert in `channel_rules.go`, `oidc.go` | Supported (SQLite 3.24.0+) |
| Partial indexes (`WHERE` clause) | `idx_tenant_keys_tenant_active`, `idx_tenant_oidc_issuer_enabled` | Supported (SQLite 3.8.0+) |
| Trigger functions | Auto-update `updated_at` timestamps | Supported but different syntax (no `CREATE FUNCTION`, inline trigger body) |
| `CHECK` constraints | Quota fields, URL patterns | Supported |

**Critical compatibility point**: The repository queries in Go code use `$1, $2` parameters and standard SQL operations (`SELECT`, `INSERT`, `UPDATE`, `DELETE`). No PostgreSQL-specific query syntax in the repository layer — all dialect differences are in DDL only.

**Decision**: Separate migration files per driver. Same repository code. The DDL is different enough (ENUMs, triggers, data types) that maintaining two sets of migration SQL files is cleaner than trying to make one set work for both.

## R3. Migration Strategy

**Question**: How do migrations run today, and what approach works for embedded SQLite?

**Finding**: Migrations live at `repository/migrations/` with two SQL files and an `atlas.sum` checksum file. Atlas is the migration tool (external runner — no in-code migration logic). Migrations are run externally via `atlas migrate apply` or similar. The Helm deployment uses an init container (`wait-for-postgres`) that waits for the database but does not run migrations.

**Decision**: **Embedded auto-migration on startup.** For simple/SQLite deployments, the provisioning service embeds migration files via Go 1.16+ `embed` package and runs them automatically at startup. A `schema_migrations` table tracks applied versions. PostgreSQL users can still use Atlas externally (opt-in via `AUTO_MIGRATE=false`).

File structure:
```
repository/
├── migrations/
│   ├── postgres/
│   │   ├── 001_initial.sql
│   │   └── 002_oidc_channel_rules.sql
│   └── sqlite/
│       ├── 001_initial.sql
│       └── 002_oidc_channel_rules.sql
├── embed.go        # //go:embed for migration files
└── migrator.go     # Auto-migration runner
```

Existing PostgreSQL migrations move from `migrations/` to `migrations/postgres/`. Atlas users update their atlas.hcl path. The embedded migrator reads files from the appropriate subdirectory based on the configured driver.

**Alternative considered**: `golang-migrate/migrate` library. Rejected — adds a dependency for simple file-based migration logic. A lightweight custom runner (~50 lines) suffices.

## R4. Gateway and ws-server Database Coupling

**Question**: Where exactly do the gateway and ws-server access the provisioning database, and what needs to change?

**Finding**: Five direct database coupling points, all of which must migrate to provisioning API calls:

### 4.1 Gateway Key Cache (`auth/keys_postgres.go`)
- **Struct**: `PostgresKeyRegistry` — holds `*sql.DB`, background ticker (1min refresh), `sync.RWMutex` + in-memory maps
- **3 SQL queries**: Full cache load (JOIN `tenant_keys` + `tenants`), single key fetch by `key_id`, keys by `tenant_id`
- **Interface**: `auth.KeyRegistry` — `GetKey()`, `GetKeysByTenant()`, `Close()`
- **Extended**: `auth.KeyRegistryWithRefresh` — adds `Refresh()`, `Stats()`
- **Wired in**: `gateway/gateway.go:119-130` via `auth.NewPostgresKeyRegistry()`
- **Config**: `PROVISIONING_DATABASE_URL`, `KEY_CACHE_REFRESH_INTERVAL` (1min), `KEY_CACHE_QUERY_TIMEOUT` (5s)

### 4.2 Gateway Tenant Registry (`gateway/tenant_registry_postgres.go`)
- **Struct**: `PostgresTenantRegistry` — holds `*sql.DB`, 3 TTL-based lazy caches with `sync.RWMutex`
- **3 SQL queries**: Issuer→tenant lookup (`tenant_oidc_config`), OIDC config by tenant, channel rules by tenant (`tenant_channel_rules`)
- **Interface**: `gateway.TenantRegistry` — `GetTenantByIssuer()`, `GetOIDCConfig()`, `GetChannelRules()`, `Close()`
- **Wired in**: `gateway/gateway.go:140-147` inside `setupMultiIssuerOIDC()`
- **Config**: `GATEWAY_ISSUER_CACHE_TTL` (5min), `GATEWAY_CHANNEL_RULES_CACHE_TTL` (1min)

### 4.3 Gateway Multi-Issuer OIDC (`gateway/multi_issuer_oidc.go`)
- **Struct**: `MultiIssuerOIDC` — holds `gateway.TenantRegistry` reference, per-issuer JWKS keyfunc cache (1hr TTL)
- **Transitive dependency**: Calls `registry.GetTenantByIssuer()` and `registry.GetOIDCConfig()` to build keyfuncs
- **Adapts automatically** when TenantRegistry switches from DB-backed to API-backed

### 4.4 ws-server Topic Discovery (`provisioning/topic_registry.go` + `orchestration/multitenant_pool.go`)
- **Struct**: `provisioning.TopicRegistry` — holds `*sql.DB`
- **2 SQL queries**: Shared tenant topics (JOIN `tenants` + `tenant_categories`), dedicated tenants + categories
- **Interface**: `kafka.TenantRegistry` — `GetSharedTenantTopics()`, `GetDedicatedTenants()`
- **Used by**: `MultiTenantConsumerPool.refreshTopics()` via background ticker (60s)
- **Wired in**: `cmd/server/main.go:208` via `provisioning.NewTopicRegistry(provisioningDB)`
- **Config**: `PROVISIONING_DATABASE_URL`, `TOPIC_REFRESH_INTERVAL` (60s)

### 4.5 Kafka Producer Topic Cache (`kafka/producer.go`)
- **Field**: `tenantRegistry kafka.TenantRegistry` — optional, for publish-time topic validation
- **Lazy TTL cache**: `provisionedTopics map[string]bool`, 30s TTL
- **Calls**: `registry.GetSharedTenantTopics()` and `registry.GetDedicatedTenants()` on cache miss
- **Note**: Currently the producer in ws-server does NOT pass a registry (field is nil). Topic validation is done elsewhere. This coupling point is dormant but should still be adapted.

### 4.6 Database Connection Pools
- **Gateway**: Single pool (`gateway/gateway.go:96-116`) — shared for keys + tenant registry + OIDC
- **ws-server**: Separate pool (`cmd/server/main.go:185-205`) — just for topic discovery

**Decision**: Replace all 5 coupling points with API-backed implementations that call the provisioning service's REST API. The existing interfaces (`auth.KeyRegistry`, `gateway.TenantRegistry`, `kafka.TenantRegistry`) remain — only the implementations change. Caching layers (TTL, background refresh) are preserved to avoid excessive API calls.

## R5. Internal API Endpoints

**Question**: What new API endpoints does the provisioning service need for gateway/ws-server consumption?

**Finding**: Existing endpoints that partially cover the need:
- `GET /api/v1/keys/active` — Returns all active keys (requires `system` role). Already exists.
- `GET /api/v1/tenants/{id}/oidc` — Returns OIDC config (requires auth + tenant access).
- `GET /api/v1/tenants/{id}/channel-rules` — Returns channel rules (requires auth + tenant access).

Missing endpoints:
- Tenant lookup by OIDC issuer URL (no existing endpoint)
- Topic discovery for shared/dedicated consumers (no existing endpoint)

**Decision**: **Add internal API route group `/internal/v1/` without auth.** Service-to-service communication within a Kubernetes cluster uses internal service DNS (e.g., `http://provisioning:8080`). No JWT auth overhead — network-level isolation is sufficient. The CLI and external clients continue using `/api/v1/` with auth.

New internal endpoints:

| Endpoint | Consumer | Data Source |
|----------|----------|-------------|
| `GET /internal/v1/keys/active` | Gateway KeyRegistry | `KeyStore.GetActiveKeys()` |
| `GET /internal/v1/tenants/by-issuer?issuer_url=...` | Gateway TenantRegistry | `OIDCConfigStore.GetByIssuer()` |
| `GET /internal/v1/tenants/{id}/oidc` | Gateway TenantRegistry | `OIDCConfigStore.Get()` |
| `GET /internal/v1/tenants/{id}/channel-rules` | Gateway TenantRegistry | `ChannelRulesStore.GetRules()` |
| `GET /internal/v1/topics/discovery?namespace=...` | ws-server TopicRegistry | `TopicStore` + `TenantStore` |

**Alternative considered**: Reusing `/api/v1/` with a service token. Rejected — adds token management complexity, JWT validation overhead on every request, and couples service-to-service auth to the same mechanism as external clients.

## R6. API Client Package Design

**Question**: Where should the provisioning API client live, and what should it expose?

**Finding**: No existing API client code in the codebase. The gateway and ws-server access data directly via `*sql.DB`.

**Decision**: Create `ws/internal/shared/provapi/` package containing:

- `client.go` — HTTP client with retry, timeout, health check
- `key_registry.go` — Implements `auth.KeyRegistry` + `auth.KeyRegistryWithRefresh` via API calls
- `tenant_registry.go` — Implements `gateway.TenantRegistry` via API calls
- `topic_registry.go` — Implements `kafka.TenantRegistry` via API calls

The package name `provapi` is concise and avoids collision with the existing `provisioning` package at `internal/provisioning/`.

Each implementation preserves the same caching patterns:
- `APIKeyRegistry`: Background ticker refresh (same as `PostgresKeyRegistry`)
- `APITenantRegistry`: TTL-based lazy caches (same as `PostgresTenantRegistry`)
- `APITopicRegistry`: Called directly by `MultiTenantConsumerPool` (pool already has its own refresh loop)

## R7. Config Mode Data Layer

**Question**: How should config mode store and serve provisioning data without a database?

**Finding**: The service layer (`service.go`) depends on 7 Store interfaces + 1 KafkaAdmin interface. In config mode, all data comes from a YAML file. Write operations (Create, Update, Delete) are not supported — the YAML file is the source of truth.

**Decision**: **In-memory Store implementations backed by parsed YAML.** A new package `provisioning/configstore/` provides:

- `parser.go` — YAML parsing into typed Go structs
- `validator.go` — Validation using the same rules as the REST API
- `stores.go` — In-memory implementations of all 7 Store interfaces
- `loader.go` — Orchestrates parse → validate → create stores; supports atomic reload

Write methods return `ErrReadOnlyMode`. Read methods serve data from in-memory maps protected by `sync.RWMutex`. Hot-reload via SIGHUP atomically swaps the in-memory data (parse new → validate → swap pointer).

**Alternative considered**: Loading YAML into an in-memory SQLite database. Rejected — adds unnecessary complexity. The in-memory map approach is simpler, faster, and avoids SQL for what is essentially a static data lookup.

## R8. YAML Config File Schema

**Question**: What should the config file format look like?

**Finding**: All 7 provisioning entities need representation. The existing `config/tenants.yaml` is a reference file (not loaded in code) with a different structure.

**Decision**: Flat tenant-centric YAML schema where each tenant contains all its nested entities:

```yaml
tenants:
  - id: "acme"
    name: "Acme Corp"
    consumer_type: "shared"
    metadata: {}
    categories:
      - name: "trade"
        partitions: 3
        retention_ms: 604800000
      - name: "analytics"
    keys:
      - id: "acme-key-1"
        algorithm: "ES256"
        public_key: |
          -----BEGIN PUBLIC KEY-----
          ...
          -----END PUBLIC KEY-----
        expires_at: "2027-01-01T00:00:00Z"
    quotas:
      max_topics: 50
      max_partitions: 200
      max_storage_bytes: 10737418240
      producer_byte_rate: 10485760
      consumer_byte_rate: 52428800
    oidc:
      issuer_url: "https://auth.acme.com"
      jwks_url: "https://auth.acme.com/.well-known/jwks.json"
      audience: "odin"
      enabled: true
    channel_rules:
      public: ["*.metadata", "*.status"]
      group_mappings:
        traders: ["*.trade.*"]
        analysts: ["*.analytics.*"]
      default: ["*.public"]
```

Validation rules match the service layer: tenant ID format, algorithm enum, PEM format, at least one category per tenant, HTTPS issuer URLs, no duplicate tenant IDs, no duplicate key IDs.

## R9. Hot-Reload Mechanism

**Question**: How should SIGHUP hot-reload work in config mode?

**Finding**: The provisioning service already handles signals for graceful shutdown (`SIGTERM`, `SIGINT`) in `cmd/provisioning/main.go:255-262`. No existing SIGHUP handler.

**Decision**: Register a SIGHUP handler in `main.go` (config mode only). On signal:

1. Re-read YAML file from `PROVISIONING_CONFIG_PATH`
2. Parse and validate (same validator as startup)
3. If valid: atomically swap in-memory stores (pointer swap under write lock)
4. If invalid: log validation errors, keep previous config, continue serving
5. Log reload outcome (success with tenant count, or failure with errors)

The swap is atomic from the API perspective — ongoing requests see either the old or new data, never a partial state. New requests after the swap see the new data immediately.

## R10. CLI Framework

**Question**: Which CLI framework should the `odin` binary use?

**Finding**: No CLI framework currently in `go.mod`. The codebase uses `flag` package only for a debug flag in `cmd/provisioning/main.go`.

**Decision**: **`spf13/cobra`** — standard for Go CLI tools (kubectl, docker, gh all use it). Provides:
- Nested subcommands (`odin tenant create`, `odin key list`)
- Built-in help generation
- Flag parsing with env var fallback
- Shell completion

The CLI reuses the `provapi.Client` HTTP client from R6 for all API communication. Output formatting (JSON/table) is handled by a shared formatter.

## R11. Existing Patterns to Reuse

| Pattern | Location | Reuse In |
|---------|----------|----------|
| `auth.StaticKeyRegistry` | `shared/auth/keys.go:166-226` | Reference for in-memory KeyRegistry in config store |
| `auth.KeyRegistry` interface | `shared/auth/keys.go:72-95` | API-backed implementation |
| `gateway.TenantRegistry` interface | `gateway/interfaces.go:13-28` | API-backed implementation |
| `kafka.TenantRegistry` interface | `shared/kafka/tenant_registry.go:14-40` | API-backed implementation |
| `httputil.WriteJSON/WriteError` | `shared/httputil/` | Internal API handlers |
| `chi/v5` router patterns | `provisioning/api/router.go` | Internal API route group |
| `caarlos0/env` struct tags | `shared/platform/` | New config fields |
| `logging.RecoverPanic()` | Used in all goroutines | New background goroutines |
| Noop KafkaAdmin fallback | `cmd/provisioning/main.go:93-140` | Config mode: noop KafkaAdmin when no brokers |
