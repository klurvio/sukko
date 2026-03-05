# Plan: Fix Silent Error Returns in WebSocket Protocol

**Status:** Implemented
**Date:** 2026-02-06
**Scope:** Add client error responses for silent return paths in WebSocket message handling

---

## Summary

Several error paths in the WebSocket server silently return after logging, without sending an error response to the client. This violates the coding guideline: **"Observable by Default - Every significant operation must be logged and/or metriced. Silent failures are forbidden."**

The `publish` and `reconnect` handlers already follow the correct pattern. The `subscribe`, `unsubscribe`, and main message parser do not.

---

## Files to Update

### 1. `ws/internal/shared/protocol/types.go` — Add response type constants

Add two new response types to the existing const block (after line 30):

```go
RespTypeSubscribeError    = "subscribe_error"
RespTypeUnsubscribeError  = "unsubscribe_error"
```

Note: `MsgTypeError = "error"` already exists at line 21 for generic errors.

---

### 2. `ws/internal/server/handlers_message.go` — Add helper function and fix error paths

#### Add ONE generic helper function (reuse pattern, avoid duplication):

```go
// sendErrorToClient sends an error response to the client.
// Used for all error types (message parsing, subscribe, unsubscribe).
func (s *Server) sendErrorToClient(c *Client, errType, code, message string) {
	errResp := map[string]any{
		"type":    errType,
		"code":    code,
		"message": message,
	}

	if data, err := json.Marshal(errResp); err == nil {
		select {
		case c.send <- data:
			// Error sent
		default:
			// Client buffer full - non-blocking per graceful degradation guidelines
		}
	}
}
```

#### Fix error path 1: Invalid JSON in main message parser (lines 19-25)

**Current:**
```go
if err := json.Unmarshal(data, &req); err != nil {
    s.logger.Warn().
        Int64("client_id", c.id).
        Err(err).
        Msg("Client sent invalid JSON")
    return
}
```

**Fixed:**
```go
if err := json.Unmarshal(data, &req); err != nil {
    s.logger.Warn().
        Int64("client_id", c.id).
        Err(err).
        Msg("Client sent invalid JSON")
    s.sendErrorToClient(c, protocol.MsgTypeError, "invalid_json", "Message is not valid JSON")
    return
}
```

#### Fix error path 2: Invalid subscribe data (lines 90-96)

**Current:**
```go
if err := json.Unmarshal(req.Data, &subReq); err != nil {
    s.logger.Warn().
        Int64("client_id", c.id).
        Err(err).
        Msg("Client sent invalid subscribe request")
    return
}
```

**Fixed:**
```go
if err := json.Unmarshal(req.Data, &subReq); err != nil {
    s.logger.Warn().
        Int64("client_id", c.id).
        Err(err).
        Msg("Client sent invalid subscribe request")
    s.sendErrorToClient(c, protocol.RespTypeSubscribeError, "invalid_request", "Invalid subscribe request format")
    return
}
```

#### Fix error path 3: Invalid unsubscribe data (lines 131-137)

**Current:**
```go
if err := json.Unmarshal(req.Data, &unsubReq); err != nil {
    s.logger.Warn().
        Int64("client_id", c.id).
        Err(err).
        Msg("Client sent invalid unsubscribe request")
    return
}
```

**Fixed:**
```go
if err := json.Unmarshal(req.Data, &unsubReq); err != nil {
    s.logger.Warn().
        Int64("client_id", c.id).
        Err(err).
        Msg("Client sent invalid unsubscribe request")
    s.sendErrorToClient(c, protocol.RespTypeUnsubscribeError, "invalid_request", "Invalid unsubscribe request format")
    return
}
```

---

### 3. `ws/internal/server/handlers_message_test.go` — Add table-driven test

Add ONE table-driven test covering all three error paths (follows existing test patterns in the file):

```go
func TestHandleClientMessage_ErrorResponses(t *testing.T) {
    t.Parallel()

    tests := []struct {
        name     string
        input    string
        wantType string
        wantCode string
    }{
        {
            name:     "invalid_json",
            input:    `{invalid json`,
            wantType: protocol.MsgTypeError,
            wantCode: "invalid_json",
        },
        {
            name:     "invalid_subscribe_data",
            input:    `{"type":"subscribe","data":"not an object"}`,
            wantType: protocol.RespTypeSubscribeError,
            wantCode: "invalid_request",
        },
        {
            name:     "invalid_unsubscribe_data",
            input:    `{"type":"unsubscribe","data":"not an object"}`,
            wantType: protocol.RespTypeUnsubscribeError,
            wantCode: "invalid_request",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()

            // Minimal server setup for error path testing
            server := &Server{
                logger:            zerolog.New(io.Discard),
                subscriptionIndex: NewSubscriptionIndex(),
            }

            // Minimal client setup
            client := &Client{
                id:            1,
                send:          make(chan []byte, 16),
                subscriptions: NewSubscriptionSet(),
            }

            server.handleClientMessage(client, []byte(tt.input))

            select {
            case respBytes := <-client.send:
                var resp map[string]any
                if err := json.Unmarshal(respBytes, &resp); err != nil {
                    t.Fatalf("Failed to unmarshal response: %v", err)
                }
                if resp["type"] != tt.wantType {
                    t.Errorf("type = %v, want %v", resp["type"], tt.wantType)
                }
                if resp["code"] != tt.wantCode {
                    t.Errorf("code = %v, want %v", resp["code"], tt.wantCode)
                }
            case <-time.After(100 * time.Millisecond):
                t.Error("Expected error response, got none")
            }
        })
    }
}
```

**Required imports** (add if not present):
```go
import (
    "io"
    "github.com/rs/zerolog"
    "github.com/Toniq-Labs/sukko/internal/shared/protocol"
)
```

---

### 4. `ws/docs/asyncapi/client-ws.asyncapi.yaml` — Document new error responses

Add to the components/schemas section:

```yaml
SubscribeError:
  type: object
  description: Error response for failed subscribe requests
  properties:
    type:
      type: string
      const: subscribe_error
    code:
      type: string
      enum: [invalid_request]
    message:
      type: string
  required: [type, code, message]

UnsubscribeError:
  type: object
  description: Error response for failed unsubscribe requests
  properties:
    type:
      type: string
      const: unsubscribe_error
    code:
      type: string
      enum: [invalid_request]
    message:
      type: string
  required: [type, code, message]

MessageError:
  type: object
  description: Generic error for malformed messages
  properties:
    type:
      type: string
      const: error
    code:
      type: string
      enum: [invalid_json]
    message:
      type: string
  required: [type, code, message]
```

---

## What NOT to Change

### Unknown message type (handlers_message.go, line 179-186)

**Reason:** Forward compatibility. The comment explicitly documents this: "Log but don't disconnect (might be future feature we haven't implemented yet)". Intentional design decision.

### Gateway proxy.go subscribe parse failures (line 340-342)

**Reason:** Defense in depth. Gateway passes through unparseable messages; backend will now send appropriate error.

### No new error code types in protocol/errors.go

**Reason:** Avoid over-engineering. The existing `PublishErrorCode` type has 9 codes justifying the pattern. Subscribe/unsubscribe each have ONE error case - typed enums with maps would be excessive. Simple string literals suffice.

---

## Coding Guidelines Compliance

| Guideline | How This Plan Complies |
|-----------|------------------------|
| **Observable by Default** | Silent failures replaced with error responses |
| **Avoid Over-Engineering** | One generic helper, no typed enums for single-value cases |
| **No Duplicate Code** | Single `sendErrorToClient` function, not three copies |
| **Graceful Degradation** | Non-blocking sends with `select { default: }` |
| **Table-Driven Tests** | One test function with test table |
| **Shared Code** | Response types added to existing `protocol/types.go` |

---

## New Client-Facing Error Responses

| Error Type | Code | When Sent |
|------------|------|-----------|
| `error` | `invalid_json` | Message is not valid JSON |
| `subscribe_error` | `invalid_request` | Subscribe data malformed |
| `unsubscribe_error` | `invalid_request` | Unsubscribe data malformed |

---

## Implementation Order

1. Add response type constants to `shared/protocol/types.go`
2. Add `sendErrorToClient` helper to `handlers_message.go`
3. Update three error paths to use helper
4. Add table-driven test to `handlers_message_test.go`
5. Update AsyncAPI documentation

---

## Verification

```bash
cd ws && go build ./...     # Clean compilation
cd ws && go test ./...      # All tests pass
cd ws && go vet ./...       # No vet issues
```

---

## Estimated Changes

| File | Lines Added | Lines Modified |
|------|-------------|----------------|
| `shared/protocol/types.go` | 2 | 0 |
| `server/handlers_message.go` | ~18 | 3 |
| `server/handlers_message_test.go` | ~55 | 0 |
| `docs/asyncapi/client-ws.asyncapi.yaml` | ~30 | 0 |

**Total:** ~105 lines added, 3 lines modified
