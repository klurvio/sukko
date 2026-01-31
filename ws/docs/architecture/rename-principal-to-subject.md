# Rename Channel Pattern Placeholder: `{principal}` → `{subject}`

**Goal:** Rename the channel pattern placeholder from `{principal}` to `{subject}` to align with JWT standard terminology.

**Status:** Planned
**Date:** 2026-01-31

---

## Rationale

- "Principal" is generic security jargon that's less intuitive
- The placeholder extracts the JWT `sub` (subject) claim
- "Subject" aligns with JWT standard terminology
- Self-documenting for anyone familiar with JWT

---

## Scope

### IN SCOPE (Channel Pattern Placeholder)
Files using `{principal}` as a channel pattern placeholder that maps to JWT subject:

| File | Changes |
|------|---------|
| `gateway/permissions.go` | Rename `extractUserPrincipal()` → `extractSubject()`, update placeholder |
| `gateway/permissions_test.go` | Update test patterns and captures |
| `gateway/gateway_test.go` | Update test config patterns |
| `gateway/proxy_test.go` | Update test config patterns |
| `shared/platform/gateway_config.go` | Update default `UserScopedPatterns` |
| `shared/platform/gateway_config_test.go` | Update test patterns |
| `asyncapi/asyncapi.yaml` | Update documentation |
| `asyncapi/bundled.yaml` | Update documentation |

### OUT OF SCOPE (Different Concept)
These use "principal" in a Kafka ACL context (User:{id} format) - NOT related:

- `provisioning/interfaces.go` - `FormatPrincipal()`, `ParsePrincipal()`, `ValidatePrincipal()`
- `provisioning/kafka/admin.go` - Kafka ACL operations
- `provisioning/lifecycle.go` - Kafka principal formatting
- `provisioning/service.go` - Kafka quota principal
- Log statements using `Str("principal", ...)` - just log field names

---

## Detailed Changes

### 1. `internal/gateway/permissions.go`

**Before:**
```go
// - User-scoped: JWT.sub must match the principal in the channel (e.g., balances.{principal})

// Check user-scoped patterns (JWT.sub must match principal)
if principal := pc.extractUserPrincipal(channel); principal != "" {
    return claims.Subject == principal
}

// extractUserPrincipal extracts the principal from a user-scoped channel.
// For pattern "balances.{principal}" and channel "balances.abc123", returns "abc123".
func (pc *PermissionChecker) extractUserPrincipal(channel string) string {
    for _, pattern := range pc.userScopedPatterns {
        result := auth.MatchPattern(pattern, channel)
        if result.Matched {
            if principal, ok := result.Captures["principal"]; ok && principal != "" {
                return principal
            }
        }
    }
    return ""
}
```

**After:**
```go
// - User-scoped: JWT.sub must match the subject in the channel (e.g., balances.{subject})

// Check user-scoped patterns (JWT.sub must match subject)
if subject := pc.extractSubject(channel); subject != "" {
    return claims.Subject == subject
}

// extractSubject extracts the subject (user/app ID) from a user-scoped channel.
// For pattern "balances.{subject}" and channel "balances.abc123", returns "abc123".
// The extracted value is compared against JWT claims.Subject.
func (pc *PermissionChecker) extractSubject(channel string) string {
    for _, pattern := range pc.userScopedPatterns {
        result := auth.MatchPattern(pattern, channel)
        if result.Matched {
            if subject, ok := result.Captures["subject"]; ok && subject != "" {
                return subject
            }
        }
    }
    return ""
}
```

### 2. `internal/gateway/permissions_test.go`

Update all test patterns:
- `{principal}` → `{subject}`
- `map[string]string{"principal": ...}` → `map[string]string{"subject": ...}`

### 3. `internal/shared/platform/gateway_config.go`

**Before:**
```go
UserScopedPatterns  []string `env:"GATEWAY_USER_SCOPED_PATTERNS" envSeparator:"," envDefault:"*.balances.{principal},*.trade.{principal},balances.{principal},notifications.{principal}"`
```

**After:**
```go
UserScopedPatterns  []string `env:"GATEWAY_USER_SCOPED_PATTERNS" envSeparator:"," envDefault:"*.balances.{subject},*.trade.{subject},balances.{subject},notifications.{subject}"`
```

### 4. `asyncapi/asyncapi.yaml` and `asyncapi/bundled.yaml`

Update documentation references from `{principal}` to `{subject}`.

---

## Migration Notes

**Breaking Change:** Yes, for users who have customized `GATEWAY_USER_SCOPED_PATTERNS` environment variable with `{principal}` placeholder.

**Migration Path:** Users must update their patterns from `{principal}` to `{subject}`.

---

## Verification

```bash
# Build all packages
go build ./...

# Run all tests
go test ./... -short

# Verify no {principal} remains in gateway code (Kafka uses are expected)
grep -r "{principal}" internal/gateway/
# Should return no results

# Verify {subject} is used
grep -r "{subject}" internal/gateway/
# Should show the new patterns
```
