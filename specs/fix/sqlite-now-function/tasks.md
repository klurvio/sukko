# Tasks: SQLite NOW() Function Compatibility Fix

**Branch**: `fix/sqlite-now-function` | **Generated**: 2026-03-05

## Phase 1: Code (Write-Side Fix — Replace NOW() with Go-side time.Now())

- [x] T001 [P] Replace 7 `NOW()` calls with `$N` parameters in `ws/internal/provisioning/repository/tenant.go`:
  - `Update` (line 115): `updated_at = NOW()` → `updated_at = $5`, add `now := time.Now()`, pass as 5th arg.
  - `UpdateStatus` (lines 235, 242, 249, 256): Add `now := time.Now()` before the switch. For each case, replace `NOW()` with `$3` and append `now` to args. Suspended: `suspended_at = $3, updated_at = $3`. Active: `updated_at = $3`. Deprovisioning: `updated_at = $3`. Deleted: `deleted_at = $3, updated_at = $3`.
  - `SetDeprovisionAt` (line 284): `updated_at = NOW()` → `updated_at = $3`, add `now := time.Now()`, pass as 3rd arg.
  - `GetTenantsForDeletion` (line 312): `deprovision_at <= NOW()` → `deprovision_at <= $1`, add `now := time.Now()`, pass as 1st arg to `QueryContext`.

- [x] T002 [P] Replace 3 `NOW()` calls with `$N` parameters in `ws/internal/provisioning/repository/key.go`:
  - `Revoke` (line 144): `revoked_at = NOW()` → `revoked_at = $2`, add `now := time.Now()`, pass as 2nd arg.
  - `RevokeAllForTenant` (line 168): `revoked_at = NOW()` → `revoked_at = $2`, add `now := time.Now()`, pass as 2nd arg.
  - `GetActiveKeys` (line 188): `expires_at > NOW()` → `expires_at > $1`, add `now := time.Now()`, pass as 1st arg to `QueryContext`.

- [x] T003 [P] Replace 3 `NOW()` calls with `$N` parameters in `ws/internal/provisioning/repository/channel_rules.go`:
  - `Create` (line 33): `NOW(), NOW()` → `$3, $3`, add `now := time.Now()`, pass as 3rd arg.
  - `Update` (lines 95, 97): `NOW(), NOW()` → `$3, $3` in INSERT, `updated_at = NOW()` → `updated_at = $3` in DO UPDATE, add `now := time.Now()`, pass as 3rd arg.

- [x] T004 [P] Replace 2 `NOW()` calls with `$N` parameters in `ws/internal/provisioning/repository/oidc.go`:
  - `Create` (line 28): `NOW(), NOW()` → `$6, $6`, add `now := time.Now()`, pass as 6th arg.
  - `Update` (line 128): `updated_at = NOW()` → `updated_at = $6`, add `now := time.Now()`, pass as 6th arg.

- [x] T005 [P] Replace 1 `NOW()` call with `$N` parameter in `ws/internal/provisioning/repository/topic.go`:
  - `MarkDeleted` (line 107): `deleted_at = NOW()` → `deleted_at = $3`, add `now := time.Now()`, pass as 3rd arg.

- [x] T006 [P] Replace 1 `NOW()` call with `$N` parameter in `ws/internal/provisioning/repository/quota.go`:
  - `Update` (line 85): `updated_at = NOW()` → `updated_at = $7`, add `now := time.Now()`, pass as 7th arg.

## Phase 2: Testing

- [x] T007 Add write-side SQLite test in `ws/internal/provisioning/repository/factory_test.go`. The test (`TestOpenDatabase_SQLite_RepositoryWriteOps`) must: (1) open a SQLite DB via `OpenDatabase` with `AutoMigrate: true`, (2) create repository instances (`NewPostgresTenantRepository`, `NewPostgresKeyRepository`, `NewPostgresChannelRulesRepository`), (3) create a tenant via `TenantRepository.Create()`, (4) update the tenant via `TenantRepository.Update()` and verify no error (exercises NOW()→$5 fix), (5) create a key via `KeyRepository.Create()`, then revoke it via `KeyRepository.Revoke()` and verify no error (exercises NOW()→$2 fix), (6) create channel rules via `ChannelRulesRepository.Create()` and verify no error (exercises NOW()→$3 fix), (7) update channel rules via `ChannelRulesRepository.Update()` and verify no error (exercises upsert NOW()→$3 fix). After each write, read the record back and assert timestamps are non-zero `time.Time`. Depends on: T001–T006.

## Phase 3: Verification

- [x] T008 Verify zero `NOW()` calls remain: run `grep -rn 'NOW()' ws/internal/provisioning/repository/*.go` — must return no matches (SC-004). Depends on: T007.

- [x] T009 Run `cd ws && go vet ./...` — must pass with no errors. Depends on: T008.

- [x] T010 Run `cd ws && go test ./...` — all packages must pass, including both `TestOpenDatabase_SQLite_TimestampRoundTrip` and `TestOpenDatabase_SQLite_RepositoryWriteOps` (SC-002, SC-003). Depends on: T009.

## Phase 4: Deploy & Verify

- [ ] T011 Build and deploy: `task k8s:build ENV=dev && task k8s:deploy ENV=dev`. Depends on: T010.

- [ ] T012 Delete stale SQLite PVC to force fresh DB creation: `kubectl delete pod -n odin-ws-dev -l app.kubernetes.io/name=provisioning` then `kubectl delete pvc -n odin-ws-dev odin-provisioning-data` (if PVC exists; if not, deleting the pod alone is sufficient). Wait for the new pod to become Ready. Depends on: T011.

- [ ] T013 Verify end-to-end: run `task k8s:provision:create ENV=dev` — all 3 steps must succeed (tenant, topics, channel rules) (SC-001). Check provisioning logs for no `NOW()` or scan errors. Depends on: T012.

## Summary

| Phase | Tasks | Parallel |
|-------|-------|----------|
| Code  | T001–T006 | Yes (all 6 files are independent) |
| Testing | T007 | — |
| Verification | T008–T010 | Sequential |
| Deploy & Verify | T011–T013 | Sequential |
| **Total** | **13** | |

## Dependencies

```
T001 ──┐
T002 ──┤
T003 ──┤
T004 ──┼──→ T007 ──→ T008 ──→ T009 ──→ T010 ──→ T011 ──→ T012 ──→ T013
T005 ──┤
T006 ──┘
```

## Notes

- The read-side fix (DSN params, TEXT→DATETIME migrations, timestamp test) is already on this branch — no tasks needed.
- All 6 code files already import `time` — no new imports required.
- Migration triggers (`datetime('now')` in SQLite, `NOW()` in PostgreSQL) are NOT changed (FR-004).
- PostgreSQL migrations are untouched (NFR-002).
- `tenant.Create()`, `key.Create()`, `topic.Create()`, `quota.Create()` already use Go-side `time.Now()` — no changes needed for those methods.
