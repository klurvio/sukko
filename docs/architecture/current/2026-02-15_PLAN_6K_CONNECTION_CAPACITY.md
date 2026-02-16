# Plan: Increase Connection Capacity to 20K

**Date:** 2026-02-15 (updated 2026-02-16)
**Status:** Implemented

---

## Context

Load testing hit a wall at ~2K connections. The gateway logs show "tenant connection limit exceeded" at 1000 connections per pod. With 2 gateway pods, the effective limit is 2000. The target is 20K total connections.

**Two limits are in play:**
- **Gateway** (`DEFAULT_TENANT_CONNECTION_LIMIT=1000`): Per-tenant, per-pod in-memory tracker. 2 pods × 1000 = 2000 effective. **This was the bottleneck.** The env vars exist in Go code (`gateway_config.go:70-71`) but were NOT exposed in the Helm chart.
- **WS-Server** (`clusterMaxConnections=100000`): Already 100K total (50K per pod × 2 pods). **Not the bottleneck.**

---

## Changes

### 1. Expose tenant connection limit in gateway Helm chart

**`deployments/helm/odin/charts/ws-gateway/values.yaml`** -- Added section:
```yaml
# Per-tenant connection limits (in-memory, per gateway pod)
# Effective cluster limit = default × replicaCount
tenantConnectionLimit:
  enabled: true
  default: 1000
```

**`deployments/helm/odin/charts/ws-gateway/templates/deployment.yaml`** -- Replaced misleading comment with env vars:
```yaml
# Per-tenant connection limits (in-memory per pod)
# Effective cluster limit = DEFAULT_TENANT_CONNECTION_LIMIT × gateway replica count
- name: TENANT_CONNECTION_LIMIT_ENABLED
  value: "{{ .Values.tenantConnectionLimit.enabled }}"
- name: DEFAULT_TENANT_CONNECTION_LIMIT
  value: "{{ .Values.tenantConnectionLimit.default }}"
```

### 2. Set 20K capacity in dev values

**`deployments/helm/odin/values/standard/dev.yaml`** -- Added under `ws-gateway:`:
```yaml
  tenantConnectionLimit:
    enabled: true
    default: 10000  # 10000 × 2 gateway pods = 20000 effective
```

**WS-Server `clusterMaxConnections: 100000` needs no change:**
- Current: `100000 / 2 replicas = 50000` per pod, 2 shards per pod = 25000 per shard
- 20K target is well within the 100K ceiling
- The `ResourceGuard` provides runtime protection beyond this hard limit -- it monitors CPU, memory, and goroutine pressure, rejecting connections before actual resource exhaustion regardless of the configured max

---

## Files Modified
- `deployments/helm/odin/charts/ws-gateway/values.yaml` -- Added `tenantConnectionLimit` defaults
- `deployments/helm/odin/charts/ws-gateway/templates/deployment.yaml` -- Added 2 env vars, fixed misleading comment
- `deployments/helm/odin/values/standard/dev.yaml` -- Set `default: 10000`

---

## Verification
1. `task k8s:deploy ENV=dev` (config-only change, no image rebuild needed)
2. Check gateway logs confirm new limit: `kubectl logs -n odin-ws-dev -l app.kubernetes.io/name=ws-gateway --tail=5 | grep "tenant"`
3. Run load test targeting 20K connections
4. No "tenant connection limit exceeded" until > 20K
