# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Sukko is a multi-tenant WebSocket infrastructure platform built in Go. It provides real-time data distribution for trading/market data via a gateway → server → client pipeline, with Kafka/Redpanda ingestion and NATS broadcast.

**Services:**
- **ws-gateway** — WebSocket reverse proxy with JWT auth, tenant isolation, rate limiting, connection tracking
- **ws-server** — Core WebSocket server with sharded connections, Kafka consumption, NATS broadcast
- **provisioning** — Multi-tenant provisioning API (tenants, API keys, topics, channel rules)

## Development Commands

```bash
# Go
cd ws && go test -race ./...    # Run all tests with race detector
cd ws && go vet ./...           # Static analysis
cd ws && go build ./cmd/server  # Build ws-server
cd ws && go build ./cmd/gateway # Build gateway

# Build & Deploy (via Taskfile)
task k8s:deploy ENV=demo        # Helm deploy to DOKS

# Helm
helm lint deployments/helm/sukko/charts/ws-server
helm lint deployments/helm/sukko/charts/ws-gateway

# Terraform
cd deployments/terraform/environments/do/demo && terraform plan

# Logs
kubectl logs -n sukko-demo -l app.kubernetes.io/name=ws-server --tail=50
kubectl logs -n sukko-demo -l app.kubernetes.io/name=ws-gateway --tail=50
```

## Architecture

### Data Flow
```
Sukko API → Redpanda (Kafka) → ws-server (franz-go consumer)
    → NATS broadcast bus → ws-server shards → WebSocket clients
                                    ↑
                              ws-gateway (reverse proxy, auth, rate limiting)
```

### Source Structure
```
ws/
├── cmd/
│   ├── server/          # ws-server entrypoint
│   └── gateway/         # ws-gateway entrypoint
├── internal/
│   ├── gateway/         # Gateway: proxy, auth, tenant tracking
│   ├── server/          # Server: shards, connections, pumps, handlers
│   │   ├── limits/      # ResourceGuard, rate limiters
│   │   ├── metrics/     # Prometheus metrics, SystemMonitor
│   │   └── orchestration/ # Multi-tenant consumer pool
│   └── shared/          # Shared across gateway + server
│       ├── auth/        # JWT, OIDC, channel patterns
│       ├── broadcast/   # NATS/Valkey broadcast bus
│       ├── kafka/       # franz-go consumer/producer
│       ├── platform/    # Config structs (env tags)
│       ├── protocol/    # WebSocket protocol types
│       ├── types/       # Core type definitions
│       └── testutil/    # Shared test utilities
├── proto/               # Protobuf definitions (buf-managed)
│   └── sukko/provisioning/v1/ # Provisioning gRPC service
deployments/
├── helm/sukko/           # Helm charts (ws-server, ws-gateway, monitoring, etc.)
│   ├── charts/          # Subchart definitions
│   └── values/doks/     # Environment overrides (demo.yaml)
├── terraform/           # DOKS + GKE clusters, reserved IPs
│   ├── environments/doks/demo/ # DOKS demo environment
│   ├── environments/gke/demo/  # GKE demo environment
│   └── modules/              # doks-cluster, gke-foundation, gke-standard-cluster
docs/architecture/       # Plans, findings, session handoffs
```

### Key Technologies
- **Go 1.26+** with modern features (any, slices, maps, for range N, errors.Join, wg.Go, errors.AsType)
- **franz-go** for Kafka/Redpanda consumption (consumer groups, partition management)
- **NATS** for inter-pod broadcast (publish/subscribe)
- **gRPC** + **protobuf** for internal service-to-service communication (buf for codegen)
- **gorilla/websocket** for WebSocket connections
- **zerolog** for structured logging
- **Prometheus** for metrics (promauto registration)
- **Helm 3** for Kubernetes deployments
- **Terraform** for DOKS cluster infrastructure
- **Taskfile** for build/deploy orchestration
- **Docker** multi-stage builds → GitHub Container Registry (ghcr.io)

### Configuration Pattern
All configuration uses `caarlos0/env` struct tags. Go `envDefault` values are the single source of truth for defaults. Fields shared across services live in `platform.BaseConfig` (embedded by each service config):
```go
type BaseConfig struct {
    LogLevel    string `env:"LOG_LEVEL" envDefault:"info"`
    LogFormat   string `env:"LOG_FORMAT" envDefault:"json"`
    Environment string `env:"ENVIRONMENT" envDefault:"local"`
}

type ServerConfig struct {
    BaseConfig // embedded — fields promoted, parsed transparently by caarlos0/env
    Port int   `env:"SERVER_PORT" envDefault:"8080"`
}
```
Helm and Docker Compose override via env vars only when a deployment needs a non-default value. Helm templates auto-wire Kubernetes service discovery (e.g., `{{ .Release.Name }}-nats`). Env var names in Go MUST match Helm template values.

## Commit Message Format

Conventional commits:
```
type: subject (min 4 chars)

Examples:
feat: add tenant connection tracking
fix: resolve kafka consumer offset reset
refactor: remove legacy metrics collector
```

Valid types: feat, fix, docs, style, refactor, perf, test, build, ci, chore, revert

## Pre-commit Hook

Install: `git config core.hooksPath .githooks`

Runs automatically: Go formatting, go vet, golangci-lint, Helm lint, binary check, secrets scan.

## Constitution

**Version**: 1.14.0 | **Ratified**: 2026-02-17 | **Last Amended**: 2026-04-07

### I. Configuration

Every configurable parameter MUST be externalized via environment variables with `env:` struct tags. Go `envDefault:` values are the **single source of truth** for all configuration defaults — they MUST reflect production-intended values. Helm values and Docker Compose MUST NOT duplicate Go defaults; they override via env vars ONLY when a deployment requires a value different from the Go default (e.g., Kubernetes service discovery addresses, mode selections, resource-derived limits). Helm templates MUST NOT compute derived values — derived configuration MUST be computed in Go. Magic numbers MUST be named constants or configuration. Magic strings (URLs, broker addresses, topic names, tenant IDs, namespace prefixes) MUST NOT be hardcoded — they MUST come from configuration or be constructed from configured values. All configuration MUST be validated at startup with clear error messages. Hysteresis thresholds MUST enforce `lower < upper`. Enum values MUST be validated against allowed sets. Invalid configuration MUST cause immediate startup failure, not silent degradation. Configuration fields shared across multiple services (e.g., `Environment`, `LogLevel`, `LogFormat`) MUST be defined once in `platform.BaseConfig` and embedded by each service config — never duplicated across config structs. Shared and internal packages MUST NOT define their own defaults for deployment-level or runtime-critical settings (e.g., environment, namespace, tenant ID); they MUST receive these values from callers. Constructors and initializers that accept configuration from external or unvalidated sources MUST validate required fields and return an error when critical values are missing or empty — silent defaulting that could cause the system to run with wrong state is forbidden. Constructors that receive config structs already validated by the platform config layer (`Validate()` at startup) are exempt from re-validation; the config layer is the single validation boundary for deployment-level settings. CLI flags (e.g., `flag.Int`) MUST use layered defaults: load config from env vars first, then use the config value as the flag default — so the precedence is CLI flag > env var > `envDefault`. CLI flags MUST NOT define their own independent defaults; doing so creates a second source of truth that diverges from the env var config. Example: `flag.Int("shards", cfg.NumShards, "...")` where `cfg.NumShards` comes from `env:"WS_NUM_SHARDS" envDefault:"3"`.

### II. Defense in Depth

Every layer MUST validate its inputs. Never assume upstream validation. The gateway validates, and the server validates again. Input validation at ALL system boundaries is mandatory.

### III. Error Handling

All errors MUST be wrapped with context using `fmt.Errorf("operation: %w", err)`. Sentinel errors MUST be defined for expected conditions. Ignored errors MUST have an explicit comment explaining why. Silent failures are forbidden.

### IV. Graceful Degradation

Optional dependencies MUST use noop implementations or nil-guarded feature flags — never half-initialized state. Multi-step cleanup MUST continue on individual failures. Health endpoints MUST report degraded state. Retry logic MUST use exponential backoff with a cap. Slow WebSocket clients MUST be detected and disconnected via circuit breaker.

### V. Structured Logging

All logging MUST use zerolog with structured fields (Str, Int, Dur, Err). Appropriate log levels MUST be used (Debug/Info/Warn/Error/Fatal). No `log.Printf` or `fmt.Println`.

**Panic Recovery** — All panic recovery MUST use `defer logging.RecoverPanic(...)`. Inline `defer func() { recover() }()` is forbidden — it silently swallows panics without logging, making production debugging impossible. This applies to both goroutine entry points (`wg.Go` functions, per VII) and inline protective wrappers (e.g., closing resources that may panic due to concurrent state).

### VI. Observability

Every significant operation MUST have Prometheus metrics. Metric names MUST use `ws_` prefix (server), `gateway_` prefix (gateway), or `provisioning_` prefix (provisioning service) with units (`_seconds`, `_bytes`, `_total`). Labels MUST be used sparingly to avoid cardinality explosion. Histograms MUST be used for latency, not summaries. **Excluded from Prometheus**: The tester service (`cmd/tester`) is a test/debugging tool, not a production service — it uses its own built-in `metrics.Collector` (atomic counters) and `stats.Histogram` with SSE streaming for real-time observability. Prometheus integration MUST NOT be added to the tester.

**Tracing and Profiling** — Distributed tracing (OpenTelemetry) and continuous profiling (pprof/Pyroscope) are opt-in observability features, disabled by default. When disabled, they MUST have zero performance overhead — no goroutines, no allocations, no network calls. Noop implementations MUST be used (e.g., noop `TracerProvider`). Tracing MUST only instrument cold paths (auth flows, config changes, provisioning API, consumer setup) — per-message hot paths (proxy forwarding, broadcast fan-out, write pump) MUST NOT be traced, as Prometheus histograms already cover per-message latency. If the tracing exporter is slow or unreachable, spans MUST be dropped silently — never queued unboundedly, never blocking callers.

### VII. Concurrency Safety

This is a high-performance WebSocket server handling thousands of concurrent connections per pod. Incorrect concurrency primitives cause panics, deadlocks, goroutine leaks, and silent data corruption. Every concurrent pattern MUST follow the established patterns below.

**Design Preference** — Prefer goroutine ownership over shared memory with locks. When a piece of state needs concurrent access, the first choice SHOULD be a dedicated goroutine that owns the state and communicates via channels (Go proverb: "share memory by communicating"). Mutexes are acceptable for simple read-heavy caches (`sync.RWMutex`) and atomic counters, but for stateful operations (connection lifecycle, subscription tracking, auth flow), a single-owner goroutine with channel-based communication is safer and eliminates lock-ordering concerns.

**Goroutine Lifecycle** — All goroutines MUST be launched via `wg.Go()` (Go 1.25+), which handles `Add(1)` before launch and `Done()` after the function returns. The function passed to `wg.Go()` MUST follow this structure:
1. `defer logging.RecoverPanic(...)` MUST be the FIRST `defer` inside the function body.
2. The function MUST NOT call `wg.Done()` — `wg.Go()` calls it automatically. Calling `Done()` inside a `wg.Go()` function causes a double-Done, driving the WaitGroup counter negative and panicking at runtime.
3. The goroutine MUST check `ctx.Done()` in its main loop via `select` for shutdown signaling.
4. `wg.Wait()` MUST be called in the shutdown/stop path to ensure all goroutines have exited before resources are released.

**`wg.Go()` with inline closures** (preferred for short-lived or context-capturing goroutines):
```go
wg.Go(func() {
    defer logging.RecoverPanic(logger, "component_name", nil)
    doWork(ctx)
})
```

**`wg.Go()` with named methods** (preferred for long-lived goroutines with their own loops):
```go
wg.Go(s.runLoop)
// Inside runLoop: NO defer wg.Done() — wg.Go handles it.
func (s *Service) runLoop() {
    defer logging.RecoverPanic(s.logger, "runLoop", nil)
    for { select { case <-s.ctx.Done(): return } }
}
```

**CRITICAL — Modernization safety rule**: When converting legacy `wg.Add(1); go method()` patterns to `wg.Go(method)`, the `defer wg.Done()` inside the method body MUST be removed in the same change. Failing to do so causes double-Done. Both the call site and the method body MUST be updated atomically — never one without the other.

Shutdown ordering MUST be: cancel context → `wg.Wait()` for goroutines → close channels → release resources. Reversing this order (e.g., closing a channel before its goroutine exits) causes panics.

**Channels** — Channel type MUST match usage pattern:
- **Signal/stop channels** (`chan struct{}`): Used for shutdown signaling. MUST be closed by exactly one goroutine. If multiple goroutines may attempt close, MUST use `sync.Once` to guard the `close()` call. Sending on a closed channel panics — this is unrecoverable in production.
- **Data channels** (e.g., `chan OutgoingMsg`): MUST be buffered with a size matching throughput requirements. Unbuffered channels MUST NOT be used in hot paths (message distribution, broadcast fan-out) because a single slow receiver blocks all senders.
- **Semaphore channels** (`chan struct{}` with capacity = limit): Used for resource limiting (max connections, max goroutines). Acquire MUST be non-blocking (`select` with `default` case) to reject callers at capacity rather than queueing them indefinitely.
- **Fan-out sends** (broadcast to multiple subscribers): MUST use non-blocking `select` with `default` to skip slow consumers. Dropped messages MUST be counted via Prometheus metrics (`_dropped_total`). A single slow subscriber MUST NOT block delivery to all other subscribers.
- **Channel close rules**: Only the sender side MUST close a channel — never the receiver. After closing, no further sends are permitted (panic). When a channel may be closed from multiple code paths, guard with `sync.Once`. When draining a channel before reuse (e.g., `sync.Pool`), use `select` with `default` in a loop.

**WaitGroups** — `sync.WaitGroup` is for goroutine lifecycle tracking only. `wg.Go(func())` is the ONLY permitted launch pattern — manual `wg.Add(1); go func() { defer wg.Done(); ... }()` is legacy and MUST NOT be introduced in new code. Additional constraints:
- `wg.Wait()` SHOULD have a timeout mechanism (e.g., wrapper with `context.WithTimeout`) to detect stuck goroutines during shutdown rather than hanging indefinitely.
- WaitGroups MUST NOT be reused after `Wait()` returns for a given set of goroutines.
- Functions passed to `wg.Go()` MUST NOT call `wg.Done()` — this is the single most common modernization bug and causes a runtime panic.

**Mutexes** — Locks MUST protect data, not code:
- `sync.RWMutex` MUST be used for read-heavy data (caches, subscription maps, metrics snapshots) where reads vastly outnumber writes. `sync.Mutex` MUST be used only when writes are as frequent as reads.
- Critical sections MUST be minimal: lock → read/write shared data → unlock. Mutexes MUST NOT be held across I/O operations (network calls, disk reads, channel sends, HTTP requests). Holding a lock across I/O blocks all other goroutines waiting for that lock, destroying throughput.
- `defer mu.Unlock()` / `defer mu.RUnlock()` MUST be used to prevent deadlocks from early returns or panics. Inline `Unlock()` without defer is forbidden.
- Nested mutex acquisition (locking mutex A while holding mutex B) MUST follow a consistent global ordering to prevent deadlocks. If ordering cannot be guaranteed, restructure to avoid nesting.
- Mutex values MUST NOT be copied. Structs containing a mutex MUST be passed by pointer and MUST NOT be assigned by value.

**Atomics** — Lock-free operations for hot-path counters and flags:
- `atomic.Int64` MUST be used for hot-path counters (messages sent/received, bytes, connection counts) instead of mutex-protected `int64`. Lock contention on frequently-incremented counters degrades throughput under load.
- `atomic.Bool` MUST be used for status flags read frequently in hot paths (health status, circuit breaker state, shutdown flag).
- `atomic.Value` SHOULD be used for periodic snapshot caching (e.g., subscriber lists rebuilt on subscription change, read lock-free on every broadcast). `Store()` replaces the snapshot; readers use `Load()` with zero contention.

**sync.Pool** — `sync.Pool` MUST be used for frequent allocations in hot paths (per-connection `Client` objects, message buffers). Objects retrieved via `Get()` MUST be fully reset before reuse: drain all channels (non-blocking `select` loop), clear all maps, zero all fields. Returning a partially-reset object causes state leakage between connections.

**sync.Once** — `sync.Once` MUST be used when an operation must execute exactly once across concurrent goroutines: connection close (`net.Conn.Close()`) and singleton initialization. Calling `Close()` twice on a `net.Conn` panics — `sync.Once` prevents this. Channel close guarding is covered in Channels close rules above.

**Message Pipeline Protection** — The message delivery pipeline (ingestion → broadcast bus → shard fan-out → per-client write pump → transport write) is the critical hot path. This applies to all transport types (WebSocket, SSE/gRPC stream, future Web Push). Feature-level operations (auth refresh, subscription management, metrics collection, provisioning lookups, OIDC validation) MUST NOT introduce blocking on this path. Specifically:
- Locks acquired for feature operations MUST NOT be held while calling into the pipeline (`forwardFrame`, `sendToClient`, `bus.Publish`, write pump sends).
- Feature operations that run on the client→backend goroutine MUST complete without waiting for backend responses when the wait would stall message reads from the client.
- Backend→client forwarding MUST remain non-blocking: observational interception (subscription tracking, metrics) MUST NOT add latency that degrades broadcast throughput.

### VIII. Testing

Tests MUST be run with Go's race detector (`-race` flag) in local development and CI. The race detector is the primary automated enforcement mechanism for VII (Concurrency Safety). Test runs without `-race` MUST NOT be considered passing. Tests MUST be table-driven for multiple cases. Mocks MUST use interfaces. `t.Parallel()` MUST NOT be used on tests with shared resources (databases, external services, `*_shared_test.go`). Edge cases MUST be covered (empty, nil, max values, error paths).

**Test coverage for all changes is mandatory:**
- **Bug fixes** MUST update or add unit tests that reproduce the bug and verify the fix. If existing tests missed the bug, they MUST be strengthened.
- **Enhancements and refactors** MUST update existing tests to reflect the changed behavior and add tests for any new code paths introduced.
- **New features** MUST include comprehensive unit tests covering: happy path, error paths, edge cases, and concurrency safety (where applicable).
- No code change (bug fix, enhancement, refactor, or feature) MUST be considered complete without corresponding test updates. Untested code changes are forbidden.

### IX. Security

**Rate Limiting** — Rate limiting MUST be applied at multiple levels (global, per-IP, per-tenant). Rate limit responses MUST use HTTP 429 with `Retry-After` header.

**Secrets Management** — Secrets MUST never appear in logs, error messages, or API responses. Sensitive keys and credentials (license keys, VAPID private keys, push provider credentials, API secrets) MUST be encrypted at rest in storage and decrypted only at the point of access — never stored as plaintext in databases, config files, or context stores. Encryption keys MUST be managed separately from the data they protect. Credential rotation MUST be supported without downtime — all credentials (JWT signing keys, API keys, VAPID keys, push provider credentials, admin tokens) MUST be rotatable via API while the system continues serving.

**Authentication & Replay Protection** — JWT validation MUST verify expiration (`exp`), issuer (`iss` when configured), and signature. JWTs MUST include `iat` (issued-at) and `exp` claims — tokens without expiration MUST be rejected. Token replay MUST be mitigated: short-lived tokens (≤15 min default), `jti` (JWT ID) claim SHOULD be used for critical operations, and the auth refresh protocol MUST issue a new token (not extend the old one). API key authentication MUST use constant-time comparison to prevent timing attacks. Webhook endpoints (e.g., license reload) MUST validate a shared secret — default deny if no secret is configured.

**Tenant Isolation** — Every data path MUST enforce tenant boundaries. Cross-tenant data leakage is a critical severity bug. Kafka topics, NATS subjects, broadcast subjects, WebSocket subscriptions, and push subscriptions MUST be scoped to the authenticated tenant. Provisioning API MUST enforce `RequireTenant()` middleware — a tenant JWT MUST NOT access another tenant's resources. Database queries MUST include `tenant_id` in WHERE clauses — no unscoped queries that could return cross-tenant data.

**Admin & Operator Endpoints** — All admin and operator endpoints MUST require authentication (admin token or equivalent). Default deny — if no admin token is configured, admin endpoints MUST reject all requests, not allow anonymous access. The `/config` endpoint MUST redact sensitive fields (fields tagged `redact:"true"`).

**Transport Security** — TLS MUST be enforced for all external-facing endpoints in production. Internal service-to-service communication (gRPC, NATS, Kafka) SHOULD use TLS when crossing network boundaries. TLS configuration MUST support custom CA certificates for private PKI.

**CORS** — CORS allowed origins MUST be explicitly configured — no wildcard (`*`) in production. Preflight responses MUST NOT cache longer than `CORS_MAX_AGE` (default 3600s). Only required headers and methods MUST be allowed.

**Dependency Security** — Dependencies with known CVEs MUST NOT be merged. `govulncheck` SHOULD be run in CI. Dependency updates MUST be reviewed for breaking changes and security implications.

**Audit Trail** — Security-relevant operations MUST be audit-logged: tenant lifecycle changes, key creation/revocation, credential rotation, license reload, admin authentication attempts (success and failure), and rate limit triggers.

**Code Annotations** — `//nolint` or `#nosec` MUST include thorough written justification explaining why the suppression is safe. Debug and profiling endpoints (`/debug/pprof/`) MUST be disabled by default — they expose memory contents, goroutine stacks, and CPU profiles. They MUST only be enabled via explicit opt-in (`PPROF_ENABLED=true`) and SHOULD be restricted to internal networks in production. Input validation at boundaries is mandated by II.

### X. Shared Code Consolidation

Before writing any new utility, `internal/shared/` MUST be checked for existing implementations. Duplicate functions, error definitions, constants, and types across packages are forbidden. HTTP utilities MUST use `shared/httputil/`. Auth helpers MUST use `shared/auth/`. New shared code MUST have tests. Conversely, types, functions, constants, interfaces, and structs used by only one service MUST live in that service's package — never in `internal/shared/`, even if they serve a similar purpose to shared types. The shared package is exclusively for code referenced by multiple services. Service-specific code in shared violates separation of concern and creates false coupling.

### XI. Prior Art Research

Before designing any new feature or protocol extension, the implementation approach MUST be informed by how established real-time/WebSocket services have solved the same problem. Reference services: Pusher Channels, Ably, Socket.IO, Phoenix Channels, Centrifugo, NATS WebSocket. Research MUST identify: (1) the common industry pattern for the feature, (2) edge cases and failure modes that mature implementations handle, (3) where Sukko's architecture requires deviation from the common pattern — with documented rationale for the deviation. "Not invented here" solutions to solved problems are forbidden.

### XII. API Design

**REST** — All external-facing APIs (admin, CLI, third-party) MUST use REST over HTTP/JSON.
- Endpoints MUST be versioned via URL path (`/api/v1/`). Health, readiness, and metrics endpoints MUST be at root level (no version).
- Routes MUST be resource-oriented: `POST` (create, 201), `GET` (read, 200), `PATCH` (partial update, 200), `PUT` (full replace, 200), `DELETE` (remove, 200). State-change actions use `POST` on sub-resources (`/suspend`, `/reactivate`).
- Error responses MUST use the `httputil.ErrorResponse` format: `{"code": "UPPER_SNAKE_CASE", "message": "human-readable"}`. HTTP status codes MUST map to semantics: 400 validation, 401 authn, 403 authz, 404 not found, 409 conflict, 500 internal.
- List endpoints MUST support pagination (`?limit=N&offset=M`) with defaults and max caps. Responses: `{items, total, limit, offset}`.
- All response writing MUST use `shared/httputil/` helpers (`WriteJSON`, `WriteError`). Raw `w.Write()` in handlers is forbidden.

**gRPC** — All internal service-to-service communication MUST use gRPC with protobuf.
- Proto files MUST live in `ws/proto/` with package naming `sukko.{service}.v1`. Style: `PascalCase` messages/services, `snake_case` fields, `UPPER_SNAKE_CASE` enums. Code generation via `buf generate`; generated code committed to repo. `buf lint` MUST pass in CI.
- Server-side streaming MUST be used for real-time data push (watch/subscribe). Unary RPCs for request-response.
- gRPC status codes MUST map to domain semantics: `NotFound`, `InvalidArgument`, `FailedPrecondition` (state conflict), `Internal`, `Unavailable` (temporary). Context via `status.Errorf()`.
- gRPC servers MUST run on a dedicated port, separate from HTTP. Both listeners MUST support graceful shutdown.
- Interceptors MUST handle: panic recovery (first), structured logging, Prometheus metrics (latency histograms, call counters).
- Stream clients MUST reconnect with exponential backoff and jitter, serve stale cache during disconnection, and reflect stream health in service health endpoints.

### XIII. Feature Gates

Every edition-gated feature MUST be documented in `internal/shared/license/features.go` with: (1) a `Feature` constant with a human-readable string value describing the capability, (2) an entry in `featureEditions` mapping it to the minimum required edition (Community/Pro/Enterprise), (3) a code comment on the constant indicating its status — `// Implemented` or `// Future — not yet implemented`. The `features.go` file is the **single source of truth** for the feature matrix — the docs site auto-generates the editions comparison page from it.

Every implemented gated feature MUST have an `EditionHasFeature()` check at its access boundary (API handler, config validation, or startup gate). Implemented features without gate checks allow Community users to access Pro/Enterprise functionality — this is a security and business logic bug.

New feature implementations MUST check the feature matrix first: if a `Feature` constant exists for the capability being built, the implementation MUST wire the gate check. Adding new gated features MUST follow: (1) add `Feature` constant with `// Future` comment, (2) add `featureEditions` entry, (3) when implementing, add `EditionHasFeature()` check and update comment to `// Implemented`.

### Governance

- This constitution supersedes all other ad-hoc practices in the codebase.
- **Correctness over pattern**: When existing code is incorrect — whether it violates the constitution, Go best practices, robustness principles, testability, Go idioms, readability, security, or performance — correctness wins. NEVER replicate a broken pattern just because the codebase uses it. Broken code is not precedent; it is a bug. Code MUST be evaluated against: (1) this constitution, (2) Go best practices and idioms, (3) robustness and error handling correctness, (4) testability, (5) readability, (6) security, (7) performance. If a pre-existing deficiency is discovered during a change, fix both the new code and the pre-existing deficiency.
- Amendments require documentation in this section and a version bump.
- All code changes MUST verify compliance with these principles.
- MINOR version bump for adding/expanding principles; PATCH for wording changes; MAJOR for removing or redefining principles.
- The detailed coding guidelines with examples live at `docs/architecture/CODING_GUIDELINES.md`.
