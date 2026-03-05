# Plan: Configurable Ping/Pong Timeouts

**Date:** 2026-02-10
**Status:** Ready for Implementation
**Priority:** High - Fixes 19-minute disconnect issue

---

## Executive Summary

Make WebSocket ping/pong timeouts configurable via environment variables. This fixes the 19-minute disconnect issue by increasing the buffer from 3 seconds to 15 seconds.

---

## Problem Statement

### Current Configuration (Hardcoded)

```go
// ws/internal/server/pump.go
func DefaultPumpConfig() PumpConfig {
    pongWait := 30 * time.Second
    return PumpConfig{
        PongWait:   pongWait,                  // 30s - timeout for receiving pong
        WriteWait:  5 * time.Second,
        PingPeriod: (pongWait * 9) / 10,       // 27s - ping every 27 seconds
    }
}
```

**Buffer Calculation:**
```
Buffer = PongWait - PingPeriod = 30s - 27s = 3 seconds
```

### Why 3 Seconds Is Too Tight

In our proxied architecture, ping/pong must traverse 8 network hops:

```
Shard → ShardProxy → ws-server → K8s Service → Gateway → Client
Client → Gateway → K8s Service → ws-server → ShardProxy → Shard
```

Any latency spike (GC pause, CPU throttle, network congestion) can push round-trip time beyond 3 seconds.

### Why 19 Minutes?

```
19 minutes = 1140 seconds
1140 / 30 second cycles = 38 successful ping/pong cycles
On cycle 39, round-trip took >3 seconds → connection dropped
```

---

## Solution

### Make Timeouts Configurable

Add environment variables with sane defaults:

| Variable | Default | Description |
|----------|---------|-------------|
| `WS_PONG_WAIT` | `60s` | Time to wait for pong response |
| `WS_PING_PERIOD` | `45s` | How often to send pings |

**New Buffer:** `60s - 45s = 15 seconds` (5x improvement)

### Why These Defaults?

- **60s pongWait:** Matches loadtest configuration, industry-standard range
- **45s pingPeriod:** 75% ratio provides 15s buffer for network latency
- **15s buffer:** Sufficient for 8-hop round-trip even with occasional delays

### Environment-Specific Recommendations

| Environment | PongWait | PingPeriod | Buffer | Rationale |
|-------------|----------|------------|--------|-----------|
| Production (direct) | 60s | 45s | 15s | Standard with comfortable buffer |
| Production (with CDN) | 90s | 60s | 30s | CDN adds latency |
| Local (Kind + port-forward) | 120s | 90s | 30s | Multiple proxies, port-forward flakiness |
| Development | 120s | 90s | 30s | Lenient for debugging |

---

## Implementation

### Architecture Note

The codebase has two `ServerConfig` types:
1. **`platform.ServerConfig`** - Loaded from environment variables (source of truth)
2. **`types.ServerConfig`** - Used internally by the server (subset of fields)

Config flows: `platform.ServerConfig` → `main.go` copies fields → `types.ServerConfig` → `Server` → `Pump`

---

### 1. Add Config Fields to platform.ServerConfig

**File:** `ws/internal/shared/platform/server_config.go`

Add after `TopicRefreshInterval` field (~line 253):

```go
// WebSocket Ping/Pong Configuration
//
// These settings control the keep-alive mechanism between the shard and clients.
// In a proxied architecture (gateway → ws-server → shard), pings travel through
// multiple hops, requiring a larger buffer than direct connections.
//
// Buffer = PongWait - PingPeriod
// The buffer must accommodate the round-trip time through all proxies.
// With 8 network hops (local Kind + port-forward), 15+ seconds is recommended.
//
// Example with defaults (60s/45s):
//   - Shard sends ping at t=0
//   - Ping travels: Shard → ShardProxy → ws-server → Gateway → Client
//   - Client sends pong
//   - Pong travels: Client → Gateway → ws-server → ShardProxy → Shard
//   - If pong arrives by t=60s, connection is healthy
//   - 15 second buffer for network latency, GC pauses, etc.
//
// Common issues:
//   - Buffer too small (<5s): Connections drop during GC pauses or network hiccups
//   - PingPeriod >= PongWait: Invalid, ping would always timeout
//
PongWait   time.Duration `env:"WS_PONG_WAIT" envDefault:"60s"`
PingPeriod time.Duration `env:"WS_PING_PERIOD" envDefault:"45s"`
```

### 2. Add Validation

**File:** `ws/internal/shared/platform/server_config.go` (in `Validate()` method, after line ~433)

```go
// WebSocket ping/pong validation
//
// Minimum thresholds rationale:
// - PongWait >= 10s: Must allow time for ping to traverse proxy chain (8 hops)
//   and return. Values under 10s cause spurious disconnects.
// - PingPeriod >= 5s: Prevents excessive ping traffic. At <5s, ping overhead
//   becomes significant (>20% of connection lifetime is ping/pong).
// - PingPeriod < PongWait: Required for the buffer to exist. If equal or greater,
//   the ping would always timeout before the next ping is sent.
const (
    minPongWait   = 10 * time.Second
    minPingPeriod = 5 * time.Second
)

if c.PongWait < minPongWait {
    return fmt.Errorf("WS_PONG_WAIT must be >= %v, got %v", minPongWait, c.PongWait)
}
if c.PingPeriod < minPingPeriod {
    return fmt.Errorf("WS_PING_PERIOD must be >= %v, got %v", minPingPeriod, c.PingPeriod)
}
if c.PingPeriod >= c.PongWait {
    return fmt.Errorf("WS_PING_PERIOD (%v) must be < WS_PONG_WAIT (%v)", c.PingPeriod, c.PongWait)
}
```

### 3. Update Print/LogConfig

**File:** `ws/internal/shared/platform/server_config.go`

Add to `Print()` (after line ~519, before `=== Monitoring ===`):
```go
fmt.Println("\n=== WebSocket Ping/Pong ===")
fmt.Printf("Pong Wait:       %s\n", c.PongWait)
fmt.Printf("Ping Period:     %s\n", c.PingPeriod)
fmt.Printf("Buffer:          %s\n", c.PongWait-c.PingPeriod)
```

Add to `LogConfig()` (after line ~575):
```go
Dur("ws_pong_wait", c.PongWait).
Dur("ws_ping_period", c.PingPeriod).
```

### 4. Add Fields to types.ServerConfig

**File:** `ws/internal/shared/types/types.go`

Add after `HTTPIdleTimeout` field (~line 80):

```go
// WebSocket ping/pong timing
PongWait   time.Duration // Timeout for pong response (default: 60s)
PingPeriod time.Duration // How often to send pings (default: 45s)
```

### 5. Update pump.go

**File:** `ws/internal/server/pump.go`

Replace `DefaultPumpConfig()` (lines 30-38):

```go
// Ping/pong timing constants with documented rationale.
const (
    // DefaultPongWait is the default timeout for receiving a pong response.
    // 60s is industry-standard (matches AWS API Gateway idle timeout).
    // Must be > PingPeriod to allow buffer for network round-trip.
    DefaultPongWait = 60 * time.Second

    // DefaultPingPeriod is the default interval for sending pings.
    // 45s = 75% of PongWait, providing 15s buffer for:
    // - 8-hop proxy chain round-trip (gateway → ws-server → shard and back)
    // - GC pauses, CPU throttling, network congestion
    DefaultPingPeriod = 45 * time.Second

    // WriteWait is the timeout for write operations.
    // 5s is sufficient for local writes; network latency handled by TCP.
    WriteWait = 5 * time.Second
)

// NewPumpConfig creates a PumpConfig with the specified timeouts.
// If pingPeriod >= pongWait (invalid), logs a warning and falls back to 75% ratio.
func NewPumpConfig(pongWait, pingPeriod time.Duration, logger zerolog.Logger) PumpConfig {
    // Validation: pingPeriod must be less than pongWait
    if pingPeriod >= pongWait {
        correctedPingPeriod := (pongWait * 3) / 4
        logger.Warn().
            Dur("configured_ping_period", pingPeriod).
            Dur("configured_pong_wait", pongWait).
            Dur("corrected_ping_period", correctedPingPeriod).
            Msg("PingPeriod >= PongWait is invalid, using 75% ratio")
        pingPeriod = correctedPingPeriod
    }

    return PumpConfig{
        PongWait:   pongWait,
        WriteWait:  WriteWait,
        PingPeriod: pingPeriod,
    }
}

// DefaultPumpConfig returns the default pump configuration.
// Uses 60s/45s for a 15-second buffer, suitable for proxied architectures.
func DefaultPumpConfig() PumpConfig {
    return PumpConfig{
        PongWait:   DefaultPongWait,
        WriteWait:  WriteWait,
        PingPeriod: DefaultPingPeriod,
    }
}
```

**Note:** `NewPumpConfig` now requires a logger parameter to log warnings when falling back.
This follows the coding guideline "Never Ignore Errors" - we log the correction.

### 6. Update server.go to Use Config

**File:** `ws/internal/server/server.go`

Replace lines 174-182 (inside `NewServer`):

```go
// Initialize Pump with adapters for testability
// Use configured ping/pong timeouts, falling back to defaults if not set
pongWait := config.PongWait
pingPeriod := config.PingPeriod

// Fall back to defaults with logging (per coding guidelines: no silent fallbacks)
if pongWait == 0 {
    pongWait = DefaultPongWait
    logger.Debug().
        Dur("pong_wait", pongWait).
        Msg("Using default PongWait (WS_PONG_WAIT not set)")
}
if pingPeriod == 0 {
    pingPeriod = DefaultPingPeriod
    logger.Debug().
        Dur("ping_period", pingPeriod).
        Msg("Using default PingPeriod (WS_PING_PERIOD not set)")
}

s.pump = NewPump(
    NewPumpConfig(pongWait, pingPeriod, logger),
    NewZerologAdapter(logger),
    logger, // ZerologLogger for panic recovery
    NewRateLimiterAdapter(s.rateLimiter),
    NewAuditLoggerAdapter(s.auditLogger),
    s.stats,
    &RealClock{},
)
```

### 7. Update main.go to Copy Config Fields

**File:** `ws/cmd/server/main.go`

Add to `shardConfig` initialization (after line ~303, before the closing brace):

```go
// WebSocket ping/pong timing
PongWait:   cfg.PongWait,
PingPeriod: cfg.PingPeriod,
```

### 8. Update Helm Values

**File:** `deployments/helm/sukko/charts/ws-server/values.yaml`

Add after `metricsInterval` (~line 57):

```yaml
  # WebSocket Ping/Pong Configuration
  # Controls keep-alive timing between shard and clients.
  # Buffer = pongWait - pingPeriod (should be >= 15s for proxied architectures)
  pongWait: "60s"     # Timeout for pong response
  pingPeriod: "45s"   # How often to send pings
```

### 9. Update Helm ConfigMap

**File:** `deployments/helm/sukko/charts/ws-server/templates/configmap.yaml`

Add after `METRICS_INTERVAL` (~line 40):

```yaml
  # WebSocket ping/pong timing
  WS_PONG_WAIT: {{ .Values.config.pongWait | quote }}
  WS_PING_PERIOD: {{ .Values.config.pingPeriod | quote }}
```

### 10. Update Local Values

**File:** `deployments/helm/sukko/values/local.yaml`

Add to existing `ws-server.config` section (after `environment: local`, ~line 49):

```yaml
ws-server:
  config:
    shards: 1
    logLevel: debug
    logFormat: pretty
    environment: local
    # Lenient timeouts for local development (multiple proxies, port-forward)
    pongWait: "120s"
    pingPeriod: "90s"
```

---

## Files to Modify

| File | Changes |
|------|---------|
| `ws/internal/shared/platform/server_config.go` | Add `PongWait`, `PingPeriod` fields + validation + Print/LogConfig |
| `ws/internal/shared/types/types.go` | Add `PongWait`, `PingPeriod` fields to internal config |
| `ws/internal/server/pump.go` | Add `NewPumpConfig()`, update `DefaultPumpConfig()` defaults |
| `ws/internal/server/pump_test.go` | Update `TestDefaultPumpConfig` expected values (30s→60s, 27s→45s) |
| `ws/internal/server/server.go` | Use `config.PongWait/PingPeriod` instead of `DefaultPumpConfig()` |
| `ws/cmd/server/main.go` | Copy `PongWait`, `PingPeriod` from platform to types config |
| `deployments/helm/sukko/charts/ws-server/values.yaml` | Add `pongWait`, `pingPeriod` config values |
| `deployments/helm/sukko/charts/ws-server/templates/configmap.yaml` | Map `WS_PONG_WAIT`, `WS_PING_PERIOD` env vars |
| `deployments/helm/sukko/values/local.yaml` | Set lenient local values (120s/90s) |

---

## Testing Plan

### 1. Update Existing Tests

**File:** `ws/internal/server/pump_test.go`

The existing `TestDefaultPumpConfig` (lines 25-38) tests the old values. Update to new defaults:

```go
func TestDefaultPumpConfig(t *testing.T) {
    t.Parallel()
    config := DefaultPumpConfig()

    if config.PongWait != 60*time.Second {  // Changed from 30s
        t.Errorf("PongWait: got %v, want 60s", config.PongWait)
    }
    if config.WriteWait != 5*time.Second {
        t.Errorf("WriteWait: got %v, want 5s", config.WriteWait)
    }
    if config.PingPeriod != 45*time.Second {  // Changed from 27s
        t.Errorf("PingPeriod: got %v, want 45s", config.PingPeriod)
    }
}
```

### 2. Add New Unit Tests (Table-Driven per Coding Guidelines)

```go
// ws/internal/server/pump_test.go

func TestNewPumpConfig(t *testing.T) {
    t.Parallel()

    tests := []struct {
        name           string
        pongWait       time.Duration
        pingPeriod     time.Duration
        wantPongWait   time.Duration
        wantPingPeriod time.Duration
        wantWarning    bool // Expect logger warning
    }{
        {
            name:           "valid_config_60s_45s",
            pongWait:       60 * time.Second,
            pingPeriod:     45 * time.Second,
            wantPongWait:   60 * time.Second,
            wantPingPeriod: 45 * time.Second,
            wantWarning:    false,
        },
        {
            name:           "valid_config_120s_90s",
            pongWait:       120 * time.Second,
            pingPeriod:     90 * time.Second,
            wantPongWait:   120 * time.Second,
            wantPingPeriod: 90 * time.Second,
            wantWarning:    false,
        },
        {
            name:           "pingPeriod_equals_pongWait_falls_back_to_75_percent",
            pongWait:       60 * time.Second,
            pingPeriod:     60 * time.Second,
            wantPongWait:   60 * time.Second,
            wantPingPeriod: 45 * time.Second, // 75% of 60s
            wantWarning:    true,
        },
        {
            name:           "pingPeriod_exceeds_pongWait_falls_back_to_75_percent",
            pongWait:       60 * time.Second,
            pingPeriod:     90 * time.Second,
            wantPongWait:   60 * time.Second,
            wantPingPeriod: 45 * time.Second, // 75% of 60s
            wantWarning:    true,
        },
        {
            name:           "small_values_with_valid_ratio",
            pongWait:       20 * time.Second,
            pingPeriod:     10 * time.Second,
            wantPongWait:   20 * time.Second,
            wantPingPeriod: 10 * time.Second,
            wantWarning:    false,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()

            // Use mock logger to verify warning is logged
            mockLogger := newTestMockLogger()
            cfg := NewPumpConfig(tt.pongWait, tt.pingPeriod, mockLogger.Logger())

            if cfg.PongWait != tt.wantPongWait {
                t.Errorf("PongWait = %v, want %v", cfg.PongWait, tt.wantPongWait)
            }
            if cfg.PingPeriod != tt.wantPingPeriod {
                t.Errorf("PingPeriod = %v, want %v", cfg.PingPeriod, tt.wantPingPeriod)
            }
            if cfg.WriteWait != WriteWait {
                t.Errorf("WriteWait = %v, want %v", cfg.WriteWait, WriteWait)
            }
            if tt.wantWarning && !mockLogger.HasWarning() {
                t.Error("Expected warning log for invalid config, got none")
            }
            if !tt.wantWarning && mockLogger.HasWarning() {
                t.Error("Expected no warning log for valid config, got one")
            }
        })
    }
}
```

### 3. Config Validation Test (Table-Driven per Coding Guidelines)

```go
// ws/internal/shared/platform/server_config_test.go

func TestServerConfig_PingPongValidation(t *testing.T) {
    t.Parallel()

    // Base valid config - tests modify only ping/pong fields
    baseConfig := func() *ServerConfig {
        return &ServerConfig{
            Addr:                    ":3002",
            ProvisioningDatabaseURL: "postgres://test:test@localhost:5432/test",
            TopicRefreshInterval:    60 * time.Second,
            ValkeyAddrs:             []string{"localhost:6379"},
            ValkeyChannel:           "test",
            PongWait:                60 * time.Second,
            PingPeriod:              45 * time.Second,
            // ... other required fields with valid defaults
        }
    }

    tests := []struct {
        name        string
        pongWait    time.Duration
        pingPeriod  time.Duration
        wantErr     bool
        errContains string
    }{
        {
            name:       "valid_60s_45s",
            pongWait:   60 * time.Second,
            pingPeriod: 45 * time.Second,
            wantErr:    false,
        },
        {
            name:       "valid_120s_90s",
            pongWait:   120 * time.Second,
            pingPeriod: 90 * time.Second,
            wantErr:    false,
        },
        {
            name:        "pongWait_too_small",
            pongWait:    5 * time.Second,
            pingPeriod:  3 * time.Second,
            wantErr:     true,
            errContains: "WS_PONG_WAIT must be >= 10s",
        },
        {
            name:        "pingPeriod_too_small",
            pongWait:    60 * time.Second,
            pingPeriod:  3 * time.Second,
            wantErr:     true,
            errContains: "WS_PING_PERIOD must be >= 5s",
        },
        {
            name:        "pingPeriod_equals_pongWait",
            pongWait:    60 * time.Second,
            pingPeriod:  60 * time.Second,
            wantErr:     true,
            errContains: "WS_PING_PERIOD",
        },
        {
            name:        "pingPeriod_exceeds_pongWait",
            pongWait:    60 * time.Second,
            pingPeriod:  90 * time.Second,
            wantErr:     true,
            errContains: "WS_PING_PERIOD",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()

            cfg := baseConfig()
            cfg.PongWait = tt.pongWait
            cfg.PingPeriod = tt.pingPeriod

            err := cfg.Validate()

            if tt.wantErr {
                if err == nil {
                    t.Errorf("expected error containing %q, got nil", tt.errContains)
                } else if !strings.Contains(err.Error(), tt.errContains) {
                    t.Errorf("expected error containing %q, got %q", tt.errContains, err.Error())
                }
            } else {
                if err != nil {
                    t.Errorf("expected no error, got %v", err)
                }
            }
        })
    }
}
```

### 4. Integration Test

```bash
# Deploy with new config
task local:build
task local:deploy

# Run 60-minute loadtest
task local:loadtest:run CONNECTIONS=1 DURATION=60m

# Verify no disconnects
kubectl logs -n sukko-local -l app.kubernetes.io/name=ws-server --tail=1000 | grep disconnect
```

### 5. Verification Checklist

- [ ] Server starts with default config (60s/45s)
- [ ] Server starts with custom config via env vars
- [ ] Config validation rejects pingPeriod >= pongWait
- [ ] Print() shows ping/pong configuration
- [ ] Logs show ping/pong configuration at startup
- [ ] Connection survives >30 minutes
- [ ] Connection survives >60 minutes

---

## Comparison: Old vs New Defaults

| Setting | Old Value | New Value | Change |
|---------|-----------|-----------|--------|
| PongWait | 30s | 60s | +30s |
| PingPeriod | 27s | 45s | +18s |
| Buffer | 3s | 15s | +12s (5x) |
| Configurable | No | Yes | ✓ |

---

## Why Not Move Ping/Pong to Gateway?

Initially considered moving ping/pong to the gateway. Research showed:

1. **Not industry standard:** AWS API Gateway, NGINX, etc. don't send pings - backend servers do
2. **Complexity:** Would require significant gateway refactor
3. **Unnecessary:** Increasing timeouts solves the problem with minimal changes

See `2026-02-10_PLAN_GATEWAY_PING_PONG.md` for the original (superseded) plan.

---

## Coding Guidelines Compliance

This plan follows the [CODING_GUIDELINES.md](../CODING_GUIDELINES.md):

| Guideline | How We Comply |
|-----------|---------------|
| **No Hardcoded Values** | All timeouts configurable via env vars with documented defaults |
| **No Magic Numbers** | Constants with rationale comments (DefaultPongWait, minPongWait, etc.) |
| **Never Ignore Errors** | `NewPumpConfig` logs warning when falling back; server.go logs when using defaults |
| **Configuration Validation** | `Validate()` rejects invalid combinations with clear error messages |
| **Table-Driven Tests** | All tests use table-driven pattern with t.Parallel() |
| **Structured Logging** | All log calls use zerolog with structured fields |
| **Observable by Default** | New values logged at startup via Print() and LogConfig() |

---

## References

- [WebSocket RFC 6455 - Ping/Pong](https://datatracker.ietf.org/doc/html/rfc6455#section-5.5.2)
- [NGINX WebSocket Proxying](https://nginx.org/en/docs/http/websocket.html)
- [AWS API Gateway WebSocket](https://repost.aws/questions/QUV-egTr6_Skylz2_OHp8irw/websocket-api-server-side-ping-pong)
- Previous investigation: `2026-02-09_PLAN_PUMP_TIMEOUT_FIX.md`
