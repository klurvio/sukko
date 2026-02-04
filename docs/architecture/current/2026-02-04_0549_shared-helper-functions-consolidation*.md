# Shared Helper Functions Consolidation Plan

**Goal:** Eliminate duplicate helper functions across gateway, server, and provisioning packages by creating shared utility packages.

**Status:** ✅ Implemented
**Date:** 2026-01-31

---

## Summary of Duplications Found

| Category | Gateway | Server | Provisioning | Shared | Action |
|----------|---------|--------|--------------|--------|--------|
| **Pattern Matching** | `matchPattern()` permissions.go:110 | - | - | `matchSimplePattern()` auth/channel.go:225 | Consolidate |
| **Token Extraction** | `extractToken()` gateway.go:348 | - | - | - | Create shared |
| **Client IP** | - | `getClientIP()` handlers_ws.go:166 | - | - | Create shared |
| **JSON Response** | - | - | `writeJSON()` api/handlers.go:347 | - | Create shared |
| **Error Response** | - | - | `writeError()` api/handlers.go:360 | - | Create shared |
| **Context Claims** | - | - | `GetClaimsFromContext()` middleware.go:172 | - | Consolidate to auth |
| **Context Actor** | - | - | `getActorFromContext()` service.go:521 (duplicate) | - | Consolidate |
| **Channel Validation** | - | `isValidPublishChannel()` handlers_publish.go:170 | - | - | Move to shared |
| **Placeholder Extract** | `extractPlaceholder()` permissions.go:144 | - | - | `PlaceholderResolver` auth/placeholders.go | Use shared |
| **Panic Recovery** | - | `recoverPanic()` pump.go:66 | - | `RecoverPanicAny()` logging/panic.go | Use shared |
| **Percentile Stats** | - | `calculateBufferStats()` handlers_http.go:236 | - | - | Low priority |

---

## Phase 1: HTTP Utilities Package (HIGH PRIORITY)

### New Package: `ws/internal/shared/httputil/`

**Files to create:**
- `request.go` - Token extraction, client IP
- `response.go` - JSON responses, error handling

### request.go

```go
package httputil

import (
    "net/http"
    "strings"
)

// ExtractBearerToken extracts JWT from query param "token" or Authorization header.
func ExtractBearerToken(r *http.Request) string {
    if token := r.URL.Query().Get("token"); token != "" {
        return token
    }
    auth := r.Header.Get("Authorization")
    if strings.HasPrefix(auth, "Bearer ") {
        return strings.TrimPrefix(auth, "Bearer ")
    }
    return ""
}

// GetClientIP extracts client IP, respecting X-Forwarded-For header.
func GetClientIP(r *http.Request) string {
    if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
        if idx := strings.Index(xff, ","); idx > 0 {
            return strings.TrimSpace(xff[:idx])
        }
        return strings.TrimSpace(xff)
    }
    if idx := strings.LastIndex(r.RemoteAddr, ":"); idx > 0 {
        return r.RemoteAddr[:idx]
    }
    return r.RemoteAddr
}
```

### response.go

```go
package httputil

import (
    "encoding/json"
    "net/http"
)

// WriteJSON writes a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, data any) error {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    return json.NewEncoder(w).Encode(data)
}

// WriteError writes a JSON error response.
func WriteError(w http.ResponseWriter, status int, code, message string) {
    WriteJSON(w, status, map[string]string{
        "code":    code,
        "message": message,
    })
}

// WriteHealthOK writes a standard health check response.
func WriteHealthOK(w http.ResponseWriter, serviceName string) {
    WriteJSON(w, http.StatusOK, map[string]string{
        "status":  "ok",
        "service": serviceName,
    })
}
```

### Files to Update

| File | Changes |
|------|---------|
| `gateway/gateway.go` | Replace `extractToken()` with `httputil.ExtractBearerToken()` |
| `server/handlers_ws.go` | Replace `getClientIP()` with `httputil.GetClientIP()` |
| `provisioning/api/handlers.go` | Replace `writeJSON()`, `writeError()` with httputil functions |

---

## Phase 2: Pattern Matching Consolidation (HIGH PRIORITY)

### New File: `ws/internal/shared/auth/pattern.go`

```go
package auth

import "strings"

// MatchPattern matches a pattern against a value using wildcard rules.
// Supports:
//   - Exact match: "foo.bar" matches "foo.bar"
//   - Catch-all: "*" matches anything
//   - Prefix wildcard: "*.suffix" matches "anything.suffix"
//   - Suffix wildcard: "prefix.*" matches "prefix.anything"
//   - Middle wildcard: "prefix*suffix" matches "prefixANYsuffix"
func MatchPattern(pattern, value string) bool {
    if pattern == "*" {
        return true
    }
    if pattern == value {
        return true
    }
    if after, found := strings.CutPrefix(pattern, "*"); found {
        return strings.HasSuffix(value, after)
    }
    if before, found := strings.CutSuffix(pattern, "*"); found {
        return strings.HasPrefix(value, before)
    }
    if idx := strings.Index(pattern, "*"); idx > 0 {
        prefix := pattern[:idx]
        suffix := pattern[idx+1:]
        return strings.HasPrefix(value, prefix) && strings.HasSuffix(value, suffix)
    }
    return false
}

// MatchAnyPattern returns true if value matches any pattern.
func MatchAnyPattern(patterns []string, value string) bool {
    for _, p := range patterns {
        if MatchPattern(p, value) {
            return true
        }
    }
    return false
}
```

### Files to Update

| File | Changes |
|------|---------|
| `gateway/permissions.go` | Replace `matchPattern()` with `auth.MatchPattern()` |
| `auth/channel.go` | Replace `matchSimplePattern()` with `MatchPattern()` (same package) |

---

## Phase 3: Context Helpers (MEDIUM PRIORITY)

### New File: `ws/internal/shared/auth/context.go`

```go
package auth

import "context"

type contextKey string

const (
    claimsKey     contextKey = "auth.claims"
    actorKey      contextKey = "auth.actor"
    actorTypeKey  contextKey = "auth.actor_type"
    clientIPKey   contextKey = "auth.client_ip"
)

// WithClaims adds JWT claims to context.
func WithClaims(ctx context.Context, claims *Claims) context.Context {
    return context.WithValue(ctx, claimsKey, claims)
}

// GetClaims retrieves JWT claims from context.
func GetClaims(ctx context.Context) *Claims {
    if v := ctx.Value(claimsKey); v != nil {
        return v.(*Claims)
    }
    return nil
}

// WithActor adds actor information to context.
func WithActor(ctx context.Context, actor, actorType, clientIP string) context.Context {
    ctx = context.WithValue(ctx, actorKey, actor)
    ctx = context.WithValue(ctx, actorTypeKey, actorType)
    ctx = context.WithValue(ctx, clientIPKey, clientIP)
    return ctx
}

// GetActor retrieves actor identifier from context.
func GetActor(ctx context.Context) string {
    if v := ctx.Value(actorKey); v != nil {
        return v.(string)
    }
    return "system"
}

// GetActorType retrieves actor type from context.
func GetActorType(ctx context.Context) string {
    if v := ctx.Value(actorTypeKey); v != nil {
        return v.(string)
    }
    return "system"
}

// GetClientIPFromContext retrieves client IP from context.
func GetClientIPFromContext(ctx context.Context) string {
    if v := ctx.Value(clientIPKey); v != nil {
        return v.(string)
    }
    return ""
}
```

### Files to Update

| File | Changes |
|------|---------|
| `provisioning/api/middleware.go` | Use `auth.WithClaims()`, `auth.GetClaims()` |
| `provisioning/service.go` | Use `auth.WithActor()`, `auth.GetActor()`, remove private duplicates |

---

## Phase 4: Channel Validation (MEDIUM PRIORITY)

### Add to: `ws/internal/shared/auth/channel.go`

```go
// ValidateInternalChannel validates internal channel format.
// Must have at least 3 dot-separated parts: {tenant}.{identifier}.{category}
func ValidateInternalChannel(channel string) error {
    if channel == "" {
        return errors.New("channel cannot be empty")
    }
    parts := strings.Split(channel, ".")
    if len(parts) < 3 {
        return fmt.Errorf("channel must have at least 3 parts: tenant.identifier.category (got %d)", len(parts))
    }
    for i, part := range parts {
        if part == "" {
            return fmt.Errorf("channel part %d cannot be empty", i)
        }
    }
    return nil
}

// ParseInternalChannel extracts tenant and category from internal channel.
func ParseInternalChannel(channel string) (tenant, category string, err error) {
    if err := ValidateInternalChannel(channel); err != nil {
        return "", "", err
    }
    parts := strings.Split(channel, ".")
    return parts[0], parts[len(parts)-1], nil
}
```

### Files to Update

| File | Changes |
|------|---------|
| `server/handlers_publish.go` | Replace `isValidPublishChannel()` with `auth.ValidateInternalChannel()` |
| `kafka/producer.go` | Use `auth.ParseInternalChannel()` in `parseChannel()` |

---

## Phase 5: Placeholder Extraction Migration (MEDIUM PRIORITY)

### Files to Update

| File | Changes |
|------|---------|
| `gateway/permissions.go` | Replace `extractPlaceholder()` with `auth.MatchPattern()` result captures |

The shared `auth.MatchPattern()` in `placeholders.go:142-184` already provides capture functionality via `MatchResult.Captures`. The gateway can use this instead of its custom `extractPlaceholder()` function.

---

## Phase 6: Panic Recovery (LOW PRIORITY)

### Files to Update

| File | Changes |
|------|---------|
| `server/pump.go` | Refactor `recoverPanic()` to delegate to `logging.RecoverPanicAny()` |

The `Pump.Logger` interface already supports panic logging. Update to use the shared implementation.

---

## Files Summary

### New Files (4)

| File | Purpose |
|------|---------|
| `ws/internal/shared/httputil/request.go` | Token extraction, client IP |
| `ws/internal/shared/httputil/response.go` | JSON responses, error handling |
| `ws/internal/shared/auth/pattern.go` | Consolidated pattern matching |
| `ws/internal/shared/auth/context.go` | Context helpers for claims/actor |

### Files to Modify (10)

| File | Changes |
|------|---------|
| `gateway/gateway.go` | Use `httputil.ExtractBearerToken()` |
| `gateway/permissions.go` | Use `auth.MatchPattern()`, remove local duplicates |
| `server/handlers_ws.go` | Use `httputil.GetClientIP()` |
| `server/handlers_publish.go` | Use `auth.ValidateInternalChannel()` |
| `server/pump.go` | Use `logging.RecoverPanicAny()` |
| `provisioning/api/handlers.go` | Use httputil functions |
| `provisioning/api/middleware.go` | Use `auth.WithClaims()`, `auth.GetClaims()` |
| `provisioning/service.go` | Use `auth.WithActor()`, `auth.GetActor()`, remove duplicates |
| `shared/auth/channel.go` | Add `ValidateInternalChannel()`, remove `matchSimplePattern()` |
| `shared/kafka/producer.go` | Use `auth.ParseInternalChannel()` |

---

## Implementation Order

| Phase | Item | Effort | Files |
|-------|------|--------|-------|
| 1 | HTTP Utilities | 1.5h | 2 new + 3 modify |
| 2 | Pattern Matching | 1h | 1 new + 2 modify |
| 3 | Context Helpers | 1h | 1 new + 2 modify |
| 4 | Channel Validation | 1h | 0 new + 3 modify |
| 5 | Placeholder Migration | 0.5h | 0 new + 1 modify |
| 6 | Panic Recovery | 0.5h | 0 new + 1 modify |

**Total:** ~6 hours

---

## Verification

```bash
# Build all packages
cd /Volumes/Dev/Codev/Toniq/odin-ws/ws
go build ./...

# Run all tests
go test ./... -short

# Verify no duplicates remain
grep -r "func extractToken" internal/
grep -r "func getClientIP" internal/
grep -r "func writeJSON" internal/
grep -r "func matchPattern" internal/
grep -r "func matchSimplePattern" internal/

# Check new package coverage
go test ./internal/shared/httputil/... -cover
go test ./internal/shared/auth/... -cover
```

---

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Import cycles | httputil has no internal dependencies |
| Breaking changes | All changes are internal packages |
| Test failures | Add tests for new shared functions before migration |
| Context key conflicts | Use package-prefixed keys |
