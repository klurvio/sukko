// Package wsserver provides a high-performance WebSocket server optimized for
// trading platforms. It handles connection management, message broadcasting,
// rate limiting, and subscription-based filtering with hierarchical channels.
//
// Key features:
//   - Token bucket rate limiting per client
//   - CPU-aware resource guards with hysteresis
//   - Subscription indexing for efficient message routing (93% CPU savings)
//   - Graceful shutdown with connection draining
//   - Integration with Kafka/Redpanda for message consumption
package wsserver

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/adred-codev/ws_poc/internal/kafka"
	"github.com/adred-codev/ws_poc/internal/limits"
	"github.com/adred-codev/ws_poc/internal/monitoring"
	"github.com/adred-codev/ws_poc/internal/types"
	"github.com/rs/zerolog"
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
	kafkaConsumer *kafka.Consumer    // Kafka/Redpanda consumer for market data

	// Connection management
	connections       *ConnectionPool    // Pre-allocated connection pool
	clients           sync.Map           // map[*Client]bool - active client tracking
	clientCount       int64              // Atomic counter for connection IDs
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
	shuttingDown int32              // Atomic flag: 1 = rejecting new connections

	// Stats
	stats *types.Stats // Runtime statistics and metrics

	// Pump for testable read/write operations
	pump *Pump // Handles WebSocket read/write with dependency injection
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
//   - Kafka consumer (unless DisableKafkaConsumer is set for shared pool mode)
//
// Returns an error if Kafka consumer initialization fails.
func NewServer(config types.ServerConfig, broadcastToBusFunc kafka.BroadcastFunc) (*Server, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Initialize structured logger
	logger := monitoring.NewLogger(monitoring.LoggerConfig{
		Level:  config.LogLevel,
		Format: config.LogFormat,
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
	s.auditLogger = monitoring.NewAuditLogger(monitoring.INFO) // Log INFO and above
	s.auditLogger.SetAlerter(monitoring.NewConsoleAlerter())   // Use console alerter for now
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

	// Initialize Kafka consumer (skip if DisableKafkaConsumer flag is set for shared pool mode)
	if len(config.KafkaBrokers) > 0 && !config.DisableKafkaConsumer {
		consumer, err := kafka.NewConsumer(kafka.ConsumerConfig{
			Brokers:       config.KafkaBrokers,
			ConsumerGroup: config.ConsumerGroup,
			Topics:        kafka.AllTopics(),
			Logger:        &logger,
			Broadcast:     broadcastToBusFunc, // Use the provided broadcast function
			ResourceGuard: s.resourceGuard,    // Enable rate limiting and CPU brake
		})
		if err != nil {
			s.auditLogger.Critical("KafkaConnectionFailed", "Failed to create Kafka consumer", map[string]any{
				"brokers": config.KafkaBrokers,
				"error":   err.Error(),
			})
			return nil, fmt.Errorf("failed to create kafka consumer: %w", err)
		}
		s.kafkaConsumer = consumer
		logger.Printf("Created Kafka consumer for brokers: %v", config.KafkaBrokers)

		s.auditLogger.Info("KafkaConsumerCreated", "Kafka consumer created successfully", map[string]any{
			"brokers":        config.KafkaBrokers,
			"consumer_group": config.ConsumerGroup,
			"topics":         kafka.AllTopics(),
		})
	} else if config.DisableKafkaConsumer {
		// In shared pool mode, use the shared consumer reference for replay operations
		if config.SharedKafkaConsumer != nil {
			s.kafkaConsumer = config.SharedKafkaConsumer.(*kafka.Consumer)
			logger.Printf("Using shared Kafka consumer for message replay")
		} else {
			logger.Printf("Kafka consumer creation skipped (using shared pool mode)")
		}
	}

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
	listener, err := net.Listen("tcp", s.config.Addr)
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

	// Start Kafka consumer
	if s.kafkaConsumer != nil {
		if err := s.kafkaConsumer.Start(); err != nil {
			s.auditLogger.Critical("KafkaStartFailed", "Failed to start Kafka consumer", map[string]any{
				"error": err.Error(),
			})
			return fmt.Errorf("failed to start kafka consumer: %w", err)
		}
		s.logger.Info().
			Msg("Started Kafka consumer for all topics")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/metrics", monitoring.HandleMetrics) // Prometheus metrics endpoint

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
		defer monitoring.RecoverPanic(s.logger, "server.Serve", nil)

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
	atomic.StoreInt32(&s.shuttingDown, 1)

	// Stop accepting new connections
	if s.listener != nil {
		s.logger.Info().Msg("Closing listener (no new connections accepted)")
		if err := s.listener.Close(); err != nil {
			s.logger.Error().Err(err).Msg("Error closing listener")
		}
	}

	// Stop receiving new messages from Kafka
	if s.kafkaConsumer != nil {
		s.logger.Info().Msg("Stopping Kafka consumer (no new messages)")
		if err := s.kafkaConsumer.Stop(); err != nil {
			s.logger.Error().
				Err(err).
				Msg("Error stopping Kafka consumer")
		}
	}

	// Count current connections
	currentConns := atomic.LoadInt64(&s.stats.CurrentConnections)
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
			remaining := atomic.LoadInt64(&s.stats.CurrentConnections)
			if remaining > 0 {
				s.logger.Warn().
					Int64("remaining_connections", remaining).
					Msg("Grace period expired, force closing remaining connections")
			}
			goto forceClose

		case <-checkTicker.C:
			// Check if all connections drained
			remaining := atomic.LoadInt64(&s.stats.CurrentConnections)
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
	s.clients.Range(func(key, value any) bool {
		if client, ok := key.(*Client); ok {
			// Record shutdown disconnect (both Prometheus and Stats)
			duration := time.Since(client.connectedAt)
			monitoring.RecordDisconnectWithStats(s.stats, monitoring.DisconnectReasonServerShutdown, monitoring.DisconnectInitiatedByServer, duration)

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
