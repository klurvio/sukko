# Research: Provisioning Modes

**Branch**: `feat/provisioning-modes` | **Date**: 2026-02-28 (updated)

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

**Critical compatibility point**: Repository queries in Go use `$1, $2` parameters and standard SQL. No PostgreSQL-specific query syntax in the repository layer — all dialect differences are in DDL only.

**Decision**: Separate migration files per driver. Same repository code. The DDL is different enough that maintaining two sets of migration SQL files is cleaner than trying to make one set work for both.

## R3. Migration Strategy

**Question**: How do migrations run today, and what approach works for embedded SQLite?

**Finding**: Migrations live at `repository/migrations/` with two SQL files and an `atlas.sum` checksum file. Atlas is the external migration tool. The Helm deployment uses an init container (`wait-for-postgres`) that waits for the database but does not run migrations.

**Decision**: **Embedded auto-migration on startup.** The provisioning service embeds migration files via Go 1.16+ `embed` package and runs them at startup. A `schema_migrations` table tracks applied versions. PostgreSQL users can still use Atlas externally (opt-in via `AUTO_MIGRATE=false`).

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

## R4. Gateway and ws-server Database Coupling

**Question**: Where exactly do the gateway and ws-server access the provisioning database, and what needs to change?

**Finding**: Five direct database coupling points, all verified accurate:

1. **Gateway Key Cache** (`auth/keys_postgres.go`) — `PostgresKeyRegistry`, background ticker (1min), `sync.RWMutex` + maps, 3 SQL queries
2. **Gateway Tenant Registry** (`gateway/tenant_registry_postgres.go`) — `PostgresTenantRegistry`, 3 TTL-based lazy caches, 3 SQL queries
3. **Gateway Keyfunc Cache** (`gateway/multi_issuer_oidc.go`) — `MultiIssuerOIDC`, depends on TenantRegistry transitively
4. **ws-server Topic Discovery** (`provisioning/topic_registry.go`) — `TopicRegistry`, used by `MultiTenantConsumerPool.refreshTopics()` via 60s ticker
5. **Kafka Producer Topic Cache** (`kafka/producer.go`) — optional `tenantRegistry` field, 30s TTL lazy cache

**Decision**: Replace all 5 with gRPC streaming subscribers that receive real-time updates from the provisioning service. Caching layers remain (in-memory maps) but are updated via stream events instead of polling/DB queries.

## R5. gRPC + Protobuf Architecture

**Question**: How should internal service-to-service communication work?

**Decision**: **gRPC with protobuf for internal, REST for external.**

- Provisioning service runs two listeners: gRPC (port 9090) + HTTP (port 8080)
- Gateway and ws-server connect via gRPC streaming
- CLI and admin UI use REST API
- Proto files in `ws/proto/odin/provisioning/v1/`
- Code generation via `buf generate`
- Dependencies: `google.golang.org/grpc`, `google.golang.org/protobuf`, `buf.build/gen/go/...`

gRPC services:
```protobuf
service ProvisioningInternal {
  rpc WatchKeys(WatchKeysRequest) returns (stream KeysSnapshot);
  rpc WatchTenantConfig(WatchTenantConfigRequest) returns (stream TenantConfigSnapshot);
  rpc WatchTopics(WatchTopicsRequest) returns (stream TopicsSnapshot);
}
```

Each stream: sends full snapshot on connect → pushes deltas on change → client reconnects with backoff on disconnect.

## R6. Event Bus for Change Detection

**Question**: How does the provisioning service detect changes to push via gRPC streams?

**Decision**: **In-process event bus.** The service layer emits events after successful writes. gRPC stream handlers subscribe and push to connected clients.

Implementation: Simple channel-based pub/sub within the process. Event types: `KeysChanged`, `TenantConfigChanged`, `TopicsChanged`. Config mode emits events on SIGHUP reload. No external dependencies.

## R7. Stream Resilience

**Question**: How do gateway/ws-server handle provisioning service unavailability?

**Decision**: **Circuit breaker with health degradation.**

- Stream disconnect → reconnect with exponential backoff (1s initial, 30s max, jitter)
- During disconnect: serve stale cached data, report degraded health
- Metrics: `provisioning_stream_state` gauge, `provisioning_stream_reconnects_total` counter
- Auto-recover when stream reconnects (fresh snapshot)

## R8. YAML Config File Schema

Same as original research — flat tenant-centric YAML schema.

## R9. Hot-Reload Mechanism

SIGHUP handler in config mode. On signal: re-read YAML → parse → validate → atomically swap in-memory stores → emit events via event bus → gRPC streams push updates to subscribers.

## R10. CLI Framework

**`spf13/cobra`** — standard for Go CLI tools. CLI uses REST API (`/api/v1/`) with optional bearer token auth.

## R11. Existing Patterns to Reuse

| Pattern | Location | Reuse In |
|---------|----------|----------|
| `auth.StaticKeyRegistry` | `shared/auth/keys.go:166-226` | Reference for in-memory KeyRegistry |
| `auth.KeyRegistry` interface | `shared/auth/keys.go:70-84` | gRPC-backed implementation |
| `gateway.TenantRegistry` interface | `gateway/interfaces.go:18-35` | gRPC-backed implementation |
| `kafka.TenantRegistry` interface | `shared/kafka/tenant_registry.go:14-40` | gRPC-backed implementation |
| `httputil.WriteJSON/WriteError` | `shared/httputil/` | REST API handlers |
| `chi/v5` router patterns | `provisioning/api/router.go` | REST API (unchanged) |
| `caarlos0/env` struct tags | `shared/platform/` | New config fields |
| `logging.RecoverPanic()` | Used in all goroutines | gRPC stream goroutines |
| `sony/gobreaker/v2` | Already in `go.mod` | Stream reconnection circuit breaker |
| Noop KafkaAdmin | `cmd/provisioning/main.go:93-140` | Config mode: noop KafkaAdmin |
