// Package server provides a high-performance WebSocket server optimized for
// trading platforms. It handles connection management, message broadcasting,
// rate limiting, and subscription-based filtering with hierarchical channels.
//
// Key features:
//   - Token bucket rate limiting per client
//   - CPU-aware resource guards with hysteresis
//   - Subscription indexing for efficient message routing (93% CPU savings)
//   - Graceful shutdown with connection draining
//   - Integration with Kafka/Redpanda for message consumption
package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/Toniq-Labs/odin-ws/internal/kafka"
	"github.com/Toniq-Labs/odin-ws/internal/limits"
	"github.com/Toniq-Labs/odin-ws/internal/monitoring"
	"github.com/Toniq-Labs/odin-ws/internal/types"
	"github.com/Toniq-Labs/odin-ws/pkg/logging"
	pkgmetrics "github.com/Toniq-Labs/odin-ws/pkg/metrics"
)

// Server is the main WebSocket server that manages client connections, message
// broadcasting, and resource protection. It integrates with Kafka/Redpanda for
// real-time message consumption and uses subscription-based filtering to route
// messages only to interested clients.
//
// Thread Safety: All public methods are safe for concurrent use. Internal state
// is protected by mutexes, atomic operations, and channel-based synchronization.
//
// Lifecycle: Create with NewServer, start with Start, stop with Shutdown.
// The server supports graceful shutdown with configurable connection draining.
type Server struct {
	config        types.ServerConfig // Immutable after creation
	logger        zerolog.Logger     // Structured logger for all server events
	listener      net.Listener       // TCP listener for accepting connections
	kafkaConsumer *kafka.Consumer    // Kafka/Redpanda consumer for market data (managed by KafkaConsumerPool)
	kafkaProducer *kafka.Producer    // Kafka/Redpanda producer for client message publishing (optional)

	// Connection management
	connections       *ConnectionPool    // Pre-allocated connection pool
	clients           sync.Map           // map[*Client]bool - active client tracking
	clientCount       atomic.Int64       // Atomic counter for connection IDs
	connectionsSem    chan struct{}      // Semaphore enforcing MaxConnections limit
	subscriptionIndex *SubscriptionIndex // Channel → subscribers index (93% CPU savings)

	// Rate limiting
	rateLimiter           *limits.RateLimiter           // Per-client message rate limiting
	connectionRateLimiter *limits.ConnectionRateLimiter // Per-IP and global connection throttling

	// Monitoring
	auditLogger      *monitoring.AuditLogger      // Security and operational audit events
	metricsCollector *monitoring.MetricsCollector // Prometheus metrics collector
	resourceGuard    *limits.ResourceGuard        // CPU-aware backpressure with hysteresis

	// Lifecycle
	ctx          context.Context    // Root context for all server goroutines
	cancel       context.CancelFunc // Cancels ctx to signal shutdown
	wg           sync.WaitGroup     // Tracks all server goroutines for clean shutdown
	shuttingDown atomic.Int32       // Atomic flag: 1 = rejecting new connections

	// Stats
	stats *types.Stats // Runtime statistics and metrics

	// Pump for testable read/write operations
	pump *Pump // Handles WebSocket read/write with dependency injection

	// NOTE: Authentication is now handled by ws-gateway
	// ws-server is a dumb broadcaster with network-level security via NetworkPolicy
}

// NewServer creates a new WebSocket server with the provided configuration.
// The broadcastToBusFunc is called for each message consumed from Kafka to
// distribute it across server instances via the BroadcastBus.
//
// The server initializes:
//   - Connection pool with pre-allocated client slots
//   - Subscription index for efficient message routing
//   - Rate limiters (per-client and per-IP if enabled)
//   - Resource guard for CPU-based backpressure
//
// Note: Kafka consumption is handled by KafkaConsumerPool (shared pool mode).
// The server receives a reference to the shared consumer for metrics only.
func NewServer(config types.ServerConfig, _ kafka.BroadcastFunc) (*Server, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Initialize structured logger
	logger := logging.NewLogger(logging.LoggerConfig{
		Level:  logging.LogLevel(config.LogLevel),
		Format: logging.LogFormat(config.LogFormat),
	})

	s := &Server{
		config:            config,
		logger:            logger,
		ctx:               ctx,
		cancel:            cancel,
		connections:       NewConnectionPool(config.MaxConnections, config.ClientSendBufferSize),
		connectionsSem:    make(chan struct{}, config.MaxConnections),
		subscriptionIndex: NewSubscriptionIndex(), // Fast channel → subscribers lookup
		rateLimiter:       limits.NewRateLimiter(),
		stats: &types.Stats{
			StartTime:                  time.Now(),
			DisconnectsByReason:        make(map[string]int64),
			DroppedBroadcastsByChannel: make(map[string]int64),
			BufferSaturationSamples:    make([]int, 0, 100),
		},
	}

	// Initialize monitoring
	// ConsoleAlerter writes to stdout, which is captured by container logging (fluentd/loki/cloudwatch)
	// For dedicated alerting, use MultiAlerter with SlackAlerter via SLACK_WEBHOOK_URL env var
	s.auditLogger = monitoring.NewAuditLogger(monitoring.INFO)
	s.auditLogger.SetAlerter(monitoring.NewConsoleAlerter())
	s.metricsCollector = monitoring.NewMetricsCollector(s)

	// Initialize ResourceGuard with static configuration
	s.resourceGuard = limits.NewResourceGuard(config, logger, &s.stats.CurrentConnections)

	// Initialize connection rate limiter (if enabled)
	if config.ConnectionRateLimitEnabled {
		s.connectionRateLimiter = limits.NewConnectionRateLimiter(limits.ConnectionRateLimiterConfig{
			IPBurst:     config.ConnRateLimitIPBurst,
			IPRate:      config.ConnRateLimitIPRate,
			IPTTL:       5 * time.Minute,
			GlobalBurst: config.ConnRateLimitGlobalBurst,
			GlobalRate:  config.ConnRateLimitGlobalRate,
			Logger:      logger,
		})
		logger.Info().Msg("Connection rate limiting enabled")
	}

	logger.Info().
		Str("addr", config.Addr).
		Int("max_connections", config.MaxConnections).
		Int("kafka_rate_limit", config.MaxKafkaMessagesPerSec).
		Int("broadcast_rate_limit", config.MaxBroadcastsPerSec).
		Msg("Server initialized with ResourceGuard")

	// In shared pool mode, the KafkaConsumerPool handles Kafka consumption.
	// Shards receive a reference to the shared consumer for metrics/replay only.
	// The shared consumer is started/stopped by the pool, not by individual servers.
	if config.SharedKafkaConsumer != nil {
		s.kafkaConsumer = config.SharedKafkaConsumer.(*kafka.Consumer)
		logger.Info().Msg("Using shared Kafka consumer (managed by pool)")
	}

	// Initialize Kafka producer for client message publishing (optional)
	// If configured, clients can publish messages to Kafka via the "publish" message type
	if config.KafkaProducer != nil {
		s.kafkaProducer = config.KafkaProducer.(*kafka.Producer)
		logger.Info().
			Str("topic", s.kafkaProducer.Topic()).
			Msg("Kafka producer enabled for client message publishing")
	}

	// NOTE: Authentication is now handled by ws-gateway
	// ws-server is a dumb broadcaster - no auth logic here

	// Initialize Pump with adapters for testability
	s.pump = NewPump(
		DefaultPumpConfig(),
		NewZerologAdapter(logger),
		logger, // ZerologLogger for panic recovery
		NewRateLimiterAdapter(s.rateLimiter),
		NewAuditLoggerAdapter(s.auditLogger),
		s.stats,
		&RealClock{},
	)

	return s, nil
}

// GetConfig implements monitoring.ServerMetrics interface
func (s *Server) GetConfig() types.ServerConfig {
	return s.config
}

// GetStats implements monitoring.ServerMetrics interface
func (s *Server) GetStats() *types.Stats {
	return s.stats
}

// GetKafkaConsumer implements monitoring.ServerMetrics interface
func (s *Server) GetKafkaConsumer() any {
	return s.kafkaConsumer
}

// Start begins accepting WebSocket connections on the configured address.
// It starts several background goroutines:
//   - HTTP server for WebSocket upgrades, health checks, and Prometheus metrics
//   - Kafka consumer for real-time message consumption
//   - Metrics collection at configured intervals
//   - Memory monitoring and buffer saturation sampling
//   - ResourceGuard CPU monitoring for backpressure
//
// Start returns immediately after launching goroutines. Use Shutdown to stop.
// Returns an error if the TCP listener cannot be created or Kafka fails to start.
func (s *Server) Start() error {
	// Create TCP listener with custom backlog for burst tolerance
	lc := net.ListenConfig{}
	listener, err := lc.Listen(s.ctx, "tcp", s.config.Addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	// Apply custom TCP backlog if configured (trading platform optimization)
	if s.config.TCPListenBacklog > 0 {
		if tcpLn, ok := listener.(*net.TCPListener); ok {
			file, err := tcpLn.File()
			if err == nil {
				// syscall.Listen sets the TCP accept queue size
				// This allows the OS to queue more pending connections during bursts
				// Critical for trading platforms where connection timing affects fairness
				_ = syscall.Listen(int(file.Fd()), s.config.TCPListenBacklog)
				_ = file.Close()

				s.logger.Info().
					Int("backlog", s.config.TCPListenBacklog).
					Msg("Set custom TCP listen backlog for burst tolerance")
			}
		}
	}

	s.listener = listener

	s.logger.Info().
		Str("address", s.config.Addr).
		Msg("Server listening")

	// Note: Kafka consumer is managed by KafkaConsumerPool in shared pool mode.
	// Individual servers don't start/stop the consumer - they just hold a reference
	// for metrics and replay operations. The pool handles lifecycle.

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/metrics", monitoring.HandleMetrics) // Prometheus metrics endpoint

	// NOTE: Token issuance moved to auth-service (internal/authservice)
	// ws-server only handles JWT validation, not token issuance

	server := &http.Server{
		Handler:        mux,
		ReadTimeout:    s.config.HTTPReadTimeout,
		WriteTimeout:   s.config.HTTPWriteTimeout,
		IdleTimeout:    s.config.HTTPIdleTimeout,
		MaxHeaderBytes: 1 << 20,
	}

	s.wg.Add(1)
	go func() {
		// CRITICAL: Panic recovery must be FIRST defer (executes LAST in LIFO order)
		defer logging.RecoverPanic(s.logger, "server.Serve", nil)

		defer s.wg.Done()
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			s.logger.Error().
				Err(err).
				Msg("Server accept loop error")
		}
	}()

	// Start metrics collection
	s.wg.Add(1)
	go s.collectMetrics()

	// Start Prometheus metrics collector
	s.metricsCollector.Start()

	// Start memory monitoring
	s.wg.Add(1)
	go s.monitorMemory()

	// Start buffer saturation sampling
	s.wg.Add(1)
	go s.sampleClientBuffers()

	// Start ResourceGuard monitoring (static limits with safety checks)
	// Now also updates server stats for unified CPU measurement
	s.resourceGuard.StartMonitoring(s.ctx, s.config.MetricsInterval, s.stats)

	s.auditLogger.Info("ServerStarted", "WebSocket server started successfully", map[string]any{
		"addr":           s.config.Addr,
		"maxConnections": s.config.MaxConnections,
	})

	return nil
}

// Shutdown performs a graceful server shutdown with connection draining.
// The shutdown sequence:
//  1. Sets shutdown flag to reject new connections immediately
//  2. Closes the TCP listener (stops accepting new connections)
//  3. Stops the Kafka consumer (no new messages)
//  4. Waits up to 30 seconds for existing connections to drain gracefully
//  5. Force-closes remaining connections after grace period
//  6. Cancels the root context to stop all background goroutines
//  7. Waits for all goroutines to complete
//
// During the grace period, clients can complete pending operations and
// disconnect cleanly. This prevents data loss for in-flight messages.
func (s *Server) Shutdown() error {
	s.logger.Info().Msg("Initiating graceful shutdown")

	// Set shutdown flag to reject new connections
	s.shuttingDown.Store(1)

	// Stop accepting new connections
	if s.listener != nil {
		s.logger.Info().Msg("Closing listener (no new connections accepted)")
		if err := s.listener.Close(); err != nil {
			s.logger.Error().Err(err).Msg("Error closing listener")
		}
	}

	// Note: Kafka consumer lifecycle is managed by KafkaConsumerPool.
	// Servers don't stop the shared consumer - the pool handles it during shutdown.

	// Count current connections
	currentConns := s.stats.CurrentConnections.Load()
	s.logger.Info().
		Int64("active_connections", currentConns).
		Int("grace_period_sec", 30).
		Msg("Draining active connections")

	// Grace period for connection draining
	gracePeriod := 30 * time.Second
	drainTimer := time.NewTimer(gracePeriod)
	checkTicker := time.NewTicker(1 * time.Second)
	defer checkTicker.Stop()
	defer drainTimer.Stop()

	// Monitor connection draining
	for {
		select {
		case <-drainTimer.C:
			// Grace period expired, force close remaining connections
			remaining := s.stats.CurrentConnections.Load()
			if remaining > 0 {
				s.logger.Warn().
					Int64("remaining_connections", remaining).
					Msg("Grace period expired, force closing remaining connections")
			}
			goto forceClose

		case <-checkTicker.C:
			// Check if all connections drained
			remaining := s.stats.CurrentConnections.Load()
			if remaining == 0 {
				s.logger.Info().Msg("All connections drained gracefully")
				goto cleanup
			}
			s.logger.Info().
				Int64("remaining_connections", remaining).
				Msg("Waiting for connections to drain")
		}
	}

forceClose:
	// Force close all remaining connections with proper metrics
	s.clients.Range(func(key, _ any) bool {
		if client, ok := key.(*Client); ok {
			// Record shutdown disconnect (both Prometheus and Stats)
			duration := time.Since(client.connectedAt)
			monitoring.RecordDisconnectWithStats(s.stats, pkgmetrics.DisconnectServerShutdown, pkgmetrics.InitiatedByServer, duration)

			close(client.send)
		}
		return true
	})

cleanup:
	// Cancel context to stop all goroutines
	s.cancel()

	// Stop connection rate limiter cleanup goroutine
	if s.connectionRateLimiter != nil {
		s.connectionRateLimiter.Stop()
	}

	// Wait for all goroutines to finish
	s.logger.Info().Msg("Waiting for all goroutines to finish")
	s.wg.Wait()

	s.logger.Info().Msg("Graceful shutdown completed")
	return nil
}
