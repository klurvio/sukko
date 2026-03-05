# Plan: Systematic Taskfiles Fix

**Date:** 2026-02-02
**Status:** Draft
**Priority:** Medium

## Summary

Systematically fix all taskfile issues while preserving the modular and composable design. This plan consolidates previous ad-hoc fixes and addresses remaining structural issues.

---

## Current State Analysis

### Directory Structure
```
taskfiles/
├── k8s/
│   ├── Taskfile.yml           (Main K8s entry point)
│   ├── common.yml             (Shared K8s tasks - asyncapi, helm, test)
│   ├── local/
│   │   └── Taskfile.yml       (KinD cluster tasks)
│   └── remote/
│       ├── Taskfile.yml       (Remote entry point)
│       └── standard.yml       (GKE Standard tasks)
├── shared/
│   ├── build.yml              (Docker image building)
│   ├── helm.yml               (Helm chart operations)
│   ├── test.yml               (Go testing & linting)
│   └── version.yml            (Version management)
├── provisioning.yml           (Database & API tasks)
└── terraform/
    └── Taskfile.yml           (Infrastructure management)
```

### Include Graph
```
Root Taskfile.yml
├── build:       → shared/build.yml          (SINGLE SOURCE)
├── test:        → shared/test.yml           (SINGLE SOURCE)
├── version:     → shared/version.yml        (SINGLE SOURCE)
├── provisioning: → provisioning.yml
├── k8s:         → k8s/Taskfile.yml
│   ├── local:   → k8s/local/Taskfile.yml
│   │   ├── helm: → shared/helm.yml          (environment-specific vars)
│   │   └── test: → shared/test.yml          (DUPLICATE)
│   ├── remote:  → k8s/remote/Taskfile.yml
│   │   └── standard: → k8s/remote/standard.yml
│   │       └── helm: → shared/helm.yml      (environment-specific vars)
│   └── common:  → k8s/common.yml
│       ├── helm: → shared/helm.yml          (DUPLICATE - no env vars)
│       └── test: → shared/test.yml          (DUPLICATE)
└── terraform:   → terraform/Taskfile.yml
```

---

## Issues Identified

### Issue 1: YAML Colon Parsing Errors (FIXED)
**Status:** Fixed in previous session

**Problem:** Task v3 requires echo statements with colons to be quoted.
```yaml
# BAD - causes YAML parsing error
- echo "Version: {{.VERSION}}"

# GOOD - works correctly
- 'echo "Version: {{.VERSION}}"'
```

**Files Fixed:**
- `taskfiles/shared/version.yml` (lines 21-23, 36)
- `taskfiles/k8s/Taskfile.yml` (lines 26-44)
- `taskfiles/k8s/remote/Taskfile.yml` (lines 16-25)
- `taskfiles/k8s/local/Taskfile.yml` (line 260)
- `taskfiles/k8s/remote/standard.yml` (lines 296-306, 490-496)

### Issue 2: Duplicate Task Error (FIXED)
**Status:** Fixed in previous session

**Problem:** `shared/build.yml` was included multiple times with `build:` namespace from different sub-taskfiles, causing "Found multiple tasks (build:gateway)" error.

**Solution Applied:**
1. Removed `build:` include from:
   - `taskfiles/k8s/common.yml`
   - `taskfiles/k8s/local/Taskfile.yml`
   - `taskfiles/k8s/remote/standard.yml`
2. Renamed root `build:` task to `build-all:` with `aliases: [build]` for backwards compatibility
3. Added comments explaining that build tasks are available from root

### Issue 3: Orphaned File (FIXED)
**Status:** Fixed in previous session

**Problem:** `taskfiles/k8s/remote/common.yml` existed but wasn't included anywhere.

**Solution:** File was deleted.

### Issue 4: Missing AsyncAPI Channel File (FIXED)
**Status:** Fixed in previous session

**Problem:** `ws/asyncapi/channel/client_events.yaml` was referenced but didn't exist.

**Solution:** File was created with proper AsyncAPI 3.0 format.

### Issue 5: Redundant Includes (OPEN)
**Status:** Open - Intentional but should be documented

**Problem:** `helm.yml` and `test.yml` are included multiple times:
- `helm.yml`: k8s/local, k8s/remote/standard, k8s/common (3 times)
- `test.yml`: k8s/local, k8s/common (2 times)

**Analysis:** This is intentional for environment-specific namespacing:
- `k8s:local:helm:*` - local cluster helm with local values
- `k8s:remote:standard:helm:*` - GKE helm with standard values
- `k8s:common:helm:*` - general helm without environment

**Recommendation:** Keep as-is but add documentation explaining the pattern.

### Issue 6: Implicit Parent Namespace Resolution (OPEN)
**Status:** Open - Works but could be confusing

**Problem:** Sub-taskfiles reference `build:*` tasks without directly including `build.yml`:
```yaml
# In k8s/remote/standard.yml
- task: build:push:all  # Works via parent namespace resolution
```

**Current Mitigation:** Comments added:
```yaml
# Note: Build tasks are available from root as build:* (no need to re-include)
```

**Recommendation:** Keep current approach (avoids duplicate includes) but ensure comments are consistent across all files.

---

## Proposed Changes

### Phase 1: Verify Previous Fixes (Immediate)
1. Run `task --list` to verify all taskfiles parse correctly
2. Run `task asyncapi:bundle` to verify client_events.yaml fix
3. Run `task k8s` to verify help output works
4. Run `task build` to verify build alias works

### Phase 2: Standardize Documentation (Low Priority)
1. Ensure all sub-taskfiles have consistent header comments explaining:
   - Purpose of the taskfile
   - Available namespaces from parent
   - Environment-specific variables
2. Add a README.md in taskfiles/ directory documenting:
   - Include hierarchy
   - Namespace conventions
   - How to add new taskfiles

### Phase 3: Optional Consolidation (Future)
If redundancy becomes a maintenance burden:

**Option A: Environment-Parameterized Includes**
Instead of separate includes per environment, use a single include with dynamic vars:
```yaml
# k8s/Taskfile.yml
includes:
  helm:
    taskfile: ../shared/helm.yml
    vars:
      CHART_PATH: "{{.ROOT_DIR}}/deployments/helm/sukko"
      # VALUES_FILE and NAMESPACE set by caller
```

**Option B: Keep Current Design**
The current approach works well:
- Clear separation of environments
- Explicit variable binding per environment
- No complex conditional logic

**Recommendation:** Keep Option B unless maintenance becomes problematic.

---

## Implementation Checklist

### Immediate (Phase 1)
- [ ] Verify `task --list` parses all taskfiles
- [ ] Verify `task asyncapi:bundle` works
- [ ] Verify `task k8s` shows help
- [ ] Verify `task build` triggers `build:all`
- [ ] Commit all fixes

### Near-Term (Phase 2)
- [ ] Review all taskfiles for consistent header comments
- [ ] Add missing "available from parent" comments
- [ ] Consider adding taskfiles/README.md

### Future (Phase 3)
- [ ] Monitor for maintenance burden from redundant includes
- [ ] Re-evaluate if team grows or includes multiply

---

## Files Summary

### Already Modified (Previous Session)
| File | Change |
|------|--------|
| `Taskfile.yml` | Renamed `build:` to `build-all:` with alias |
| `taskfiles/shared/version.yml` | Fixed colon issues |
| `taskfiles/k8s/Taskfile.yml` | Fixed colon issues |
| `taskfiles/k8s/common.yml` | Removed `build:` include |
| `taskfiles/k8s/local/Taskfile.yml` | Fixed colon, removed `build:` include |
| `taskfiles/k8s/remote/Taskfile.yml` | Fixed colon issues |
| `taskfiles/k8s/remote/standard.yml` | Fixed colon, removed `build:` include |
| `ws/asyncapi/channel/client_events.yaml` | Created missing file |

### Deleted (Previous Session)
| File | Reason |
|------|--------|
| `taskfiles/k8s/remote/common.yml` | Orphaned file |

### No Changes Needed
| File | Reason |
|------|--------|
| `taskfiles/shared/build.yml` | Clean, well-structured |
| `taskfiles/shared/helm.yml` | Clean, parameterized |
| `taskfiles/shared/test.yml` | Clean, no issues |
| `taskfiles/provisioning.yml` | Clean, no colon issues |
| `taskfiles/terraform/Taskfile.yml` | Clean, no colon issues |

---

## Verification Commands

```bash
# Basic verification
task --list                    # Should list all tasks
task --list-all               # Should show complete task tree

# Specific verification
task k8s                       # Should show K8s help menu
task build                     # Should trigger build:all
task asyncapi:bundle          # Should bundle AsyncAPI spec

# Environment-specific
task k8s:local:status         # Should work with local namespace
task k8s:remote:standard:status ENV=develop  # Should work with remote
```

---

## Design Principles Preserved

1. **Modular**: Each taskfile focuses on one concern (build, helm, test, etc.)
2. **Composable**: Taskfiles can be included and combined as needed
3. **Environment-Aware**: Local and remote environments have separate configurations
4. **DRY**: Shared tasks live in `shared/` and are included where needed
5. **Explicit**: Variable bindings are clear at include site
6. **Backwards Compatible**: Aliases preserve existing command names

---

## Related Documents

- `docs/architecture/SESSION_HANDOFF_2026-02-02_TASKFILES_FIXES_AND_PLANS.md` - Previous session handoff
- `docs/architecture/PLAN_TASKFILES_DEPLOYMENTS_REORG*.md` - Original reorganization plan
