// Package orchestration provides multi-instance coordination for horizontally
// scaled WebSocket servers. It uses Valkey (Redis-compatible) Pub/Sub for
// broadcasting messages across server instances and shards.
//
// Key components:
//   - BroadcastBus: Valkey-based inter-instance message distribution
//   - LoadBalancer: Routes connections across local shards
//   - Shard: Manages a subset of WebSocket connections
//   - KafkaConsumerPool: Shared Kafka consumer to avoid message duplication
package orchestration

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

// BroadcastBus is a Valkey Pub/Sub-based system for inter-shard and inter-instance communication.
// It replaces the in-memory channel approach to enable horizontal scaling across multiple VM instances.
// Uses go-redis client which is compatible with both Valkey and Redis.
//
// Architecture: Each VM instance has one BroadcastBus. When a message is published,
// Valkey broadcasts it to all connected BroadcastBus instances. Each BroadcastBus
// then fans out to its local shards (subscribers).
//
// Thread Safety: All public methods are safe for concurrent use.
//
// Lifecycle: Create with NewBroadcastBus, call Subscribe for each shard, call Run
// to start receiving, and Shutdown to stop.
type BroadcastBus struct {
	client *redis.Client // Valkey client (supports Sentinel failover)
	pubsub *redis.PubSub // Valkey Pub/Sub subscription

	channel string // Pub/Sub channel name (e.g., "ws.broadcast")

	subscribers []chan *BroadcastMessage // Local shard subscription channels
	subMu       sync.RWMutex             // Protects subscribers slice

	ctx    context.Context    // Root context for bus goroutines
	cancel context.CancelFunc // Cancels ctx to signal shutdown
	wg     sync.WaitGroup     // Tracks receiveLoop and healthCheckLoop

	// Health tracking - all fields are atomic for lock-free reads
	healthy       atomic.Bool   // True if Valkey connection is operational
	lastPublish   atomic.Int64  // Unix timestamp of last successful publish
	publishErrors atomic.Uint64 // Count of failed publish attempts
	messagesRecv  atomic.Uint64 // Count of messages received from Valkey

	logger zerolog.Logger // Structured logger with "broadcast_bus" component tag
}

// BroadcastBusConfig holds configuration for creating a BroadcastBus
type BroadcastBusConfig struct {
	// Valkey connection configuration (supports both Sentinel and single instance)
	SentinelAddrs []string // e.g., ["valkey-1:26379", "valkey-2:26379", "valkey-3:26379"] or ["10.0.0.3:6379"]
	MasterName    string   // e.g., "mymaster"
	Password      string
	DB            int

	// Pub/Sub configuration
	Channel    string // e.g., "ws.broadcast"
	BufferSize int    // Subscriber channel buffer (default: 1024)

	Logger zerolog.Logger
}

// NewBroadcastBus creates a new Valkey-based BroadcastBus with the provided configuration.
// It supports two connection modes:
//   - Direct mode (single address): Connects directly to a Valkey instance
//   - Sentinel mode (multiple addresses): Uses Sentinel for automatic failover
//
// The connection is tested before returning. Returns an error if the connection fails.
func NewBroadcastBus(cfg BroadcastBusConfig) (*BroadcastBus, error) {
	// Validate configuration
	if len(cfg.SentinelAddrs) == 0 {
		return nil, fmt.Errorf("VALKEY_ADDRS is required (at least one address)")
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

	// Create Valkey client (using go-redis which is compatible with Valkey)
	// If single address: Direct connection (single Valkey instance)
	// If multiple addresses: Sentinel failover (self-hosted HA)
	var client *redis.Client
	if len(cfg.SentinelAddrs) == 1 {
		// Direct connection mode (single Valkey instance)
		busLogger.Info().
			Str("mode", "direct").
			Str("addr", cfg.SentinelAddrs[0]).
			Msg("Connecting to Valkey (direct mode)")

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
		// Sentinel failover mode (self-hosted Valkey Sentinel cluster)
		busLogger.Info().
			Str("mode", "sentinel").
			Strs("sentinel_addrs", cfg.SentinelAddrs).
			Str("master_name", cfg.MasterName).
			Msg("Connecting to Valkey Sentinel")

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
		return nil, fmt.Errorf("failed to connect to Valkey: %w", err)
	}

	busLogger.Info().
		Str("channel", cfg.Channel).
		Int("buffer_size", cfg.BufferSize).
		Msg("Successfully connected to Valkey")

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

// Run starts the BroadcastBus's receive and health check loops.
// It subscribes to the Valkey Pub/Sub channel and starts goroutines for:
//   - receiveLoop: Receives messages from Valkey and fans out to local subscribers
//   - healthCheckLoop: Periodic Valkey PING to verify connectivity
//
// Must be called after all shards have called Subscribe(). Returns immediately
// after launching goroutines. Use Shutdown to stop.
func (b *BroadcastBus) Run() {
	// Subscribe to Valkey channel
	b.pubsub = b.client.Subscribe(b.ctx, b.channel)

	b.logger.Info().
		Str("channel", b.channel).
		Int("subscribers", len(b.subscribers)).
		Msg("BroadcastBus started (Valkey Pub/Sub)")

	// Start receive loop (fans out Valkey messages to local subscribers)
	b.wg.Add(1)
	go b.receiveLoop()

	// Start health check loop (periodic Valkey PING)
	b.wg.Add(1)
	go b.healthCheckLoop()
}

// Shutdown gracefully stops the BroadcastBus with a 5-second timeout.
// The shutdown sequence:
//  1. Cancels the root context to signal goroutines to stop
//  2. Waits up to 5 seconds for goroutines to finish
//  3. Closes Valkey Pub/Sub subscription and client connections
//  4. Closes subscriber channels (only if goroutines stopped cleanly)
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

	goroutinesStopped := false
	select {
	case <-done:
		b.logger.Info().Msg("All BroadcastBus goroutines stopped")
		goroutinesStopped = true
	case <-time.After(5 * time.Second):
		b.logger.Warn().Msg("BroadcastBus shutdown timeout (5s), forcing exit")
	}

	// Close Valkey connections
	// This will cause any goroutines still running to error out
	if b.pubsub != nil {
		if err := b.pubsub.Close(); err != nil {
			b.logger.Error().Err(err).Msg("Failed to close Valkey Pub/Sub")
		}
	}

	if err := b.client.Close(); err != nil {
		b.logger.Error().Err(err).Msg("Failed to close Valkey client")
	}

	// Only close subscriber channels if goroutines have stopped
	// If timeout occurred, goroutines might still be sending to these channels
	// Let channels be GC'd when goroutines eventually exit
	if goroutinesStopped {
		b.subMu.Lock()
		for _, subCh := range b.subscribers {
			close(subCh)
		}
		b.subscribers = nil
		b.subMu.Unlock()
	} else {
		b.logger.Warn().Msg("Skipping channel close (goroutines may still be running)")
		// Set subscribers to nil to prevent new messages, but don't close channels
		b.subMu.Lock()
		b.subscribers = nil
		b.subMu.Unlock()
	}

	b.logger.Info().Msg("BroadcastBus shutdown complete")
}

// Publish sends a message to Valkey Pub/Sub (broadcasts to all instances)
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

	// Publish to Valkey with timeout (non-blocking)
	ctx, cancel := context.WithTimeout(b.ctx, 100*time.Millisecond)
	defer cancel()

	if err := b.client.Publish(ctx, b.channel, payload).Err(); err != nil {
		b.logger.Error().
			Err(err).
			Str("channel", b.channel).
			Str("subject", msg.Subject).
			Msg("Failed to publish message to Valkey")

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
	totalSubscribers := len(b.subscribers) // Capture count while holding lock
	b.subMu.Unlock()

	b.logger.Info().
		Int("total_subscribers", totalSubscribers).
		Msg("New subscriber registered")

	return subCh
}

// receiveLoop receives messages from Valkey and fans out to local subscribers
func (b *BroadcastBus) receiveLoop() {
	defer b.wg.Done()

	ch := b.pubsub.Channel()

	b.logger.Info().Msg("Valkey receive loop started")

	for {
		select {
		case <-b.ctx.Done():
			b.logger.Info().Msg("Valkey receive loop stopping (context cancelled)")
			return

		case valkeyMsg, ok := <-ch:
			if !ok {
				// Channel closed, attempt reconnection
				b.logger.Warn().Msg("Valkey Pub/Sub channel closed, reconnecting...")
				b.healthy.Store(false)

				if b.reconnect() {
					// Reconnected, get new channel
					ch = b.pubsub.Channel()
					b.logger.Info().Msg("Reconnected to Valkey, resuming receive loop")
				} else {
					// Failed to reconnect, exit loop
					b.logger.Error().Msg("Failed to reconnect to Valkey, exiting receive loop")
					return
				}
				continue
			}

			// Deserialize message
			var msg BroadcastMessage
			if err := json.Unmarshal([]byte(valkeyMsg.Payload), &msg); err != nil {
				b.logger.Error().
					Err(err).
					Str("payload", valkeyMsg.Payload).
					Msg("Failed to deserialize Valkey message")
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
// Uses a snapshot of subscribers to avoid holding the lock during channel sends.
func (b *BroadcastBus) fanOut(msg *BroadcastMessage) {
	// Check if context is cancelled before doing any work
	select {
	case <-b.ctx.Done():
		return // Bus is shutting down
	default:
	}

	// Take a snapshot of subscribers while holding the lock
	b.subMu.RLock()
	if len(b.subscribers) == 0 {
		b.subMu.RUnlock()
		return
	}
	subscribers := make([]chan *BroadcastMessage, len(b.subscribers))
	copy(subscribers, b.subscribers)
	b.subMu.RUnlock()

	// Iterate over snapshot without holding the lock
	// This prevents deadlock if Shutdown() is waiting for the lock
	sent := 0
	dropped := 0

	for _, subCh := range subscribers {
		// Check context before each send
		select {
		case <-b.ctx.Done():
			return // Bus is shutting down
		default:
		}

		// Non-blocking send with panic recovery for closed channels
		func() {
			defer func() {
				if r := recover(); r != nil {
					// Channel was closed (shutdown in progress), this is expected
					dropped++
				}
			}()

			select {
			case subCh <- msg:
				sent++
			default:
				// Subscriber channel full, drop message
				dropped++
			}
		}()
	}

	if dropped > 0 {
		b.logger.Warn().
			Int("sent", sent).
			Int("dropped", dropped).
			Str("subject", msg.Subject).
			Msg("Dropped messages due to slow subscribers")
	}
}

// reconnect attempts to resubscribe to Valkey after connection loss
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
				Msg("Attempting to reconnect to Valkey Pub/Sub")

			// Close old subscription
			if b.pubsub != nil {
				_ = b.pubsub.Close()
			}

			// Create new subscription
			b.pubsub = b.client.Subscribe(b.ctx, b.channel)

			// Test subscription with ping
			if err := b.pubsub.Ping(b.ctx); err != nil {
				b.logger.Error().
					Err(err).
					Int("attempt", attempt).
					Msg("Valkey Pub/Sub reconnection failed")

				// Exponential backoff
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				continue
			}

			b.logger.Info().
				Int("attempt", attempt).
				Msg("Successfully reconnected to Valkey Pub/Sub")
			b.healthy.Store(true)
			return true
		}
	}

	b.logger.Error().
		Int("max_attempts", maxAttempts).
		Msg("Failed to reconnect to Valkey after max attempts")
	return false
}

// healthCheckLoop periodically pings Valkey to verify connectivity
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
					Msg("Valkey health check failed")
				b.healthy.Store(false)
			} else {
				// Only log health check success at debug level to reduce noise
				if b.logger.GetLevel() <= zerolog.DebugLevel {
					b.logger.Debug().Msg("Valkey health check passed")
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
				Msg("No recent Valkey publish (might be normal)")
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
		"type":              "valkey",
		"healthy":           b.IsHealthy(),
		"channel":           b.channel,
		"subscribers":       len(b.subscribers),
		"publish_errors":    b.publishErrors.Load(),
		"messages_received": b.messagesRecv.Load(),
		"last_publish_ago":  lastPubAgo,
	}
}
