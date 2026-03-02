# Implementation Plan: Pluggable Message Backends

**Branch**: `feat/message-backends` | **Date**: 2026-02-27 | **Spec**: specs/feat/message-backends/spec.md

## Summary

Replace the hard Kafka dependency in the ws-server with a pluggable `MessageBackend` interface that supports three implementations: Direct (zero dependencies), Kafka/Redpanda (existing behavior), and NATS JetStream (lightweight persistence). The server's handler code calls the interface; the concrete backend is selected at startup via `MESSAGE_BACKEND` env var.

## Technical Context

**Language**: Go 1.22+
**Services**: ws-server (primary), provisioning (KafkaAdmin removal), ws-gateway (unchanged)
**Infrastructure**: Kubernetes (GKE Standard), Helm, Terraform
**Messaging**: Redpanda/Kafka (franz-go), NATS Core (broadcast bus), NATS JetStream (new backend)
**Monitoring**: Prometheus, Grafana
**Build/Deploy**: Docker, Taskfile, Artifact Registry

## Constitution Compliance

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Configuration | PASS | `MESSAGE_BACKEND` via env var with `envDefault:"direct"`. All NATS JetStream config via env vars. Validated at startup against allowed set. |
| II. Defense in Depth | PASS | Backend validates internally; handlers validate before calling backend. |
| III. Error Handling | PASS | Backend methods return wrapped errors. Sentinel errors for expected conditions. |
| IV. Graceful Degradation | PASS | Direct mode is the noop fallback. Backend unavailable → fail fast at startup, circuit breaker mid-operation. |
| V. Structured Logging | PASS | zerolog with structured fields in all backend implementations. |
| VI. Observability | PASS | Per-backend Prometheus metrics with `ws_backend_` prefix. |
| VII. Concurrency Safety | PASS | Backend lifecycle via Context + WaitGroup. |
| VIII. Testing | PASS | Interface enables mock testing. Each backend gets unit tests. Shared integration test suite deferred to integration testing phase. |
| IX. Security | PASS | Input validation at handler level (unchanged). NATS JetStream supports auth. Secrets (tokens, passwords) MUST be redacted in `LogConfig()` — log only whether they are set, not their values. |
| X. Shared Code | PASS | `MessageBackend` interface in `server/backend/` (server-only). `TenantRegistry` relocated to `shared/types/` (multi-service). |
| XI. Prior Art Research | PASS | Research phase completed in `research.md`. Patterns from Pusher, Ably, Centrifugo informed the pluggable backend design. |
| XII. API Design | PASS | No REST or gRPC API changes. Health endpoint JSON key rename (`"kafka"` → `"backend"`) is internal, not client-facing. |

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
    Publish(ctx context.Context, clientID int64, channel string, data []byte) error

    // Replay returns messages from the specified positions for client reconnection.
    // positions: map of topic/stream name → last seen position (offset or sequence).
    // Returns nil, nil if replay is not supported (direct mode).
    Replay(ctx context.Context, req ReplayRequest) ([]ReplayMessage, error)

    // IsHealthy returns true if the backend is operational.
    IsHealthy() bool

    // Shutdown gracefully stops the backend.
    Shutdown(ctx context.Context) error
}

// DefaultMaxReplayMessages is the maximum number of messages replayed on reconnect.
const DefaultMaxReplayMessages = 100

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
├── backend.go                          # MessageBackend interface, ReplayRequest, ReplayMessage types
├── directbackend/
│   ├── directbackend.go                # DirectBackend implementation
│   └── directbackend_test.go           # Direct backend tests
├── kafkabackend/
│   ├── kafkabackend.go                 # KafkaBackend wrapping existing kafka + orchestration packages
│   └── kafkabackend_test.go            # Kafka backend tests
└── jetstreambackend/
    ├── jetstreambackend.go             # JetStreamBackend implementation
    └── jetstreambackend_test.go        # JetStream backend tests
```

**Note**: Backend selection is wired in `ws/cmd/server/main.go` via an inline `switch` on `MESSAGE_BACKEND` — no separate factory file.

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

// Topic refresh interval (applies to both Kafka and NATS JetStream backends)
// Periodic re-sync of tenant topics/streams from registry — safety net for missed gRPC events.
TopicRefreshInterval time.Duration `env:"TOPIC_REFRESH_INTERVAL" envDefault:"60s"`
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
| CREATE | `ws/internal/server/backend/kafkabackend/kafkabackend.go` | `KafkaBackend` struct wrapping pool + producer |

### KafkaBackend Design

```go
type KafkaBackend struct {
    pool           *orchestration.MultiTenantConsumerPool
    producer       *kafka.Producer
    admin          *kadm.Client       // Kafka admin for on-demand topic creation
    kgoClient      *kgo.Client        // Underlying kgo client for admin ops
    topicRegistry  *provapi.StreamTopicRegistry // gRPC stream to provisioning service
    logger         zerolog.Logger
    healthy        atomic.Bool
    wg             sync.WaitGroup     // Tracks in-flight topic update goroutines
}

type Config struct { // kafkabackend.Config (package-qualified)
    // All existing Kafka + pool config fields
    // Passed through to pool and producer constructors
    // Provisioning gRPC config (for topic discovery):
    //   ProvisioningGRPCAddr, GRPCReconnectDelay, GRPCReconnectMaxDelay
}
```

- `Start()` → calls `pool.Start()`, wires `topicRegistry.SetOnUpdate(pool.RefreshTopics)`
- `Publish()` → calls `producer.Publish(ctx, clientID, channel, data)` — delegates entirely
- `Replay()` → calls `pool.GetSharedConsumer().ReplayFromOffsets()` — converts `kafka.ReplayMessage` → `backend.ReplayMessage`
- `IsHealthy()` → checks pool and producer health
- `Shutdown()` → stops pool, closes producer, closes topic registry gRPC connection (continue on individual failures per Constitution IV)

The `KafkaBackend` owns the topic registry (gRPC stream), pool, producer, and admin client — all moved out of `main.go` into the backend. This encapsulates all Kafka-specific initialization.

### On-Demand Topic Creation

With KafkaAdmin removed from provisioning (see Provisioning Cleanup below), the `KafkaBackend` is responsible for creating Kafka topics on demand. When the `topicRegistry` delivers new topic configs via gRPC stream, the `KafkaBackend` wraps the pool's refresh callback to:

1. Ensure Kafka topics exist for each discovered category (via `kadm.Client`)
2. Then call `pool.RefreshTopics()` to start consumers

```go
// In New():
kb.topicRegistry.SetOnUpdate(func() {
    kb.wg.Add(1) // Constitution VII: Add BEFORE go statement
    go func() {
        defer logging.RecoverPanic(kb.logger, "topic_update", nil) // Constitution VII: RecoverPanic first
        defer kb.wg.Done() // Constitution VII: Done second
        kb.ensureTopicsExist() // Read from registry, create missing Kafka topics
        kb.pool.RefreshTopics() // Read from registry, start/refresh consumers
    }()
})
```

The callback runs asynchronously to avoid blocking the gRPC stream goroutine. Full Constitution VII goroutine lifecycle: `wg.Add(1)` before `go`, `RecoverPanic` as first defer, `wg.Done()` as second defer. `Shutdown()` calls `kb.wg.Wait()` (with timeout) before closing resources to ensure in-flight updates complete.

`ensureTopicsExist()` calls `registry.GetSharedTenantTopics()` and `registry.GetDedicatedTenants()` to discover current topics, then calls `admin.CreateTopics()` for each — Kafka's `TopicAlreadyExists` error is a no-op (same pattern as the provisioning `kafka.Admin.CreateTopic` that handled this). This follows the observer/signal pattern: the `func()` callback signals "something changed", each consumer pulls the current state from the registry independently.

## Phase 3 — DirectBackend

Implement the simplest backend: messages go directly to the broadcast bus.

### Files

| Action | File | Description |
|--------|------|-------------|
| CREATE | `ws/internal/server/backend/directbackend/directbackend.go` | `DirectBackend` implementation |

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
| MODIFY | `ws/internal/server/server.go` | Replace `kafkaConsumer`/`kafkaProducer` with `backend MessageBackend` |
| MODIFY | `ws/internal/server/handlers_publish.go` | Use `s.backend.Publish()` instead of `s.kafkaProducer.Publish()` |
| MODIFY | `ws/internal/server/handlers_message.go` | Rename `handleKafkaReconnect` → `handleReconnect`. Use `s.backend.Replay()` |
| MODIFY | `ws/internal/server/handlers_http.go` | Update `/health` endpoint: replace `s.config.KafkaConsumerDisabled` / `s.kafkaConsumer` check with `s.backend` health. Rename `"kafka"` → `"backend"` in JSON response. |
| MODIFY | `ws/internal/shared/types/types.go` | Replace `SharedKafkaConsumer any` and `KafkaProducer any` with `MessageBackend any`. Remove `KafkaBrokers []string` (now internal to KafkaBackend) and `KafkaConsumerDisabled bool` (replaced by `s.backend.IsHealthy()`). |
| MODIFY | `ws/internal/server/orchestration/shard.go` | Replace `SharedKafkaConsumer`/`KafkaProducer` with `MessageBackend` in `ShardConfig` |
| MODIFY | `ws/cmd/server/main.go` | Use inline switch to create backend, pass to shards. Move Kafka init into KafkaBackend. |

### Backend Selection (main.go)

Backend wiring is done inline in `main.go` via a `switch` on the `MESSAGE_BACKEND` config value:

```go
var msgBackend backend.MessageBackend
switch cfg.MessageBackend {
case "direct":
    msgBackend, err = directbackend.New(broadcastBus, logger)
case "kafka":
    msgBackend, err = kafkabackend.New(kafkabackend.Config{...})
case "nats":
    msgBackend, err = jetstreambackend.New(jetstreambackend.Config{...})
default:
    logger.Fatal().Str("backend", cfg.MessageBackend).Msg("Unknown message backend")
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
        MaxMessages:   backend.DefaultMaxReplayMessages, // named constant, not magic number
        Subscriptions: subs,
    })
```

`handlers_http.go`:
```go
// Before:
if s.config.KafkaConsumerDisabled {
    kafkaStatus = "disabled"
    kafkaHealthy = true
} else if s.kafkaConsumer != nil {
    kafkaStatus = "running"
    kafkaHealthy = true
} else { ... }

// After:
backendStatus := "unavailable"
backendHealthy := false
if s.backend == nil {
    backendStatus = "not_configured"
    backendHealthy = true // no backend configured — not a failure
} else if s.backend.IsHealthy() {
    backendStatus = "healthy"
    backendHealthy = true
} else { ... }
// JSON key: "backend" instead of "kafka"
```

### main.go Changes

The bulk of main.go's Kafka initialization (lines 140-260) moves into `KafkaBackend` construction. main.go uses an inline switch (see Backend Selection above) to create the correct backend, then starts it:

```go
if err := msgBackend.Start(ctx); err != nil {
    logger.Fatal().Err(err).Msg("Failed to start message backend")
}
```

## Phase 4b — Provisioning Cleanup

Remove KafkaAdmin from the provisioning service to make it fully backend-agnostic. Provisioning only records tenant metadata (categories) in the database and notifies ws-server via gRPC stream. All backend resource creation (Kafka topics, JetStream streams) is the ws-server's `MessageBackend` responsibility.

### Files

| Action | File | Description |
|--------|------|-------------|
| MODIFY | `ws/cmd/provisioning/main.go` | Always use `NoopKafkaAdmin` — remove Kafka admin initialization block (lines 156-198) |
| MODIFY | `ws/internal/shared/platform/provisioning_config.go` | Remove 9 Kafka admin config fields (`KafkaBrokers`, `KafkaAdminTimeout`, SASL/TLS). Update `Print()` to remove entire Kafka/Redpanda section (lines 312-320). Update `LogConfig()` (zerolog) to remove kafka_brokers, kafka_sasl_enabled, kafka_tls_enabled entries (lines 356-358). |
| MODIFY | `ws/internal/shared/testutil/provisioning_fixtures.go` | Remove `KafkaBrokers` and `KafkaAdminTimeout` from `ProvisioningConfig` fixture. |
| DELETE | `ws/internal/provisioning/kafka/admin.go` | Remove real `kafka.Admin` implementation — no longer needed |

### Changes

1. **`ws/cmd/provisioning/main.go`**: In API mode, the block that conditionally creates `provkafka.NewAdmin()` or `NoopKafkaAdmin` (lines 156-198) is replaced with unconditional `provisioning.NewNoopKafkaAdmin()`. Remove the `provkafka` import. Remove Kafka admin config fields from provisioning's config struct if they exist solely for admin creation (`KafkaBrokers`, `KafkaSASLEnabled`, etc. — verify which ones are used only by admin).

2. **`ws/internal/provisioning/kafka/admin.go`**: Delete this file entirely. The `kafka.Admin` struct and `provkafka.NewAdmin()` are no longer needed since provisioning always uses `NoopKafkaAdmin`. The `kafka.BuildTopicName()` function (if in this package) must be preserved or moved if other code uses it.

3. **Keep the `KafkaAdmin` interface** in `interfaces.go` — it's still used by the service's `createSingleTopic()` flow. The `NoopKafkaAdmin` implementation satisfies it. The interface + noop pattern allows provisioning to remain backend-agnostic: `createSingleTopic()` calls `s.kafka.CreateTopic()` (noop) then records metadata in the database.

## Phase 5 — JetStreamBackend (P2)

Implement NATS JetStream backend with persistent streams and sequence-based replay.

### Files

| Action | File | Description |
|--------|------|-------------|
| CREATE | `ws/internal/server/backend/jetstreambackend/jetstreambackend.go` | `JetStreamBackend` implementation |

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
    consumers  map[string]jetstream.ConsumeContext // stream → active consume context

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

- One stream per tenant: `ODIN_{namespace}_{tenant}` (e.g., `ODIN_DEV_ACME`)
- Stream names are uppercase-normalized with hyphens replaced by underscores (e.g., `my-namespace` → `MY_NAMESPACE`, `tenant-one` → `TENANT_ONE`). This is handled by `streamName()`.
- Subject filter: `{namespace}.{tenant}.>` captures all categories for that tenant
- Retention: `MaxAge` from config (default 24h)
- Replicas: from config (default 1)

### Topic Discovery

Like Kafka's `MultiTenantConsumerPool`, the JetStream backend needs to discover provisioned topics. Both backends use the `StreamTopicRegistry` — a gRPC streaming client that connects to the provisioning service and receives tenant/topic snapshots and deltas.

Decision: Reuse `TenantRegistry` interface for consistency. The backend periodically refreshes streams (same pattern as Kafka pool's `refreshLoop`).

### TenantRegistry Ownership

Both Kafka and JetStream backends need a `TenantRegistry` for topic discovery. The ws-server discovers topics via gRPC streaming from the provisioning service (NOT via a direct database connection). The ownership model:

- **KafkaBackend**: Creates its own `StreamTopicRegistry` (gRPC client to provisioning service) internally. Config includes `ProvisioningGRPCAddr`, `GRPCReconnectDelay`, `GRPCReconnectMaxDelay`.
- **JetStreamBackend**: Receives a `TenantRegistry` via its config. `main.go` creates the `StreamTopicRegistry` and passes it to the backend's `Config.Registry` field.

**Resolution**: `KafkaBackend` creates its own `StreamTopicRegistry` internally using the provisioning gRPC address from its config. `JetStreamBackend` receives a `TenantRegistry` via its config — `main.go` creates the `StreamTopicRegistry` for the `nats` case and passes it to the backend. For `direct` mode, no registry is needed.

This means `main.go` retains a `provapi` import for the `nats` case (registry creation) but does not need `orchestration` or `kafka` imports — those are internal to `KafkaBackend`.

### Replay Position Propagation

For Kafka, the broadcast message pipeline embeds topic offsets that clients echo back in `reconnect.last_offset`. For JetStream, the same mechanism must embed JetStream stream sequence numbers. The `Publish()` path for JetStream publishes to JetStream and receives an ack with the stream sequence — this sequence must be propagated through the broadcast bus message so clients can include it in reconnect requests. The exact mechanism (embedding in broadcast message metadata vs. separate tracking) will be designed during Phase 5 implementation, following the same pattern Kafka uses today.

**Note**: The `TenantRegistry` interface and `TenantTopics` struct currently live in `ws/internal/shared/kafka/tenant_registry.go`. Since JetStream also uses them, both must be moved to `ws/internal/shared/types/tenant_registry.go` before Phase 5 implementation. This is a broader refactor than just moving the interface — `TenantTopics` is used as a field type in `StreamTopicRegistry` (provapi), as a method parameter in `MultiTenantConsumerPool` (orchestration), and in provisioning's `TopicRegistry`. All 5 consuming files must update imports from `kafka.TenantRegistry`/`kafka.TenantTopics` to `types.TenantRegistry`/`types.TenantTopics`. This avoids a misleading `kafka` import in JetStream and provapi code (Constitution X — shared types used by multiple packages must not live in a service-specific package).

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
| CREATE | `ws/internal/server/backend/backend_test.go` | Interface contract tests (run against DirectBackend and mocks) |
| CREATE | `ws/internal/server/backend/directbackend/directbackend_test.go` | Direct backend unit tests |
| CREATE | `ws/internal/server/backend/kafkabackend/kafkabackend_test.go` | Kafka backend unit tests (SplitBrokers, buildKgoOpts, isTopicAlreadyExistsError, Publish validation) |
| CREATE | `ws/internal/server/backend/jetstreambackend/jetstreambackend_test.go` | JetStream backend unit tests (SplitURLs, streamName, constants, Publish validation) |
| MODIFY | `ws/internal/server/handlers_publish_test.go` | Add reconnect parsing and replay test cases |
| MODIFY | `ws/internal/server/handlers_message_test.go` | Add reconnect request parsing and MessageEnvelope serialization tests |
| CREATE | `ws/internal/server/broadcast/nats_tls_test.go` | NATS TLS configuration tests |
| CREATE | `ws/internal/server/broadcast/valkey_tls_test.go` | Valkey TLS configuration tests |

### Test Strategy

1. **Interface compliance**: Each backend has a compile-time interface check (`var _ backend.MessageBackend = (*XxxBackend)(nil)`).
2. **Direct backend**: Verify publish routes to broadcast bus, replay returns empty, health always true.
3. **Kafka backend**: Test helper functions (SplitBrokers, buildKgoOpts, isTopicAlreadyExistsError), input validation (empty channel, nil producer). Full integration requires Kafka cluster.
4. **JetStream backend**: Test helper functions (SplitURLs, streamName), constants, input validation (empty channel). Full integration requires NATS server.
5. **Handler tests**: Updated to use mock `MessageBackend` instead of mock Kafka types. Reconnect parsing and MessageEnvelope serialization verified.
6. **Broadcast bus TLS**: Verify TLS config is applied to NATS and Valkey connections. Test CA loading, invalid PEM, insecure skip, missing CA file.

## Phase 9 — Helm & Deploy

### Files

| Action | File | Description |
|--------|------|-------------|
| MODIFY | `deployments/helm/odin/charts/ws-server/templates/deployment.yaml` | Remove `wait-for-provisioning` init container (soft dependency — gRPC client reconnects asynchronously). Make `wait-for-redpanda` conditional on `messageBackend: kafka`. Add conditional `wait-for-nats-jetstream` for `messageBackend: nats` (separate from broadcast bus NATS). The `wait-for-nats` init container is conditional on `broadcast.type=nats` (only needed when NATS is the broadcast bus). Add `MESSAGE_BACKEND`, NATS JetStream config, and broadcast bus TLS env vars. |
| MODIFY | `deployments/helm/odin/charts/ws-server/templates/configmap.yaml` | Remove dead `KAFKA_TOPICS` entry. Make Kafka-specific entries conditional on `messageBackend: kafka`. |
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
| CREATE | 11 | `backend/backend.go`, `backend/backend_test.go`, `backend/directbackend/directbackend.go`, `backend/kafkabackend/kafkabackend.go`, `backend/jetstreambackend/jetstreambackend.go`, `backend/directbackend/directbackend_test.go`, `backend/kafkabackend/kafkabackend_test.go`, `backend/jetstreambackend/jetstreambackend_test.go`, `broadcast/nats_tls_test.go`, `broadcast/valkey_tls_test.go`, `shared/types/tenant_registry.go` (moved from kafka/) |
| MODIFY | 30 | `server.go`, `server_test.go`, `handlers_publish.go`, `handlers_publish_test.go`, `handlers_message.go`, `handlers_message_test.go`, `handlers_http.go`, `types.go`, `types_test.go`, `shard.go`, `main.go` (ws-server), `main.go` (provisioning), `server_config.go`, `server_config_test.go`, `metrics.go`, `broadcast/bus.go`, `broadcast/nats.go`, `broadcast/valkey.go`, `provapi/topic_registry.go`, `orchestration/multitenant_pool.go`, `orchestration/multitenant_pool_test.go`, `shared/kafka/producer.go`, `provisioning/topic_registry.go`, `provisioning/topic_registry_test.go`, `provisioning_config.go`, `testutil/provisioning_fixtures.go`, Helm (3: `deployment.yaml`, `configmap.yaml`, `values.yaml`), `dev.yaml` |
| DELETE | 4 | `provisioning/kafka/admin.go`, `provisioning/kafka/admin_test.go`, `provisioning/kafka/admin_integration_test.go`, `shared/kafka/tenant_registry.go` (moved to shared/types/) |
| TOTAL | 45 | |

## Phase Dependencies

```
Phase 1 (Interface + Config) ─── Phase 2 (KafkaBackend) ───┐
                              └── Phase 3 (DirectBackend) ──┤
                                                            ├── Phase 4 (Server Integration)
                                                            │
                                        Phase 4b (Provisioning Cleanup) ──┐
                                                                          │
Phase 5 (JetStreamBackend) ───────────────────────────────────────────────┘
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
- Phase 4b (provisioning cleanup) depends on Phase 4 — separate service, can be done after ws-server integration
- Phase 5 depends on Phase 4b — needs provisioning to be backend-agnostic before adding new backend
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
