# Feature Specification: Minimal Helm Configuration

**Branch**: `feat/helm-minimal-config`
**Base Branch**: `feat/message-backends`
**Created**: 2026-03-02
**Status**: Ready for Planning

## Context

The current Helm configuration is a barrier to developer adoption. Values files duplicate Go `envDefault` values, contain unused fields, expose internal tuning knobs that developers don't need, and have conflicting defaults between parent and subchart values. A developer trying Sukko for the first time must wade through hundreds of configuration lines to deploy what should be a zero-config experience.

The Go binaries already have sensible defaults for every setting. Helm should only override what Kubernetes requires (service ports, image refs, resource limits) and let developers add overrides only when they need non-default behavior.

### Current State

- **ws-server/values.yaml**: ~290 lines, including CPU thresholds, hysteresis bands, rate limiter tuning, broadcast bus config, Kafka SASL/TLS, alerting, audit — most duplicating Go defaults
- **Provisioning/values.yaml**: Exposes database config, quota defaults, lifecycle settings — most never overridden
- **Parent values.yaml**: ~300 lines, conflicts with subchart defaults (e.g., `broadcast.type: nats` in parent vs `valkey` in subchart)
- **Environment overrides (dev/stg/prod.yaml)**: 150-200 lines each, mixing deployment concerns (replicas, resources) with application tuning
- **Broadcast type default**: Go says `valkey`, parent Helm says `nats` — inconsistent
- **Rate limit defaults**: Go envDefaults (conservative for local) never reachable via Helm (overridden at every level)

### Desired State

A developer runs `docker compose up` and gets a working system with:
- **Provisioning**: SQLite storage, API mode (zero external dependencies)
- **Broadcast bus**: NATS (lightweight, already required as infrastructure)
- **Message backend**: Direct mode (no persistence, lowest friction)
- **All tuning**: Go envDefaults (no overrides needed)

For Kubernetes (dev/staging/production), the Helm chart is minimal — only deployment-level settings and mode selections. Local Kubernetes (Kind/minikube) is NOT supported as a dev workflow — too resource-heavy. Docker Compose is the local dev path; Helm is for real K8s clusters (GKE dev, staging, production).

## User Scenarios

### Scenario 1a - Zero-Config Local Dev with Docker Compose (Priority: P0)

A developer clones the repo and runs `docker compose up` to get a fully working system on their laptop. No Kubernetes, no Helm, no kubectl — just Docker.

**Acceptance Criteria**:
1. **Given** a machine with only Docker installed, **When** the developer runs `docker compose up` from the repo root, **Then** all services (ws-gateway, ws-server, provisioning, NATS) start and reach healthy state within 60 seconds
2. **Given** the docker-compose deployment, **When** the developer connects a WebSocket client to `ws://localhost:3000`, **Then** they can subscribe to channels and receive messages published by other clients
3. **Given** the docker-compose deployment, **When** the developer modifies code and runs `docker compose up --build`, **Then** the changed service rebuilds and restarts with the new code

### Scenario 2 - Override to Kafka/Redpanda Mode (Priority: P2)

A developer wants persistent message storage with offset-based replay. They provide a small values override to switch from direct to Kafka mode.

**Acceptance Criteria**:
1. **Given** a running Kafka/Redpanda cluster, **When** the developer deploys with a values file containing only `ws-server.messageBackend: kafka` and broker address, **Then** the ws-server connects to Kafka and begins consuming/producing messages
2. **Given** the Kafka override, **When** the developer does NOT specify SASL, TLS, partitions, or replication factor, **Then** the system uses Go envDefaults for all unspecified fields

### Scenario 3 - Override to NATS JetStream Mode (Priority: P2)

A developer wants lightweight persistent streams. They provide a small values override to switch from direct to NATS JetStream mode.

**Acceptance Criteria**:
1. **Given** the NATS subchart with JetStream enabled, **When** the developer deploys with a values file containing only `ws-server.messageBackend: nats`, **Then** Helm auto-wires `NATS_JETSTREAM_URLS` from the release name, the ws-server creates streams and begins consuming messages. For external/managed JetStream, the developer additionally sets `natsJetStream.urls`
2. **Given** the NATS override, **When** the developer does NOT specify replicas, max age, or TLS, **Then** the system uses Go envDefaults

### Scenario 4 - Override to PostgreSQL Storage (Priority: P2)

A developer wants production-grade storage for the provisioning service. They switch from SQLite to PostgreSQL.

**Acceptance Criteria**:
1. **Given** a running PostgreSQL instance, **When** the developer deploys with a values file containing `provisioning.database.driver: postgres` and a connection URL, **Then** the provisioning service connects to PostgreSQL and runs migrations
2. **Given** the PostgreSQL override, **When** the developer does NOT specify pool settings, **Then** the system uses Go envDefaults for connection pool configuration

### Scenario 5 - Override Broadcast Bus to Valkey (Priority: P3)

A developer wants to use Valkey (Redis-compatible) instead of NATS for the broadcast bus.

**Acceptance Criteria**:
1. **Given** a running Valkey instance, **When** the developer deploys with a values file containing `ws-server.broadcast.type: valkey` and Valkey addresses, **Then** the ws-server uses Valkey Pub/Sub for inter-instance messaging

### Scenario 6 - Production Deployment (Priority: P2)

An operator deploys to production with environment-specific overrides for resources, replicas, and security.

**Acceptance Criteria**:
1. **Given** a production environment override file, **When** the operator deploys, **Then** only deployment-level settings (replicas, resources, image tags, node affinity) and mode selections (Kafka, PostgreSQL) are in the override — no application tuning knobs
2. **Given** the production deployment, **When** the operator needs to tune a specific parameter (e.g., slow client threshold), **Then** they add ONLY that field to their override file

### Edge Cases

- What happens when a developer specifies `messageBackend: kafka` but omits broker addresses? The Go binary MUST fail at startup with a clear error message naming the missing field
- What happens when a developer specifies `broadcast.type: valkey` but no Valkey addresses? Same — clear startup failure
- What happens when conflicting overrides exist (e.g., parent says nats, subchart says valkey)? Helm's merge precedence resolves this — subchart values take precedence. This must be documented
- What happens when a field is removed from values.yaml that an existing deployment overrides? Helm ignores unknown values — no breakage, but the override becomes a no-op. Document migration path

## Requirements

### Functional Requirements

#### Helm Values Cleanup

- **FR-001**: Subchart `values.yaml` files MUST contain ONLY fields that differ from Go `envDefault` values or are required by Kubernetes (image, resources, service ports, probes, security context)
- **FR-002**: Fields where Go `envDefault` is the correct default for all environments MUST be removed from `values.yaml` — the Go binary provides the default
- **FR-003**: The parent `values.yaml` MUST NOT override subchart defaults unless the override is required for the chart to function (e.g., service discovery addresses between subcharts)
- **FR-003a**: Inter-service discovery addresses (NATS URLs, provisioning gRPC address, etc.) MUST be auto-computed in Helm templates using `{{ .Release.Name }}` conventions (e.g., `nats://{{ .Release.Name }}-nats:4222`) when the corresponding subchart is enabled. When a subchart is disabled, the developer MUST provide the external service URL. Go `envDefault` values MUST remain `localhost:*` for local binary execution without Kubernetes
- **FR-004**: Environment override files (dev/stg/prod.yaml) MUST contain ONLY deployment-level settings (replicas, resources, image tags, tolerations), mode selections (message backend, database driver), and infrastructure topology (which subcharts are enabled, external service URLs) — no application tuning. Staging MAY use a different infrastructure topology than production (e.g., in-cluster NATS vs external managed NATS)

#### Default Mode Changes

- **FR-005**: The Go `envDefault` for `BROADCAST_TYPE` MUST be changed from `valkey` to `nats` — NATS is lighter, already required as infrastructure, and reduces the minimum dependency footprint. Helm values.yaml MUST NOT set this field — the Go default is the source of truth. Helm comments MUST document valid values (`nats`, `valkey`). Existing deployments using Valkey MUST add `broadcast.type: valkey` to their override files
- **FR-006**: The Go `envDefault` for `MESSAGE_BACKEND` MUST remain `direct` — zero external dependencies for first deploy. Helm values.yaml MUST NOT set this field. Comments MUST document valid values (`direct`, `kafka`, `nats`)
- **FR-006a**: Broadcast bus (NATS Core pub/sub) and message backend (NATS JetStream persistent streams) are architecturally distinct. Their NATS addresses MUST be configured independently — broadcast via `NATS_URLS`, JetStream via `NATS_JETSTREAM_URLS`. When using the in-cluster NATS subchart, both are auto-wired to the same NATS instance. Operators MAY point either URL to a different cluster (in-cluster or external) for isolation
- **FR-007**: The Go `envDefault` for `DATABASE_DRIVER` MUST remain `sqlite` — zero external dependencies for first deploy. Helm values.yaml MUST NOT set this field. Comments MUST document valid values (`sqlite`, `postgres`)
- **FR-007a**: The `PROVISIONING_MODE=config` (YAML file-backed) mode MUST be removed from the provisioning service. Provisioning always uses the database (SQLite or PostgreSQL). This eliminates the `PROVISIONING_MODE` env var, `PROVISIONING_CONFIG_PATH`, and all config-mode code paths. The provisioning service becomes simpler: one mode, two database drivers
- **FR-005a**: For ALL configurable fields: the Go `envDefault` is the single source of truth for defaults. Helm values.yaml MUST NOT duplicate the default — it MUST only document the valid values as comments. Developers override by adding fields to their values file
- **FR-005b**: Where Go `envDefault` and Helm values currently disagree, Go MUST be updated to match the effective Helm subchart/parent value. After alignment, the Helm field is removed. Known mismatches: `BROADCAST_TYPE` Go=valkey → nats, `WS_MAX_BROADCAST_RATE` Go=20 → 25, `CONN_RATE_LIMIT_IP_BURST` Go=10 → 100, `CONN_RATE_LIMIT_IP_RATE` Go=1.0 → 100.0, `ENVIRONMENT` Go=development → local, `AUTH_ENABLED` (gateway) Go=true → false (temporary — see FR-018a)
- **FR-005c**: Infrastructure subcharts MUST default to minimal footprint: NATS enabled (JetStream disabled), Redpanda disabled, Valkey disabled, PostgreSQL disabled. Operators enable them when switching modes
- **FR-005d**: Every infrastructure dependency (NATS, Kafka/Redpanda, Valkey, PostgreSQL) MUST support the same topology flexibility across ALL deployment contexts:
  - **Local (Docker Compose)**: Dependencies run as local containers (default: minimal footprint) or the developer points to external services via env var overrides
  - **Kubernetes (any environment — dev, staging, production)**: Dependencies run as in-cluster subcharts (`subchart.enabled: true`, auto-wired from `{{ .Release.Name }}`) or external services (`subchart.enabled: false` + explicit URL)
  - The choice is always the operator's. Staging MAY mirror production topology or use a lighter setup. The application services are agnostic to where dependencies run — they receive addresses via environment variables regardless of deployment context
- **FR-005e**: The NATS subchart MUST support JetStream as a togglable feature (`nats.jetstream.enabled`). When `messageBackend=nats`, JetStream MUST be enabled on the in-cluster NATS (if using the subchart). A single NATS instance handles both broadcast (Core pub/sub) and message backend (JetStream) in-cluster. For production isolation, operators MAY point `NATS_JETSTREAM_URLS` to a separate NATS cluster (in-cluster or external) while keeping broadcast on the default NATS

#### Configuration Coherence

- **FR-008**: Every env var set in a Helm template MUST have a corresponding `env:` struct tag in Go — no orphaned template variables
- **FR-009**: Helm templates MUST NOT compute derived values (e.g., hysteresis bands via `sub`) — derived values MUST be computed in Go from the primary config fields
- **FR-010**: When a mode is selected (e.g., `messageBackend: kafka`), ONLY the fields relevant to that mode MUST be required — all other fields MUST be omitted from templates via conditionals

#### Pre-Install Config Validation

- **FR-014**: Each Go binary (ws-server, ws-gateway, provisioning) MUST support a `--validate-config` flag that loads environment variables, runs `Validate()`, prints any error to stdout, and exits with code 0 (valid) or 1 (invalid). No network connections, no service startup — config check only
- **FR-015**: Helm MUST include a `pre-install` and `pre-upgrade` hook Job for each service that runs the binary with `--validate-config`. If validation fails, `helm install`/`upgrade` aborts with the error visible in the developer's terminal — no pods are created
- **FR-016**: Validation error messages MUST name the exact env var, the current value (or "not set"), and what's expected. Format: `[CONFIG ERROR] KAFKA_BROKERS is required when MESSAGE_BACKEND=kafka`

#### Docker Compose Local Dev

- **FR-018**: The repository MUST include a `docker-compose.yml` at the repo root that starts all services (ws-gateway, ws-server, provisioning) plus NATS with zero configuration. The default profile starts the minimal footprint (direct mode + NATS broadcast + SQLite provisioning). Services MUST use Docker Compose service names for inter-service discovery (e.g., `nats://nats:4222`, `provisioning:9090`). All other config uses Go `envDefault` values
- **FR-018a**: The Go `envDefault` for `AUTH_ENABLED` (gateway) MUST be changed from `true` to `false` to match the current Helm subchart/parent default. This is documented as temporary — the default will change to `true` when sukko-api auth integration is production-ready. Since auth is off by default, Docker Compose needs no override. The `docker-compose.yml` MUST include commented instructions for enabling auth when ready to test it. The Go config MUST include a TODO comment: `// TODO: Change envDefault to "true" when sukko-api auth integration is production-ready`
- **FR-018b**: Docker Compose MUST support the same topology flexibility as Helm — developers can compose any environment configuration locally by enabling additional services (Redpanda/Kafka for message backend, NATS with JetStream, PostgreSQL for provisioning, Valkey for broadcast) and setting the corresponding env vars. Docker Compose profiles or commented service blocks MUST make it easy to switch modes without editing service definitions
- **FR-019**: The `docker-compose.yml` MUST expose gateway on `localhost:3000` for WebSocket client connections. Developer connects and it works immediately
- **FR-020**: The `docker-compose.yml` MUST include commented-out service blocks and environment overrides showing how to switch to each mode (e.g., uncomment Redpanda service + set `MESSAGE_BACKEND=kafka`, uncomment PostgreSQL service + set `DATABASE_DRIVER=postgres`). These serve as inline documentation matching the Helm decision guide

#### Config & Version Introspection

- **FR-022**: Each Go binary (ws-server, ws-gateway, provisioning) MUST expose `GET /config`, `GET /health`, and `GET /version` endpoints on its admin/operational HTTP mux (same as `/metrics`). These are **internal endpoints** — used by Kubernetes probes (`/health`), Prometheus (`/metrics`), and operators querying individual services within the cluster. The `/config` endpoint MUST return the effective runtime configuration as JSON with sensitive fields (passwords, tokens, SASL credentials, TLS private keys, database connection strings containing credentials) redacted as `"[REDACTED]"`
- **FR-023**: The existing `/version` endpoint (already implemented on all three binaries via `internal/shared/version/`) MUST be preserved as-is. It returns `{version, commit_hash, build_time, service}` with values injected via `-ldflags` at build time. Docker Compose services MUST pass build args so `/version` returns meaningful values even in local dev (at minimum, the commit hash from `git rev-parse --short HEAD`)
- **FR-024**: *(REMOVED)* Gateway aggregation of `/health`, `/config`, `/version` is not needed. Each service exposes its own internal endpoints and operators query them individually within the cluster (e.g., via `kubectl port-forward` or K8s service DNS). No aggregator, no gateway admin URL config

#### Deployment Mode Guide

- **FR-017**: The parent `values.yaml` MUST begin with a commented decision tree that helps developers choose the right mode without understanding internals. The guide MUST cover: zero-config defaults, message persistence (Kafka vs JetStream), production database (PostgreSQL), and broadcast bus override (Valkey). Each option MUST show the exact fields to set (1-3 lines max per mode) and reference the corresponding example file (e.g., "See examples/helm/values-kafka.yaml for a complete example"). The guide MUST also explain the two deployment paths: Docker Compose for local dev, Helm for K8s clusters

#### Example Configurations

- **FR-011**: The repository MUST include example values files demonstrating each deployment mode. Each example MUST be a complete, copy-paste-ready values file that a developer can use immediately — not a fragment requiring guesswork:
  - `examples/helm/values-minimal.yaml` — zero-config (direct + NATS + SQLite). Shows what you get out of the box with comments explaining each default
  - `examples/helm/values-kafka.yaml` — Kafka/Redpanda mode. Includes: broker address, SASL auth (commented with instructions), TLS (commented with instructions), topic namespace, consumer group settings. Every field a developer might need for Kafka is present — commented if optional, uncommented if required
  - `examples/helm/values-nats-jetstream.yaml` — NATS JetStream mode. Includes: JetStream URLs, stream replica count, max age, TLS (commented). Shows both in-cluster (subchart) and external (managed) configurations
  - `examples/helm/values-production.yaml` — production-grade with PostgreSQL + Kafka + TLS + resource limits + replicas + node affinity. A realistic production template with every production-relevant field filled in or commented with guidance
- **FR-011a**: *(DEFERRED)* Generic GKE deployment examples (`examples/gke/`) — terraform, helm values, README. Deferred to a future iteration
- **FR-012**: Each example file MUST include inline comments that explain: what each field does, when and why to change it, what the Go default is (so the developer knows what happens if they omit the field), and links to related fields (e.g., "if you set messageBackend: kafka, you must also set kafka.brokers"). The examples are the primary onboarding documentation for new developers deploying Sukko
- **FR-013**: Example Helm values files MUST be validated by `helm lint` and `helm template` in CI

#### Sukko Internal Deployment Update

- **FR-021**: Sukko's internal Helm environment overrides (`dev.yaml`, `stg.yaml`, `prod.yaml`) MUST be rewritten to only contain deployment-level settings (replicas, resources, image tags, tolerations), mode selections (message backend, database driver, broadcast type), and infrastructure topology (which subcharts are enabled, external service URLs) — consistent with FR-004. Fields that now match Go defaults MUST be removed. The resulting deployment MUST produce identical runtime behavior to the current deployment. Taskfiles: adjust with caution only if the new Helm values structure breaks existing targets

### Non-Functional Requirements

- **NFR-001**: A developer MUST be able to deploy a working system with `docker compose up` in under 60 seconds (first build excluded). Helm is for real Kubernetes clusters (dev/staging/production), not local dev
- **NFR-002**: Switching between modes (direct/kafka/nats, sqlite/postgres, nats/valkey) MUST require changing no more than 3 fields in the values override
- **NFR-003**: The total line count of subchart `values.yaml` files MUST be reduced by at least 50% from current state
- **NFR-004**: All existing deployments (dev/stg/prod) MUST continue to work after the cleanup. Where Go defaults change (e.g., BROADCAST_TYPE valkey→nats), the existing override files MUST be updated to explicitly set the previous value. A migration checklist MUST be provided documenting every default that changed

## Success Criteria

- **SC-000**: `docker compose up` from repo root starts all services and accepts WebSocket connections within 60 seconds — the primary "try it out" path
- **SC-001**: `helm install sukko ./deployments/helm/sukko -f values/standard/dev.yaml` succeeds on the dev GKE cluster with the cleaned-up override file
- **SC-002**: All three services reach healthy state within 60 seconds of `docker compose up`
- **SC-003**: Each example values file passes `helm lint` and `helm template` without errors
- **SC-004**: Existing `values/standard/{dev,stg,prod}.yaml` deployments produce equivalent runtime behavior after the cleanup — override files are updated per migration checklist, manifests may differ (fewer env vars set explicitly) but application behavior is identical
- **SC-005**: ws-server `values.yaml` is under 100 lines (currently ~290)
- **SC-006**: provisioning `values.yaml` is under 80 lines (current size TBD)

## Out of Scope

- Adding new features or configuration options — this is cleanup and simplification only (removing config mode from provisioning per FR-007a is an intentional simplification)
- Changing application behavior — Go `envDefault` values change to align with Helm production defaults (per FR-005b), but runtime behavior for existing deployments is preserved via migration checklist
- Local Kubernetes deployment (Kind, minikube, Docker Desktop) — too resource-heavy for dev machines. Docker Compose is the local dev path
- GKE example deployment files (`examples/gke/`) — deferred to future iteration (FR-011a)
- Terraform module restructuring — existing `deployments/terraform/` module architecture stays the same. No new modules are created
- CI/CD pipeline changes (beyond adding `helm lint` for example files)
- Creating a Helm chart repository or OCI packaging
- Gateway config struct (`gateway_config.go`) already exists — no changes needed beyond default alignment (FR-005b). No new config fields added (FR-024 removed)
- Taskfiles: adjust with caution only if the new Helm values structure breaks existing targets

## Clarifications

- Q: FR-005 changes BROADCAST_TYPE default from `valkey` to `nats` — how do we handle backward compatibility for existing deployments? → A: Go `envDefault` is the single source of truth for defaults. Helm values.yaml MUST NOT duplicate the default — it only documents valid values as comments. Existing deployments that need non-default values (e.g., `valkey` for broadcast) add the field to their override file. The migration checklist documents every default that changed.
- Q: What is the default infrastructure footprint for a zero-config deploy? → A: Minimal: SQLite for provisioning, NATS for broadcast bus, direct for message backend (no backend at all). For ALL configurable fields (not just these three), whatever the current Helm value is becomes the Go `envDefault`, then the Helm field is removed.
- Q: SC-004 requires existing deployments to produce "equivalent" manifests — should this be strict YAML equality or equivalent runtime behavior? → A: Relaxed to "equivalent runtime behavior". Manifests may differ (fewer env vars set explicitly since Go defaults handle them), but application behavior MUST be identical. Override files are updated per migration checklist.
- Q: How should services discover each other in the zero-config Kubernetes deploy? → A: Helm auto-computes inter-service addresses from `{{ .Release.Name }}` (e.g., `nats://{{ .Release.Name }}-nats:4222`, `{{ .Release.Name }}-provisioning:9090`). These are internal wiring, not user-configurable fields. Go `envDefault` values remain `localhost:*` for local binary execution without K8s.
- Q: How should configuration errors surface to the developer? → A: Helm pre-install validation hook. Each binary supports `--validate-config` flag (load env, run Validate(), exit 0/1). Helm Job runs this as pre-install/pre-upgrade hook — if validation fails, `helm install` aborts with a clear error in the terminal. No pods are created for invalid config.
- Q: When broadcast.type=nats and messageBackend=nats, should they share config? → A: Always separate. Broadcast bus uses NATS Core (pub/sub), message backend uses NATS JetStream (persistent streams). JetStream can be in-cluster (subchart) or external (managed), so its address is independent from the broadcast NATS address. Both are auto-wired by Helm via `{{ .Release.Name }}` when using in-cluster subcharts, but configured independently. Switching to JetStream requires `messageBackend: nats` — Helm auto-computes `NATS_JETSTREAM_URLS` from the release name. For external JetStream, the developer overrides `natsJetStream.urls`.
- Q: Should the spec require a decision guide for choosing deployment modes? → A: Yes, inline at the top of parent `values.yaml`. A commented decision tree showing: zero-config defaults, persistence options (Kafka vs JetStream), production DB (PostgreSQL), broadcast override (Valkey). Each option shows exact fields to set (1-3 lines). Developers see it the moment they open the file.
- Q: Should the spec include a local dev path without Kubernetes? → A: Yes, docker-compose is the primary "try it out" path. `docker compose up` from repo root starts everything — no Kind, no kubectl, no Helm. Kubernetes/Helm is for deployment testing and production. Docker Compose is included in this spec because it directly depends on the Go default alignment work (FR-005b) and is the highest-impact devex improvement.
- Q: How should auth work for docker-compose local dev? → A: Go `envDefault` for `AUTH_ENABLED` is `false` (temporary — see FR-018a). Auth is off by default everywhere (local binary, Docker Compose, fresh K8s). Docker Compose needs no override. Developer connects immediately — `docker compose up` → `wscat -c ws://localhost:3000` → working. Auth can be enabled by setting `AUTH_ENABLED=true` and provisioning tenants via API.
- Q: Should infrastructure subcharts be in-cluster only for dev, or also valid for production? → A: In-cluster subcharts are a first-class deployment option at ALL environment levels (dev, staging, production). The choice between in-cluster (subchart) and external (managed service) is the operator's — it is NOT tied to environment. Every dependency (NATS, Kafka/Redpanda, Valkey, PostgreSQL) follows the same pattern: enable subchart for in-cluster, disable + provide URL for external.
- Q: How should NATS handle both broadcast and JetStream? → A: One NATS subchart with JetStream as a togglable feature. When `messageBackend=nats`, JetStream is enabled on the in-cluster NATS — a single instance handles both broadcast (Core) and persistence (JetStream). For isolation, operators point `NATS_JETSTREAM_URLS` to a separate cluster (in-cluster or external).
- Q: Must staging mirror exact production topology? → A: No. Staging MAY mirror production (same external services, same modes) or use a lighter in-cluster setup. The Helm values structure supports both without template changes — environment override files select infrastructure topology independently per environment.
- Q: Does local dev (Docker Compose) support the same topology flexibility? → A: Yes. Docker Compose follows the same universal pattern — defaults to minimal footprint (SQLite provisioning, local NATS for broadcast, direct for message backend) but developers can compose any environment configuration by enabling additional services (Redpanda, PostgreSQL, Valkey, NATS JetStream) and setting env vars. Docker Compose is the local mechanism; Helm subcharts are the Kubernetes mechanism. The application services are agnostic to which mechanism provides the dependency.
- Q: Should provisioning support both API mode and config mode? → A: No. Remove `PROVISIONING_MODE=config` (YAML file-backed) entirely. Provisioning always uses the database — SQLite by default, PostgreSQL when overridden. One mode, two database drivers. This eliminates `PROVISIONING_MODE`, `PROVISIONING_CONFIG_PATH`, and all config-mode code paths.
- Q: Should the repo include sample GKE deployment files? → A: Yes. Provide `examples/gke/` with sample Terraform (GKE cluster, VPC, Cloud NAT, Artifact Registry, IAM), Helm values for GKE, and a step-by-step README. Based on existing `deployments/terraform/` but simplified and self-contained as examples. Existing Terraform is not modified.
- Q: Are example Helm and Terraform files helpful for developers? → A: Yes. Example Helm values files (`examples/helm/`) show the exact 1-5 fields needed per deployment mode — eliminates guesswork. Minimal reference Terraform (`examples/gke/terraform/`, ~100 lines) shows the GKE cluster shape the Helm chart deploys onto — enough to fork and adapt, not a production module. The examples are generic reference material, separate from Sukko's internal deployment which is updated separately (FR-021) to be backend-agnostic.
- Q: What is the implementation scope? → A: Option 4 — All except GKE examples. Keep: Go default alignment, Helm values cleanup, config mode removal, Docker Compose, `--validate-config` + Helm pre-install hooks, example Helm values files (values-minimal/kafka/jetstream/production), decision guide in parent values.yaml. Defer ONLY: FR-011a (GKE terraform/helm examples). Taskfiles: adjust with caution if needed. Terraform modules: not restructured.
- Q: What should the Go default be for gateway AUTH_ENABLED? Subchart and parent both set `false`, Go default is `true`. → A: Go default = `false`, documented as temporary. Add TODO in Go: "Change to true when sukko-api auth integration is production-ready." This matches current Helm behavior (auth off by default). The migration checklist notes this will be a future breaking change when auth is enabled by default.
- Q: Should there be endpoints for viewing configuration and version/build info? → A: **Version (`/version`)**: Already exists on all three binaries via `internal/shared/version/` package with ldflags injection. Returns `{version, commit_hash, build_time, service}`. No changes needed — preserved as-is (FR-023). **Config (`/config`)**: New endpoint needed on all three binaries. Returns effective runtime configuration as JSON with sensitive fields redacted. Registered on the admin/operational HTTP mux alongside `/health` and `/metrics` (FR-022).
- Q: Should /health, /config, and /version be accessible via the gateway for all services? → A: No aggregation needed. Each service exposes its own `/health`, `/config`, `/version` endpoints on its internal admin mux. Operators query individual services directly within the cluster (e.g., `kubectl port-forward`, K8s service DNS). No gateway aggregation layer — simpler, fewer moving parts (FR-024 REMOVED).
- Q: Should per-binary /health, /config, /version endpoints be removed? → A: No — they MUST stay as **internal** endpoints on every binary. K8s liveness/readiness probes hit each pod's `/health` directly. Prometheus scrapes each pod's `/metrics` directly. Operators use `/config` and `/version` for debugging via direct service access (FR-022).
- Q: Should local Kubernetes (Kind/minikube) be supported as a dev workflow? → A: No. Local K8s is too resource-heavy for dev machines. Docker Compose is the local dev path (`docker compose up` — zero config, works on any machine with Docker). Helm is for real Kubernetes clusters (dev/staging/production on GKE), not local K8s. Scenario 1b (local K8s deploy) removed. SC-001 updated to validate Helm on real clusters (dev GKE), not local Kind.
- Q: Should example Helm values files be kept or deferred since external distribution isn't the primary concern? → A: Keep all examples (FR-011) and make them comprehensive. Examples must be copy-paste-ready, not fragments. Every field a developer might need for each mode should be present — required fields uncommented, optional fields commented with instructions. The examples are the primary onboarding documentation for developers deploying Sukko on their own K8s clusters. The decision guide (FR-017) references each example file.
- Q: How should removal of provisioning config reload metrics (provisioning_config_reload_total, _failures_total, _duration_seconds) be handled? → A: No action needed. Production deployment doesn't use config mode, so these metrics are unused. Remove them along with config mode code — no migration checklist entry required.
- Q: Should the /config endpoint cache its reflection result to avoid per-request reflection on the shared hot-path mux? → A: Yes — cache at startup. `RedactConfig()` runs once during handler creation and the result is captured in the closure. Subsequent requests serialize the cached map with zero reflection at request time. Config is immutable after startup so caching is safe. This follows the same pattern as `/version` (static data computed once). A separate admin port is a good future improvement but out of scope for this cleanup feature.
- Q: How should the broadcast factory handle empty BROADCAST_TYPE after the default changes to nats? → A: Reject empty in `Validate()`. If `BROADCAST_TYPE` is unset, envDefault provides `"nats"` (zero config works). If explicitly set to empty (`BROADCAST_TYPE=`), `Validate()` rejects with `[CONFIG ERROR] BROADCAST_TYPE="" is invalid (valid: nats, valkey)`. Remove the `case ""` fallback from `broadcast.NewBus()` factory — the factory only receives validated values. This follows Constitution I: invalid config fails at startup, not silently.
- Q: Should ENVIRONMENT be validated against a fixed set (local/dev/stg/prod) or be free-form for distribution compatibility? → A: Free-form, document conventions. `ENVIRONMENT` is not validated against a fixed set — any string works as a deployment identity label (used for Kafka topic namespace, consumer group naming, alerts). envDefault is `"local"` for zero-config local dev. Sukko internally uses `local/dev/stg/prod` by convention. Specific values trigger safety behavior: `"prod"` blocks `KAFKA_TOPIC_NAMESPACE_OVERRIDE`, `"dev"`/`"development"`/`"local"` relaxes admin token length requirements. External users can use any naming — if they want prod safety guards, they use `"prod"`. Document these conventions in inline comments near the `Environment` field and in the decision guide.
