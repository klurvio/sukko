# Plan: Per-Topic Grafana Metrics + Collector Cleanup

**Date:** 2026-02-15 (updated 2026-02-16)
**Status:** Implemented

## Context

Two issues discovered during loadtesting:

1. **No per-topic visibility:** Grafana dashboard panels show aggregated totals — impossible to investigate which Kafka topics drive throughput or where messages are dropped.
2. **CPU/Memory gauge inaccuracy:** The legacy `Collector` (one per shard) overwrites SystemMonitor's accurate Prometheus gauges with stale data every 15s. See `2026-02-16_FINDING_CPU_MEMORY_METRIC_DISCREPANCY.md`.

This plan:
- Adds per-topic and per-consumer-group labels to Kafka metrics
- Updates Grafana dashboard queries for per-topic visibility + gateway connections
- **Removes the legacy Collector** — consolidates all system metrics into SystemMonitor (singleton)

**Resource impact: negligible.** Per-topic labels add ~100ns per call (~2.5μs/sec at 25 msg/sec). Label cardinality is ~8 topics × ~2 consumer groups = ~16 time series. Collector removal is net-negative (removes redundant `runtime.ReadMemStats()` and CPU overwrite per shard every 15s).

---

## Part A: Per-Topic Metrics (2 code files + 1 dashboard file)

### 1. `ws/internal/server/metrics/metrics.go` — Per-topic labels

**Change Kafka counters from Counter to CounterVec** (lines 193-201):

Replace:
```go
kafkaMessagesReceived = promauto.NewCounter(prometheus.CounterOpts{
    Name: "ws_kafka_messages_received_total",
    Help: "Total number of messages received from Kafka",
})

kafkaMessagesDropped = promauto.NewCounter(prometheus.CounterOpts{
    Name: "ws_kafka_messages_dropped_total",
    Help: "Total number of Kafka messages dropped due to backpressure",
})
```

With:
```go
kafkaMessagesReceived = promauto.NewCounterVec(prometheus.CounterOpts{
    Name: "ws_kafka_messages_received_total",
    Help: "Total number of messages received from Kafka",
}, []string{"topic", "consumer_group"})

kafkaMessagesDropped = promauto.NewCounterVec(prometheus.CounterOpts{
    Name: "ws_kafka_messages_dropped_total",
    Help: "Total number of Kafka messages dropped due to backpressure",
}, []string{"topic", "consumer_group"})
```

**Update helper functions** (lines 435-443):

```go
func IncrementKafkaMessages(topic, consumerGroup string) {
    kafkaMessagesReceived.WithLabelValues(topic, consumerGroup).Inc()
}

func IncrementKafkaDropped(topic, consumerGroup string) {
    kafkaMessagesDropped.WithLabelValues(topic, consumerGroup).Inc()
}
```

### 2. `ws/internal/shared/kafka/consumer.go` — Pass topic through call chain

**Store consumerGroup on struct** — currently only passed to `kgo.ConsumerGroup()` in `NewConsumer()` but not stored. Needed for metrics labels.

Add field to `Consumer` struct (after `mu` at line 88):
```go
consumerGroup string       // Consumer group ID for metrics labels
```

Set in `NewConsumer()` — add to the struct literal at line 243 where `Consumer{...}` is constructed.

**Add topic to batchedMessage struct** (lines 370-373 in `consumeLoop()`):
```go
type batchedMessage struct {
    topic   string
    subject string
    message []byte
}
```

**Update prepareMessage return type** (line 482) — add `topic` field to the anonymous struct:
```go
func (c *Consumer) prepareMessage(record *kgo.Record) *struct {
    topic   string
    subject string
    message []byte
}
```
And in the return value (lines 550-556), include `topic: record.Topic`.

**Change incrementProcessed/incrementDropped to accept topic** (lines 679-697):
```go
func (c *Consumer) incrementProcessed(topic string) {
    c.mu.Lock()
    c.messagesProcessed++
    c.mu.Unlock()
    metrics.IncrementKafkaMessages(topic, c.consumerGroup)
}

func (c *Consumer) incrementDropped(topic string) {
    c.mu.Lock()
    c.messagesDropped++
    c.mu.Unlock()
    metrics.IncrementKafkaDropped(topic, c.consumerGroup)
}
```

**Update all 5 call sites:**

| Location | Current | New |
|---|---|---|
| `consumeLoop()` flush (line 387) | `c.incrementProcessed()` | `c.incrementProcessed(msg.topic)` |
| `processRecord()` (line 661) | `c.incrementProcessed()` | `c.incrementProcessed(record.Topic)` |
| `prepareMessage()` rate limit (line 491) | `c.incrementDropped()` | `c.incrementDropped(record.Topic)` |
| `processRecord()` rate limit (line 575) | `c.incrementDropped()` | `c.incrementDropped(record.Topic)` |
| `incrementFailed()` (line 546) | unchanged | unchanged (no Prometheus metric) |

### 3. `deployments/helm/odin/charts/monitoring/dashboards/overview.json` — Dashboard updates

**Message Throughput panel** (id: 11, line 595) — add per-topic Kafka ingestion lines:
```json
"targets": [
  {"expr": "sum(rate(ws_messages_sent_total[1m]))", "legendFormat": "WS Sent (total)", "refId": "A"},
  {"expr": "sum(rate(ws_messages_received_total[1m]))", "legendFormat": "WS Received (total)", "refId": "B"},
  {"expr": "sum by (topic)(rate(ws_kafka_messages_received_total[1m]))", "legendFormat": "Kafka: {{topic}}", "refId": "C"}
]
```

**Redpanda Throughput panel** (id: 20, line 696) — break down by topic (dashboard-only, no code change):
```json
"targets": [
  {"expr": "sum(rate(vectorized_cluster_partition_bytes_produced_total[1m]))", "legendFormat": "Bytes Produced (total)", "refId": "A"},
  {"expr": "sum(rate(vectorized_cluster_partition_bytes_fetched_total[1m]))", "legendFormat": "Bytes Fetched (total)", "refId": "B"},
  {"expr": "sum by (topic)(rate(vectorized_cluster_partition_bytes_produced_total[1m]))", "legendFormat": "Produced: {{topic}}", "refId": "C"},
  {"expr": "sum by (topic)(rate(vectorized_cluster_partition_bytes_fetched_total[1m]))", "legendFormat": "Fetched: {{topic}}", "refId": "D"}
]
```

**Kafka Consumer panel** (id: 21, line 784) — break down by topic and consumer group:
```json
"targets": [
  {"expr": "sum(rate(ws_kafka_messages_received_total[1m]))", "legendFormat": "Received (total)", "refId": "A"},
  {"expr": "sum(rate(ws_kafka_messages_dropped_total[1m]))", "legendFormat": "Dropped (total)", "refId": "B"},
  {"expr": "sum by (topic, consumer_group)(rate(ws_kafka_messages_received_total[1m]))", "legendFormat": "{{consumer_group}}: {{topic}}", "refId": "C"},
  {"expr": "sum by (topic, consumer_group)(rate(ws_kafka_messages_dropped_total[1m]))", "legendFormat": "Dropped: {{consumer_group}}/{{topic}}", "refId": "D"}
]
```

**Update "Active Connections" stat panel** (id: 4, line 302) — show gateway alongside ws-server:
```json
"targets": [
  {"expr": "sum(ws_connections_active)", "legendFormat": "ws-server", "refId": "A"},
  {"expr": "sum(gateway_connections_active)", "legendFormat": "gateway", "refId": "B"}
]
```

**Update "Active Connections by Pod" timeseries panel** (id: 10, line 512) — add gateway pods:
```json
"targets": [
  {"expr": "sum(ws_connections_active) by (pod)", "legendFormat": "ws: {{pod}}", "refId": "A"},
  {"expr": "sum(gateway_connections_active) by (pod)", "legendFormat": "gw: {{pod}}", "refId": "B"}
]
```

**Add new "Gateway Tenant Connections" timeseries panel** — shows per-tenant active connections and rejections. Place in the "Connections" row (after id: 10, gridPos y: 6, x: 12, w: 12, h: 8). Use **id: 32** (id: 30 is already taken by "WS Server CPU Usage" at line 1116, id: 31 is "WS Server Memory Usage"):
```json
{
  "title": "Gateway Tenant Connections",
  "type": "timeseries",
  "id": 32,
  "gridPos": {"h": 8, "w": 12, "x": 12, "y": 6},
  "targets": [
    {"expr": "sum by (tenant_id)(gateway_tenant_connections_active)", "legendFormat": "Active: {{tenant_id}}", "refId": "A"},
    {"expr": "sum by (tenant_id)(rate(gateway_tenant_connections_rejected_total[1m]))", "legendFormat": "Rejected/sec: {{tenant_id}}", "refId": "B"}
  ]
}
```

This panel answers key loadtest questions:
- How many connections does each tenant have? (vs the 10K/pod limit)
- Are connections being rejected? (hitting the ceiling)

---

## Part B: Collector Removal (3 code files + 1 test file)

### Background

The `Collector` (metrics.go:530-627) was the original per-shard metrics gatherer. When `SystemMonitor` was added as a singleton with cgroup-aware CPU measurement, the Collector's CPU/memory writes were never removed — creating the dual-writer bug documented in `2026-02-16_FINDING_CPU_MEMORY_METRIC_DISCREPANCY.md`.

The connection gauge had the same bug and was already fixed (see metrics.go:594-597 comment). This plan applies the same fix to CPU and memory, and removes all Collector-related code.

**What Collector does today (all replaced or removed):**

| Collector Responsibility | Replacement |
|---|---|
| `estimateCPU()` → `CPUUsagePercent.Set(stale)` | **Remove** — SystemMonitor already sets this every 1s with fresh cgroup data |
| `runtime.ReadMemStats()` → `memoryUsageBytes.Set()` | **Move to SystemMonitor** — `updateMetrics()` already calls `ReadMemStats` |
| `runtime.NumGoroutine()` → `goroutinesActive.Set()` | **Move to SystemMonitor** — `updateMetrics()` already calls `NumGoroutine()` |
| `kafkaConnected.Set()` via nil check | **Move to main.go** — set once after pool starts (fixes nil interface bug) |
| `connectionsMax.Set()` in `Start()` | **Already handled** — `LoadBalancer.aggregateMetrics()` calls `SetAggregatedConnectionMetrics()` |
| `memoryLimitBytes.Set()` in `Start()` | **Move to SystemMonitor** — cgroup limit fits naturally in SystemMonitor init |

### 4. `ws/internal/server/metrics/metrics.go` — Remove Collector + dead code

**Remove entire Collector section** (lines 530-627):
- `ServerMetrics` interface (lines 535-541)
- `Collector` struct (lines 543-547)
- `NewCollector()` (lines 549-555)
- `Start()` (lines 557-583)
- `Stop()` (lines 585-588)
- `collect()` (lines 590-616)
- `estimateCPU()` (lines 618-627)

**Remove dead helper functions** (never called anywhere in codebase):
- `SetMemoryUsage()` (lines 402-405)
- `SetMemoryLimit()` (lines 407-410)
- `SetCPUUsage()` (lines 412-415)
- `SetGoroutines()` (lines 417-420)
- `SetMaxConnections()` (lines 292-295) — `SetAggregatedConnectionMetrics()` handles this via LoadBalancer

**Clean up imports** — remove `runtime` and `platform` (only used by Collector).

### 5. `ws/internal/server/metrics/system_monitor.go` — Gain gauge responsibilities

**In `updateMetrics()` (line 138)** — add Prometheus gauge updates for memory and goroutines. The variables `mem` and `goroutines` already exist in this function:

```go
// After existing CPU gauge updates (line 164), add:
memoryUsageBytes.Set(float64(mem.Alloc))
goroutinesActive.Set(float64(goroutines))
```

**In `GetSystemMonitor()` init (line 50)** — set cgroup memory limit once at startup:

```go
// After SystemMonitor creation, before return:
memLimit, err := platform.GetMemoryLimit()
if err == nil && memLimit > 0 {
    memoryLimitBytes.Set(float64(memLimit))
}
```

`platform` is already imported in system_monitor.go — no import change needed.

### 6. `ws/internal/server/server.go` — Remove Collector wiring

**Remove field** (line 65):
```go
metricsCollector *metrics.Collector    // Prometheus metrics collector
```

**Remove creation** (line 128):
```go
s.metricsCollector = metrics.NewCollector(s)
```

**Remove start** (line 283):
```go
s.metricsCollector.Start()
```

**Remove `GetKafkaConsumer()` method** (lines 198-201) — only existed for `ServerMetrics` interface, has nil interface bug (documented in `TestServer_GetKafkaConsumer_Nil`):
```go
// GetKafkaConsumer implements metrics.ServerMetrics interface
func (s *Server) GetKafkaConsumer() any {
    return s.kafkaConsumer
}
```

**Update comments** on `GetConfig()` (line 188) and `GetStats()` (line 193) — remove "implements metrics.ServerMetrics interface" since that interface is deleted.

### 7. `ws/internal/server/server_test.go` — Remove dead test

**Remove `TestServer_GetKafkaConsumer_Nil`** (lines 63-94) — tests the method we're removing. This test documented the nil interface bug that the Collector suffered from.

**Remove `kafka` import** if no other test references it (line 9):
```go
"github.com/Toniq-Labs/odin-ws/internal/shared/kafka"
```

### 8. `ws/cmd/server/main.go` — Set kafka connected status

**After multi-tenant pool starts** (after line 237), add:
```go
metrics.SetKafkaConnected(true)
```

This replaces the Collector's per-shard nil interface check (which always returned true due to Go nil semantics — see finding doc) with a single correct call when the pool actually starts.

---

## What Does NOT Change

- No changes to `pump.go`, `connection.go`, `handlers_http.go`
- No changes to `ws_messages_sent_total` or `ws_messages_received_total` (WebSocket frame counters, independent of Kafka topics)
- No changes to `websocket.json` or `redpanda.json` dashboards
- No changes to consumer logic, batching, rate limiting, or CPU brake
- `incrementFailed()` unchanged (no Prometheus metric attached)
- `server/metrics.go` `collectMetrics()` / `monitorMemory()` / `sampleClientBuffers()` — untouched (separate concern)
- `GetConfig()` and `GetStats()` methods remain on Server (used by shard.go and tests)

## Resource Impact

| Aspect | Impact |
|---|---|
| CPU (ws-server) | Net negative — removes per-shard `ReadMemStats()` + `NumGoroutine()` + stale CPU overwrite every 15s; adds ~2.5μs/sec for label hashing |
| Memory (Prometheus) | ~16 extra time series (~2KB) for per-topic labels |
| Cardinality | 8 topics × 2 consumer groups = 16 (safe) |
| Latency | Zero — fire-and-forget, async scrape |
| Code removed | ~100 lines (Collector + dead helpers) |
| Accuracy | CPU gauge now reflects 1s-fresh cgroup data instead of up-to-15s stale copy |

## Files Modified

| File | Part | Change |
|---|---|---|
| `ws/internal/server/metrics/metrics.go` | A+B | Counter → CounterVec for Kafka metrics; remove Collector, dead helpers, imports |
| `ws/internal/shared/kafka/consumer.go` | A | Store consumerGroup, pass topic through call chain |
| `ws/internal/server/metrics/system_monitor.go` | B | Add memory/goroutine gauge updates + memory limit init |
| `ws/internal/server/server.go` | B | Remove metricsCollector field/creation/start, remove GetKafkaConsumer() |
| `ws/internal/server/server_test.go` | B | Remove TestServer_GetKafkaConsumer_Nil |
| `ws/cmd/server/main.go` | B | Set kafkaConnected after pool starts |
| `overview.json` | A | Per-topic queries + gateway panels (id: 32) |

## Verification

1. `cd ws && go test ./...` — all tests pass (including consumer_test.go for new signatures)
2. Build + deploy: `task k8s:build:push:ws-server ENV=dev && task k8s:deploy ENV=dev`
3. Verify per-topic labels: `curl -s <ws-server>/metrics | grep ws_kafka_messages_received_total` — should show `topic` and `consumer_group` labels
4. Verify Collector removed: `curl -s <ws-server>/metrics | grep ws_cpu_usage_percent` — still present (now only from SystemMonitor)
5. Verify kafka status: `curl -s <ws-server>/metrics | grep ws_kafka_connected` — shows 1 when consumer pool running
6. Grafana: Message Throughput, Redpanda Throughput, and Kafka Consumer panels show per-topic lines
7. Grafana: Active Connections shows both gateway + ws-server; Gateway Tenant Connections panel (id: 32) shows per-tenant counts and rejections
8. Grafana: CPU Usage panel reflects real-time spikes (no more stale overwrites)

## Future Consideration

`ws/internal/server/metrics.go` `collectMetrics()` (line 15) is another legacy piece — uses **gopsutil** to measure process RSS memory every 2s and writes `stats.MemoryMB`. This same field is also written by `ResourceGuard.UpdateResources()` from SystemMonitor every 15s (same dual-writer pattern). CPU measurement was already removed from it (comment at line 40). Could be cleaned up in a follow-up to fully consolidate memory measurement into SystemMonitor.
