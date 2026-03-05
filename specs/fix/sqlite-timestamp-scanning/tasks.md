# Tasks: SQLite Timestamp Scanning Fix

**Branch**: `fix/sqlite-timestamp-scanning` | **Generated**: 2026-03-05

## Phase 1: Code (Schema Fix)

- [x] T001 [P] Change timestamp columns from `TEXT` to `DATETIME` in `ws/internal/provisioning/repository/migrations/sqlite/001_initial.sql` — tables: `tenant_keys` (created_at, expires_at, revoked_at), `tenant_categories` (created_at, deleted_at), `tenant_quotas` (updated_at), `provisioning_audit` (created_at). Leave `tenants` table as-is (already DATETIME). Preserve all `DEFAULT (datetime('now'))` expressions. Do not touch triggers or indexes.

- [x] T002 [P] Change timestamp columns from `TEXT` to `DATETIME` in `ws/internal/provisioning/repository/migrations/sqlite/002_oidc_channel_rules.sql` — tables: `tenant_oidc_config` (created_at, updated_at), `tenant_channel_rules` (created_at, updated_at). Preserve all `DEFAULT (datetime('now'))` expressions and triggers.

## Phase 2: Testing

- [x] T003 Add SQLite timestamp round-trip test in `ws/internal/provisioning/repository/factory_test.go`. The test must: (1) open a SQLite DB via `OpenDatabase` with `AutoMigrate: true`, (2) insert a tenant row using `INSERT INTO tenants` with `datetime('now')` defaults, (3) scan the row back and assert `created_at`/`updated_at` are valid `time.Time` (not zero), (4) verify nullable timestamp `suspended_at` scans as nil when NULL, (5) update `suspended_at` to a value and verify it scans as a valid `time.Time`, (6) insert a `tenant_keys` row with `datetime('now')` default and NULL `expires_at`/`revoked_at`, scan it back, and assert `created_at` is valid `time.Time` and nullable fields scan as nil. Depends on: T001, T002.

## Phase 3: Verify & Deploy

- [x] T004 Run repository tests: `cd ws && go test ./internal/provisioning/repository/...` — all tests must pass including the new timestamp test (SC-004). Depends on: T001, T002, T003.

- [x] T005 Run full test suite: `cd ws && go test ./...` — confirms no regressions in PostgreSQL path or other packages (SC-003). Depends on: T004.

- [ ] T006 Build and deploy to dev: `task k8s:build ENV=dev`, then delete stale SQLite PVC `kubectl delete pvc -n odin-ws-dev -l app.kubernetes.io/name=provisioning` (if no PVC matches this label, manually delete the provisioning pod to force fresh DB creation), then `task k8s:deploy ENV=dev`. Depends on: T005.

- [ ] T007 Verify end-to-end: run `task k8s:provision:create ENV=dev` and confirm all 3 steps succeed (tenant, topics, channel rules) (SC-001). Check provisioning logs for no scan errors. Check ws-server logs for WatchTopics stream connected (SC-002). Depends on: T006.

## Summary

| Phase | Tasks | Parallel |
|-------|-------|----------|
| Code  | T001, T002 | Yes (different files) |
| Testing | T003 | — |
| Verify & Deploy | T004–T007 | Sequential |
| **Total** | **7** | |

## Dependencies

```
T001 ──┐
       ├──→ T003 ──→ T004 ──→ T005 ──→ T006 ──→ T007
T002 ──┘
```

## Notes

- `factory.go` DSN change (FR-001) is already applied — no task needed.
- `tenants` table in `001_initial.sql` already has DATETIME — no task needed.
- **Prerequisite**: T001 and T002 assume the `factory.go` DSN change and `tenants` table DATETIME change are already present on the working branch. If the branch is reset, re-apply these first: DSN in `factory.go:79`, tenants columns in `001_initial.sql:16-20`.
- PostgreSQL migrations are untouched — no task needed (NFR-002).
- No Phase 1 (Config) or Phase 3 (Infrastructure) tasks — this is a pure schema + test fix.
