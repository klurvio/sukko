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

1. **Embedded SQLite as default storage** — replace the PostgreSQL dependency with embedded SQLite (`modernc.org/sqlite`, pure Go, no CGO). The provisioning service and gateway store data in a local file (`odin.db`) with zero external dependencies. PostgreSQL remains available as an opt-in for teams that want centralized or HA storage.
2. **Config file mode** — the gateway can load all provisioning data from a YAML file with no database at all. For static, rarely-changing setups.
3. **CLI tool** — an `odin` binary for managing tenants via the provisioning API from the command line.

All three interfaces (API + config + CLI) share the same domain logic (validation, Kafka admin operations) through a common service layer.

## User Scenarios

### Scenario 1 — Single-Tenant Config File Setup (Priority: P1)

An operator deploys Odin with a single tenant. All provisioning data is defined in a YAML config file. No database or provisioning service is required.

**Acceptance Criteria**:
1. **Given** a YAML config file defining one tenant with keys, categories, and channel rules, **When** the gateway starts with `PROVISIONING_MODE=config` and `PROVISIONING_CONFIG_PATH=/etc/odin/tenants.yaml`, **Then** the gateway loads all provisioning data from the file and serves connections without a database.
2. **Given** a running gateway in config mode, **When** the operator modifies the YAML file and sends `SIGHUP` to the process, **Then** the gateway reloads the configuration without restarting and applies changes to new connections.
3. **Given** a config file with validation errors (invalid tenant ID, malformed PEM key, missing required fields), **When** the gateway starts, **Then** it fails fast with a clear error message identifying the problem.

### Scenario 2 — Multi-Tenant Config File Setup (Priority: P1)

An operator deploys Odin with multiple tenants defined in a YAML config file for a stable, rarely-changing environment.

**Acceptance Criteria**:
1. **Given** a YAML config file defining multiple tenants with their respective keys, categories, quotas, OIDC configs, and channel rules, **When** the gateway starts in config mode, **Then** all tenants are loaded and connections for each tenant are served correctly.
2. **Given** a multi-tenant config file, **When** the gateway validates the file, **Then** it rejects duplicate tenant IDs, duplicate key IDs across tenants, and conflicting OIDC issuer URLs.

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
2. **Given** `PROVISIONING_MODE=api` and `DATABASE_DRIVER=postgres` with a `DATABASE_URL`, **When** the services start, **Then** they connect to PostgreSQL (opt-in for HA/centralized storage).
3. **Given** `PROVISIONING_MODE=config`, **When** the gateway starts, **Then** it reads provisioning data from the YAML file and does NOT require any database connection.
4. **Given** `PROVISIONING_MODE=config` and the config file is missing, **When** the gateway starts, **Then** it fails fast with a clear error message.

### Edge Cases

- What happens when an operator switches from `api` mode to `config` mode? The config file must be manually created (or exported via `odin config export`). There is no automatic migration.
- What happens when a config file defines a tenant with keys but no categories? Invalid — at least one category MUST be provisioned per tenant. Categories are required for topic registration, publishing, and broadcast. The config file validator MUST reject tenants with zero categories.
- What happens when a config file defines OIDC config with an unreachable issuer URL? The gateway logs a warning at startup but does not fail — graceful degradation. OIDC validation will fail at runtime with clear errors.
- What happens when `SIGHUP` is sent but the new config is invalid? The gateway rejects the reload, logs the validation errors, and continues with the previous valid configuration.

## Requirements

### Functional Requirements

**Embedded SQLite (Default Storage)**

- **FR-S01**: The provisioning service and gateway MUST support embedded SQLite as the default database via `modernc.org/sqlite` (pure Go, no CGO, bundled in the binary).
- **FR-S02**: When no `DATABASE_URL` is configured, the service MUST default to SQLite with a local file (`odin.db` in the working directory or a configurable path via `DATABASE_PATH`).
- **FR-S03**: PostgreSQL MUST remain supported as an opt-in via `DATABASE_DRIVER=postgres` with a `DATABASE_URL`.
- **FR-S04**: The repository layer MUST use a database abstraction (e.g., `database/sql` with driver selection) so that SQLite and PostgreSQL share the same query logic where possible.
- **FR-S05**: Database migrations MUST work for both SQLite and PostgreSQL. SQL dialect differences (e.g., `SERIAL` vs `INTEGER PRIMARY KEY AUTOINCREMENT`) MUST be handled.
- **FR-S06**: SQLite MUST use WAL mode for concurrent read access. With SQLite, the provisioning service runs as a singleton (replicas: 1). With PostgreSQL, the provisioning service MAY scale horizontally.
- **FR-S07**: The gateway and ws-server MUST read provisioning data via the provisioning REST API — not direct database access. The gateway reads keys, OIDC config, and channel rules. The ws-server reads categories for topic discovery. Only the provisioning service accesses the database directly. This decouples all services from the database driver and enables independent scaling.

**Config Mode**

- **FR-001**: The gateway MUST support a `PROVISIONING_MODE` setting with values `api` (default) and `config`.
- **FR-002**: In config mode, the gateway MUST load all provisioning data (tenants, keys, categories, quotas, OIDC configs, channel rules) from a YAML file specified by `PROVISIONING_CONFIG_PATH`.
- **FR-003**: The config file MUST be validated at startup using the same validation rules as the REST API (tenant ID format, key algorithm, PEM format, channel pattern validity, etc.).
- **FR-004**: Invalid config MUST cause immediate startup failure with a clear, actionable error message.
- **FR-005**: The provisioning service MUST support hot-reload of the config file via `SIGHUP` signal without restarting. Updated data is immediately available via the API.
- **FR-006**: If a hot-reload fails validation, the provisioning service MUST reject the reload, log the errors, and continue with the previous configuration.
- **FR-007**: In config mode, no database connection is required. The provisioning service loads all data from the YAML config file and serves it via its REST API. The gateway and ws-server query the provisioning API as usual.
- **FR-008**: The config-mode data layer MUST implement the same `TenantRegistry` and `KeyRegistry` interfaces used in API mode so the gateway's auth and proxy logic is mode-agnostic.
- **FR-009**: The config file format MUST support all entities that the REST API supports: tenants, keys, categories, quotas, OIDC configs, and channel rules.

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
- **FR-020**: The CLI MUST support authentication via `--token` flag or `ODIN_TOKEN` environment variable when the API requires auth.

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
- **ConfigRegistry**: In-memory implementation of `TenantRegistry` and `KeyRegistry` interfaces, backed by the parsed config file.

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
- Q: The gateway and ws-server currently poll the provisioning database directly with in-memory caching. How must these patterns adapt? → A: **All 5 polling/caching patterns must migrate from direct DB queries to provisioning API calls.** The caching layers themselves remain (they prevent excessive API calls just as they prevented excessive DB queries), but the data source changes from PostgreSQL to the provisioning REST API. The 5 patterns are: (1) **Gateway Key Cache** (`auth/keys_postgres.go`) — background ticker (1min interval), sync.RWMutex + maps, queries `tenant_keys` + `tenants` tables → must query provisioning API instead. (2) **Gateway Tenant Registry Cache** (`gateway/tenant_registry_postgres.go`) — 3 TTL-based lazy caches: issuer→tenant (5min), OIDC config (5min), channel rules (1min) → must query provisioning API instead. (3) **Gateway Keyfunc Cache** (`gateway/multi_issuer_oidc.go`) — TTL-based (1hr), keyfunc/v3 manages JWKS refresh → depends on OIDC config from tenant registry, adapts transitively. (4) **ws-server Topic Discovery** (`orchestration/multitenant_pool.go`) — background ticker (60sec interval), queries `tenants` + `tenant_categories` → must query provisioning API instead. (5) **Kafka Producer Topic Cache** (`kafka/producer.go`) — TTL lazy cache (30sec), queries `tenants` + `tenant_categories` on-demand → must query provisioning API instead.

## Out of Scope

- **Config mode write operations**: In config mode, the provisioning service serves data read-only from the YAML file. Write operations (create tenant, update quota, etc.) via the REST API are not supported — the YAML file is the source of truth, managed externally. The CLI `config` subcommands handle file management.
- **Hybrid mode**: Running both config and API modes simultaneously on the same gateway is not supported. It's one or the other.
- **Config file encryption**: Secrets (PEM keys) in the config file are stored in plaintext. Operators should use filesystem permissions, encrypted volumes, or Kubernetes secrets mounted as files.
- **Auto-sync from API to config**: There is no automatic config file generation from the running API state (except via explicit `odin config export`).
- **GUI / web admin panel**: The CLI is the non-API interface. A web UI is a separate effort.
