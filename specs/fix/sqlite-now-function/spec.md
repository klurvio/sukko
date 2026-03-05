# Feature Specification: SQLite NOW() Function Compatibility Fix

**Branch**: `fix/sqlite-now-function`
**Created**: 2026-03-05
**Status**: Draft

## Context

The provisioning service repository layer uses `NOW()` in SQL queries for timestamp assignment (INSERT and UPDATE operations). `NOW()` is a PostgreSQL-only function — SQLite has no built-in `NOW()` and fails at runtime with:

```
SQL logic error: no such function: NOW (1)
```

This blocks all mutating operations on the SQLite backend: tenant updates, status changes, key revocation, topic deletion, quota updates, OIDC config, and channel rule assignment. Only the initial tenant INSERT works because it uses Go-side `time.Now()` for timestamps.

This branch also includes the read-side fix (formerly `fix/sqlite-timestamp-scanning`): configuring the `modernc.org/sqlite` driver DSN with `_time_format=sqlite&_texttotime=1` and changing all SQLite migration timestamp columns from `TEXT` to `DATETIME`. Together, the read-side and write-side fixes make the provisioning service fully functional with SQLite.

## User Scenarios

### Scenario 1 - All provisioning write operations with SQLite (Priority: P1)
An operator deploys with the default SQLite backend. They create a tenant, then perform all downstream operations: create topics, set channel rules, configure OIDC, manage keys, and update quotas.

**Acceptance Criteria**:
1. **Given** provisioning is running with SQLite, **When** channel rules are set for a tenant, **Then** the operation succeeds and timestamps are recorded.
2. **Given** provisioning is running with SQLite, **When** a tenant is updated (name, status, suspension, deprovisioning), **Then** all timestamp fields are correctly set.
3. **Given** provisioning is running with SQLite, **When** keys are revoked or topics are deleted, **Then** the revoked_at/deleted_at timestamps are correctly set.
4. **Given** provisioning is running with SQLite, **When** OIDC config is created or updated, **Then** timestamps are correctly set.
5. **Given** provisioning is running with SQLite, **When** quotas are updated, **Then** the updated_at timestamp is correctly set.

### Scenario 2 - PostgreSQL backend unaffected (Priority: P1)
All existing PostgreSQL behavior continues to work identically.

**Acceptance Criteria**:
1. **Given** provisioning is running with PostgreSQL, **When** any CRUD operation is performed, **Then** timestamp behavior works exactly as before with no change.

### Scenario 3 - Timestamp comparison in WHERE clauses (Priority: P1)
Some queries compare stored timestamps against the current time (e.g., key expiry checks, deprovision deadline queries). These must work with both backends.

**Acceptance Criteria**:
1. **Given** a key with an expiry time in the past, **When** active keys are queried, **Then** the expired key is excluded regardless of database backend.
2. **Given** a tenant past its deprovision deadline, **When** tenants ready for deletion are queried, **Then** the tenant is returned regardless of database backend.

### Edge Cases
- What happens when Go's `time.Now()` has sub-second precision but SQLite's `datetime('now')` does not? (Go-side timestamps should be used consistently so precision is uniform.)
- What happens when the Go application clock and the database clock differ? (Using Go-side timestamps eliminates this concern entirely since all timestamps come from one source.)

## Requirements

### Functional Requirements
- **FR-001**: All SQL queries that currently use `NOW()` MUST be changed to accept timestamps as Go-side parameters instead of relying on database-specific SQL functions.
- **FR-002**: The replacement MUST work identically with both SQLite and PostgreSQL — no database-specific branching in query strings.
- **FR-003**: WHERE clause comparisons against current time (e.g., `expires_at > NOW()`, `deprovision_at <= NOW()`) MUST be changed to use parameterized Go-side time values.
- **FR-004**: Existing trigger-based timestamps (`datetime('now')` in SQLite, `NOW()` in PostgreSQL triggers) MUST NOT be changed — triggers are migration-level and already use the correct backend-specific syntax.

### Non-Functional Requirements
- **NFR-001**: No performance regression — passing `time.Now()` as a parameter is equivalent to calling `NOW()` in the database.
- **NFR-002**: No breaking change to the PostgreSQL migration schemas or trigger behavior.

### Key Entities (affected query sites)
- **tenant.go**: 7 sites — Update, UpdateStatus (4 status transitions), SetDeprovisionAt, GetTenantsForDeletion
- **key.go**: 3 sites — RevokeKey, RevokeAllKeys, GetActiveKey
- **channel_rules.go**: 3 sites — Set (INSERT + UPSERT)
- **oidc.go**: 2 sites — Create, Update
- **topic.go**: 1 site — SoftDeleteByTenant
- **quota.go**: 1 site — CreateOrUpdate

## Success Criteria
- **SC-001**: `task k8s:provision:create ENV=dev` completes all three steps (tenant, topics, channel rules) against SQLite.
- **SC-002**: All existing provisioning unit tests pass with no modification (PostgreSQL path unaffected).
- **SC-003**: New or updated tests verify that the parameterized timestamp queries work for the SQLite backend.
- **SC-004**: No `NOW()` calls remain in any `.go` file under `ws/internal/provisioning/repository/`.

## Clarifications
- Q: Should we add a dialect abstraction layer, or follow the Go-side `time.Now()` parameter pattern already used by `tenant.Create()`? → A: Use Go-side `time.Now()` parameters everywhere. Both `lib/pq` (PostgreSQL) and `modernc.org/sqlite` (with `_time_format=sqlite`) correctly serialize Go `time.Time` values. This follows the proven pattern in the codebase with zero new abstractions.
- Q: Should the read-side fix (`fix/sqlite-timestamp-scanning`) be kept as a separate branch? → A: No. Merged into this branch. Neither fix works alone — both are required for SQLite to function. Single branch, single PR, single deploy.

## Out of Scope
- Changing SQLite or PostgreSQL migration triggers (they already use the correct backend-specific syntax).
- Adding a database abstraction layer or query builder.
- Changing the existing `tenant.Create()` method (it already uses Go-side `time.Now()`).
