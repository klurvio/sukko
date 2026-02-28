# Implementation Plan: Pluggable Message Backends

**Branch**: `feat/message-backends` | **Date**: 2026-02-27 | **Spec**: specs/feat/message-backends/spec.md

## Summary

Replace the hard Kafka dependency in the ws-server with a pluggable `MessageBackend` interface that supports three implementations: Direct (zero dependencies), Kafka/Redpanda (existing behavior), and NATS JetStream (lightweight persistence). The server's handler code calls the interface; the concrete backend is selected at startup via `MESSAGE_BACKEND` env var.

## Technical Context

**Language**: Go 1.22+
**Services**: ws-server (primary), ws-gateway (unchanged), provisioning (unchanged)
**Infrastructure**: Kubernetes (GKE Standard), Helm, Terraform
**Messaging**: Redpanda/Kafka (franz-go), NATS Core (broadcast bus), NATS JetStream (new backend)
**Monitoring**: Prometheus, Grafana
**Build/Deploy**: Docker, Taskfile, Artifact Registry

## Constitution Compliance

| Principle | Status | Notes |
|-----------|--------|-------|
| I. No Hardcoded Values | PASS | `MESSAGE_BACKEND` via env var with `envDefault:"direct"`. All NATS JetStream config via env vars. |
| II. Defense in Depth | PASS | Backend validates internally; handlers validate before calling backend. |
| III. Error Handling | PASS | Backend methods return wrapped errors. Sentinel errors for expected conditions. |
| IV. Graceful Degradation | PASS | Direct mode is the noop fallback. Backend unavailable → fail fast at startup, circuit breaker mid-operation. |
| V. Structured Logging | PASS | zerolog with structured fields in all backend implementations. |
| VI. Observability | PASS | Per-backend Prometheus metrics with `ws_backend_` prefix. |
| VII. Concurrency Safety | PASS | Backend lifecycle via Context + WaitGroup. |
| VIII. Config Validation | PASS | `MESSAGE_BACKEND` validated against allowed set at startup. Backend-specific config validated. |
| IX. Testing | PASS | Interface enables mock testing. Each backend gets unit tests. Integration test suite shared across backends. |
| X. Security | PASS | Input validation at handler level (unchanged). NATS JetStream supports auth. Secrets (tokens, passwords) MUST be redacted in `LogConfig()` — log only whether they are set, not their values. |
| XI. Shared Code | PASS | `MessageBackend` interface in shared location. Backend implementations use existing shared packages. |

## Architecture

### Data Flow Per Backend

```
DIRECT MODE:
  Client publishes → DirectBackend.Publish() → BroadcastBus → All Pods → Subscribers
  Client reconnects → DirectBackend.Replay() → empty (no persistence)

KAFKA MODE (current behavior preserved):
  Consumer Pool → Kafka topics → BroadcastBus → All Pods → Subscribers
  Client publishes → KafkaBackend.Publish() → Kafka → consumed back → BroadcastBus → Subscribers
  Client reconnects → KafkaBackend.Replay() → Kafka offsets → messages

NATS JETSTREAM MODE:
  JetStream consumers → streams → BroadcastBus → All Pods → Subscribers
  Client publishes → JetStreamBackend.Publish() → JetStream stream → consumed back → BroadcastBus → Subscribers
  Client reconnects → JetStreamBackend.Replay() → stream sequences → messages
```

### Interface Definition

```go
// Package backend provides the pluggable message backend abstraction.
package backend

// MessageBackend is the abstraction for message ingestion, publishing, and replay.
// Implementations: DirectBackend, KafkaBackend, JetStreamBackend.
type MessageBackend interface {
    // Start begins the backend's consumption loop (if applicable).
    // For Kafka: starts the MultiTenantConsumerPool.
    // For JetStream: starts stream consumers.
    // For Direct: no-op (returns nil immediately).
    Start(ctx context.Context) error

    // Publish sends a client-published message through the backend.
    // The backend is responsible for routing the message to reach subscribers
    // (either directly via broadcast bus or through the backend's consume loop).
    Publish(ctx context.Context, clientID string, channel string, data json.RawMessage) error

    // Replay returns messages from the specified positions for client reconnection.
    // positions: map of topic/stream name → last seen position (offset or sequence).
    // Returns nil, nil if replay is not supported (direct mode).
    Replay(ctx context.Context, req ReplayRequest) ([]ReplayMessage, error)

    // IsHealthy returns true if the backend is operational.
    IsHealthy() bool

    // Shutdown gracefully stops the backend.
    Shutdown(ctx context.Context) error
}

// Sentinel errors for expected backend conditions.
var (
    ErrBackendUnavailable = errors.New("message backend unavailable")
    ErrReplayNotSupported = errors.New("replay not supported by this backend")
    ErrPublishFailed      = errors.New("message publish failed")
    ErrUnknownBackend     = errors.New("unknown message backend")
)

// ReplayRequest contains parameters for message replay on reconnect.
type ReplayRequest struct {
    Positions     map[string]int64 // Topic/stream → last position (offset or sequence)
    MaxMessages   int              // Maximum messages to replay
    Subscriptions []string         // Only replay messages for these channels
}

// ReplayMessage is a backend-agnostic replayed message.
type ReplayMessage struct {
    Subject string // Channel/routing key (e.g., "acme.BTC.trade")
    Data    []byte // Raw message payload
}
```

### Package Structure

```
ws/internal/server/backend/
├── backend.go          # MessageBackend interface, ReplayRequest, ReplayMessage types
├── direct.go           # DirectBackend implementation
├── kafka.go            # KafkaBackend wrapping existing kafka + orchestration packages
├── jetstream.go        # JetStreamBackend implementation
├── factory.go          # NewMessageBackend() factory — selects by config
├── direct_test.go      # Direct backend tests
├── kafka_test.go       # Kafka backend tests
├── jetstream_test.go   # JetStream backend tests
└── factory_test.go     # Factory selection tests
```

## Phase 1 — Interface & Configuration

Define the `MessageBackend` interface, backend-agnostic types, and configuration.

### Files

| Action | File | Description |
|--------|------|-------------|
| CREATE | `ws/internal/server/backend/backend.go` | `MessageBackend` interface, `ReplayRequest`, `ReplayMessage` types |
| MODIFY | `ws/internal/shared/platform/server_config.go` | Add `MessageBackend string` field with `env:"MESSAGE_BACKEND" envDefault:"direct"`. Add NATS JetStream config fields. Update `Validate()` to check allowed values. |

### Configuration Fields (server_config.go)

```go
// Message backend selection
MessageBackend string `env:"MESSAGE_BACKEND" envDefault:"direct"` // direct, kafka, nats

// NATS JetStream config (only used when MESSAGE_BACKEND=nats)
NATSJetStreamURLs     string        `env:"NATS_JETSTREAM_URLS"`                          // Comma-separated NATS URLs
NATSJetStreamToken    string        `env:"NATS_JETSTREAM_TOKEN"`                         // Auth token
NATSJetStreamUser     string        `env:"NATS_JETSTREAM_USER"`                          // Username
NATSJetStreamPassword string        `env:"NATS_JETSTREAM_PASSWORD"`                      // Password
NATSJetStreamReplicas int           `env:"NATS_JETSTREAM_REPLICAS" envDefault:"1"`       // Stream replicas
NATSJetStreamMaxAge   time.Duration `env:"NATS_JETSTREAM_MAX_AGE" envDefault:"24h"`      // Message retention
NATSJetStreamTLSEnabled bool        `env:"NATS_JETSTREAM_TLS_ENABLED" envDefault:"false"` // TLS for managed NATS (Synadia Cloud, etc.)
NATSJetStreamTLSInsecure bool       `env:"NATS_JETSTREAM_TLS_INSECURE" envDefault:"false"` // Skip TLS verification (not for production)
NATSJetStreamTLSCAPath string       `env:"NATS_JETSTREAM_TLS_CA_PATH"`                    // Custom CA certificate path
```

### Validation Rules

- `MESSAGE_BACKEND` must be one of: `direct`, `kafka`, `nats`
- If `kafka`: existing Kafka validation applies (brokers required)
- If `nats`: `NATS_JETSTREAM_URLS` required
- If `direct`: no additional config required

## Phase 2 — KafkaBackend (Wrap Existing Code)

Wrap the existing `MultiTenantConsumerPool` and `kafka.Producer` behind the `MessageBackend` interface. This is a zero-behavior-change refactor — the Kafka path works exactly as before.

### Files

| Action | File | Description |
|--------|------|-------------|
| CREATE | `ws/internal/server/backend/kafka.go` | `KafkaBackend` struct wrapping pool + producer |

### KafkaBackend Design

```go
type KafkaBackend struct {
    pool     *orchestration.MultiTenantConsumerPool
    producer *kafka.Producer
    logger   zerolog.Logger
}

type KafkaBackendConfig struct {
    // All existing Kafka + pool config fields
    // Passed through to pool and producer constructors
}
```

- `Start()` → calls `pool.Start()`
- `Publish()` → calls `producer.Publish(ctx, clientID, channel, data)` — delegates entirely
- `Replay()` → calls `pool.GetSharedConsumer().ReplayFromOffsets()` — converts `kafka.ReplayMessage` → `backend.ReplayMessage`
- `IsHealthy()` → checks pool and producer health
- `Shutdown()` → stops pool, closes producer, closes provisioning DB

The `KafkaBackend` owns the provisioning DB connection, pool, and producer — all moved out of `main.go` into the backend. This encapsulates all Kafka-specific initialization.

## Phase 3 — DirectBackend

Implement the simplest backend: messages go directly to the broadcast bus.

### Files

| Action | File | Description |
|--------|------|-------------|
| CREATE | `ws/internal/server/backend/direct.go` | `DirectBackend` implementation |

### DirectBackend Design

```go
type DirectBackend struct {
    bus    broadcast.Bus
    logger zerolog.Logger
}
```

- `Start()` → no-op, returns nil
- `Publish()` → `bus.Publish(&broadcast.Message{Subject: channel, Payload: data})` — zero latency
- `Replay()` → returns `nil, nil` — no persistence
- `IsHealthy()` → always true (in-process)
- `Shutdown()` → no-op

## Phase 4 — Server Integration

Rewire the server to use `MessageBackend` instead of concrete Kafka types.

### Files

| Action | File | Description |
|--------|------|-------------|
| CREATE | `ws/internal/server/backend/factory.go` | `NewMessageBackend()` factory function |
| MODIFY | `ws/internal/server/server.go` | Replace `kafkaConsumer`/`kafkaProducer` with `backend MessageBackend` |
| MODIFY | `ws/internal/server/handlers_publish.go` | Use `s.backend.Publish()` instead of `s.kafkaProducer.Publish()` |
| MODIFY | `ws/internal/server/handlers_message.go` | Rename `handleKafkaReconnect` → `handleReconnect`. Use `s.backend.Replay()` |
| MODIFY | `ws/internal/shared/types/types.go` | Replace `SharedKafkaConsumer any` and `KafkaProducer any` with `MessageBackend any` |
| MODIFY | `ws/internal/server/orchestration/shard.go` | Replace `SharedKafkaConsumer`/`KafkaProducer` with `MessageBackend` in `ShardConfig` |
| MODIFY | `ws/cmd/server/main.go` | Use factory to create backend, pass to shards. Move Kafka init into KafkaBackend. |

### Factory Function

```go
func NewMessageBackend(cfg Config) (MessageBackend, error) {
    switch cfg.Type {
    case "direct":
        return NewDirectBackend(cfg.BroadcastBus, cfg.Logger)
    case "kafka":
        return NewKafkaBackend(cfg.Kafka, cfg.Logger)
    case "nats":
        return NewJetStreamBackend(cfg.JetStream, cfg.Logger)
    default:
        return nil, fmt.Errorf("unknown message backend: %q (valid: direct, kafka, nats)", cfg.Type)
    }
}
```

### Server Changes

`server.go`:
```go
// Before:
kafkaConsumer *kafka.Consumer
kafkaProducer *kafka.Producer

// After:
backend backend.MessageBackend
```

`handlers_publish.go`:
```go
// Before:
if s.kafkaProducer == nil { ... }
s.kafkaProducer.Publish(ctx, c.id, pubReq.Channel, pubReq.Data)

// After:
if s.backend == nil { ... }
s.backend.Publish(ctx, c.id, pubReq.Channel, pubReq.Data)
```

`handlers_message.go`:
```go
// Before:
func (s *Server) handleKafkaReconnect(c *Client, data []byte) {
    ...
    s.kafkaConsumer.ReplayFromOffsets(ctx, offsets, max, subs)

// After:
func (s *Server) handleReconnect(c *Client, data []byte) {
    ...
    s.backend.Replay(ctx, backend.ReplayRequest{
        Positions:     offsets,
        MaxMessages:   100,
        Subscriptions: subs,
    })
```

### main.go Changes

The bulk of main.go's Kafka initialization (lines 140-260) moves into `KafkaBackend` construction. main.go becomes:

```go
// Create message backend
msgBackend, err := backend.NewMessageBackend(backend.Config{
    Type:         cfg.MessageBackend,
    BroadcastBus: broadcastBus,
    Logger:       logger,
    Kafka: backend.KafkaConfig{
        // ... existing kafka config fields
    },
    JetStream: backend.JetStreamConfig{
        // ... NATS JetStream config fields
    },
})
if err != nil {
    logger.Fatalf("Failed to create message backend: %v", err)
}
if err := msgBackend.Start(ctx); err != nil {
    logger.Fatalf("Failed to start message backend: %v", err)
}
```

## Phase 5 — JetStreamBackend (P2)

Implement NATS JetStream backend with persistent streams and sequence-based replay.

### Files

| Action | File | Description |
|--------|------|-------------|
| CREATE | `ws/internal/server/backend/jetstream.go` | `JetStreamBackend` implementation |

### JetStreamBackend Design

```go
type JetStreamBackend struct {
    conn       *nats.Conn
    js         jetstream.JetStream
    bus        broadcast.Bus
    logger     zerolog.Logger

    // Stream management
    streams    map[string]jetstream.Stream  // tenant → stream
    streamsMu  sync.RWMutex

    // Consumer management
    consumers  map[string]jetstream.Consumer // stream → consumer

    // Configuration
    namespace  string
    replicas   int
    maxAge     time.Duration

    ctx        context.Context
    cancel     context.CancelFunc
    wg         sync.WaitGroup
}
```

- `Start()` → connects to NATS, enables JetStream, creates streams for provisioned topics, starts push consumers that route to broadcast bus
- `Publish()` → `js.Publish(ctx, subject, data)` — synchronous with ack
- `Replay()` → creates ordered consumer starting from sequence number, reads up to max messages
- `IsHealthy()` → checks NATS connection + JetStream availability
- `Shutdown()` → drains consumers, closes connection

### Stream Strategy

- One stream per tenant: `ODIN_{namespace}_{tenant}` (e.g., `ODIN_dev_acme`)
- Subject filter: `odin.{tenant}.>` captures all categories for that tenant
- Retention: `MaxAge` from config (default 24h)
- Replicas: from config (default 1)

### Topic Discovery

Like Kafka's `MultiTenantConsumerPool`, the JetStream backend needs to discover provisioned topics. Two options:

1. **Reuse `TenantRegistry`**: Same provisioning DB query. The backend creates/updates streams based on registry data.
2. **Static config**: Topics defined in config file (when using `PROVISIONING_MODE=config`).

Decision: Reuse `TenantRegistry` interface for consistency. The backend periodically refreshes streams (same pattern as Kafka pool's `refreshLoop`).

### TenantRegistry Ownership

Both Kafka and JetStream backends need a `TenantRegistry` for topic discovery. The ownership model:

- **KafkaBackend**: Creates its own provisioning DB connection and `TenantRegistry` internally (encapsulated — DB URL in `KafkaBackendConfig`).
- **JetStreamBackend**: Also needs a `TenantRegistry`, but should NOT duplicate DB connection logic.

**Resolution**: The factory (`NewMessageBackend`) is responsible for creating the `TenantRegistry` when the backend needs one. The factory config includes `ProvisioningDatabaseURL string` and `ProvisioningMode string` at the top level. For `kafka` mode, the factory passes the DB URL into `KafkaBackendConfig` (Kafka backend creates its own DB + registry internally, as it also needs the DB for consumer pool management). For `nats` mode, the factory creates the provisioning DB and `TenantRegistry`, then passes the registry to `JetStreamBackendConfig.Registry`. For `direct` mode, no registry is needed.

This means `main.go` does NOT need provisioning imports — the factory handles everything. T014's instruction to "remove provisioning imports" remains valid.

### Replay Position Propagation

For Kafka, the broadcast message pipeline embeds topic offsets that clients echo back in `reconnect.last_offset`. For JetStream, the same mechanism must embed JetStream stream sequence numbers. The `Publish()` path for JetStream publishes to JetStream and receives an ack with the stream sequence — this sequence must be propagated through the broadcast bus message so clients can include it in reconnect requests. The exact mechanism (embedding in broadcast message metadata vs. separate tracking) will be designed during Phase 5 implementation, following the same pattern Kafka uses today.

**Note**: The `TenantRegistry` interface currently lives in `ws/internal/shared/kafka/tenant_registry.go`. Since JetStream also uses it, the interface should be moved to `ws/internal/shared/types/` (or a new `ws/internal/shared/registry/` package) before Phase 5 implementation. Both Kafka and JetStream backends import from the shared location. This avoids a misleading `kafka` import in JetStream code (Constitution XI).

## Phase 6 — Broadcast Bus TLS (Managed Service Support)

Add TLS configuration to the existing NATS and Valkey broadcast bus implementations for managed service connectivity.

### Files

| Action | File | Description |
|--------|------|-------------|
| MODIFY | `ws/internal/server/broadcast/bus.go` | Add TLS fields to `NATSConfig` and `ValkeyConfig` |
| MODIFY | `ws/internal/server/broadcast/nats.go` | Apply TLS config when connecting to NATS |
| MODIFY | `ws/internal/server/broadcast/valkey.go` | Apply TLS config when connecting to Valkey |
| MODIFY | `ws/internal/shared/platform/server_config.go` | Add `NATS_TLS_ENABLED`, `NATS_TLS_INSECURE`, `NATS_TLS_CA_PATH`, `VALKEY_TLS_ENABLED`, `VALKEY_TLS_INSECURE`, `VALKEY_TLS_CA_PATH` env vars |
| MODIFY | `ws/cmd/server/main.go` | Pass TLS config to broadcast bus creation |

### Config Fields (server_config.go)

```go
// NATS broadcast bus TLS (for managed NATS: Synadia Cloud, etc.)
NATSTLSEnabled  bool   `env:"NATS_TLS_ENABLED" envDefault:"false"`
NATSTLSInsecure bool   `env:"NATS_TLS_INSECURE" envDefault:"false"`
NATSTLSCAPath   string `env:"NATS_TLS_CA_PATH"`

// Valkey broadcast bus TLS (for managed Valkey/Redis: ElastiCache, Memorystore, Upstash, etc.)
ValkeyTLSEnabled  bool   `env:"VALKEY_TLS_ENABLED" envDefault:"false"`
ValkeyTLSInsecure bool   `env:"VALKEY_TLS_INSECURE" envDefault:"false"`
ValkeyTLSCAPath   string `env:"VALKEY_TLS_CA_PATH"`
```

### Broadcast Bus Config Changes (bus.go)

```go
// NATSConfig additions
TLSEnabled  bool
TLSInsecure bool   // Skip verification (not for production)
TLSCAPath   string // Custom CA certificate path

// ValkeyConfig additions
TLSEnabled  bool
TLSInsecure bool
TLSCAPath   string
```

### NATS TLS Implementation (nats.go)

When `TLSEnabled` is true, add `nats.Secure()` option. If `TLSCAPath` is set, load the custom CA. If `TLSInsecure`, use `tls.Config{InsecureSkipVerify: true}`.

### Valkey TLS Implementation (valkey.go)

When `TLSEnabled` is true, configure the Valkey client with `tls.Config`. Same CA and insecure skip patterns as NATS.

## Phase 7 — Metrics

### Files

| Action | File | Description |
|--------|------|-------------|
| MODIFY | `ws/internal/server/metrics/metrics.go` | Add backend-agnostic metrics |

### Metrics

```
ws_backend_messages_published_total{backend="direct|kafka|nats"}
ws_backend_messages_consumed_total{backend="direct|kafka|nats"}
ws_backend_publish_errors_total{backend="direct|kafka|nats"}
ws_backend_replay_requests_total{backend="direct|kafka|nats"}
ws_backend_replay_messages_total{backend="direct|kafka|nats"}
ws_backend_publish_latency_seconds{backend="direct|kafka|nats"}
ws_backend_healthy{backend="direct|kafka|nats"}  (gauge)
```

Existing Kafka-specific metrics remain for Kafka mode. New metrics provide backend-agnostic observability.

## Phase 8 — Tests

### Files

| Action | File | Description |
|--------|------|-------------|
| CREATE | `ws/internal/server/backend/backend_test.go` | Interface contract tests (mock-based) |
| CREATE | `ws/internal/server/backend/direct_test.go` | Direct backend unit tests |
| CREATE | `ws/internal/server/backend/kafka_test.go` | Kafka backend unit tests (mock kafka.Producer/Consumer) |
| CREATE | `ws/internal/server/backend/jetstream_test.go` | JetStream backend unit tests |
| CREATE | `ws/internal/server/backend/factory_test.go` | Factory selection tests |
| MODIFY | `ws/internal/server/handlers_publish_test.go` | Update to use mock MessageBackend |
| MODIFY | `ws/internal/server/handlers_message_test.go` | Update to use mock MessageBackend |
| CREATE | `ws/internal/server/broadcast/nats_tls_test.go` | NATS TLS configuration tests |
| CREATE | `ws/internal/server/broadcast/valkey_tls_test.go` | Valkey TLS configuration tests |

### Test Strategy

1. **Interface compliance**: Each backend implementation passes the same set of contract tests.
2. **Direct backend**: Verify publish routes to broadcast bus, replay returns empty, health always true.
3. **Kafka backend**: Verify delegation to pool/producer (mock-based), error mapping preserved.
4. **JetStream backend**: Verify stream creation, publish to JetStream, replay by sequence.
5. **Handler tests**: Updated to use mock `MessageBackend` instead of mock Kafka types.
6. **Factory tests**: Verify correct backend created for each `MESSAGE_BACKEND` value, error on unknown.
7. **Broadcast bus TLS**: Verify TLS config is applied to NATS and Valkey connections. Test CA loading, insecure skip.

## Phase 9 — Helm & Deploy

### Files

| Action | File | Description |
|--------|------|-------------|
| MODIFY | `deployments/helm/odin/charts/ws-server/templates/deployment.yaml` | Add `MESSAGE_BACKEND`, NATS JetStream config, and broadcast bus TLS env vars |
| MODIFY | `deployments/helm/odin/charts/ws-server/values.yaml` | Add `messageBackend: direct`, JetStream config, and broadcast bus TLS config |
| MODIFY | `deployments/helm/odin/values/standard/dev.yaml` | Set `messageBackend: kafka` for dev (preserve current behavior) |

### Helm Values

```yaml
# values.yaml (defaults)
messageBackend: direct  # direct, kafka, nats

jetstream:
  urls: ""
  token: ""
  user: ""
  password: ""
  replicas: 1
  maxAge: "24h"
  tls:
    enabled: false
    insecure: false
    caPath: ""

# Broadcast bus TLS (for managed NATS / Valkey services)
nats:
  tls:
    enabled: false
    insecure: false
    caPath: ""

valkey:
  tls:
    enabled: false
    insecure: false
    caPath: ""
```

```yaml
# dev.yaml (override)
messageBackend: kafka  # Dev uses Kafka (existing behavior)
```

## File Change Summary

| Action | Count | Files |
|--------|-------|-------|
| CREATE | 12 | `backend/backend.go`, `backend/direct.go`, `backend/kafka.go`, `backend/jetstream.go`, `backend/factory.go`, `backend/*_test.go` (6: backend, direct, kafka, jetstream, factory, contract), `broadcast/*_tls_test.go` (2) |
| MODIFY | 14 | `server.go`, `handlers_publish.go`, `handlers_message.go`, `types.go`, `shard.go`, `main.go`, `server_config.go`, `metrics.go`, `broadcast/bus.go`, `broadcast/nats.go`, `broadcast/valkey.go`, `shared/kafka/tenant_registry.go` → `shared/types/tenant_registry.go` (move), Helm files (3) |
| TOTAL | 26 | |

## Phase Dependencies

```
Phase 1 (Interface + Config) ─── Phase 2 (KafkaBackend) ───┐
                              └── Phase 3 (DirectBackend) ──┤
                                                            ├── Phase 4 (Server Integration)
                                                            │
Phase 5 (JetStreamBackend) ─────────────────────────────────┘
                                                            │
Phase 6 (Broadcast Bus TLS) ────────────────────────────────┤  (independent, can parallel with 4/5)
                                                            │
                                          Phase 7 (Metrics) ┤
                                                            │
                                          Phase 8 (Tests) ──┤
                                                            │
                                          Phase 9 (Helm) ───┘
```

- Phase 1 must complete first (interface definition)
- Phases 2 and 3 are parallelizable (different files, no deps)
- Phase 4 depends on Phases 2 and 3 (needs at least one backend to test)
- Phase 5 can start after Phase 1 but must integrate after Phase 4
- Phase 6 can run in parallel with Phases 2-3 (different packages), but T027 must follow T002 (both modify `server_config.go`) and T030 must follow T014 (both modify `main.go`). Effectively: Phase 6 config tasks (T026-T027) parallel with Phases 2-3, Phase 6 wiring tasks (T028-T030) after Phase 4.
- Phases 7, 8, 9 are parallelizable after Phases 4 and 6

## Verification

After each phase:
```bash
cd ws && go vet ./...
cd ws && go test ./...
```

After Phase 4 (integration):
- Start server with `MESSAGE_BACKEND=direct` — verify no Kafka connection attempted
- Start server with `MESSAGE_BACKEND=kafka` — verify identical behavior to current
- Client publish in direct mode — verify message reaches subscribers via broadcast bus
- Client reconnect in direct mode — verify `reconnect_ack` with `messages_replayed: 0`

After Phase 9 (Helm):
```bash
helm lint deployments/helm/odin/charts/ws-server
```
