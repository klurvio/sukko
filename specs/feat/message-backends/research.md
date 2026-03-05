# Research: Pluggable Message Backends

**Branch**: `feat/message-backends` | **Date**: 2026-02-27

## R1. Current Kafka Coupling Points

**Question**: Where exactly is Kafka coupled into the ws-server, and what are the abstraction boundaries?

**Finding**: Kafka is coupled at 5 distinct points:

1. **`cmd/server/main.go:140-260`** — Creates `MultiTenantConsumerPool` and `kafka.Producer`. Passes them to shards via `ShardConfig`. This is the wiring point.

2. **`server/server.go:49-50`** — `Server` struct holds `kafkaConsumer *kafka.Consumer` and `kafkaProducer *kafka.Producer` as concrete types, not interfaces. Set at lines 154-166 via type assertions from `config.SharedKafkaConsumer` (`any`).

3. **`server/handlers_publish.go:36,90`** — `handleClientPublish()` checks `s.kafkaProducer == nil` and calls `s.kafkaProducer.Publish(ctx, c.id, channel, data)`. Publish is synchronous (waits for Kafka ack).

4. **`server/handlers_message.go:219,236`** — `handleKafkaReconnect()` checks `s.kafkaConsumer == nil` and calls `s.kafkaConsumer.ReplayFromOffsets(ctx, offsets, maxMessages, subscriptions)`. Returns `[]kafka.ReplayMessage`.

5. **`server/orchestration/multitenant_pool.go`** — Manages consumer lifecycle, topic discovery, and routes consumed messages to broadcast bus. This entire package is Kafka-specific.

**Decision**: The abstraction layer sits between the server and these 5 points. The server should depend on a `MessageBackend` interface, not on concrete Kafka types. The `MultiTenantConsumerPool` and `kafka.Producer` become internal implementation details of `KafkaBackend`.

## R2. MessageBackend Interface Design

**Question**: What interface can unify direct, Kafka, and NATS JetStream backends?

**Finding**: The server uses exactly 3 capabilities from the Kafka layer:
1. **Publish** — `s.kafkaProducer.Publish(ctx, clientID, channel, data)` returns `error`
2. **Replay** — `s.kafkaConsumer.ReplayFromOffsets(ctx, offsets, max, subs)` returns `[]ReplayMessage, error`
3. **Health/lifecycle** — startup, shutdown, health checks

The consumer pool runs independently (started in `main.go`, pushes to broadcast bus). Shards don't interact with the consumer pool for normal message flow — they subscribe to the broadcast bus.

**Decision**: The interface has 5 methods:

```go
type MessageBackend interface {
    Start(ctx context.Context) error
    Publish(ctx context.Context, clientID int64, channel string, data []byte) error
    Replay(ctx context.Context, req ReplayRequest) ([]ReplayMessage, error)
    IsHealthy() bool
    Shutdown(ctx context.Context) error
}
```

`ReplayRequest` and `ReplayMessage` are backend-agnostic types. `ReplayMessage` mirrors `kafka.ReplayMessage` but without Kafka-specific fields (Topic, Partition, Offset). Only `Subject` and `Data` are needed by the handler.

**Alternative considered**: Separate `Publisher` and `Replayer` interfaces. Rejected because the lifecycle (Start/Shutdown) is shared across both concerns, and splitting them adds complexity without benefit. A single interface is cleaner.

## R3. Direct Backend Publish Path

**Question**: How does direct mode route published messages to subscribers without Kafka?

**Finding**: In the current architecture, client-published messages take a round trip:
```
Client → Producer → Kafka → Consumer → BroadcastBus → Shards → Subscribers
```

In direct mode, the message should skip Kafka entirely:
```
Client → DirectBackend.Publish() → BroadcastBus → Shards → Subscribers
```

The `DirectBackend` needs a reference to the `broadcast.Bus` to publish messages directly. The broadcast bus already handles inter-pod distribution (NATS Core / Valkey), so multi-pod direct mode works correctly — the message reaches all pods via the bus.

**Decision**: `DirectBackend.Publish()` calls `bus.Publish(&broadcast.Message{Subject: channel, Payload: data})` directly. Zero external system involvement. The broadcast bus handles the rest.

For replay: `DirectBackend.Replay()` returns `nil, nil` — no messages, no error. The handler sends `reconnect_ack` with `messages_replayed: 0`.

## R4. NATS JetStream Consumer/Producer Patterns

**Question**: How should NATS JetStream consume and produce messages for the ws-server?

**Finding**: The `nats.go` library provides JetStream API via `jetstream` package (`github.com/nats-io/nats.go/jetstream`). Key patterns:

- **Streams** → equivalent to Kafka topics. Each tenant's categories map to streams.
- **Subjects** → equivalent to Kafka keys. The channel name is the subject.
- **Consumers** → equivalent to Kafka consumer groups. Durable consumers track sequence numbers.
- **Publish** → `js.Publish(ctx, subject, data)` — synchronous, returns ack with sequence number.
- **Replay** → `consumer.FetchBySequence(startSeq)` or create ordered consumer starting from a sequence.

Stream naming: `SUKKO_{namespace}_{tenant}_{category}` (e.g., `SUKKO_dev_acme_trade`).
Subject naming: `sukko.{tenant}.{identifier}.{category}` (matches existing channel format).

**Decision**: `JetStreamBackend` manages streams per tenant/category (mirroring Kafka topics). It creates a push subscription that routes messages to the broadcast bus, similar to how the Kafka consumer pool works. For replay, it uses ordered consumers starting from a given sequence number.

## R5. Multi-Tenant Stream Isolation in JetStream

**Question**: How does JetStream handle multi-tenant isolation equivalent to Kafka's topic-per-tenant model?

**Finding**: JetStream streams can filter by subject. Two approaches:

1. **Stream-per-tenant**: Each tenant gets its own stream (e.g., `SUKKO_dev_acme`). Subjects within the stream use wildcards: `sukko.acme.>`. Strong isolation, independent retention.

2. **Stream-per-category**: A single stream with subject filtering (e.g., `SUKKO_dev_trade` captures `sukko.*.*.trade`). Simpler but weaker isolation — all tenants share retention and limits.

**Decision**: Stream-per-tenant (option 1). This matches Kafka's topic-per-tenant model and provides the same isolation guarantees. Stream subjects use `sukko.{tenant}.>` wildcards. This is a P2 feature so detailed design will be in the JetStream implementation phase.

## R6. Replay Abstraction — Offsets vs Sequences

**Question**: Kafka uses offsets (per-topic, per-partition), NATS JetStream uses stream sequences, and direct mode has no replay. How to abstract this?

**Finding**: The current reconnect request sends:
```json
{"client_id": "...", "last_offset": {"topic1": 12345, "topic2": 67890}}
```

This is Kafka-specific. The client shouldn't need to know the backend's position tracking mechanism.

**Decision**: The `ReplayRequest` type uses `map[string]int64` for positions — generic enough for both offsets and sequences. The key is the topic/stream name, the value is the position (offset for Kafka, sequence for JetStream).

For the client protocol, the position field name stays generic in the response (the client receives it and sends it back — it's opaque). The existing `last_offset` field name in the reconnect request is Kafka-specific but changing it is a protocol change (out of scope). Instead, the backend interprets the map values appropriately:
- **Kafka**: keys = topic names, values = Kafka offsets
- **JetStream**: keys = stream names, values = JetStream sequence numbers
- **Direct**: map is ignored, returns empty replay

## R7. Server Struct Refactoring Scope

**Question**: How much does the `Server` struct need to change?

**Finding**: The `Server` struct has two Kafka fields:
```go
kafkaConsumer *kafka.Consumer  // line 49
kafkaProducer *kafka.Producer  // line 50
```

These are set via type assertions in `NewServer()` (lines 154-166) from `config.SharedKafkaConsumer` (any) and `config.KafkaProducer` (any). The `types.ServerConfig` struct also has these as `any` fields.

Only two handler methods reference these fields:
- `handleClientPublish()` → `s.kafkaProducer`
- `handleKafkaReconnect()` → `s.kafkaConsumer`

**Decision**: Replace both fields with a single `backend MessageBackend` field. Update the two handlers to call `s.backend.Publish()` and `s.backend.Replay()`. The `types.ServerConfig` `any` fields are replaced with a typed `MessageBackend` field. This is a minimal, surgical change.

## R8. ShardConfig and Wiring Changes

**Question**: How do shards receive the backend reference?

**Finding**: Currently `ShardConfig` has:
```go
SharedKafkaConsumer any  // Passed through to server.Server
KafkaProducer       any  // Passed through to server.Server
```

These are set in `main.go:313-325` and passed through to `server.NewServer()` via `types.ServerConfig`.

**Decision**: Replace both fields with `MessageBackend MessageBackend`. The shard passes it through to the server config. The server stores it directly — no type assertions needed. This simplifies the wiring and removes the `any` fields.
