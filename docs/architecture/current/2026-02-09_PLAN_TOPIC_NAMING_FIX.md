# Plan: Fix Topic Naming - Provisioning API as Single Source

**Status:** Done

**Goal:**
1. Remove topic creation from Helm - Provisioning API is the single source of truth
2. Align KAFKA_TOPIC_NAMESPACE with environment names (local, dev, stg, prod)

---

## Problem

1. Topics created in multiple places (Helm, taskfile, API)
2. Inconsistent namespace naming (dev uses `main`, code says `stag` vs `stg`)

---

## Solution

### Topic Creation
- Delete Helm topics-job.yaml
- Remove `topics:` from all Helm values
- Remove direct `rpk` calls from taskfile
- Provisioning API creates all topics

### Namespace Alignment

| Environment | KAFKA_TOPIC_NAMESPACE | Topic Example |
|-------------|----------------------|---------------|
| local | `local` | `local.odin.trade` |
| dev | `dev` | `dev.odin.trade` |
| stg | `stg` | `stg.odin.trade` |
| prod | `prod` | `prod.odin.trade` |

---

## Changes

### 1. Delete topics-job.yaml

```bash
rm deployments/helm/odin/charts/redpanda/templates/topics-job.yaml
```

### 2. Remove topics from Helm values

Remove `redpanda.topics` section from:
- `values/local.yaml`
- `values/standard/dev.yaml`
- `values/standard/stg.yaml`
- `values/standard/prod.yaml`

### 3. Fix kafkaTopicNamespace in dev.yaml

```yaml
# Before
ws-server:
  config:
    kafkaTopicNamespace: main  # Remove this line

# After - let it default to environment (dev)
ws-server:
  config:
    environment: dev
    # kafkaTopicNamespace defaults to environment
```

### 4. Remove direct rpk from taskfile

In `taskfiles/local.yml`, remove:
```yaml
- kubectl exec ... -- rpk topic create local.odin.trade -p 3 2>/dev/null || true
```

### 5. Update code comments (stag → stg)

In `ws/internal/shared/kafka/config.go`, update valid namespaces:
```go
// Valid namespaces: local, dev, stg, prod
```

---

## Files to Modify

| File | Action |
|------|--------|
| `charts/redpanda/templates/topics-job.yaml` | Delete |
| `values/local.yaml` | Remove `topics:` section |
| `values/standard/dev.yaml` | Remove `topics:`, remove `kafkaTopicNamespace: main` |
| `values/standard/stg.yaml` | Remove `topics:` section |
| `values/standard/prod.yaml` | Remove `topics:` section |
| `taskfiles/local.yml` | Remove `rpk topic create` line |
| `ws/internal/shared/kafka/config.go` | Update comments: stag → stg |

---

## Topic Naming Convention

```
{namespace}.{tenant}.{category}
```

| Namespace | Description |
|-----------|-------------|
| `local` | Local Kind cluster |
| `dev` | Development GKE |
| `stg` | Staging GKE |
| `prod` | Production GKE |

---

## Test

```bash
# Delete old topics
kubectl exec -n odin-local odin-redpanda-0 -- rpk topic delete -r '.*'

# Redeploy
task local:deploy

# Create topics via API
task local:provision:create

# Verify topics are named correctly (local.odin.*)
task local:provision:status

# Test publisher
task local:publisher:run
```
