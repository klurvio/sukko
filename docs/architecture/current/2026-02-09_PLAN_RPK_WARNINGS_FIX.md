# Plan: Fix Redpanda Startup Warnings

**Date:** 2026-02-09
**Status:** Ready for Implementation

---

## Issues

```
WARN  - can't find {kafka_internal/id_allocator} in the metadata cache
WARN  - Insecure Admin API listener on 0.0.0.0:9644
WARN  - Path: '/var/lib/redpanda/data' is on ext4, not XFS
ERROR - Memory: '268435456' below recommended: '1073741824'
```

---

## Analysis

| Issue | Severity | Cause | Fix |
|-------|----------|-------|-----|
| id_allocator not found | Low | Transient startup message | Use `--mode dev-container` |
| Insecure Admin API | Low | No auth on admin port | Expected for local dev |
| ext4 not XFS | Low | Docker uses host filesystem | Use `--mode dev-container` |
| Memory below 1GB | Medium | Local config uses 256MB | Use `--mode dev-container` or increase memory |

---

## Important: XFS Filesystem Requirement

**Redpanda requires XFS filesystem for production deployments.**

- XFS is the only officially tested and supported filesystem
- ext4 "will probably work" but is not guaranteed
- XFS provides better performance for Redpanda's write patterns (copy-on-write, direct I/O)

**Local dev (Kind/Docker):** ext4 is acceptable since we're using `--mode=dev-container` which disables filesystem checks. Performance is not critical for local testing.

**Production (GKE/EKS):** Must use XFS-formatted persistent volumes. GCP Persistent Disks default to ext4, so you need to either:
1. Use a StorageClass that formats as XFS
2. Pre-format the disk as XFS before attaching
3. Use Redpanda Cloud (managed service)

---

## Solution: Use Developer Container Mode

Redpanda has a `--mode dev-container` flag specifically for development/testing environments. This mode:
- Disables hardware requirement checks (memory, filesystem)
- Silences non-critical warnings
- Optimized for low-resource containers

**Change:** Add `--mode dev-container` to local Redpanda startup.

---

## Files to Modify

### 1. `deployments/helm/sukko/charts/redpanda/templates/statefulset.yaml`

Add `developerMode` conditional:

```yaml
command:
  - rpk
  - redpanda
  - start
  {{- if .Values.developerMode }}
  - --mode=dev-container
  {{- end }}
  - --smp=1
  - --memory={{ .Values.memory | default "512M" }}
  - --reserve-memory=0M
  - --overprovisioned
  - --node-id=0
  - --check=false
  # ... rest of args
```

### 2. `deployments/helm/sukko/charts/redpanda/values.yaml`

Add default:

```yaml
# Developer mode - silences hardware warnings (memory, filesystem)
# Enable for local/dev environments, disable for production
developerMode: false
```

### 3. `deployments/helm/sukko/values/local.yaml`

Enable developer mode:

```yaml
redpanda:
  enabled: true
  developerMode: true  # ADD: Silence hardware warnings
  memory: "256M"
  # ... rest unchanged
```

---

## Alternative: Increase Memory

If you want to run Redpanda with production-like settings locally, increase memory:

```yaml
# local.yaml
redpanda:
  memory: "1G"  # Changed from 256M
  resources:
    limits:
      memory: "1.5Gi"  # Changed from 384Mi
```

**Tradeoff:** Uses more resources on M1 MacBook. Not recommended for local dev.

---

## Detailed Changes

### statefulset.yaml

```yaml
# Line 32-40, add developerMode conditional
command:
  - rpk
  - redpanda
  - start
  {{- if .Values.developerMode }}
  - --mode=dev-container
  {{- end }}
  - --smp=1
  - --memory={{ .Values.memory | default "512M" }}
  - --reserve-memory=0M
  - --overprovisioned
  - --node-id=0
  - --check=false
```

### values.yaml (base)

```yaml
# Add after line 10 (image section)

# Developer mode - silences hardware requirement warnings
# Enable for local/dev environments, disable for production
developerMode: false
```

### local.yaml

```yaml
# Update redpanda section
redpanda:
  enabled: true
  developerMode: true  # Silence hardware warnings for local dev
  memory: "256M"
  externalAccess:
    enabled: true
    advertisedHost: host.docker.internal
    containerPort: 19092
  resources:
    requests:
      cpu: "250m"
      memory: "256Mi"
    limits:
      cpu: "500m"
      memory: "384Mi"
  storage:
    enabled: false
```

---

## Warnings That Will Remain

After this fix, you may still see:
- **Insecure Admin API** - This is expected for local dev (no auth). Can be ignored.

To silence this warning, add `admin_api_require_auth: false` to redpanda.yaml config, but this is overkill for local dev.

---

## Verification

1. **Deploy:**
   ```bash
   task local:deploy
   ```

2. **Check logs:**
   ```bash
   kubectl logs -n sukko-local sukko-redpanda-0 | head -50
   ```

3. **Should NOT see:**
   - Memory below recommended error
   - XFS warning
   - id_allocator warning

4. **May still see (OK to ignore):**
   - Insecure Admin API warning

---

## Files Summary

| File | Change |
|------|--------|
| `deployments/helm/sukko/charts/redpanda/templates/statefulset.yaml` | Add `--mode=dev-container` conditional |
| `deployments/helm/sukko/charts/redpanda/values.yaml` | Add `developerMode: false` default |
| `deployments/helm/sukko/values/local.yaml` | Set `developerMode: true` |
