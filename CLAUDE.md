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
cd ws && go test ./...          # Run all tests
cd ws && go vet ./...           # Static analysis
cd ws && go build ./cmd/server  # Build ws-server
cd ws && go build ./cmd/gateway # Build gateway

# Build & Deploy (via Taskfile)
task k8s:build ENV=dev          # Build + push all images
task k8s:deploy ENV=dev         # Helm deploy to GKE
task k8s:build:push:ws-server ENV=dev  # Build single service

# Helm
helm lint deployments/helm/sukko/charts/ws-server
helm lint deployments/helm/sukko/charts/ws-gateway

# Terraform
cd deployments/terraform/environments/standard/dev && terraform plan

# Logs
kubectl logs -n sukko-dev -l app.kubernetes.io/name=ws-server --tail=50
kubectl logs -n sukko-dev -l app.kubernetes.io/name=ws-gateway --tail=50
```

## Architecture

### Data Flow
```
Publishers → ws-gateway (auth, rate limiting) → ws-server (ingestion)
    → Redpanda (Kafka) → ws-server (franz-go consumer)
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
├── helm/sukko/          # Helm charts (ws-server, ws-gateway, monitoring, etc.)
│   ├── charts/          # Subchart definitions
│   └── values/standard/ # Environment overrides (dev.yaml, stg.yaml)
├── terraform/           # GKE, Cloud NAT, VPC, static IPs
│   ├── environments/    # Per-env configs (dev, stg)
│   └── modules/         # Reusable Terraform modules
docs/architecture/       # Plans, findings, session handoffs
```

### Key Technologies
- **Go 1.26+** with modern features (any, slices, maps, for range N, errors.Join, errors.AsType[T], new(expr), self-referential generics, reflect iterators)
- **franz-go** for Kafka/Redpanda consumption (consumer groups, partition management)
- **NATS** for inter-pod broadcast (publish/subscribe)
- **gRPC** + **protobuf** for internal service-to-service communication (buf for codegen)
- **gorilla/websocket** for WebSocket connections
- **zerolog** for structured logging
- **Prometheus** for metrics (promauto registration)
- **Helm 3** for Kubernetes deployments
- **Terraform** for GKE Standard cluster infrastructure
- **Taskfile** for build/deploy orchestration
- **Docker** multi-stage builds → Google Artifact Registry

### Configuration Pattern
All configuration uses `caarlos0/env` struct tags. Go `envDefault` values are the single source of truth for defaults:
```go
type Config struct {
    Port int `env:"GATEWAY_PORT" envDefault:"3000"`
}
```
Helm and Docker Compose override via env vars only when a deployment needs a non-default value. Helm templates auto-wire Kubernetes service discovery (e.g., `{{ .Release.Name }}-nats`). Env var names in Go MUST match Helm template values.

## Commit Message Format

Conventional commits with GitHub Issue number required:
```
type[#issue]: subject (min 4 chars)

Examples:
feat[#42]: add tenant connection tracking
fix[#15]: resolve kafka consumer offset reset
refactor[#78]: remove legacy metrics collector
```

Valid types: feat, fix, docs, style, refactor, perf, test, build, ci, chore, revert

## Pre-commit Hook

Install: `git config core.hooksPath .githooks`

Runs automatically: Go formatting, go vet, golangci-lint, Helm lint, binary check, secrets scan.

## Constitution

**Version**: 1.5.2 | **Ratified**: 2026-02-17 | **Last Amended**: 2026-03-04

### I. Configuration

Every configurable parameter MUST be externalized via environment variables with `env:` struct tags. Go `envDefault:` values are the **single source of truth** for all configuration defaults — they MUST reflect production-intended values. Helm values and Docker Compose MUST NOT duplicate Go defaults; they override via env vars ONLY when a deployment requires a value different from the Go default (e.g., Kubernetes service discovery addresses, mode selections, resource-derived limits). Helm templates MUST NOT compute derived values — derived configuration MUST be computed in Go. Magic numbers MUST be named constants or configuration. Magic strings (URLs, broker addresses, topic names, tenant IDs, namespace prefixes) MUST NOT be hardcoded — they MUST come from configuration or be constructed from configured values. All configuration MUST be validated at startup with clear error messages. Hysteresis thresholds MUST enforce `lower < upper`. Enum values MUST be validated against allowed sets. Invalid configuration MUST cause immediate startup failure, not silent degradation.

### II. Defense in Depth

Every layer MUST validate its inputs. Never assume upstream validation. The gateway validates, and the server validates again. Input validation at ALL system boundaries is mandatory.

### III. Error Handling

All errors MUST be wrapped with context using `fmt.Errorf("operation: %w", err)`. Sentinel errors MUST be defined for expected conditions. Ignored errors MUST have an explicit comment explaining why. Silent failures are forbidden.

### IV. Graceful Degradation

Optional dependencies MUST use noop implementations or nil-guarded feature flags — never half-initialized state. Multi-step cleanup MUST continue on individual failures. Health endpoints MUST report degraded state. Retry logic MUST use exponential backoff with a cap. Slow WebSocket clients MUST be detected and disconnected via circuit breaker.

### V. Structured Logging

All logging MUST use zerolog with structured fields (Str, Int, Dur, Err). Appropriate log levels MUST be used (Debug/Info/Warn/Error/Fatal). No `log.Printf` or `fmt.Println`. Panic recovery logging is mandated by VII (Goroutine Lifecycle step 2).

### VI. Observability

Every significant operation MUST have Prometheus metrics. Metric names MUST use `ws_` prefix (server), `gateway_` prefix (gateway), or `provisioning_` prefix (provisioning service) with units (`_seconds`, `_bytes`, `_total`). Labels MUST be used sparingly to avoid cardinality explosion. Histograms MUST be used for latency, not summaries.

### VII. Concurrency Safety

This is a high-performance WebSocket server handling thousands of concurrent connections per pod. Incorrect concurrency primitives cause panics, deadlocks, goroutine leaks, and silent data corruption. Every concurrent pattern MUST follow the established patterns below.

**Design Preference** — Prefer goroutine ownership over shared memory with locks. When a piece of state needs concurrent access, the first choice SHOULD be a dedicated goroutine that owns the state and communicates via channels (Go proverb: "share memory by communicating"). Mutexes are acceptable for simple read-heavy caches (`sync.RWMutex`) and atomic counters, but for stateful operations (connection lifecycle, subscription tracking, auth flow), a single-owner goroutine with channel-based communication is safer and eliminates lock-ordering concerns.

**Goroutine Lifecycle** — All goroutines MUST follow this exact launch sequence:
1. `wg.Add(1)` MUST be called BEFORE the `go` statement — never inside the goroutine. Calling `Add()` inside the goroutine is a race condition: `Done()` can execute before `Add()`, causing `Wait()` to return prematurely.
2. `defer logging.RecoverPanic(...)` MUST be the FIRST `defer` inside the goroutine body.
3. `defer wg.Done()` MUST be the SECOND `defer` inside the goroutine body. Using `defer` (not inline) guarantees execution on panic or early return.
4. The goroutine MUST check `ctx.Done()` in its main loop via `select` for shutdown signaling.
5. `wg.Wait()` MUST be called in the shutdown/stop path to ensure all goroutines have exited before resources are released.

Shutdown ordering MUST be: cancel context → `wg.Wait()` for goroutines → close channels → release resources. Reversing this order (e.g., closing a channel before its goroutine exits) causes panics.

**Channels** — Channel type MUST match usage pattern:
- **Signal/stop channels** (`chan struct{}`): Used for shutdown signaling. MUST be closed by exactly one goroutine. If multiple goroutines may attempt close, MUST use `sync.Once` to guard the `close()` call. Sending on a closed channel panics — this is unrecoverable in production.
- **Data channels** (e.g., `chan OutgoingMsg`): MUST be buffered with a size matching throughput requirements. Unbuffered channels MUST NOT be used in hot paths (message distribution, broadcast fan-out) because a single slow receiver blocks all senders.
- **Semaphore channels** (`chan struct{}` with capacity = limit): Used for resource limiting (max connections, max goroutines). Acquire MUST be non-blocking (`select` with `default` case) to reject callers at capacity rather than queueing them indefinitely.
- **Fan-out sends** (broadcast to multiple subscribers): MUST use non-blocking `select` with `default` to skip slow consumers. Dropped messages MUST be counted via Prometheus metrics (`_dropped_total`). A single slow subscriber MUST NOT block delivery to all other subscribers.
- **Channel close rules**: Only the sender side MUST close a channel — never the receiver. After closing, no further sends are permitted (panic). When a channel may be closed from multiple code paths, guard with `sync.Once`. When draining a channel before reuse (e.g., `sync.Pool`), use `select` with `default` in a loop.

**WaitGroups** — `sync.WaitGroup` is for goroutine lifecycle tracking only. The `Add`/`Done`/`Wait` sequence is defined in Goroutine Lifecycle above. Additional constraints:
- `wg.Wait()` SHOULD have a timeout mechanism (e.g., wrapper with `context.WithTimeout`) to detect stuck goroutines during shutdown rather than hanging indefinitely.
- WaitGroups MUST NOT be reused after `Wait()` returns for a given set of goroutines.

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

**Message Pipeline Protection** — The message delivery pipeline (ingestion → broadcast bus → shard fan-out → per-client write pump → WebSocket write) is the critical hot path. Feature-level operations (auth refresh, subscription management, metrics collection, provisioning lookups, OIDC validation) MUST NOT introduce blocking on this path. Specifically:
- Locks acquired for feature operations MUST NOT be held while calling into the pipeline (`forwardFrame`, `sendToClient`, `bus.Publish`, write pump sends).
- Feature operations that run on the client→backend goroutine MUST complete without waiting for backend responses when the wait would stall message reads from the client.
- Backend→client forwarding MUST remain non-blocking: observational interception (subscription tracking, metrics) MUST NOT add latency that degrades broadcast throughput.

### VIII. Testing

Tests MUST be table-driven for multiple cases. Mocks MUST use interfaces. `t.Parallel()` MUST NOT be used on tests with shared resources (databases, external services, `*_shared_test.go`). Edge cases MUST be covered (empty, nil, max values, error paths).

**Test coverage for all changes is mandatory:**
- **Bug fixes** MUST update or add unit tests that reproduce the bug and verify the fix. If existing tests missed the bug, they MUST be strengthened.
- **Enhancements and refactors** MUST update existing tests to reflect the changed behavior and add tests for any new code paths introduced.
- **New features** MUST include comprehensive unit tests covering: happy path, error paths, edge cases, and concurrency safety (where applicable).
- No code change (bug fix, enhancement, refactor, or feature) MUST be considered complete without corresponding test updates. Untested code changes are forbidden.

### IX. Security

Rate limiting MUST be applied at multiple levels (global, per-IP, per-tenant). Secrets MUST never appear in logs or error messages. JWT validation MUST verify expiration and issuer. `//nolint` or `#nosec` MUST include thorough written justification. Input validation at boundaries is mandated by II.

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

### Governance

- This constitution supersedes all other ad-hoc practices in the codebase.
- **Correctness over pattern**: When existing code is incorrect — whether it violates the constitution, Go best practices, robustness principles, testability, Go idioms, readability, security, or performance — correctness wins. NEVER replicate a broken pattern just because the codebase uses it. Broken code is not precedent; it is a bug. Code MUST be evaluated against: (1) this constitution, (2) Go best practices and idioms, (3) robustness and error handling correctness, (4) testability, (5) readability, (6) security, (7) performance. If a pre-existing deficiency is discovered during a change, fix both the new code and the pre-existing deficiency.
- Amendments require documentation in this section and a version bump.
- All code changes MUST verify compliance with these principles.
- MINOR version bump for adding/expanding principles; PATCH for wording changes; MAJOR for removing or redefining principles.
- The detailed coding guidelines with examples live at `docs/architecture/CODING_GUIDELINES.md`.
