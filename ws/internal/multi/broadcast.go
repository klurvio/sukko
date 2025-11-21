package multi

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// BroadcastBus is a Redis Pub/Sub-based system for inter-shard and inter-instance communication.
// It replaces the in-memory channel approach to enable horizontal scaling across multiple VM instances.
type BroadcastBus struct {
	// Redis client (Sentinel failover support)
	client *redis.Client
	pubsub *redis.PubSub

	// Pub/Sub configuration
	channel string // e.g., "ws.broadcast"

	// Subscription management
	subscribers []chan *BroadcastMessage
	subMu       sync.RWMutex

	// Lifecycle management
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Health tracking
	healthy       atomic.Bool
	lastPublish   atomic.Int64 // Unix timestamp of last successful publish
	publishErrors atomic.Uint64
	messagesRecv  atomic.Uint64

	logger zerolog.Logger
}

// BroadcastBusConfig holds configuration for creating a BroadcastBus
type BroadcastBusConfig struct {
	// Sentinel configuration (supports both self-hosted Sentinel and GCP Memorystore)
	SentinelAddrs []string // e.g., ["redis-1:26379", "redis-2:26379", "redis-3:26379"] or ["10.0.0.3:6379"]
	MasterName    string   // e.g., "mymaster"
	Password      string
	DB            int

	// Pub/Sub configuration
	Channel    string // e.g., "ws.broadcast"
	BufferSize int    // Subscriber channel buffer (default: 1024)

	Logger zerolog.Logger
}

// NewBroadcastBus creates a new Redis-based BroadcastBus
func NewBroadcastBus(cfg BroadcastBusConfig) (*BroadcastBus, error) {
	// Validate configuration
	if len(cfg.SentinelAddrs) == 0 {
		return nil, fmt.Errorf("REDIS_SENTINEL_ADDRS is required (at least one address)")
	}
	if cfg.MasterName == "" {
		cfg.MasterName = "mymaster" // Default
	}
	if cfg.Channel == "" {
		cfg.Channel = "ws.broadcast" // Default channel
	}
	if cfg.BufferSize == 0 {
		cfg.BufferSize = 1024 // Default buffer
	}

	busLogger := cfg.Logger.With().Str("component", "broadcast_bus").Logger()

	// Create Redis client
	// If single address: Direct connection (GCP Memorystore or single Redis)
	// If multiple addresses: Sentinel failover (self-hosted HA)
	var client *redis.Client
	if len(cfg.SentinelAddrs) == 1 {
		// Direct connection mode (GCP Memorystore or single Redis instance)
		busLogger.Info().
			Str("mode", "direct").
			Str("addr", cfg.SentinelAddrs[0]).
			Msg("Connecting to Redis (direct mode)")

		client = redis.NewClient(&redis.Options{
			Addr:     cfg.SentinelAddrs[0],
			Password: cfg.Password,
			DB:       cfg.DB,

			// Connection pooling
			PoolSize:     50,
			MinIdleConns: 10,

			// Timeouts
			DialTimeout:  5 * time.Second,
			ReadTimeout:  3 * time.Second,
			WriteTimeout: 3 * time.Second,

			// Retry policy
			MaxRetries:      3,
			MinRetryBackoff: 100 * time.Millisecond,
			MaxRetryBackoff: 1 * time.Second,
		})
	} else {
		// Sentinel failover mode (self-hosted Redis Sentinel cluster)
		busLogger.Info().
			Str("mode", "sentinel").
			Strs("sentinel_addrs", cfg.SentinelAddrs).
			Str("master_name", cfg.MasterName).
			Msg("Connecting to Redis Sentinel")

		client = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:    cfg.MasterName,
			SentinelAddrs: cfg.SentinelAddrs,
			Password:      cfg.Password,
			DB:            cfg.DB,

			// Connection pooling
			PoolSize:     50,
			MinIdleConns: 10,

			// Timeouts
			DialTimeout:  5 * time.Second,
			ReadTimeout:  3 * time.Second,
			WriteTimeout: 3 * time.Second,

			// Retry policy
			MaxRetries:      3,
			MinRetryBackoff: 100 * time.Millisecond,
			MaxRetryBackoff: 1 * time.Second,
		})
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	busLogger.Info().
		Str("channel", cfg.Channel).
		Int("buffer_size", cfg.BufferSize).
		Msg("Successfully connected to Redis")

	// Create context for lifecycle management
	busCtx, busCancel := context.WithCancel(context.Background())

	bus := &BroadcastBus{
		client:      client,
		channel:     cfg.Channel,
		subscribers: make([]chan *BroadcastMessage, 0, 3), // Pre-allocate for 3 shards
		ctx:         busCtx,
		cancel:      busCancel,
		logger:      busLogger,
	}

	bus.healthy.Store(true)
	bus.lastPublish.Store(time.Now().Unix())

	return bus, nil
}

// Run starts the BroadcastBus's main loop
// Must be called after all shards have called Subscribe()
func (b *BroadcastBus) Run() {
	// Subscribe to Redis channel
	b.pubsub = b.client.Subscribe(b.ctx, b.channel)

	b.logger.Info().
		Str("channel", b.channel).
		Int("subscribers", len(b.subscribers)).
		Msg("BroadcastBus started (Redis Pub/Sub)")

	// Start receive loop (fans out Redis messages to local subscribers)
	b.wg.Add(1)
	go b.receiveLoop()

	// Start health check loop (periodic Redis PING)
	b.wg.Add(1)
	go b.healthCheckLoop()
}

// Shutdown gracefully stops the BroadcastBus
func (b *BroadcastBus) Shutdown() {
	b.logger.Info().Msg("Shutting down BroadcastBus")

	// Stop all goroutines
	b.cancel()

	// Wait for goroutines to finish (with timeout)
	done := make(chan struct{})
	go func() {
		b.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		b.logger.Info().Msg("All BroadcastBus goroutines stopped")
	case <-time.After(5 * time.Second):
		b.logger.Warn().Msg("BroadcastBus shutdown timeout (5s), forcing exit")
	}

	// Close Redis connections
	if b.pubsub != nil {
		if err := b.pubsub.Close(); err != nil {
			b.logger.Error().Err(err).Msg("Failed to close Redis Pub/Sub")
		}
	}

	if err := b.client.Close(); err != nil {
		b.logger.Error().Err(err).Msg("Failed to close Redis client")
	}

	// Close all subscriber channels
	b.subMu.Lock()
	for _, subCh := range b.subscribers {
		close(subCh)
	}
	b.subscribers = nil
	b.subMu.Unlock()

	b.logger.Info().Msg("BroadcastBus shutdown complete")
}

// Publish sends a message to Redis Pub/Sub (broadcasts to all instances)
func (b *BroadcastBus) Publish(msg *BroadcastMessage) {
	// Serialize message to JSON
	payload, err := json.Marshal(msg)
	if err != nil {
		b.logger.Error().
			Err(err).
			Str("subject", msg.Subject).
			Msg("Failed to serialize broadcast message")
		b.publishErrors.Add(1)
		return
	}

	// Publish to Redis with timeout (non-blocking)
	ctx, cancel := context.WithTimeout(b.ctx, 100*time.Millisecond)
	defer cancel()

	if err := b.client.Publish(ctx, b.channel, payload).Err(); err != nil {
		b.logger.Error().
			Err(err).
			Str("channel", b.channel).
			Str("subject", msg.Subject).
			Msg("Failed to publish message to Redis")

		b.publishErrors.Add(1)
		b.healthy.Store(false)
		return
	}

	// Update health tracking
	b.lastPublish.Store(time.Now().Unix())
	b.healthy.Store(true)
}

// Subscribe returns a channel for receiving broadcast messages
// Each shard calls this once to get its subscription channel
func (b *BroadcastBus) Subscribe() chan *BroadcastMessage {
	subCh := make(chan *BroadcastMessage, 1024) // Buffered for bursts

	b.subMu.Lock()
	b.subscribers = append(b.subscribers, subCh)
	b.subMu.Unlock()

	b.logger.Info().
		Int("total_subscribers", len(b.subscribers)).
		Msg("New subscriber registered")

	return subCh
}

// receiveLoop receives messages from Redis and fans out to local subscribers
func (b *BroadcastBus) receiveLoop() {
	defer b.wg.Done()

	ch := b.pubsub.Channel()

	b.logger.Info().Msg("Redis receive loop started")

	for {
		select {
		case <-b.ctx.Done():
			b.logger.Info().Msg("Redis receive loop stopping (context cancelled)")
			return

		case redisMsg, ok := <-ch:
			if !ok {
				// Channel closed, attempt reconnection
				b.logger.Warn().Msg("Redis Pub/Sub channel closed, reconnecting...")
				b.healthy.Store(false)

				if b.reconnect() {
					// Reconnected, get new channel
					ch = b.pubsub.Channel()
					b.logger.Info().Msg("Reconnected to Redis, resuming receive loop")
				} else {
					// Failed to reconnect, exit loop
					b.logger.Error().Msg("Failed to reconnect to Redis, exiting receive loop")
					return
				}
				continue
			}

			// Deserialize message
			var msg BroadcastMessage
			if err := json.Unmarshal([]byte(redisMsg.Payload), &msg); err != nil {
				b.logger.Error().
					Err(err).
					Str("payload", redisMsg.Payload).
					Msg("Failed to deserialize Redis message")
				continue
			}

			// Track received messages
			b.messagesRecv.Add(1)

			// Fan out to all local subscribers (non-blocking)
			b.fanOut(&msg)
		}
	}
}

// fanOut sends a message to all local subscribers (shards on this instance)
func (b *BroadcastBus) fanOut(msg *BroadcastMessage) {
	b.subMu.RLock()
	defer b.subMu.RUnlock()

	sent := 0
	dropped := 0

	for _, subCh := range b.subscribers {
		select {
		case subCh <- msg:
			sent++
		case <-b.ctx.Done():
			return // Bus is shutting down
		default:
			// Subscriber channel full, drop message
			dropped++
		}
	}

	if dropped > 0 {
		b.logger.Warn().
			Int("sent", sent).
			Int("dropped", dropped).
			Str("subject", msg.Subject).
			Msg("Dropped messages due to slow subscribers")
	}
}

// reconnect attempts to resubscribe to Redis after connection loss
func (b *BroadcastBus) reconnect() bool {
	backoff := 100 * time.Millisecond
	maxBackoff := 30 * time.Second
	maxAttempts := 10

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		select {
		case <-b.ctx.Done():
			return false // Shutdown requested
		case <-time.After(backoff):
			b.logger.Info().
				Int("attempt", attempt).
				Dur("backoff", backoff).
				Msg("Attempting to reconnect to Redis Pub/Sub")

			// Close old subscription
			if b.pubsub != nil {
				b.pubsub.Close()
			}

			// Create new subscription
			b.pubsub = b.client.Subscribe(b.ctx, b.channel)

			// Test subscription with ping
			if err := b.pubsub.Ping(b.ctx); err != nil {
				b.logger.Error().
					Err(err).
					Int("attempt", attempt).
					Msg("Redis Pub/Sub reconnection failed")

				// Exponential backoff
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				continue
			}

			b.logger.Info().
				Int("attempt", attempt).
				Msg("Successfully reconnected to Redis Pub/Sub")
			b.healthy.Store(true)
			return true
		}
	}

	b.logger.Error().
		Int("max_attempts", maxAttempts).
		Msg("Failed to reconnect to Redis after max attempts")
	return false
}

// healthCheckLoop periodically pings Redis to verify connectivity
func (b *BroadcastBus) healthCheckLoop() {
	defer b.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-b.ctx.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(b.ctx, 5*time.Second)
			err := b.client.Ping(ctx).Err()
			cancel()

			if err != nil {
				b.logger.Error().
					Err(err).
					Msg("Redis health check failed")
				b.healthy.Store(false)
			} else {
				// Only log health check success at debug level to reduce noise
				if b.logger.GetLevel() <= zerolog.DebugLevel {
					b.logger.Debug().Msg("Redis health check passed")
				}
				b.healthy.Store(true)
			}
		}
	}
}

// IsHealthy returns true if the broadcast bus is operational
func (b *BroadcastBus) IsHealthy() bool {
	// Check atomic health flag
	if !b.healthy.Load() {
		return false
	}

	// Check if last publish was recent (within 60 seconds)
	// Note: This assumes messages are being published regularly
	// If no messages for 60s, health check still passes (it's not an error)
	lastPub := b.lastPublish.Load()
	if lastPub > 0 && time.Since(time.Unix(lastPub, 0)) > 60*time.Second {
		// This is just a warning, not a health failure
		// (might be normal if no market activity)
		if b.logger.GetLevel() <= zerolog.DebugLevel {
			b.logger.Debug().
				Dur("since_last_publish", time.Since(time.Unix(lastPub, 0))).
				Msg("No recent Redis publish (might be normal)")
		}
	}

	return true
}

// GetMetrics returns current bus metrics (for monitoring/debugging)
func (b *BroadcastBus) GetMetrics() map[string]interface{} {
	lastPubTime := time.Unix(b.lastPublish.Load(), 0)
	var lastPubAgo float64
	if !lastPubTime.IsZero() {
		lastPubAgo = time.Since(lastPubTime).Seconds()
	} else {
		lastPubAgo = -1 // Never published
	}

	return map[string]interface{}{
		"type":              "redis",
		"healthy":           b.IsHealthy(),
		"channel":           b.channel,
		"subscribers":       len(b.subscribers),
		"publish_errors":    b.publishErrors.Load(),
		"messages_received": b.messagesRecv.Load(),
		"last_publish_ago":  lastPubAgo,
	}
}
