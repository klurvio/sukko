# Research: Fix Broadcast Message Sequence Numbers

**Branch**: `fix/broadcast-seq-zero` | **Date**: 2026-02-25

## R1: Current Wire Format (json.Marshal output)

`json.Marshal(MessageEnvelope{...})` produces fields in struct declaration order:

```json
{"type":"message","seq":0,"ts":1708903200000,"channel":"BTC.trade","data":{"token":"BTC","price":45000}}
```

Field order: `type` → `seq` → `ts` → `channel` → `data`
- `Priority` is excluded (`json:"-"`)
- `channel` value is JSON-escaped string (quotes included)
- `data` is raw JSON (no re-encoding via `json.RawMessage`)

## R2: Channel Value Safety

Channels come from `extractChannel(subject)` which validates 2+ dot-separated non-empty parts.
Typical values: `BTC.trade`, `ETH.liquidity`, `BTC.trade.user123`

These don't contain JSON-special characters (`"`, `\`, control chars), but for FR-004 compliance (byte-identical to `json.Marshal`), we must use `json.Marshal(channel)` once per broadcast to safely produce the quoted+escaped channel string.

## R3: Per-Client Cost Analysis

Current (shared serialization, seq=0):
- 1x `json.Marshal()` (~300µs) per broadcast
- 0 per-client cost (shared `[]byte` via channel)

Proposed (byte concat, per-client seq):
- 0x `json.Marshal()` for envelope (eliminated entirely)
- 1x `json.Marshal(channel)` (~50ns, just a string) per broadcast
- Per-client: `seqGen.Next()` (~10ns) + `make([]byte)` (~20ns) + 3x `append()` (~50ns) = ~80-100ns
- At 10K clients: ~1ms total per broadcast event
- At 25 events/sec: ~25ms/sec CPU = 2.5% (vs ~0.75% before)
- Overhead increase: ~1.75% absolute CPU — well within 20% relative budget

## R4: Replay Path (DO NOT TOUCH)

`handlers_message.go:255-278` — Uses `MessageEnvelope{}` + `c.seqGen.Next()` + `Serialize()`.
This path already produces correct per-client sequences. It must remain unchanged.
After the fix, both broadcast and replay share the same `seqGen` per client → single sequence space (FR-003).

## R5: gobwas/ws Write Requirements

`wsutil.WriteServerMessage(writer, ws.OpText, message)` requires complete `[]byte`.
Cannot split-write at WebSocket frame level. Per-client bytes must be fully assembled before sending to the `send` channel.
