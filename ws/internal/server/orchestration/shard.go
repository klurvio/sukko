package orchestration

import (
	"context"
	"fmt"
	"sync"

	"github.com/rs/zerolog"

	"github.com/Toniq-Labs/odin-ws/internal/server"
	"github.com/Toniq-Labs/odin-ws/internal/server/metrics"
	"github.com/Toniq-Labs/odin-ws/internal/server/broadcast"
	"github.com/Toniq-Labs/odin-ws/internal/shared/types"
	"github.com/Toniq-Labs/odin-ws/internal/shared/logging"
)

// Shard represents a single instance of the WebSocket server, running on its own core.
// It manages a subset of total connections and communicates via a central BroadcastBus.
// Capacity control is handled by the server's ResourceGuard.
type Shard struct {
	ID             int // Exported for external access
	server         *server.Server
	advertiseAddr  string                    // Address advertised to LoadBalancer (e.g., localhost:3002)
	broadcastChan  <-chan *broadcast.Message // Channel to receive messages from the central bus
	logger         zerolog.Logger
	maxConnections int // Max connections this shard can handle

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// ShardConfig holds configuration for a single Shard
type ShardConfig struct {
	ID                  int
	Addr                string // Address for this shard to bind/listen on (e.g., 0.0.0.0:3002)
	AdvertiseAddr       string // Address advertised to LoadBalancer (e.g., localhost:3002)
	ServerConfig        types.ServerConfig
	BroadcastBus        broadcast.Bus // Reference to the central bus
	SharedKafkaConsumer any           // Shared Kafka consumer (managed by KafkaConsumerPool)
	KafkaProducer       any           // Kafka producer for client message publishing (optional)
	Logger              zerolog.Logger
	MaxConnections      int
}

// NewShard creates a new Shard instance
func NewShard(cfg ShardConfig) (*Shard, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Create a server.Server instance for this shard
	// Kafka consumption is handled by KafkaConsumerPool, not individual shards
	serverConfig := cfg.ServerConfig
	serverConfig.MaxConnections = cfg.MaxConnections           // Override with shard-specific limit
	serverConfig.SharedKafkaConsumer = cfg.SharedKafkaConsumer // Pass shared consumer for metrics
	serverConfig.KafkaProducer = cfg.KafkaProducer             // Pass shared producer for client publishing

	// Create broadcast function that will publish to the central bus
	// instead of directly broadcasting to local clients
	// The subject (Kafka Key) IS the broadcast channel
	broadcastToBusFunc := func(subject string, message []byte) {
		cfg.BroadcastBus.Publish(&broadcast.Message{
			Subject: subject,
			Payload: message,
		})
	}

	// Create the server.Server instance
	shardServer, err := server.NewServer(serverConfig, broadcastToBusFunc) // Pass the bus-publishing broadcast func

	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create shared server for shard %d: %w", cfg.ID, err)
	}

	// Subscribe to the central broadcast bus
	broadcastChan := cfg.BroadcastBus.Subscribe()

	shard := &Shard{
		ID:             cfg.ID,
		server:         shardServer,
		advertiseAddr:  cfg.AdvertiseAddr, // Address for LoadBalancer to connect to
		broadcastChan:  broadcastChan,
		logger:         cfg.Logger.With().Int("shard_id", cfg.ID).Logger(),
		maxConnections: cfg.MaxConnections,
		ctx:            ctx,
		cancel:         cancel,
	}

	// Log bind vs advertise addresses (one-time, no performance impact)
	shard.logger.Info().
		Str("bind_addr", cfg.Addr).
		Str("advertise_addr", cfg.AdvertiseAddr).
		Int("max_connections", cfg.MaxConnections).
		Msg("Shard created with separate bind/advertise addresses")

	return shard, nil
}

// Start begins the shard's operation
func (s *Shard) Start() error {
	s.logger.Info().Msg("Starting shard")

	// Start the underlying shared server
	if err := s.server.Start(); err != nil {
		return fmt.Errorf("failed to start shared server for shard %d: %w", s.ID, err)
	}

	// Start goroutine to listen to the central broadcast bus
	s.wg.Add(1)
	go s.runBroadcastListener()

	s.logger.Info().Msg("Shard started")
	return nil
}

// Shutdown gracefully stops the shard
func (s *Shard) Shutdown() error {
	s.logger.Info().Msg("Shutting down shard")

	// Stop the underlying shared server
	if err := s.server.Shutdown(); err != nil {
		s.logger.Error().Err(err).Msg("Error during shared server shutdown")
	}

	// Cancel context to stop broadcast listener
	s.cancel()
	s.wg.Wait() // Wait for broadcast listener to finish

	s.logger.Info().Msg("Shard shut down")
	return nil
}

// runBroadcastListener listens to the central BroadcastBus and broadcasts messages locally
func (s *Shard) runBroadcastListener() {
	// CRITICAL: Panic recovery must be FIRST defer (executes LAST in LIFO order)
	defer logging.RecoverPanic(s.logger, "runBroadcastListener", map[string]any{
		"shard_id": s.ID,
	})

	defer s.wg.Done()
	s.logger.Info().Msg("Broadcast listener started")

	for {
		select {
		case msg := <-s.broadcastChan:
			if msg == nil {
				s.logger.Info().Msg("Broadcast channel closed, listener stopping")
				return
			}
			// Call the local broadcast function of the shared server
			s.server.Broadcast(msg.Subject, msg.Payload)
		case <-s.ctx.Done():
			s.logger.Info().Msg("Broadcast listener stopped")
			return
		}
	}
}

// GetCurrentConnections returns the current number of active connections for this shard
func (s *Shard) GetCurrentConnections() int64 {
	return s.server.GetStats().CurrentConnections.Load()
}

// GetMaxConnections returns the maximum number of connections for this shard
func (s *Shard) GetMaxConnections() int {
	return s.maxConnections
}

// GetAddr returns the address this shard is listening on
func (s *Shard) GetAddr() string {
	return s.advertiseAddr // Return advertise address for LoadBalancer, not bind address
}

// GetSystemStats returns system-wide CPU and memory metrics.
// Since all shards run in the same process, these metrics are shared.
// Queries directly from SystemMonitor singleton (single source of truth).
func (s *Shard) GetSystemStats() (cpuPercent float64, memoryMB float64) {
	// Get SystemMonitor singleton (all shards share the same instance)
	systemMonitor := metrics.GetSystemMonitor(s.logger)
	metrics := systemMonitor.GetMetrics()
	return metrics.CPUPercent, metrics.MemoryMB
}
