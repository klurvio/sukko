# WebSocket Server Configuration

All server parameters can be configured via environment variables.

## Environment Variables

### Dynamic Capacity Thresholds

| Variable | Default | Description |
|----------|---------|-------------|
| `CPU_REJECT_THRESHOLD` | `80.0` | Reject new connections when CPU exceeds this % (e.g., `75.0` for earlier rejection) |
| `CPU_PAUSE_THRESHOLD` | `85.0` | Pause NATS message consumption when CPU exceeds this % (activates backpressure) |
| `CPU_TARGET_MAX` | `70.0` | Target maximum CPU usage % for capacity calculations (aim to stay below this) |

**Tuning Guide:**
- **Low resources (e2-micro)**: Set `CPU_REJECT_THRESHOLD=75` and `CPU_TARGET_MAX=65` for more headroom
- **High resources (n2-standard)**: Set `CPU_REJECT_THRESHOLD=85` and `CPU_TARGET_MAX=75` for higher utilization
- **Critical systems**: Lower all thresholds by 10-15% for extra safety margin

### Capacity Limits

| Variable | Default | Description |
|----------|---------|-------------|
| `MIN_CONNECTIONS` | `50` | Minimum connection limit (server starts here and scales up) |
| `MAX_CAPACITY` | `10000` | Maximum connection limit (hard cap for safety) |
| `SAFETY_MARGIN` | `0.9` | Safety margin multiplier (0.9 = use 90% of calculated capacity) |

**Tuning Guide:**
- **Conservative deployment**: `MIN_CONNECTIONS=25`, `SAFETY_MARGIN=0.8`
- **Aggressive scaling**: `MIN_CONNECTIONS=100`, `SAFETY_MARGIN=0.95`
- **Shared infrastructure**: Lower `MAX_CAPACITY` to leave resources for other services

### JetStream Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `JS_STREAM_NAME` | `SUKKO_TOKENS` | Name of JetStream stream for token updates |
| `JS_STREAM_MAX_AGE` | `30s` | Maximum age of messages in stream (e.g., `1m`, `5m`) |
| `JS_STREAM_MAX_MSGS` | `100000` | Maximum number of messages in stream |
| `JS_STREAM_MAX_BYTES` | `52428800` | Maximum bytes in stream (default: 50MB) |
| `JS_CONSUMER_NAME` | `ws-server` | Durable consumer name (survives restarts) |
| `JS_CONSUMER_ACK_WAIT` | `30s` | Time to wait for ack before redelivery (e.g., `15s`, `1m`) |

**Tuning Guide:**
- **High-volume trading**: `JS_STREAM_MAX_MSGS=500000`, `JS_STREAM_MAX_BYTES=262144000` (250MB)
- **Low-latency priority**: `JS_STREAM_MAX_AGE=10s` (discard old data quickly)
- **Reliable delivery**: `JS_CONSUMER_ACK_WAIT=1m` (more time to process before retry)

### Monitoring Intervals

| Variable | Default | Description |
|----------|---------|-------------|
| `METRICS_INTERVAL` | `15s` | How often to collect and update Prometheus metrics |
| `CAPACITY_INTERVAL` | `30s` | How often to recalculate dynamic capacity limits |

**Tuning Guide:**
- **Fast response**: `CAPACITY_INTERVAL=15s` (quicker scaling reactions)
- **Stable environment**: `CAPACITY_INTERVAL=1m` (less churn, more stable limits)
- **Heavy monitoring**: `METRICS_INTERVAL=5s` (more granular metrics)
- **Resource-constrained**: `METRICS_INTERVAL=30s` (lower overhead)

## Usage Examples

### Local Development

```bash
# Default configuration (sensible defaults)
docker compose up -d
```

### Conservative Production (e2-micro)

```bash
# Lower thresholds, smaller capacity
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d \
  -e CPU_REJECT_THRESHOLD=75 \
  -e CPU_PAUSE_THRESHOLD=80 \
  -e CPU_TARGET_MAX=65 \
  -e MIN_CONNECTIONS=25 \
  -e MAX_CAPACITY=500 \
  -e SAFETY_MARGIN=0.8
```

### High-Performance Production (n2-standard-4)

```bash
# Higher thresholds, larger capacity
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d \
  -e CPU_REJECT_THRESHOLD=85 \
  -e CPU_PAUSE_THRESHOLD=90 \
  -e CPU_TARGET_MAX=75 \
  -e MIN_CONNECTIONS=100 \
  -e MAX_CAPACITY=5000 \
  -e SAFETY_MARGIN=0.95 \
  -e JS_STREAM_MAX_MSGS=500000 \
  -e JS_STREAM_MAX_BYTES=262144000
```

### Fast-Scaling Configuration

```bash
# Quicker reactions to load changes
docker compose up -d \
  -e CAPACITY_INTERVAL=15s \
  -e METRICS_INTERVAL=10s
```

## Adding to docker-compose.yml

To make configuration persistent, add to your `docker-compose.yml`:

```yaml
services:
  ws-go:
    environment:
      # Dynamic capacity thresholds
      - CPU_REJECT_THRESHOLD=80.0
      - CPU_PAUSE_THRESHOLD=85.0
      - CPU_TARGET_MAX=70.0

      # Capacity limits
      - MIN_CONNECTIONS=50
      - MAX_CAPACITY=10000
      - SAFETY_MARGIN=0.9

      # JetStream configuration
      - JS_STREAM_NAME=SUKKO_TOKENS
      - JS_STREAM_MAX_AGE=30s
      - JS_STREAM_MAX_MSGS=100000
      - JS_STREAM_MAX_BYTES=52428800
      - JS_CONSUMER_NAME=ws-server
      - JS_CONSUMER_ACK_WAIT=30s

      # Monitoring intervals
      - METRICS_INTERVAL=15s
      - CAPACITY_INTERVAL=30s
```

## Monitoring Configuration Changes

Server logs will show active configuration on startup:

```
[WS] Configuration:
[WS]   CPU Reject Threshold: 80.0%
[WS]   CPU Pause Threshold: 85.0%
[WS]   CPU Target Max: 70.0%
[WS]   Capacity Range: 50 - 10000 connections
[WS]   JetStream Stream: SUKKO_TOKENS (max age: 30s)
[WS]   Monitoring: metrics=15s, capacity=30s
```

## Grafana Dashboards

All these settings can be monitored in Grafana:

- **Current CPU threshold**: `ws_capacity_cpu_threshold_percent`
- **Current max connections**: `ws_capacity_max_connections`
- **Capacity rejections**: `ws_capacity_rejections_total`
- **CPU/Memory headroom**: `ws_capacity_headroom_percent`
- **Stream metrics**: JetStream admin UI at `http://nats:8222`

## Tuning Recommendations

1. **Start conservative** with default values
2. **Monitor for 24 hours** to understand baseline load
3. **Adjust gradually** (change one parameter at a time)
4. **Use Grafana** to see impact of changes in real-time
5. **Test under load** before applying to production

## Common Scenarios

### Scenario: Server rejecting too many connections

**Symptoms**: High `ws_capacity_rejections_total{reason="cpu_overload"}`

**Solution**:
- Increase `CPU_REJECT_THRESHOLD` (e.g., from 80 to 85)
- OR reduce load (scale horizontally)
- OR upgrade instance type

### Scenario: CPU hitting 100% despite rejections

**Symptoms**: CPU at 100%, connections still accepted

**Solution**:
- Lower `CPU_REJECT_THRESHOLD` (e.g., from 80 to 75)
- Lower `CPU_TARGET_MAX` (e.g., from 70 to 65)
- Check for other processes consuming CPU

### Scenario: Capacity not scaling up

**Symptoms**: `ws_capacity_max_connections` stuck at `MIN_CONNECTIONS`

**Solution**:
- Check CPU headroom in Grafana
- Increase `CPU_TARGET_MAX` to allow more headroom
- Reduce `SAFETY_MARGIN` (e.g., 0.9 → 0.95)

### Scenario: JetStream messages piling up

**Symptoms**: High message count in stream, NAK rate increasing

**Solution**:
- Increase `JS_CONSUMER_ACK_WAIT` for slower processing
- Reduce `CPU_PAUSE_THRESHOLD` to process more messages
- Scale horizontally (add more server instances)
