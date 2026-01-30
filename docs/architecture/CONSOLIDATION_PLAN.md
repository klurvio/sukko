# Plan: Consolidate Shared Packages

## Summary

1. Move single-service packages from `internal/shared/` into their respective service directories
2. Move `pkg/` packages into `internal/shared/` (they're not public APIs, so `internal/` enforces privacy)

## Analysis Results

### Packages to KEEP in `internal/shared/` (used by 2+ services):

| Package | Used By | Reason |
|---------|---------|--------|
| `auth` | gateway, provisioning | Auth middleware and JWT verification |
| `kafka` | server, provisioning | Kafka client utilities |
| `platform` | server, gateway, provisioning | Config structs for all services |
| `testutil` | server, provisioning | Test fixtures and mocks |
| `types` | server, provisioning, internal packages | Foundational type definitions |
| `version` | server, gateway, provisioning | Build-time version info via ldflags (now integrated) |

### Packages to MOVE from `pkg/` to `internal/shared/`:

| Package | Purpose |
|---------|---------|
| `pkg/alerting` | Alert notifications (Slack, console) |
| `pkg/audit` | Audit logging |
| `pkg/logging` | Logger utilities |
| `pkg/metrics` | Prometheus metrics helpers |

**Reason:** These are not public APIs. Moving to `internal/` enforces privacy at compiler level.

### Packages to MOVE from `internal/shared/` to service directories (single-service only):

| Package | Used By | Move To |
|---------|---------|---------|
| `broadcast` | server | `internal/server/broadcast/` |
| `limits` | server | `internal/server/limits/` |
| `messaging` | server | `internal/server/messaging/` |

## Implementation Steps

### Step 1: Move `pkg/` packages to `internal/shared/`

Move each package and update all imports:

| Source | Destination |
|--------|-------------|
| `pkg/alerting/` | `internal/shared/alerting/` |
| `pkg/audit/` | `internal/shared/audit/` |
| `pkg/logging/` | `internal/shared/logging/` |
| `pkg/metrics/` | `internal/shared/metrics/` |

**Import path changes:**
```go
// Before
"github.com/Toniq-Labs/odin-ws/pkg/alerting"
// After
"github.com/Toniq-Labs/odin-ws/internal/shared/alerting"
```

After moving, delete the empty `pkg/` directory.

### Step 2: Move `broadcast` package
- **Source:** `internal/shared/broadcast/`
- **Destination:** `internal/server/broadcast/`
- **Files:** `bus.go`, `bus_test.go`, `kafka_bus.go`, `kafka_bus_test.go`
- **Update imports in:**
  - `internal/server/orchestration/kafka_pool.go`
  - `internal/server/orchestration/kafka_pool_test.go`
  - `internal/server/orchestration/multitenant_pool.go`
  - `internal/server/orchestration/multitenant_pool_test.go`
  - `internal/server/orchestration/shard.go`
  - `internal/server/orchestration/shard_test.go`

### Step 3: Move `limits` package
- **Source:** `internal/shared/limits/`
- **Destination:** `internal/server/limits/`
- **Files:** `resource_guard.go`, `resource_guard_test.go`, `token_bucket.go`
- **Update imports in:**
  - `internal/server/server.go`
  - `internal/server/client_lifecycle_test.go`

### Step 4: Move `messaging` package
- **Source:** `internal/shared/messaging/`
- **Destination:** `internal/server/messaging/`
- **Files:** `types.go`
- **Update imports in:**
  - `internal/server/broadcast.go`
  - `internal/server/connection.go`
  - `internal/server/handlers_message.go`
  - `internal/server/handlers_message_test.go`
  - `internal/server/pump.go`
  - `internal/server/pump_test.go`

### Step 5: Clean up empty directories
- Delete empty `pkg/` directory
- Delete empty `internal/shared/broadcast/`, `internal/shared/limits/`, `internal/shared/messaging/` directories

## Import Path Changes Summary

**pkg/ → internal/shared/ (Step 1):**
```go
"github.com/Toniq-Labs/odin-ws/pkg/<package>"
→
"github.com/Toniq-Labs/odin-ws/internal/shared/<package>"
```
For packages: `alerting`, `audit`, `logging`, `metrics`

**internal/shared/ → internal/server/ (Steps 2-4):**
```go
"github.com/Toniq-Labs/odin-ws/internal/shared/<package>"
→
"github.com/Toniq-Labs/odin-ws/internal/server/<package>"
```
For packages: `broadcast`, `limits`, `messaging`

## Verification

1. Run `go build ./...` from workspace root to verify all imports resolve
2. Run `go test ./...` to verify tests pass
3. Verify no remaining references to old import paths:
   ```bash
   grep -r "shared/broadcast\|shared/limits\|shared/messaging" internal/
   grep -r "odin-ws/pkg/" .
   ```

## Risks & Mitigations

- **Risk:** Breaking imports during move
  - **Mitigation:** Move one package at a time, verify build after each

## Final Directory Structure

After consolidation:
```
internal/
├── shared/
│   ├── alerting/       # From pkg/ - alert notifications
│   ├── audit/          # From pkg/ - audit logging
│   ├── auth/           # Auth middleware and JWT
│   ├── kafka/          # Kafka client utilities
│   ├── logging/        # From pkg/ - logger utilities
│   ├── metrics/        # From pkg/ - Prometheus helpers
│   ├── platform/       # Config structs for all services
│   ├── testutil/       # Test fixtures and mocks
│   ├── types/          # Foundational type definitions
│   └── version/        # Build-time version info
├── server/
│   ├── broadcast/      # Moved from shared/ (server-only)
│   ├── limits/         # Moved from shared/ (server-only)
│   ├── messaging/      # Moved from shared/ (server-only)
│   └── ...
├── gateway/
│   └── ...
└── provisioning/
    └── ...

pkg/  # DELETED (empty after migration)
```
