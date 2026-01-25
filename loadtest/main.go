// Package main provides a load testing tool for WebSocket servers.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
)

// Configuration
type Config struct {
	WSURL              string
	HealthURL          string
	TargetConnections  int
	RampRate           int // connections per second
	SustainDurationSec int
	ReportIntervalSec  int
	HealthCheckSec     int
	Channels           []string
	SubscriptionMode   string // "all", "single", "random"
	ChannelsPerClient  int
	ConnectionTimeout  int // connection timeout in milliseconds
	MaxConnections     int // server max connections (for test mode detection)

	// Authentication
	Token     string // Pre-generated JWT token
	JWTSecret string // Secret to generate test tokens
	Principal string // Principal ID for generated tokens
}

// State tracks test metrics
type State struct {
	// Connection tracking
	activeConnections int64
	totalCreated      int64
	failedConnections int64
	connectionErrors  sync.Map // map[string]int64

	// Message metrics
	messagesReceived    int64
	errors              int64
	messagesFilteredOut int64

	// Subscription metrics
	subscriptionsSent      int64
	subscriptionsConfirmed int64
	subscriptionsFailed    int64

	// Health monitoring
	lastHealthCheck *HealthResponse

	// Timing
	startTime        time.Time
	rampStartTime    time.Time
	sustainStartTime time.Time
	phase            string // "ramping", "sustaining", "completed"

	mu sync.RWMutex
}

// HealthResponse from server
// Supports both ws-gateway format: {"status":"ok","service":"ws-gateway"}
// And ws-server format: {"status":"ok","healthy":true,"checks":{...}}
type HealthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"` // ws-gateway includes this
	Healthy *bool  `json:"healthy"` // Pointer to distinguish missing from false
	Checks  struct {
		Capacity struct {
			Current int `json:"current"`
		} `json:"capacity"`
		CPU struct {
			Percentage float64 `json:"percentage"`
		} `json:"cpu"`
		Memory struct {
			Percentage float64 `json:"percentage"`
		} `json:"memory"`
	} `json:"checks"`
}

// IsHealthy returns true if the server is healthy
// Handles both gateway format (status=ok) and server format (healthy=true)
func (h *HealthResponse) IsHealthy() bool {
	// Gateway format: status == "ok" means healthy
	if h.Status == "ok" {
		return true
	}
	// Server format: explicit healthy field
	if h.Healthy != nil && *h.Healthy {
		return true
	}
	return false
}

// Connection represents a WebSocket client
// IMPORTANT: Keep this simple to match JavaScript browser client behavior
// No fancy optimizations - just what a browser does
type Connection struct {
	id                  int
	ws                  *websocket.Conn
	messagesReceived    int64
	connected           bool
	subscribedChannels  []string
	subscribed          bool
	subscriptionPending bool
	ctx                 context.Context
	cancel              context.CancelFunc
	writeMu             sync.Mutex
	connectTime         time.Time
	closeOnce           sync.Once // Ensure close() is called only once
}

var (
	state  *State
	config *Config
)

func main() {
	// Parse command-line flags
	config = parseFlags()

	// Initialize state
	state = &State{
		startTime:     time.Now(),
		rampStartTime: time.Now(),
		phase:         "ramping",
	}

	log.Printf("%s", "\n"+strings.Repeat("=", 80))
	log.Printf("🧪 SUSTAINED LOAD TEST (Go Client)")
	log.Printf("%s", strings.Repeat("=", 80))

	// Determine test mode
	testMode := "📊 CAPACITY TEST"
	testModeDesc := "Testing at server capacity limit"
	if config.TargetConnections > config.MaxConnections {
		testMode = "⚠️  STRESS/OVERLOAD TEST"
		testModeDesc = fmt.Sprintf("Intentional overload (%d > %d limit)", config.TargetConnections, config.MaxConnections)
	} else if config.RampRate >= 1000 {
		testMode = "⚡ BURST/SPIKE TEST"
		testModeDesc = fmt.Sprintf("Rapid connection burst (%d conn/sec)", config.RampRate)
	}

	log.Printf("\n%s", testMode)
	log.Printf("   %s", testModeDesc)
	log.Printf("\n📋 Configuration:")
	log.Printf("   Target:       %d connections", config.TargetConnections)
	log.Printf("   Server Limit: %d connections (WS_MAX_CONNECTIONS)", config.MaxConnections)
	log.Printf("   Ramp Rate:    %d conn/sec", config.RampRate)
	log.Printf("   Timeout:      %ds (connection timeout)", config.ConnectionTimeout/1000)
	log.Printf("   Sustain:      %ds (%d minutes)", config.SustainDurationSec, config.SustainDurationSec/60)
	log.Printf("   Server:       %s", config.WSURL)
	log.Printf("   Health:       %s", config.HealthURL)

	if config.Token != "" {
		log.Printf("\n🔐 Authentication:")
		log.Printf("   Principal:    %s", config.Principal)
		log.Printf("   Token:        %s...%s", config.Token[:10], config.Token[len(config.Token)-10:])
	} else {
		log.Printf("\n⚠️  Authentication: DISABLED (no token provided)")
	}

	if len(config.Channels) > 0 {
		log.Printf("\n🔔 Subscription Settings:")
		log.Printf("   Mode:         %s", config.SubscriptionMode)
		log.Printf("   Channels:     %v (%d total)", config.Channels, len(config.Channels))
		if config.SubscriptionMode == "random" {
			log.Printf("   Per Client:   %d channels", config.ChannelsPerClient)
		}
		log.Printf("   Impact:       Expected %dx reduction in message fanout", len(config.Channels))
	} else {
		log.Printf("\n⚠️  Subscription Filtering: DISABLED (all clients receive all messages)")
	}

	log.Printf("%s", "\n"+strings.Repeat("=", 80)+"\n")

	// Setup signal handling
	ctx, cancel := context.WithCancel(context.Background())

	// Initial health check
	log.Printf("🏥 Performing initial health check...")
	if err := checkServerHealth(ctx); err != nil {
		log.Fatalf("❌ Server health check failed: %v", err)
	}
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Printf("\n🛑 Received shutdown signal, gracefully shutting down...")
		cancel()
	}()

	// Start periodic health checks
	go periodicHealthChecks(ctx)

	// Start periodic reporting
	go periodicReports(ctx)

	// Ramp up connections
	if err := rampUpConnections(ctx); err != nil {
		log.Printf("❌ Ramp-up failed: %v", err)
		return
	}

	// Sustain phase
	if state.phase == "sustaining" {
		log.Printf("🔒 Sustaining load for %ds...", config.SustainDurationSec)
		select {
		case <-time.After(time.Duration(config.SustainDurationSec) * time.Second):
			state.phase = "completed"
		case <-ctx.Done():
			log.Printf("⚠️  Sustain phase interrupted")
		}
	}

	// Final report
	log.Printf("\n✅ Test completed!")
	printReport()

	log.Printf("🎉 Sustained load test finished!")
	log.Printf("Program finished.")
}

func parseFlags() *Config {
	cfg := &Config{}

	flag.StringVar(&cfg.WSURL, "url", getEnv("WS_URL", "ws://localhost:3000/ws"), "WebSocket server URL (gateway)")
	flag.StringVar(&cfg.HealthURL, "health", getEnv("HEALTH_URL", "http://localhost:3000/health"), "Health check URL")
	flag.IntVar(&cfg.TargetConnections, "connections", getEnvInt("TARGET_CONNECTIONS", 7000), "Target number of connections")
	flag.IntVar(&cfg.RampRate, "ramp-rate", getEnvInt("RAMP_RATE", 100), "Connections per second during ramp-up")
	flag.IntVar(&cfg.SustainDurationSec, "duration", getEnvInt("DURATION", 1800), "Sustain duration in seconds")
	flag.IntVar(&cfg.ReportIntervalSec, "report-interval", 10, "Report interval in seconds")
	flag.IntVar(&cfg.HealthCheckSec, "health-interval", 5, "Health check interval in seconds")
	flag.IntVar(&cfg.ConnectionTimeout, "connection-timeout", getEnvInt("CONNECTION_TIMEOUT", 10000), "Connection timeout in milliseconds")
	flag.IntVar(&cfg.MaxConnections, "max-connections", getEnvInt("WS_MAX_CONNECTIONS", 18000), "Server max connections (for test mode detection)")

	// Authentication flags
	flag.StringVar(&cfg.Token, "token", getEnv("JWT_TOKEN", ""), "Pre-generated JWT token for authentication")
	flag.StringVar(&cfg.JWTSecret, "jwt-secret", getEnv("JWT_SECRET", ""), "JWT secret to generate test tokens (min 32 chars)")
	flag.StringVar(&cfg.Principal, "principal", getEnv("PRINCIPAL", "loadtest-user"), "Principal ID for generated tokens")

	channelsStr := flag.String("channels", getEnv("CHANNELS", "BTC.trade,ETH.trade,SOL.trade,ODIN.trade,DOGE.trade"), "Comma-separated list of channels")
	flag.StringVar(&cfg.SubscriptionMode, "subscription-mode", getEnv("SUBSCRIPTION_MODE", "all"), "Subscription mode: all, single, random")
	flag.IntVar(&cfg.ChannelsPerClient, "channels-per-client", getEnvInt("CHANNELS_PER_CLIENT", 3), "Channels per client (for random mode)")

	flag.Parse()

	// Parse channels
	if *channelsStr != "" {
		cfg.Channels = strings.Split(*channelsStr, ",")
		for i := range cfg.Channels {
			cfg.Channels[i] = strings.TrimSpace(cfg.Channels[i])
		}
	}

	// Generate token if secret provided but no token
	if cfg.Token == "" && cfg.JWTSecret != "" {
		token, err := generateTestToken(cfg.JWTSecret, cfg.Principal)
		if err != nil {
			log.Fatalf("❌ Failed to generate JWT token: %v", err)
		}
		cfg.Token = token
		log.Printf("🔑 Generated JWT token for principal: %s", cfg.Principal)
	}

	return cfg
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

// generateTestToken creates a JWT token for loadtest authentication
func generateTestToken(secret, principal string) (string, error) {
	if len(secret) < 32 {
		return "", errors.New("JWT secret must be at least 32 characters")
	}

	claims := jwt.MapClaims{
		"sub":       principal,
		"tenant_id": "loadtest",
		"groups":    []string{"loadtest"},
		"iat":       time.Now().Unix(),
		"exp":       time.Now().Add(24 * time.Hour).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func rampUpConnections(ctx context.Context) error {
	log.Printf("🚀 Starting ramp-up: %d connections at %d/sec", config.TargetConnections, config.RampRate)

	batchSize := max(1, config.RampRate/10) // 10 batches per second, minimum 1
	batchInterval := 100 * time.Millisecond

	ticker := time.NewTicker(batchInterval)
	defer ticker.Stop()

	connectionID := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// Check if we've reached target
			if atomic.LoadInt64(&state.totalCreated) >= int64(config.TargetConnections) {
				state.phase = "sustaining"
				state.sustainStartTime = time.Now()
				active := atomic.LoadInt64(&state.activeConnections)
				log.Printf("✅ Ramp-up complete! %d connections established", active)
				log.Printf("🔒 Sustaining load for %ds...", config.SustainDurationSec)
				return nil
			}

			// Create batch of connections
			var wg sync.WaitGroup
			for i := 0; i < batchSize && atomic.LoadInt64(&state.totalCreated) < int64(config.TargetConnections); i++ {
				wg.Add(1)
				id := connectionID
				connectionID++
				atomic.AddInt64(&state.totalCreated, 1)

				go func(connID int) {
					defer wg.Done()
					log.Printf("Creating connection %d", connID)
					conn := NewConnection(ctx, connID)
					if err := conn.Connect(); err != nil {
						atomic.AddInt64(&state.failedConnections, 1)
						// Track error type
						errorType := "UNKNOWN"
						if err != nil {
							errorType = err.Error()
						}
						if val, _ := state.connectionErrors.LoadOrStore(errorType, new(int64)); val != nil {
							atomic.AddInt64(val.(*int64), 1)
						}
					}
				}(id)
			}
			wg.Wait()
		}
	}
}

func NewConnection(ctx context.Context, id int) *Connection {
	connCtx, cancel := context.WithCancel(ctx)
	log.Printf("NewConnection: %d", id)
	return &Connection{
		id:     id,
		ctx:    connCtx,
		cancel: cancel,
	}
}

func (c *Connection) Connect() error {
	log.Printf("Connecting connection %d", c.id)
	// Configure dialer to match Node.js WebSocket client behavior
	// ONLY difference: TCP keep-alive for Docker/GCE (Node.js doesn't expose this)
	dialer := websocket.Dialer{
		HandshakeTimeout: time.Duration(config.ConnectionTimeout) * time.Millisecond,

		// CRITICAL FOR DOCKER/GCE: TCP keep-alive prevents stale connections
		// when cloud load balancer drops idle connections
		NetDialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			d := &net.Dialer{
				Timeout:   time.Duration(config.ConnectionTimeout) * time.Millisecond,
				KeepAlive: 30 * time.Second,
			}
			conn, err := d.DialContext(ctx, network, addr)
			if err != nil {
				log.Printf("Connection %d: dial context error: %v", c.id, err)
				return nil, err
			}

			if tcpConn, ok := conn.(*net.TCPConn); ok {
				_ = tcpConn.SetKeepAlive(true)
				_ = tcpConn.SetKeepAlivePeriod(30 * time.Second)
			}

			return conn, nil
		},
	}

	u, err := url.Parse(config.WSURL)
	if err != nil {
		log.Printf("Connection %d: url parse error: %v", c.id, err)
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Add JWT token as query parameter for authentication
	if config.Token != "" {
		q := u.Query()
		q.Set("token", config.Token)
		u.RawQuery = q.Encode()
	}

	ws, resp, err := dialer.Dial(u.String(), nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		log.Printf("Connection %d: dial error: %v", c.id, err)
		return fmt.Errorf("dial failed: %w", err)
	}

	log.Printf("Connection %d: connected", c.id)
	c.ws = ws
	c.connected = true
	c.connectTime = time.Now()
	atomic.AddInt64(&state.activeConnections, 1)

	// ============================================================================
	// WebSocket Keep-Alive Configuration (Industry Standard: Coinbase/Bloomberg)
	// ============================================================================
	// Server sends PING every 27 seconds, expects PONG within 30 seconds
	// Client MUST respond to PING frames to keep connection alive
	//
	// How gorilla/websocket handles PING/PONG:
	// - Automatically responds to PING with PONG (RFC 6455 compliant)
	// - Requires read deadline to be set (prevents zombie connections)
	// - PongHandler extends deadline when PONG received from server
	//
	// Timeout: 60 seconds (2× server ping interval for safety)
	// Why 60s: Server pings every 27s, allows for 1 missed ping before timeout
	const readTimeout = 60 * time.Second

	// Set initial read deadline (prevents zombie connections)
	_ = c.ws.SetReadDeadline(time.Now().Add(readTimeout))

	// Configure PONG handler (called when server sends PING and we auto-respond)
	// This extends the deadline every time we successfully respond to a PING
	c.ws.SetPongHandler(func(_ string) error {
		_ = c.ws.SetReadDeadline(time.Now().Add(readTimeout))
		return nil
	})

	// Note: gorilla/websocket automatically responds to PING with PONG
	// We don't need SetPingHandler - the library handles it per RFC 6455
	// ============================================================================

	// Auto-subscribe if channels configured (matches Node.js line 226-228)
	if len(config.Channels) > 0 {
		c.autoSubscribe()
	}

	// Start message handling (matches Node.js line 233-250)
	go c.readPump()
	go c.writePump()

	return nil
}

func (c *Connection) autoSubscribe() {
	var channelsToSubscribe []string

	switch config.SubscriptionMode {
	case "all":
		channelsToSubscribe = config.Channels
	case "single":
		idx := c.id % len(config.Channels)
		channelsToSubscribe = []string{config.Channels[idx]}
	case "random":
		numChannels := min(config.ChannelsPerClient, len(config.Channels))
		// Simple random selection
		perm := rand.Perm(len(config.Channels))
		for i := range numChannels {
			channelsToSubscribe = append(channelsToSubscribe, config.Channels[perm[i]])
		}
	default:
		channelsToSubscribe = config.Channels
	}

	c.subscribe(channelsToSubscribe)
}

func (c *Connection) subscribe(channels []string) {
	if c.ws == nil || !c.connected || c.subscriptionPending {
		return
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	msg := map[string]any{
		"type": "subscribe",
		"data": map[string]any{
			"channels": channels,
		},
	}

	if err := c.ws.WriteJSON(msg); err != nil {
		atomic.AddInt64(&state.subscriptionsFailed, 1)
		return
	}

	c.subscribedChannels = channels
	c.subscriptionPending = true
	atomic.AddInt64(&state.subscriptionsSent, 1)
}

func (c *Connection) readPump() {
	defer func() {
		c.close()
	}()

	// Simple message loop - matches Node.js behavior (line 233-250)
	// No batching needed - Go goroutines don't have event loop bottleneck
	const readTimeout = 60 * time.Second

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		var msg map[string]any
		if err := c.ws.ReadJSON(&msg); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("Connection %d closed unexpectedly: %v", c.id, err)
			}
			return
		}

		// Reset read deadline on every successful message
		// This keeps connection alive during active message flow
		_ = c.ws.SetReadDeadline(time.Now().Add(readTimeout))

		// Handle different message types (matches Node.js line 269-298)
		msgType, _ := msg["type"].(string)
		switch msgType {
		case "subscription_ack":
			c.subscribed = true
			c.subscriptionPending = false
			atomic.AddInt64(&state.subscriptionsConfirmed, 1)
		case "unsubscription_ack":
			// Handle unsubscription acknowledgment
		case "pong":
			// Heartbeat response
		default:
			// Regular message
			atomic.AddInt64(&c.messagesReceived, 1)
			atomic.AddInt64(&state.messagesReceived, 1)

			// Track pre-subscription messages (shouldn't happen with server filtering)
			if len(config.Channels) > 0 && !c.subscribed {
				atomic.AddInt64(&state.messagesFilteredOut, 1)
			}
		}
	}
}

func (c *Connection) writePump() {
	// Send heartbeat every 15 seconds (CRITICAL: Must be < server's 30s timeout)
	// Industry standard (Coinbase): Heartbeat at 1/2 of server timeout
	// Server timeout: 30s (pongWait in server.go:33)
	// Client heartbeat: 15s (safe margin to prevent race conditions)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if c.ws == nil || !c.connected {
				return
			}

			c.writeMu.Lock()
			heartbeat := map[string]any{
				"type": "heartbeat",
			}
			err := c.ws.WriteJSON(heartbeat)
			c.writeMu.Unlock()

			if err != nil {
				// CRITICAL: Matches Node.js error detection (line 375-385)
				// When heartbeat send fails, connection is dead - close it properly
				// This prevents "dwindling connections" where dead connections stay tracked as active
				log.Printf("⚠️  Connection %d dead (heartbeat send failed): %v", c.id, err)
				c.close()
				return
			}
		}
	}
}

func (c *Connection) close() {
	// Use sync.Once to ensure this only executes once, even if called from multiple goroutines
	// This fixes the race condition where readPump and writePump both call close()
	c.closeOnce.Do(func() {
		c.connected = false

		// Only decrement counter once per connection
		atomic.AddInt64(&state.activeConnections, -1)

		if c.ws != nil {
			_ = c.ws.Close()
		}

		c.cancel()
	})
}

func checkServerHealth(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, config.HealthURL, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	var health HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return err
	}

	state.mu.Lock()
	state.lastHealthCheck = &health
	state.mu.Unlock()

	if !health.IsHealthy() {
		log.Printf("⚠️  Server reports unhealthy status but continuing...")
	}

	return nil
}

func periodicHealthChecks(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(config.HealthCheckSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := checkServerHealth(ctx); err != nil {
				log.Printf("❌ Health check failed: %v", err)
			}
		}
	}
}

func periodicReports(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(config.ReportIntervalSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			printReport()
		}
	}
}

func printReport() {
	elapsed := int(time.Since(state.startTime).Seconds())

	state.mu.RLock()
	health := state.lastHealthCheck
	state.mu.RUnlock()

	active := atomic.LoadInt64(&state.activeConnections)
	totalCreated := atomic.LoadInt64(&state.totalCreated)
	failed := atomic.LoadInt64(&state.failedConnections)
	messagesRcvd := atomic.LoadInt64(&state.messagesReceived)
	errors := atomic.LoadInt64(&state.errors)

	successRate := 100.0
	if totalCreated > 0 {
		successRate = float64(totalCreated-failed) / float64(totalCreated) * 100
	}

	msgRate := float64(messagesRcvd) / float64(max(elapsed, 1))

	serverConns := 0
	cpuUsage := 0.0
	memUsage := 0.0
	if health != nil {
		serverConns = health.Checks.Capacity.Current
		cpuUsage = health.Checks.CPU.Percentage
		memUsage = health.Checks.Memory.Percentage
	}

	log.Printf("%s", "\n"+strings.Repeat("=", 80))
	log.Printf("📊 SUSTAINED LOAD TEST - Elapsed: %ds - Phase: %s", elapsed, strings.ToUpper(state.phase))
	log.Printf("%s", strings.Repeat("=", 80))
	log.Printf("\n🔌 Connections:")
	log.Printf("   Active:       %d / %d target", active, config.TargetConnections)
	log.Printf("   Created:      %d", totalCreated)
	log.Printf("   Failed:       %d", failed)
	log.Printf("   Success Rate: %.1f%%", successRate)
	log.Printf("   Server Reports: %d active", serverConns)

	log.Printf("\n📨 Messages:")
	log.Printf("   Received:     %s", formatNumber(messagesRcvd))
	log.Printf("   Rate:         %.2f msg/sec", msgRate)

	// Calculate error percentage (avoid division by zero)
	divisor := max(messagesRcvd, 1)
	log.Printf("   Errors:       %d (%.2f%%)", errors, float64(errors)/float64(divisor)*100)

	filteredOut := atomic.LoadInt64(&state.messagesFilteredOut)
	if filteredOut > 0 {
		log.Printf("   ⚠️  Pre-sub msgs: %d (should be 0 with filtering)", filteredOut)
	}

	if len(config.Channels) > 0 {
		subsSent := atomic.LoadInt64(&state.subscriptionsSent)
		subsConfirmed := atomic.LoadInt64(&state.subscriptionsConfirmed)
		subsFailed := atomic.LoadInt64(&state.subscriptionsFailed)

		subRate := 100.0
		if subsSent > 0 {
			subRate = float64(subsConfirmed) / float64(subsSent) * 100
		}

		log.Printf("\n🔔 Subscriptions:")
		log.Printf("   Mode:         %s", config.SubscriptionMode)
		log.Printf("   Channels:     %v", config.Channels)
		log.Printf("   Sent:         %d", subsSent)
		log.Printf("   Confirmed:    %d", subsConfirmed)
		log.Printf("   Failed:       %d", subsFailed)
		log.Printf("   Success Rate: %.1f%%", subRate)
	}

	log.Printf("\n💻 Server Health:")
	if health != nil {
		healthStatus := "✅ Healthy"
		if !health.IsHealthy() {
			healthStatus = "❌ Unhealthy"
		}
		log.Printf("   Status:       %s", healthStatus)
		log.Printf("   CPU:          %.1f%%", cpuUsage)
		log.Printf("   Memory:       %.1f%%", memUsage)
	} else {
		log.Printf("   Status:       ⚠️  No health data")
	}

	switch state.phase {
	case "ramping":
		rampElapsed := int(time.Since(state.rampStartTime).Seconds())
		rampProgress := float64(totalCreated) / float64(config.TargetConnections) * 100
		log.Printf("\n🚀 Ramp Progress:")
		log.Printf("   Progress:     %.1f%%", rampProgress)
		log.Printf("   Time:         %ds", rampElapsed)
	case "sustaining":
		sustainElapsed := int(time.Since(state.sustainStartTime).Seconds())
		remaining := max(0, config.SustainDurationSec-sustainElapsed)
		log.Printf("\n🔒 Sustain Status:")
		log.Printf("   Elapsed:      %ds", sustainElapsed)
		log.Printf("   Remaining:    %ds", remaining)
	}

	// Print connection errors if any
	hasErrors := false
	state.connectionErrors.Range(func(_, _ any) bool {
		hasErrors = true
		return false
	})

	if hasErrors {
		log.Printf("\n⚠️  Connection Errors:")
		state.connectionErrors.Range(func(key, value any) bool {
			count := atomic.LoadInt64(value.(*int64))
			log.Printf("   %s: %d", key, count)
			return true
		})
	}

	log.Printf("%s", strings.Repeat("=", 80)+"\n")
}

func formatNumber(n int64) string {
	// Match Node.js toLocaleString() behavior
	if n < 1000 {
		return strconv.FormatInt(n, 10)
	}
	// Use comma formatting like Node.js
	str := strconv.FormatInt(n, 10)
	result := make([]rune, 0, len(str)+len(str)/3)
	for i, ch := range str {
		if i > 0 && (len(str)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, ch)
	}
	return string(result)
}
