# Plan: Refactor WebSocket Load Testing Tool

**Status:** Implemented
**Date:** 2026-02-06
**Scope:** Rename and refactor `loadtest/` → `wsloadtest/` to follow coding guidelines and support full subscription testing

---

## Summary

The existing `loadtest/` tool has the core features needed but requires refactoring to:
1. Follow the coding guidelines (no global state, structured logging, config validation)
2. Support explicit tenant-prefixed channels (`sukko.BTC.trade`)
3. Add proper unit tests
4. Improve code organization
5. Align with the AsyncAPI spec (`ws/docs/asyncapi/bundled.yaml`)

**Rename:** `loadtest/` → `wsloadtest/` (Go idiomatic: lowercase, no underscores, descriptive)

**Why rename:** The tool tests WebSocket connections, subscriptions, and message receiving. `wsloadtest` is more accurate than generic `loadtest`.

---

## Protocol Reference

All message formats must conform to `ws/docs/asyncapi/bundled.yaml`:

### Client → Server Messages

| Type | Format | Reference |
|------|--------|-----------|
| `subscribe` | `{"type":"subscribe","data":{"channels":["sukko.BTC.trade"]}}` | `#/components/messages/subscribe` |
| `unsubscribe` | `{"type":"unsubscribe","data":{"channels":["sukko.BTC.trade"]}}` | `#/components/messages/unsubscribe` |
| `heartbeat` | `{"type":"heartbeat","data":{}}` | `#/components/messages/heartbeat` |

### Server → Client Messages

| Type | Format | Reference |
|------|--------|-----------|
| `subscription_ack` | `{"type":"subscription_ack","subscribed":[...],"count":N}` | `#/components/schemas/SubscriptionAck` |
| `unsubscription_ack` | `{"type":"unsubscription_ack","unsubscribed":[...],"count":N}` | `#/components/schemas/UnsubscriptionAck` |
| `message` | `{"type":"message","seq":N,"ts":N,"channel":"...","data":{}}` | `#/components/schemas/MessageEnvelope` |
| `pong` | `{"type":"pong","ts":N}` | `#/components/schemas/Pong` |
| `subscribe_error` | `{"type":"subscribe_error","code":"...","message":"..."}` | `#/components/schemas/SubscribeError` |

### Channel Format

Per AsyncAPI spec:
- Format: `{tenant}.{identifier}.{category}`
- Minimum 3 dot-separated parts
- Examples: `sukko.BTC.trade`, `sukko.all.liquidity`, `sukko.ETH.metadata`

---

## Current State Analysis

### What Works Well
- Ramp-up with configurable rate
- Target connection count
- Persistent connections (readPump/writePump)
- Message receiving and counting
- Subscription modes (all, single, random)
- Health monitoring
- JWT authentication
- Graceful shutdown

### What Needs Fixing

| Issue | Guideline Violated | Fix |
|-------|-------------------|-----|
| Global `state` and `config` vars | "No Hacks" - global state is hard to test | Inject via struct fields |
| No configuration validation | "Configuration Validation" | Add `Validate()` method |
| Uses `log.Printf` | "Structured Logging with zerolog" | Switch to zerolog |
| Emojis in output | "Only use emojis if user requests" | Remove emojis |
| Old channel format (`BTC.trade`) | Explicit channels refactor | Use `sukko.BTC.trade` |
| 836-line single file | Maintainability | Split into logical files |
| No unit tests | "Testing" guidelines | Add table-driven tests |
| Uses gorilla/websocket | Consistency with project | Keep gorilla (acceptable for client) |
| No panic recovery | "Panic Recovery Logging" | Add defer recover in goroutines |

---

## New Directory Structure

```
wsloadtest/
├── main.go              # Entry point only (~50 lines)
├── config.go            # Config struct, parsing, validation (~150 lines)
├── runner.go            # LoadRunner orchestration (~200 lines)
├── connection.go        # Connection struct and lifecycle (~250 lines)
├── protocol.go          # Message types matching AsyncAPI spec (~100 lines)
├── stats.go             # Stats collection and reporting (~150 lines)
├── health.go            # Health check logic (~50 lines)
├── config_test.go       # Config validation tests
├── connection_test.go   # Connection unit tests
├── protocol_test.go     # Protocol message tests
├── stats_test.go        # Stats calculation tests
├── go.mod
├── go.sum
├── Dockerfile
└── README.md
```

---

## Configuration Changes

### Current Config (Implicit Channels)
```go
Channels: []string{"BTC.trade", "ETH.trade"}  // Missing tenant prefix
```

### New Config (Explicit Tenant-Prefixed)
```go
type Config struct {
    // Connection
    WSURL             string        `env:"WS_URL" envDefault:"ws://localhost:3000/ws"`
    HealthURL         string        `env:"HEALTH_URL" envDefault:"http://localhost:3000/health"`
    TargetConnections int           `env:"TARGET_CONNECTIONS" envDefault:"1000"`
    RampRate          int           `env:"RAMP_RATE" envDefault:"100"`  // connections per second
    SustainDuration   time.Duration `env:"DURATION" envDefault:"30m"`
    ConnectionTimeout time.Duration `env:"CONNECTION_TIMEOUT" envDefault:"10s"`

    // Subscriptions (explicit tenant-prefixed channels per AsyncAPI spec)
    Channels          []string `env:"CHANNELS" envDefault:"sukko.all.trade,sukko.BTC.trade,sukko.ETH.trade,sukko.SOL.trade,sukko.BTC.orderbook,sukko.ETH.liquidity"`
    SubscriptionMode  string   `env:"SUBSCRIPTION_MODE" envDefault:"random"`
    ChannelsPerClient int      `env:"CHANNELS_PER_CLIENT" envDefault:"3"`

    // Authentication
    TenantID  string `env:"TENANT_ID" envDefault:"sukko"`  // For JWT token generation
    Token     string `env:"JWT_TOKEN"`
    JWTSecret string `env:"JWT_SECRET"`
    Principal string `env:"PRINCIPAL" envDefault:"loadtest-user"`

    // Reporting
    ReportInterval time.Duration `env:"REPORT_INTERVAL" envDefault:"10s"`
    HealthInterval time.Duration `env:"HEALTH_INTERVAL" envDefault:"5s"`
    LogLevel       string        `env:"LOG_LEVEL" envDefault:"info"`
}
```

### Channel Format
Channels must be explicit tenant-prefixed format per AsyncAPI spec:
```
{tenant}.{identifier}.{category}
```

Examples:
- `sukko.BTC.trade` — BTC trades for tenant sukko
- `sukko.ETH.liquidity` — ETH liquidity for tenant sukko
- `sukko.all.trade` — All trades (aggregate channel)

### Configuration Validation
```go
func (c *Config) Validate() error {
    if c.WSURL == "" {
        return errors.New("WS_URL is required")
    }
    if c.TargetConnections < 1 {
        return fmt.Errorf("TARGET_CONNECTIONS must be >= 1, got %d", c.TargetConnections)
    }
    if c.RampRate < 1 {
        return fmt.Errorf("RAMP_RATE must be >= 1, got %d", c.RampRate)
    }

    // Validate channels are provided
    if len(c.Channels) == 0 {
        return errors.New("CHANNELS is required (at least one channel)")
    }

    // Validate channels format (must have at least 3 dot-separated parts)
    for _, ch := range c.Channels {
        parts := strings.Split(ch, ".")
        if len(parts) < 3 {
            return fmt.Errorf("channel %q must have format {tenant}.{identifier}.{category}", ch)
        }
    }

    validModes := map[string]bool{"all": true, "single": true, "random": true}
    if !validModes[c.SubscriptionMode] {
        return fmt.Errorf("SUBSCRIPTION_MODE must be all/single/random, got %s", c.SubscriptionMode)
    }

    // Validate ChannelsPerClient for random mode
    if c.SubscriptionMode == "random" {
        if c.ChannelsPerClient < 1 {
            return fmt.Errorf("CHANNELS_PER_CLIENT must be >= 1, got %d", c.ChannelsPerClient)
        }
        if c.ChannelsPerClient > len(c.Channels) {
            return fmt.Errorf("CHANNELS_PER_CLIENT (%d) cannot exceed number of channels (%d)",
                c.ChannelsPerClient, len(c.Channels))
        }
    }

    if err := ValidateLogLevel(c.LogLevel); err != nil {
        return err
    }
    return nil
}
```

---

## Protocol Types (protocol.go)

Define message types matching the AsyncAPI spec exactly:

```go
// Message types (from AsyncAPI #/components/schemas/ClientMessage)
const (
    MsgTypeSubscribe   = "subscribe"
    MsgTypeUnsubscribe = "unsubscribe"
    MsgTypeHeartbeat   = "heartbeat"
)

// Response types (from AsyncAPI spec)
const (
    RespTypeSubscriptionAck   = "subscription_ack"
    RespTypeUnsubscriptionAck = "unsubscription_ack"
    RespTypeMessage           = "message"
    RespTypePong              = "pong"
    RespTypeSubscribeError    = "subscribe_error"
    RespTypeUnsubscribeError  = "unsubscribe_error"
    RespTypeError             = "error"
)

// ClientMessage is the envelope for all client-to-server messages.
// Matches AsyncAPI #/components/schemas/ClientMessage
type ClientMessage struct {
    Type string          `json:"type"`
    Data json.RawMessage `json:"data,omitempty"`
}

// SubscribeData matches AsyncAPI #/components/schemas/SubscribeData
type SubscribeData struct {
    Channels []string `json:"channels"`
}

// SubscriptionAck matches AsyncAPI #/components/schemas/SubscriptionAck
type SubscriptionAck struct {
    Type       string   `json:"type"`
    Subscribed []string `json:"subscribed"`
    Count      int      `json:"count"`
}

// MessageEnvelope matches AsyncAPI #/components/schemas/MessageEnvelope
type MessageEnvelope struct {
    Type    string          `json:"type"`
    Seq     int64           `json:"seq"`
    Ts      int64           `json:"ts"`
    Channel string          `json:"channel"`
    Data    json.RawMessage `json:"data"`
}

// Pong matches AsyncAPI #/components/schemas/Pong
type Pong struct {
    Type string `json:"type"`
    Ts   int64  `json:"ts"`
}

// ErrorResponse matches AsyncAPI error schemas
type ErrorResponse struct {
    Type    string `json:"type"`
    Code    string `json:"code,omitempty"`
    Message string `json:"message"`
}
```

---

## Core Components

### 1. LoadRunner (Orchestrator)

```go
// LoadRunner coordinates the load test
type LoadRunner struct {
    config      *Config
    logger      zerolog.Logger
    stats       *Stats
    connections []*Connection

    ctx    context.Context
    cancel context.CancelFunc
    wg     sync.WaitGroup
}

// Run executes the full load test lifecycle
func (r *LoadRunner) Run(ctx context.Context) error {
    r.ctx, r.cancel = context.WithCancel(ctx)
    defer r.cancel()

    // Start background workers
    r.wg.Add(2)
    go r.healthChecker()
    go r.statsReporter()

    // Ramp up connections
    if err := r.rampUp(); err != nil {
        return fmt.Errorf("ramp up: %w", err)
    }

    // Sustain phase
    r.sustain()

    // Final report
    r.printFinalReport()

    return nil
}
```

### 2. Connection (Per-Client State)

```go
// Connection represents a single WebSocket client
type Connection struct {
    id     int
    ws     *websocket.Conn
    logger zerolog.Logger

    // Subscription state
    channels   []string
    subscribed atomic.Bool

    // Metrics
    messagesReceived atomic.Int64
    sendAttempts     atomic.Int32

    // Lifecycle
    ctx       context.Context
    cancel    context.CancelFunc
    closeOnce sync.Once
    writeMu   sync.Mutex

    connectedAt time.Time
}
```

### 3. Stats (Thread-Safe Metrics)

```go
// Stats collects test metrics using atomic operations
type Stats struct {
    // Connection metrics
    ActiveConnections atomic.Int64
    TotalCreated      atomic.Int64
    FailedConnections atomic.Int64

    // Message metrics
    MessagesReceived atomic.Int64

    // Subscription metrics
    SubscriptionsSent      atomic.Int64
    SubscriptionsConfirmed atomic.Int64
    SubscriptionsFailed    atomic.Int64

    // Timing
    StartTime        time.Time
    RampCompleteTime time.Time
    Phase            atomic.Value // "ramping", "sustaining", "completed"

    // Error tracking
    ErrorCounts sync.Map // map[string]*atomic.Int64
}
```

---

## Structured Logging

### Before (Printf)
```go
log.Printf("🚀 Starting ramp-up: %d connections at %d/sec", config.TargetConnections, config.RampRate)
```

### After (zerolog)
```go
r.logger.Info().
    Int("target_connections", r.config.TargetConnections).
    Int("ramp_rate", r.config.RampRate).
    Msg("Starting ramp-up")
```

### Log Levels
| Level | Usage |
|-------|-------|
| Debug | Per-connection events, message counts |
| Info | Phase changes, final report |
| Warn | Health degraded, slow connections |
| Error | Connection failures, subscription failures |

---

## Testing Plan

### 1. Config Validation Tests (`config_test.go`)

```go
func TestConfig_Validate(t *testing.T) {
    t.Parallel()

    tests := []struct {
        name    string
        config  Config
        wantErr string
    }{
        {
            name:    "empty_ws_url",
            config:  Config{WSURL: ""},
            wantErr: "WS_URL is required",
        },
        {
            name:    "invalid_target_connections",
            config:  Config{WSURL: "ws://localhost", TargetConnections: 0, RampRate: 1},
            wantErr: "TARGET_CONNECTIONS must be >= 1",
        },
        {
            name:    "invalid_ramp_rate",
            config:  Config{WSURL: "ws://localhost", TargetConnections: 1, RampRate: 0},
            wantErr: "RAMP_RATE must be >= 1",
        },
        {
            name:    "invalid_subscription_mode",
            config:  Config{WSURL: "ws://localhost", TargetConnections: 1, RampRate: 1, Channels: []string{"sukko.BTC.trade"}, SubscriptionMode: "invalid"},
            wantErr: "SUBSCRIPTION_MODE must be all/single/random",
        },
        {
            name:    "empty_channels",
            config:  Config{WSURL: "ws://localhost", TargetConnections: 1, RampRate: 1, Channels: []string{}, SubscriptionMode: "all"},
            wantErr: "CHANNELS is required",
        },
        {
            name:    "channels_per_client_zero",
            config:  Config{WSURL: "ws://localhost", TargetConnections: 1, RampRate: 1, Channels: []string{"sukko.BTC.trade"}, SubscriptionMode: "random", ChannelsPerClient: 0},
            wantErr: "CHANNELS_PER_CLIENT must be >= 1",
        },
        {
            name:    "channels_per_client_exceeds_channels",
            config:  Config{WSURL: "ws://localhost", TargetConnections: 1, RampRate: 1, Channels: []string{"sukko.BTC.trade"}, SubscriptionMode: "random", ChannelsPerClient: 5},
            wantErr: "CHANNELS_PER_CLIENT (5) cannot exceed number of channels (1)",
        },
        {
            name:    "invalid_log_level",
            config:  Config{WSURL: "ws://localhost", TargetConnections: 1, RampRate: 1, Channels: []string{"sukko.BTC.trade"}, SubscriptionMode: "all", LogLevel: "invalid"},
            wantErr: "invalid log level",
        },
        {
            name: "valid_config",
            config: Config{
                WSURL:             "ws://localhost",
                TargetConnections: 100,
                RampRate:          10,
                Channels:          []string{"sukko.BTC.trade"},
                TenantID:          "sukko",
                SubscriptionMode:  "random",
                ChannelsPerClient: 1,
                LogLevel:          "info",
            },
            wantErr: "",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            err := tt.config.Validate()
            if tt.wantErr == "" {
                if err != nil {
                    t.Errorf("unexpected error: %v", err)
                }
            } else {
                if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
                    t.Errorf("error = %v, want containing %q", err, tt.wantErr)
                }
            }
        })
    }
}
```

### 2. Channel Validation Tests

```go
func TestConfig_ValidateChannels(t *testing.T) {
    t.Parallel()

    tests := []struct {
        name     string
        channels []string
        wantErr  string
    }{
        {
            name:     "valid_channels",
            channels: []string{"sukko.BTC.trade", "sukko.ETH.liquidity"},
            wantErr:  "",
        },
        {
            name:     "valid_aggregate_channel",
            channels: []string{"sukko.all.trade"},
            wantErr:  "",
        },
        {
            name:     "invalid_two_parts",
            channels: []string{"BTC.trade"},
            wantErr:  "must have format {tenant}.{identifier}.{category}",
        },
        {
            name:     "invalid_one_part",
            channels: []string{"trade"},
            wantErr:  "must have format {tenant}.{identifier}.{category}",
        },
        {
            name:     "mixed_valid_invalid",
            channels: []string{"sukko.BTC.trade", "invalid"},
            wantErr:  "must have format {tenant}.{identifier}.{category}",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            cfg := &Config{
                WSURL:             "ws://localhost",
                TargetConnections: 1,
                RampRate:          1,
                Channels:          tt.channels,
                SubscriptionMode:  "all",
                LogLevel:          "info",
            }
            err := cfg.Validate()
            if tt.wantErr == "" {
                if err != nil {
                    t.Errorf("unexpected error: %v", err)
                }
            } else {
                if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
                    t.Errorf("error = %v, want containing %q", err, tt.wantErr)
                }
            }
        })
    }
}
```

### 3. Protocol Tests (`protocol_test.go`)

```go
func TestSubscribeMessage_Marshal(t *testing.T) {
    t.Parallel()

    msg := ClientMessage{
        Type: MsgTypeSubscribe,
    }
    data := SubscribeData{
        Channels: []string{"sukko.BTC.trade", "sukko.ETH.trade"},
    }
    dataBytes, _ := json.Marshal(data)
    msg.Data = dataBytes

    got, err := json.Marshal(msg)
    if err != nil {
        t.Fatalf("Marshal error: %v", err)
    }

    // Verify it matches AsyncAPI spec format
    want := `{"type":"subscribe","data":{"channels":["sukko.BTC.trade","sukko.ETH.trade"]}}`
    if string(got) != want {
        t.Errorf("Marshal = %s, want %s", got, want)
    }
}

func TestSubscriptionAck_Unmarshal(t *testing.T) {
    t.Parallel()

    input := `{"type":"subscription_ack","subscribed":["sukko.BTC.trade"],"count":5}`

    var ack SubscriptionAck
    if err := json.Unmarshal([]byte(input), &ack); err != nil {
        t.Fatalf("Unmarshal error: %v", err)
    }

    if ack.Type != RespTypeSubscriptionAck {
        t.Errorf("Type = %s, want %s", ack.Type, RespTypeSubscriptionAck)
    }
    if len(ack.Subscribed) != 1 || ack.Subscribed[0] != "sukko.BTC.trade" {
        t.Errorf("Subscribed = %v, want [sukko.BTC.trade]", ack.Subscribed)
    }
    if ack.Count != 5 {
        t.Errorf("Count = %d, want 5", ack.Count)
    }
}

func TestMessageEnvelope_Unmarshal(t *testing.T) {
    t.Parallel()

    input := `{"type":"message","seq":1234,"ts":1706000000000,"channel":"sukko.BTC.trade","data":{"price":50000}}`

    var env MessageEnvelope
    if err := json.Unmarshal([]byte(input), &env); err != nil {
        t.Fatalf("Unmarshal error: %v", err)
    }

    if env.Type != RespTypeMessage {
        t.Errorf("Type = %s, want %s", env.Type, RespTypeMessage)
    }
    if env.Seq != 1234 {
        t.Errorf("Seq = %d, want 1234", env.Seq)
    }
    if env.Channel != "sukko.BTC.trade" {
        t.Errorf("Channel = %s, want sukko.BTC.trade", env.Channel)
    }
}
```

### 4. Stats Tests

```go
func TestStats_ConnectionMetrics(t *testing.T) {
    t.Parallel()

    stats := NewStats()

    // Simulate connections
    stats.ActiveConnections.Add(1)
    stats.TotalCreated.Add(1)

    if got := stats.ActiveConnections.Load(); got != 1 {
        t.Errorf("ActiveConnections = %d, want 1", got)
    }

    // Simulate disconnect
    stats.ActiveConnections.Add(-1)

    if got := stats.ActiveConnections.Load(); got != 0 {
        t.Errorf("ActiveConnections after disconnect = %d, want 0", got)
    }
}
```

---

## Panic Recovery

All goroutines must have panic recovery:

```go
func (c *Connection) readPump() {
    defer func() {
        if r := recover(); r != nil {
            c.logger.Error().
                Interface("panic", r).
                Str("stack", string(debug.Stack())).
                Msg("Panic in readPump")
        }
    }()
    defer c.close()

    // ... read loop
}
```

---

## CLI Flags and Environment Variables

```go
func parseConfig() (*Config, error) {
    cfg := &Config{}

    // Connection flags
    flag.StringVar(&cfg.WSURL, "url", getEnv("WS_URL", "ws://localhost:3000/ws"), "WebSocket URL")
    flag.StringVar(&cfg.HealthURL, "health", getEnv("HEALTH_URL", "http://localhost:3000/health"), "Health URL")
    flag.IntVar(&cfg.TargetConnections, "connections", getEnvInt("TARGET_CONNECTIONS", 1000), "Target connections")
    flag.IntVar(&cfg.RampRate, "ramp-rate", getEnvInt("RAMP_RATE", 100), "Connections per second during ramp-up")
    flag.DurationVar(&cfg.SustainDuration, "duration", getEnvDuration("DURATION", 30*time.Minute), "Sustain duration")
    flag.DurationVar(&cfg.ConnectionTimeout, "timeout", getEnvDuration("CONNECTION_TIMEOUT", 10*time.Second), "Connection timeout")

    // Subscription flags
    channelsStr := flag.String("channels", getEnv("CHANNELS", "sukko.all.trade,sukko.BTC.trade,sukko.ETH.trade,sukko.SOL.trade,sukko.BTC.orderbook,sukko.ETH.liquidity"), "Comma-separated channels (format: tenant.identifier.category)")
    flag.StringVar(&cfg.SubscriptionMode, "mode", getEnv("SUBSCRIPTION_MODE", "random"), "Subscription mode: all/single/random")
    flag.IntVar(&cfg.ChannelsPerClient, "channels-per-client", getEnvInt("CHANNELS_PER_CLIENT", 3), "Channels per client (for random mode)")

    // Auth flags
    flag.StringVar(&cfg.TenantID, "tenant", getEnv("TENANT_ID", "sukko"), "Tenant ID for JWT token generation")
    flag.StringVar(&cfg.Token, "token", getEnv("JWT_TOKEN", ""), "Pre-generated JWT token")
    flag.StringVar(&cfg.JWTSecret, "jwt-secret", getEnv("JWT_SECRET", ""), "JWT secret to generate tokens")
    flag.StringVar(&cfg.Principal, "principal", getEnv("PRINCIPAL", "loadtest-user"), "Principal for JWT")

    // Reporting
    flag.DurationVar(&cfg.ReportInterval, "report-interval", getEnvDuration("REPORT_INTERVAL", 10*time.Second), "Stats report interval")
    flag.DurationVar(&cfg.HealthInterval, "health-interval", getEnvDuration("HEALTH_INTERVAL", 5*time.Second), "Health check interval")

    // Logging
    flag.StringVar(&cfg.LogLevel, "log-level", getEnv("LOG_LEVEL", "info"), "Log level: debug/info/warn/error")

    flag.Parse()

    // Parse comma-separated channels
    if *channelsStr != "" {
        cfg.Channels = strings.Split(*channelsStr, ",")
        for i := range cfg.Channels {
            cfg.Channels[i] = strings.TrimSpace(cfg.Channels[i])
        }
    }

    // Validate
    if err := cfg.Validate(); err != nil {
        return nil, fmt.Errorf("config validation: %w", err)
    }

    return cfg, nil
}
```

### CLI Reference

| Flag | Env Var | Default | Description |
|------|---------|---------|-------------|
| `-url` | `WS_URL` | `ws://localhost:3000/ws` | WebSocket server URL |
| `-health` | `HEALTH_URL` | `http://localhost:3000/health` | Health check URL |
| `-connections` | `TARGET_CONNECTIONS` | `1000` | Target number of connections |
| `-ramp-rate` | `RAMP_RATE` | `100` | Connections per second during ramp-up |
| `-duration` | `DURATION` | `30m` | Sustain duration after ramp-up |
| `-timeout` | `CONNECTION_TIMEOUT` | `10s` | Connection timeout |
| `-channels` | `CHANNELS` | `sukko.all.trade,sukko.BTC.trade,...` (6 channels) | Comma-separated channels |
| `-mode` | `SUBSCRIPTION_MODE` | `random` | all/single/random |
| `-channels-per-client` | `CHANNELS_PER_CLIENT` | `3` | Channels per client (random mode) |
| `-tenant` | `TENANT_ID` | `sukko` | Tenant for JWT generation |
| `-token` | `JWT_TOKEN` | - | Pre-generated JWT token |
| `-jwt-secret` | `JWT_SECRET` | - | JWT secret for token generation |
| `-principal` | `PRINCIPAL` | `loadtest-user` | Principal for JWT |
| `-report-interval` | `REPORT_INTERVAL` | `10s` | Stats report interval |
| `-health-interval` | `HEALTH_INTERVAL` | `5s` | Health check interval |
| `-log-level` | `LOG_LEVEL` | `info` | Log level |

---

## Example Usage

### Basic Connection Test (100 connections, 10/sec ramp)
```bash
./wsloadtest -connections 100 -ramp-rate 10 -duration 1m
```

### Subscription Test with Multiple Channels (GKE Target)
```bash
./wsloadtest \
  -url wss://ws.sukko.example.com/ws \
  -channels "sukko.all.trade,sukko.BTC.trade,sukko.ETH.trade,sukko.SOL.trade,sukko.BTC.orderbook,sukko.ETH.liquidity" \
  -mode random \
  -channels-per-client 3 \
  -connections 5000 \
  -ramp-rate 100 \
  -duration 10m
```

### Aggregate Channel Test (all trades)
```bash
# All clients subscribe to sukko.all.trade
./wsloadtest \
  -channels "sukko.all.trade" \
  -mode all \
  -connections 500 \
  -ramp-rate 100
```

### High Ramp Rate Stress Test (e2-standard-8)
```bash
# 500 connections per second from GCE instance
./wsloadtest \
  -url wss://ws.sukko.example.com/ws \
  -channels "sukko.all.trade,sukko.BTC.trade,sukko.ETH.trade,sukko.SOL.trade,sukko.BTC.orderbook,sukko.ETH.liquidity" \
  -connections 10000 \
  -ramp-rate 500 \
  -duration 15m
```

### With Authentication
```bash
./wsloadtest \
  -jwt-secret "your-32-character-secret-here" \
  -tenant sukko \
  -principal loadtest-user \
  -channels "sukko.BTC.trade" \
  -connections 1000
```

### Verify Subscription Flow (Debug Mode)
```bash
./wsloadtest \
  -url ws://localhost:3000/ws \
  -channels "sukko.BTC.trade" \
  -mode all \
  -connections 5 \
  -ramp-rate 1 \
  -duration 1m \
  -log-level debug
```

Expected output (debug level):
```
INF Starting ramp-up target_connections=5 ramp_rate=1
DBG Connection established id=1
DBG Sending subscribe channels=["sukko.BTC.trade"] id=1
DBG Received subscription_ack subscribed=["sukko.BTC.trade"] count=1 id=1
DBG Received message channel=sukko.BTC.trade seq=1 id=1
...
```

---

## Files to Create/Modify

| File | Action | Lines (Est.) |
|------|--------|--------------|
| `wsloadtest/main.go` | Rewrite (entry point only) | 50 |
| `wsloadtest/config.go` | New | 150 |
| `wsloadtest/runner.go` | New | 200 |
| `wsloadtest/connection.go` | New | 250 |
| `wsloadtest/protocol.go` | New (AsyncAPI types) | 100 |
| `wsloadtest/stats.go` | New | 150 |
| `wsloadtest/health.go` | New | 50 |
| `wsloadtest/config_test.go` | New | 100 |
| `wsloadtest/connection_test.go` | New | 100 |
| `wsloadtest/protocol_test.go` | New | 50 |
| `wsloadtest/stats_test.go` | New | 50 |
| `wsloadtest/README.md` | New | - |
| `loadtest/` | Delete (after migration) | - |

**Total:** ~1,250 lines (vs current 836 single file, but with tests, protocol types, and better organization)

---

## Implementation Order

1. **Create `wsloadtest/` directory** with `go.mod`
2. **Create `protocol.go`** — Message types matching AsyncAPI spec
3. **Create `protocol_test.go`** — Protocol serialization tests
4. **Create `config.go`** — Config struct, parsing, validation, channel building
5. **Create `config_test.go`** — Config validation tests
6. **Create `stats.go`** — Stats struct with atomic fields
7. **Create `stats_test.go`** — Stats calculation tests
8. **Create `health.go`** — Health check logic
9. **Create `connection.go`** — Connection lifecycle, read/write pumps
10. **Create `connection_test.go`** — Connection unit tests
11. **Create `runner.go`** — LoadRunner orchestration
12. **Create `main.go`** — Entry point only
13. **Create `README.md`** — Document usage and flags
14. **Delete `loadtest/`** — Remove old directory

---

## Verification

```bash
cd wsloadtest
go build ./...      # Clean compilation
go test ./...       # All tests pass
go vet ./...        # No vet issues

# Integration test (local, auth disabled)
./wsloadtest \
  -url ws://localhost:3000/ws \
  -channels "sukko.all.trade,sukko.BTC.trade,sukko.ETH.trade" \
  -connections 10 \
  -ramp-rate 5 \
  -duration 30s \
  -log-level debug

# Production test (GKE target from e2-standard-8)
./wsloadtest \
  -url wss://ws.sukko.example.com/ws \
  -channels "sukko.all.trade,sukko.BTC.trade,sukko.ETH.trade,sukko.SOL.trade,sukko.BTC.orderbook,sukko.ETH.liquidity" \
  -connections 5000 \
  -ramp-rate 100 \
  -duration 10m \
  -jwt-secret "$JWT_SECRET"

# Verify:
# 1. All 10 connections established
# 2. subscription_ack received for each connection
# 3. Messages received on subscribed channels
# 4. Clean shutdown (no errors)
```

---

## Subscription-Specific Metrics

The tool should track and report subscription-related metrics:

| Metric | Description |
|--------|-------------|
| `subscriptions_sent` | Number of subscribe messages sent |
| `subscriptions_confirmed` | Number of subscription_ack received |
| `subscriptions_failed` | Number of subscribe_error received |
| `subscriptions_pending` | Sent but no ack yet |
| `subscription_latency_ms` | Time from subscribe to subscription_ack |
| `messages_received` | Total broadcast messages received |
| `messages_per_channel` | Messages received per channel |
| `message_rate` | Messages per second |

### Subscription Success Criteria

A successful subscription test should verify:
1. All connections established
2. All subscriptions acknowledged (`subscription_ack` received)
3. Messages received on subscribed channels only
4. No `subscribe_error` responses
5. Clean unsubscribe on shutdown

---

## Infrastructure

### Deployment Topology

```
┌─────────────────────────────────────┐     ┌─────────────────────────────────────┐
│     GCE Instance (Load Test)        │     │         GKE Cluster (Target)        │
│     e2-standard-8                   │     │                                     │
│     8 vCPU, 32 GB RAM               │     │   ┌─────────────────────────────┐   │
│                                     │     │   │      ws-server pods         │   │
│   ┌───────────────────────────┐     │     │   │   (WebSocket server)        │   │
│   │       wsloadtest          │─────┼─────┼──►│                             │   │
│   │                           │     │     │   └─────────────────────────────┘   │
│   │  - 5000+ connections      │     │     │                                     │
│   │  - 6 channels             │     │     │   ┌─────────────────────────────┐   │
│   │  - random subscription    │     │     │   │      Redpanda               │   │
│   └───────────────────────────┘     │     │   │   (message broker)          │   │
│                                     │     │   └─────────────────────────────┘   │
└─────────────────────────────────────┘     └─────────────────────────────────────┘
```

### GCE Instance Specs (e2-standard-8)

| Resource | Value |
|----------|-------|
| vCPUs | 8 |
| Memory | 32 GB |
| Max file descriptors | ~1M (needs ulimit tuning) |
| Network | Up to 16 Gbps |

### System Tuning (Load Test Instance)

```bash
# /etc/security/limits.conf
* soft nofile 1000000
* hard nofile 1000000

# /etc/sysctl.conf
net.ipv4.ip_local_port_range = 1024 65535
net.ipv4.tcp_tw_reuse = 1
net.core.somaxconn = 65535
net.ipv4.tcp_max_syn_backlog = 65535
```

### Default Channels (6 total)

| Channel | Description |
|---------|-------------|
| `sukko.all.trade` | Aggregate trades (all tokens) |
| `sukko.BTC.trade` | BTC trades |
| `sukko.ETH.trade` | ETH trades |
| `sukko.SOL.trade` | SOL trades |
| `sukko.BTC.orderbook` | BTC orderbook updates |
| `sukko.ETH.liquidity` | ETH liquidity updates |

---

## What NOT to Change

- Keep `go.mod` as separate module (allows independent builds)
- Keep Dockerfile structure
- Keep gorilla/websocket (acceptable for client, simpler API than gobwas)
- Keep heartbeat mechanism (industry standard pattern)

---

## Coding Guidelines Compliance

| Guideline | How This Plan Complies |
|-----------|------------------------|
| No Hardcoded Values | All values via config/env vars |
| Defense in Depth | Config validation at startup |
| Observable by Default | zerolog structured logging |
| No Shortcuts | Proper error handling, no ignored errors |
| Table-Driven Tests | All tests use table-driven pattern |
| Configuration Validation | `Validate()` method on Config |
| Panic Recovery | defer recover in all goroutines |
| No Global State | Runner, Config, Stats as struct fields |
| Structured Logging | zerolog with contextual fields |
