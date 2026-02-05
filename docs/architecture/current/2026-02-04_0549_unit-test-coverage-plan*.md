# Unit Test Coverage Plan

**Status:** ✅ Implemented
**Date:** 2026-01-31
**Scope:** Add missing unit tests for shared protocol types and validation helpers

---

## Analysis Summary

### Packages Missing Tests

| Package | File | Status | Priority |
|---------|------|--------|----------|
| `shared/protocol` | `types.go` | **NO TESTS** | High |
| `shared/protocol` | `errors.go` | **NO TESTS** | High |
| `shared/protocol` | `limits.go` | **NO TESTS** | Medium |
| `shared/platform` | `validation.go` | **NO TESTS** | High |

### Packages With Adequate Tests

| Package | File | Status |
|---------|------|--------|
| `gateway` | `proxy_test.go` | Has tests for intercept, channel mapping |
| `server` | `handlers_publish_test.go` | Has tests for channel validation, parsing |
| `shared/kafka` | `producer_test.go` | Has tests for parseChannel, config |
| `shared/platform` | `gateway_config_test.go` | Has tests for log level/format via config |
| `shared/platform` | `server_config_test.go` | Has tests for log level/format via config |

---

## New Test Files to Create

### 1. `ws/internal/shared/protocol/types_test.go`

Test JSON serialization/deserialization for all protocol types.

```go
// Tests to implement:

// ClientMessage
- TestClientMessage_MarshalJSON
- TestClientMessage_UnmarshalJSON
- TestClientMessage_EmptyData

// SubscribeData
- TestSubscribeData_MarshalJSON
- TestSubscribeData_UnmarshalJSON
- TestSubscribeData_EmptyChannels
- TestSubscribeData_MultipleChannels

// UnsubscribeData
- TestUnsubscribeData_MarshalJSON
- TestUnsubscribeData_UnmarshalJSON

// PublishData
- TestPublishData_MarshalJSON
- TestPublishData_UnmarshalJSON
- TestPublishData_RawMessagePreserved

// Constants
- TestMessageTypeConstants_Values
- TestResponseTypeConstants_Values
```

### 2. `ws/internal/shared/protocol/errors_test.go`

Test error codes, messages, and sentinel errors.

```go
// Tests to implement:

// Error Code Constants
- TestPublishErrorCode_Values
- TestPublishErrorCode_UniqueValues

// Error Messages Map
- TestPublishErrorMessages_AllCodesHaveMessages
- TestPublishErrorMessages_NoEmptyMessages

// Sentinel Errors
- TestSentinelErrors_NotNil
- TestSentinelErrors_UniqueMessages
- TestSentinelErrors_ErrorInterface
```

### 3. `ws/internal/shared/protocol/limits_test.go`

Test constant values and relationships.

```go
// Tests to implement:

// Size Limits
- TestDefaultMaxPublishSize_Value
- TestDefaultMaxPublishSize_Is64KB
- TestDefaultSendBufferSize_Value

// Rate Limits
- TestDefaultPublishRateLimit_Value
- TestDefaultPublishBurst_Value

// Channel Parts
- TestMinClientChannelParts_Value
- TestMinInternalChannelParts_Value
- TestMinInternalChannelParts_GreaterThanClient
```

### 4. `ws/internal/shared/platform/validation_test.go`

Test validation helper functions.

```go
// Tests to implement:

// ValidateLogLevel
- TestValidateLogLevel_ValidLevels (debug, info, warn, error)
- TestValidateLogLevel_InvalidLevels (DEBUG, trace, invalid, empty)
- TestValidateLogLevel_ErrorMessage

// ValidateLogFormat
- TestValidateLogFormat_ValidFormats (json, text, pretty)
- TestValidateLogFormat_InvalidFormats (JSON, xml, console, empty)
- TestValidateLogFormat_ErrorMessage

// ValidateKafkaSASLMechanism
- TestValidateKafkaSASLMechanism_ValidMechanisms (scram-sha-256, scram-sha-512)
- TestValidateKafkaSASLMechanism_InvalidMechanisms (plain, SCRAM-SHA-256, empty)
- TestValidateKafkaSASLMechanism_ErrorMessage

// Maps
- TestValidLogLevels_Contents
- TestValidLogFormats_Contents
- TestValidKafkaSASLMechanisms_Contents
```

---

## Missing Tests in Existing Files

### `ws/internal/gateway/proxy_test.go`

Add tests for publish-specific functionality:

```go
// Tests to add:

// Rate Limiting
- TestProxy_InterceptPublish_RateLimited
- TestProxy_InterceptPublish_RateLimitAllows

// Size Validation
- TestProxy_InterceptPublish_MessageTooLarge
- TestProxy_InterceptPublish_MessageWithinLimit

// Error Responses
- TestProxy_SendPublishErrorToClient_RateLimited
- TestProxy_SendPublishErrorToClient_MessageTooLarge
- TestProxy_SendPublishErrorToClient_Forbidden

// Double-Prefix Detection
- TestProxy_InterceptPublish_DoublePrefixPrevented
- TestProxy_InterceptPublish_AlreadyPrefixed
```

---

## Implementation Order

### Phase 1: Protocol Package Tests (High Priority)
1. `protocol/errors_test.go` - Ensures error codes are correct
2. `protocol/types_test.go` - Ensures JSON serialization works
3. `protocol/limits_test.go` - Ensures constants are correct

### Phase 2: Validation Tests (High Priority)
4. `platform/validation_test.go` - Dedicated tests for validators

### Phase 3: Gateway Publish Tests (Medium Priority)
5. Add missing tests to `proxy_test.go`

---

## Test Patterns to Follow

### Table-Driven Tests
```go
func TestValidateLogLevel_ValidLevels(t *testing.T) {
    t.Parallel()
    validLevels := []string{"debug", "info", "warn", "error"}

    for _, level := range validLevels {
        t.Run(level, func(t *testing.T) {
            t.Parallel()
            err := ValidateLogLevel(level)
            if err != nil {
                t.Errorf("ValidateLogLevel(%q) = %v, want nil", level, err)
            }
        })
    }
}
```

### JSON Round-Trip Tests
```go
func TestClientMessage_JSONRoundTrip(t *testing.T) {
    t.Parallel()
    original := ClientMessage{
        Type: MsgTypeSubscribe,
        Data: json.RawMessage(`{"channels":["BTC.trade"]}`),
    }

    data, err := json.Marshal(original)
    if err != nil {
        t.Fatalf("Marshal failed: %v", err)
    }

    var decoded ClientMessage
    if err := json.Unmarshal(data, &decoded); err != nil {
        t.Fatalf("Unmarshal failed: %v", err)
    }

    if decoded.Type != original.Type {
        t.Errorf("Type = %q, want %q", decoded.Type, original.Type)
    }
}
```

---

## Verification

```bash
# Run all tests
go test ./... -v

# Run with coverage
go test ./... -cover

# Check specific package coverage
go test -coverprofile=coverage.out ./internal/shared/protocol/
go tool cover -func=coverage.out

# Run new tests only
go test ./internal/shared/protocol/... -v
go test ./internal/shared/platform/... -v -run "Validate"
```

---

## Expected Coverage Improvements

| Package | Before | After |
|---------|--------|-------|
| `shared/protocol` | 0% | ~90% |
| `shared/platform/validation.go` | 0% (direct) | ~95% |
| `gateway/proxy.go` | ~70% | ~85% |

---

## Files to Create

| File | Lines (est.) |
|------|--------------|
| `ws/internal/shared/protocol/types_test.go` | ~150 |
| `ws/internal/shared/protocol/errors_test.go` | ~120 |
| `ws/internal/shared/protocol/limits_test.go` | ~80 |
| `ws/internal/shared/platform/validation_test.go` | ~150 |

**Total new test code:** ~500 lines
