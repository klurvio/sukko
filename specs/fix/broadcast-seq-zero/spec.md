# Feature Specification: Fix Broadcast Message Sequence Numbers

**Branch**: `fix/broadcast-seq-zero`
**Created**: 2026-02-25
**Status**: Draft

## Context

The `MessageEnvelope` protocol defines a `seq` field for per-client gap detection — a standard feature in financial WebSocket platforms (FIX protocol, Coinbase, Binance). Clients are expected to track sequence numbers to detect missing messages and request replays.

Currently, all broadcast messages are sent with `seq: 0` to every client. This is a deliberate performance optimization introduced to serialize the message envelope **once** for all subscribers rather than per-client (99.92% reduction in JSON marshalling overhead, enabling 12K connections vs 6.5K). However, it completely breaks the client-side gap detection contract documented in the `MessageEnvelope` struct itself.

The per-client `SequenceGenerator` exists and works correctly — it is already used for Kafka replay messages on reconnect — but is bypassed in the main broadcast path (`broadcast.go:115-122`).

**Impact**: Clients cannot detect dropped or missing messages. The `seq` field is effectively dead metadata consuming bandwidth on every message.

## User Scenarios

### Scenario 1 - Client-side Gap Detection (Priority: P1)
A WebSocket client subscribes to `sukko.BTC.trade` and receives a stream of messages. The client tracks the `seq` field to detect if any messages were dropped (e.g., due to slow-client buffer overflow or network issues). When a gap is detected, the client can alert the user or request a replay.

**Acceptance Criteria**:
1. **Given** a connected client subscribed to a channel, **When** broadcast messages are delivered, **Then** each message MUST contain a monotonically increasing `seq` value unique to that client connection (starting at 1).
2. **Given** two clients subscribed to the same channel, **When** the same broadcast event occurs, **Then** each client MUST receive a different `seq` value (their own per-connection sequence).
3. **Given** a client receiving messages on multiple channels, **When** messages arrive from different channels, **Then** the `seq` values MUST still be monotonically increasing across all channels for that connection.

### Scenario 2 - Performance Preservation (Priority: P1)
The current single-serialization optimization is critical to maintaining 12K+ concurrent connections at acceptable CPU usage. The fix MUST NOT regress to per-client full JSON marshalling.

**Acceptance Criteria**:
1. **Given** 10K connected clients and 25 messages/sec throughput, **When** broadcast occurs, **Then** the system MUST NOT perform per-client `json.Marshal()` of the full envelope.
2. **Given** the same load, **When** comparing CPU usage before and after the fix, **Then** broadcast loop per-client cost MUST NOT exceed 50ns (current: ~15ns channel send; after: ~25ns atomic + channel send). Per-client byte assembly is deferred to write pump goroutines.

### Scenario 3 - Replay Sequence Continuity (Priority: P2)
When a client reconnects and receives Kafka-based replay messages, the sequence numbers on replayed messages should be consistent with the new connection's sequence space.

**Acceptance Criteria**:
1. **Given** a client that reconnects, **When** replay messages are delivered, **Then** replay messages MUST use the same per-connection `SequenceGenerator` so sequences are continuous from the client's perspective.

### Edge Cases
- What happens when `seq` overflows `int64`? At 100K msg/sec, overflow takes ~2.9 million years — not a concern.
- What happens when a message is dropped (slow client, buffer full)? The sequence number MUST be consumed (assigned) even when the message is dropped, so the client sees a gap (e.g., 5 → 7) and knows it missed something. This is standard financial platform behavior.
- What happens with very large subscriber lists (10K+)? Per-client cost in the broadcast loop is ~25ns (atomic + channel send). Byte assembly (~100ns) is deferred to write pump goroutines and parallelized across CPU cores.

## Requirements

### Functional Requirements
- **FR-001**: Every broadcast message delivered to a client MUST contain a per-connection, monotonically increasing `seq` value starting at 1.
- **FR-002**: The `SequenceGenerator` already assigned to each client connection MUST be the source of sequence numbers for broadcast messages.
- **FR-003**: Sequence numbers MUST be consistent across all message types a client receives (broadcast, replay, control) — one sequence space per connection.
- **FR-004**: The wire format MUST be byte-identical to what `json.Marshal(MessageEnvelope{...})` produces, with the only difference being `seq` containing the real per-client value instead of `0`. The field order MUST remain: `type`, `seq`, `ts`, `channel`, `data`. The `channel` value MUST be properly JSON-escaped. Example:
  ```
  Before: {"type":"message","seq":0,"ts":1708903200000,"channel":"sukko.BTC.trade","data":{...}}
  After:  {"type":"message","seq":1234,"ts":1708903200000,"channel":"sukko.BTC.trade","data":{...}}
  ```

### Non-Functional Requirements
- **NFR-001**: The broadcast path MUST NOT perform per-client `json.Marshal()`. Shared byte fragments MUST be pre-computed once per broadcast. Per-client byte assembly MUST use `append()` (300-600x faster than `json.Marshal`), deferred to the write pump goroutine.
- **NFR-002**: The broadcast loop MUST remain O(1) per client — only `seqGen.Next()` (~10ns atomic) and a channel send (~15ns), totalling ~25ns/client. The current implementation is ~15ns/client (channel send only). The ~10ns delta from the atomic increment is negligible at scale. No per-client byte allocation or assembly in the broadcast loop.
- **NFR-003**: The solution MUST be thread-safe — multiple shards broadcast concurrently, and the per-client `SequenceGenerator` uses `atomic.Int64`. The `BroadcastEnvelope` is immutable and safe for concurrent reads.
- **NFR-004**: Sequence injection MUST always be enabled — no configuration toggle. The overhead must be inherently small enough to not require a fallback.
- **NFR-005**: The `send` channel type changes from `chan []byte` to `chan OutgoingMsg`. The `OutgoingMsg` type carries either pre-built bytes (for acks, errors, replay) or a deferred broadcast envelope (shared template + per-client seq). The write pump resolves to `[]byte` before writing.

## Cleanup

### Obsolete Code (remove/replace)
- `broadcast.go:115-122` — `baseEnvelope := &messaging.MessageEnvelope{...}` struct literal with `Seq: 0`. Replace with `BroadcastEnvelope`.
- `broadcast.go:125` — `baseEnvelope.Serialize()` call. No longer needed; write pump builds bytes.
- `broadcast.go:99-134` — Optimization comments referencing `json.Marshal` overhead. Update to reflect the write-pump deferred assembly approach.

### Must NOT Remove (still used by replay path)
- `messaging/message.go` — `MessageEnvelope` struct, `Serialize()`, `WrapMessage()`, `SequenceGenerator`, all Priority constants. ALL still used by `handlers_message.go:256-278` (Kafka reconnect replay). Do not touch.
- `handlers_message.go:256-278` — Replay path creates per-client envelopes via `MessageEnvelope{}` + `Serialize()`. Untouched by this change (sends pre-built bytes via `OutgoingMsg{raw: ...}`).
- `connection.go:62,168,172` — Per-client `seqGen` initialization and reset. Untouched.

### Senders to Update (mechanical wrap in OutgoingMsg)
All production code writing to `client.send` must wrap `[]byte` in `OutgoingMsg{raw: data}`:
- `handlers_message.go:57,121,163,269,290,316` — heartbeat, subscribe/unsubscribe acks, replay, reconnect ack, errors
- `handlers_publish.go:135` — publish ack
- `pump.go:273` — rate limit error

### Tests to Update
- `broadcast_test.go` — Add tests for new broadcast with per-client seq and `OutgoingMsg.Bytes()` dispatch.
- `pump_test.go` — Update to use `OutgoingMsg` channel type.
- `handlers_message_test.go` — Update test sends to use `OutgoingMsg{raw: ...}`.
- `client_lifecycle_test.go` — Update test sends.
- `metrics_test.go` — Update test sends.
- `messaging/message_test.go` — Keep all existing tests unchanged. Add `broadcast_envelope_test.go`.

## Regression Protection

- **RP-001**: The replay path (`handlers_message.go:handleKafkaReconnect`) MUST NOT be modified. It continues to use `MessageEnvelope{}` + `Serialize()` with per-client `seqGen.Next()`. The result is wrapped in `OutgoingMsg{raw: envelopeData}`.
- **RP-002**: A test MUST verify that the manual byte concat output is byte-identical to `json.Marshal(MessageEnvelope{...})` for the same inputs (proving no wire format regression).
- **RP-003**: All existing test logic MUST be preserved. Test files are updated only to adapt to the `OutgoingMsg` channel type — no test logic changes.
- **RP-004**: The `MessageEnvelope` struct, `WrapMessage()`, `Serialize()`, `SequenceGenerator`, and all Priority constants in `messaging/message.go` MUST NOT be removed or modified.

## Success Criteria
- **SC-001**: All broadcast messages received by clients have `seq > 0` and are strictly monotonically increasing per connection.
- **SC-002**: Benchmark showing the broadcast loop per-client cost MUST NOT exceed 50ns (current: ~15ns channel send; after: ~25ns atomic + channel send — well within budget).
- **SC-003**: Existing Kafka replay messages continue to use the same `SequenceGenerator`, maintaining a single sequence space per client.
- **SC-004**: Benchmark showing `BroadcastEnvelope.Build()` is 300-600x faster than `json.Marshal(MessageEnvelope{...})`.

## Clarifications
- Q: When a broadcast message is dropped for a slow client, should a sequence number still be consumed? → A: Yes — consume seq on drop so the client sees a gap and knows it missed something (standard financial platform behavior).
- Q: Should sequence injection be configurable via env var? → A: No — always enabled, no toggle. The overhead of the chosen approach is small enough to not need a fallback.
- Q: How to inject per-client seq without per-client byte allocation in the broadcast loop? → A: Write-pump deferred assembly. The broadcast loop sends a lightweight `OutgoingMsg{envelope, seq}` struct. The write pump goroutine (per-client, parallelized) calls `envelope.Build(seq)` to assemble the final bytes just before writing to the WebSocket. The broadcast loop stays O(1) per client (~25ns, up from ~15ns — the ~10ns atomic increment is negligible at scale).
- Q: Why write-pump over byte-concat-in-broadcast-loop? → A: The broadcast loop runs in a single goroutine and processes messages sequentially. Keeping it at ~25ns/client (vs ~120ns with in-loop concat) preserves throughput at scale. The byte assembly is parallelized across write pump goroutines on multiple CPU cores — idiomatic Go concurrency.

## Out of Scope
- Server-side message replay by sequence range (clients use Kafka offset-based replay today).
- Cross-pod sequence coordination (sequences are per-connection, not global).
- Client SDK changes (clients already expect `seq` to be populated per the `MessageEnvelope` contract).
- Changing the message envelope schema or wire format.
- Per-channel broadcast worker pool (future optimization for 50K+ clients at 100+ msg/sec).
