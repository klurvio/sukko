# Plan: Clean Up Local Development Port Dependencies

**Status:** Ready for Review
**Date:** 2026-02-10

---

## Problem

The local development Taskfile uses port-forwarding for services that are already exposed (or should be exposed) via Kind's `extraPortMappings`. This causes:
1. Loadtest failing because gateway port-forward isn't running
2. Publisher failing because Redpanda port-forward isn't running
3. Provisioning tasks with fragile port-forward logic that can fail silently
4. Port mismatches between Kind config and Taskfile variables

---

## Current State

### Kind Config (`deployments/k8s/local/kind-config.yaml`) - Current
| Service | containerPort | hostPort | Keep? |
|---------|---------------|----------|-------|
| Gateway | 30000 | 3000 | ✅ Yes |
| WS Server | 30080 | 3005 | ❌ Remove |
| Provisioning | 30081 | 8081 | ✅ Yes |
| Grafana | 30300 | 3010 | ✅ Yes |
| Prometheus | 30090 | 9090 | ❌ Remove |
| Redpanda Console | 30800 | 8080 | ❌ Remove |
| **Redpanda (Kafka)** | ❌ Not exposed | - | ✅ Add |

### Taskfile Variables - After Fix
```yaml
LOCAL_GATEWAY_PORT: 3000      # Kind: 30000→3000
LOCAL_PROVISIONING_PORT: 8081 # Kind: 30081→8081
LOCAL_REDPANDA_PORT: 9092     # Kind: 31092→9092
LOCAL_GRAFANA_PORT: 3010      # Kind: 30300→3010
```

### Services Needing Port-Forward
| Service | Current | Should Be |
|---------|---------|-----------|
| Gateway | port-forward | Kind exposed (3000) |
| Provisioning | port-forward | Kind exposed (8081) |
| Redpanda | port-forward | **Add to Kind** (9092) |
| PostgreSQL | port-forward | Keep port-forward (infrequent use) |

---

## Implementation

### 1. Set Fixed NodePort for Redpanda in Local Values

**File:** `deployments/helm/odin/values/local.yaml`

Change Redpanda external access to use NodePort with a fixed port:
```yaml
redpanda:
  externalAccess:
    enabled: true
    type: NodePort              # Change from LoadBalancer to NodePort
    nodePort: 31092             # Fixed NodePort for Kind mapping
    advertisedHost: host.docker.internal
    containerPort: 19092
```

### 2. Simplify Kind extraPortMappings

**File:** `deployments/k8s/local/kind-config.yaml`

Replace current 6 mappings with only 4:
```yaml
extraPortMappings:
  # WebSocket Gateway (client entry point)
  - containerPort: 30000
    hostPort: 3000
    protocol: TCP
  # Provisioning API (tenant management)
  - containerPort: 30081
    hostPort: 8081
    protocol: TCP
  # Redpanda (Kafka broker for publisher)
  - containerPort: 31092
    hostPort: 9092
    protocol: TCP
  # Grafana (metrics dashboard)
  - containerPort: 30300
    hostPort: 3010
    protocol: TCP
```

**Removed:**
- WS Server (30080→3005) - internal, accessed via Gateway
- Prometheus (30090→9090) - internal, accessed via Grafana
- Redpanda Console (30800→8080) - use `rpk` CLI instead

### 3. Remove Port-Forward Logic from Taskfile

**File:** `taskfiles/local.yml`

#### Remove from `provision:create`
The task already runs in a single shell block now but still starts a port-forward. Since provisioning is exposed on 8081, remove the port-forward logic entirely.

**Before:**
```yaml
# Kill any existing port-forward to provisioning
pkill -f "port-forward.*provisioning" ...
kubectl port-forward ... &
# Wait for port-forward...
```

**After:**
```yaml
# Just use the Kind-exposed port directly
curl -sf http://localhost:8081/api/v1/tenants ...
```

#### Remove from `provision:status`
Same as above - remove port-forward, use localhost:8081 directly.

#### Update `publisher:run`
Change Redpanda port from port-forward to Kind-exposed:
```yaml
-e KAFKA_BROKERS=host.docker.internal:9092
```
(This already uses 9092, just need Kind to expose it)

#### Remove `port-forward:start` and `port-forward:stop` tasks
Delete these tasks entirely - all main services are now exposed via Kind.
Keep port-forward logic only in `db:migrate` and `db:status` for PostgreSQL.

### 4. Keep Port-Forward for PostgreSQL Only

**File:** `taskfiles/local.yml`

PostgreSQL is only used for:
- `db:migrate` - one-time operation
- `db:status` - occasional check

Keep port-forward logic only in these tasks since they're infrequent.

---

## Files to Modify

| File | Changes |
|------|---------|
| `deployments/helm/odin/values/local.yaml` | Set Redpanda `type: NodePort` with `nodePort: 31092` |
| `deployments/k8s/local/kind-config.yaml` | Replace 6 mappings with 4 (gateway, provisioning, redpanda, grafana) |
| `taskfiles/local.yml` | Remove all port-forward logic except for PostgreSQL; delete `port-forward:start/stop` tasks |

---

## Verification

After changes, test the full local workflow without manual port-forwarding:

```bash
# 1. Recreate cluster (needed for Kind config changes)
task local:destroy
task local:setup

# 2. Provision (should work without port-forward)
task local:provision:create
task local:provision:status

# 3. Publisher (should connect to Redpanda directly)
task local:publisher:run RATE=1

# 4. Loadtest (should connect to gateway directly)
task local:loadtest:run CONNECTIONS=1 DURATION=1m RAMP=1
```

**Expected:** All commands work without any manual `kubectl port-forward` commands.

---

## Summary of Port Mappings After Fix

Only 4 services exposed via Kind (minimal footprint):

| Service | NodePort | Host Port | Purpose |
|---------|----------|-----------|---------|
| Gateway | 30000 | 3000 | WebSocket client entry |
| Provisioning | 30081 | 8081 | Tenant/topic management |
| Redpanda (Kafka) | 31092 | 9092 | Publisher message ingestion |
| Grafana | 30300 | 3010 | Metrics dashboard |

**Not exposed (internal only):**
- WS Server (accessed via Gateway)
- Prometheus (accessed via Grafana)
- Redpanda Console (use `rpk` CLI instead)
- PostgreSQL (port-forward when needed)
