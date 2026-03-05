# Implementation Plan: SQLite NOW() Function Compatibility Fix

**Branch**: `fix/sqlite-now-function` | **Date**: 2026-03-05 | **Spec**: specs/fix/sqlite-now-function/spec.md

## Summary

Replace all 17 `NOW()` PostgreSQL-only SQL function calls in the provisioning repository with Go-side `time.Now()` parameters. This follows the proven pattern already used by `tenant.Create()`, `key.Create()`, `topic.Create()`, and `quota.Create()` — methods that already pass `time.Now()` as query parameters. Combined with the already-implemented read-side fix (DSN `_time_format=sqlite&_texttotime=1` + `TEXT→DATETIME` column types), this makes provisioning fully functional with SQLite.

## Technical Context

**Language**: Go 1.22+
**Service**: provisioning (ws/internal/provisioning/repository/)
**Storage**: SQLite (modernc.org/sqlite) and PostgreSQL (lib/pq) — dual-backend via single `*sql.DB`
**Pattern**: Go-side `time.Now()` passed as `$N` positional parameters — both drivers serialize `time.Time` correctly

## Constitution Check

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Configuration | PASS | No new config — uses existing `time.Now()` |
| II. Defense in Depth | PASS | Parameterized queries prevent SQL injection |
| III. Error Handling | PASS | No error handling changes needed |
| IV. Graceful Degradation | PASS | Both backends work identically |
| VIII. Testing | PASS | Tests will verify parameterized timestamps on SQLite |
| X. Shared Code | PASS | No new utilities — follows existing codebase pattern |
| XI. Prior Art | PASS | Go-side timestamps is the standard pattern for cross-DB compat |

No constitution violations.

## Approach

**Pattern**: For each `NOW()` call in a SQL query string:
1. Add `now := time.Now()` before the query (or reuse existing `now` in the function)
2. Replace `NOW()` with the next positional parameter `$N`
3. Add `now` to the `ExecContext`/`QueryContext` argument list

**Key insight**: When `NOW()` appears twice in one statement (e.g., `created_at = NOW(), updated_at = NOW()`), both can reference the same `$N` parameter — ensuring identical timestamps.

## Change Inventory

### Already Implemented (read-side fix, on this branch)

| File | Change |
|------|--------|
| `repository/factory.go:79` | DSN: `_time_format=sqlite&_texttotime=1` |
| `migrations/sqlite/001_initial.sql` | `TEXT` → `DATETIME` on all timestamp columns (4 tables) |
| `migrations/sqlite/002_oidc_channel_rules.sql` | `TEXT` → `DATETIME` on all timestamp columns (2 tables) |
| `repository/factory_test.go` | `TestOpenDatabase_SQLite_TimestampRoundTrip` added |

### Pending (write-side fix — 17 sites across 6 files)

#### tenant.go — 7 sites

**Site 1: `Update` (line 115)**
```
- SET name = $2, consumer_type = $3, metadata = $4, updated_at = NOW()
+ SET name = $2, consumer_type = $3, metadata = $4, updated_at = $5
  Add: now := time.Now(), pass now as 5th arg
```

**Site 2: `UpdateStatus` → suspended (line 235)**
```
- SET status = $2, suspended_at = NOW(), updated_at = NOW()
+ SET status = $2, suspended_at = $3, updated_at = $3
  Add: now to args (3rd)
```

**Site 3: `UpdateStatus` → active (line 242)**
```
- SET status = $2, suspended_at = NULL, updated_at = NOW()
+ SET status = $2, suspended_at = NULL, updated_at = $3
  Add: now to args (3rd)
```

**Site 4: `UpdateStatus` → deprovisioning (line 249)**
```
- SET status = $2, updated_at = NOW()
+ SET status = $2, updated_at = $3
  Add: now to args (3rd)
```

**Site 5: `UpdateStatus` → deleted (line 256)**
```
- SET status = $2, deleted_at = NOW(), updated_at = NOW()
+ SET status = $2, deleted_at = $3, updated_at = $3
  Add: now to args (3rd)
```

**Site 6: `SetDeprovisionAt` (line 284)**
```
- SET deprovision_at = $2, updated_at = NOW()
+ SET deprovision_at = $2, updated_at = $3
  Add: now := time.Now(), pass now as 3rd arg
```

**Site 7: `GetTenantsForDeletion` (line 312) — WHERE clause**
```
- AND deprovision_at <= NOW()
+ AND deprovision_at <= $1
  Add: now := time.Now(), pass now as 1st arg to QueryContext
```

#### key.go — 3 sites

**Site 8: `Revoke` (line 144)**
```
- SET is_active = false, revoked_at = NOW() WHERE key_id = $1
+ SET is_active = false, revoked_at = $2 WHERE key_id = $1
  Add: now := time.Now(), pass now as 2nd arg
```

**Site 9: `RevokeAllForTenant` (line 168)**
```
- SET is_active = false, revoked_at = NOW() WHERE tenant_id = $1
+ SET is_active = false, revoked_at = $2 WHERE tenant_id = $1
  Add: now := time.Now(), pass now as 2nd arg
```

**Site 10: `GetActiveKeys` (line 188) — WHERE clause**
```
- AND (expires_at IS NULL OR expires_at > NOW())
+ AND (expires_at IS NULL OR expires_at > $1)
  Add: now := time.Now(), pass now as 1st arg to QueryContext
```

#### channel_rules.go — 3 sites

**Site 11: `Create` (line 33)**
```
- VALUES ($1, $2, NOW(), NOW())
+ VALUES ($1, $2, $3, $3)
  Add: now := time.Now(), pass now as 3rd arg
```

**Site 12–13: `Update` (lines 95, 97) — upsert**
```
- VALUES ($1, $2, NOW(), NOW()) ON CONFLICT (tenant_id) DO UPDATE SET rules = $2, updated_at = NOW()
+ VALUES ($1, $2, $3, $3) ON CONFLICT (tenant_id) DO UPDATE SET rules = $2, updated_at = $3
  Add: now := time.Now(), pass now as 3rd arg
```

#### oidc.go — 2 sites

**Site 14: `Create` (line 28)**
```
- VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
+ VALUES ($1, $2, $3, $4, $5, $6, $6)
  Add: now := time.Now(), pass now as 6th arg
```

**Site 15: `Update` (line 128)**
```
- SET issuer_url = $2, jwks_url = $3, audience = $4, enabled = $5, updated_at = NOW()
+ SET issuer_url = $2, jwks_url = $3, audience = $4, enabled = $5, updated_at = $6
  Add: now := time.Now(), pass now as 6th arg
```

#### topic.go — 1 site

**Site 16: `MarkDeleted` (line 107)**
```
- SET deleted_at = NOW() WHERE tenant_id = $1 AND category = $2
+ SET deleted_at = $3 WHERE tenant_id = $1 AND category = $2
  Add: now := time.Now(), pass now as 3rd arg
```

#### quota.go — 1 site

**Site 17: `Update` (line 85)**
```
- SET ... updated_at = NOW() WHERE tenant_id = $1
+ SET ... updated_at = $7 WHERE tenant_id = $1
  Add: now := time.Now(), pass now as 7th arg
```

## Testing Strategy

1. **Existing tests** (`cd ws && go test ./...`): Must pass unchanged — PostgreSQL-compatible queries still work because both `$N` params and `NOW()` produce valid timestamps; the `$N` pattern is already used throughout.

2. **Existing SQLite timestamp round-trip test** (`TestOpenDatabase_SQLite_TimestampRoundTrip`): Already on this branch, validates read-side scanning.

3. **New verification**: After implementation, run `grep -r 'NOW()' ws/internal/provisioning/repository/*.go` to confirm zero remaining `NOW()` calls in repository Go files (SC-004).

4. **E2E verification**: Build, deploy, and run `task k8s:provision:create ENV=dev` — all 3 steps (tenant, topics, channel rules) must succeed against SQLite (SC-001).

## Files Modified

| File | Sites | Status |
|------|-------|--------|
| `ws/internal/provisioning/repository/tenant.go` | 7 | Pending |
| `ws/internal/provisioning/repository/key.go` | 3 | Pending |
| `ws/internal/provisioning/repository/channel_rules.go` | 3 | Pending |
| `ws/internal/provisioning/repository/oidc.go` | 2 | Pending |
| `ws/internal/provisioning/repository/topic.go` | 1 | Pending |
| `ws/internal/provisioning/repository/quota.go` | 1 | Pending |
| `ws/internal/provisioning/repository/factory.go` | — | Done (read-side) |
| `ws/internal/provisioning/repository/factory_test.go` | — | Done (read-side) |
| `migrations/sqlite/001_initial.sql` | — | Done (read-side) |
| `migrations/sqlite/002_oidc_channel_rules.sql` | — | Done (read-side) |

## Verification Checklist

- [ ] `grep -rn 'NOW()' ws/internal/provisioning/repository/*.go` returns zero matches
- [ ] `cd ws && go vet ./...` passes
- [ ] `cd ws && go test ./...` — all packages pass
- [ ] `task k8s:build ENV=dev && task k8s:deploy ENV=dev` succeeds
- [ ] Delete stale PVC, then `task k8s:provision:create ENV=dev` — all 3 steps succeed

## Notes

- `time` import already exists in all 6 affected files — no new imports needed.
- Migration triggers (`datetime('now')` in SQLite, `NOW()` in PostgreSQL) are NOT changed per FR-004 — they are backend-specific and already correct.
- PostgreSQL migrations are untouched — no NFR-002 impact.
- The `tenant.Create()`, `key.Create()`, `topic.Create()`, and `quota.Create()` methods already use Go-side `time.Now()` — this plan extends that pattern to all remaining methods.
