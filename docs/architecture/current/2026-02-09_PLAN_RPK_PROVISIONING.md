# Plan: Redpanda Topic Provisioning via rpk

**Date:** 2026-02-09
**Status:** Planning

**Scope:** Add taskfile tasks to provision Redpanda topics using `rpk` command for local and remote environments.

---

## Requirements Summary

| Environment | Mode | Topics | Channels/Keys |
|-------------|------|--------|---------------|
| **Local** | testing | `local.sukko.trade` | `sukko.all.trade` |
| **Remote** | official dev | `{namespace}.sukko.trade` | `sukko.all.trade` |
| **Remote** | smoke test | `{namespace}.sukko.trade` | `sukko.all.trade` |
| **Remote** | load test | Multiple topics | Multiple channels |

---

## Task Structure

### Local Tasks (in `taskfiles/k8s/local/Taskfile.yml`)

```
k8s:local:provision:topics      - Create topic for local testing
k8s:local:provision:status      - List topics
k8s:local:provision:delete      - Delete topics (cleanup)
```

### Remote Tasks (in `taskfiles/k8s/remote/standard.yml`)

```
k8s:remote:standard:provision:official   - Create official dev topic
k8s:remote:standard:provision:smoke      - Create smoke test topic
k8s:remote:standard:provision:loadtest   - Create load test topics
k8s:remote:standard:provision:status     - List topics
k8s:remote:standard:provision:delete     - Delete all test topics
```

---

## Topic Naming Convention

```
{namespace}.{tenant}.{category}
```

- **namespace**: Environment prefix (`local`, `std-develop`, `std-staging`, etc.)
- **tenant**: Tenant ID (`sukko`)
- **category**: Data category (`trade`, `liquidity`, `orderbook`, etc.)

---

## Implementation Details

### Local Provisioning

```yaml
provision:topics:
  desc: Create Redpanda topics for local testing
  cmds:
    - kubectl exec -n {{.LOCAL_NAMESPACE}} {{.LOCAL_RELEASE_NAME}}-redpanda-0 -- rpk topic create local.sukko.trade -p 3

provision:status:
  desc: List Redpanda topics
  cmds:
    - kubectl exec -n {{.LOCAL_NAMESPACE}} {{.LOCAL_RELEASE_NAME}}-redpanda-0 -- rpk topic list

provision:delete:
  desc: Delete all topics (cleanup)
  cmds:
    - kubectl exec -n {{.LOCAL_NAMESPACE}} {{.LOCAL_RELEASE_NAME}}-redpanda-0 -- rpk topic delete local.sukko.trade
```

### Remote Official Dev

```yaml
provision:official:
  desc: Create official dev topic
  cmds:
    - kubectl exec -n {{.STD_NAMESPACE}} {{.STD_RELEASE_NAME}}-redpanda-0 -- rpk topic create {{.STD_NAMESPACE}}.sukko.trade -p 3
```

### Remote Smoke Test

```yaml
provision:smoke:
  desc: Create smoke test topic (same as official)
  cmds:
    - task: provision:official
```

### Remote Load Test

```yaml
provision:loadtest:
  desc: Create multiple topics for load testing
  cmds:
    - |
      for ID in BTC ETH SOL DOGE SHIB PEPE WIF BONK; do
        for CAT in trade liquidity orderbook; do
          kubectl exec -n {{.STD_NAMESPACE}} {{.STD_RELEASE_NAME}}-redpanda-0 -- \
            rpk topic create {{.STD_NAMESPACE}}.sukko.$ID.$CAT -p 3
        done
      done
```

---

## rpk Connection Methods

### Local (via kubectl exec)
```bash
kubectl exec -n sukko-local sukko-redpanda-0 -- rpk topic list
```

### Remote (via kubectl exec)
```bash
kubectl exec -n sukko-std-develop sukko-redpanda-0 -- rpk topic list
```

---

## Files to Modify

| File | Changes |
|------|---------|
| `taskfiles/k8s/local/Taskfile.yml` | Add `provision:topics`, `provision:status`, `provision:delete` |
| `taskfiles/k8s/remote/standard.yml` | Add `provision:official`, `provision:smoke`, `provision:loadtest`, `provision:status`, `provision:delete` |

---

## Load Test Topic Matrix

| Identifier | Categories | Total Topics |
|------------|------------|--------------|
| BTC, ETH, SOL | trade, liquidity, orderbook | 9 |
| DOGE, SHIB, PEPE | trade, liquidity, orderbook | 9 |
| WIF, BONK | trade, liquidity, orderbook | 6 |
| **Total** | | **24 topics** |

Channel pattern: `sukko.{identifier}.{category}` (e.g., `sukko.BTC.trade`)

---

## Verification

1. `task k8s:local:provision:topics` creates `local.sukko.trade`
2. `task k8s:local:provision:status` lists the topic
3. `task k8s:remote:standard:provision:smoke` creates single topic
4. `task k8s:remote:standard:provision:loadtest` creates 24 topics
5. `task publish:local RATE=1` successfully publishes to topics
