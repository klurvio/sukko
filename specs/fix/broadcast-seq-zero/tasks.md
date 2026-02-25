# Tasks: Fix Broadcast Message Sequence Numbers

**Branch**: `fix/broadcast-seq-zero` | **Generated**: 2026-02-25

## Phase 1: Core Types (foundation — everything depends on this)

- [x] T001 — Create `BroadcastEnvelope` type in `ws/internal/server/messaging/broadcast_envelope.go`
  - New file. Implement `BroadcastEnvelope` struct with `prefix`, `sharedSuffix`, `totalEstimate` fields.
  - Implement `NewBroadcastEnvelope(channel string, timestamp int64, payload []byte) (*BroadcastEnvelope, error)`:
    - Normalize nil payload to `[]byte("null")` at the top of the function (produces valid JSON `"data":null`)
    - `prefix` = `{"type":"message","seq":` (constant)
    - `sharedSuffix` = `,"ts":` + `strconv.AppendInt(ts)` + `,"channel":` + `json.Marshal(channel)` + `,"data":` + payload + `}`
    - `totalEstimate` = `len(prefix) + 20 + len(sharedSuffix)` (20 = max int64 digits)
  - Implement `Build(seq int64) []byte`:
    - `buf := make([]byte, 0, e.totalEstimate)`
    - `buf = append(buf, e.prefix...)`
    - `buf = strconv.AppendInt(buf, seq, 10)`
    - `buf = append(buf, e.sharedSuffix...)`
    - Return `buf`
  - Imports: `encoding/json`, `strconv`
  - **RP-004**: Do NOT modify `message.go`. This is a new file only.

- [x] T002 — Add `OutgoingMsg` type and `RawMsg` helper in `ws/internal/server/connection.go`
  - Add `OutgoingMsg` struct near the `Client` struct definition (before or after it):
    - `raw []byte` — pre-built bytes (non-nil = use directly)
    - `envelope *messaging.BroadcastEnvelope` — shared broadcast template
    - `seq int64` — per-client sequence number
  - Add `Bytes()` method: if `raw != nil` return `raw`; if `envelope == nil` return `nil` (defensive guard for zero-value); else return `envelope.Build(seq)`
  - Add `RawMsg(data []byte) OutgoingMsg` constructor that returns `OutgoingMsg{raw: data}`
  - Change `send` field type from `chan []byte` to `chan OutgoingMsg` (line 55)
  - Update `NewConnectionPool` pool factory: `make(chan []byte, ...)` → `make(chan OutgoingMsg, ...)` (line 137)
  - Update comments referencing `chan []byte` to `chan OutgoingMsg` (including buffer sizing comments on the `send` field and in `NewConnectionPool`)
  - Channel drain in `ConnectionPool.Get` (line 153-157) works as-is (same syntax for any channel type)

## Phase 2: Broadcast Path (depends on T001, T002)

- [x] T003 — Refactor `Broadcast()` in `ws/internal/server/broadcast.go` to use write-pump deferred assembly
  - **Lines 1-14 (imports)**: Remove `"encoding/json"` — no longer used after removing `json.RawMessage(message)` from envelope creation (the `json.Marshal(channel)` call moves to the `messaging` package). Verify removal with `go vet`. Keep `messaging` import.
  - **Lines 99-134**: Replace `baseEnvelope` creation + `Serialize()` with:
    - `envelope, err := messaging.NewBroadcastEnvelope(channel, time.Now().UnixMilli(), message)`
    - Handle error with existing `metrics.RecordSerializationError` + structured logging pattern
    - Update comments to explain write-pump deferred assembly approach
  - **Lines 149-166 (success case in subscriber loop)**: Replace `client.send <- sharedData` with:
    - `seq := client.seqGen.Next()` before the `select` (consumed even if dropped)
    - `client.send <- OutgoingMsg{envelope: envelope, seq: seq}`
    - Add `Int64("seq", seq)` to the debug log
  - **Lines 167+ (default/drop case)**: No logic changes — seq already consumed above
  - Confirm `"encoding/json"` was removed in the imports step above (no remaining usages in broadcast.go after this refactor).

## Phase 3: Write Pump (depends on T002)

- [x] T004 — Update `WriteLoop` in `ws/internal/server/pump.go` to resolve `OutgoingMsg`
  - **Line 273**: `c.send <- errorMsg` → `c.send <- RawMsg(errorMsg)`
  - **Line 337**: `case message, ok := <-c.send:` → `case msg, ok := <-c.send:`
  - **Lines 356-361**: Add `message := msg.Bytes()` before `batchByteCount` and `WriteServerMessage` calls
  - **Lines 374-396 (batch loop)**:
    - `var batchMsg []byte` → `var batchOutgoing OutgoingMsg`
    - `case batchMsg, batchOk = <-c.send:` → `case batchOutgoing, batchOk = <-c.send:`
    - Add `batchMsg := batchOutgoing.Bytes()` before `WriteServerMessage` call
    - `batchByteCount += int64(len(batchMsg))` stays as-is (uses resolved bytes)

## Phase 4: Sender Wraps (depends on T002, can be parallel)

- [x] T005 [P] — Wrap sends in `ws/internal/server/handlers_message.go` with `RawMsg()`
  - Line 57: `c.send <- data` → `c.send <- RawMsg(data)` (heartbeat pong)
  - Line 121: `c.send <- data` → `c.send <- RawMsg(data)` (subscribe ack)
  - Line 163: `c.send <- data` → `c.send <- RawMsg(data)` (unsubscribe ack)
  - Line 269: `c.send <- envelopeData` → `c.send <- RawMsg(envelopeData)` (replay)
  - Line 290: `c.send <- ackData` → `c.send <- RawMsg(ackData)` (reconnect ack)
  - Line 316: `c.send <- data` → `c.send <- RawMsg(data)` (error response)
  - **RP-001**: Do NOT modify any logic in `handleKafkaReconnect`. Only wrap the channel send.

- [x] T006 [P] — Wrap send in `ws/internal/server/handlers_publish.go` with `RawMsg()`
  - Line 135: `c.send <- data` → `c.send <- RawMsg(data)` (publish ack)

## Phase 5: Test Updates (depends on T002, can be parallel with Phase 4)

- [x] T007 [P] — Update `ws/internal/server/pump_test.go` for `OutgoingMsg` channel type
  - Search for all `make(chan []byte` and `chan []byte` references in this file — update each to `make(chan OutgoingMsg` / `chan OutgoingMsg`
  - Line 356: `client.send <- []byte("existing message")` → `client.send <- RawMsg([]byte("existing message"))`
  - Line 1404: `client.send <- dataMsg` → `client.send <- RawMsg(dataMsg)`
  - Update all **read sites** from `client.send` (e.g., `msg := <-client.send`) to extract bytes via `msg.Bytes()` or `msg.raw` for assertions
  - **RP-003**: No test logic changes — only type adaptation (send wraps + read-site extraction).

- [x] T008 [P] — Update `ws/internal/server/handlers_message_test.go` for `OutgoingMsg` channel type
  - Search for all `make(chan []byte` and `chan []byte` references in this file — update each to `make(chan OutgoingMsg` / `chan OutgoingMsg`
  - Lines 374-375: `client.send <- []byte("msg1")` → `client.send <- RawMsg([]byte("msg1"))` (and msg2)
  - Line 381: `case client.send <- data:` → `case client.send <- RawMsg(data):`
  - Lines 399-401: Same wrapping for msg1, msg2, msg3
  - Update all **read sites** from `client.send` (e.g., `msg := <-client.send`) to extract bytes via `msg.Bytes()` or `msg.raw` for assertions
  - **RP-003**: No test logic changes — only type adaptation (send wraps + read-site extraction).

- [x] T009 [P] — Update `ws/internal/server/client_lifecycle_test.go` for `OutgoingMsg` channel type
  - Search for all `make(chan []byte` and `chan []byte` references in this file — update each to `make(chan OutgoingMsg` / `chan OutgoingMsg`
  - Line 556: `client.send <- []byte("msg")` → `client.send <- RawMsg([]byte("msg"))`
  - Update all **read sites** from `client.send` (e.g., `msg := <-client.send`) to extract bytes via `msg.Bytes()` or `msg.raw` for assertions
  - **RP-003**: No test logic changes — only type adaptation (send wraps + read-site extraction).

- [x] T010 [P] — Update `ws/internal/server/metrics_test.go` for `OutgoingMsg` channel type
  - Search for all `make(chan []byte` and `chan []byte` references in this file — update each to `make(chan OutgoingMsg` / `chan OutgoingMsg`
  - Line 515: `client.send <- []byte("msg")` → `client.send <- RawMsg([]byte("msg"))`
  - Update all **read sites** from `client.send` (e.g., `msg := <-client.send`) to extract bytes via `msg.Bytes()` or `msg.raw` for assertions
  - **RP-003**: No test logic changes — only type adaptation (send wraps + read-site extraction).

## Phase 6: New Tests (depends on all code phases)

- [x] T011 — Create `ws/internal/server/messaging/broadcast_envelope_test.go`
  - New file. Comprehensive tests for `BroadcastEnvelope`:
  - `TestBroadcastEnvelope_MatchesJsonMarshal`: For multiple inputs (different channels, payloads, seq values, timestamps), verify `Build(seq)` output is byte-identical to `json.Marshal(MessageEnvelope{Type:"message", Seq:seq, Timestamp:ts, Channel:ch, Data:payload})`. Table-driven.
  - `TestBroadcastEnvelope_FieldOrder`: Verify JSON field order is `type`, `seq`, `ts`, `channel`, `data` by checking byte positions.
  - `TestBroadcastEnvelope_SequenceValues`: Build with seq 1, 2, 3 — verify each output has correct seq and they differ.
  - `TestBroadcastEnvelope_ChannelEscaping`: Test channel with special chars (e.g., `"test\"channel"`, `back\\slash`) — verify proper JSON escaping.
  - `TestBroadcastEnvelope_NilPayload`: Build with `nil` payload — verify output contains `"data":null` (valid JSON). Verify byte-identical to `json.Marshal(MessageEnvelope{..., Data: nil})`.
  - `TestBroadcastEnvelope_EmptyPayload`: Build with `[]byte("null")` payload — verify valid JSON output.
  - `TestBroadcastEnvelope_LargePayload`: Build with 100KB payload — verify integrity.
  - `TestBroadcastEnvelope_ValidJSON`: Unmarshal output into `map[string]any` and verify all fields present.
  - `TestBroadcastEnvelope_Concurrent`: 100 goroutines calling `Build()` on same envelope — no races (run with `-race`).
  - `BenchmarkBroadcastEnvelope_Build`: Measure per-call cost of `Build()`.
  - `BenchmarkBroadcastEnvelope_VsJsonMarshal`: Compare `Build()` vs `json.Marshal(MessageEnvelope{})`.

- [x] T012 — Add broadcast sequence tests in `ws/internal/server/broadcast_test.go`
  - Add below existing `extractChannel` tests (do NOT modify existing tests):
  - `TestBroadcast_OutgoingMsgBytes`: Test `Bytes()` dispatch — raw mode returns raw, envelope mode calls `Build()`, zero-value `OutgoingMsg{}` returns nil (no panic).
  - `TestBroadcast_PerClientSequence`: Two mock clients with `seqGen` subscribed to same channel. After broadcast, verify each received different seq.
  - `TestBroadcast_SequenceMonotonicallyIncreasing`: Multiple broadcasts to one client. Extract seq from each received message, verify strictly increasing.
  - `TestBroadcast_SequenceConsumedOnDrop`: Client with full send buffer. After broadcast, verify `seqGen.Current()` incremented (seq consumed even though message dropped).
  - `TestBroadcast_CrossChannelSequenceContinuity`: Broadcast on channel A then channel B to same client. Verify seq is continuous across channels.

## Phase 7: Verification (depends on all previous phases)

- [x] T013 — Run full test suite and static analysis
  - `cd ws && go test ./... -race` — all tests pass, no race conditions
  - `cd ws && go vet ./...` — no static analysis errors
  - `cd ws && go test ./internal/server/messaging/ -bench BroadcastEnvelope -benchmem` — verify Build() performance
  - Verify `messaging/message.go` has no diff (RP-004)
  - Verify `messaging/message_test.go` has no diff (RP-003)

## Dependency Graph

```
T001 (BroadcastEnvelope) ─┐
                          ├──► T003 (broadcast.go)
T002 (OutgoingMsg)  ──────┤
                          ├──► T004 (pump.go)
                          ├──► T005 [P] (handlers_message.go)
                          ├──► T006 [P] (handlers_publish.go)
                          ├──► T007 [P] (pump_test.go)
                          ├──► T008 [P] (handlers_message_test.go)
                          ├──► T009 [P] (client_lifecycle_test.go)
                          └──► T010 [P] (metrics_test.go)

T001 ──────────────────────► T011 (broadcast_envelope_test.go)

T003 + T004 + T005-T010 ──► T012 (broadcast sequence tests)

All ───────────────────────► T013 (verification)
```

## Summary

| Phase | Tasks | Parallel | Description |
|-------|-------|----------|-------------|
| 1. Core Types | T001, T002 | No (sequential) | Foundation types |
| 2. Broadcast Path | T003 | No (depends on T001+T002) | Main fix |
| 3. Write Pump | T004 | No (depends on T002) | Deferred assembly |
| 4. Sender Wraps | T005, T006 | Yes (independent files) | Mechanical RawMsg() |
| 5. Test Updates | T007-T010 | Yes (independent files) | Mechanical type adaption |
| 6. New Tests | T011, T012 | Partial (T011 independent of T012) | Comprehensive coverage |
| 7. Verification | T013 | No | Full test + benchmark |

**Total**: 13 tasks
**Parallel opportunities**: T005+T006 (sender wraps), T007-T010 (test updates), T011 partial
**Start with**: T001 (BroadcastEnvelope — no dependencies, enables everything else)
**Next command**: `/implement`
