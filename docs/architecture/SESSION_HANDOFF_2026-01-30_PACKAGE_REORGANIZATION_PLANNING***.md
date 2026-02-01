# Session Handoff: 2026-01-30 - Package Reorganization Planning

**Date:** 2026-01-30
**Duration:** ~2 hours
**Status:** Planning Complete - Ready for Implementation

## Session Goals

Analyze the `internal/monitoring` package structure and create a comprehensive plan to:
- Extract shared utilities into reusable packages
- Reorganize `internal/` for clear service separation
- Eliminate code duplication and metric pollution
- Standardize on consistent patterns (promauto)

## What Was Accomplished

### 1. Root Cause Analysis
- Investigated why `monitoring/metrics.go` uses manual `prometheus.MustRegister()` while gateway/provisioning use `promauto`
- **Finding:** No technical reason - just legacy code that was never refactored
- Both approaches panic on duplicate metrics (promauto uses MustRegister internally)

### 2. Package Structure Analysis
- Identified `internal/monitoring` as a "kitchen sink" package mixing:
  - Logging utilities (`logger.go`)
  - Audit logging (`audit_logger.go`)
  - Alerting (`alerting.go`)
  - System monitoring (`system_monitor.go`)
  - Server-specific metrics (`metrics.go` - 37 `ws_*` metrics)

### 3. Problem Identification
- **Metric pollution:** Gateway/provisioning import `monitoring` for `NewLogger()` but `init()` registers 37 unused `ws_*` metrics
- **Duplicate collection:** Three places collect system metrics (MetricsCollector, Server.collectMetrics, SystemMonitor)
- **Unclear organization:** `internal/` mixes service-specific and shared code
- **No alerting/audit in gateway/provisioning**

### 4. Comprehensive Plan Created
Created `ws/PLAN-extract-shared-packages.md` with 10 phases covering:
- Extract `pkg/logging`, `pkg/alerting`, `pkg/audit`
- Reorganize `internal/` with `shared/` subdirectory
- Consolidate server metrics into `internal/server/metrics/` directory (10 domain-specific files)
- Delete `internal/monitoring/` entirely
- Update CODING_GUIDELINES.md

## Key Decisions & Context

| Decision | Rationale |
|----------|-----------|
| Pattern A for directory organization | Minimal disruption, clear `internal/shared/` separation |
| `orchestration` is server-specific | Only used by server (load balancer, sharding) - stays in `internal/server/orchestration/` |
| Use `promauto` everywhere | Consistent pattern, eliminates 50-line `init()` blocks |
| `pkg/audit` depends on `pkg/alerting` | Decoupled - audit can optionally trigger alerts on WARNING+ events |
| Split metrics by domain | `connection.go`, `kafka.go`, etc. instead of one 600-line file |
| Add rate limiting to alerting | Prevent Slack spam (max 3 alerts per 5 min per unique message) |

## Technical Details

### Target Package Structure
```
internal/
├── server/
│   ├── metrics/           # 10 files by domain
│   ├── orchestration/     # Moved from internal/orchestration/
│   └── ...
├── gateway/
│   ├── metrics/
│   └── ...
├── provisioning/
│   └── api/metrics/
└── shared/                # NEW
    ├── auth/
    ├── kafka/
    ├── platform/
    ├── limits/
    ├── types/
    └── testutil/

pkg/
├── logging/               # NEW - from monitoring/logger.go
├── alerting/              # NEW - from monitoring/alerting.go
├── audit/                 # NEW - from monitoring/audit_logger.go
└── metrics/               # EXISTING - shared buckets/interfaces
```

### Server Metrics Files
```
internal/server/metrics/
├── connection.go      # ~80 lines
├── message.go         # ~40 lines
├── reliability.go     # ~100 lines
├── system.go          # ~150 lines (includes SystemMonitor)
├── kafka.go           # ~60 lines
├── capacity.go        # ~60 lines
├── errors.go          # ~50 lines
├── multitenant.go     # ~50 lines
├── collector.go       # ~120 lines (consolidated collection)
└── handler.go         # ~15 lines
```

## Issues & Solutions

| Issue | Solution |
|-------|----------|
| `monitoring` is a kitchen sink | Split into `pkg/logging`, `pkg/alerting`, `pkg/audit` + service-specific metrics |
| Duplicate metric collection | Consolidate into single `metrics/collector.go` |
| Unclear what's shared vs service-specific | Add `internal/shared/` subdirectory |
| CODING_GUIDELINES.md has legacy examples | Update as part of Phase 10 |

## Current State

- **Plan file:** `ws/PLAN-extract-shared-packages.md` (complete)
- **Code:** Unchanged - planning only
- **Ready for:** Phase 1 implementation (extract `pkg/logging`)

## Next Steps

### Immediate Priority
- Begin Phase 1: Extract `pkg/logging` from `internal/monitoring/logger.go`

### Implementation Order (from plan)
| Phase | Task | Effort |
|-------|------|--------|
| 1 | Extract `pkg/logging` | 3h |
| 2 | Extract `pkg/alerting` | 4h |
| 3 | Extract `pkg/audit` | 3h |
| 4 | Reorganize `internal/shared/` + move orchestration | 2.5h |
| 5 | Consolidate server metrics | 4h |
| 6 | Reorganize gateway metrics | 2h |
| 7 | Reorganize provisioning metrics | 2h |
| 8 | Delete `internal/monitoring/` | 30m |
| 9 | Update Helm charts | 1h |
| 10 | Update CODING_GUIDELINES.md | 2h |

**Total: ~24 hours**

### Future Considerations
- Consider adding PagerDuty alerter for CRITICAL alerts
- May want to add alert grouping (10 similar alerts → 1 summary)

## Files Modified

| File | Description |
|------|-------------|
| `ws/PLAN-extract-shared-packages.md` | Created comprehensive 10-phase reorganization plan |

## Commands for Next Session

```bash
# Start implementation
cd /Volumes/Dev/Codev/Toniq/odin-ws/ws

# Read the plan
cat PLAN-extract-shared-packages.md

# Phase 1: Create pkg/logging directory
mkdir -p pkg/logging

# After changes, verify builds
go build ./...

# Run tests
go test ./...
```

## Open Questions

None - plan is complete and ready for execution.

## Reference Files

- Plan: `/Volumes/Dev/Codev/Toniq/odin-ws/ws/PLAN-extract-shared-packages.md`
- Source (logging): `/Volumes/Dev/Codev/Toniq/odin-ws/ws/internal/monitoring/logger.go`
- Source (alerting): `/Volumes/Dev/Codev/Toniq/odin-ws/ws/internal/monitoring/alerting.go`
- Source (audit): `/Volumes/Dev/Codev/Toniq/odin-ws/ws/internal/monitoring/audit_logger.go`
- Source (metrics): `/Volumes/Dev/Codev/Toniq/odin-ws/ws/internal/monitoring/metrics.go`
- Source (system monitor): `/Volumes/Dev/Codev/Toniq/odin-ws/ws/internal/monitoring/system_monitor.go`
- Guidelines: `/Volumes/Dev/Codev/Toniq/odin-ws/docs/architecture/CODING_GUIDELINES.md`
