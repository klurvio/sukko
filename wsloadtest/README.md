# wsloadtest

WebSocket load testing tool for the Sukko platform. Tests connections, subscriptions, and message throughput.

## Build

```bash
go build -o wsloadtest .
```

## Usage

```bash
./wsloadtest [flags]
```

### Flags

| Flag | Env Var | Default | Description |
|------|---------|---------|-------------|
| `-url` | `WS_URL` | `ws://localhost:3000/ws` | WebSocket server URL |
| `-health` | `HEALTH_URL` | `http://localhost:3000/health` | Health check URL |
| `-connections` | `TARGET_CONNECTIONS` | `1000` | Target number of connections |
| `-ramp-rate` | `RAMP_RATE` | `100` | Connections per second during ramp-up |
| `-duration` | `DURATION` | `30m` | Sustain duration after ramp-up |
| `-timeout` | `CONNECTION_TIMEOUT` | `10s` | Connection timeout |
| `-channels` | `CHANNELS` | 6 default channels | Comma-separated channels |
| `-mode` | `SUBSCRIPTION_MODE` | `random` | all/single/random |
| `-channels-per-client` | `CHANNELS_PER_CLIENT` | `3` | Channels per client (random mode) |
| `-tenant` | `TENANT_ID` | `sukko` | Tenant for JWT generation |
| `-token` | `JWT_TOKEN` | - | Pre-generated JWT token |
| `-jwt-secret` | `JWT_SECRET` | - | JWT secret for token generation |
| `-principal` | `PRINCIPAL` | `loadtest-user` | Principal for JWT |
| `-report-interval` | `REPORT_INTERVAL` | `10s` | Stats report interval |
| `-health-interval` | `HEALTH_INTERVAL` | `5s` | Health check interval |
| `-log-level` | `LOG_LEVEL` | `info` | Log level |

### Default Channels

- `sukko.all.trade` - Aggregate trades (all tokens)
- `sukko.BTC.trade` - BTC trades
- `sukko.ETH.trade` - ETH trades
- `sukko.SOL.trade` - SOL trades
- `sukko.BTC.orderbook` - BTC orderbook updates
- `sukko.ETH.liquidity` - ETH liquidity updates

## Examples

### Basic Test (Local)

```bash
./wsloadtest -connections 100 -ramp-rate 10 -duration 1m
```

### Production Test (GKE)

```bash
./wsloadtest \
  -url wss://ws.sukko.example.com/ws \
  -connections 5000 \
  -ramp-rate 100 \
  -duration 10m \
  -jwt-secret "$JWT_SECRET"
```

### Debug Mode

```bash
./wsloadtest \
  -connections 5 \
  -ramp-rate 1 \
  -duration 1m \
  -log-level debug
```

## Subscription Modes

| Mode | Description |
|------|-------------|
| `all` | Each client subscribes to all configured channels |
| `single` | Each client subscribes to one random channel |
| `random` | Each client subscribes to N random channels (set by `-channels-per-client`) |

## Metrics

The tool reports:

- `active_connections` - Current active connections
- `total_created` - Total connections created
- `failed_connections` - Failed connection attempts
- `messages_received` - Total messages received
- `message_rate` - Messages per second
- `subscriptions_sent` - Subscribe messages sent
- `subscriptions_confirmed` - Subscription acknowledgements received
- `subscriptions_failed` - Subscription errors received

## Infrastructure

For high connection counts (5000+), tune system limits:

```bash
# /etc/security/limits.conf
* soft nofile 1000000
* hard nofile 1000000

# /etc/sysctl.conf
net.ipv4.ip_local_port_range = 1024 65535
net.ipv4.tcp_tw_reuse = 1
net.core.somaxconn = 65535
```
