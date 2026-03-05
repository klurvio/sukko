# Session Handoff: 2026-02-14 - Cloud NAT 64-Connection Limit & Loadtest Investigation

**Date:** 2026-02-14
**Status:** In Progress
**Branch:** `refactor/taskfile-provisioning-consolidation`

## Session Goals

1. Investigate why `task gce:loadtest:run ENV=dev CONNECTIONS=10000` only sustained 64 connections
2. Investigate why ws-server appeared to not consume data from Redpanda
3. Fix the 64-connection bottleneck

## What Was Accomplished

### 1. Root Cause: Cloud NAT 64-Port Limit (FIXED in Terraform, APPLIED to dev)

- Loadtest VM (`--no-address`) routes through Cloud NAT to reach gateway external LB IP (`34.44.7.48:443`)
- GCP Cloud NAT default `minPortsPerVm` = 64, limiting the VM to exactly 64 simultaneous connections to the same destination
- **Fix applied**: Added `nat_min_ports_per_vm`, `nat_enable_dynamic_port_allocation`, `nat_max_ports_per_vm` as variables to the shared `gke-standard-cluster` Terraform module with safe GCP defaults
- Overridden in dev and stg: `min=16384 (2^14)`, `dynamic=true`, `max=32768 (2^15)`
- `task k8s:tf:plan ENV=dev` confirmed only NAT resource updates in-place (no cluster changes)
- `task k8s:tf:apply ENV=dev` was executed successfully

### 2. Redpanda Consumer Investigation (NO BUG - working correctly)

- ws-server consumer group is `sukko-shared-dev` (constructed as `"sukko-shared-" + namespace` in code, NOT from `KAFKA_CONSUMER_GROUP` env var)
- Confirmed by matching pod IPs (`10.1.0.38`, `10.1.0.39`) to consumer group members
- Consumer has LAG=0 on all topics — fully caught up
- Dashboard showed 0 messages/sec because **publisher wasn't running**, not a consumer bug
- Redpanda Throughput "Bytes Fetched" is a broker-level metric (all consumers), while "Kafka Consumer (ws-server)" is application-specific

### 3. Topic/Category Caching Answer (from prior session)

- `TOPIC_REFRESH_INTERVAL` defaults to `60s` (server config)
- `MultiTenantConsumerPool.refreshInterval` defaults to `30s`
- `Producer.topicCacheTTL` defaults to `30s`

## Key Decisions & Context

- **gcloud vs Terraform for NAT fix**: Initially recommended gcloud (dev-only, zero blast radius). User needed it for stg too, so implemented as Terraform variables with safe defaults. Prod is untouched.
- **`min_ports_per_vm` must be power of 2** when dynamic allocation is enabled. User wanted max >= 30K, so `max_ports_per_vm = 32768` (2^15).
- **Cloud NAT is NOT vital for GKE cluster** — subnet has `private_ip_google_access = true`. NAT is only needed for GCE VMs (loadtest/publisher) to reach external IPs.
- **Publisher VM doesn't need NAT port increase** — connects to Redpanda via internal VPC IP.

## Technical Details

### Cloud NAT config applied to dev

```
google_compute_router_nat.nat updated in-place:
  enable_dynamic_port_allocation: false -> true
  max_ports_per_vm:               0     -> 32768
  min_ports_per_vm:               0     -> 16384
```

### Consumer group naming (important for debugging)

```go
// multitenant_pool.go:308
ConsumerGroup: "sukko-shared-" + p.config.Namespace  // e.g., "sukko-shared-dev"

// Dedicated tenants:
ConsumerGroup: fmt.Sprintf("sukko-%s-%s", tenant.TenantID, p.config.Namespace)
```

### Redpanda consumer groups in dev

```
sukko-shared-dev          ← ws-server (2 members)
sukko-cdc-consumer-server ← CDC pipeline
nestjs-group-client      ← nestjs app
1                        ← unknown
```

## Issues & Solutions

| Issue | Root Cause | Solution |
|-------|-----------|----------|
| 64 connection limit | Cloud NAT default `minPortsPerVm=64` | Terraform variables + override in dev/stg |
| ws-server "not consuming" | Publisher not running; dashboard misread | No code fix needed — start publisher |
| Loadtest VM preempted | `--preemptible` flag on GCE VM | Expected behavior; redeploy with `task gce:loadtest:deploy` |
| Docker auth failed on new VM | Startup script hadn't finished | Wait for startup script to complete before running loadtest |

## Current State

- **NAT fix**: Applied to dev via `terraform apply`. Stg Terraform updated but not applied yet.
- **Loadtest VM**: Freshly redeployed (`task gce:loadtest:deploy ENV=dev`). Startup script may still be running (Docker install + image pull).
- **Loadtest**: Not yet run with the NAT fix. User attempted `CONNECTIONS=500 RAMP=5` but hit Docker auth error (startup script still in progress).
- **Publisher**: Not running. Need `task gce:publisher:run ENV=dev` to see message throughput.
- **ws-server**: Healthy, 64 connections from prior test still active and stable (pong fix confirmed working from previous session).

## Next Steps

### Immediate Priority
- Wait for loadtest VM startup script to finish, then run:
  ```bash
  task gce:loadtest:run ENV=dev CONNECTIONS=500 DURATION=30m RAMP=5
  ```
- Verify connections exceed 64 (confirming NAT fix works)
- Scale up to higher connection counts (10K, 30K)

### Near Term
- Apply NAT Terraform changes to stg: `task k8s:tf:plan ENV=stg` + `task k8s:tf:apply ENV=stg`
- Run publisher alongside loadtest to verify end-to-end message flow:
  ```bash
  task gce:publisher:run ENV=dev
  ```
- Monitor dashboard: active connections, messages/sec, error rate

### Future Considerations
- Prod Cloud NAT stays on defaults (64 ports) — no loadtest VMs expected in prod
- Consider adding a `task gce:loadtest:status` command to check VM + container state in one step
- The loadtest `LOG_LEVEL=info` doesn't log individual connection failures (only at Debug level) — may want to add a summary log for failed dial reasons

## Files Modified

| File | Change |
|------|--------|
| `deployments/terraform/modules/gke-standard-cluster/variables.tf` | Added 3 NAT port allocation variables with safe GCP defaults |
| `deployments/terraform/modules/gke-standard-cluster/main.tf` | NAT resource uses variables for port config |
| `deployments/terraform/environments/standard/dev/main.tf` | NAT override: min=16384, dynamic=true, max=32768 |
| `deployments/terraform/environments/standard/stg/main.tf` | NAT override: min=16384, dynamic=true, max=32768 |
| `docs/architecture/current/2026-02-14_FIX_CLOUD_NAT_64_CONNECTION_LIMIT.md` | Investigation doc with root cause, fix, safety review |

## Commands for Next Session

```bash
# Check if loadtest VM is ready
gcloud compute ssh wsloadtest-dev --zone=us-central1-a --project=sukko-9e902 \
  --tunnel-through-iap --command="docker images | grep wsloadtest"

# Run loadtest (start small, scale up)
task gce:loadtest:run ENV=dev CONNECTIONS=500 DURATION=30m RAMP=5

# Check loadtest logs
gcloud compute ssh wsloadtest-dev --zone=us-central1-a --project=sukko-9e902 \
  --tunnel-through-iap --command="docker logs wsloadtest --tail=20"

# Start publisher for message throughput
task gce:publisher:run ENV=dev

# Apply NAT fix to stg
task k8s:tf:init ENV=stg
task k8s:tf:plan ENV=stg
task k8s:tf:apply ENV=stg
```

## Open Questions

1. **Loadtest at 30K connections**: Will the 2 gateway pods + 2 ws-server pods handle 30K? May need to scale replicas or node resources.
2. **`KAFKA_CONSUMER_GROUP` env var**: Set to `sukko-consumer` but the code constructs `sukko-shared-dev`. The env var appears unused — should it be removed or is it used elsewhere?
3. **`KAFKA_TOPIC_NAMESPACE` empty**: The env var is empty but the code derives `dev` from the environment. The Helm comment says "defaults to dev" — should it be set explicitly for clarity?
