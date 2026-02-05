# Plan: Post-Review Cleanup — Explicit Channels Refactor

**Status:** Implemented
**Date:** 2026-02-05
**Scope:** Fix 4 issues found during the implementation review of the explicit channels refactor.

---

## Summary

After completing the explicit channels refactor (Tasks #10–#15), a review identified 4 actionable issues:

1. **Concurrent writes to `clientConn`** in the gateway proxy (medium severity)
2. **Missing channel format validation on subscribe** — publish checks `MinInternalChannelParts` but subscribe does not (medium severity)
3. **Dead `ChannelMapper` code** — ~500 lines of struct, methods, and tests from the implicit channel model (medium severity)
4. **Stale comments** in `message.go` showing old channel format (low severity)

---

## Task 1: Fix concurrent writes to `clientConn`

**Problem:** Both goroutines in the proxy can write to `clientConn` simultaneously — `proxyBackendToClient` forwards frames (`proxy.go:240`) while `proxyClientToBackend` sends publish errors via `sendPublishErrorToClient` (`proxy.go:481`). `forwardFrame` does a two-phase write (header + payload) that isn't atomic, risking frame corruption.

**File:** `ws/internal/gateway/proxy.go`

**Changes:**

1. Add `clientWriteMu sync.Mutex` field to `Proxy` struct (after line 28)
2. Add a `sendToClient(opCode ws.OpCode, payload []byte, fin bool)` method that locks `clientWriteMu` and calls `forwardFrame(p.clientConn, ...)` with `mask=false`
3. Replace all direct `forwardFrame(p.clientConn, ...)` calls with `sendToClient(...)`:
   - `proxyBackendToClient` line 240: normal frame forwarding
   - `proxyBackendToClient` line 231: close frame forwarding (add `sendCloseToClient` or handle inline)
   - `sendPublishErrorToClient` line 481: error response
4. Add `"sync"` import

**Note:** `forwardFrame(p.backendConn, ...)` is only called from `proxyClientToBackend`, so no mutex needed for backend writes.

---

## Task 2: Add `MinInternalChannelParts` check to `interceptSubscribe`

**Problem:** `interceptPublish` validates channels have >= 3 dot-separated parts (`proxy.go:437`), but `interceptSubscribe` does not. Malformed channels like `"odin."` or `"odin.x"` pass tenant validation and reach the backend.

**File:** `ws/internal/gateway/proxy.go`

**Changes:**

In `interceptSubscribe` (lines 329–340), inside the tenant validation loop, add a channel format check after `validateChannelTenant`:

```go
for _, ch := range subData.Channels {
    if p.validateChannelTenant(ch) && len(strings.Split(ch, ".")) >= protocol.MinInternalChannelParts {
        channels = append(channels, ch)
    }
}
```

No new imports needed (`strings` and `protocol` already imported).

---

## Task 3: Remove dead `ChannelMapper` code

**Problem:** `ChannelMapper` struct, its methods, and related wrappers on `TenantIsolator` are dead code from the implicit channel model. Only `ExtractTenant` is still called in production (from `CheckChannelAccess`).

### 3a. `ws/internal/shared/auth/channel.go` — Remove ChannelMapper

**Remove:**

- `ChannelConfig` struct and `DefaultChannelConfig()` function
- `ChannelMapper` struct, `NewChannelMapper()` constructor
- All `ChannelMapper` methods: `MapToInternal`, `MapToInternalWithTenant`, `MapToClient`, `MapToClientWithTenant`, `ExtractTenant`, `ParseChannel`, `BuildInternalChannel`
- `ChannelParts` struct

**Keep:**

- `ValidateInternalChannel()` (line 217)
- `IsValidInternalChannel()` (line 239)
- `ParseInternalChannel()` (line 250)
- `IsSharedChannel()` (line 205)
- `MatchWildcard()` / `MatchAnyWildcard()`
- Any imports required by the kept functions

**Add:**

- `extractChannelTenant(channel, separator string) string` — standalone function replacing `ChannelMapper.ExtractTenant`. Implementation: find first separator, return prefix. No `TenantImplicit` config flag (always extract in explicit model).

### 3b. `ws/internal/shared/auth/tenant.go` — Remove ChannelMapper usage

**Remove:**

- `channelMapper *ChannelMapper` field (line 16)
- `WithChannelMapper` option function (lines 60–64)
- Auto-creation block in constructor (lines 93–96)
- `MapClientToInternal` method (lines 247–249)
- `MapInternalToClient` method (lines 252–254)
- `GetTenantFromChannel` method (lines 262–264)

**Change:**

- `CheckChannelAccess` line 184: Replace `t.channelMapper.ExtractTenant(channel)` with `extractChannelTenant(channel, ".")`

### 3c. `ws/internal/shared/auth/channel_test.go` — Remove stale tests

**Remove tests** (lines 7–476):

- `TestNewChannelMapper`
- `TestChannelMapper_MapToInternal`
- `TestChannelMapper_MapToInternal_TenantExplicit`
- `TestChannelMapper_MapToInternalWithTenant`
- `TestChannelMapper_MapToInternalWithTenant_TenantExplicit`
- `TestChannelMapper_MapToClient`
- `TestChannelMapper_MapToClientWithTenant`
- `TestChannelMapper_ExtractTenant`
- `TestChannelMapper_ParseChannel`
- `TestChannelMapper_BuildInternalChannel`
- `TestDefaultChannelConfig`
- `TestChannelMapper_RoundTrip`

**Keep tests:**

- `TestIsSharedChannel`
- `TestMatchWildcard_SharedChannel`
- `TestValidateInternalChannel`
- `TestIsValidInternalChannel`
- `TestParseInternalChannel`

**Add test:**

- `TestExtractChannelTenant` — test the new standalone function with cases: `"odin.BTC.trade"` → `"odin"`, `"BTC.trade"` → `"BTC"`, `""` → `""`, `"nodotshere"` → `""`

### 3d. `ws/internal/shared/auth/tenant_test.go` — Remove stale tests

**Remove:**

- "with custom channel mapper" subtest in `TestNewTenantIsolator` (lines 24–34)
- `iso.channelMapper` nil-check assertions (if any in "with default config" subtest)
- `TestTenantIsolator_MapClientToInternal` (lines 213–224)
- `TestTenantIsolator_MapInternalToClient` (lines 226–236)
- `TestTenantIsolator_GetTenantFromChannel` (lines 250–260)

---

## Task 4: Fix stale channel comments in `message.go`

**Problem:** Channel field comments in `MessageEnvelope` show old implicit format without tenant prefix.

**File:** `ws/internal/server/messaging/message.go` (lines 80–95)

**Change comment examples:**

```
"BTC.trade"                → "odin.BTC.trade"
"all.trade"                → "odin.all.trade"
"ETH.liquidity"            → "odin.ETH.liquidity"
"BTC.balances.user123"     → "odin.BTC.balances.user123"
case "BTC.trade":          → case "odin.BTC.trade":
case "all.trade":          → case "odin.all.trade":
```

---

## Implementation Order

1. Task 1 — concurrent writes fix (isolated to proxy.go)
2. Task 2 — subscribe validation (isolated to proxy.go, can be done with Task 1)
3. Task 3 — ChannelMapper removal (auth package, 4 files)
4. Task 4 — comment fix (1 line change)

---

## Verification

1. `cd ws && go build ./...` — clean compilation
2. `cd ws && go test ./...` — all tests pass
3. `cd ws && go vet ./...` — no vet issues
4. Grep for `ChannelMapper` — should only appear in kept standalone function references (if any), not as a struct or method
5. Grep for `MapToClient`, `MapToInternal`, `MapToInternalWithTenant` — zero results expected
