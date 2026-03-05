# Implementation Plan: SQLite Timestamp Scanning Fix

**Branch**: `fix/sqlite-timestamp-scanning` | **Date**: 2026-03-05 | **Spec**: specs/fix/sqlite-timestamp-scanning/spec.md

## Summary

Fix SQLite timestamp scanning failures by configuring the `modernc.org/sqlite` driver DSN with time-parsing parameters and changing all SQLite migration timestamp columns from `TEXT` to `DATETIME` so the driver auto-parses them into `time.Time`.

## Technical Context

**Language**: Go 1.22+
**Services**: provisioning (only service with a database)
**Infrastructure**: Kubernetes (GKE Standard), Helm, Terraform
**Storage**: SQLite (dev/minimal), PostgreSQL (production)
**Driver**: `modernc.org/sqlite` (pure Go, no CGO)
**Build/Deploy**: Docker, Taskfile, Artifact Registry

## Constitution Check

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Configuration | PASS | DSN parameters are part of driver config, not magic strings |
| II. Defense in Depth | N/A | No input validation changes |
| III. Error Handling | PASS | No new error paths — fix removes existing errors |
| IV. Graceful Degradation | N/A | No optional dependencies changed |
| V. Structured Logging | N/A | No logging changes |
| VI. Observability | N/A | No metrics changes |
| VII. Concurrency | N/A | No concurrent code changes |
| VIII. Testing | PASS | New SQLite-specific timestamp tests required |
| IX. Security | N/A | No security changes |
| X. Shared Code | PASS | Fix is at driver/schema level — no repo code changes (FR-004) |
| NFR-002 | PASS | PostgreSQL migrations and driver config are untouched |

## Phase 0 — Research

### How `_texttotime` and `_time_format` work

The `modernc.org/sqlite` driver supports two DSN parameters:

1. **`_time_format=sqlite`** — Controls how Go `time.Time` values are **written** to SQLite. With `sqlite`, it writes as `"YYYY-MM-DD HH:MM:SS±HH:MM"` format, compatible with SQLite's `datetime()` function output.

2. **`_texttotime=1`** — Enables automatic **scanning** of TEXT-stored timestamps back to `time.Time`, but **only** for columns declared with type affinity `DATETIME`, `DATE`, or `TIMESTAMP`. Columns declared as `TEXT` are not auto-parsed, even if they contain valid datetime strings.

This is the root cause: our SQLite migrations declared timestamp columns as `TEXT` in 4 of 7 tables. The `tenants` table already used `DATETIME` (fixed earlier in this session), but `tenant_keys`, `tenant_categories`, `tenant_quotas`, and `provisioning_audit` (in `001_initial.sql`) plus `tenant_oidc_config` and `tenant_channel_rules` (in `002_oidc_channel_rules.sql`) still use `TEXT`.

### Impact on existing behavior

- SQLite `TEXT` affinity and `DATETIME` affinity are identical at the storage level — SQLite uses TEXT storage for both. The `DEFAULT (datetime('now'))` expressions and trigger `datetime('now')` calls work identically with either column type declaration.
- PostgreSQL migrations are completely separate files and are not affected.
- No existing SQLite databases need to be preserved (confirmed in spec clarifications).

## Phase 1 — Design

### Approach: Schema-level fix (2 changes)

The fix has two parts, both already partially applied:

1. **DSN parameters** (factory.go) — Already done. Adds `_time_format=sqlite&_texttotime=1` to the SQLite DSN.
2. **Column type declarations** (SQLite migrations) — Change remaining `TEXT` timestamp columns to `DATETIME` in both migration files.

No repository scan code changes needed (FR-004). The driver handles the parsing transparently when column types are declared correctly.

### File Changes

#### 1. `ws/internal/provisioning/repository/factory.go` — ALREADY DONE
- DSN: `file:%s?_time_format=sqlite&_texttotime=1`
- No further changes needed.

#### 2. `ws/internal/provisioning/repository/migrations/sqlite/001_initial.sql`
Change `TEXT` → `DATETIME` for timestamp columns in 4 tables:

| Table | Columns to change |
|-------|-------------------|
| `tenant_keys` | `created_at`, `expires_at`, `revoked_at` |
| `tenant_categories` | `created_at`, `deleted_at` |
| `tenant_quotas` | `updated_at` |
| `provisioning_audit` | `created_at` |

`tenants` table is already done (changed earlier in this session).

Defaults (`datetime('now')`) and triggers remain unchanged.

#### 3. `ws/internal/provisioning/repository/migrations/sqlite/002_oidc_channel_rules.sql`
Change `TEXT` → `DATETIME` for timestamp columns in 2 tables:

| Table | Columns to change |
|-------|-------------------|
| `tenant_oidc_config` | `created_at`, `updated_at` |
| `tenant_channel_rules` | `created_at`, `updated_at` |

Triggers remain unchanged.

#### 4. `ws/internal/provisioning/repository/factory_test.go` — ADD timestamp scan tests
Add a test that verifies the full round-trip: create a SQLite database with migrations → insert a row with timestamps → scan it back and assert `time.Time` values are returned correctly. Covers:

- Non-nullable timestamps (`created_at`, `updated_at`)
- Nullable timestamps (`suspended_at` as NULL → nil, then as non-NULL → valid time)
- Timestamps written by `datetime('now')` defaults

This test uses the real SQLite driver + migrations (integration-style) to verify the DSN + schema fix works end-to-end.

#### 5. PostgreSQL migrations — NO CHANGES
`migrations/postgres/001_initial.sql` and `002_oidc_channel_rules.sql` already use `TIMESTAMPTZ` — no changes needed.

## Verification Steps

1. **Unit tests**: `cd ws && go test ./internal/provisioning/repository/...`
2. **Full test suite**: `cd ws && go test ./...` (confirms PostgreSQL path is unaffected — SC-003)
3. **Build + deploy**: `task k8s:build ENV=dev && task k8s:deploy ENV=dev`
4. **Delete stale SQLite PVC**: `kubectl delete pvc -n odin-ws-dev -l app.kubernetes.io/name=provisioning` (force fresh DB)
5. **Provision**: `task k8s:provision:create ENV=dev` (SC-001)
6. **Check logs**: `kubectl logs -n odin-ws-dev -l app.kubernetes.io/name=provisioning --tail=50` — no scan errors
7. **WatchTopics**: `kubectl logs -n odin-ws-dev -l app.kubernetes.io/name=ws-server --tail=50` — stream connects (SC-002)

## Risk Assessment

- **Risk**: LOW — Changes are limited to SQLite column type declarations (storage-identical) and one DSN parameter that was already applied.
- **Blast radius**: SQLite backend only. PostgreSQL path completely untouched.
- **Rollback**: Delete SQLite PVC and redeploy previous image.
