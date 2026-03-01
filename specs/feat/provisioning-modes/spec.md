# Feature Specification: Provisioning Modes (Config + CLI)

**Branch**: `feat/provisioning-modes`
**Created**: 2026-02-26
**Status**: Draft

## Context

Odin WS provisions tenants, keys, topics, quotas, OIDC configurations, and channel rules through a REST API backed by PostgreSQL. The gateway reads provisioning data directly from the same database (no HTTP calls between services). This architecture works but imposes a hard dependency on an external PostgreSQL instance for every deployment — even single-tenant setups that never change configuration.

Today, the only way to manage tenants is through the REST API. This doesn't fit all operational workflows:

1. **Simple deployments** — a team running Odin with one tenant and static configuration shouldn't need an external database and a provisioning service. A config file should suffice.
2. **GitOps / automation-driven teams** — operators who manage infrastructure via scripts, Terraform, and CI pipelines prefer CLI tools over HTTP APIs for provisioning operations.
3. **Local development** — developers running Odin locally shouldn't need an external database just to connect a test client.
4. **Unnecessary infrastructure** — provisioning data is small and rarely changes. An external PostgreSQL instance is overkill for most deployments.

This spec adds three changes:

1. **Embedded SQLite as default storage** — replace the PostgreSQL dependency with embedded SQLite (`modernc.org/sqlite`, pure Go, no CGO). The provisioning service stores data in a local file (`odin.db`) with zero external dependencies. The gateway and ws-server receive provisioning data via gRPC streaming — they never access the database directly. PostgreSQL remains available as an opt-in for teams that want centralized or HA storage.
2. **Config file mode** — the provisioning service can load all provisioning data from a YAML file with no database at all, serving it via gRPC streams and REST API. For static, rarely-changing setups.
3. **CLI tool** — an `odin` binary for managing tenants via the provisioning API from the command line.

All three interfaces (API + config + CLI) share the same domain logic (validation, Kafka admin operations) through a common service layer.

## User Scenarios

### Scenario 1 — Single-Tenant Config File Setup (Priority: P1)

An operator deploys Odin with a single tenant. All provisioning data is defined in a YAML config file. No database is required. The provisioning service runs in config mode, loading data from the YAML file and serving it via gRPC streams.

**Acceptance Criteria**:
1. **Given** a YAML config file defining one tenant with keys, categories, and channel rules, **When** the provisioning service starts with `PROVISIONING_MODE=config` and `PROVISIONING_CONFIG_PATH=/etc/odin/tenants.yaml`, **Then** the provisioning service loads all provisioning data from the file and the gateway receives it via gRPC streaming — no database required.
2. **Given** a running provisioning service in config mode, **When** the operator modifies the YAML file and sends `SIGHUP` to the provisioning process, **Then** the provisioning service reloads the configuration and pushes updates to gateway/ws-server via gRPC streams without restarting.
3. **Given** a config file with validation errors (invalid tenant ID, malformed PEM key, missing required fields), **When** the provisioning service starts, **Then** it fails fast with a clear error message identifying the problem.

### Scenario 2 — Multi-Tenant Config File Setup (Priority: P1)

An operator deploys Odin with multiple tenants defined in a YAML config file for a stable, rarely-changing environment.

**Acceptance Criteria**:
1. **Given** a YAML config file defining multiple tenants with their respective keys, categories, quotas, OIDC configs, and channel rules, **When** the provisioning service starts in config mode, **Then** all tenants are loaded and served via gRPC streams and REST API. The gateway receives all tenant data and serves connections correctly.
2. **Given** a multi-tenant config file, **When** the provisioning service validates the file at startup, **Then** it rejects duplicate tenant IDs, duplicate key IDs across tenants, and conflicting OIDC issuer URLs.

### Scenario 3 — CLI Tenant Management (Priority: P1)

An operator uses a CLI tool to create and manage tenants against a running provisioning service.

**Acceptance Criteria**:
1. **Given** a running provisioning service, **When** the operator runs `odin tenant create --id acme --name "Acme Corp" --category trade --category analytics`, **Then** the tenant is created with the specified categories and the output shows the tenant details.
2. **Given** an existing tenant, **When** the operator runs `odin key create --tenant acme --algorithm ES256 --public-key-file ./key.pem`, **Then** a public key is registered for the tenant.
3. **Given** an existing tenant, **When** the operator runs `odin tenant get acme`, **Then** the tenant details (status, keys, topics, quotas) are displayed in a readable format.
4. **Given** a CLI command with invalid input, **When** the operator runs it, **Then** a clear error message is shown with usage hints.

### Scenario 4 — CLI Config File Management (Priority: P2)

An operator uses the CLI to generate and validate config files.

**Acceptance Criteria**:
1. **Given** no existing config, **When** the operator runs `odin config init --tenant my-tenant`, **Then** a well-commented YAML config file is generated with sensible defaults and placeholder values.
2. **Given** an existing config file, **When** the operator runs `odin config validate --file tenants.yaml`, **Then** the file is validated against the full provisioning schema and errors are reported with line numbers.
3. **Given** a running provisioning service with existing tenants, **When** the operator runs `odin config export --file tenants.yaml`, **Then** the current provisioning state is exported as a valid YAML config file.

### Scenario 5 — Mode Coexistence (Priority: P2)

Different deployment environments use different provisioning modes. The gateway adapts based on configuration.

**Acceptance Criteria**:
1. **Given** `PROVISIONING_MODE=api` (default), **When** the gateway and provisioning service start without `DATABASE_URL`, **Then** they use embedded SQLite with a local `odin.db` file — no external database required.
2. **Given** `PROVISIONING_MODE=api` and PostgreSQL is enabled via Helm values (`postgresql.enabled: true` or `externalDatabase` configured), **When** the services start, **Then** Helm auto-injects `DATABASE_DRIVER=postgres` with the appropriate `DATABASE_URL` — the developer sets high-level Helm values, not env vars directly.
3. **Given** `PROVISIONING_MODE=config`, **When** the provisioning service starts, **Then** it reads provisioning data from the YAML file and does NOT require any database connection. The gateway and ws-server receive data via gRPC streaming.
4. **Given** `PROVISIONING_MODE=config` and the config file is missing, **When** the provisioning service starts, **Then** it fails fast with a clear error message.

### Scenario 6 — Database Configuration DevEx (Priority: P1)

Three database scenarios exist with progressive configuration complexity:

**Acceptance Criteria**:
1. **Given** a fresh Helm deploy with no database values set, **When** the provisioning service starts, **Then** it uses embedded SQLite automatically — zero developer configuration required.
2. **Given** `postgresql.enabled: true` in the parent Helm values, **When** `task k8s:deploy` runs, **Then** Helm deploys the PostgreSQL subchart, auto-generates the password, auto-constructs `DATABASE_URL`, and injects `DATABASE_DRIVER=postgres` into the provisioning pod — zero developer intervention beyond the single flag.
3. **Given** a managed/external PostgreSQL database, **When** the operator runs `task k8s:db:setup-external URL="postgres://..." ENV=dev` and sets `provisioning.externalDatabase.existingSecret` in Helm values, **Then** Helm sets `DATABASE_DRIVER=postgres` and injects the URL from the pre-created K8s secret — the only scenario requiring a developer-provided value.

### Edge Cases

- What happens when an operator switches from `api` mode to `config` mode? The config file must be manually created (or exported via `odin config export`). There is no automatic migration.
- What happens when a config file defines a tenant with keys but no categories? Invalid — at least one category MUST be provisioned per tenant. Categories are required for topic registration, publishing, and broadcast. The config file validator MUST reject tenants with zero categories.
- What happens when a config file defines OIDC config with an unreachable issuer URL? The gateway logs a warning at startup but does not fail — graceful degradation. OIDC validation will fail at runtime with clear errors.
- What happens when `SIGHUP` is sent but the new config is invalid? The provisioning service rejects the reload, logs the validation errors, and continues serving the previous valid configuration.

## Requirements

### Functional Requirements

**Embedded SQLite (Default Storage)**

- **FR-S01**: The provisioning service MUST support embedded SQLite as the default database via `modernc.org/sqlite` (pure Go, no CGO, bundled in the binary). The gateway and ws-server receive data via gRPC streaming and never access the database directly.
- **FR-S02**: The provisioning service MUST auto-detect the database driver based on configuration. The default is embedded SQLite with a local file (`odin.db` or configurable via `DATABASE_PATH`). The Go binary reads `DATABASE_DRIVER` and `DATABASE_URL` env vars, but these are auto-injected by Helm templates based on high-level values — developers do not set them directly.
- **FR-S03**: PostgreSQL MUST remain supported via two opt-in paths: (1) **Bundled subchart** — set `postgresql.enabled: true` in parent Helm values; Helm auto-generates credentials, auto-constructs `DATABASE_URL`, and sets `DATABASE_DRIVER=postgres`. (2) **External/managed** — set `provisioning.externalDatabase.url` or `.existingSecret`; Helm sets `DATABASE_DRIVER=postgres` and injects the provided URL. Both paths require zero manual env var configuration. Only the external path requires the developer to provide a value (the connection URL).
- **FR-S04**: The repository layer MUST use a database abstraction (e.g., `database/sql` with driver selection) so that SQLite and PostgreSQL share the same query logic where possible.
- **FR-S05**: Database migrations MUST work for both SQLite and PostgreSQL. SQL dialect differences (e.g., `SERIAL` vs `INTEGER PRIMARY KEY AUTOINCREMENT`) MUST be handled.
- **FR-S06**: SQLite MUST use WAL mode for concurrent read access. With SQLite, the provisioning service runs as a singleton (replicas: 1). With PostgreSQL, the provisioning service MAY scale horizontally.
- **FR-S07**: The gateway and ws-server MUST receive provisioning data via gRPC streaming from the provisioning service — not direct database access. The provisioning service exposes gRPC server-side streams (`WatchKeys`, `WatchTenantConfig`, `WatchTopics`) that push a full snapshot on connect and deltas on change. The gateway subscribes for keys, OIDC config, and channel rules. The ws-server subscribes for topic discovery. Only the provisioning service accesses the database directly. This decouples all services from the database driver and enables real-time propagation.

**Config Mode**

- **FR-001**: The provisioning service MUST support a `PROVISIONING_MODE` setting with values `api` (default) and `config`.
- **FR-002**: In config mode, the provisioning service MUST load all provisioning data (tenants, keys, categories, quotas, OIDC configs, channel rules) from a YAML file specified by `PROVISIONING_CONFIG_PATH` and serve it via gRPC streams and REST API.
- **FR-003**: The config file MUST be validated at startup using the same validation rules as the REST API (tenant ID format, key algorithm, PEM format, channel pattern validity, etc.).
- **FR-004**: Invalid config MUST cause immediate startup failure with a clear, actionable error message.
- **FR-005**: The provisioning service MUST support hot-reload of the config file via `SIGHUP` signal without restarting. Updated data is immediately pushed to gRPC stream subscribers and available via the REST API.
- **FR-006**: If a hot-reload fails validation, the provisioning service MUST reject the reload, log the errors, and continue with the previous configuration.
- **FR-007**: In config mode, no database connection is required. The provisioning service loads all data from the YAML config file and serves it via gRPC streams and REST API. The gateway and ws-server receive data via gRPC streaming as usual.
- **FR-008**: The config-mode data layer MUST implement the same Store interfaces used in API mode so the provisioning service's gRPC and REST layers are mode-agnostic.
- **FR-009**: The config file format MUST support all entities that the REST API supports: tenants, keys, categories, quotas, OIDC configs, and channel rules.

**gRPC Internal API**

- **FR-G01**: The provisioning service MUST expose a gRPC server on a configurable port (`GRPC_PORT`, default 9090) for service-to-service communication.
- **FR-G02**: The gRPC API MUST provide server-side streaming RPCs: `WatchKeys`, `WatchTenantConfig`, `WatchTopics`. Each stream sends a full snapshot on initial connect and pushes deltas when data changes.
- **FR-G03**: Change detection MUST use an in-process event bus. The service layer emits events after successful writes (DB or config reload). gRPC stream handlers subscribe to the event bus and push updates to connected clients.
- **FR-G04**: The gateway and ws-server MUST maintain in-memory caches updated via gRPC stream events, replacing the current polling-based caches.
- **FR-G05**: On stream disconnection, clients MUST reconnect with exponential backoff (initial 1s, max 30s). During disconnection, stale cached data MUST continue to be served. Health endpoints MUST report degraded state.
- **FR-G06**: Proto files MUST be defined in the `ws/proto/` directory. Code generation MUST use `buf`. Generated Go code MUST be committed to the repository.

**Admin API Authentication**

- **FR-A01**: The provisioning REST API MUST support an opaque admin token for operator authentication via `PROVISIONING_ADMIN_TOKEN` env var. This is separate from the tenant JWT system — it's an API key for management operations, not a signed JWT.
- **FR-A02**: Admin token middleware MUST check `Authorization: Bearer <token>` against `PROVISIONING_ADMIN_TOKEN` using constant-time comparison (`crypto/subtle.ConstantTimeCompare`). On match, grant full admin access. On mismatch, fall through to existing JWT validation.
- **FR-A03**: When `PROVISIONING_ADMIN_TOKEN` is unset or empty, the admin token auth path MUST be disabled — no backdoor. Existing JWT auth (if enabled) continues to work.
- **FR-A04**: The admin token MUST be redacted in all logs, config print output, and error messages. `LogConfig()` MUST show `"admin_token": "[REDACTED]"` when set.
- **FR-A05**: Auth failure rate limiting MUST apply to the admin token path: after 10 failures from the same IP within 1 minute, return HTTP 429 for 60 seconds. This is transparent to legitimate users — only brute-force attempts are throttled.
- **FR-A06**: At startup, if `PROVISIONING_ADMIN_TOKEN` is set and shorter than 16 characters, the service MUST log a WARNING (not fail) suggesting a stronger token. In non-development environments (`ENVIRONMENT != dev`), tokens shorter than 16 characters MUST cause startup failure.

**Admin API Exposure (Optional Ingress)**

- **FR-A07**: The provisioning Helm chart MUST include an optional Ingress template (disabled by default via `ingress.enabled: false`). When enabled, it exposes the REST API externally with a configurable hostname and TLS.
- **FR-A08**: When Ingress is enabled and `PROVISIONING_ADMIN_TOKEN` is unset, deployment MUST be prevented. This is enforced via a Helm template `fail` guard (template-time validation), not runtime code — `helm install/upgrade` will fail with a clear error before any pod is created.

**CLI**

- **FR-010**: A CLI tool (`odin`) MUST support tenant lifecycle operations: `create`, `get`, `list`, `update`, `suspend`, `reactivate`, `deprovision`.
- **FR-011**: The CLI MUST support key management: `key create`, `key list`, `key revoke`.
- **FR-012**: The CLI MUST support category management: `category create`, `category list`.
- **FR-013**: The CLI MUST support quota management: `quota get`, `quota update`.
- **FR-014**: The CLI MUST support OIDC configuration: `oidc get`, `oidc create`, `oidc update`, `oidc delete`.
- **FR-015**: The CLI MUST support channel rules: `rules get`, `rules set`, `rules delete`, `rules test`.
- **FR-016**: The CLI MUST support config file operations: `config init`, `config validate`, `config export`.
- **FR-017**: The CLI MUST communicate with the provisioning service via its REST API (not direct database access).
- **FR-018**: The CLI MUST support configuring the API endpoint via `--api-url` flag or `ODIN_API_URL` environment variable.
- **FR-019**: The CLI MUST support JSON and table output formats via `--output json|table` flag.
- **FR-020**: The CLI MUST support authentication via `--token` flag or `ODIN_TOKEN` environment variable. The token is the opaque admin token set as `PROVISIONING_ADMIN_TOKEN` on the provisioning service — not a JWT.

### Non-Functional Requirements

- **NFR-001**: Config mode startup MUST complete within 100ms for files with up to 100 tenants (no database or network calls).
- **NFR-002**: Config file hot-reload MUST complete within 500ms and MUST NOT interrupt active connections.
- **NFR-003**: The CLI binary MUST be a single static binary with no runtime dependencies.
- **NFR-004**: CLI commands MUST complete within 5 seconds for standard operations (excluding network latency to the API).
- **NFR-005**: The config file format MUST be human-readable and well-documented with inline comments.

### Key Entities

- **ProvisioningMode**: Enum — `api` (database-backed, default) or `config` (file-backed).
- **DatabaseDriver**: Enum — `sqlite` (embedded, default) or `postgres` (external, opt-in). Only applies in `api` mode.
- **ConfigFile**: YAML file containing an array of tenant definitions with nested keys, topics, quotas, OIDC config, and channel rules.
- **ConfigStore**: In-memory implementation of all Store interfaces, backed by the parsed config file.
- **EventBus**: In-process pub/sub for change notifications. Emits events after writes (DB or config reload). gRPC stream handlers subscribe to push updates.

## Success Criteria

- **SC-001**: An operator can deploy Odin with a single tenant using only a config file — no database required. The provisioning service runs but loads data from the YAML file instead of a database.
- **SC-002**: An operator can manage tenants via CLI commands against the provisioning API with the same capabilities as direct API calls.
- **SC-003**: Switching between `api` and `config` modes requires only changing `PROVISIONING_MODE` (and providing the config file). No code or gateway logic changes.
- **SC-004**: The CLI `config validate` command catches all errors that would cause a startup failure.
- **SC-005**: Hot-reload via `SIGHUP` applies config changes to new connections without dropping existing ones.
- **SC-006**: A fresh Odin deployment with API mode works out of the box with embedded SQLite — no external database provisioning required.

## Clarifications

- Q: Should API mode always require an external PostgreSQL, or should there be a lighter option? → A: **Embedded SQLite as default, PostgreSQL opt-in.** SQLite via `modernc.org/sqlite` (pure Go, no CGO) is bundled in the binary. Provisioning data is small and rarely changes — SQLite handles it comfortably. PostgreSQL is available via `DATABASE_DRIVER=postgres` for teams that want centralized or HA storage. This matches the industry standard (Grafana, Gitea, Drone CI all default to SQLite).
- Q: Only categories are provisioned, not full topic names — correct? → A: **Yes.** The database stores categories (e.g., "trade", "liquidity"). Full topic names are composed at runtime as `{namespace}.{tenant_id}.{category}`. At least one category MUST be provisioned per tenant — publishing fails with `topic_not_provisioned` if the category doesn't exist.
- Q: In API mode with SQLite, how do the gateway and provisioning service share the database across pods? → A: **Gateway queries the provisioning REST API, not the database directly.** The provisioning service is a singleton (replicas: 1 with SQLite) that owns the database file. SQLite only supports a single writer and doesn't work over network storage — multiple replicas would cause data divergence or corruption. The gateway reads provisioning data (keys, OIDC config, channel rules) via the provisioning API, not direct DB access. With PostgreSQL, the provisioning service CAN scale horizontally (multiple replicas, HPA enabled).
- Q: How does the ws-server discover categories for topic consumption when there's no database (config mode)? → A: **ws-server also queries the provisioning REST API** for category/topic discovery, same as the gateway. This unifies the pattern: only the provisioning service accesses the database. All other services (gateway, ws-server) use the provisioning API. Works uniformly for both `api` and `config` provisioning modes.
- Q: Does config mode require the provisioning service to be running? → A: **Yes, the provisioning service always runs.** In config mode, the provisioning service loads data from the YAML file (no database) and serves it via API. In API mode, it uses SQLite/PostgreSQL. The gateway and ws-server always query the provisioning API — their behavior is identical regardless of provisioning mode. This means the provisioning service itself supports two data sources: database (api mode) and YAML file (config mode).
- Q: Should the gateway refactor (direct DB → provisioning API) be part of this spec? → A: **Yes, included in this spec.** The gateway currently reads keys, OIDC config, and channel rules directly from PostgreSQL. This spec replaces those with provisioning API calls — a prerequisite for config mode and SQLite to work correctly. The ws-server's topic discovery via `TenantRegistry` also moves to API calls.
- Q: Where should the CLI binary live? → A: **`ws/cmd/cli/`** — same Go module as other services. Shares types, validation, and API client code. Builds as a separate binary named `odin`. Consistent with existing `cmd/` convention.
- Q: How should internal service-to-service communication (provisioning ↔ gateway/ws-server) be implemented? → A: **gRPC with protobuf for internal, REST for external.** The provisioning service exposes two interfaces: (1) gRPC on a separate port (e.g., 9090) for gateway and ws-server — type-safe protobuf contracts, binary serialization, code-generated clients. (2) REST API on port 8080 for CLI, admin UI, and external consumers. The storage backend (SQLite/PostgreSQL/YAML) is an internal detail — gRPC applies uniformly regardless of storage mode. Proto files define the internal contract; `buf` generates Go code. Dependencies: `google.golang.org/grpc`, `google.golang.org/protobuf`.
- Q: The gateway and ws-server currently poll the provisioning database directly with in-memory caching. How must these patterns adapt? → A: **Replace polling with gRPC server-side streaming.** All 5 polling/caching patterns migrate from direct DB queries + polling to gRPC streaming subscriptions. The provisioning service pushes changes in real-time instead of clients polling. Stream lifecycle: (1) client connects → receives full snapshot, (2) provisioning detects changes (DB write / SIGHUP reload) → pushes deltas, (3) stream disconnects → client reconnects with exponential backoff, receives new snapshot. In-memory caches remain but are updated via stream events instead of polling. The 5 patterns that migrate: (1) **Gateway Key Cache** (`auth/keys_postgres.go`) → `WatchKeys` stream replaces 1min background ticker. (2) **Gateway Tenant Registry Cache** (`gateway/tenant_registry_postgres.go`) → `WatchTenantConfig` stream replaces 3 TTL-based lazy caches (issuer→tenant, OIDC config, channel rules). (3) **Gateway Keyfunc Cache** (`gateway/multi_issuer_oidc.go`) → adapts transitively when tenant registry receives OIDC config updates via stream. (4) **ws-server Topic Discovery** (`orchestration/multitenant_pool.go`) → `WatchTopics` stream replaces 60sec background ticker. (5) **Kafka Producer Topic Cache** (`kafka/producer.go`) → receives topic updates from the same `WatchTopics` stream, replaces 30sec TTL lazy cache.
- Q: How does the provisioning service detect data changes to push via gRPC streams? → A: **In-process event bus.** The service layer emits events after successful writes (e.g., `KeysChanged`, `TenantConfigChanged`, `TopicsChanged`). The gRPC stream handlers subscribe to this event bus and push updates to all connected clients. Simple, no external dependencies, works uniformly with SQLite, PostgreSQL, and config mode. In config mode, SIGHUP reload emits events for all changed entities. The event bus is an internal `chan` or pub/sub within the provisioning process — not an external system.
- Q: How does the CLI connect to the provisioning API, and how is authentication handled? → A: **Opaque admin token + optional Ingress.** The provisioning API is ClusterIP-only by default (internal). An optional Ingress template exposes it externally when needed. CLI authenticates via an opaque admin token (`PROVISIONING_ADMIN_TOKEN` env var on provisioning, `ODIN_TOKEN` or `--token` on CLI). This is separate from tenant JWT auth — it's a simple API key for operator access. Security measures: constant-time comparison, redacted in logs, rate-limited auth failures (10 failures/min per IP → 429), minimum 16-char length enforced in non-dev environments. All transparent to legitimate users.
- Q: How are `DATABASE_DRIVER` and `DATABASE_URL` configured without regressing the current zero-config DevEx? → A: **Helm templates auto-derive them from high-level values.** Developers never set `DATABASE_DRIVER` or `DATABASE_URL` directly. The provisioning Helm template detects the database scenario from three high-level values: (1) Default (nothing set) → `DATABASE_DRIVER=sqlite`, no `DATABASE_URL`. (2) `postgresql.enabled: true` → `DATABASE_DRIVER=postgres`, `DATABASE_URL` auto-constructed from the bundled subchart's predictable service name and auto-generated password (same pattern as today). (3) `provisioning.externalDatabase.url` or `.existingSecret` → `DATABASE_DRIVER=postgres`, `DATABASE_URL` from the provided value. This preserves the current automation where `task k8s:deploy` handles everything — the only new manual step is setting `postgresql.enabled: true` (one line) if the developer wants PostgreSQL instead of the new SQLite default.
- Q: For external/managed PostgreSQL (option c), what's the deployment flow and where should setup automation live? → A: **Pre-create K8s secret via Taskfile command.** The database must exist before `helm deploy` because provisioning needs the URL at startup. The setup is automated via `task k8s:db:setup-external URL="postgres://..." ENV=dev` — a Taskfile command (not a CLI command) because it requires kubectl context and namespace awareness that the Taskfile already has. Flow: (1) Create managed database externally (Terraform, cloud console, or separate Helm release). (2) Run `task k8s:db:setup-external URL="postgres://..." ENV=dev` — validates URL, creates K8s secret with correct name/key, prints the Helm value to set. (3) Set `provisioning.externalDatabase.existingSecret` in environment Helm values file. (4) Run `task k8s:deploy`. The CLI stays focused on provisioning API operations — K8s secret management is a deployment concern, not a provisioning concern. This is the only DB scenario (of three) requiring manual setup.
- Q: When the provisioning API is temporarily unavailable (rolling update, crash), how should the gateway and ws-server behave? → A: **Circuit breaker with health degradation.** When the gRPC stream disconnects, the client enters a reconnect loop with exponential backoff. During disconnection: continue serving stale cached data — no connection failures. Report degraded health via the health endpoint. After N consecutive reconnect failures, trip the circuit breaker. Auto-recover when the stream reconnects (receives fresh snapshot). Metrics: `provisioning_stream_state` gauge (0=connected, 1=reconnecting, 2=disconnected), `provisioning_stream_reconnects_total` counter. Config: `PROVISIONING_GRPC_RECONNECT_DELAY` (initial backoff, default 1s), `PROVISIONING_GRPC_RECONNECT_MAX_DELAY` (backoff cap, default 30s).

## Out of Scope

- **Config mode write operations**: In config mode, the provisioning service serves data read-only from the YAML file. Write operations (create tenant, update quota, etc.) via the REST API are not supported — the YAML file is the source of truth, managed externally. The CLI `config` subcommands handle file management.
- **Hybrid mode**: Running both config and API modes simultaneously on the same gateway is not supported. It's one or the other.
- **Config file encryption**: Secrets (PEM keys) in the config file are stored in plaintext. Operators should use filesystem permissions, encrypted volumes, or Kubernetes secrets mounted as files.
- **Auto-sync from API to config**: There is no automatic config file generation from the running API state (except via explicit `odin config export`).
- **GUI / web admin panel**: The CLI is the non-API interface. A web UI is a separate effort.
