# Plan: Decouple Consumer Group Naming from Topic Namespace

## Context

The Odin API (external) always publishes real data to `prod.odin.trade`. In non-prod environments, ws-server defaults to consuming `{environment}.odin.trade` (e.g., `dev.odin.trade`), which is empty. A `kafkaTopicNamespace` override exists to point ws-server at `prod.odin.*` topics, but there's an architectural flaw: **consumer group naming uses the topic namespace, not the environment**.

When dev sets `kafkaTopicNamespace: prod`:
- Topics: `prod.odin.trade` — correct (real data)
- Consumer group: `odin-shared-prod` — wrong (should be `odin-shared-dev`, conflicts with actual prod)

**Root cause**: `MultiTenantPoolConfig.Namespace` is used for both topic names AND consumer groups. These are orthogonal concerns that need to be decoupled.

**Goal**: Consumer group = f(environment), Topic name = f(kafkaTopicNamespace). Then set `kafkaTopicNamespace: prod` in dev so ws-server receives real data with environment-isolated consumer groups.

## Changes

### 1. `ws/internal/server/orchestration/multitenant_pool.go` — Core logic

**Add `Environment` field to config struct** (after line 84):
```go
// Namespace is the topic namespace (e.g., "prod", "dev")
// Used for topic name prefixes via TenantRegistry: {namespace}.{tenant}.{category}
Namespace string

// Environment is the deployment environment (e.g., "dev", "stg", "prod")
// Used for consumer group naming: odin-shared-{environment}, odin-{tenant}-{environment}
// If empty, falls back to Namespace for backward compatibility.
Environment string
```

**Add fallback in `NewMultiTenantConsumerPool`** (after existing validation, ~line 128):
```go
if config.Environment == "" {
    config.Environment = config.Namespace
}
```

**Update init log** (line 151-154): Add `Str("environment", config.Environment)`

**Change consumer group construction** (2 lines):
- Line 308: `"odin-shared-" + p.config.Namespace` → `"odin-shared-" + p.config.Environment`
- Line 401: `fmt.Sprintf("odin-%s-%s", tenant.TenantID, p.config.Namespace)` → `fmt.Sprintf("odin-%s-%s", tenant.TenantID, p.config.Environment)`

**Update docstring** (lines 19-44): Change `{namespace}` to `{environment}` in consumer group references. Add note about the decoupling.

### 2. `ws/cmd/server/main.go` — Wiring

**Pass `Environment` to pool config** (line 217-228):
```go
multiTenantPool, err = orchestration.NewMultiTenantConsumerPool(orchestration.MultiTenantPoolConfig{
    Brokers:         kafkaBrokers,
    Namespace:       topicNamespace,
    Environment:     kafka.NormalizeEnv(cfg.Environment),  // consumer group naming
    Registry:        topicRegistry,
    // ... rest unchanged
})
```

**Update comments** (lines 147-149): `odin-shared-{namespace}` → `odin-shared-{environment}`

### 3. `ws/internal/server/orchestration/multitenant_pool_test.go` — Tests

**Update `TestMultiTenantPool_ConsumerGroupNaming`** (line 467):
- Add `environment` and `namespace` fields to test table
- Add cross-environment test case: `{env: "dev", ns: "prod"} → "odin-shared-dev"`
- Consumer group formula uses `environment`, not `namespace`

**Add `TestMultiTenantPool_CrossEnvironmentConsumption`**: Create pool with `Namespace: "prod"`, `Environment: "dev"`, verify config stored correctly.

**Add `TestMultiTenantPool_EnvironmentFallback`**: Create pool with `Environment: ""`, verify it falls back to `Namespace`.

### 4. `deployments/helm/odin/values/standard/dev.yaml` — Config

**ws-server** (line 83-84):
```yaml
environment: dev
kafkaTopicNamespace: prod  # Consume from prod.odin.* topics (Odin API publishes here)
```

**provisioning** (line 186):
```yaml
topicNamespace: prod  # Must match ws-server's kafkaTopicNamespace
```

### 5. `ws/internal/shared/platform/server_config.go` — Documentation

**Update `KafkaTopicNamespace` docstring** (line 165-173): Add note that consumer group naming always uses `ENVIRONMENT`, never `KafkaTopicNamespace`.

## What does NOT change

- **`kafka/topic.go`**: `BuildTopicName(namespace, tenantID, category)` — unchanged
- **`kafka/consumer.go`**: Receives consumer group string, doesn't construct it — unchanged
- **`kafka/producer.go`**: Uses topic namespace only — unchanged
- **Provisioning service code**: Uses `TopicNamespace` for topic creation only — unchanged
- **wspublisher**: Stays on `dev` namespace for independent testing
- **Zero performance impact**: Purely string construction in consumer group names

## Key Behavior After Deploy

| Environment | `kafkaTopicNamespace` | Topics Consumed | Consumer Group | Conflict? |
|---|---|---|---|---|
| dev | `prod` | `prod.odin.trade` | `odin-shared-dev` | No |
| stg | `prod` (if set) | `prod.odin.trade` | `odin-shared-stg` | No |
| prod | (defaults to `prod`) | `prod.odin.trade` | `odin-shared-prod` | No |

## Consumer Group Offset Note

Changing from `odin-shared-dev` (current) to a new `odin-shared-dev` consuming `prod.odin.*` topics means fresh offsets on `prod.odin.trade`. The consumer uses `kgo.ConsumeResetOffset(kgo.NewOffset().AtEnd())` (`consumer.go:143`), so it starts from the latest offset — no replay, no data loss concern.

## Verification

1. `cd ws && go test ./...` — all tests pass (including new cross-environment tests)
2. `go vet ./...` — clean
3. `task k8s:deploy ENV=dev` — deploy updated config
4. Check logs:
   ```bash
   kubectl logs -n odin-ws-dev -l app.kubernetes.io/name=ws-server --tail=50 | grep "Topic namespace"
   # Should show: Topic namespace: prod (environment: dev)
   ```
5. Check Prometheus:
   ```bash
   curl -s -u admin:admin 'http://localhost:3000/api/datasources/proxy/uid/prometheus/api/v1/query?query=ws_kafka_messages_received_total' | python3 -m json.tool
   # Should show topic="prod.odin.trade" with consumer_group="odin-shared-dev"
   ```
6. Grafana: Redpanda Throughput panel shows `Fetched: prod.odin.trade` with non-zero traffic
7. Grafana: Kafka Consumer panel shows `odin-shared-dev: prod.odin.trade` with non-zero rate

## Post-Deploy Cleanup (Optional)

After verifying consumption works:
```bash
# Delete orphaned dev-prefixed topics
rpk topic delete dev.odin.trade dev.odin.liquidity dev.odin.balances \
    dev.odin.community dev.odin.creation dev.odin.metadata \
    dev.odin.social dev.odin.analytics

# Delete orphaned consumer group
rpk group delete odin-shared-dev

# Investigate odin.main.trade (84.9 B/s — unknown origin)
rpk topic describe odin.main.trade
rpk group list | grep main
```
