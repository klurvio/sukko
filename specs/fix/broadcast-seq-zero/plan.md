# Implementation Plan: Fix Broadcast Message Sequence Numbers

**Branch**: `fix/broadcast-seq-zero` | **Date**: 2026-02-25 | **Spec**: specs/fix/broadcast-seq-zero/spec.md

## Summary

Replace the hardcoded `seq: 0` broadcast envelope with per-client sequence numbers using write-pump deferred assembly. The broadcast loop sends a lightweight struct (`OutgoingMsg`) containing a shared `BroadcastEnvelope` + per-client seq. Each write pump goroutine assembles the final bytes via `Build(seq)` before writing to the WebSocket. The broadcast loop stays O(1) per client (~25ns, up from ~15ns — the ~10ns atomic increment is negligible at scale).

## Technical Context

**Language**: Go 1.22+
**Services**: ws-server (broadcast + write pump paths)
**Key packages**: `internal/server` (broadcast.go, connection.go, pump.go), `internal/server/messaging` (new helper)
**Dependencies**: `strconv` (new, used by messaging package), `encoding/json` (existing)

## Constitution Compliance

| Principle | Status | Notes |
|-----------|--------|-------|
| I. No Hardcoded Values | PASS | Removes hardcoded `Seq: 0`. JSON syntax fragments are structural constants, not configurable values. |
| II. Defense in Depth | PASS | Channel is JSON-escaped via `json.Marshal(channel)` to prevent injection. |
| III. Error Handling | PASS | `json.Marshal(channel)` error is handled with structured logging. |
| IV. Graceful Degradation | PASS | If channel escaping fails, broadcast is skipped with error log. |
| V. Structured Logging | PASS | All log statements use zerolog with structured fields. |
| VI. Observability | PASS | Existing broadcast metrics unchanged. Write pump metrics adapted to new type. |
| VII. Concurrency Safety | PASS | `seqGen.Next()` is atomic. `BroadcastEnvelope` is immutable (safe for concurrent read by write pumps). Per-client `Build()` produces owned `[]byte`. `sync.Pool` not used for `Build()` allocations — these are per-goroutine one-shot buffers immediately written to WebSocket and discarded; Pool's interface boxing and cross-goroutine coordination overhead exceeds benefit here. |
| VIII. Configuration Validation | N/A | No new configuration. |
| IX. Testing | PASS | Wire format equivalence test, per-client seq tests, benchmark, all existing tests adapted. |
| X. Security | PASS | Channel JSON-escaped to prevent injection. |
| XI. Shared Code Consolidation | PASS | New helper in `messaging` package (existing shared location). |

## Design

### Architecture: Write-Pump Deferred Assembly

```
BEFORE:
  Broadcast() → MessageEnvelope{Seq:0} → json.Marshal() → sharedData []byte
  Per client:  client.send <- sharedData       (chan []byte, same pointer for all)
  Write pump:  wsutil.WriteServerMessage(sharedData)

AFTER:
  Broadcast() → NewBroadcastEnvelope(channel, ts, payload) → shared *BroadcastEnvelope
  Per client:  seq := client.seqGen.Next()     (~10ns atomic)
               client.send <- OutgoingMsg{     (~15ns channel send)
                   envelope: envelope,          shared pointer (immutable)
                   seq: seq,                    per-client value
               }
  Write pump:  message := msg.Bytes()          Build(seq) → []byte (~100ns, parallelized)
               wsutil.WriteServerMessage(message)
```

### Why This Architecture

The broadcast listener (`shard.go:143-156`) runs a sequential loop in **one goroutine**:
```go
for {
    msg := <-broadcastChan
    s.server.Broadcast(msg.Subject, msg.Payload)  // blocks until done
}
```

The faster `Broadcast()` returns, the sooner the next Kafka message gets processed.
With deferred assembly, the broadcast loop cost is ~25ns/client (up from ~15ns with current shared `[]byte` — the ~10ns atomic increment is negligible).
The ~100ns byte assembly is parallelized across 10K independent write pump goroutines on multiple CPU cores.

### OutgoingMsg Type

```go
// OutgoingMsg is the message type for the client send channel.
// It supports two modes:
//   - Pre-built: raw bytes (acks, errors, replay) — write pump sends directly
//   - Deferred:  shared envelope + per-client seq — write pump builds bytes
type OutgoingMsg struct {
    raw      []byte                        // pre-built bytes (non-nil = use directly)
    envelope *messaging.BroadcastEnvelope  // shared broadcast template (immutable)
    seq      int64                         // per-client sequence number
}

// Bytes resolves the message to sendable bytes.
// For pre-built messages, returns raw directly (zero-cost).
// For broadcast messages, assembles bytes from envelope + seq (~100ns).
// Returns nil for zero-value OutgoingMsg (defensive guard against channel misuse).
func (m OutgoingMsg) Bytes() []byte {
    if m.raw != nil {
        return m.raw
    }
    if m.envelope == nil {
        return nil
    }
    return m.envelope.Build(m.seq)
}
```

### BroadcastEnvelope (Byte Layout)

Target output (byte-identical to json.Marshal):
```
{"type":"message","seq":1234,"ts":1708903200000,"channel":"BTC.trade","data":{"price":45000}}
|--- prefix ---||-- seq --||-------------- sharedSuffix (computed once) ----------------------|
```

Pre-computed fragments (once per broadcast):
- `prefix`       = `{"type":"message","seq":` (constant)
- `sharedSuffix` = `,"ts":` + tsBytes + `,"channel":` + channelJSON + `,"data":` + payload + `}`

Per-client `Build(seq)`:
- `buf := make([]byte, 0, len(prefix)+20+len(sharedSuffix))`
- `buf = append(prefix + strconv.AppendInt(seq) + sharedSuffix)`

### Sequence Assignment Timing

```go
// In broadcast subscriber loop — seq consumed BEFORE send attempt
seq := client.seqGen.Next()  // atomic, always consumed even if dropped
select {
case client.send <- OutgoingMsg{envelope: envelope, seq: seq}:
    // delivered to write pump
default:
    // dropped — seq consumed, client sees gap (e.g., 5 → 7)
}
```

## File Changes

### 1. NEW: `ws/internal/server/messaging/broadcast_envelope.go`

New file containing the broadcast-specific byte assembly helper. Separate from `message.go` to keep existing code untouched (RP-004).

```go
package messaging

// BroadcastEnvelope holds pre-computed shared byte fragments for efficient
// per-client message assembly. Immutable after creation — safe for concurrent
// reads by multiple write pump goroutines.
type BroadcastEnvelope struct {
    prefix       []byte // {"type":"message","seq":
    sharedSuffix []byte // ,"ts":...,"channel":"...","data":...}
    totalEstimate int   // pre-calculated capacity for Build()
}

// NewBroadcastEnvelope pre-computes shared byte fragments from broadcast parameters.
// The channel is JSON-escaped via json.Marshal to match json.Marshal(MessageEnvelope{}) output.
// A nil payload is normalized to the JSON literal `null` to produce valid JSON output.
// Called once per broadcast event — O(1), not per-client.
func NewBroadcastEnvelope(channel string, timestamp int64, payload []byte) (*BroadcastEnvelope, error)

// Build assembles the final message bytes for a specific client sequence number.
// Returns a new []byte owned by the caller (safe to send via WebSocket).
// Called per-client in write pump goroutines — O(1), ~100ns.
func (e *BroadcastEnvelope) Build(seq int64) []byte
```

### 2. NEW: `ws/internal/server/messaging/broadcast_envelope_test.go`

| Test | What it verifies | Spec requirement |
|------|-----------------|-----------------|
| `TestBroadcastEnvelope_MatchesJsonMarshal` | Byte-identical output to `json.Marshal(MessageEnvelope{...})` across multiple inputs | RP-002, FR-004 |
| `TestBroadcastEnvelope_FieldOrder` | Field order: `type`, `seq`, `ts`, `channel`, `data` | FR-004 |
| `TestBroadcastEnvelope_SequenceValues` | Different seq values produce different outputs | FR-001 |
| `TestBroadcastEnvelope_ChannelEscaping` | Special characters in channel are properly JSON-escaped | FR-004 |
| `TestBroadcastEnvelope_NilPayload` | `nil` payload normalized to `"data":null`, byte-identical to `json.Marshal` | Edge case, A01 |
| `TestBroadcastEnvelope_EmptyPayload` | `[]byte("null")` payload produces valid JSON | Edge case |
| `TestBroadcastEnvelope_LargePayload` | Large payload preserves byte integrity | Edge case |
| `TestBroadcastEnvelope_ValidJSON` | Output is always parseable by `json.Unmarshal` | FR-004 |
| `TestBroadcastEnvelope_Concurrent` | Multiple goroutines calling `Build()` on same envelope | NFR-003 |
| `BenchmarkBroadcastEnvelope_Build` | Per-call cost of `Build()` | NFR-001 |
| `BenchmarkBroadcastEnvelope_VsJsonMarshal` | Comparison vs `json.Marshal(MessageEnvelope{})` | SC-004 |

### 3. MODIFY: `ws/internal/server/connection.go`

**Add `OutgoingMsg` type** (near the `Client` struct):

```go
// OutgoingMsg is the message type for the client send channel.
type OutgoingMsg struct {
    raw      []byte
    envelope *messaging.BroadcastEnvelope
    seq      int64
}

func (m OutgoingMsg) Bytes() []byte { ... }

// RawMsg creates an OutgoingMsg from pre-built bytes.
func RawMsg(data []byte) OutgoingMsg {
    return OutgoingMsg{raw: data}
}
```

**Change send channel type**:
```go
// Before:
send chan []byte

// After:
send chan OutgoingMsg
```

Update `NewConnectionPool` and `ConnectionPool.Get`:
```go
// Before:
send: make(chan []byte, cp.bufferSize),

// After:
send: make(chan OutgoingMsg, cp.bufferSize),
```

Update `ConnectionPool.Get` drain:
```go
// Before:
case <-client.send:

// After:
case <-client.send:  // same syntax — channel drain works for any type
```

### 4. MODIFY: `ws/internal/server/broadcast.go`

**Lines 1-14 (imports)**: Remove `"encoding/json"` — it is no longer used in broadcast.go after removing `json.RawMessage(message)` from the envelope creation. The `json.Marshal(channel)` call moves to the `messaging` package. Verify with `go vet` after removal. Keep `messaging` import.

**Lines 99-134 (envelope creation + serialization)**: Replace with:
```go
// Pre-compute broadcast envelope template (shared across all write pump goroutines).
// Write-pump deferred assembly: broadcast loop stays O(1) per client (~25ns),
// byte assembly (~100ns) is parallelized across write pump goroutines.
//
// Performance vs old shared-serialization (seq=0):
//   - Broadcast loop: ~25ns/client (up from ~15ns — atomic increment is negligible)
//   - Byte assembly: ~100ns/client (deferred to write pumps, parallelized)
//   - Enables client-side gap detection (seq>0, monotonically increasing)
envelope, err := messaging.NewBroadcastEnvelope(channel, time.Now().UnixMilli(), message)
if err != nil {
    metrics.RecordSerializationError(pkgmetrics.SeverityCritical)
    s.logger.Error().
        Err(err).
        Str("channel", channel).
        Int("subscribers", totalCount).
        Msg("Failed to prepare broadcast envelope")
    return
}
```

**Lines 149-159 (client send)**: Replace:
```go
// Assign sequence BEFORE send attempt — consumed even if dropped.
// Client sees gap (e.g., 5 → 7) if message is dropped, enabling gap detection.
seq := client.seqGen.Next()

select {
case client.send <- OutgoingMsg{envelope: envelope, seq: seq}:
    // Success — lightweight struct sent to write pump for deferred assembly
    client.sendAttempts.Store(0)
    client.lastMessageSentAt = time.Now()
    successCount++

    s.logger.Debug().
        Int64("client_id", client.id).
        Str("channel", channel).
        Int64("seq", seq).
        Msg("Broadcast to client")
```

**Lines 167+ (default/drop case)**: No logic changes needed. The seq was already consumed above.

### 5. MODIFY: `ws/internal/server/pump.go`

**Line 273 (rate limit error)**:
```go
// Before:
case c.send <- errorMsg:

// After:
case c.send <- RawMsg(errorMsg):
```

**Line 337 (first message receive)**:
```go
// Before:
case message, ok := <-c.send:

// After:
case msg, ok := <-c.send:
```

**Lines 356-361 (first message write)**:
```go
// Before:
batchByteCount := int64(len(message))
err := wsutil.WriteServerMessage(writer, ws.OpText, message)

// After:
message := msg.Bytes()  // resolve: raw passthrough or envelope.Build(seq)
batchByteCount := int64(len(message))
err := wsutil.WriteServerMessage(writer, ws.OpText, message)
```

**Lines 374-395 (batch loop)**:
```go
// Before:
var batchMsg []byte
var batchOk bool
select {
case batchMsg, batchOk = <-c.send:

// After:
var batchOutgoing OutgoingMsg
var batchOk bool
select {
case batchOutgoing, batchOk = <-c.send:
```

```go
// Before:
err := wsutil.WriteServerMessage(writer, ws.OpText, batchMsg)
batchByteCount += int64(len(batchMsg))

// After:
batchMsg := batchOutgoing.Bytes()
err := wsutil.WriteServerMessage(writer, ws.OpText, batchMsg)
batchByteCount += int64(len(batchMsg))
```

### 6. MODIFY: `ws/internal/server/handlers_message.go` (mechanical wraps)

All 6 send sites — wrap `data` in `RawMsg(data)`:

```go
// Line 57 (heartbeat pong):
case c.send <- RawMsg(data):

// Line 121 (subscribe ack):
case c.send <- RawMsg(data):

// Line 163 (unsubscribe ack):
case c.send <- RawMsg(data):

// Line 269 (replay envelope):
case c.send <- RawMsg(envelopeData):

// Line 290 (reconnect ack):
case c.send <- RawMsg(ackData):

// Line 316 (error response):
case c.send <- RawMsg(data):
```

### 7. MODIFY: `ws/internal/server/handlers_publish.go` (mechanical wrap)

```go
// Line 135 (publish ack):
case c.send <- RawMsg(data):
```

### 8. MODIFY: Test files (mechanical wraps)

All test sends updated from `client.send <- []byte(...)` to `client.send <- RawMsg([]byte(...))`:

| File | Lines | Change |
|------|-------|--------|
| `pump_test.go` | 356, 1404 | Wrap in `RawMsg()` |
| `handlers_message_test.go` | 374, 375, 381, 399, 400, 401 | Wrap in `RawMsg()` |
| `client_lifecycle_test.go` | 556 | Wrap in `RawMsg()` |
| `metrics_test.go` | 515 | Wrap in `RawMsg()` |

Channel type in test client construction also changes from `make(chan []byte, N)` to `make(chan OutgoingMsg, N)`.
Read sites from `client.send` (e.g., `msg := <-client.send`) also need `.Bytes()` or `.raw` extraction for byte comparisons.

### 9. NEW tests in `ws/internal/server/broadcast_test.go`

| Test | What it verifies |
|------|-----------------|
| `TestBroadcast_PerClientSequence` | Two mock clients receive different seq values for same broadcast |
| `TestBroadcast_SequenceMonotonicallyIncreasing` | Multiple broadcasts produce incrementing seq per client |
| `TestBroadcast_SequenceConsumedOnDrop` | When send channel is full, seq is still consumed (gap visible) |
| `TestBroadcast_CrossChannelSequenceContinuity` | Messages on different channels share one seq space per client |
| `TestBroadcast_OutgoingMsgBytes` | `Bytes()` dispatches correctly for raw vs envelope vs zero-value modes |

### 10. NOT MODIFIED (RP verification)

| File | Status | Verification |
|------|--------|-------------|
| `messaging/message.go` | Untouched | No diff expected (RP-004) |
| `messaging/message_test.go` | Untouched | All tests pass as-is (RP-003) |

## Verification Steps

### 1. Unit Tests
```bash
cd ws && go test ./internal/server/messaging/ -v -run BroadcastEnvelope
cd ws && go test ./internal/server/ -v -run TestBroadcast
```

### 2. Regression Tests (all existing tests must pass)
```bash
cd ws && go test ./...
```

### 3. Benchmarks
```bash
cd ws && go test ./internal/server/messaging/ -bench BroadcastEnvelope -benchmem
```

### 4. Static Analysis
```bash
cd ws && go vet ./...
```

### 5. Deploy & Log Verification
```bash
task k8s:build:push:ws-server ENV=dev
task k8s:deploy ENV=dev
kubectl logs -n odin-ws-dev -l app.kubernetes.io/name=ws-server --tail=50
```

Verify in client WebSocket messages: `"seq"` field is > 0 and incrementing.

## Resource Impact Assessment

| Resource | Before | After | Delta |
|----------|--------|-------|-------|
| Broadcast loop (per client) | ~15ns (channel send) | ~25ns (atomic + channel send) | +10ns (negligible at scale) |
| Write pump (per message) | ~0 (send raw bytes) | ~100ns (Build, for broadcast msgs only) | +100ns (parallelized) |
| Memory (per broadcast) | 1 shared `[]byte` | 1 `BroadcastEnvelope` + 1 `[]byte` per client in write pump | +5MB per event at 10K clients |
| Channel buffer memory | 24 bytes × bufferSize × clients | 40 bytes × bufferSize × clients | +16 bytes/slot (+67%); at 10K clients × 512 buffer = ~80MB delta |
| GC pressure | Minimal | Moderate (250K allocs/sec, distributed across goroutines) | Acceptable |
| Channel struct size | 24 bytes (`[]byte` header) | 40 bytes (`OutgoingMsg`) | +16 bytes per channel slot |

## Next Step

`/implement` to execute the task list.
