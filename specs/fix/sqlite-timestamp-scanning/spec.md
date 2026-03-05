# Feature Specification: SQLite Timestamp Scanning Fix

**Branch**: `fix/sqlite-timestamp-scanning`
**Created**: 2026-03-05
**Status**: Draft

## Context

The provisioning service supports two database backends: PostgreSQL and SQLite. When running with SQLite (the default for dev/minimal deployments), all timestamp operations fail at runtime with:

```
sql: Scan error on column index 5, name "created_at": unsupported Scan, storing driver.Value type string into type *time.Time
```

This blocks all provisioning operations after tenant creation: topic creation, channel rule assignment, tenant listing, and the gRPC WatchTopics stream used by ws-server. The root cause is a mismatch between how SQLite stores timestamps (TEXT strings via `datetime('now')`) and how the Go repository layer scans them (`time.Time` / `sql.NullTime`). PostgreSQL handles this natively; SQLite with the `modernc.org/sqlite` driver does not.

## User Scenarios

### Scenario 1 - Provisioning with SQLite backend (Priority: P1)
An operator deploys the Odin stack with the default SQLite configuration. They create a tenant via the REST API, then create topics and set channel rules. The ws-server connects via gRPC and watches for topic changes.

**Acceptance Criteria**:
1. **Given** provisioning is running with SQLite, **When** a tenant is created and then queried, **Then** all timestamp fields (`created_at`, `updated_at`, nullable timestamps) are correctly returned as `time.Time` values.
2. **Given** a tenant exists in SQLite, **When** topics are created for that tenant, **Then** the operation succeeds and topic timestamps are correctly scanned.
3. **Given** a tenant exists in SQLite, **When** channel rules are set, **Then** the operation succeeds and rule timestamps are correctly scanned.
4. **Given** provisioning is running with SQLite, **When** ws-server calls the WatchTopics gRPC stream, **Then** the stream connects and delivers topic data without scan errors.

### Scenario 2 - PostgreSQL backend unaffected (Priority: P1)
An operator deploys with PostgreSQL. All existing behavior continues to work identically.

**Acceptance Criteria**:
1. **Given** provisioning is running with PostgreSQL, **When** any CRUD operation is performed, **Then** timestamp scanning works exactly as before with no behavioral change.

### Scenario 3 - Nullable timestamp handling (Priority: P1)
Timestamps that can be NULL (`suspended_at`, `deprovision_at`, `deleted_at`, `expires_at`, `revoked_at`) must scan correctly from SQLite as both NULL and non-NULL values.

**Acceptance Criteria**:
1. **Given** a tenant with no `suspended_at` value in SQLite, **When** the tenant is queried, **Then** `SuspendedAt` is nil (not a zero time).
2. **Given** a tenant that has been suspended in SQLite, **When** the tenant is queried, **Then** `SuspendedAt` contains the correct timestamp.

### Edge Cases
- What happens when a SQLite database created with TEXT-typed columns is opened with the updated driver configuration? (Existing data must still scan correctly.)
- What happens when timestamps are written by SQLite triggers (`datetime('now')`) vs. written by Go code? Both formats must be scannable.

## Requirements

### Functional Requirements
- **FR-001**: The `modernc.org/sqlite` driver connection MUST be configured with DSN parameters that enable automatic time parsing for timestamp columns.
- **FR-002**: SQLite migration schemas MUST declare timestamp columns with a type recognized by the driver's time parsing (e.g., `DATETIME` instead of `TEXT`), while preserving `datetime('now')` defaults and trigger behavior.
- **FR-003**: All repository scan operations (tenant, key, topic/category, quota, audit, OIDC config, channel rules) MUST succeed with both SQLite and PostgreSQL backends.
- **FR-004**: The fix MUST NOT require changes to the shared repository scan code (`tenant.go`, `key.go`, `topic.go`, etc.) — the solution should be at the driver/schema level so both backends work with the same Go scan logic.
- **FR-005**: `sql.NullTime` usage for nullable timestamps MUST continue to work correctly with SQLite.

### Non-Functional Requirements
- **NFR-001**: No performance regression — timestamp parsing overhead must be negligible.
- **NFR-002**: No breaking change to the PostgreSQL migration schemas or PostgreSQL driver configuration.

### Key Entities (affected tables)
- **tenants**: `created_at`, `updated_at`, `suspended_at`, `deprovision_at`, `deleted_at`
- **tenant_keys**: `created_at`, `expires_at`, `revoked_at`
- **tenant_categories**: `created_at`, `deleted_at`
- **tenant_quotas**: `updated_at`
- **provisioning_audit**: `created_at`
- **tenant_oidc_config**: `created_at`, `updated_at`
- **tenant_channel_rules**: `created_at`, `updated_at`

## Success Criteria
- **SC-001**: `task k8s:provision:create ENV=dev` completes with all three steps (tenant, topics, channel rules) succeeding against a SQLite-backed provisioning service.
- **SC-002**: The ws-server WatchTopics gRPC stream connects without scan errors in provisioning logs.
- **SC-003**: All existing provisioning unit tests pass with no modifications (confirming PostgreSQL path is unaffected).
- **SC-004**: New or updated tests verify timestamp scanning works for the SQLite backend.

## Clarifications
- Q: Are there any existing SQLite databases that need to survive this change? → A: No. All current deployments use PostgreSQL. The dev SQLite was just created and can be recreated. Only the initial migration needs fixing.

## Out of Scope
- Migrating existing TEXT-typed SQLite databases to DATETIME columns (fresh databases only; existing deployments are all PostgreSQL).
- Adding timezone-aware timestamps (SQLite `datetime('now')` returns UTC, which matches current behavior).
- Changing the repository layer to use separate SQLite/PostgreSQL implementations.
