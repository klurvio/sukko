# Odin WebSocket Server - Coding Guidelines

This document establishes coding standards for the Odin WebSocket server codebase. All contributors must follow these guidelines to ensure code is **robust**, **testable**, **secure**, **performant**, and **maintainable**.

---

## Table of Contents

1. [Core Principles](#core-principles)
2. [Go Idioms and Best Practices](#go-idioms-and-best-practices)
3. [Package Organization](#package-organization)
4. [File Naming Conventions](#file-naming-conventions)
5. [Error Handling](#error-handling)
6. [Logging](#logging)
7. [Configuration](#configuration)
8. [Testing](#testing)
9. [Observability and Metrics](#observability-and-metrics)
10. [Security](#security)
11. [Performance](#performance)
12. [Concurrency Patterns](#concurrency-patterns)
13. [WebSocket SaaS Patterns](#websocket-saas-patterns)
14. [Code Review Checklist](#code-review-checklist)

---

## Core Principles

### 1. No Hardcoded Values
Every configurable parameter MUST be externalized via environment variables or configuration files.

```go
// BAD - Hardcoded values
const maxConnections = 1000
const kafkaBrokers = "localhost:9092"

// GOOD - Configurable via environment
type Config struct {
    MaxConnections int    `env:"WS_MAX_CONNECTIONS" envDefault:"500"`
    KafkaBrokers   string `env:"KAFKA_BROKERS" envDefault:"localhost:9092"`
}
```

### 2. Defense in Depth
Every layer must validate its inputs. Never assume upstream validation.

```go
// GOOD - Validate at every boundary
func (s *Server) HandleMessage(clientID int64, msg []byte) error {
    // Validate even though gateway may have validated
    if len(msg) > s.config.MaxMessageSize {
        return ErrMessageTooLarge
    }
    // ...
}
```

### 3. Fail Fast, Recover Gracefully
Detect problems early and fail with clear errors. In production, recover from panics without crashing.

### 4. Observable by Default
Every significant operation must be logged and/or metriced. Silent failures are forbidden.

### 5. No Shortcuts, No Hacks, No Technical Debt

**This is non-negotiable.** We do not cut corners. Every line of code must be production-quality.

#### What "No Hacks" Means

```go
// FORBIDDEN - "Temporary" fixes that become permanent
// TODO: fix this later
// HACK: this works for now
// FIXME: quick workaround
time.Sleep(100 * time.Millisecond) // "fixes" race condition

// FORBIDDEN - Magic numbers without explanation
if retries > 3 {  // Why 3? What's the reasoning?

// FORBIDDEN - Swallowing errors to "make it work"
result, _ := someFunction()  // Ignore error, it "usually works"

// FORBIDDEN - Copy-paste code instead of proper abstraction
// (Same 50 lines repeated in 3 files)

// FORBIDDEN - Skipping validation "because the caller already checked"
func processData(data []byte) {
    // "Gateway already validated this"  <- WRONG
}

// FORBIDDEN - Disabling security checks for convenience
// #nosec or //nolint without proper justification
```

#### The Right Way

```go
// CORRECT - Proper retry logic with configurable values
maxRetries := s.config.MaxRetries  // Configurable
for attempt := range maxRetries {
    if err := s.doOperation(); err != nil {
        if attempt == maxRetries-1 {
            return fmt.Errorf("operation failed after %d attempts: %w", maxRetries, err)
        }
        backoff := s.config.RetryBackoff * time.Duration(attempt+1)
        s.logger.Warn().
            Err(err).
            Int("attempt", attempt+1).
            Dur("backoff", backoff).
            Msg("Operation failed, retrying")
        time.Sleep(backoff)
        continue
    }
    break
}

// CORRECT - Every error is handled
result, err := someFunction()
if err != nil {
    return fmt.Errorf("someFunction: %w", err)
}

// CORRECT - Validate at every boundary, document why
func processData(data []byte) error {
    // Defense in depth: validate even if caller claims to have validated
    // This protects against future refactoring that might bypass the caller
    if len(data) == 0 {
        return ErrEmptyData
    }
    if len(data) > maxDataSize {
        return ErrDataTooLarge
    }
    // ...
}

// CORRECT - If you must use nolint, explain thoroughly
//nolint:gosec // G404: math/rand is acceptable here because this is for
// non-cryptographic jitter in retry backoff, not security-sensitive randomness.
// Using crypto/rand would add unnecessary overhead for this use case.
jitter := time.Duration(rand.Intn(100)) * time.Millisecond
```

#### Why This Matters

1. **Hacks accumulate** - One "temporary" fix becomes ten, then the codebase is unmaintainable
2. **Production systems fail at 3 AM** - That hack you wrote will wake someone up
3. **Security vulnerabilities** - Shortcuts often bypass security checks
4. **Debugging nightmares** - Future you (or your teammate) will spend hours debugging a hack
5. **Trust erosion** - Once hacks are acceptable, quality standards collapse

#### The "I Don't Have Time" Excuse

If you don't have time to do it right:
1. **Raise the concern** - Tell your lead the timeline is unrealistic
2. **Reduce scope** - Do less, but do it properly
3. **Never ship hacks** - It's better to miss a deadline than ship broken code

```go
// If you're tempted to write this:
// TODO: fix this race condition later
mu.Lock()
data := sharedMap[key]  // HACK: sometimes nil, just check
mu.Unlock()
if data != nil {
    // ...
}

// STOP. Fix it now:
mu.RLock()
data, exists := sharedMap[key]
mu.RUnlock()
if !exists {
    return ErrKeyNotFound
}
// Now it's correct, documented, and won't cause a 3 AM page
```

#### Code Review Standard

Reviewers MUST reject:
- Any `TODO`, `HACK`, `FIXME` comments without a linked ticket and deadline
- Any `//nolint` or `#nosec` without thorough justification
- Any ignored errors without explicit documentation of why it's safe
- Any "temporary" solutions without a concrete plan to fix them
- Any copy-pasted code that should be abstracted
- Any magic numbers without named constants or configuration

**If it's not good enough for production, it doesn't get merged.**

---

## Go Idioms and Best Practices

### Use Modern Go Features (Go 1.22+)

```go
// Use 'any' instead of 'interface{}'
func LogFields(fields map[string]any) { /* ... */ }

// Use range over integers
for i := range 10 {
    // ...
}

// Use min/max builtins
maxSize := max(requestedSize, minBufferSize)

// Use strings.Cut instead of SplitN
scheme, token, found := strings.Cut(header, " ")

// Use slices package
if slices.Contains(allowedMethods, method) {
    // ...
}
```

### Interface Design

Define small, focused interfaces at the point of use:

```go
// GOOD - Small interface in consumer package
type Logger interface {
    Debug() LogEvent
    Info() LogEvent
    Error() LogEvent
}

// BAD - Large interface that forces unnecessary implementations
type Logger interface {
    Debug() LogEvent
    Info() LogEvent
    Warn() LogEvent
    Error() LogEvent
    Fatal() LogEvent
    Panic() LogEvent
    Trace() LogEvent
    // ... 20 more methods
}
```

### Accept Interfaces, Return Concrete Types

```go
// GOOD
func NewServer(logger Logger, limiter RateLimiter) *Server {
    return &Server{
        logger:  logger,
        limiter: limiter,
    }
}

// BAD - Returns interface unnecessarily
func NewServer(logger Logger) ServerInterface {
    // ...
}
```

### Use Functional Options for Complex Constructors

```go
type ServerOption func(*Server)

func WithLogger(logger Logger) ServerOption {
    return func(s *Server) {
        s.logger = logger
    }
}

func WithRateLimiter(limiter RateLimiter) ServerOption {
    return func(s *Server) {
        s.limiter = limiter
    }
}

func NewServer(config Config, opts ...ServerOption) *Server {
    s := &Server{
        config: config,
        logger: defaultLogger,  // Sensible default
    }
    for _, opt := range opts {
        opt(s)
    }
    return s
}
```

---

## Package Organization

### Directory Structure

```
internal/
├── auth/                 # Authentication & authorization
│   ├── engine.go         # Policy engine
│   ├── jwt.go            # JWT validation
│   ├── jwt_multitenant.go # Multi-tenant JWT
│   └── engine_test.go
├── broadcast/            # Inter-instance messaging
│   ├── bus.go            # Interface definition
│   ├── valkey.go         # Valkey implementation
│   ├── nats.go           # NATS implementation
│   └── factory.go        # Factory for creating buses
├── gateway/              # WebSocket gateway
├── kafka/                # Kafka integration
├── limits/               # Rate limiting & resource guards
│   ├── rate_limiter.go
│   ├── resource_guard.go
│   └── connection_rate_limiter.go
├── messaging/            # Message types & protocols
├── monitoring/           # Logging, metrics, alerting
│   ├── logger.go
│   ├── metrics.go
│   ├── alerting.go
│   └── audit_logger.go
├── orchestration/        # Consumer pools & load balancing
├── platform/             # Platform detection & config
│   ├── server_config.go
│   ├── gateway_config.go
│   └── cgroup.go
├── provisioning/         # Multi-tenant provisioning
│   ├── api/              # HTTP API layer
│   ├── kafka/            # Kafka admin operations
│   └── repository/       # Data access layer
├── server/               # Core WebSocket server
│   ├── server.go         # Main server
│   ├── client.go         # Client connection
│   ├── pump.go           # Read/write pumps
│   ├── handlers_http.go  # HTTP handlers
│   ├── handlers_ws.go    # WebSocket handlers
│   └── interfaces.go     # Interface definitions
├── testutil/             # Shared test utilities
│   └── mocks.go          # Reusable mocks
├── types/                # Core type definitions
│   └── types.go
└── version/              # Version information
```

### Package Guidelines

1. **One responsibility per package** - A package should do one thing well
2. **Minimize public API** - Export only what's necessary
3. **No circular dependencies** - Use interfaces to break cycles
4. **Shared types go in `types/`** - Config structs, constants, enums
5. **Test utilities go in `testutil/`** - Reusable mocks and helpers

---

## File Naming Conventions

| Pattern | Description | Examples |
|---------|-------------|----------|
| `{feature}.go` | Main implementation | `engine.go`, `consumer.go`, `server.go` |
| `{feature}_test.go` | Unit tests | `engine_test.go`, `consumer_test.go` |
| `{feature}_integration_test.go` | Integration tests | `admin_integration_test.go` |
| `{feature}_config.go` | Configuration | `server_config.go`, `gateway_config.go` |
| `{feature}_{variant}.go` | Variant implementation | `keys_postgres.go`, `jwt_multitenant.go` |
| `handlers_{type}.go` | HTTP/WS handlers | `handlers_http.go`, `handlers_ws.go` |
| `interfaces.go` | Package interfaces | Single file for all interfaces |
| `mocks_test.go` | Package-local mocks | Test-only mocks |
| `factory.go` | Factory functions | When multiple implementations exist |

---

## Error Handling

### Wrap Errors with Context

Always wrap errors with context using `fmt.Errorf` and `%w`:

```go
// GOOD - Provides call chain context
func (s *Service) CreateTenant(ctx context.Context, t *Tenant) error {
    if err := t.Validate(); err != nil {
        return fmt.Errorf("validate tenant: %w", err)
    }

    if err := s.repo.Insert(ctx, t); err != nil {
        return fmt.Errorf("insert tenant %s: %w", t.ID, err)
    }

    return nil
}

// BAD - Loses context
func (s *Service) CreateTenant(ctx context.Context, t *Tenant) error {
    if err := t.Validate(); err != nil {
        return err  // Where did this come from?
    }
    // ...
}
```

### Define Sentinel Errors

Define package-level sentinel errors for expected error conditions:

```go
// errors.go or at top of main file
var (
    ErrInvalidToken     = errors.New("invalid token")
    ErrTokenExpired     = errors.New("token expired")
    ErrTenantNotFound   = errors.New("tenant not found")
    ErrRateLimitExceeded = errors.New("rate limit exceeded")
)

// Usage - allows callers to check error type
if errors.Is(err, ErrTenantNotFound) {
    return http.StatusNotFound, err
}
```

### Error Handling Patterns

```go
// Pattern 1: Critical errors - return immediately
if err := s.db.Connect(ctx); err != nil {
    return fmt.Errorf("connect database: %w", err)
}

// Pattern 2: Non-critical errors - log and continue
if err := s.cache.Warm(ctx); err != nil {
    s.logger.Warn().Err(err).Msg("Failed to warm cache, continuing without")
    // Don't return - cache is optional
}

// Pattern 3: Aggregate errors for batch operations
var errs []error
for _, item := range items {
    if err := process(item); err != nil {
        errs = append(errs, fmt.Errorf("process %s: %w", item.ID, err))
    }
}
if len(errs) > 0 {
    return errors.Join(errs...)
}
```

### Never Ignore Errors

```go
// BAD - Silent failure
json.Unmarshal(data, &msg)

// GOOD - Handle or explicitly ignore with comment
if err := json.Unmarshal(data, &msg); err != nil {
    return fmt.Errorf("unmarshal message: %w", err)
}

// ACCEPTABLE - Explicitly ignored with reason
_ = conn.Close() // Best effort cleanup, error logged elsewhere
```

---

## Logging

### Use Structured Logging with zerolog

All logging must use zerolog with structured fields:

```go
// GOOD - Structured logging
s.logger.Info().
    Str("tenant_id", tenant.ID).
    Int("connection_count", count).
    Dur("latency", latency).
    Msg("Tenant connected")

// BAD - Printf-style logging
log.Printf("Tenant %s connected with %d connections in %v", tenant.ID, count, latency)
```

### Log Levels

| Level | Usage |
|-------|-------|
| `Debug` | Detailed debugging info, disabled in production |
| `Info` | Normal operations, state changes, metrics |
| `Warn` | Recoverable issues, degraded performance |
| `Error` | Failures that need attention but don't stop service |
| `Fatal` | Unrecoverable errors, service shutdown |

### Required Fields

Always include contextual fields:

```go
// Request context
logger.Info().
    Str("tenant_id", tenantID).
    Str("client_id", clientID).
    Str("request_id", requestID).
    Msg("Processing request")

// Error context
logger.Error().
    Err(err).
    Str("operation", "create_tenant").
    Str("tenant_id", tenantID).
    Str("stack_trace", string(debug.Stack())).
    Msg("Operation failed")
```

### Panic Recovery Logging

All goroutines must have panic recovery:

```go
func (s *Server) backgroundWorker() {
    // CRITICAL: Must be FIRST defer (LIFO order means it executes LAST)
    defer monitoring.RecoverPanic(s.logger, "backgroundWorker", map[string]any{
        "worker_id": s.workerID,
    })

    defer s.wg.Done()

    // ... worker logic ...
}
```

---

## Configuration

### Environment Variable Configuration

Use `caarlos0/env` with struct tags:

```go
type ServerConfig struct {
    // Network
    Addr string `env:"WS_ADDR" envDefault:":3002"`

    // Kafka
    KafkaBrokers  string `env:"KAFKA_BROKERS" envDefault:"localhost:9092"`
    ConsumerGroup string `env:"KAFKA_CONSUMER_GROUP" envDefault:"ws-server-group"`

    // Limits - ALL must be configurable
    MaxConnections int     `env:"WS_MAX_CONNECTIONS" envDefault:"500"`
    MaxMessageSize int     `env:"WS_MAX_MESSAGE_SIZE" envDefault:"65536"`
    MaxKafkaRate   int     `env:"WS_MAX_KAFKA_RATE" envDefault:"1000"`

    // Thresholds with hysteresis
    CPURejectThreshold      float64 `env:"WS_CPU_REJECT_THRESHOLD" envDefault:"80.0"`
    CPURejectThresholdLower float64 `env:"WS_CPU_REJECT_THRESHOLD_LOWER" envDefault:"70.0"`

    // Timeouts
    ReadTimeout  time.Duration `env:"WS_READ_TIMEOUT" envDefault:"60s"`
    WriteTimeout time.Duration `env:"WS_WRITE_TIMEOUT" envDefault:"10s"`

    // Feature flags
    EnableMetrics bool `env:"WS_ENABLE_METRICS" envDefault:"true"`
}
```

### Configuration Validation

Always validate configuration at startup:

```go
func (c *ServerConfig) Validate() error {
    // Range validation
    if c.MaxConnections < 1 {
        return fmt.Errorf("WS_MAX_CONNECTIONS must be > 0, got %d", c.MaxConnections)
    }

    if c.CPURejectThreshold < 0 || c.CPURejectThreshold > 100 {
        return fmt.Errorf("WS_CPU_REJECT_THRESHOLD must be 0-100, got %.1f", c.CPURejectThreshold)
    }

    // Logical validation (hysteresis: lower must be < upper)
    if c.CPURejectThresholdLower >= c.CPURejectThreshold {
        return fmt.Errorf("WS_CPU_REJECT_THRESHOLD_LOWER (%.1f) must be < WS_CPU_REJECT_THRESHOLD (%.1f)",
            c.CPURejectThresholdLower, c.CPURejectThreshold)
    }

    // Enum validation
    validLogLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
    if !validLogLevels[c.LogLevel] {
        return fmt.Errorf("LOG_LEVEL must be one of: debug, info, warn, error (got: %s)", c.LogLevel)
    }

    return nil
}
```

### Configuration Loading

```go
func LoadConfig(logger *zerolog.Logger) (*ServerConfig, error) {
    // Load .env file (optional)
    if err := godotenv.Load(); err != nil {
        if logger != nil {
            logger.Info().Msg("No .env file found, using environment variables")
        }
    }

    cfg := &ServerConfig{}

    if err := env.Parse(cfg); err != nil {
        return nil, fmt.Errorf("parse config: %w", err)
    }

    if err := cfg.Validate(); err != nil {
        return nil, fmt.Errorf("validate config: %w", err)
    }

    return cfg, nil
}
```

---

## Testing

### Table-Driven Tests

Use table-driven tests for comprehensive coverage:

```go
func TestCPURejectHysteresis(t *testing.T) {
    t.Parallel()

    tests := []struct {
        name           string
        initialState   bool
        currentCPU     float64
        upperThreshold float64
        lowerThreshold float64
        wantAccept     bool
        wantState      bool
    }{
        {
            name:           "accepting_below_upper_stays_accepting",
            initialState:   false,
            currentCPU:     70.0,
            upperThreshold: 80.0,
            lowerThreshold: 60.0,
            wantAccept:     true,
            wantState:      false,
        },
        {
            name:           "accepting_above_upper_starts_rejecting",
            initialState:   false,
            currentCPU:     85.0,
            upperThreshold: 80.0,
            lowerThreshold: 60.0,
            wantAccept:     false,
            wantState:      true,
        },
        // ... more test cases
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()

            rg := NewResourceGuard(tt.upperThreshold, tt.lowerThreshold)
            rg.isRejecting.Store(tt.initialState)

            got := rg.ShouldAccept(tt.currentCPU)

            if got != tt.wantAccept {
                t.Errorf("ShouldAccept() = %v, want %v", got, tt.wantAccept)
            }
            if rg.isRejecting.Load() != tt.wantState {
                t.Errorf("isRejecting = %v, want %v", rg.isRejecting.Load(), tt.wantState)
            }
        })
    }
}
```

### Mock Interfaces for Testing

Define mocks in `testutil/mocks.go` or `mocks_test.go`:

```go
// testutil/mocks.go - Shared mocks
type MockLogger struct {
    mu       sync.Mutex
    messages []LogMessage
}

func (m *MockLogger) Debug() LogEvent { return &MockLogEvent{logger: m, level: "debug"} }
func (m *MockLogger) Info() LogEvent  { return &MockLogEvent{logger: m, level: "info"} }
func (m *MockLogger) Error() LogEvent { return &MockLogEvent{logger: m, level: "error"} }

func (m *MockLogger) HasMessage(substr string) bool {
    m.mu.Lock()
    defer m.mu.Unlock()
    for _, msg := range m.messages {
        if strings.Contains(msg.Message, substr) {
            return true
        }
    }
    return false
}
```

### Dependency Injection for Testing

```go
// Production code uses interface
type Server struct {
    logger Logger
    clock  Clock
    deps   Dependencies
}

type Dependencies struct {
    Logger   Logger   // nil = use default
    Clock    Clock    // nil = use real time
    TestMode bool     // Skip background goroutines
}

// Test with mocks
func TestServer(t *testing.T) {
    mockLogger := testutil.NewMockLogger()
    mockClock := testutil.NewMockClock()

    s := NewServer(config, Dependencies{
        Logger:   mockLogger,
        Clock:    mockClock,
        TestMode: true,
    })

    // Test logic
}
```

### Test Parallelization Rules

```go
// GOOD - Pure unit tests can run in parallel
func TestPureFunction(t *testing.T) {
    t.Parallel()
    // ...
}

// BAD - Tests with shared resources must NOT run in parallel
func TestDatabaseIntegration(t *testing.T) {
    // NO t.Parallel() - uses shared database
    // ...
}

// BAD - Tests in *_shared_test.go files
func TestSharedResource(t *testing.T) {
    // NO t.Parallel() - shared test fixture
    // ...
}
```

---

## Observability and Metrics

### Prometheus Metrics

Define metrics at package level:

```go
var (
    connectionsTotal = prometheus.NewCounter(prometheus.CounterOpts{
        Name: "ws_connections_total",
        Help: "Total WebSocket connections established",
    })

    connectionsActive = prometheus.NewGauge(prometheus.GaugeOpts{
        Name: "ws_connections_active",
        Help: "Current active WebSocket connections",
    })

    messageLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
        Name:    "ws_message_latency_seconds",
        Help:    "Message processing latency",
        Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0},
    }, []string{"message_type"})

    disconnectsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "ws_disconnects_total",
        Help: "Total disconnections by reason",
    }, []string{"reason", "initiated_by"})
)

func init() {
    prometheus.MustRegister(
        connectionsTotal,
        connectionsActive,
        messageLatency,
        disconnectsTotal,
    )
}
```

### Metrics Guidelines

1. **Use consistent naming**: `ws_` prefix for WebSocket server metrics
2. **Include units in name**: `_seconds`, `_bytes`, `_total`
3. **Use labels sparingly**: High cardinality labels cause memory issues
4. **Prefer histograms for latency**: Not summaries (aggregatable)
5. **Document every metric**: Clear `Help` text

### Audit Logging

Security-sensitive operations must use audit logging:

```go
// Audit logger for security events
type AuditLogger interface {
    Info(event, message string, metadata map[string]any)
    Warning(event, message string, metadata map[string]any)
    Critical(event, message string, metadata map[string]any)
}

// Usage
auditLogger.Warning("rate_limit_exceeded", "Client exceeded rate limit", map[string]any{
    "client_id":   clientID,
    "tenant_id":   tenantID,
    "rate_limit":  limit,
    "request_rate": currentRate,
})

auditLogger.Critical("auth_failure", "Invalid API key", map[string]any{
    "key_id":     keyID,
    "tenant_id":  tenantID,
    "ip_address": remoteAddr,
})
```

---

## Security

### Input Validation

Validate ALL inputs at system boundaries:

```go
func (h *Handler) HandleSubscribe(ctx context.Context, req *SubscribeRequest) error {
    // Size limits
    if len(req.Channels) > h.config.MaxChannelsPerClient {
        return ErrTooManyChannels
    }

    // Format validation
    for _, ch := range req.Channels {
        if !isValidChannelName(ch) {
            return fmt.Errorf("invalid channel name: %s", ch)
        }
    }

    // Authorization check
    for _, ch := range req.Channels {
        if !h.authz.CanSubscribe(ctx, req.ClientID, ch) {
            return fmt.Errorf("not authorized for channel: %s", ch)
        }
    }

    return nil
}

func isValidChannelName(name string) bool {
    // Only allow alphanumeric, dots, hyphens, underscores
    // Max length, no special characters
    if len(name) == 0 || len(name) > 256 {
        return false
    }
    for _, r := range name {
        if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '.' && r != '-' && r != '_' {
            return false
        }
    }
    return true
}
```

### Rate Limiting

Apply rate limiting at multiple levels:

```go
// Connection rate limiting (DoS protection)
type ConnectionRateLimiter struct {
    ipLimiters    map[string]*rate.Limiter  // Per-IP
    globalLimiter *rate.Limiter             // Global
}

func (l *ConnectionRateLimiter) Allow(ip string) bool {
    // Global limit first (fast path rejection)
    if !l.globalLimiter.Allow() {
        return false
    }

    // Per-IP limit
    ipLimiter := l.getOrCreateIPLimiter(ip)
    return ipLimiter.Allow()
}

// Message rate limiting (per client)
type MessageRateLimiter struct {
    limiter *rate.Limiter
}

func (l *MessageRateLimiter) Allow() bool {
    return l.limiter.Allow()
}
```

### Secrets Management

Never log or expose secrets:

```go
// BAD - Logs secret
logger.Info().Str("api_key", apiKey).Msg("Authenticating")

// GOOD - Logs only identifier
logger.Info().Str("key_id", keyID).Msg("Authenticating")

// BAD - Returns secret in error
return fmt.Errorf("invalid key: %s", apiKey)

// GOOD - Generic error
return ErrInvalidAPIKey
```

### JWT Validation

```go
func (v *JWTValidator) Validate(tokenString string) (*Claims, error) {
    token, err := jwt.ParseWithClaims(tokenString, &Claims{}, v.keyFunc)
    if err != nil {
        return nil, ErrInvalidToken
    }

    claims, ok := token.Claims.(*Claims)
    if !ok || !token.Valid {
        return nil, ErrInvalidToken
    }

    // Expiration check
    if claims.ExpiresAt.Before(time.Now()) {
        return nil, ErrTokenExpired
    }

    // Issuer validation
    if claims.Issuer != v.expectedIssuer {
        return nil, ErrInvalidIssuer
    }

    return claims, nil
}
```

---

## Performance

### Memory Allocation

Minimize allocations in hot paths:

```go
// BAD - Allocates on every call
func (s *Server) Broadcast(msg []byte) {
    envelope := &MessageEnvelope{
        Data: msg,
        Seq:  s.seqGen.Next(),
    }
    data, _ := json.Marshal(envelope)
    // ...
}

// GOOD - Use sync.Pool for reusable objects
var envelopePool = sync.Pool{
    New: func() any { return &MessageEnvelope{} },
}

func (s *Server) Broadcast(msg []byte) {
    envelope := envelopePool.Get().(*MessageEnvelope)
    defer envelopePool.Put(envelope)

    envelope.Data = msg
    envelope.Seq = s.seqGen.Next()
    // ...
}
```

### Connection Pooling

Reuse client objects:

```go
type ConnectionPool struct {
    pool sync.Pool
}

func NewConnectionPool(bufferSize int) *ConnectionPool {
    return &ConnectionPool{
        pool: sync.Pool{
            New: func() any {
                return &Client{
                    send: make(chan []byte, bufferSize),
                }
            },
        },
    }
}

func (p *ConnectionPool) Get() *Client {
    c := p.pool.Get().(*Client)
    c.Reset()  // Clear previous state
    return c
}

func (p *ConnectionPool) Put(c *Client) {
    c.Reset()  // Clear before returning
    p.pool.Put(c)
}
```

### Avoid Blocking Operations

Use non-blocking channel operations where appropriate:

```go
// BAD - Blocks indefinitely
c.send <- msg

// GOOD - Non-blocking with fallback
select {
case c.send <- msg:
    // Sent successfully
default:
    // Buffer full - client is slow
    c.markSlow()
}

// GOOD - With timeout
select {
case c.send <- msg:
    // Sent successfully
case <-time.After(s.config.SendTimeout):
    // Timeout - disconnect slow client
    c.disconnect("slow_client")
}
```

### Profile Before Optimizing

Always profile before optimizing:

```bash
# CPU profiling
go test -cpuprofile=cpu.prof -bench=.
go tool pprof cpu.prof

# Memory profiling
go test -memprofile=mem.prof -bench=.
go tool pprof mem.prof

# Trace
go test -trace=trace.out -bench=.
go tool trace trace.out
```

---

## Concurrency Patterns

### Goroutine Lifecycle Management

Always manage goroutine lifecycles:

```go
type Server struct {
    ctx    context.Context
    cancel context.CancelFunc
    wg     sync.WaitGroup
}

func (s *Server) Start() {
    s.ctx, s.cancel = context.WithCancel(context.Background())

    s.wg.Add(1)
    go s.worker()
}

func (s *Server) Stop() {
    s.cancel()          // Signal shutdown
    s.wg.Wait()         // Wait for goroutines
}

func (s *Server) worker() {
    defer monitoring.RecoverPanic(s.logger, "worker", nil)
    defer s.wg.Done()

    ticker := time.NewTicker(s.config.WorkerInterval)
    defer ticker.Stop()

    for {
        select {
        case <-s.ctx.Done():
            return
        case <-ticker.C:
            s.doWork()
        }
    }
}
```

### Mutex Usage

Keep critical sections small:

```go
// BAD - Large critical section
func (s *Stats) Update(msg Message) {
    s.mu.Lock()
    defer s.mu.Unlock()

    s.count++
    s.bytes += len(msg.Data)
    s.process(msg)      // Slow operation inside lock!
    s.notify()          // Another slow operation!
}

// GOOD - Minimal critical section
func (s *Stats) Update(msg Message) {
    s.mu.Lock()
    s.count++
    s.bytes += len(msg.Data)
    s.mu.Unlock()

    s.process(msg)      // Outside lock
    s.notify()          // Outside lock
}
```

### Use sync/atomic for Simple Counters

```go
// GOOD - Atomic for simple counters
type Stats struct {
    connections atomic.Int64
    messages    atomic.Int64
}

func (s *Stats) IncrConnections() {
    s.connections.Add(1)
}

// BAD - Mutex for simple counter
type Stats struct {
    mu          sync.Mutex
    connections int64
}

func (s *Stats) IncrConnections() {
    s.mu.Lock()
    s.connections++
    s.mu.Unlock()
}
```

---

## WebSocket SaaS Patterns

### Client Lifecycle

```go
type ClientState int

const (
    StateConnecting ClientState = iota
    StateAuthenticated
    StateSubscribed
    StateDisconnecting
)

type Client struct {
    id            int64
    tenantID      string
    state         atomic.Int32
    subscriptions *SubscriptionSet
    send          chan []byte
    connectedAt   time.Time
}

func (c *Client) SetState(s ClientState) {
    c.state.Store(int32(s))
}

func (c *Client) GetState() ClientState {
    return ClientState(c.state.Load())
}
```

### Message Delivery Guarantees

```go
// At-most-once: Fire and forget (best effort)
func (s *Server) broadcastBestEffort(msg []byte) {
    s.clients.Range(func(key, _ any) bool {
        client := key.(*Client)
        select {
        case client.send <- msg:
        default:
            // Drop if buffer full
        }
        return true
    })
}

// At-least-once: With acknowledgment
type MessageEnvelope struct {
    Seq     int64  `json:"seq"`
    Data    []byte `json:"data"`
    AckBy   int64  `json:"ack_by,omitempty"`  // Deadline
}

func (s *Server) broadcastReliable(msg []byte) {
    envelope := &MessageEnvelope{
        Seq:   s.seqGen.Next(),
        Data:  msg,
        AckBy: time.Now().Add(s.config.AckTimeout).UnixMilli(),
    }

    // Store for replay
    s.replayBuffer.Store(envelope)

    // Send to clients
    s.broadcast(envelope)
}
```

### Slow Client Detection

```go
const (
    slowClientThreshold = 3  // Consecutive failures before disconnect
)

func (c *Client) trySend(msg []byte, timeout time.Duration) bool {
    select {
    case c.send <- msg:
        atomic.StoreInt32(&c.sendAttempts, 0)  // Reset on success
        return true
    case <-time.After(timeout):
        attempts := atomic.AddInt32(&c.sendAttempts, 1)
        if attempts >= slowClientThreshold {
            return false  // Trigger disconnect
        }
        return true  // Allow retry
    }
}
```

### Subscription Management

```go
type SubscriptionSet struct {
    mu      sync.RWMutex
    subs    map[string]struct{}  // channel -> struct{}
    maxSubs int
}

func (s *SubscriptionSet) Add(channel string) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    if len(s.subs) >= s.maxSubs {
        return ErrTooManySubscriptions
    }

    s.subs[channel] = struct{}{}
    return nil
}

func (s *SubscriptionSet) Has(channel string) bool {
    s.mu.RLock()
    defer s.mu.RUnlock()
    _, ok := s.subs[channel]
    return ok
}

func (s *SubscriptionSet) Match(subject string) bool {
    s.mu.RLock()
    defer s.mu.RUnlock()

    // Exact match
    if _, ok := s.subs[subject]; ok {
        return true
    }

    // Wildcard match (e.g., "BTC.*" matches "BTC.trades")
    for pattern := range s.subs {
        if matchWildcard(pattern, subject) {
            return true
        }
    }

    return false
}
```

---

## Code Review Checklist

Before submitting code for review, verify:

### No Hacks / Code Quality (BLOCKING)
- [ ] **No TODO/HACK/FIXME comments** without linked ticket and deadline
- [ ] **No `//nolint` or `#nosec`** without thorough written justification
- [ ] **No ignored errors** - every error handled or explicitly documented why safe
- [ ] **No magic numbers** - all values are named constants or configurable
- [ ] **No copy-paste code** - duplicated logic must be abstracted
- [ ] **No "temporary" solutions** - if it's not production-ready, don't merge it
- [ ] **No `time.Sleep` to "fix" race conditions** - proper synchronization required
- [ ] **No skipped tests** - all tests must pass, no `t.Skip()` without justification

### General
- [ ] No hardcoded values - all configurable
- [ ] All errors wrapped with context (`fmt.Errorf("...: %w", err)`)
- [ ] Structured logging with appropriate fields
- [ ] Panic recovery on all goroutines
- [ ] Code is self-documenting or has clear comments for complex logic

### Security
- [ ] Input validation at ALL boundaries (defense in depth)
- [ ] Rate limiting where appropriate
- [ ] No secrets in logs or errors
- [ ] Authorization checks present
- [ ] No disabled security checks without security team approval

### Performance
- [ ] No allocations in hot paths (or justified with benchmarks)
- [ ] Mutex critical sections are minimal
- [ ] No blocking operations without timeout
- [ ] sync.Pool used for frequent allocations
- [ ] Benchmarks added for performance-critical code

### Testing
- [ ] Table-driven tests for multiple cases
- [ ] Mocks use interfaces
- [ ] No t.Parallel() on shared resource tests
- [ ] Edge cases covered (empty, nil, max values, error paths)
- [ ] Test coverage for new code paths

### Observability
- [ ] Metrics for key operations
- [ ] Audit logging for security events
- [ ] Health check endpoints updated
- [ ] Errors are logged with sufficient context for debugging

### Configuration
- [ ] New settings have environment variables
- [ ] Sensible defaults provided
- [ ] Validation for new config fields
- [ ] Configuration changes documented

---

## Appendix: Go Version Requirements

This codebase requires **Go 1.22+** and uses modern Go features:

| Feature | Go Version | Usage |
|---------|------------|-------|
| `any` type alias | 1.18+ | Replace `interface{}` |
| `atomic.Bool/Int64` | 1.19+ | Type-safe atomics |
| `errors.Join` | 1.20+ | Aggregate errors |
| `strings.Cut` | 1.20+ | String parsing |
| `min/max` builtins | 1.21+ | Numeric comparisons |
| `slices` package | 1.21+ | Slice operations |
| `maps` package | 1.21+ | Map operations |
| `for range N` | 1.22+ | Integer iteration |

---

*Last updated: January 2026*
