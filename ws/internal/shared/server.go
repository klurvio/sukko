package shared

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/adred-codev/ws_poc/internal/shared/kafka"
	"github.com/adred-codev/ws_poc/internal/shared/limits"
	"github.com/adred-codev/ws_poc/internal/shared/monitoring"
	"github.com/adred-codev/ws_poc/internal/shared/types"
	"github.com/rs/zerolog"
)

const (
	// Time allowed to write a message to the peer.
	// Reduced from 10s to 5s for faster detection of slow clients
	writeWait = 5 * time.Second

	// Time allowed to read the next pong message from the peer.
	// Reduced from 60s to 30s to detect dead connections faster
	// Industry standard: 15-30s (Bloomberg: 15s, Coinbase: 30s)
	pongWait = 30 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	// = 27 seconds with new pongWait
	pingPeriod = (pongWait * 9) / 10
)

type Server struct {
	config        types.ServerConfig
	logger        zerolog.Logger // Structured logger
	listener      net.Listener
	kafkaConsumer *kafka.Consumer
	kafkaPaused   int32 // Atomic flag: 1 = paused, 0 = active

	// Connection management
	connections       *ConnectionPool
	clients           sync.Map // map[*Client]bool
	clientCount       int64
	connectionsSem    chan struct{}      // Semaphore for max connections
	subscriptionIndex *SubscriptionIndex // Fast lookup: channel → subscribers (93% CPU savings!)

	// Rate limiting
	rateLimiter           *limits.RateLimiter
	connectionRateLimiter *limits.ConnectionRateLimiter

	// Monitoring
	auditLogger      *monitoring.AuditLogger
	metricsCollector *monitoring.MetricsCollector
	resourceGuard    *limits.ResourceGuard // Replaces DynamicCapacityManager

	// Lifecycle
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	shuttingDown int32 // Atomic flag for graceful shutdown

	// Stats
	stats *types.Stats
}

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
func (s *Server) GetKafkaConsumer() interface{} {
	return s.kafkaConsumer
}

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
				syscall.Listen(int(file.Fd()), s.config.TCPListenBacklog)
				file.Close()

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

func (s *Server) Shutdown() error {
	s.logger.Info().Msg("Initiating graceful shutdown")

	// Set shutdown flag to reject new connections
	atomic.StoreInt32(&s.shuttingDown, 1)

	// Stop accepting new connections
	if s.listener != nil {
		s.logger.Info().Msg("Closing listener (no new connections accepted)")
		s.listener.Close()
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
	s.clients.Range(func(key, value interface{}) bool {
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
