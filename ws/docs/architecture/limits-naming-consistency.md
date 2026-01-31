# Limits Naming Consistency Plan

**Status:** Implemented
**Date:** 2026-01-31
**Scope:** Rename constants in `limits.go` for consistency and clarity

---

## Problem Statement

The constants in `ws/internal/shared/protocol/limits.go` have inconsistent naming:

| Current Name | Issue |
|--------------|-------|
| `MaxPublishMessageSize` | Sounds like a hard protocol limit |
| `DefaultPublishRateLimit` | Clearly a default ✅ |
| `DefaultPublishBurst` | Clearly a default ✅ |

All these values are actually **defaults** that can be overridden via configuration. The naming should reflect this.

---

## Solution

Rename `MaxPublishMessageSize` to `DefaultMaxPublishSize` for consistency with other constants.

---

## Files to Modify

| File | Change |
|------|--------|
| `ws/internal/shared/protocol/limits.go` | Rename constant, improve comments |
| `ws/internal/gateway/proxy.go` | Update reference |
| `ws/internal/server/handlers_publish.go` | Update reference |
| `ws/internal/server/handlers_publish_test.go` | Update reference |

---

## Planned Changes

### 1. `ws/internal/shared/protocol/limits.go`

**Before:**
```go
// Size limits for WebSocket messages.
const (
	// MaxPublishMessageSize is the maximum size for publish message payloads (64KB).
	MaxPublishMessageSize = 64 * 1024

	// DefaultSendBufferSize is the default size for client send buffers.
	DefaultSendBufferSize = 256
)
```

**After:**
```go
// Default size limits for WebSocket messages.
// These can be overridden via environment variables in each component's config.
const (
	// DefaultMaxPublishSize is the default maximum size for publish payloads (64KB).
	// Gateway: GATEWAY_MAX_PUBLISH_SIZE
	DefaultMaxPublishSize = 64 * 1024

	// DefaultSendBufferSize is the default size for client send buffers.
	DefaultSendBufferSize = 256
)
```

### 2. `ws/internal/gateway/proxy.go`

**Before:**
```go
maxPublishSize = protocol.MaxPublishMessageSize
```

**After:**
```go
maxPublishSize = protocol.DefaultMaxPublishSize
```

### 3. `ws/internal/server/handlers_publish.go`

**Before:**
```go
if len(pubReq.Data) > protocol.MaxPublishMessageSize {
```

**After:**
```go
if len(pubReq.Data) > protocol.DefaultMaxPublishSize {
```

### 4. `ws/internal/server/handlers_publish_test.go`

Update all test references from `protocol.MaxPublishMessageSize` to `protocol.DefaultMaxPublishSize`.

---

## Verification

```bash
# Build all packages
go build ./...

# Run tests
go test ./... -short

# Verify no old references remain
grep -r "MaxPublishMessageSize" internal/
```

---

## Impact

- **Breaking change:** None (internal packages only)
- **Lines changed:** ~10
- **Risk:** Low
