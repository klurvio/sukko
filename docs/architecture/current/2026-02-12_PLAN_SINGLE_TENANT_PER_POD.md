# Architecture Review: Single-Tenant-Per-Pod

**Date:** 2026-02-12
**Status:** Rejected — keep current bridge model, apply fixes below

---

## Original Proposal

Each tenant gets its own `ws-server` Deployment (pod or cluster of pods) in Kubernetes, replacing the current shared multi-tenant consumer pool. Topic discovery via Kafka regex subscription (`^{namespace}\.{tenant_id}\..*`), tenant routing via gateway extracting `tenant_id` from auth token.

---

## Why This Design Is Problematic

### 1. Kafka regex subscription is a regression

The current codebase (`multitenant_pool.go:30-32`) **explicitly avoids regex subscriptions**:

```go
// Instead of regex subscriptions (which cause O(m) memory, O(m*p) CPU, and rebalance storms),
// we use explicit topic lists with periodic refresh from the provisioning database.
// New topics are added via AddConsumeTopics() which avoids rebalance.
```

| Aspect | Current (explicit + AddConsumeTopics) | Proposed (regex) |
|---|---|---|
| Metadata scope | Only subscribed topics | ALL topics in cluster |
| Rebalance on new topic | No (incremental, KIP-429) | Yes (full group rebalance) |
| Broker load at 100 tenants | Low (targeted metadata) | 100 full-metadata fetches per refresh cycle |
| `AddConsumeTopics()` works? | Yes | No (franz-go: no-op with regex) |
| New topic discovery | Database query (provisioning API) | Client-side regex on full cluster metadata |

**Key finding:** franz-go regex consumers send metadata requests with `nil` topic list, which returns **every topic in the cluster**. At 1,000 tenants x 10 topics = 10,000 topics, each consumer fetches all 10,000 topics every metadata refresh cycle. With 1,000 consumers at 30s refresh interval, that's ~33 full-metadata requests/second hitting the broker.

**Additional gotcha:** `AddConsumeTopics()` — which the current code uses to avoid rebalances — is a **no-op when regex is enabled** in franz-go. Switching to regex means losing the ability to add topics incrementally without rebalance.

### 2. Per-tenant pods don't scale

Each Deployment generates ~8 Kubernetes objects (Deployment, ReplicaSet, Pod, Service, ConfigMap, HPA, NetworkPolicy, ServiceAccount):

| Tenants | K8s Objects | Min Memory Reserved | Feasibility |
|---|---|---|---|
| 10 | ~80 | 5 GiB | Trivial |
| 100 | ~800 | 50 GiB | Comfortable |
| 1,000 | ~8,000 | 500 GiB | etcd/API server stress |
| 10,000 | ~80,000 | 5 TiB | Breaks without extraordinary measures |

Real-world evidence:
- **Mattermost** (CNCF case study): 5,000 workspaces with 10,000+ pods — hit IP exhaustion, DB connection pooling problems, monitoring infrastructure strain. Had to implement "hibernation" for idle workspaces.
- **Kolide**: Reached "several hundred tenants" but required ~12,000 lines of custom Kubernetes operator code.

**Idle tenant waste:** With 512Mi memory request per pod and 80% idle tenants, 100 tenants wastes ~40 GiB of memory and 80 CPU cores. Scale-to-zero (KEDA) has cold-start latency (5-30s) that is unacceptable for WebSocket connections.

### 3. The current architecture IS the industry standard

The existing shared + optional dedicated model is the "bridge model" recommended by the AWS SaaS Well-Architected Lens.

| Platform | Architecture | Model |
|---|---|---|
| Pusher | Shared multi-tenant infrastructure | Pool |
| Ably | Shared clusters, logical isolation | Pool |
| PubNub | Shared infrastructure | Pool |
| Discord | Shared gateway, per-guild routing | Pool/Bridge |
| Slack | Per-workspace pods | Bridge (50+ platform engineers) |
| Bloomberg | Shared gateways, per-client routing | Pool |

AWS explicitly states the bridge model is "the pragmatic reality" for most SaaS. Per-tenant pods (silo model) is recommended only for regulatory compliance or extreme noisy-neighbor isolation.

---

## Recommendation

**Keep the current bridge model (shared + dedicated).** The dedicated consumer path is the escape hatch for high-volume tenants that could cause head-of-line blocking in the shared group. Removing it eliminates the only mitigation for noisy tenants without requiring per-tenant Kubernetes Deployments.

---

## Action Items

### 1. Fix stale topic subscriptions (performance bug) — DONE

**Problem:** `updateSharedConsumer()` removes deprovisioned topics from the tracking map (`sharedTopics`) but doesn't actually stop fetching them. franz-go keeps fetching from those topics, wasting Kafka fetch bandwidth.

**Fix:** Added `PauseFetchTopics()` to `kafka.Consumer` wrapper and call it in `multitenant_pool.go` for deprovisioned topics. franz-go does not support removing topics from a group consumer — `PauseFetchTopics` is the best available alternative. It stops fetch bandwidth waste while keeping the consumer group stable. Paused topics are cleaned up on consumer restart.

**Files changed:**
| File | Change |
|---|---|
| `shared/kafka/consumer.go` | Added `PauseFetchTopics(topics ...string)` method wrapping `c.client.PauseFetchTopics()` |
| `orchestration/multitenant_pool.go:348` | Call `p.sharedConsumer.PauseFetchTopics(toRemove...)` before deleting from map |

### 2. Delete dead `KafkaConsumerPool` code — DONE

**Problem:** `KafkaConsumerPool` was dead code — marked deprecated, zero callers. `MultiTenantConsumerPool` fully subsumes it.

**Deleted:**
- `orchestration/kafka_pool.go` (188 lines)
- `orchestration/kafka_pool_test.go`

**Updated stale references:**
- `server/server.go` — 5 comments updated
- `orchestration/shard.go` — 2 comments updated
- `shared/types/types.go` — 2 comments updated

---

## Key Files

| File | Purpose |
|---|---|
| `ws/internal/server/orchestration/multitenant_pool.go` | Multi-tenant consumer pool |
| `ws/internal/shared/kafka/consumer.go` | Consumer wrapper (PauseFetchTopics added) |
| `ws/internal/provisioning/types.go` | Tenant types including ConsumerType |
| `ws/internal/shared/kafka/tenant_registry.go` | TenantRegistry interface |
| `ws/internal/provisioning/topic_registry.go` | Database-backed topic registry |

## Sources

- [AWS SaaS Lens: Silo, Pool, and Bridge Models](https://docs.aws.amazon.com/wellarchitected/latest/saas-lens/silo-pool-and-bridge-models.html)
- [GKE Multi-tenancy Overview](https://cloud.google.com/kubernetes-engine/docs/concepts/multitenancy-overview)
- [Kubernetes Large Cluster Considerations](https://kubernetes.io/docs/setup/best-practices/cluster-large/)
- [franz-go: ConsumeRegex and AddConsumeTopics interaction](https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo)
- [KIP-429: Incremental Cooperative Rebalancing](https://cwiki.apache.org/confluence/display/KAFKA/KIP-429)
- [CNCF/Mattermost: Building a SaaS Architecture with a Single Tenant Application](https://www.cncf.io/blog/2022/04/26/building-a-saas-architecture-with-a-single-tenant-application/)
- [Kolide: Using a Kubernetes Operator to Manage Tenancy](https://www.kolide.com/blog/using-a-kubernetes-operator-to-manage-tenancy-in-a-b2b-saas-app)
