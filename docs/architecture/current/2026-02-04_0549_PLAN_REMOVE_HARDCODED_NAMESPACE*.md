# Plan: Remove Hardcoded Namespace Normalization

## Goal
Remove hardcoded namespace mapping from `NormalizeEnv()` function. Namespaces should be configured directly via Helm charts without code-level normalization.

---

## Current State

**Hardcoded mapping** in `ws/internal/shared/kafka/config.go:75-89`:
```go
switch env {
case "development", "local", "":
    return "local"
case "develop", "dev":
    return "dev"
case "staging", "stage":
    return "staging"
case "production", "prod":
    return "prod"
default:
    return env  // Pass-through
}
```

**Problem**: Multiple aliases normalized to same value. Configuration should be explicit.

---

## Solution: Simplify to Pass-Through

Replace `NormalizeEnv()` with simple trimming/lowercase only. Helm charts will specify exact namespace values.

### Valid Namespaces (configured, not hardcoded)
- `local`
- `dev`
- `stag`
- `prod`

### Changes Required

#### 1. Simplify `NormalizeEnv()`
**File**: `ws/internal/shared/kafka/config.go`

```go
// NormalizeEnv normalizes the namespace string (lowercase, trim whitespace).
// The actual namespace value comes from configuration, not code mapping.
func NormalizeEnv(env string) string {
    return strings.ToLower(strings.TrimSpace(env))
}
```

#### 2. Update Tests
**File**: `ws/internal/shared/kafka/config_test.go`

Remove tests for hardcoded mappings, keep only:
- Lowercase conversion
- Whitespace trimming
- Pass-through behavior

#### 3. Update Helm Default Values
**Files**:
- `deployments/helm/odin/charts/ws-server/values.yaml`
- `deployments/helm/odin/charts/provisioning/values.yaml`

Ensure defaults are explicit normalized values:
- `local` (not "development")
- `dev` (not "develop")
- `stag` (not "staging" or "stage")
- `prod` (not "production")

#### 4. Update AsyncAPI Documentation
**File**: `ws/docs/asyncapi/asyncapi.yaml`

Update namespace documentation to reflect that values are configured, not hardcoded.

---

## Files to Modify

| File | Change |
|------|--------|
| `ws/internal/shared/kafka/config.go` | Simplify `NormalizeEnv()` to just trim/lowercase |
| `ws/internal/shared/kafka/config_test.go` | Update tests for new behavior |
| `ws/docs/asyncapi/asyncapi.yaml` | Update namespace documentation |

---

## Verification

1. **Run tests**: `task test:unit` - verify config tests pass
2. **Validate AsyncAPI**: `task asyncapi:bundle` - verify spec is valid
3. **Code grep**: Ensure no other code relies on the old mappings
