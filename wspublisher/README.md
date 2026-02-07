# wspublisher

CDC message publisher that publishes to Redpanda/Kafka following the [asyncapi spec](../ws/docs/asyncapi/redpanda.asyncapi.yaml).

## Message Format

Following the asyncapi spec:

```
Topic: {namespace}.{tenant}.{category}    e.g., local.odin.trade
Key:   {tenant}.{identifier}.{category}   e.g., odin.BTC.trade
Value: JSON payload (forwarded as-is to WebSocket clients)
```

The Kafka key becomes the WebSocket channel that clients subscribe to.

## Quick Start

```bash
# Build
go build -o wspublisher .

# Run locally
KAFKA_BROKERS=localhost:19092 \
KAFKA_NAMESPACE=local \
CHANNELS=odin.BTC.trade,odin.ETH.trade,odin.all.trade \
./wspublisher
```

## Configuration

### Required

| Variable | Description |
|----------|-------------|
| `KAFKA_BROKERS` | Comma-separated broker list |
| `KAFKA_NAMESPACE` | Environment: `local`, `dev`, `stag`, `prod` |

### Channels

| Variable | Default | Description |
|----------|---------|-------------|
| `CHANNELS` | - | Static channel list (like wsloadtest) |
| `CHANNEL_PATTERN` | `{tenant}.{identifier}.{category}` | Pattern for dynamic channels |
| `TENANT_ID` | `odin` | Tenant for dynamic channels |
| `IDENTIFIERS` | `BTC,ETH,SOL,all` | Identifiers for dynamic channels |
| `CATEGORIES` | `trade,liquidity,orderbook` | Categories for dynamic channels (synced with wsloadtest) |

### Timing

| Variable | Default | Description |
|----------|---------|-------------|
| `TIMING_MODE` | `poisson` | `poisson`, `uniform`, or `burst` |
| `POISSON_LAMBDA` | `100` | Events/sec for poisson mode |
| `MIN_INTERVAL` | `10ms` | Min delay for uniform mode |
| `MAX_INTERVAL` | `1s` | Max delay for uniform mode |
| `BURST_COUNT` | `10` | Messages per burst |
| `BURST_PAUSE` | `1s` | Pause between bursts |
| `DURATION` | `0` | Run duration (0 = infinite) |

### Security

| Variable | Default | Description |
|----------|---------|-------------|
| `KAFKA_SASL_MECHANISM` | - | `scram-sha-256` or `scram-sha-512` |
| `KAFKA_SASL_USERNAME` | - | SASL username |
| `KAFKA_SASL_PASSWORD` | - | SASL password |
| `KAFKA_TLS_ENABLED` | `false` | Enable TLS |
| `ALLOW_PROD` | `false` | Allow production namespace |

## Examples

```bash
# Like wsloadtest channels
KAFKA_BROKERS=localhost:19092 \
KAFKA_NAMESPACE=local \
CHANNELS=odin.all.trade,odin.BTC.trade,odin.ETH.trade,odin.SOL.trade \
TIMING_MODE=poisson \
POISSON_LAMBDA=500 \
DURATION=5m \
./wspublisher

# Dynamic channel generation
KAFKA_BROKERS=localhost:19092 \
KAFKA_NAMESPACE=local \
TENANT_ID=odin \
IDENTIFIERS=BTC,ETH,SOL,all \
CATEGORIES=trade,liquidity,orderbook \
./wspublisher
```

## Docker

```bash
docker build -t wspublisher .
docker run -e KAFKA_BROKERS=host.docker.internal:19092 \
           -e KAFKA_NAMESPACE=local \
           -e CHANNELS=odin.BTC.trade \
           wspublisher
```
