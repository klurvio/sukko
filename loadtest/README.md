# WebSocket Load Testing Tool

A high-performance Go-based load testing tool for WebSocket servers. This tool uses goroutines to simulate thousands of concurrent WebSocket connections, avoiding the event loop bottleneck present in Node.js-based load testers.

## Why Go?

**Node.js Problem:**
- Single-threaded event loop
- Performance degrades at ~3K-7K connections
- CPU bound on one core at high connection counts
- Event loop becomes the bottleneck, not the server

**Go Solution:**
- Independent goroutines for each connection
- Distributes load across all CPU cores
- Tested up to 12K+ concurrent connections
- No event loop bottleneck

See: `docs/NODE_JS_EVENT_LOOP_BOTTLENECK.md` for detailed analysis.

## Features

- **High Concurrency**: Test with 10K+ simultaneous connections
- **Gradual Ramp-Up**: Configure connections per second during ramp phase
- **Multiple Subscription Modes**: Test different subscription patterns
- **Real-Time Metrics**: Live connection, message, and subscription stats
- **Health Monitoring**: Periodic server health checks during test
- **Graceful Shutdown**: Clean connection teardown on SIGINT/SIGTERM
- **Phase Detection**: Automatic test mode detection (lite/standard/stress)
- **Detailed Reporting**: Comprehensive metrics at completion

## Installation

### Build from Source

```bash
cd loadtest
go build -o loadtest
```

### Using Docker

The test runner Dockerfile builds this tool:

```bash
# From odin-ws root
docker build -t loadtest -f deployments/gcp-distributed/test-runner/Dockerfile --build-context . loadtest/
```

## Usage

### Basic Usage

```bash
./loadtest -connections 1000 -url ws://localhost:3004/ws
```

### Configuration Options

| Flag | Default | Description |
|------|---------|-------------|
| `-url` | `ws://localhost:3004/ws` | WebSocket server URL |
| `-health` | `http://localhost:3004/health` | Health check endpoint URL |
| `-connections` | `7000` | Target number of concurrent connections |
| `-ramp-rate` | `100` | Connections per second during ramp-up |
| `-duration` | `1800` | Sustain duration in seconds (30 minutes) |
| `-channels` | `BTC.trade,ETH.trade,...` | Comma-separated channels to subscribe |
| `-subscription-mode` | `random` | Subscription mode: `all`, `single`, `random` |
| `-channels-per-client` | `3` | Channels per client (random mode only) |
| `-report-interval` | `10` | Statistics report interval in seconds |
| `-health-interval` | `5` | Health check interval in seconds |
| `-connection-timeout` | `10000` | Connection timeout in milliseconds |
| `-max-connections` | `18000` | Server max connections (for test mode detection) |

### Subscription Modes

**1. All Channels (`-subscription-mode all`)**
- Every client subscribes to all channels
- Maximum broadcast load on server
- Use for stress testing broadcast system

**2. Single Channel (`-subscription-mode single`)**
- Each client subscribes to one channel
- Evenly distributes clients across channels
- Use for testing channel isolation

**3. Random Channels (`-subscription-mode random`)**
- Each client subscribes to N random channels
- N configured by `-channels-per-client`
- Realistic simulation of varied client behavior

### Examples

**Light Test (500 connections, 5 minutes):**
```bash
./loadtest \
  -url ws://localhost:3004/ws \
  -connections 500 \
  -ramp-rate 50 \
  -duration 300
```

**Standard Test (2000 connections, 30 minutes):**
```bash
./loadtest \
  -url ws://10.128.0.3:3004/ws \
  -health http://10.128.0.3:3004/health \
  -connections 2000 \
  -ramp-rate 100 \
  -duration 1800
```

**Stress Test (12000 connections, 1 hour):**
```bash
./loadtest \
  -url ws://10.128.0.3:3004/ws \
  -connections 12000 \
  -ramp-rate 200 \
  -duration 3600 \
  -subscription-mode random \
  -channels-per-client 5
```

**Broadcast Stress (all clients, all channels):**
```bash
./loadtest \
  -url ws://localhost:3004/ws \
  -connections 5000 \
  -subscription-mode all \
  -channels "BTC.trade,ETH.trade,SOL.trade,ODIN.trade,DOGE.trade"
```

**Channel Isolation Test (one channel per client):**
```bash
./loadtest \
  -url ws://localhost:3004/ws \
  -connections 5000 \
  -subscription-mode single \
  -channels "BTC.trade,ETH.trade,SOL.trade,ODIN.trade,DOGE.trade"
```

## Docker Usage

### Using Pre-built Image

```bash
docker run --rm \
  -e WS_URL=ws://host.docker.internal:3004/ws \
  -e TARGET_CONNECTIONS=1000 \
  -e RAMP_RATE=100 \
  -e DURATION=300 \
  loadtest
```

### Environment Variables

When running in Docker, you can use environment variables instead of flags:

| Environment Variable | Flag Equivalent |
|---------------------|-----------------|
| `WS_URL` | `-url` |
| `HEALTH_URL` | `-health` |
| `TARGET_CONNECTIONS` | `-connections` |
| `RAMP_RATE` | `-ramp-rate` |
| `DURATION` | `-duration` |
| `CHANNELS` | `-channels` |
| `SUBSCRIPTION_MODE` | `-subscription-mode` |
| `CHANNELS_PER_CLIENT` | `-channels-per-client` |

**Note:** Flags take precedence over environment variables.

## Output Metrics

### During Test

Every 10 seconds (configurable), the tool reports:

```
[00:01:30] RAMPING Phase
  Connections: 900/7000 (active/target)
  Created: 900, Failed: 0
  Messages: 15234 received (254/sec avg, 312/sec current)
  Subscriptions: 900 sent, 898 confirmed, 2 pending
  Filtered: 0 messages (0.00%)
  
  Server Health:
  ✅ Status: healthy
  📊 CPU: 45.2% | Memory: 62.3% | Active Conns: 900
```

### Test Phases

1. **RAMPING**: Gradually creating connections at configured rate
2. **SUSTAINING**: All connections established, monitoring for duration
3. **COMPLETED**: Test finished, final statistics shown

### Final Report

At completion, a comprehensive summary is displayed:

```
================================================================================
LOAD TEST COMPLETED
================================================================================

Test Configuration:
  Target Connections: 7000
  Ramp Rate: 100 conn/sec
  Duration: 1800 seconds (30m0s)
  Test Mode: STANDARD (2K-10K connections)

Connection Statistics:
  Total Created: 7000
  Peak Active: 6998
  Failed: 2 (0.03%)
  Success Rate: 99.97%

Message Statistics:
  Total Received: 1,234,567
  Rate: 685.9 msg/sec average
  Filtered Out: 12 (0.00%)

Subscription Statistics:
  Sent: 7000
  Confirmed: 6998 (99.97%)
  Failed: 2 (0.03%)

Timing:
  Ramp Phase: 1m10s
  Sustain Phase: 30m0s
  Total Duration: 31m10s

Final Server State:
  Status: healthy
  CPU: 42.1%
  Memory: 58.7%
  Active Connections: 6998
```

## GCP Deployment

### Deploy to Test Runner VM

```bash
# Deploy test runner to dedicated VM
task gcp2:deploy:test-runner-go

# Run test from local machine (controls remote VM)
task gcp2:run:stress-test-7k
task gcp2:run:stress-test-12k
```

### Run on Test Runner VM

```bash
# SSH to test runner
gcloud compute ssh odin-test-runner --zone=us-central1-a

# Run test
docker run -d --name loadtest \
  -e WS_URL=ws://10.128.0.3:3004/ws \
  -e HEALTH_URL=http://10.128.0.3:3004/health \
  -e TARGET_CONNECTIONS=12000 \
  -e RAMP_RATE=200 \
  -e DURATION=3600 \
  loadtest

# Monitor logs
docker logs -f loadtest
```

## Architecture

### Connection Flow

```
1. Dial WebSocket → 2. Send Subscribe → 3. Receive Confirmation → 4. Receive Messages
```

Each connection runs in its own goroutine with:
- **Connect goroutine**: Establishes connection, sends subscription
- **Read goroutine**: Continuously reads messages
- **Graceful shutdown**: Cleans up on context cancellation

### Goroutine Model

```
Main Goroutine
├── Stats Reporter (every 10s)
├── Health Checker (every 5s)
└── N Connection Goroutines
    ├── Connect & Subscribe
    └── Message Reader (continuous)
```

### Resource Usage

**Per Connection:**
- Goroutine: ~2-8 KB stack
- Buffers: ~4 KB (read/write)
- Overhead: ~6-12 KB total

**Example (7000 connections):**
- Memory: ~50-100 MB
- Goroutines: ~7000-8000
- CPU: Distributed across all cores

## Performance Characteristics

### Tested Limits

| Connections | Ramp Time | Memory | CPU Cores | Result |
|------------|-----------|--------|-----------|--------|
| 1,000 | 10s | 15 MB | 1 | ✅ Stable |
| 5,000 | 50s | 70 MB | 2 | ✅ Stable |
| 7,000 | 70s | 95 MB | 2 | ✅ Stable |
| 12,000 | 120s | 160 MB | 4 | ✅ Stable |
| 15,000 | 150s | 200 MB | 4 | ⚠️ Check server |

### Bottlenecks

**Client-side (this tool):**
- Goroutine limit: ~50K+ (OS dependent)
- Memory: ~10-15 MB per 1K connections
- Network: Local port exhaustion at ~64K connections (single IP)

**Server-side:**
- CPU usage (message broadcasts)
- Memory usage (connection buffers)
- Network bandwidth (egress)
- Connection limit (configured max)

**Solution for >64K connections:**
- Use multiple client IPs
- Deploy multiple test runner instances
- Use NAT with multiple external IPs

## Error Handling

The tool tracks and reports:

**Connection Errors:**
- Dial failures
- Timeout errors
- Connection refused
- Protocol errors

**Subscription Errors:**
- Missing confirmations
- Rejected subscriptions
- Timeout waiting for confirmation

**Message Errors:**
- Invalid JSON
- Unexpected message types
- Connection drops during test

**All errors are categorized and counted in final report.**

## Comparison: Go vs Node.js

| Metric | Node.js | Go (This Tool) |
|--------|---------|----------------|
| Event Loop | Single-threaded | No event loop |
| Max Connections | ~7K (bottleneck) | 50K+ (tested 12K) |
| CPU Usage | 1 core maxed | All cores utilized |
| Memory (7K conns) | ~250 MB | ~95 MB |
| Connection Rate | Degrades >3K | Stable >12K |
| Code Complexity | Callback hell | Straightforward |

## Troubleshooting

### "Too many open files"

Increase file descriptor limit:

```bash
# Temporary
ulimit -n 65536

# Permanent (Linux)
echo "* soft nofile 65536" >> /etc/security/limits.conf
echo "* hard nofile 65536" >> /etc/security/limits.conf
```

### "Cannot assign requested address"

Local port exhaustion. Solutions:

1. **Increase port range:**
```bash
# Linux
sysctl -w net.ipv4.ip_local_port_range="1024 65535"

# macOS
sysctl -w net.inet.ip.portrange.first=10000
sysctl -w net.inet.ip.portrange.last=65535
```

2. **Enable port reuse:**
```bash
sysctl -w net.ipv4.tcp_tw_reuse=1
```

3. **Use multiple IPs** (for >64K connections)

### Slow ramp-up

- Increase `-ramp-rate`
- Check network latency
- Verify server isn't rejecting connections
- Monitor server CPU/memory during ramp

### Low message rate

- Ensure NATS publisher is running
- Check server broadcast rate limits
- Verify subscription confirmations
- Check `-subscription-mode` is appropriate

## Development

### Build

```bash
cd loadtest
go build -o loadtest
```

### Run Tests (if added)

```bash
go test ./...
```

### Cross-Compile

```bash
# Linux
GOOS=linux GOARCH=amd64 go build -o loadtest-linux

# Windows
GOOS=windows GOARCH=amd64 go build -o loadtest.exe

# macOS
GOOS=darwin GOARCH=arm64 go build -o loadtest-macos
```

## Dependencies

- **gorilla/websocket** - WebSocket client implementation
- **Go 1.23+** - Required for standard library features

Install dependencies:

```bash
cd loadtest
go mod download
```

## Related Documentation

- `docs/NODE_JS_EVENT_LOOP_BOTTLENECK.md` - Why we switched from Node.js
- `docs/LOAD_TESTING.md` - Load testing strategy and scenarios
- `deployments/gcp-distributed/README.md` - GCP deployment architecture
- `taskfiles/isolated-setup.yml` - GCP deployment automation

## License

Same as parent project.

---

**Built with ❤️ in Go**
