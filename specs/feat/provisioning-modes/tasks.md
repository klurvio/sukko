# Tasks: Provisioning Modes

**Branch**: `feat/provisioning-modes` | **Generated**: 2026-02-28

---

## Phase 1: Config — Environment Variables, Helm Values, Proto Setup

> Dependencies: None. This phase establishes all configuration and protobuf contracts.

- [x] T001 Add `GRPC_PORT`, `PROVISIONING_MODE`, `DATABASE_DRIVER`, `DATABASE_PATH`, `AUTO_MIGRATE`, `PROVISIONING_CONFIG_PATH`, `PROVISIONING_ADMIN_TOKEN` env vars to provisioning config struct at `ws/internal/shared/platform/provisioning_config.go`. Change `DATABASE_URL` from required to optional. Add `AdminToken string` (`env:"PROVISIONING_ADMIN_TOKEN"`). Add validation: `DATABASE_DRIVER` in `{sqlite, postgres}`, `PROVISIONING_MODE` in `{api, config}`, `DATABASE_URL` required only when `DATABASE_DRIVER=postgres`, `PROVISIONING_CONFIG_PATH` required only when `PROVISIONING_MODE=config`. If `AdminToken` is set and `len < 16` and `Environment != "dev"`, return startup error. If `len < 16` and `Environment == "dev"`, log WARNING. `LogConfig()` MUST print `"admin_token": "[REDACTED]"` when set. Update `Validate()`, `Print()`, and `LogConfig()` methods.

- [x] T002 Replace gateway DB config with gRPC config at `ws/internal/shared/platform/gateway_config.go`. Remove: `ProvisioningDBURL`, `DBMaxOpenConns`, `DBMaxIdleConns`, `DBConnMaxLifetime`, `DBConnMaxIdleTime`, `DBPingTimeout`, `KeyCacheRefreshInterval`, `KeyCacheQueryTimeout`, `IssuerCacheTTL`, `ChannelRulesCacheTTL`. Add: `ProvisioningGRPCAddr string` (`env:"PROVISIONING_GRPC_ADDR" envDefault:"localhost:9090"`), `GRPCReconnectDelay time.Duration` (`env:"PROVISIONING_GRPC_RECONNECT_DELAY" envDefault:"1s"`), `GRPCReconnectMaxDelay time.Duration` (`env:"PROVISIONING_GRPC_RECONNECT_MAX_DELAY" envDefault:"30s"`). Retain `OIDCKeyfuncCacheTTL`, `JWKSFetchTimeout`, `JWKSRefreshInterval`. Update `Validate()`.

- [x] T003 Replace ws-server DB config with gRPC config at `ws/internal/shared/platform/server_config.go`. Remove: `ProvisioningDatabaseURL`, `ProvisioningDBMaxOpenConns`, `ProvisioningDBMaxIdleConns`, `ProvisioningDBConnMaxLifetime`, `ProvisioningDBConnMaxIdleTime`. Add: `ProvisioningGRPCAddr string` (`env:"PROVISIONING_GRPC_ADDR" envDefault:"localhost:9090"`), `GRPCReconnectDelay time.Duration` (`env:"PROVISIONING_GRPC_RECONNECT_DELAY" envDefault:"1s"`), `GRPCReconnectMaxDelay time.Duration` (`env:"PROVISIONING_GRPC_RECONNECT_MAX_DELAY" envDefault:"30s"`). Update `Validate()`.

- [x] T004 Create buf configuration files: `ws/buf.yaml` (v2 config, module path `proto`, STANDARD lint rules, FILE breaking rules) and `ws/buf.gen.yaml` (v2 config, protocolbuffers/go and grpc/go plugins, output to `gen/proto`, `paths=source_relative`).

- [x] T005 Create protobuf definitions at `ws/proto/sukko/provisioning/v1/provisioning.proto`. Define `ProvisioningInternal` service with 3 streaming RPCs: `WatchKeys`, `WatchTenantConfig`, `WatchTopics`. Define all messages: `WatchKeysRequest`, `KeysUpdate` (is_snapshot, repeated KeyInfo, repeated removed_key_ids), `KeyInfo` (key_id, tenant_id, algorithm, public_key_pem, is_active, expires_at_unix), `WatchTenantConfigRequest`, `TenantConfigUpdate` (is_snapshot, repeated TenantConfig, repeated removed_tenant_ids), `TenantConfig` (tenant_id, OIDCConfig, ChannelRules), `OIDCConfig`, `ChannelRules` (public_channels, map group_mappings, default_channels), `GroupChannels`, `WatchTopicsRequest` (namespace), `TopicsUpdate` (is_snapshot, shared_topics, repeated DedicatedTenant), `DedicatedTenant`. Use `go_package = "github.com/Toniq-Labs/sukko/gen/proto/sukko/provisioning/v1;provisioningv1"`.

- [x] T006 Run `buf generate` to produce generated Go code at `ws/gen/proto/sukko/provisioning/v1/provisioning.pb.go` and `provisioning_grpc.pb.go`. Add `google.golang.org/grpc`, `google.golang.org/protobuf` to `ws/go.mod`. Verify `buf lint` passes. Add `buf lint` to `.githooks/pre-commit` alongside existing Go and Helm lint checks. Also add `buf lint` to CI if a pipeline config exists (e.g., `.github/workflows/ci.yml`); if no CI config exists yet, skip and note for future setup. Commit generated code.

---

## Phase 2: Code — Go Implementation

> Dependencies: Phase 1 must be complete. Within this phase, tasks are ordered by dependency chain.

### Sub-phase 2A: Event Bus + gRPC Server (provisioning service)

- [x] T007 Create in-process event bus at `ws/internal/provisioning/eventbus/bus.go`. Define `EventType` constants: `KeysChanged`, `TenantConfigChanged`, `TopicsChanged`. Define `Event` struct with `Type` field. Implement `Bus` struct with `sync.RWMutex`, `subscribers map[int]chan Event`, `nextID int`. Methods: `New(logger)`, `Subscribe() (id int, ch <-chan Event)`, `Unsubscribe(id int)`, `Publish(event Event)`. `Publish` must use non-blocking `select` with `default` to skip slow subscribers per Constitution VII fan-out rules. Subscriber channels buffered (capacity 16). Add Prometheus counter `provisioning_event_bus_events_total` (by type) and `provisioning_event_bus_dropped_total` (by subscriber).

- [x] T008 Create event bus test at `ws/internal/provisioning/eventbus/bus_test.go`. Table-driven tests: publish/subscribe, non-blocking fan-out with slow subscriber, unsubscribe stops delivery, concurrent publish/subscribe safety, multiple event types.

- [x] T009 Wire event bus into provisioning service at `ws/internal/provisioning/service.go`. Add `EventBus *eventbus.Bus` to `ServiceConfig` and `Service` structs. Add `emitEvent(eventType)` helper (nil-guarded). Call `emitEvent()` after successful writes: `CreateTenant`/`UpdateTenant`/`SuspendTenant`/`ReactivateTenant` → `TopicsChanged`+`TenantConfigChanged`, `CreateKey`/`RevokeKey` → `KeysChanged`, `CreateTopics`/`DeleteTopics` → `TopicsChanged`, `CreateOIDCConfig`/`UpdateOIDCConfig`/`DeleteOIDCConfig` → `TenantConfigChanged`, `SetChannelRules`/`DeleteChannelRules` → `TenantConfigChanged`.

- [x] T010 Create gRPC interceptors at `ws/internal/provisioning/grpcserver/interceptors.go`. Three stream interceptors: (1) recovery — catches panics via `logging.RecoverPanic` pattern, returns `codes.Internal`, (2) logging — zerolog with method name, duration, status code, (3) metrics — Prometheus histogram `provisioning_grpc_request_duration_seconds` (by method), counter `provisioning_grpc_requests_total` (by method, status).

- [x] T010b Create admin token middleware at `ws/internal/provisioning/api/admin_auth.go`. Implement as a struct with lifecycle management (Constitution VII): `type AdminAuth struct { ... }` with constructor `NewAdminAuth(ctx context.Context, wg *sync.WaitGroup, adminToken string, logger zerolog.Logger) *AdminAuth`, method `Middleware() func(http.Handler) http.Handler`, and `Close()` for shutdown. The constructor starts a background cleanup goroutine following Constitution VII lifecycle: accept a `*sync.WaitGroup` parameter, call `wg.Add(1)` before `go`, `defer logging.RecoverPanic()` first, `defer wg.Done()` second, `select` on `ctx.Done()` and cleanup ticker. Middleware logic: if `adminToken` is empty, skip (disabled). Extract `Authorization: Bearer <token>` from header. Compare using `crypto/subtle.ConstantTimeCompare`. On match: set admin context (bypass tenant checks), call next. On mismatch: fall through to existing JWT middleware (do NOT reject — allow JWT path to handle). Add per-IP rate limiting for auth failures: track failures in `sync.Map` of IP → `{count, resetAt}`, after 10 failures within 1 minute return HTTP 429 for 60 seconds. The background cleanup goroutine (every 5 minutes) sweeps entries older than 2× the window (2 minutes) to prevent unbounded memory growth from abandoned IPs. Add metrics: `provisioning_admin_auth_total` counter (by result: `success`, `fallthrough`), `provisioning_admin_auth_rate_limited_total` counter. Wire into router at `ws/internal/provisioning/api/router.go` — insert `adminAuth.Middleware()` before existing `AuthMiddleware` in the middleware chain. Wire `adminAuth.Close()` into provisioning main shutdown path.

- [x] T010c Create admin token middleware test at `ws/internal/provisioning/api/admin_auth_test.go`. Table-driven tests: valid admin token grants access, invalid token falls through (doesn't reject), empty admin token disables middleware, rate limiting after 10 failures, rate limit resets after 60s, constant-time comparison used (no timing leak), admin token not in response body or logs.

- [x] T011 Create gRPC stream handlers at `ws/internal/provisioning/grpcserver/server.go`. Implement `Server` struct embedding `UnimplementedProvisioningInternalServer` with `eventBus`, `service`, `logger` fields. Implement `WatchKeys`: load all active keys from `service.GetActiveKeys()` → send snapshot → subscribe to event bus → loop receiving `KeysChanged` events → reload keys → send delta. Implement `WatchTenantConfig`: load all tenants with OIDC+channel rules → send snapshot → subscribe → loop. Implement `WatchTopics`: build topic discovery from `service.ListTenants()` + categories → send snapshot → subscribe → loop. Each method must: check `stream.Context().Done()` in select, unsubscribe from bus on exit, use `defer logging.RecoverPanic()`.

- [x] T012 Create gRPC server test at `ws/internal/provisioning/grpcserver/server_test.go`. Use `bufconn` for in-memory gRPC. Test: snapshot on connect for each stream, delta after event bus publish, stream cancellation cleanup, concurrent streams.

### Sub-phase 2B: gRPC Stream Registries (gateway + ws-server consumers)

- [x] T013 [P] Create gRPC stream-backed key registry at `ws/internal/shared/provapi/key_registry.go`. Implement `auth.KeyRegistry` and `auth.KeyRegistryWithRefresh`. Struct: `StreamKeyRegistry` with `grpc.ClientConn`, `sync.RWMutex`, `keysByID map`, `keysByTenant map`, `streamState atomic.Int32`, `reconnects atomic.Int64`. Config struct with `GRPCAddr`, `ReconnectDelay`, `ReconnectMaxDelay`, `MetricPrefix`, `Logger`. The `MetricPrefix` field (e.g., `"gateway"` or `"ws"`) ensures metric names comply with Constitution VI: gateway callers pass `"gateway"` → `gateway_provisioning_stream_state`, ws-server callers pass `"ws"` → `ws_provisioning_stream_state`. Constructor dials gRPC, starts stream goroutine (wg.Add before go, RecoverPanic first defer, wg.Done second defer). Stream goroutine: call `WatchKeys()`, receive messages, update maps under write lock. On error: reconnect loop with exponential backoff + jitter, increment `streamState` and `reconnects`. `GetKey()`/`GetKeysByTenant()`: read lock lookup. `Close()`: cancel ctx, wg.Wait. Add metrics: `{prefix}_provisioning_stream_state` gauge, `{prefix}_provisioning_stream_reconnects_total` counter.

- [x] T014 [P] Create gRPC stream-backed tenant registry at `ws/internal/shared/provapi/tenant_registry.go`. Implement `gateway.TenantRegistry`. Struct: `StreamTenantRegistry` with `grpc.ClientConn`, `sync.RWMutex`, `issuerToTenant map`, `oidcConfigs map`, `channelRules map`, `streamState`, `logger`. Config struct includes `MetricPrefix` (same pattern as T013). Same streaming + reconnection pattern as key registry. `GetTenantByIssuer()`, `GetOIDCConfig()`, `GetChannelRules()`: read lock lookups. Return existing sentinel errors (`types.ErrIssuerNotFound`, `types.ErrOIDCNotConfigured`, `types.ErrChannelRulesNotFound`) when not in cache.

- [x] T015 [P] Create gRPC stream-backed topic registry at `ws/internal/shared/provapi/topic_registry.go`. Implement `kafka.TenantRegistry`. Struct: `StreamTopicRegistry` with `grpc.ClientConn`, `sync.RWMutex`, `sharedTopics []string`, `dedicatedTenants []kafka.TenantTopics`, `namespace string`, `onUpdate func()` callback. Config struct includes `MetricPrefix` (same pattern as T013). Same streaming pattern. `GetSharedTenantTopics()`/`GetDedicatedTenants()`: read lock return cached data. When stream receives update and `onUpdate != nil`, call it to notify `MultiTenantConsumerPool`.

- [x] T016 Create stream registry tests at `ws/internal/shared/provapi/key_registry_test.go`, `tenant_registry_test.go`, `topic_registry_test.go`. Use `bufconn` for in-memory gRPC with a mock `ProvisioningInternalServer`. Test: cache populated from snapshot, cache updated from delta, `GetKey`/`GetTenantByIssuer`/`GetSharedTenantTopics` return correct data, reconnection on server disconnect, concurrent reads during stream update.

### Sub-phase 2C: Gateway Refactor

- [x] T017 Refactor gateway to use gRPC registries at `ws/internal/gateway/gateway.go`. In `setupValidator()`: remove `sql.Open("postgres", ...)`, connection pool config, `db.PingContext()`, `auth.NewPostgresKeyRegistry(...)`, `NewPostgresTenantRegistry(...)`. Add: `provapi.NewStreamKeyRegistry(provapi.StreamKeyRegistryConfig{GRPCAddr: gw.config.ProvisioningGRPCAddr, ...})`, `provapi.NewStreamTenantRegistry(...)`. Remove `dbConn *sql.DB` from `Gateway` struct. In `Close()`: remove `gw.dbConn.Close()` (registries close own connections). Update health handler at `/healthz` to check `streamKeyRegistry.State()` and `streamTenantRegistry.State()` — return HTTP 200 with `"status":"degraded"` when any stream is disconnected (instead of 503, to avoid unnecessary restarts), and `"status":"ok"` when all streams connected. Include `provisioning_keys_stream` and `provisioning_config_stream` status fields in the JSON response.

### Sub-phase 2D: ws-server Refactor

- [x] T018 Refactor ws-server to use gRPC topic registry at `ws/cmd/server/main.go`. Remove: `sql.Open("postgres", cfg.ProvisioningDatabaseURL)`, connection pool config, `db.PingContext()`, `provisioning.NewTopicRegistry(provisioningDB)`. Add: `provapi.NewStreamTopicRegistry(provapi.StreamTopicRegistryConfig{GRPCAddr: cfg.ProvisioningGRPCAddr, Namespace: cfg.TopicNamespace, MetricPrefix: "ws", OnUpdate: pool.RefreshTopics, ...})`. Pass to `MultiTenantConsumerPool` as `Registry`. The `OnUpdate` callback wires the stream topic registry to trigger `pool.RefreshTopics()` immediately when topics change (see T015 and T019).

- [x] T019 Add `onUpdate` callback support to `MultiTenantConsumerPool` at `ws/internal/server/orchestration/multitenant_pool.go`. Add `OnTopicUpdate func()` to pool config. When stream topic registry detects changes, it calls this callback to trigger `refreshTopics()` immediately. Keep existing ticker as a 5-minute safety net (change from 60s).

### Sub-phase 2E: Database Factory + SQLite

- [x] T020 Move existing PostgreSQL migrations to `ws/internal/provisioning/repository/migrations/postgres/001_initial.sql` and `002_oidc_channel_rules.sql` (and `atlas.sum`). Create SQLite-equivalent migrations at `ws/internal/provisioning/repository/migrations/sqlite/001_initial.sql` and `002_oidc_channel_rules.sql`. SQLite adaptations: `CREATE TYPE` → `TEXT` + `CHECK(IN)`, `SERIAL` → `INTEGER PRIMARY KEY`, `JSONB` → `TEXT`, `INET` → `TEXT`, regex CHECKs → omit, `NOW()` → `datetime('now')`, triggers use inline body.

- [x] T021 Create embedded migration files at `ws/internal/provisioning/repository/embed.go` using `//go:embed migrations/postgres/*.sql` and `//go:embed migrations/sqlite/*.sql`. Create migration runner at `ws/internal/provisioning/repository/migrator.go`: creates `schema_migrations` table, reads files sorted by name, skips already-applied, applies in transaction, logs each migration.

- [x] T022 Create database factory at `ws/internal/provisioning/repository/factory.go`. Define `DatabaseConfig` struct (Driver, URL, Path, AutoMigrate, pool settings). Implement `OpenDatabase(cfg) (*sql.DB, error)`: switch on driver — `sqlite`: open with `modernc.org/sqlite`, set `PRAGMA journal_mode=WAL`, `busy_timeout=5000`, `foreign_keys=ON`; `postgres`: open with `lib/pq`, configure pool. Run migrations if `AutoMigrate=true`. Add `modernc.org/sqlite` to `ws/go.mod` with blank import `_ "modernc.org/sqlite"`.

- [x] T023 Create database factory and migrator tests at `ws/internal/provisioning/repository/factory_test.go` and `migrator_test.go`. Test: SQLite creation with WAL mode, migration application, skip already-applied, version tracking, invalid driver error.

- [x] T024 Update provisioning main to use database factory at `ws/cmd/provisioning/main.go`. Replace `sql.Open("postgres", cfg.DatabaseURL)` with `repository.OpenDatabase(repository.DatabaseConfig{Driver: cfg.DatabaseDriver, ...})`. Create `AllStores` interface embedding all 7 store interfaces (`TenantStore`, `KeyStore`, `TopicStore`, `QuotaStore`, `AuditStore`, `OIDCConfigStore`, `ChannelRulesStore`) in `ws/internal/provisioning/interfaces.go`. Create `NewStores(db *sql.DB) AllStores` constructor in `ws/internal/provisioning/repository/stores.go` that instantiates all Postgres repositories and returns a struct satisfying `AllStores`. Wire event bus into service. Add gRPC server startup (create `grpc.NewServer` with interceptors, register `ProvisioningInternalServer`, listen on `cfg.GRPCPort`). Add gRPC graceful shutdown alongside HTTP shutdown.

### Sub-phase 2F: Config File Mode

- [x] T025 Create config file types at `ws/internal/provisioning/configstore/types.go`. Define `ConfigFile`, `TenantConfig`, `CategoryConfig`, `KeyConfig`, `QuotaConfig`, `OIDCConfig`, `ChannelRulesConfig` structs with `yaml:` tags matching the schema from research R8.

- [x] T026 Create config file parser at `ws/internal/provisioning/configstore/parser.go`. Implement `ParseFile(path string) (*ConfigFile, error)` and `ParseBytes(data []byte) (*ConfigFile, error)`.

- [x] T027 Create config file validator at `ws/internal/provisioning/configstore/validator.go`. Implement `Validate(cfg *ConfigFile) error`. Return `errors.Join` with all failures. Checks: tenant ID format (`^[a-z][a-z0-9-]{2,62}$`), unique tenant IDs, unique key IDs across all tenants, at least one category per tenant, algorithm in `{ES256, RS256, EdDSA}`, PEM format valid, HTTPS issuer URLs, no conflicting OIDC issuers.

- [x] T028 Create in-memory store implementations at `ws/internal/provisioning/configstore/stores.go`. Implement all 7 Store interfaces (`TenantStore`, `KeyStore`, `TopicStore`, `QuotaStore`, `AuditStore`, `OIDCConfigStore`, `ChannelRulesStore`) backed by in-memory maps. Read methods: look up under `RLock()`. Write methods: return `ErrReadOnlyMode` sentinel error. `Ping()`: always returns nil. `AuditStore`: implement `Log()` as a noop (log entry to zerolog at Debug level instead of persisting — config mode has no writable storage) and `ListByTenant()` returns empty slice. Define `ErrReadOnlyMode` in this package.

- [x] T029 Create config loader with atomic reload at `ws/internal/provisioning/configstore/loader.go`. Implement `Loader` struct with `path`, `atomic.Pointer[ConfigStores]`, `logger`. Methods: `NewLoader`, `Load()`, `Reload()`, `Stores()`. Load/Reload flow: ParseFile → Validate → build ConfigStores → atomic swap. On reload failure: log errors, return error, previous stores remain.

- [x] T030 Create config store tests at `ws/internal/provisioning/configstore/parser_test.go`, `validator_test.go`, `stores_test.go`, `loader_test.go`. Test: valid/invalid YAML parsing, all validation rules, read operations on stores, write operations return `ErrReadOnlyMode`, atomic reload swaps stores, reload failure keeps previous.

- [x] T031 Add config mode branching to provisioning main at `ws/cmd/provisioning/main.go`. Add `switch cfg.ProvisioningMode` with `api` (database factory path) and `config` (loader path). In config mode: create loader, call `Load()`, pass `loader.Stores()` to service. Register SIGHUP handler in a dedicated goroutine following Constitution VII lifecycle: `wg.Add(1)` before `go`, `defer logging.RecoverPanic()` first, `defer wg.Done()` second, `select` on signal channel and `ctx.Done()`. On successful reload: publish all event types via event bus. On failed reload: log errors, increment `provisioning_config_reload_failures_total` counter. Add metrics: `provisioning_config_reload_total` counter, `provisioning_config_reload_failures_total` counter, `provisioning_config_reload_duration_seconds` histogram. Map `ErrReadOnlyMode` to HTTP 405 in handler error mapping.

### Sub-phase 2G: CLI Tool

- [x] T032 Create REST admin client at `ws/cmd/cli/client/admin_client.go`. Implement `AdminClient` struct with `baseURL`, `httpClient`, `token`, `logger`. Config struct with `BaseURL`, `Timeout`, `Token`. Methods for all `/api/v1/` CRUD operations: `CreateTenant`, `GetTenant`, `ListTenants`, `UpdateTenant`, `SuspendTenant`, `ReactivateTenant`, `DeprovisionTenant`, `CreateKey`, `ListKeys`, `RevokeKey`, `CreateCategory`, `ListCategories`, `GetQuota`, `UpdateQuota`, `GetOIDCConfig`, `CreateOIDCConfig`, `UpdateOIDCConfig`, `DeleteOIDCConfig`, `GetChannelRules`, `SetChannelRules`, `DeleteChannelRules`, `TestAccess`. Bearer token in Authorization header.

- [x] T033 Create admin client test at `ws/cmd/cli/client/admin_client_test.go`. Use `httptest.NewServer` with mock handlers. Test: create/get/list tenants, error responses, bearer token sent, timeout handling.

- [x] T034 Add `github.com/spf13/cobra` to `ws/go.mod`. Create CLI entrypoint at `ws/cmd/cli/main.go` and root command at `ws/cmd/cli/commands/root.go` with global flags: `--api-url` (env `SUKKO_API_URL`, default `http://localhost:8080`), `--token` (env `SUKKO_TOKEN`), `--output` (json|table, default table).

- [x] T035 [P] Create tenant commands at `ws/cmd/cli/commands/tenant.go`. Subcommands: `create` (--id required, --name, --category repeatable, --consumer-type), `get` (arg: tenant ID), `list` (--limit, --offset, --status), `update` (arg: tenant ID, --name), `suspend` (arg: tenant ID), `reactivate` (arg: tenant ID), `deprovision` (arg: tenant ID). Each calls `AdminClient` and formats output.

- [x] T036 [P] Create key commands at `ws/cmd/cli/commands/key.go`. Subcommands: `create` (--tenant required, --algorithm required, --public-key-file required, --expires-at), `list` (--tenant required), `revoke` (--tenant required, --key-id required).

- [x] T037 [P] Create category, quota, oidc, rules commands at `ws/cmd/cli/commands/category.go`, `quota.go`, `oidc.go`, `rules.go`. Each follows the same pattern: parse flags → call AdminClient → format output. Category: `create` (--tenant, --name, --partitions, --retention-ms), `list` (--tenant). Quota: `get` (--tenant), `update` (--tenant, --max-topics, etc.). OIDC: `get`, `create`, `update`, `delete` (--tenant, --issuer-url, --jwks-url, --audience). Rules: `get`, `set`, `delete`, `test` (--tenant, --rules-file for set).

- [x] T038 [P] Create config file commands at `ws/cmd/cli/commands/config.go`. Subcommands: `init` (--tenant, generates well-commented YAML template), `validate` (--file required, runs parser + validator, reports errors), `export` (--file required, calls AdminClient to list all tenants, converts to YAML).

- [x] T039 Create output formatter at `ws/cmd/cli/commands/output.go`. Implement `printOutput(data any, format string)`: `json` → `json.MarshalIndent` to stdout, `table` → `text/tabwriter` with typed formatting per resource (tenant table, key table, etc.).

- [x] T039b Create CLI command tests at `ws/cmd/cli/commands/tenant_test.go`, `key_test.go`, `category_test.go`, `quota_test.go`, `oidc_test.go`, `rules_test.go`, `config_test.go`, `output_test.go`. Table-driven tests for each command group: flag parsing (required flags, defaults, invalid values), output formatting (JSON and table modes), error handling (API errors, invalid input), and `RunE` execution against a mock `AdminClient`. Constitution VIII requires test coverage for all new code.

---

## Phase 3: Infrastructure — Helm Charts, Dockerfiles, K8s Manifests

> Dependencies: Phase 2 code must compile. Helm values must match Go env struct tags.

- [x] T040 Update provisioning Helm values at `deployments/helm/sukko/charts/provisioning/values.yaml`. Use three top-level sections (see plan Phase 8.1 for exact structure): (1) `config.*` — add `grpcPort: 9090`, `provisioningMode: "api"`, `configFilePath: "/etc/sukko/tenants.yaml"`, `configFileConfigMap: ""` (name of ConfigMap containing the YAML config file; required when `provisioningMode=config` in K8s), `autoMigrate: true`, `adminToken: ""`. (2) `database.*` — add `sqlitePath: "/data/sukko.db"`, `maxOpenConns: 25`, `maxIdleConns: 5`, `connMaxLifetime: "5m"`. (3) `externalDatabase.*` — add `url: ""`, `existingSecret: ""`, `existingSecretKey: "database-url"`. Do NOT add `config.databaseDriver` — the deployment template auto-derives `DATABASE_DRIVER` from `externalDatabase.*` and `global.postgresql.enabled`. Add `persistence.enabled: true`, `persistence.size: 1Gi`, `persistence.accessMode: ReadWriteOnce`. Add `ingress.enabled: false`, `ingress.className: ""`, `ingress.host: ""`, `ingress.tls: []`, `ingress.annotations: {}`. Set `replicaCount: 1` with comment: `# MUST be 1 with SQLite. Scale horizontally only with PostgreSQL.` Remove from provisioning `values.yaml`: `global.provisioning.existingSecret` (replaced by `externalDatabase.existingSecret`), `config.dbMaxOpenConns` / `config.dbMaxIdleConns` / `config.dbConnMaxLifetime` (moved to `database.*` section). Also update parent chart `deployments/helm/sukko/values.yaml`: remove `global.provisioning.existingSecret` and `global.provisioning.databaseUrl` (dead — gateway/ws-server no longer access DB, provisioning has own `externalDatabase.*`), remove the "Configure ONE of the following" comment block. Add `global.postgresql.enabled: false` (default SQLite). Add `provisioning.externalDatabase` section. Add Helm template guard in deployment.yaml: detect `$dbDriver` from values and if `$dbDriver == "sqlite"` and `replicaCount > 1`, fail with error.

- [x] T041 Update provisioning deployment template at `deployments/helm/sukko/charts/provisioning/templates/deployment.yaml`. Implement auto-derivation of `DATABASE_DRIVER` and `DATABASE_URL` from high-level values: define `$dbDriver` variable — if `externalDatabase.url` or `externalDatabase.existingSecret` is set → `postgres`; else if `global.postgresql.enabled` → `postgres`; else → `sqlite`. Inject `DATABASE_DRIVER` from `$dbDriver`. When `$dbDriver == sqlite`: inject `DATABASE_PATH` from `database.sqlitePath`. When `$dbDriver == postgres`: inject `DATABASE_URL` from the appropriate source (externalDatabase.existingSecret → secretKeyRef, externalDatabase.url → direct value, bundled PostgreSQL → existing provisioning-db secret). Also inject pool settings from `database.*` (only when `$dbDriver == postgres`): `DB_MAX_OPEN_CONNS` → `database.maxOpenConns`, `DB_MAX_IDLE_CONNS` → `database.maxIdleConns`, `DB_CONN_MAX_LIFETIME` → `database.connMaxLifetime`. These replace the old `config.dbMaxOpenConns` references — SQLite doesn't use pool settings. Add env vars: `GRPC_PORT`, `PROVISIONING_MODE`, `AUTO_MIGRATE`, `PROVISIONING_CONFIG_PATH`. Add `PROVISIONING_ADMIN_TOKEN` from Secret ref (only when `config.adminToken` is set). Add gRPC container port. Add volume mount `/data/` for SQLite PVC (only when `$dbDriver == sqlite`). When `config.provisioningMode == "config"` and `config.configFileConfigMap` is set: mount the ConfigMap as a volume at `PROVISIONING_CONFIG_PATH` (default `/etc/sukko/tenants.yaml`). Conditional init containers: `wait-for-postgres` only when `$dbDriver == postgres` and not external; remove when SQLite.

- [x] T041b Create provisioning admin token Secret template at `deployments/helm/sukko/charts/provisioning/templates/secret-admin-token.yaml`. Only rendered when `config.adminToken` is set. Stores the admin token as a Kubernetes Secret.

- [x] T041c Create provisioning Ingress template at `deployments/helm/sukko/charts/provisioning/templates/ingress.yaml`. Only rendered when `ingress.enabled: true`. Standard Ingress resource with configurable `className`, `host`, TLS, and annotations. Add Helm template `fail` guard for FR-A08: `{{- if and .Values.ingress.enabled (not .Values.config.adminToken) }}{{ fail "config.adminToken is required when ingress is enabled (FR-A08)" }}{{- end }}`. Add a Helm values note: when Ingress is enabled, `config.adminToken` MUST be set for security.

- [x] T042 Create provisioning PVC template at `deployments/helm/sukko/charts/provisioning/templates/pvc.yaml`. Only rendered when `persistence.enabled` AND NOT (`externalDatabase.url` OR `externalDatabase.existingSecret` OR `global.postgresql.enabled`) — i.e., the same `$dbDriver == sqlite` logic used in the deployment template. Default 1Gi ReadWriteOnce.

- [x] T043 Create provisioning gRPC service template at `deployments/helm/sukko/charts/provisioning/templates/service-grpc.yaml`. ClusterIP service exposing gRPC port. Name: `{{ fullname }}-grpc`. Port name: `grpc`.

- [x] T044 Update gateway Helm chart at `deployments/helm/sukko/charts/ws-gateway/values.yaml` and `templates/deployment.yaml`. Remove from `values.yaml`: `global.provisioning.existingSecret`, `config.keyCacheRefreshInterval`, `config.keyCacheQueryTimeout`, `config.dbMaxOpenConns`, `config.dbMaxIdleConns`, `config.dbConnMaxLifetime`, `config.dbConnMaxIdleTime`, `config.dbPingTimeout`, `config.issuerCacheTTL`, `config.channelRulesCacheTTL`. Update comment on `config.authEnabled` — remove "from provisioning database", replace with "via gRPC streaming from provisioning service". Remove from `templates/deployment.yaml`: `PROVISIONING_DATABASE_URL` secret ref block, `DB_MAX_OPEN_CONNS`, `DB_MAX_IDLE_CONNS`, `DB_CONN_MAX_LIFETIME`, `DB_CONN_MAX_IDLE_TIME`, `DB_PING_TIMEOUT`, `KEY_CACHE_REFRESH_INTERVAL`, `KEY_CACHE_QUERY_TIMEOUT`, `GATEWAY_ISSUER_CACHE_TTL`, `GATEWAY_CHANNEL_RULES_CACHE_TTL`, `wait-for-postgres` init container. Retain `REQUIRE_TENANT_ID` (controls multi-tenant JWT behavior, not DB access). Add to `values.yaml`: `config.provisioningGRPCAddr: ""` (default dynamic). Add to deployment template: `PROVISIONING_GRPC_ADDR` (default: `{{ .Release.Name }}-provisioning-grpc:9090`), `PROVISIONING_GRPC_RECONNECT_DELAY`, `PROVISIONING_GRPC_RECONNECT_MAX_DELAY`. Replace `wait-for-postgres` with `wait-for-provisioning` init container (check gRPC port on provisioning service).

- [x] T045 Update ws-server Helm chart at `deployments/helm/sukko/charts/ws-server/values.yaml` and `templates/deployment.yaml`. Remove from `values.yaml`: `global.provisioning.existingSecret`, entire `multiTenant` section (`topicRefreshInterval`, `dbMaxOpenConns`, `dbMaxIdleConns`, `dbConnMaxLifetime`, `dbConnMaxIdleTime`). Update comment on line 179 — remove "queries the provisioning database", replace with "receives tenant topics via gRPC streaming from provisioning service". Remove from `templates/deployment.yaml`: `PROVISIONING_DATABASE_URL` secret ref block, `TOPIC_REFRESH_INTERVAL`, `PROVISIONING_DB_MAX_OPEN_CONNS`, `PROVISIONING_DB_MAX_IDLE_CONNS`, `PROVISIONING_DB_CONN_MAX_LIFETIME`, `PROVISIONING_DB_CONN_MAX_IDLE_TIME`, `wait-for-postgres` init container. Add to `values.yaml`: `config.provisioningGRPCAddr: ""` (default dynamic). Add to deployment template: `PROVISIONING_GRPC_ADDR` (default: `{{ .Release.Name }}-provisioning-grpc:9090`), `PROVISIONING_GRPC_RECONNECT_DELAY`, `PROVISIONING_GRPC_RECONNECT_MAX_DELAY`. Replace `wait-for-postgres` with `wait-for-provisioning` init container (check gRPC port on provisioning service).

- [x] T046 Update provisioning Dockerfile at `ws/build/provisioning/Dockerfile`. Add `EXPOSE 9090` for gRPC port. Add `VOLUME ["/data"]` for SQLite.

- [x] T047 Create CLI Dockerfile at `ws/build/cli/Dockerfile`. Same multi-stage pattern as other services. Build `ws/cmd/cli/` → `/sukko` binary. No server, no ports.

- [x] T047b Update Taskfile at `taskfiles/k8s.yml` to support SQLite default. The `deploy` task currently unconditionally handles PostgreSQL password (lines 80-87), runs `wait-for-db` and `db:migrate`. These MUST be conditional on `postgresql.enabled`. When SQLite (default): skip password handling, skip `wait-for-db`, skip `db:migrate` (auto-migrated at startup). When PostgreSQL bundled: keep current flow. Add a `K8S_DB_DRIVER` variable derived from the Helm values file (parse `global.postgresql.enabled`), and wrap the PostgreSQL-specific steps in `if [ "$K8S_DB_DRIVER" = "postgres" ]`. Also auto-sync `global.postgresql.enabled`: when `postgresql.enabled=true` is detected in the env values file, pass `--set global.postgresql.enabled=true` to the Helm install/upgrade command to keep both values in sync. Also update `db:migrate` and `db:status` tasks to check `K8S_DB_DRIVER` and skip gracefully with a message when SQLite is used.

- [x] T047c Add `task k8s:db:setup-external` command to `taskfiles/k8s.yml` for external/managed PostgreSQL setup. Accepts `URL` variable (required). Flow: (1) Validate URL format (must start with `postgres://`), (2) Create K8s secret named `provisioning-external-db` in `K8S_NAMESPACE` with key `database-url` containing the URL, (3) Print instruction to set `provisioning.externalDatabase.existingSecret: "provisioning-external-db"` in the environment Helm values file. Usage: `task k8s:db:setup-external URL="postgres://user:pass@host:5432/sukko" ENV=dev`.

---

## Phase 4: Testing — Unit Tests, Integration Verification

> Dependencies: Phase 2 code complete. Tests can be run incrementally per sub-phase.

- [x] T048 Run `go vet ./...` and `go test ./...` from `ws/` to verify all code compiles and existing tests still pass. Fix any compilation errors from the gateway/ws-server DB removal.

- [x] T049 Run `helm lint` on all three charts: `deployments/helm/sukko/charts/provisioning`, `ws-gateway`, `ws-server`. Verify templates render with `helm template`. Fix any template errors.

- [x] T050 Run `helm template` for all three database scenarios: (1) Default (no database values) → verify `DATABASE_DRIVER=sqlite`, `DATABASE_PATH` set, PVC renders, no `wait-for-postgres`. (2) With `global.postgresql.enabled=true` → verify `DATABASE_DRIVER=postgres`, `DATABASE_URL` from bundled secret, `wait-for-postgres` present, no PVC. (3) With `provisioning.externalDatabase.url=postgres://...` → verify `DATABASE_DRIVER=postgres`, `DATABASE_URL` from provided value, no `wait-for-postgres`, no PVC.

---

## Phase 5: Deploy & Verify

> Dependencies: Phase 4 tests pass.

- [ ] T051 Build all Docker images: `task k8s:build ENV=dev` (provisioning, gateway, ws-server, cli). Verify builds succeed with `CGO_ENABLED=0`.

- [ ] T052 Deploy to dev with `task k8s:deploy ENV=dev`. Verify: provisioning starts with SQLite (check logs for `"database_driver": "sqlite"`), gateway connects via gRPC (check logs for `"provisioning_stream": "connected"`), ws-server discovers topics via gRPC stream.

- [ ] T053 Verify end-to-end: use CLI to create a tenant (`sukko tenant create --id test --name "Test" --category trade`), verify gateway can authenticate connections for the new tenant, verify ws-server discovers the new topic via gRPC stream event.

---

## Phase Dependencies Summary

```
Phase 1 (T001-T006): Config + Proto
    │
    ▼
Phase 2A (T007-T012, T010b-T010c): Event Bus + gRPC Server + Admin Auth
    │
    ├──▶ Phase 2B (T013-T016): Stream Registries [T013/T014/T015 parallel]
    │       │
    │       ├──▶ Phase 2C (T017): Gateway Refactor
    │       └──▶ Phase 2D (T018-T019): ws-server Refactor
    │
    ├──▶ Phase 2E (T020-T024): SQLite + DB Factory
    │       │
    │       └──▶ Phase 2F (T025-T031): Config File Mode
    │
    └──▶ Phase 2G (T032-T039b): CLI Tool [T035-T038 parallel]

Phase 3 (T040-T047c, T041b-T041c): Helm + Docker + Ingress + Taskfile
Phase 4 (T048-T050): Testing Verification
Phase 5 (T051-T053): Deploy & Verify
```

**Parallel opportunities within Phase 2**:
- T013, T014, T015 (key/tenant/topic registries — different files, same pattern)
- T010b (admin auth) can run in parallel with T011 (stream handlers) — different packages
- T035, T036, T037, T038 (CLI command files — independent)
- Phase 2B, 2E, 2G can start simultaneously after 2A completes
