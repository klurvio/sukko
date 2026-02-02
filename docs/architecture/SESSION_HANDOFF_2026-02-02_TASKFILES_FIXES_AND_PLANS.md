# Session Handoff: 2026-02-02 - Taskfiles Fixes & Performance Plans

**Date:** 2026-02-02
**Duration:** ~2 hours
**Status:** In Progress

## Session Goals

1. Review PLAN_SERVER_PERFORMANCE_REVIEW.md and provide feedback
2. Investigate config constants location (shared vs server package)
3. Analyze health endpoint staleness for buffer stats caching
4. Create CPU monitoring improvements plan
5. Fix taskfile YAML parsing errors

## What Was Accomplished

### 1. Plan Reviews & Updates

**PLAN_SERVER_PERFORMANCE_REVIEW.md** - Updated with investigation findings:
- Changed config constants location from `server/config/` to `shared/config/timeouts.go`
- Added buffer stats cache implementation details (5s TTL is safe)
- Documented what to cache vs what must remain live
- Added list of files that will import from `shared/config/`

**Created PLAN_CPU_MONITORING_IMPROVEMENTS.md** - New plan for CPU monitoring:
- Analyzed current CPU calculation (cgroup-based, container-aware)
- Verified hysteresis mechanism is working correctly
- Identified gaps: silent cgroup fallback, 1s sampling, no throttling warnings
- Proposed phases: fail-fast cgroup detection, throttling warnings, faster sampling, adaptive rate limiting

### 2. Config Constants Investigation

**Finding:** Cross-package constants must go in `shared/config/`, NOT `server/config/`

| Constant | Packages Using It |
|----------|-------------------|
| 5 seconds | server/handlers_*.go, broadcast/valkey.go, **shared/alerting/slack.go** |
| 10 seconds | broadcast/valkey.go, metrics.go, gateway/multi_issuer_oidc.go |
| 30 seconds | server.go, pump.go, broadcast/valkey.go, provisioning/kafka/admin.go |

**Reason:** If constants live in `server/config/`, then `shared/alerting/slack.go` would need to import from `server/` — this breaks dependency direction.

### 3. Health Endpoint Analysis

**Finding:** 5-second buffer stats cache is safe.

Verified consumers of `/health` endpoint:
- K8s liveness/readiness probes - only check HTTP status code
- Load balancer - uses overall status, not buffer_saturation
- Prometheus - scrapes `/metrics`, not `/health`
- `loadtest/main.go` - only uses capacity, cpu, memory (NOT buffer_saturation)

### 4. Taskfile Fixes

Fixed YAML parsing errors caused by colons in echo statements and duplicate includes.

**Files fixed for colon issues** (wrapped in single quotes):
- `taskfiles/shared/version.yml` - lines 21-23, 36
- `taskfiles/k8s/Taskfile.yml` - lines 26-44
- `taskfiles/k8s/remote/Taskfile.yml` - lines 16-25
- `taskfiles/k8s/local/Taskfile.yml` - line 260
- `taskfiles/k8s/remote/standard.yml` - lines 296-306, 490-496

**Fixed duplicate include issue:**
- Root cause: `shared/build.yml` included multiple times with `build:` namespace
- Removed `build:` include from:
  - `taskfiles/k8s/common.yml`
  - `taskfiles/k8s/local/Taskfile.yml`
  - `taskfiles/k8s/remote/standard.yml`
- Renamed root `build:` task to `build-all:` with alias

**Cleaned up orphaned file:**
- Deleted `taskfiles/k8s/remote/common.yml` (not included anywhere)

### 5. AsyncAPI Fix (Partial)

Created missing `ws/asyncapi/channel/client_events.yaml` file that was referenced but didn't exist.

## Key Decisions & Context

| Decision | Rationale |
|----------|-----------|
| Use `shared/config/timeouts.go` | Prevents reverse dependency (shared → server) |
| 5s buffer stats cache is safe | Buffer samples are already 10s stale; no critical monitoring depends on it |
| Hysteresis mechanism is correct | 60/50% for rejection, 70/60% for Kafka pause - verified working |
| Remove duplicate `build:` includes | Task v3 sees same file included multiple times as duplicate tasks |

## Technical Details

### CPU Monitoring Architecture (Existing)

```
Container-Aware Measurement:
- Primary: cgroup v1/v2 (cgroup_cpu.go)
- Fallback: gopsutil (host CPU)
- Sampling: 1s CPU, 15s full metrics
- Singleton: SystemMonitor with sync.Once

Hysteresis State Machine:
  ACCEPTING ←→ REJECTING
  (CPU > 60% enters, CPU < 50% exits)

  RUNNING ←→ PAUSED (Kafka)
  (CPU > 70% enters, CPU < 60% exits)
```

### Taskfile Include Structure (After Fix)

```
Root Taskfile.yml
├── build: → shared/build.yml (ONLY place it's included)
├── test: → shared/test.yml
├── version: → shared/version.yml
├── provisioning: → provisioning.yml
├── k8s: → k8s/Taskfile.yml
│   ├── local: → k8s/local/Taskfile.yml (no build include)
│   ├── remote: → k8s/remote/Taskfile.yml
│   │   └── standard: → k8s/remote/standard.yml (no build include)
│   └── common: → k8s/common.yml (no build include)
└── terraform: → terraform/Taskfile.yml
```

## Issues & Solutions

| Issue | Solution |
|-------|----------|
| YAML colon parsing errors | Wrap echo statements with colons in single quotes |
| "Found multiple tasks (build:gateway)" | Remove redundant `build:` includes from sub-taskfiles |
| Missing client_events.yaml | Created the file with proper AsyncAPI format |

## Current State

**Working:**
- All taskfiles parse correctly
- `task --list` works
- `task k8s` shows help without errors
- Plan documents updated with investigation findings

**Pending:**
- `task asyncapi:bundle` - file created but command was interrupted before verification
- Plans not yet implemented (just documented)

## Next Steps

### Immediate Priority
- Test `task asyncapi:bundle` to verify the fix works
- Commit the taskfile fixes

### Near Term
- Implement Phase 1 of PLAN_CPU_MONITORING_IMPROVEMENTS.md:
  - Fail-fast on cgroup detection failure in containers
  - Add throttling event early warnings
- Implement Phase 3 of PLAN_SERVER_PERFORMANCE_REVIEW.md:
  - Create `ws/internal/shared/config/timeouts.go`

### Future Considerations
- Implement adaptive rate limiting based on CPU (Phase 4 of CPU plan)
- Add Prometheus gauges for hysteresis state
- Reduce CPU_POLL_INTERVAL from 1s to 500ms

## Files Modified

### Created
| File | Description |
|------|-------------|
| `docs/architecture/PLAN_CPU_MONITORING_IMPROVEMENTS.md` | New plan for CPU monitoring fixes |
| `ws/asyncapi/channel/client_events.yaml` | Missing AsyncAPI channel definition |

### Modified
| File | Description |
|------|-------------|
| `docs/architecture/PLAN_SERVER_PERFORMANCE_REVIEW*.md` | Added investigation findings, changed config location |
| `Taskfile.yml` | Renamed `build:` task to `build-all:` with alias |
| `taskfiles/shared/version.yml` | Fixed colon issues in echo statements |
| `taskfiles/k8s/Taskfile.yml` | Fixed colon issues |
| `taskfiles/k8s/remote/Taskfile.yml` | Fixed colon issues |
| `taskfiles/k8s/local/Taskfile.yml` | Fixed colon, removed `build:` include |
| `taskfiles/k8s/remote/standard.yml` | Fixed colon, removed `build:` include |
| `taskfiles/k8s/common.yml` | Removed `build:` include |

### Deleted
| File | Reason |
|------|--------|
| `taskfiles/k8s/remote/common.yml` | Orphaned file, not included anywhere |

## Commands for Next Session

```bash
# Verify taskfiles work
task --list
task asyncapi:bundle
task k8s
task build

# If implementing CPU monitoring plan
cd /Volumes/Dev/Codev/Toniq/odin-ws/ws
go test -v ./internal/shared/platform/...
go test -v ./internal/server/limits/...

# If implementing config consolidation
mkdir -p ws/internal/shared/config
# Create timeouts.go as specified in plan
```

## Open Questions

1. **AsyncAPI bundle verification** - Was the client_events.yaml file created correctly? Test needed.

2. **Config constants migration** - When implementing, should we do it incrementally (one file at a time) or all at once?

3. **CPU monitoring** - Should we add `ALLOW_HOST_CPU_FALLBACK=true` env var for non-containerized dev environments, or just warn?

## Plan Documents Reference

| Plan | Status | Location |
|------|--------|----------|
| Server Performance Review | Reviewed, Updated | `docs/architecture/PLAN_SERVER_PERFORMANCE_REVIEW*.md` |
| CPU Monitoring Improvements | Created | `docs/architecture/PLAN_CPU_MONITORING_IMPROVEMENTS.md` |
| Deployment OIDC Config | Implemented (previous session) | `docs/architecture/PLAN_DEPLOYMENT_OIDC_CONFIG*.md` |
| Taskfiles Reorg | Implemented (previous session) | `docs/architecture/PLAN_TASKFILES_DEPLOYMENTS_REORG*.md` |
