# Feature Specification: Pluggable Message Backends

**Branch**: `feat/message-backends`
**Created**: 2026-02-27
**Status**: Implemented

## Context

Sukko currently requires Kafka/Redpanda as its message ingestion and persistence layer. External data enters the system via Kafka consumers, client-published messages are routed through Kafka, and message replay on reconnection reads from Kafka offsets. This creates a hard dependency on Kafka infrastructure for every deployment.

The inter-pod broadcast layer (NATS Core / Valkey) is already abstracted behind a `Bus` interface — switching between NATS and Valkey is a configuration change. But the ingestion layer (Kafka) has no such abstraction. The consumer, producer, multi-tenant pool, and replay logic are all tightly coupled to franz-go.

This limits the product in three ways:

1. **High barrier to entry** — teams without existing Kafka infrastructure must set up a Kafka/Redpanda cluster just to evaluate Sukko. Competitors like Pusher require zero backend infrastructure.
2. **Over-provisioned for simple use cases** — a team that only needs real-time publish/subscribe (no persistence, no replay) still needs Kafka running.
3. **No middle ground** — teams that want persistence and replay but find Kafka too heavy have no lighter alternative.

This spec introduces a pluggable message backend system with three options:

- **Direct** — no external message bus for ingestion. Client-published messages flow directly to the broadcast bus without passing through Kafka or NATS JetStream. No persistence, no replay. The only infrastructure requirement is the inter-pod broadcast bus (NATS Core or Valkey).
- **Kafka/Redpanda** — current behavior. Full persistence, offset-based replay, multi-tenant consumer isolation. For teams with existing Kafka infrastructure or high-reliability requirements.
- **NATS JetStream** — persistent streams with sequence-based replay. Lighter than Kafka, supports multi-tenant stream isolation. Connects to an external NATS server with JetStream enabled. A middle ground between direct and Kafka.

The message backend is selected via a single configuration value. The rest of the system — gateway, proxy, subscription index, WebSocket protocol, client-facing behavior — remains unchanged regardless of backend.

## User Scenarios

### Scenario 1 — Direct Mode: Zero-Infrastructure Pub/Sub (Priority: P1)

A developer evaluating Sukko runs the server without Kafka or NATS JetStream. Clients connect, subscribe, and publish messages to each other in real time. The only infrastructure requirement is the broadcast bus (NATS Core or Valkey) for inter-pod communication.

**Acceptance Criteria**:
1. **Given** `MESSAGE_BACKEND=direct`, **When** the ws-server starts, **Then** it starts successfully without any Kafka or NATS JetStream connection. The only requirement is the inter-pod broadcast bus (NATS Core or Valkey).
2. **Given** a running server in direct mode, **When** a client publishes a message to a channel, **Then** all clients subscribed to that channel receive the message immediately without the message passing through any external system.
3. **Given** a running server in direct mode, **When** a client sends a `reconnect` request, **Then** the server responds with `reconnect_ack` with `messages_replayed: 0` — replay is not available in direct mode.

### Scenario 2 — Kafka/Redpanda: Full Persistence and Replay (Priority: P1)

An operator runs Sukko with Kafka/Redpanda for production-grade persistence, replay, and multi-tenant consumer isolation. This is the current behavior, preserved exactly.

**Acceptance Criteria**:
1. **Given** `MESSAGE_BACKEND=kafka`, **When** the ws-server starts, **Then** it connects to Kafka brokers, starts consumer groups, and begins consuming messages — identical to current behavior.
2. **Given** a running server with Kafka backend, **When** a client publishes a message, **Then** the message is produced to the appropriate Kafka topic and consumed back through the standard pipeline.
3. **Given** a running server with Kafka backend, **When** a client sends a `reconnect` request with offsets, **Then** the server replays messages from the specified Kafka offsets — identical to current behavior.

### Scenario 3 — NATS JetStream: Lightweight Persistence (Priority: P2)

An operator runs Sukko with NATS JetStream for message persistence and replay without the operational overhead of Kafka.

**Acceptance Criteria**:
1. **Given** `MESSAGE_BACKEND=nats`, **When** the ws-server starts, **Then** it connects to NATS with JetStream enabled, creates streams for configured tenants/topics, and begins consuming messages.
2. **Given** a running server with NATS JetStream backend, **When** a client publishes a message, **Then** the message is published to the appropriate JetStream stream and delivered to subscribers.
3. **Given** a running server with NATS JetStream backend, **When** a client sends a `reconnect` request, **Then** the server replays messages from the JetStream stream using sequence numbers.
4. **Given** a running server with NATS JetStream backend, **When** a new tenant topic is provisioned, **Then** a corresponding JetStream stream is created automatically.

### Scenario 4 — Backend-Agnostic Client Experience (Priority: P1)

Clients connect and interact with Sukko identically regardless of the message backend. The WebSocket protocol is unchanged.

**Acceptance Criteria**:
1. **Given** any message backend, **When** a client subscribes, publishes, or unsubscribes, **Then** the WebSocket protocol messages and responses are identical.
2. **Given** direct mode where replay is unavailable, **When** a client sends a `reconnect` request, **Then** the server responds with `reconnect_ack` (not an error) indicating zero messages replayed. The client is not broken by the lack of replay — it gracefully receives no replayed data.
3. **Given** any message backend, **When** the server broadcasts a message, **Then** it includes the same fields (`type`, `channel`, `data`, `seq`, `ts`) regardless of backend.

### Edge Cases

- What happens when the message backend is unavailable at startup? The server MUST fail fast with a clear error (Kafka connection refused, NATS unreachable). Exception: direct mode has no external dependency and always starts.
- What happens when the message backend becomes unavailable mid-operation? Kafka: circuit breaker opens, publishes fail with `service_unavailable`. NATS JetStream: the nats.go client handles reconnection automatically with configurable backoff; application-level circuit breaker is not needed — publishes fail with wrapped errors during disconnection. Direct: cannot happen (in-process).
- What happens when a client requests replay in direct mode? The server returns `reconnect_ack` with `messages_replayed: 0`. No error — the feature is simply not available.
- What happens when switching backends on an existing deployment? Message history is not migrated between backends. The switch is a clean cut — new messages use the new backend.
- What happens with multi-tenant isolation in direct mode? Tenant isolation at the channel level (subscription filtering, permission checks) still works. There is no topic-level isolation because there are no topics.
- What happens with multi-tenant isolation in NATS JetStream? Each tenant's topics map to JetStream streams with subject-based filtering, providing isolation equivalent to Kafka's topic-per-tenant model.

## Requirements

### Functional Requirements

**Core Abstraction**

- **FR-001**: The ws-server MUST support a `MESSAGE_BACKEND` setting with values `direct` (default), `kafka`, and `nats`.
- **FR-002**: The message backend MUST be abstracted behind a common interface that supports: starting ingestion, publishing messages, replaying messages on reconnect, health checking, and graceful shutdown.
- **FR-003**: The ws-server's broadcast path (subscription index, per-client sequencing, write pump) MUST be completely backend-agnostic — it receives messages from the backend and distributes them to clients without knowledge of the source.
- **FR-004**: The existing inter-pod broadcast bus (`Bus` interface with NATS Core / Valkey implementations) MUST remain independent of the message backend choice.

**Direct Mode**

- **FR-010**: In direct mode, client-published messages MUST be routed directly to the broadcast bus without passing through any external system.
- **FR-011**: In direct mode, the server MUST NOT attempt to connect to Kafka or any external message broker at startup.
- **FR-012**: In direct mode, reconnect requests MUST return `reconnect_ack` with zero messages replayed.

**Kafka/Redpanda Mode**

- **FR-020**: In Kafka mode, the server MUST preserve the existing behavior: consumer group-based ingestion, producer-based publishing, and offset-based replay. The `KafkaBackend` wraps the existing `MultiTenantConsumerPool` and `Producer` without behavioral changes.
- **FR-021**: Kafka mode MUST be activated when `MESSAGE_BACKEND=kafka` is explicitly set.

**NATS JetStream Mode**

- **FR-030**: In NATS JetStream mode, the server MUST create JetStream streams corresponding to provisioned tenant topics.
- **FR-031**: In NATS JetStream mode, client-published messages MUST be published to the appropriate JetStream stream.
- **FR-032**: In NATS JetStream mode, reconnect requests MUST replay messages from JetStream using stream sequence numbers.
- **FR-033**: In NATS JetStream mode, the server MUST support multi-tenant isolation via stream-per-tenant or subject-based filtering.

**Broadcast Bus — Managed Service Support**

- **FR-050**: The NATS broadcast bus MUST support TLS connections for managed NATS services (Synadia Cloud, etc.) via `NATS_TLS_ENABLED`, `NATS_TLS_INSECURE`, and `NATS_TLS_CA_PATH` configuration.
- **FR-051**: The Valkey broadcast bus MUST support TLS connections for managed Valkey/Redis services (AWS ElastiCache, Google Memorystore, Upstash, etc.) via `VALKEY_TLS_ENABLED`, `VALKEY_TLS_INSECURE`, and `VALKEY_TLS_CA_PATH` configuration.

### Non-Functional Requirements

- **NFR-001**: Switching message backends MUST require only a configuration change (`MESSAGE_BACKEND`). No code changes, no recompilation.
- **NFR-002**: Direct mode MUST add zero latency compared to the current Kafka path for client-to-client messaging (messages skip external systems entirely).
- **NFR-003**: The message backend abstraction MUST NOT degrade performance of the existing Kafka path. The abstraction layer overhead MUST be less than 1 microsecond per message.
- **NFR-004**: NATS JetStream mode MUST support at least 10,000 messages/second throughput per stream. Verification via load test is deferred to the performance validation phase.
- **NFR-005**: Each message backend MUST expose health status and operational metrics via the existing Prometheus metrics system.
- **NFR-006**: The periodic topic/stream refresh interval MUST be configurable via `TOPIC_REFRESH_INTERVAL` (default: 60s). This applies to both Kafka and NATS JetStream backends as a safety net for missed gRPC stream events.

### Key Entities

- **MessageBackend**: The pluggable ingestion and persistence layer. Implementations: `DirectBackend`, `KafkaBackend`, `JetStreamBackend`.
- **BroadcastBus**: The existing inter-pod distribution layer (unchanged). Implementations: NATS Core, Valkey.

## Success Criteria

- **SC-001**: `MESSAGE_BACKEND=direct` starts a fully functional WebSocket server that requires only the broadcast bus (NATS Core or Valkey) — no Kafka or JetStream infrastructure needed.
- **SC-002**: The existing Kafka integration passes all current tests when `MESSAGE_BACKEND=kafka` with no behavioral changes.
- **SC-003**: NATS JetStream mode supports publish, subscribe, and replay with sequence-based message recovery.
- **SC-004**: A developer can go from zero to a working real-time pub/sub system in under 60 seconds with a single binary and a config file.
- **SC-005**: All three backends pass the same integration test suite for core pub/sub operations (subscribe, publish, receive). **Note**: Each backend has unit tests verifying interface compliance. A shared integration test suite requiring live Kafka/NATS infrastructure is deferred to the integration testing phase.

## Clarifications

- Q: Should embedded NATS server be included in this spec? → A: **No.** Embedding NATS in-process prevents horizontal scaling — each pod would have an isolated server with no shared streams. NATS JetStream always connects to an external NATS server. The broadcast bus is also out of scope for this spec.
- Q: What should the default value of MESSAGE_BACKEND be? → A: **`direct`**. No production deployments exist yet, so backward compatibility is not a concern. Default to the lowest-friction path (zero dependencies).
- Q: Should HTTP ingestion (POST /v1/publish) be included? → A: **No.** All backends are client-to-client only. External system ingestion into Kafka/NATS is outside Sukko's scope — external systems use their own Kafka/NATS clients directly. No special Sukko code needed.
- Q: Should single-pod / in-memory broadcast be in this spec? → A: **No.** All message backends support multi-pod deployment via the existing broadcast bus (NATS Core / Valkey). The broadcast bus is out of scope for this spec.
- Q: Do the backends support managed/cloud services? → A: **Yes.** Kafka mode already supports SASL + TLS for managed services (Confluent Cloud, AWS MSK, Redpanda Cloud). NATS JetStream mode includes TLS + auth config for managed NATS (Synadia Cloud). Operators point the config at their managed service URLs and provide credentials — no special "managed" mode needed.
- Q: How should the multi-tenant consumer pool relate to the MessageBackend abstraction? → A: **Each backend owns its own ingestion/isolation strategy.** Kafka wraps the existing `MultiTenantConsumerPool` internally (shared/dedicated consumer groups). JetStream uses stream-per-tenant for natural isolation. Direct mode has no ingestion layer. The ws-server only interacts with the `MessageBackend` interface — pool management is an internal implementation detail of each backend.
- Q: Who creates backend-specific resources (Kafka topics, JetStream streams) when tenants are provisioned? → A: **The ws-server's MessageBackend creates resources on demand** when it receives tenant config from the provisioning gRPC stream (`WatchTopics`). Provisioning stays backend-agnostic — it only manages tenant metadata. This does NOT affect hot paths: resource creation is a cold-path operation (tenant onboarding), while message publish/consume only touches pre-existing resources.
- Q: In direct mode, does the ws-server need its own publish permission check? → A: **No, the gateway's check is sufficient.** The gateway validates channel permissions before forwarding. Defense-in-depth is provided by NetworkPolicy (only gateway can reach ws-server). No duplicate auth logic in ws-server — keeps the publish hot path minimal.
- Q: How should tenant topics map to JetStream streams and subjects? → A: **Stream-per-tenant.** One JetStream stream per tenant (e.g., `prod-acme`) capturing all their subjects via wildcard (`prod.acme.>`). Each category is a subject within the stream (e.g., `prod.acme.trade`, `prod.acme.liquidity`). One durable consumer per stream. This provides natural noisy-tenant isolation (separate storage per tenant, separate stream limits) with fewer resources than Kafka's topic-per-category model.
- Q: When both broadcast bus and message backend use NATS, should they share a connection? → A: **No, separate connections.** Each component manages its own NATS connection independently. This allows pointing at different NATS clusters (e.g., JetStream on a dedicated cluster, Core on a lightweight one) and simplifies lifecycle management.
- Q: Should the `wait-for-provisioning` init container be conditional on the message backend? → A: **Remove it entirely.** The `StreamTopicRegistry` already handles reconnection with exponential backoff — it's a soft dependency that connects asynchronously. The pod starts immediately; topic data appears when provisioning connects. Direct mode never creates a registry (zero impact). The `wait-for-nats` init container is conditional on `broadcast.type=nats` (only needed when NATS is the broadcast bus). The `wait-for-redpanda` stays conditional on kafka mode (KafkaBackend fails fast if brokers unreachable).
- Q: How should the KafkaBackend's on-demand topic creation integrate with the `SetOnUpdate` callback? → A: **Use the existing `func()` (no arguments) observer/signal pattern.** The callback signals "topics changed" — `ensureTopicsExist()` reads from `registry.GetSharedTenantTopics()` and `GetDedicatedTenants()` to discover topics, then creates missing ones via `kadm.Client`. This matches how `pool.RefreshTopics()` already works (reads from registry internally). The callback is a signal, not a data delivery — Go idiomatic pull model.
- Q: Should the provisioning service's KafkaAdmin be kept for Kafka topic creation, or should all backend resource creation move to ws-server? → A: **Remove KafkaAdmin from provisioning entirely.**
- Q: How do clients obtain stream sequence numbers for JetStream replay? → A: **Replay position propagation is deferred to a future spec.** The Replay API works correctly when clients provide valid positions. The mechanism for clients to discover these positions (embedding backend-specific sequences in broadcast messages) is a cross-cutting concern that applies to both Kafka and JetStream backends. The current implementation supports replay when clients supply positions via the `reconnect.last_offset` field.
- Q: Where is TLS required in the system? → A: **Only for managed external services.** TLS is required for: (1) managed provisioning DB (Cloud SQL — handled outside this spec), (2) managed broadcast bus (NATS/Valkey — FR-050, FR-051), and (3) managed message backend (Kafka SASL/TLS already exists, JetStream TLS config added). No TLS is needed for internal pod-to-pod communication within the cluster. All backend resource creation (Kafka topics, JetStream streams) is the ws-server's MessageBackend responsibility. Provisioning only records tenant metadata (categories) in the database and notifies via gRPC stream. The ws-server's KafkaBackend creates Kafka topics on demand when it discovers new categories from the provisioning gRPC stream. This makes provisioning fully backend-agnostic — it has no knowledge of Kafka, JetStream, or direct mode.

## Out of Scope

- **Message migration between backends**: Switching from Kafka to NATS JetStream does not migrate message history. It's a clean cut.
- **Embedded Kafka**: Kafka cannot be embedded in a Go binary. It remains an external dependency when selected.
- **Embedded NATS server**: Embedding NATS in-process prevents horizontal scaling (each pod would have an isolated server with no shared streams). NATS JetStream mode always connects to an external NATS server.
- **Embedded NATS for broadcast bus**: Embedding NATS for the inter-pod broadcast bus is out of scope. The broadcast bus remains unchanged (external NATS Core / Valkey / in-memory).
- **Custom backend plugins**: The backend interface is internal. Third-party backend plugins (e.g., RabbitMQ, Pulsar) are not supported in this iteration but the interface design should not preclude them.
- **Changing the inter-pod broadcast bus logic**: The `Bus` interface (NATS Core / Valkey) behavior is unchanged. The only addition is TLS config for managed service connectivity (FR-050, FR-051).
- **Protocol changes**: The WebSocket protocol between clients and server is unchanged. Clients are unaware of which backend is in use.
