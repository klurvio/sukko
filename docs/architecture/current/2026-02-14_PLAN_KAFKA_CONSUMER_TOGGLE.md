# Plan: KAFKA_CONSUMER_ENABLED Toggle (Simplified)

**Date:** 2026-02-14
**Status:** Planned
**Branch:** `refactor/taskfile-provisioning-consolidation`

## Context

When loadtesting ws-server, we need to isolate **connection handling** from **message consumption** to measure how many WebSocket connections the server can sustain over time. This toggle skips starting the consumer pool entirely, so the server only handles WebSocket connections (accept, sustain, ping/pong) with zero Kafka overhead. Dev-only — consumer group rebalancing is acceptable.

## Coding Guidelines Compliance

References: `docs/architecture/CODING_GUIDELINES.md`

| Guideline Section | Line | Compliance |
|---|---|---|
| No Hardcoded Values | 29 | `env:"KAFKA_CONSUMER_ENABLED" envDefault:"true"` — env var with default |
| Observable by Default | 61 | Logs at startup when disabled; health endpoint reports status |
| Feature Flags | 790 | "Feature must be entirely absent — not half-initialized" — pool never created |
| Degradation Decision Matrix | 822 | Kafka Brokers classified as Optional |
| Health check three-state | 704 | healthy/degraded/unhealthy preserved; disabled = healthy (intentional) |
| Configuration (env struct tags) | 904 | Uses `caarlos0/env` with proper tags |
| Nil checks acceptable | 631 | Boolean flag guards distinct code path (skipping entire feature) |
| Logging (startup) | 836 | main.go uses `logger.Printf` — consistent with existing startup pattern (lines 143, 210, 236) |
| Print()/LogConfig() | 503/594 | New field included in both methods |

## Changes (5 files)

### 1. `ws/internal/shared/platform/server_config.go`

**Add field** after `ConsumerGroup` (line 48):
```go
// KafkaConsumerEnabled controls whether the Kafka consumer pool is started.
// When false, the consumer pool is not created — no consumer group join, no message
// consumption. WebSocket connections still work (connection-only mode for loadtesting).
// Default: true
KafkaConsumerEnabled bool `env:"KAFKA_CONSUMER_ENABLED" envDefault:"true"`
```

**No Validate() change needed** — boolean parsed by `caarlos0/env` is always true/false, no range to validate.

**Update `Print()`** — add after `Consumer Group` line (after line 513, before `\n=== Kafka Security ===`):
```go
fmt.Printf("Consumer Enabled: %v\n", c.KafkaConsumerEnabled)
```

**Update `LogConfig()`** — add field (after `consumer_group` field, line 600):
```go
Bool("kafka_consumer_enabled", c.KafkaConsumerEnabled).
```

### 2. `ws/internal/shared/types/types.go`

Add to `ServerConfig` struct after `KafkaProducer` (line 40):
```go
KafkaConsumerDisabled bool // True when KAFKA_CONSUMER_ENABLED=false (connection-only mode)
```

### 3. `ws/cmd/server/main.go`

**Wrap provisioning DB + pool creation** (lines 190-236) inside existing `if len(kafkaBrokers) > 0` block.

The provisioning DB and topic registry (lines 190-213) are **exclusively** used by the consumer pool constructor. When consumer is disabled, they must also be skipped — otherwise an unreachable DB would crash the server at line 205 (`Fatalf`), defeating the purpose of the toggle.

```go
if cfg.KafkaConsumerEnabled {
    // ... existing provisioning DB + topic registry + pool creation code (lines 190-236) ...
} else {
    logger.Printf("Kafka consumer DISABLED (KAFKA_CONSUMER_ENABLED=false) — connection-only mode for loadtesting")
}
```

Producer creation (lines 238-259) stays **outside** the guard — clients can still publish.

**Pass flag to shard config** (line ~308 area, inside shardConfig struct literal):
```go
KafkaConsumerDisabled: !cfg.KafkaConsumerEnabled,
```

### 4. `ws/internal/server/handlers_http.go`

**Replace** Kafka check block (lines 49-59). Update comment from "critical dependency" to reflect optional classification per Degradation Decision Matrix. When intentionally disabled, report healthy with status "disabled" — don't log error or set unhealthy:
```go
// Check Kafka consumer (optional — see Degradation Decision Matrix in CODING_GUIDELINES.md)
kafkaStatus := "stopped"
kafkaHealthy := false
if s.config.KafkaConsumerDisabled {
    kafkaStatus = "disabled"
    kafkaHealthy = true
} else if s.kafkaConsumer != nil {
    kafkaStatus = "running"
    kafkaHealthy = true
} else {
    isHealthy = false
    errors = append(errors, "Kafka consumer not initialized")
    s.logger.Error().Msg("Health check failed: Kafka consumer not initialized")
}
```

### 5. Helm chart (`deployments/helm/odin/charts/ws-server/`)

**`values.yaml`** — add under `kafka:` section (after `consumerGroup`, line 147):
```yaml
  # Kafka consumer toggle (for loadtesting — connection-only mode)
  # When false, consumer pool is not started. WebSocket connections still work.
  # Default: true (normal operation)
  consumerEnabled: true
```

**`templates/configmap.yaml`** — add after `KAFKA_TOPICS` (line 60):
```yaml
  # Kafka consumer toggle (connection-only mode for loadtesting)
  KAFKA_CONSUMER_ENABLED: "{{ .Values.kafka.consumerEnabled }}"
```

## What does NOT change

- No changes to `consumer.go`, `multitenant_pool.go`, `metrics.go`, `server.go`
- No new shared code, no new types, no new interfaces

## Verification

1. `cd ws && go test ./...` — existing tests pass (default `true`, no behavior change)
2. Set `KAFKA_CONSUMER_ENABLED=false` in `.env`, start server locally, confirm:
   - `/health` returns 200 with `kafka.status: "disabled"`, `kafka.healthy: true`
   - Logs show "Kafka consumer DISABLED"
   - WebSocket connections still accepted and sustained
