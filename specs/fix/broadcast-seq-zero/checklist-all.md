# Requirements Quality Checklist: Fix Broadcast Message Sequence Numbers

**Branch**: `fix/broadcast-seq-zero` | **Generated**: 2026-02-25
**Domains**: Concurrency, Performance, Wire Format Correctness

---

## Concurrency

### Completeness
- [ ] CHK001 Are thread-safety requirements specified for every shared data structure introduced (BroadcastEnvelope, OutgoingMsg)?
- [ ] CHK002 Are goroutine ownership semantics documented for the BroadcastEnvelope lifecycle (who creates, who reads, when it becomes garbage)?
- [ ] CHK003 Are requirements defined for what happens when multiple shards broadcast concurrently to the same client's seqGen?
- [ ] CHK004 Are requirements specified for the ordering guarantee (or lack thereof) when concurrent broadcasts from different shards assign sequence numbers to the same client?

### Clarity
- [ ] CHK005 Is "immutable after creation" for BroadcastEnvelope specified with enough precision (no mutation of prefix/sharedSuffix slices, no append that could trigger reallocation)?
- [ ] CHK006 Is the atomic operation type for seqGen.Next() specified (Add vs CompareAndSwap) and is its memory ordering documented?
- [ ] CHK007 Is "per-client sequence number" unambiguous — does it mean per-connection (reset on reconnect) or per-logical-client (persistent)?

### Measurability
- [ ] CHK008 Can the thread-safety requirement (NFR-003) be objectively verified via `-race` flag testing?
- [ ] CHK009 Are the concurrent test parameters specified (number of goroutines, iterations) for the BroadcastEnvelope.Concurrent test?

### Coverage
- [ ] CHK010 Are requirements defined for the scenario where a write pump goroutine calls Build() after the broadcast loop has moved on to the next message (stale envelope reference)?
- [ ] CHK011 Are requirements specified for channel close behavior — what happens to in-flight OutgoingMsg structs when client.send is closed?
- [ ] CHK012 Are requirements defined for the interaction between seqGen.Next() in broadcast and seqGen.Next() in the replay path when both execute concurrently?

---

## Performance

### Completeness
- [ ] CHK013 Are per-client timing budgets specified for both the broadcast loop (~25ns) and the write pump Build() (~100ns)?
- [ ] CHK014 Are memory allocation requirements specified for Build() (single allocation per call, pre-calculated capacity)?
- [ ] CHK015 Are GC pressure requirements defined with specific thresholds (allocs/sec, acceptable pause impact)?
- [ ] CHK016 Are requirements specified for the total memory impact of the channel type change (OutgoingMsg vs []byte per buffer slot)?

### Clarity
- [ ] CHK017 Is "300-600x faster than json.Marshal" (SC-004) quantified with specific ns/op targets rather than only relative comparison?
- [ ] CHK018 Is the 50ns broadcast loop budget (SC-002) defined as a p50, p99, or worst-case measurement?
- [ ] CHK019 Is "negligible at scale" for the ~10ns atomic increment quantified with a specific throughput impact at the target scale (10K clients, 25 msg/sec)?

### Consistency
- [ ] CHK020 Are the timing numbers consistent between spec (NFR-002: ~25ns), plan (resource impact: ~25ns), and tasks (no specific timing targets in task descriptions)?
- [ ] CHK021 Are the memory impact numbers consistent between spec (no specific number) and plan (+80MB delta at 10K clients × 512 buffer)?

### Measurability
- [ ] CHK022 Can the broadcast loop timing budget (SC-002) be measured with a specific benchmark methodology (BenchmarkBroadcast per-client iteration)?
- [ ] CHK023 Are benchmark comparison baselines defined (before vs after on same hardware, or absolute ns/op targets)?
- [ ] CHK024 Is the 250K allocs/sec GC pressure estimate derived from specific assumptions (10K clients × 25 msg/sec) and can it be verified?

### Coverage
- [ ] CHK025 Are performance requirements defined for the batch loop in the write pump (multiple Build() calls in sequence)?
- [ ] CHK026 Are requirements specified for the memory overhead when broadcast messages are queued but not yet consumed by the write pump (envelope retained in channel)?
- [ ] CHK027 Are degradation thresholds defined — at what client count or message rate does the approach become unacceptable?

---

## Wire Format Correctness

### Completeness
- [ ] CHK028 Are wire format requirements specified for all JSON field types (string escaping for channel, integer formatting for seq/ts, raw passthrough for data)?
- [ ] CHK029 Are requirements defined for the exact byte representation of edge-case values (seq=0, seq=max_int64, negative seq, ts=0)?
- [ ] CHK030 Are requirements specified for payload types beyond JSON objects (null, arrays, strings, numbers, booleans)?

### Clarity
- [ ] CHK031 Is "byte-identical to json.Marshal(MessageEnvelope{...})" (FR-004) specific about which Go version's json.Marshal behavior is the reference?
- [ ] CHK032 Is the field order requirement ("type, seq, ts, channel, data") specified as a MUST or derived from Go struct tag ordering — and is the dependency on struct field ordering documented?
- [ ] CHK033 Is the channel JSON escaping requirement specific about which characters need escaping (quotes, backslashes, control characters, Unicode)?

### Consistency
- [ ] CHK034 Are the wire format examples in spec (FR-004) consistent with the byte layout diagram in the plan (BroadcastEnvelope section)?
- [ ] CHK035 Is the nil payload behavior ("data":null) consistent between the spec (edge case), plan (NewBroadcastEnvelope normalization), and test (T011 NilPayload)?

### Measurability
- [ ] CHK036 Can wire format correctness be objectively verified by byte comparison against json.Marshal output for a defined set of inputs?
- [ ] CHK037 Are the test inputs for MatchesJsonMarshal specified with enough variety (different channel names, payload sizes, seq ranges, timestamp values)?

### Coverage
- [ ] CHK038 Are requirements defined for channel names containing JSON-special characters that could break the manual byte assembly (embedded quotes, backslashes, null bytes)?
- [ ] CHK039 Are requirements defined for extremely long channel names that could exceed typical buffer sizes?
- [ ] CHK040 Are requirements specified for payloads that contain the sequence of bytes `,"ts":` which could theoretically confuse naive parsing of the sharedSuffix?

---

## Cross-Domain

### Dependencies
- [ ] CHK041 Are requirements documented for the dependency between BroadcastEnvelope (messaging package) and OutgoingMsg (server package) — is the import direction specified?
- [ ] CHK042 Is the dependency on gobwas/ws WriteServerMessage requiring complete []byte documented as a constraint in the spec?

### Regression
- [ ] CHK043 Are regression protection requirements (RP-001 through RP-004) each mapped to at least one verifiable test?
- [ ] CHK044 Is the "no diff" verification for messaging/message.go (RP-004) specified as an automated check rather than manual inspection?

### Observability
- [ ] CHK045 Are requirements defined for how the new seq field appears in structured logs (debug log in broadcast loop)?
- [ ] CHK046 Are requirements specified for metrics impact — does the change affect any existing Prometheus metric values or labels?

---

## Summary

| Domain | Items | Focus |
|--------|-------|-------|
| Concurrency | CHK001-CHK012 | Thread safety, atomic semantics, concurrent access patterns |
| Performance | CHK013-CHK027 | Timing budgets, memory impact, GC pressure, benchmarks |
| Wire Format | CHK028-CHK040 | Byte identity, JSON correctness, edge-case payloads |
| Cross-Domain | CHK041-CHK046 | Dependencies, regression, observability |

**Total items**: 46
**Focus areas**: Concurrency ordering guarantees (CHK004), benchmark methodology (CHK018, CHK022-CHK023), wire format edge cases (CHK029, CHK038-CHK040)
