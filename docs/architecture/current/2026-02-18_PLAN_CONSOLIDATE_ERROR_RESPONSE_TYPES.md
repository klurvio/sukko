# Plan: Consolidate Error Response Types

## Context

The AsyncAPI spec (`ws/docs/asyncapi/client-ws.asyncapi.yaml`) defines two overlapping error schemas:

- **`Error`** (line 633): `{type: "error", message: "..."}` — no `code` field
- **`MessageError`** (line 647): `{type: "error", code: "invalid_json", message: "..."}` — has `code` field

Both use `type: "error"` on the wire. The generic `Error` schema is never actually sent by any code path — the only place that sends `type: "error"` is the invalid JSON handler, which always includes a `code` field (matching `MessageError`). The generic `Error` is dead spec.

Additionally, `reconnect_error` is the only error type that lacks a `code` field, which is inconsistent.

Finally, error code constants are split: `PublishErrorCode` (typed) for publish errors, raw string
literals for everything else. Values like `"invalid_request"` and `"not_available"` are used by
both publish and non-publish error paths but have no shared constant. The `server` package has
two near-identical helpers (`sendErrorToClient` and `sendPublishError`) doing the same thing.

## Changes

### 1. AsyncAPI Spec — Merge `Error` + `MessageError` into single `Error` schema

**`ws/docs/asyncapi/client-ws.asyncapi.yaml`**

Remove the `MessageError` schema (lines 647-669) entirely.

Update the `Error` schema (lines 633-645) to include a `code` field:
```yaml
Error:
  type: object
  description: >
    Generic error response. Sent when a client message cannot be parsed
    (e.g., invalid JSON). The code field identifies the specific error.
  required:
    - type
    - code
    - message
  properties:
    type:
      type: string
      const: error
    code:
      type: string
      description: Machine-readable error code.
      enum:
        - invalid_json
    message:
      type: string
      description: Human-readable error description.
  examples:
    - type: error
      code: invalid_json
      message: "Message is not valid JSON"
```

### 2. AsyncAPI Spec — Remove `messageError` message, keep only `error`

In `components.messages` section:
- Remove `messageError` message definition (lines 913-919)
- Keep `error` message definition (lines 905-911), which now references the updated `Error` schema

In `channels.clientReceive.messages` section (line 154):
- Remove `messageError` reference

In `operations` section:
- Remove `receiveMessageError` operation (lines 300-308)

### 3. AsyncAPI Spec — Add `code` to `ReconnectError` for consistency

Update `ReconnectError` schema to include a `code` field:
```yaml
ReconnectError:
  type: object
  description: Error response when reconnect/replay fails.
  required:
    - type
    - code
    - message
  properties:
    type:
      type: string
      const: reconnect_error
    code:
      type: string
      description: Machine-readable error code.
      enum:
        - invalid_request
        - not_available
        - replay_failed
    message:
      type: string
      description: Human-readable error description.
```

### 4. Go Code — Unify `PublishErrorCode` → `ErrorCode` in `shared/protocol/errors.go`

**`ws/internal/shared/protocol/errors.go`**

Rename `PublishErrorCode` to `ErrorCode` and consolidate all error codes under the single type.
The typing is justified by `PublishErrorMessages` map lookup (typed key) and by the typed
`code ErrorCode` parameter on `sendErrorToClient` / `sendPublishError`.

```go
// ErrorCode represents machine-readable error codes for all error response types.
// Used across error, subscribe_error, unsubscribe_error, publish_error,
// and reconnect_error responses.
type ErrorCode string

const (
	// General error codes (used across multiple response types).

	// ErrCodeInvalidJSON indicates a client message is not valid JSON.
	ErrCodeInvalidJSON ErrorCode = "invalid_json"

	// ErrCodeInvalidRequest indicates a malformed request.
	// Used by subscribe_error, unsubscribe_error, reconnect_error, publish_error.
	ErrCodeInvalidRequest ErrorCode = "invalid_request"

	// ErrCodeNotAvailable indicates the requested feature is not available.
	// Used by publish_error and reconnect_error.
	ErrCodeNotAvailable ErrorCode = "not_available"

	// Publish-specific error codes.

	// ErrCodeInvalidChannel indicates invalid channel format.
	ErrCodeInvalidChannel ErrorCode = "invalid_channel"

	// ErrCodeMessageTooLarge indicates payload exceeds size limit.
	ErrCodeMessageTooLarge ErrorCode = "message_too_large"

	// ErrCodeRateLimited indicates publish rate limit exceeded.
	ErrCodeRateLimited ErrorCode = "rate_limited"

	// ErrCodePublishFailed indicates Kafka publish failed.
	ErrCodePublishFailed ErrorCode = "publish_failed"

	// ErrCodeForbidden indicates not authorized to publish to channel.
	ErrCodeForbidden ErrorCode = "forbidden"

	// ErrCodeTopicNotProvisioned indicates the category topic doesn't exist.
	ErrCodeTopicNotProvisioned ErrorCode = "topic_not_provisioned"

	// ErrCodeServiceUnavailable indicates Kafka is unavailable (circuit open).
	ErrCodeServiceUnavailable ErrorCode = "service_unavailable"

	// Reconnect-specific error codes.

	// ErrCodeReplayFailed indicates Kafka message replay failed.
	ErrCodeReplayFailed ErrorCode = "replay_failed"
)

// PublishErrorMessages provides human-readable messages for publish error codes.
var PublishErrorMessages = map[ErrorCode]string{
	ErrCodeNotAvailable:        "Publishing is not enabled on this server",
	ErrCodeInvalidRequest:      "Invalid publish request format",
	ErrCodeInvalidChannel:      "Channel must have format: tenant.identifier.category",
	ErrCodeMessageTooLarge:     "Message exceeds maximum size limit",
	ErrCodeRateLimited:         "Publish rate limit exceeded",
	ErrCodePublishFailed:       "Failed to publish message",
	ErrCodeForbidden:           "Not authorized to publish to this channel",
	ErrCodeTopicNotProvisioned: "Category is not provisioned for your tenant",
	ErrCodeServiceUnavailable:  "Service temporarily unavailable, please retry",
}
```

**What changed vs current `errors.go`:**
- `PublishErrorCode` → `ErrorCode` (rename only, same underlying `string` type)
- `ErrCodeInvalidRequest` / `ErrCodeNotAvailable` comments updated (no longer publish-specific)
- Added: `ErrCodeInvalidJSON`, `ErrCodeReplayFailed`
- `PublishErrorMessages` map key: `PublishErrorCode` → `ErrorCode`
- Sentinel errors comment (line 53): "mapped to PublishErrorCode" → "mapped to ErrorCode"
- No constants removed, no values changed — fully backward-compatible rename

### 5. Go Code — Update `errors_test.go`

**`ws/internal/shared/protocol/errors_test.go`**

Rename all `PublishErrorCode` references → `ErrorCode`. Update test function names:
- `TestPublishErrorCode_Values` → `TestErrorCode_Values` — add new constants to expected map
- `TestPublishErrorCode_UniqueValues` → `TestErrorCode_UniqueValues` — add new constants to list
- `TestPublishErrorCode_TypeConversion` → `TestErrorCode_TypeConversion` — update type name
- `TestPublishErrorMessages_*` tests — update map key type references
- `BenchmarkErrorCode_StringConversion` — update type name

Add `ErrCodeInvalidJSON` and `ErrCodeReplayFailed` to all value/uniqueness test lists.

### 6. Go Code — Update `sendErrorToClient` and `sendPublishError` signatures

**`ws/internal/server/handlers_message.go`**

Change `sendErrorToClient` to accept `ErrorCode`:

```go
func (s *Server) sendErrorToClient(c *Client, errType string, code protocol.ErrorCode, message string) {
	errResp := map[string]any{
		"type":    errType,
		"code":    code,
		"message": message,
	}
	// ... (rest unchanged)
}
```

**`ws/internal/server/handlers_publish.go`**

Change `sendPublishError` to accept `ErrorCode` and delegate to `sendErrorToClient`:

```go
// sendPublishError sends a publish error response to the client.
func (s *Server) sendPublishError(c *Client, code protocol.ErrorCode, message string) {
	s.sendErrorToClient(c, protocol.RespTypePublishError, code, message)
}
```

This eliminates the duplicate `map[string]any` + marshal + non-blocking send logic (was copy-paste
of `sendErrorToClient` body). `sendPublishError` remains as a convenience wrapper with a
descriptive name.

### 7. Go Code — Update all call sites

**`ws/internal/server/handlers_message.go`** — use `sendErrorToClient` for reconnect errors,
use `ErrCode*` constants everywhere:

```go
// Line 24 — invalid JSON error
s.sendErrorToClient(c, protocol.MsgTypeError, protocol.ErrCodeInvalidJSON, "Message is not valid JSON")

// Line 96 — invalid subscribe
s.sendErrorToClient(c, protocol.RespTypeSubscribeError, protocol.ErrCodeInvalidRequest, "Invalid subscribe request format")

// Line 138 — invalid unsubscribe
s.sendErrorToClient(c, protocol.RespTypeUnsubscribeError, protocol.ErrCodeInvalidRequest, "Invalid unsubscribe request format")

// Line ~209 — invalid reconnect request (replaces 10-line block)
s.sendErrorToClient(c, protocol.RespTypeReconnectError, protocol.ErrCodeInvalidRequest, "Invalid reconnect request format")

// Line ~234 — no Kafka consumer (replaces 10-line block)
s.sendErrorToClient(c, protocol.RespTypeReconnectError, protocol.ErrCodeNotAvailable, "Message replay not available (no Kafka consumer)")

// Line ~268 — replay failed (replaces 10-line block)
s.sendErrorToClient(c, protocol.RespTypeReconnectError, protocol.ErrCodeReplayFailed, fmt.Sprintf("Failed to replay messages: %v", err))
```

**`ws/internal/server/handlers_publish.go`** — remove all `string()` conversions (6 call sites):

```go
// Before (repeated 6 times):
s.sendPublishError(c, string(protocol.ErrCodeNotAvailable), protocol.PublishErrorMessages[protocol.ErrCodeNotAvailable])

// After:
s.sendPublishError(c, protocol.ErrCodeNotAvailable, protocol.PublishErrorMessages[protocol.ErrCodeNotAvailable])
```

**`ws/internal/gateway/proxy.go`** — rename type in `sendPublishErrorToClient` signature:

```go
// Before:
func (p *Proxy) sendPublishErrorToClient(code protocol.PublishErrorCode) ([]byte, error) {

// After:
func (p *Proxy) sendPublishErrorToClient(code protocol.ErrorCode) ([]byte, error) {
```

No other changes needed in `proxy.go` — call sites already pass `protocol.ErrCode*` constants
which are now `ErrorCode` type. The `string(code)` conversion inside the function body is
required (map is `map[string]string`, not `map[string]any`). The `PublishErrorMessages[code]`
map lookup works unchanged with the renamed type.

### 8. Bundled spec — Update `bundled.yaml` to match

**`ws/docs/asyncapi/bundled.yaml`**

The bundled spec inlines everything (no `$ref`), so schemas/messages appear in 3 sections:
`channels` (inline payloads), `components.schemas`, and `components.messages`. All locations
must be updated in sync.

**Remove (messageError) — 4 locations:**

| # | Section Path | Lines | Action |
|---|---|---|---|
| 1 | `channels.clientReceive.messages.messageError` | 662-689 | Delete entire message block |
| 2 | `operations.receiveMessageError` | 873-881 | Delete entire operation block |
| 3 | `components.schemas.MessageError` | 1216-1238 | Delete entire schema |
| 4 | `components.messages.messageError` | 1726-1753 | Delete entire message block |

**Update Error schema (add `code` field) — 3 locations:**

| # | Section Path | Lines | Action |
|---|---|---|---|
| 5 | `channels.clientReceive.messages.error` (inline payload) | 644-661 | Add `code` to required + properties, add examples |
| 6 | `components.schemas.Error` | 1203-1215 | Add `code` to required + properties, add examples |
| 7 | `components.messages.error` (inline payload) | 1708-1725 | Add `code` to required + properties, add examples |

**Update ReconnectError schema (add `code` field) — 3 locations:**

| # | Section Path | Lines | Action |
|---|---|---|---|
| 8 | `channels.clientReceive.messages.reconnectError` (inline payload) | 601-621 | Add `code` to required + properties, update examples |
| 9 | `components.schemas.ReconnectError` | 1170-1185 | Add `code` to required + properties, update examples |
| 10 | `components.messages.reconnectError` (inline payload) | 1665-1685 | Add `code` to required + properties, update examples |

## Files Modified (7 files)

| # | File | Change |
|---|---|---|
| 1 | `ws/docs/asyncapi/client-ws.asyncapi.yaml` | Merge Error+MessageError, remove messageError message/operation, add code to ReconnectError |
| 2 | `ws/docs/asyncapi/bundled.yaml` | Same changes as above (10 locations) |
| 3 | `ws/internal/shared/protocol/errors.go` | Rename `PublishErrorCode` → `ErrorCode`, add `ErrCodeInvalidJSON` + `ErrCodeReplayFailed` |
| 4 | `ws/internal/shared/protocol/errors_test.go` | Rename type refs, add new constants to test lists |
| 5 | `ws/internal/server/handlers_message.go` | `sendErrorToClient` accepts `ErrorCode`, use for reconnect errors, use `ErrCode*` constants |
| 6 | `ws/internal/server/handlers_publish.go` | `sendPublishError` accepts `ErrorCode` + delegates to `sendErrorToClient`, remove `string()` conversions |
| 7 | `ws/internal/gateway/proxy.go` | Rename `PublishErrorCode` → `ErrorCode` in `sendPublishErrorToClient` signature |

## Go Code Impact Analysis

- **Error/MessageError merge**: Spec-only change. Wire format unchanged.
- **Reconnect error `code` field**: Replaces 3 inline `map[string]any` blocks with `sendErrorToClient` calls. Net ~24 lines removed.
- **`PublishErrorCode` → `ErrorCode` rename**: All 9 existing `ErrCode*` constants keep their names and values. Only the type name changes. Fully backward-compatible.
- **`sendPublishError` delegation**: Body replaced with single `sendErrorToClient` call. Eliminates duplicate marshal + non-blocking send logic. Wire format unchanged.
- **`string()` conversion removal**: 6 call sites in `handlers_publish.go` drop unnecessary `string()` conversions. The `string(code)` in `proxy.go` is retained — required because the map is `map[string]string` (not `map[string]any`).
- **`wsloadtest/connection.go`**: Does NOT reference `PublishErrorCode` or `messageError` — no changes needed.
- **No breaking changes**: All wire `type` and `code` values unchanged. Only addition is `code` field on `reconnect_error` responses (additive).

## Guidelines Compliance

| Guideline | Status | Notes |
|---|---|---|
| No hardcoded values / magic strings | PASS | All error codes use `ErrCode*` constants from `shared/protocol/` |
| No copy-paste code | PASS | Reconnect errors use `sendErrorToClient`; `sendPublishError` delegates to it |
| Shared code consolidation | PASS | Single `ErrorCode` type in `shared/protocol/errors.go` — no duplicates |
| No duplicate constants | PASS | Unified under `ErrorCode` — zero overlap |
| Go idioms — consistent patterns | PASS | Typed `ErrorCode` justified by map key + typed function parameters |
| Non-blocking message delivery | PASS | All error sends go through `sendErrorToClient` with `select/default` |
| Error handling | PASS | No changes to error handling patterns |
| New shared code has tests | PASS | Existing tests updated + new constants added to value/uniqueness checks |

## Verification

1. `cd ws && go build ./...` — builds clean
2. `cd ws && go test ./...` — all tests pass
3. Review AsyncAPI spec in an AsyncAPI viewer to confirm schema renders correctly
4. Verify all error `type` values on the wire remain unchanged (no breaking changes for clients)

## Error Type Summary After Consolidation

| Wire Type | Schema | Has Code | Codes | Constants |
|---|---|---|---|---|
| `error` | Error | Yes | `invalid_json` | `ErrCodeInvalidJSON` |
| `subscribe_error` | SubscribeError | Yes | `invalid_request` | `ErrCodeInvalidRequest` |
| `unsubscribe_error` | UnsubscribeError | Yes | `invalid_request` | `ErrCodeInvalidRequest` |
| `publish_error` | PublishError | Yes | 9 codes | `ErrCode*` (see errors.go) |
| `reconnect_error` | ReconnectError | Yes (new) | `invalid_request`, `not_available`, `replay_failed` | `ErrCodeInvalidRequest`, `ErrCodeNotAvailable`, `ErrCodeReplayFailed` |

All constants are `ErrorCode` type. No duplicate constants. No unnecessary `string()` conversions
(`proxy.go` retains `string(code)` — required by `map[string]string`).
