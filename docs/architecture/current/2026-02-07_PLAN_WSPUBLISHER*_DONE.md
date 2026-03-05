# Plan: wspublisher - Go-based CDC Message Publisher

**Status:** Implemented

**Scope:** Create a Go-based publisher that publishes CDC messages to random channels at random times directly to Redpanda.

---

## Summary

- **New tool**: `wspublisher/` - Go application for random CDC message publishing
- **Safety**: Isolated namespace, refuses prod without explicit flag
- **No deployment scripts or taskfiles** - run directly or via existing patterns

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                                                                  │
│  ┌────────────────────┐              ┌──────────────────────┐   │
│  │  Redpanda          │◄─────────────│  wspublisher         │   │
│  │  (Kafka)           │   Publishes  │  (Go binary)         │   │
│  │                    │   to random  │                      │   │
│  │  Topics:           │   channels   │  Run locally or      │   │
│  │  local.sukko.*      │              │  in container        │   │
│  └────────────────────┘              └──────────────────────┘   │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

**Key Concept**: The Kafka message **key** = the broadcast **channel** that clients subscribe to.

---

## File Structure

```
wspublisher/
├── main.go              # Entry point, signal handling
├── config.go            # Configuration parsing & validation
├── config_test.go       # Config tests
├── publisher.go         # Kafka producer (franz-go)
├── publisher_test.go    # Publisher tests
├── generator.go         # Message & channel generation
├── generator_test.go    # Generator tests
├── scheduler.go         # Random timing (Poisson/uniform/burst)
├── scheduler_test.go    # Scheduler tests
├── runner.go            # Main run loop
├── stats.go             # Metrics collection
├── stats_test.go        # Stats tests
├── Dockerfile           # Multi-stage build
├── go.mod
├── go.sum
└── README.md
```

---

## Configuration

### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `KAFKA_BROKERS` | Yes | - | Comma-separated broker list |
| `KAFKA_NAMESPACE` | Yes | - | Environment: local/dev/stag/prod |
| `CHANNELS` | No | - | Static channel list (comma-separated) |
| `CHANNEL_PATTERN` | No | `{tenant}.{identifier}.{category}` | Pattern for dynamic channels |
| `IDENTIFIERS` | No | `BTC,ETH,SOL,all` | Identifiers for pattern |
| `CATEGORIES` | No | `trade,liquidity,orderbook` | Categories for pattern (synced with wsloadtest) |
| `TENANT_ID` | No | `sukko` | Default tenant in channels |
| `TIMING_MODE` | No | `poisson` | poisson/uniform/burst |
| `POISSON_LAMBDA` | No | `100` | Events/second for Poisson |
| `MIN_INTERVAL` | No | `10ms` | Min interval for uniform |
| `MAX_INTERVAL` | No | `1s` | Max interval for uniform |
| `BURST_COUNT` | No | `10` | Messages per burst |
| `BURST_PAUSE` | No | `1s` | Pause between bursts |
| `DURATION` | No | `0` | Run duration (0=infinite) |
| `LOG_LEVEL` | No | `info` | debug/info/warn/error |
| `KAFKA_SASL_MECHANISM` | No | - | scram-sha-256/scram-sha-512 |
| `KAFKA_SASL_USERNAME` | No | - | SASL username |
| `KAFKA_SASL_PASSWORD` | No | - | SASL password |
| `KAFKA_TLS_ENABLED` | No | `false` | Enable TLS |
| `ALLOW_PROD` | No | `false` | Allow production namespace |

---

## Timing Modes

### 1. Poisson (default)
Realistic random arrival times. Lambda=100 means ~100 events/sec with natural variation.

```go
delay := -math.Log(rand.Float64()) / lambda * time.Second
```

### 2. Uniform
Random interval between min and max.

```go
delay := minInterval + rand.Duration(maxInterval - minInterval)
```

### 3. Burst
Send N messages rapidly, then pause.

```go
// Send 10 messages with 1ms delay, then pause 1s
```

---

## Message Format

Following the [asyncapi spec](../../ws/docs/asyncapi/redpanda.asyncapi.yaml):

```
Topic: {namespace}.{tenant}.{category}
       local.sukko.trade

Key:   {tenant}.{identifier}.{category}
       sukko.BTC.trade

Value: JSON payload (opaque, forwarded as-is to clients)
{
  "timestamp": 1707321600000,
  "token": "BTC",
  "price": 45123.50,
  "volume": 0.5,
  "side": "buy"
}
```

The payload schema is owned by the publishing service. The WebSocket platform treats it as opaque and forwards it as-is to subscribed clients.

---

## Sync with wsloadtest

wspublisher and wsloadtest use the same channel format and defaults:

| Tool | Default Channels |
|------|------------------|
| wsloadtest | `sukko.all.trade,sukko.BTC.trade,sukko.ETH.trade,sukko.SOL.trade,sukko.BTC.orderbook,sukko.ETH.liquidity` |
| wspublisher | Generated from: IDENTIFIERS=`BTC,ETH,SOL,all` × CATEGORIES=`trade,liquidity,orderbook` |

Both follow the asyncapi spec channel format: `{tenant}.{identifier}.{category}`

---

## Safety Measures

| Measure | Implementation |
|---------|----------------|
| Namespace validation | Only allows: local, dev, stag, prod |
| Prod protection | Refuses to start if namespace=prod without ALLOW_PROD=true |

---

## Usage

```bash
# Build
cd wspublisher
go build -o wspublisher .

# Run locally (connects to local Redpanda)
KAFKA_BROKERS=localhost:19092 \
KAFKA_NAMESPACE=local \
CHANNELS=sukko.BTC.trade,sukko.ETH.trade \
TIMING_MODE=poisson \
POISSON_LAMBDA=100 \
DURATION=1m \
./wspublisher

# Run with Docker
docker build -t wspublisher .
docker run -e KAFKA_BROKERS=host.docker.internal:19092 \
           -e KAFKA_NAMESPACE=local \
           -e CHANNELS=sukko.BTC.trade,sukko.ETH.trade \
           wspublisher
```

---

## Key Dependencies

```go
// go.mod
require (
    github.com/twmb/franz-go v1.18.0      // Kafka client
    github.com/rs/zerolog v1.31.0         // Structured logging
    github.com/caarlos0/env/v10 v10.0.0   // Env parsing
)
```

---

## Verification

1. Binary builds successfully: `go build`
2. All tests pass: `go test -v ./...`
3. Publishes to local Redpanda with random timing
4. Messages appear in correct topics with random channels
5. Timing distribution matches selected mode
6. Refuses to start with namespace=prod

---

## Files Summary

| Path | Action |
|------|--------|
| `wspublisher/main.go` | Created |
| `wspublisher/config.go` | Created |
| `wspublisher/config_test.go` | Created |
| `wspublisher/scheduler.go` | Created |
| `wspublisher/scheduler_test.go` | Created |
| `wspublisher/generator.go` | Created |
| `wspublisher/generator_test.go` | Created |
| `wspublisher/publisher.go` | Created |
| `wspublisher/publisher_test.go` | Created |
| `wspublisher/runner.go` | Created |
| `wspublisher/stats.go` | Created |
| `wspublisher/stats_test.go` | Created |
| `wspublisher/Dockerfile` | Created |
| `wspublisher/go.mod` | Created |
| `wspublisher/README.md` | Created |
