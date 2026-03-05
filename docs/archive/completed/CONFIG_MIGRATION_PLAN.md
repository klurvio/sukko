# Configuration Migration Plan: Centralized .env with Type-Safe Parsing

## Executive Summary

**Goal**: Replace scattered environment variable parsing with centralized, type-safe configuration using `.env` files.

**Approach**: Use **godotenv** (load .env) + **caarlos0/env** (parse to structs) for lightweight, zero-dependency config management.

**Why Not Viper?**
- **Bloated**: 313% larger binary than alternatives
- **Overkill**: We only need .env file support, not 10+ formats
- **Dependencies**: Pulls in many transitive dependencies

**Recommended Stack**:
- `github.com/joho/godotenv` - Load .env files (14KB, zero dependencies)
- `github.com/caarlos0/env/v11` - Parse env to structs (8KB, minimal dependencies)
- **Total overhead**: ~22KB vs Viper's ~2MB

---

## Current State Analysis

### 1. Go Server (ws-go)

**Location**: `src/main.go`

**Current Approach**: Manual parsing with helper functions
```go
func getEnvInt(key string, defaultValue int) int { ... }
func getEnvFloat(key string, defaultValue float64) float64 { ... }
func getEnvDuration(key string, defaultValue time.Duration) time.Duration { ... }
```

**Problems**:
- ❌ Scattered parsing logic (lines 16-62)
- ❌ No validation (invalid values silently use defaults)
- ❌ No type safety (easy to mix up keys)
- ❌ Repetitive code (4 similar functions)
- ❌ No central source of truth

**Config Variables** (28 total):
```
WS_CPU_LIMIT, WS_MEMORY_LIMIT, WS_MAX_CONNECTIONS
WS_WORKER_POOL_SIZE, WS_WORKER_QUEUE_SIZE
WS_MAX_NATS_RATE, WS_MAX_BROADCAST_RATE, WS_MAX_GOROUTINES
WS_CPU_REJECT_THRESHOLD, WS_CPU_PAUSE_THRESHOLD
JS_STREAM_MAX_AGE, JS_STREAM_MAX_MSGS, JS_STREAM_MAX_BYTES
JS_CONSUMER_ACK_WAIT, JS_STREAM_NAME, JS_CONSUMER_NAME
METRICS_INTERVAL, LOG_LEVEL, LOG_FORMAT
```

### 2. Docker Compose

**Location**: `isolated/ws-go/docker-compose.yml`

**Current Approach**: Environment variables embedded in YAML
```yaml
environment:
  - WS_CPU_LIMIT=1.9
  - WS_MEMORY_LIMIT=7516192768
  - WS_MAX_CONNECTIONS=7000
  # ... 10 more variables
```

**Problems**:
- ❌ Values duplicated between compose files (dev/prod/isolated)
- ❌ No comments explaining values
- ❌ Hard to override for local development
- ❌ Requires `envsubst` for BACKEND_INTERNAL_IP substitution

### 3. JavaScript Test Scripts

**Location**: `scripts/*.cjs`

**Current Approach**: Direct `process.env` access
```javascript
const CONFIG = {
  WS_URL: process.env.WS_URL || 'ws://localhost:3004/ws',
  TARGET_CONNECTIONS: parseInt(process.env.TARGET_CONNECTIONS) || 18000,
  CONNECTION_TIMEOUT_MS: parseInt(process.env.CONNECTION_TIMEOUT) || 10000,
  // ... 15 more
};
```

**Problems**:
- ❌ Defaults scattered across multiple files
- ❌ Type conversion repeated everywhere
- ❌ No validation (NaN from invalid integers)
- ❌ Inconsistent naming conventions

### 4. Publisher

**Location**: `publisher/`

**Has**: `.env.example` but incomplete
**Missing**: Many production-critical configs

---

## Proposed Solution

### Architecture Overview

```
┌─────────────────┐
│   .env file     │  Single source of truth
│  (gitignored)   │  User-specific overrides
└────────┬────────┘
         │
         ↓ loads
┌─────────────────┐
│ .env.example    │  Committed to git
│  (documented)   │  Production defaults + docs
└────────┬────────┘
         │
         ↓ parses
┌─────────────────┐
│  config struct  │  Type-safe configuration
│   (validated)   │  Required fields enforced
└─────────────────┘
```

### Tech Stack Comparison

| Library | Binary Size | Dependencies | Features | Use Case |
|---------|-------------|--------------|----------|----------|
| **Viper** | 2.0 MB | 30+ deps | JSON, YAML, TOML, env, flags, remote | Enterprise, complex config |
| **Koanf** | 640 KB | 10+ deps | JSON, YAML, TOML, env, flags | Mid-size projects |
| **godotenv + env** | **22 KB** | **0-2 deps** | **.env files only** | **Our use case** ✅ |
| Standard library | 0 KB | 0 deps | Manual parsing | Small projects |

**Decision**: Use `godotenv + env` for minimal overhead and simplicity.

---

## Implementation Plan

### Phase 1: Create Config Package (2 hours)

#### Step 1.1: Add Dependencies
```bash
cd src/
go get github.com/joho/godotenv
go get github.com/caarlos0/env/v11
```

#### Step 1.2: Create `src/config/config.go`

<details>
<summary>Complete implementation (click to expand)</summary>

```go
package config

import (
    "fmt"
    "os"
    "time"

    "github.com/caarlos0/env/v11"
    "github.com/joho/godotenv"
)

// Config holds all server configuration
// Tags:
//   env: Environment variable name
//   envDefault: Default value if not set
//   required: Must be provided (no default)
type Config struct {
    // Server basics
    Addr    string `env:"WS_ADDR" envDefault:":3002"`
    NATSUrl string `env:"NATS_URL" envDefault:""`

    // Resource limits (from container)
    CPULimit    float64 `env:"WS_CPU_LIMIT" envDefault:"1.0"`
    MemoryLimit int64   `env:"WS_MEMORY_LIMIT" envDefault:"536870912"` // 512MB

    // Capacity
    MaxConnections int `env:"WS_MAX_CONNECTIONS" envDefault:"500"`

    // Worker pool (computed from CPU if not set)
    WorkerPoolSize  int `env:"WS_WORKER_POOL_SIZE" envDefault:"0"`  // 0 = auto-calculate
    WorkerQueueSize int `env:"WS_WORKER_QUEUE_SIZE" envDefault:"0"` // 0 = auto-calculate

    // Rate limiting
    MaxNATSRate      int `env:"WS_MAX_NATS_RATE" envDefault:"20"`
    MaxBroadcastRate int `env:"WS_MAX_BROADCAST_RATE" envDefault:"20"`
    MaxGoroutines    int `env:"WS_MAX_GOROUTINES" envDefault:"1000"`

    // Safety thresholds
    CPURejectThreshold float64 `env:"WS_CPU_REJECT_THRESHOLD" envDefault:"75.0"`
    CPUPauseThreshold  float64 `env:"WS_CPU_PAUSE_THRESHOLD" envDefault:"80.0"`

    // JetStream
    JSStreamMaxAge    time.Duration `env:"JS_STREAM_MAX_AGE" envDefault:"30s"`
    JSStreamMaxMsgs   int64         `env:"JS_STREAM_MAX_MSGS" envDefault:"100000"`
    JSStreamMaxBytes  int64         `env:"JS_STREAM_MAX_BYTES" envDefault:"52428800"` // 50MB
    JSConsumerAckWait time.Duration `env:"JS_CONSUMER_ACK_WAIT" envDefault:"30s"`
    JSStreamName      string        `env:"JS_STREAM_NAME" envDefault:"SUKKO_TOKENS"`
    JSConsumerName    string        `env:"JS_CONSUMER_NAME" envDefault:"ws-server"`

    // Monitoring
    MetricsInterval time.Duration `env:"METRICS_INTERVAL" envDefault:"15s"`

    // Logging
    LogLevel  string `env:"LOG_LEVEL" envDefault:"info"`
    LogFormat string `env:"LOG_FORMAT" envDefault:"json"`

    // Environment
    Environment string `env:"ENVIRONMENT" envDefault:"development"`
}

// Load reads configuration from .env file and environment variables
// Priority: ENV vars > .env file > defaults
func Load() (*Config, error) {
    // Load .env file (optional - OK if it doesn't exist)
    // In production (Docker), we use environment variables directly
    // In development, .env file provides convenience
    if err := godotenv.Load(); err != nil {
        // Only log, don't fail - we can run without .env file
        fmt.Printf("Info: No .env file found (using environment variables only)\n")
    }

    cfg := &Config{}

    // Parse environment variables into struct
    // This validates types and applies defaults
    if err := env.Parse(cfg); err != nil {
        return nil, fmt.Errorf("failed to parse config: %w", err)
    }

    // Post-processing: Auto-calculate worker pool if not set
    if cfg.WorkerPoolSize == 0 {
        cfg.WorkerPoolSize = int(cfg.CPULimit * 2)
    }
    if cfg.WorkerQueueSize == 0 {
        cfg.WorkerQueueSize = cfg.WorkerPoolSize * 100
    }

    // Validation
    if err := cfg.Validate(); err != nil {
        return nil, fmt.Errorf("config validation failed: %w", err)
    }

    return cfg, nil
}

// Validate checks configuration for errors
func (c *Config) Validate() error {
    // Required fields (no sensible defaults)
    if c.Addr == "" {
        return fmt.Errorf("WS_ADDR is required")
    }

    // Range checks
    if c.MaxConnections < 1 {
        return fmt.Errorf("WS_MAX_CONNECTIONS must be > 0, got %d", c.MaxConnections)
    }
    if c.WorkerPoolSize < 1 {
        return fmt.Errorf("WS_WORKER_POOL_SIZE must be > 0, got %d", c.WorkerPoolSize)
    }
    if c.CPURejectThreshold < 0 || c.CPURejectThreshold > 100 {
        return fmt.Errorf("WS_CPU_REJECT_THRESHOLD must be 0-100, got %.1f", c.CPURejectThreshold)
    }
    if c.CPUPauseThreshold < 0 || c.CPUPauseThreshold > 100 {
        return fmt.Errorf("WS_CPU_PAUSE_THRESHOLD must be 0-100, got %.1f", c.CPUPauseThreshold)
    }

    // Logical checks
    if c.CPUPauseThreshold < c.CPURejectThreshold {
        return fmt.Errorf("WS_CPU_PAUSE_THRESHOLD (%.1f) must be >= WS_CPU_REJECT_THRESHOLD (%.1f)",
            c.CPUPauseThreshold, c.CPURejectThreshold)
    }

    // Enum checks
    validLogLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
    if !validLogLevels[c.LogLevel] {
        return fmt.Errorf("LOG_LEVEL must be one of: debug, info, warn, error (got: %s)", c.LogLevel)
    }

    validLogFormats := map[string]bool{"json": true, "text": true}
    if !validLogFormats[c.LogFormat] {
        return fmt.Errorf("LOG_FORMAT must be one of: json, text (got: %s)", c.LogFormat)
    }

    return nil
}

// Print logs configuration for debugging
func (c *Config) Print() {
    fmt.Println("=== Server Configuration ===")
    fmt.Printf("Environment:     %s\n", c.Environment)
    fmt.Printf("Address:         %s\n", c.Addr)
    fmt.Printf("NATS URL:        %s\n", c.NATSUrl)
    fmt.Println("\n=== Resource Limits ===")
    fmt.Printf("CPU Limit:       %.1f cores\n", c.CPULimit)
    fmt.Printf("Memory Limit:    %d MB\n", c.MemoryLimit/(1024*1024))
    fmt.Printf("Max Connections: %d\n", c.MaxConnections)
    fmt.Println("\n=== Worker Pool ===")
    fmt.Printf("Workers:         %d\n", c.WorkerPoolSize)
    fmt.Printf("Queue Size:      %d\n", c.WorkerQueueSize)
    fmt.Println("\n=== Rate Limits ===")
    fmt.Printf("NATS Messages:   %d/sec\n", c.MaxNATSRate)
    fmt.Printf("Broadcasts:      %d/sec\n", c.MaxBroadcastRate)
    fmt.Printf("Max Goroutines:  %d\n", c.MaxGoroutines)
    fmt.Println("\n=== Safety Thresholds ===")
    fmt.Printf("CPU Reject:      %.1f%%\n", c.CPURejectThreshold)
    fmt.Printf("CPU Pause:       %.1f%%\n", c.CPUPauseThreshold)
    fmt.Println("\n=== JetStream ===")
    fmt.Printf("Stream:          %s\n", c.JSStreamName)
    fmt.Printf("Consumer:        %s\n", c.JSConsumerName)
    fmt.Printf("Max Age:         %s\n", c.JSStreamMaxAge)
    fmt.Printf("Max Messages:    %d\n", c.JSStreamMaxMsgs)
    fmt.Printf("Max Bytes:       %d MB\n", c.JSStreamMaxBytes/(1024*1024))
    fmt.Println("\n=== Logging ===")
    fmt.Printf("Level:           %s\n", c.LogLevel)
    fmt.Printf("Format:          %s\n", c.LogFormat)
    fmt.Println("============================")
}
```
</details>

#### Step 1.3: Create Comprehensive `.env.example`

<details>
<summary>Complete .env.example with documentation</summary>

```bash
# =============================================================================
# WebSocket Server Configuration
# =============================================================================
# Copy this file to .env and customize for your environment
# Priority: Environment variables > .env file > defaults

# =============================================================================
# ENVIRONMENT
# =============================================================================
# Environment name: development, staging, production
# Used for environment-specific behavior and logging
ENVIRONMENT=development

# =============================================================================
# SERVER
# =============================================================================
# WebSocket server listen address
# Format: :PORT or HOST:PORT
# Production: :3002 (internal), proxy handles external access
WS_ADDR=:3002

# NATS server URL
# Production: nats://BACKEND_INTERNAL_IP:4222
# Development: nats://localhost:4222
NATS_URL=nats://localhost:4222

# =============================================================================
# RESOURCE LIMITS
# =============================================================================
# These MUST match your docker-compose resource limits!
# Server uses these for admission control and capacity planning

# CPU limit (cores)
# Example: 1.9 for e2-standard-2 (leaves 0.1 for Promtail)
# Must match docker-compose: deploy.resources.limits.cpus
WS_CPU_LIMIT=1.9

# Memory limit (bytes)
# Example: 7516192768 = 7 GB (leaves 1GB for system + Promtail)
# Must match docker-compose: deploy.resources.limits.memory
# Calculate: GB * 1024 * 1024 * 1024
WS_MEMORY_LIMIT=7516192768

# Maximum concurrent connections
# Production: 7,000 (validated for 99%+ success rate)
# Testing: Set to 8,000-10,000 to find breaking point
# Formula: 7,000 × 0.7MB + 974MB overhead = 5,874MB (81% of 7GB)
WS_MAX_CONNECTIONS=7000

# =============================================================================
# WORKER POOL
# =============================================================================
# Leave at 0 for auto-calculation (recommended)
# Auto formula: CPU_LIMIT * 2 (e.g., 1.9 cores → 4 workers)
#
# Manual sizing formula:
#   workers = max(32, connections/40)
#   For 7K connections: max(32, 7000/40) = 175 → round to power of 2 = 192
#
# Queue size formula: workers * 100
WS_WORKER_POOL_SIZE=192
WS_WORKER_QUEUE_SIZE=19200

# =============================================================================
# RATE LIMITING
# =============================================================================
# Critical for preventing overload and goroutine explosion

# NATS message consumption rate (messages/sec)
# Based on production metrics: 280K users, 40K tx/day peak
# Actual rates: 5 msg/sec (average) to 19.7 msg/sec (high peak)
# Setting: 25 msg/sec provides 25% headroom above peak
WS_MAX_NATS_RATE=25

# Broadcast rate limit (broadcasts/sec)
# Should match NATS rate (1 NATS message = 1 broadcast)
WS_MAX_BROADCAST_RATE=25

# Maximum goroutines (hard limit)
# Formula: ((connections × 2) + workers + 13) × 1.2
# Example: ((7000 × 2) + 192 + 13) × 1.2 = 17,046 → round to 17,500
# Actual at runtime: (7,000 × 2) + 192 + 13 = 14,205 (81% of limit)
WS_MAX_GOROUTINES=17500

# =============================================================================
# SAFETY THRESHOLDS
# =============================================================================
# Emergency brakes to prevent cascading failures

# CPU reject threshold (percentage)
# Above this: Reject new connections with 503
# Prevents new load when system struggling
WS_CPU_REJECT_THRESHOLD=75.0

# CPU pause threshold (percentage)
# Above this: Pause NATS consumption (NAK messages for redelivery)
# Gives system time to recover without dropping data
WS_CPU_PAUSE_THRESHOLD=80.0

# =============================================================================
# JETSTREAM CONFIGURATION
# =============================================================================
# NATS JetStream settings for reliable message delivery

# Stream name (should match publisher)
JS_STREAM_NAME=SUKKO_TOKENS

# Consumer name (unique per ws-server instance)
JS_CONSUMER_NAME=ws-server

# Maximum message age in stream
# Old messages automatically deleted
# Recommendation: 30s (sufficient for reconnection scenarios)
JS_STREAM_MAX_AGE=30s

# Maximum messages in stream
# Older messages deleted when limit reached
# 100K messages ≈ 50MB @ 512 bytes/message
JS_STREAM_MAX_MSGS=100000

# Maximum stream size (bytes)
# 50MB = enough for 100K messages @ 512 bytes
JS_STREAM_MAX_BYTES=52428800

# Consumer acknowledgment timeout
# How long JetStream waits for ACK before redelivery
# Match with processing time: 30s allows for retries
JS_CONSUMER_ACK_WAIT=30s

# =============================================================================
# MONITORING
# =============================================================================
# Metrics collection interval
# Balance: More frequent = better visibility, higher CPU cost
# Recommendation: 15s for production
METRICS_INTERVAL=15s

# =============================================================================
# LOGGING
# =============================================================================
# Log level: debug, info, warn, error
# Production: info (balance between visibility and volume)
# Development: debug (verbose)
# Troubleshooting: debug (temporary)
LOG_LEVEL=info

# Log format: json, text
# Production: json (structured logs for Loki/Grafana)
# Development: text (human-readable)
LOG_FORMAT=json

# =============================================================================
# PRODUCTION DEPLOYMENT NOTES
# =============================================================================
#
# 1. INSTANCE SIZING (e2-standard-2: 2 vCPU, 8GB RAM)
#    - WS_CPU_LIMIT=1.9        (reserve 0.1 for Promtail)
#    - WS_MEMORY_LIMIT=7GB     (reserve 1GB for system + Promtail)
#    - WS_MAX_CONNECTIONS=7000 (99%+ success rate validated)
#
# 2. RATE LIMITS
#    - Based on production metrics: 280K users, 40K tx/day peak
#    - WS_MAX_NATS_RATE=25     (25% headroom above peak)
#    - Total throughput: 7,000 × 25 = 175K writes/sec
#
# 3. WORKER POOL
#    - Formula: max(32, connections/40) = 192 workers
#    - Load per worker: (7000 × 25) / 192 = 911 msg/sec (optimal: 300-1,000)
#
# 4. MONITORING
#    - Prometheus metrics: /metrics endpoint
#    - Structured logs: JSON → Promtail → Loki → Grafana
#    - Health checks: /health endpoint (includes resource usage)
#
# 5. SCALING
#    - Vertical: Increase to e2-standard-4 (4 vCPU, 16GB) → 15K connections
#    - Horizontal: Add instances behind load balancer (sticky sessions required)
#
# See: /docs/production/IMPLEMENTATION_PLAN.md for complete architecture
```
</details>

### Phase 2: Refactor Go Server (1 hour)

#### Step 2.1: Update `src/main.go`

**Before** (176 lines):
```go
// Manual parsing with 4 helper functions (lines 16-62)
cpuLimit := getEnvFloat("WS_CPU_LIMIT", float64(maxProcs))
maxConnections := getEnvInt("WS_MAX_CONNECTIONS", 500)
// ... 25 more variables
```

**After** (50 lines):
```go
import "yourmodule/config"

func main() {
    // Load configuration (one line!)
    cfg, err := config.Load()
    if err != nil {
        log.Fatalf("Failed to load config: %v", err)
    }

    // Print config for debugging
    cfg.Print()

    // Create server config from loaded config
    serverConfig := ServerConfig{
        Addr:                  cfg.Addr,
        NATSUrl:              cfg.NATSUrl,
        MaxConnections:       cfg.MaxConnections,
        CPULimit:            cfg.CPULimit,
        MemoryLimit:         cfg.MemoryLimit,
        WorkerCount:         cfg.WorkerPoolSize,
        WorkerQueueSize:     cfg.WorkerQueueSize,
        MaxNATSMessagesPerSec: cfg.MaxNATSRate,
        MaxBroadcastsPerSec:   cfg.MaxBroadcastRate,
        MaxGoroutines:         cfg.MaxGoroutines,
        CPURejectThreshold:   cfg.CPURejectThreshold,
        CPUPauseThreshold:    cfg.CPUPauseThreshold,
        JSStreamMaxAge:       cfg.JSStreamMaxAge,
        JSStreamMaxMsgs:      cfg.JSStreamMaxMsgs,
        JSStreamMaxBytes:     cfg.JSStreamMaxBytes,
        JSConsumerAckWait:    cfg.JSConsumerAckWait,
        JSStreamName:         cfg.JSStreamName,
        JSConsumerName:       cfg.JSConsumerName,
        MetricsInterval:      cfg.MetricsInterval,
        LogLevel:            LogLevel(cfg.LogLevel),
        LogFormat:           LogFormat(cfg.LogFormat),
        BufferSize:          4096, // Constant
    }

    server, err := NewServer(serverConfig, log.New(os.Stdout, "[WS] ", log.LstdFlags))
    if err != nil {
        log.Fatalf("Failed to create server: %v", err)
    }

    // ... rest unchanged
}
```

**Benefits**:
- ✅ 126 lines removed (72% reduction)
- ✅ Type-safe (compiler catches mistakes)
- ✅ Validated (invalid configs fail fast)
- ✅ Documented (all defaults in one place)

### Phase 3: Update Docker Compose (30 min)

#### Step 3.1: Create `isolated/ws-go/.env.production`

```bash
# Production configuration for sukko-go instance
# Instance: e2-standard-2 (2 vCPU, 8GB RAM)

ENVIRONMENT=production
WS_ADDR=:3002
NATS_URL=nats://${BACKEND_INTERNAL_IP}:4222

# Resource limits (must match docker-compose)
WS_CPU_LIMIT=1.9
WS_MEMORY_LIMIT=7516192768
WS_MAX_CONNECTIONS=7000

# Worker pool (production-validated)
WS_WORKER_POOL_SIZE=192
WS_WORKER_QUEUE_SIZE=19200

# Rate limiting (production traffic)
WS_MAX_NATS_RATE=25
WS_MAX_BROADCAST_RATE=25
WS_MAX_GOROUTINES=17500

# Safety thresholds
WS_CPU_REJECT_THRESHOLD=75.0
WS_CPU_PAUSE_THRESHOLD=80.0

# JetStream
JS_STREAM_NAME=SUKKO_TOKENS
JS_CONSUMER_NAME=ws-server
JS_STREAM_MAX_AGE=30s
JS_STREAM_MAX_MSGS=100000
JS_STREAM_MAX_BYTES=52428800
JS_CONSUMER_ACK_WAIT=30s

# Monitoring
METRICS_INTERVAL=15s
LOG_LEVEL=info
LOG_FORMAT=json
```

#### Step 3.2: Update `docker-compose.yml`

**Before**:
```yaml
services:
  ws-go:
    environment:
      - WS_CPU_LIMIT=1.9
      - WS_MEMORY_LIMIT=7516192768
      # ... 13 more variables (hard to maintain)
```

**After**:
```yaml
services:
  ws-go:
    build:
      context: ./src
      dockerfile: Dockerfile
    container_name: sukko-go
    ports:
      - "0.0.0.0:3004:3002"
    command:
      - "./sukko-server"
    env_file:
      - .env.production    # All config in one file!
    restart: unless-stopped
    deploy:
      resources:
        limits:
          cpus: "1.9"      # Must match WS_CPU_LIMIT in .env
          memory: 7168M    # Must match WS_MEMORY_LIMIT in .env
    ulimits:
      nofile:
        soft: 200000
        hard: 200000
```

**Benefits**:
- ✅ Clean YAML (16 lines removed)
- ✅ Easy to override: `docker-compose --env-file .env.staging up`
- ✅ Version control friendly (changes show in one file)
- ✅ Self-documenting (comments in .env file)

### Phase 4: Update Test Scripts (1 hour)

#### Option A: Create Shared Config Module

**Create** `scripts/lib/config.cjs`:
```javascript
const fs = require('fs');
const path = require('path');

/**
 * Load configuration from .env file and environment variables
 * Priority: ENV > .env > defaults
 */
function loadConfig(defaults = {}) {
  // Load .env file if exists
  const envPath = path.join(__dirname, '../../.env');
  if (fs.existsSync(envPath)) {
    const envContent = fs.readFileSync(envPath, 'utf-8');
    for (const line of envContent.split('\n')) {
      const match = line.match(/^([^#=]+)=(.*)$/);
      if (match && !process.env[match[1]]) {
        process.env[match[1]] = match[2].trim();
      }
    }
  }

  // Helper: Get env with type conversion
  const getEnv = (key, defaultValue, parser = String) => {
    const value = process.env[key];
    if (value === undefined || value === '') return defaultValue;
    try {
      return parser(value);
    } catch {
      console.warn(`Warning: Invalid ${key}=${value}, using default ${defaultValue}`);
      return defaultValue;
    }
  };

  return {
    // Server
    WS_URL: getEnv('WS_URL', defaults.WS_URL || 'ws://localhost:3004/ws'),
    HEALTH_URL: getEnv('HEALTH_URL', defaults.HEALTH_URL || 'http://localhost:3004/health'),

    // Test parameters
    TARGET_CONNECTIONS: getEnv('TARGET_CONNECTIONS', defaults.TARGET_CONNECTIONS || 7000, parseInt),
    RAMP_RATE: getEnv('RAMP_RATE', defaults.RAMP_RATE || 100, parseInt),
    DURATION: getEnv('DURATION', defaults.DURATION || 1800, parseInt),
    CONNECTION_TIMEOUT: getEnv('CONNECTION_TIMEOUT', defaults.CONNECTION_TIMEOUT || 10000, parseInt),

    // Subscriptions
    CHANNELS: getEnv('CHANNELS', defaults.CHANNELS || 'BTC.trade,ETH.trade,SOL.trade,SUKKO.trade,DOGE.trade'),
    SUBSCRIPTION_MODE: getEnv('SUBSCRIPTION_MODE', defaults.SUBSCRIPTION_MODE || 'all'),
    CHANNELS_PER_CLIENT: getEnv('CHANNELS_PER_CLIENT', defaults.CHANNELS_PER_CLIENT || 3, parseInt),

    // Reporting
    REPORT_INTERVAL: getEnv('REPORT_INTERVAL', defaults.REPORT_INTERVAL || 10000, parseInt),
    HEALTH_CHECK_INTERVAL: getEnv('HEALTH_CHECK_INTERVAL', defaults.HEALTH_CHECK_INTERVAL || 5000, parseInt),
  };
}

module.exports = { loadConfig };
```

**Update** `scripts/sustained-load-test.cjs`:
```javascript
const { loadConfig } = require('./lib/config.cjs');

const CONFIG = loadConfig({
  // Script-specific defaults
  TARGET_CONNECTIONS: 18000,
  RAMP_RATE: 100,
});

console.log('Loaded configuration:', CONFIG);
// ... rest of script
```

#### Option B: Use `dotenv` Package

```bash
cd scripts/
npm install dotenv
```

**Update scripts**:
```javascript
require('dotenv').config({ path: '../.env' });

const CONFIG = {
  WS_URL: process.env.WS_URL || 'ws://localhost:3004/ws',
  // ... rest
};
```

### Phase 5: Testing & Validation (1 hour)

#### Test Checklist

- [ ] **Unit test config package**
  ```bash
  cd src/
  go test ./config -v
  ```

- [ ] **Test with .env file**
  ```bash
  cp .env.example .env
  # Edit .env with test values
  go run main.go
  # Verify: Config prints correctly
  ```

- [ ] **Test without .env file**
  ```bash
  rm .env
  WS_MAX_CONNECTIONS=1000 go run main.go
  # Verify: Falls back to env vars + defaults
  ```

- [ ] **Test validation (should fail)**
  ```bash
  WS_CPU_REJECT_THRESHOLD=150 go run main.go
  # Verify: Fails with validation error
  ```

- [ ] **Test Docker with .env**
  ```bash
  cd isolated/ws-go/
  docker-compose --env-file .env.production up
  # Verify: Container starts with correct config
  ```

- [ ] **Test load test scripts**
  ```bash
  cp .env.example .env
  npm run test:sustained
  # Verify: Loads config from .env
  ```

---

## Migration Timeline

| Phase | Task | Duration | Effort |
|-------|------|----------|--------|
| 1 | Create config package | 2 hours | Medium |
| 2 | Refactor main.go | 1 hour | Easy |
| 3 | Update docker-compose | 30 min | Easy |
| 4 | Update test scripts | 1 hour | Easy |
| 5 | Testing & validation | 1 hour | Easy |
| **Total** | | **5.5 hours** | **Low-Medium** |

---

## Rollout Strategy

### Development Environment
1. Create `.env` from `.env.example`
2. Test locally: `go run main.go`
3. Verify config loaded correctly
4. Test docker-compose: `docker-compose up`

### Staging Environment
1. Create `.env.staging` with staging values
2. Deploy: `docker-compose --env-file .env.staging up`
3. Run load tests
4. Monitor for 24 hours

### Production Environment
1. Create `.env.production` with production values
2. Deploy during maintenance window
3. Monitor metrics closely for 1 hour
4. Rollback plan: Keep old docker-compose as backup

---

## Benefits Summary

### Code Quality
- ✅ **126 lines removed** from main.go (72% reduction)
- ✅ **Type-safe** configuration (compile-time checks)
- ✅ **Validated** config (fail fast on errors)
- ✅ **DRY** (Don't Repeat Yourself) - single source of truth

### Developer Experience
- ✅ **Single .env file** to configure everything
- ✅ **Self-documenting** (comprehensive .env.example)
- ✅ **Easy testing** (override specific values)
- ✅ **IDE support** (struct autocomplete)

### Operations
- ✅ **Environment-specific** configs (.env.dev, .env.prod)
- ✅ **Version control** (track config changes)
- ✅ **Deployment** (no code changes for config tweaks)
- ✅ **Debugging** (Print() method shows all config)

### Binary Size
- ✅ **+22KB** only (godotenv + env packages)
- ✅ **vs +2MB** if using Viper (99% size reduction)

---

## Alternative Approaches Considered

### 1. Keep Current Approach
- ❌ Pro: No migration cost
- ❌ Con: Scattered, error-prone, hard to maintain

### 2. Use Viper
- ✅ Pro: Feature-rich (YAML, JSON, remote config)
- ❌ Con: **2MB binary bloat** for features we don't need
- ❌ Con: 30+ dependencies (security surface, build time)

### 3. Use Koanf
- ✅ Pro: Lighter than Viper (640KB vs 2MB)
- ✅ Pro: Modular design
- ❌ Con: Still overkill for .env-only needs
- ❌ Con: 10+ dependencies

### 4. Use godotenv + env (CHOSEN ✅)
- ✅ Pro: Minimal overhead (22KB)
- ✅ Pro: Zero/minimal dependencies
- ✅ Pro: Simple API (fits our use case perfectly)
- ✅ Pro: Type-safe struct mapping
- ❌ Con: .env files only (not a problem for us)

---

## Risk Mitigation

### Risk 1: Breaking Production
**Mitigation**:
- Comprehensive testing in dev/staging first
- Deploy during maintenance window
- Keep old docker-compose as rollback
- Monitor metrics for 1 hour post-deployment

### Risk 2: Config Validation Too Strict
**Mitigation**:
- Start with lenient validation
- Add strict checks incrementally
- Log warnings for deprecated configs
- Provide clear error messages

### Risk 3: Missing Config Values
**Mitigation**:
- Keep all existing defaults
- .env.example has 100% coverage
- Config.Print() shows what's loaded
- Test suite validates all paths

---

## Next Steps

1. **Review this plan** with team
2. **Approve library choice** (godotenv + env)
3. **Create config package** (Phase 1)
4. **Test in development** (Phase 5)
5. **Deploy to staging** (validate)
6. **Deploy to production** (maintenance window)

---

**Last Updated**: 2025-10-13
**Status**: 📋 Plan ready for implementation
**Estimated Effort**: 5.5 hours (1 dev day)
