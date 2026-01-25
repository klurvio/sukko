package broadcast

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// valkeyBus implements Bus using Valkey/Redis Pub/Sub.
// It supports both direct connections and Sentinel failover.
type valkeyBus struct {
	client *redis.Client
	pubsub *redis.PubSub

	channel    string
	bufferSize int

	subscribers []chan *Message
	subMu       sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Health tracking (atomic for lock-free reads)
	healthy       atomic.Bool
	lastPublish   atomic.Int64
	publishErrors atomic.Uint64
	messagesRecv  atomic.Uint64

	shutdownTimeout time.Duration
	logger          zerolog.Logger
}

// Compile-time interface check
var _ Bus = (*valkeyBus)(nil)

// newValkeyBus creates a new Valkey-based broadcast bus.
func newValkeyBus(cfg Config, logger zerolog.Logger) (*valkeyBus, error) {
	vcfg := cfg.Valkey

	// Validate configuration
	if len(vcfg.Addrs) == 0 {
		return nil, errors.New("valkey: at least one address is required (VALKEY_ADDRS)")
	}
	if vcfg.MasterName == "" {
		vcfg.MasterName = "mymaster"
	}
	if vcfg.Channel == "" {
		vcfg.Channel = "ws.broadcast"
	}

	busLogger := logger.With().Str("component", "broadcast_bus").Str("backend", "valkey").Logger()

	// Create Valkey client
	var client *redis.Client
	if len(vcfg.Addrs) == 1 {
		// Direct connection mode (single Valkey instance)
		busLogger.Info().
			Str("mode", "direct").
			Str("addr", vcfg.Addrs[0]).
			Msg("Connecting to Valkey (direct mode)")

		client = redis.NewClient(&redis.Options{
			Addr:     vcfg.Addrs[0],
			Password: vcfg.Password,
			DB:       vcfg.DB,

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
		// Sentinel failover mode
		busLogger.Info().
			Str("mode", "sentinel").
			Strs("sentinel_addrs", vcfg.Addrs).
			Str("master_name", vcfg.MasterName).
			Msg("Connecting to Valkey Sentinel")

		client = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:    vcfg.MasterName,
			SentinelAddrs: vcfg.Addrs,
			Password:      vcfg.Password,
			DB:            vcfg.DB,

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
		return nil, fmt.Errorf("valkey: failed to connect: %w", err)
	}

	busLogger.Info().
		Str("channel", vcfg.Channel).
		Int("buffer_size", cfg.BufferSize).
		Msg("Successfully connected to Valkey")

	// Create context for lifecycle management
	busCtx, busCancel := context.WithCancel(context.Background())

	bus := &valkeyBus{
		client:          client,
		channel:         vcfg.Channel,
		bufferSize:      cfg.BufferSize,
		subscribers:     make([]chan *Message, 0, 3),
		ctx:             busCtx,
		cancel:          busCancel,
		shutdownTimeout: cfg.ShutdownTimeout,
		logger:          busLogger,
	}

	bus.healthy.Store(true)
	bus.lastPublish.Store(time.Now().Unix())

	return bus, nil
}

// Publish sends a message to Valkey Pub/Sub (broadcasts to all instances).
func (b *valkeyBus) Publish(msg *Message) {
	payload, err := json.Marshal(msg)
	if err != nil {
		b.logger.Error().
			Err(err).
			Str("subject", msg.Subject).
			Msg("Failed to serialize broadcast message")
		b.publishErrors.Add(1)
		return
	}

	// Publish with timeout (non-blocking)
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

	b.lastPublish.Store(time.Now().Unix())
	b.healthy.Store(true)
}

// Subscribe returns a channel for receiving broadcast messages.
func (b *valkeyBus) Subscribe() <-chan *Message {
	subCh := make(chan *Message, b.bufferSize)

	b.subMu.Lock()
	b.subscribers = append(b.subscribers, subCh)
	totalSubscribers := len(b.subscribers)
	b.subMu.Unlock()

	b.logger.Info().
		Int("total_subscribers", totalSubscribers).
		Msg("New subscriber registered")

	return subCh
}

// Run starts the receive and health check loops.
func (b *valkeyBus) Run() {
	b.pubsub = b.client.Subscribe(b.ctx, b.channel)

	b.logger.Info().
		Str("channel", b.channel).
		Int("subscribers", len(b.subscribers)).
		Msg("BroadcastBus started (Valkey Pub/Sub)")

	b.wg.Add(2)
	go b.receiveLoop()
	go b.healthCheckLoop()
}

// Shutdown gracefully stops the bus with default timeout.
func (b *valkeyBus) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), b.shutdownTimeout)
	defer cancel()
	b.ShutdownWithContext(ctx)
}

// ShutdownWithContext gracefully stops the bus with custom context.
func (b *valkeyBus) ShutdownWithContext(ctx context.Context) {
	b.logger.Info().Msg("Shutting down BroadcastBus")

	// Signal goroutines to stop
	b.cancel()

	// Wait for goroutines to finish
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
	case <-ctx.Done():
		b.logger.Warn().Msg("BroadcastBus shutdown timeout, forcing exit")
	}

	// Close Valkey connections
	if b.pubsub != nil {
		if err := b.pubsub.Close(); err != nil {
			b.logger.Error().Err(err).Msg("Failed to close Valkey Pub/Sub")
		}
	}

	if err := b.client.Close(); err != nil {
		b.logger.Error().Err(err).Msg("Failed to close Valkey client")
	}

	// Only close subscriber channels if goroutines have stopped
	if goroutinesStopped {
		b.subMu.Lock()
		for _, subCh := range b.subscribers {
			close(subCh)
		}
		b.subscribers = nil
		b.subMu.Unlock()
	} else {
		b.logger.Warn().Msg("Skipping channel close (goroutines may still be running)")
		b.subMu.Lock()
		b.subscribers = nil
		b.subMu.Unlock()
	}

	b.logger.Info().Msg("BroadcastBus shutdown complete")
}

// IsHealthy returns true if the Valkey connection is operational.
func (b *valkeyBus) IsHealthy() bool {
	if !b.healthy.Load() {
		return false
	}

	lastPub := b.lastPublish.Load()
	if lastPub > 0 && time.Since(time.Unix(lastPub, 0)) > 60*time.Second {
		if b.logger.GetLevel() <= zerolog.DebugLevel {
			b.logger.Debug().
				Dur("since_last_publish", time.Since(time.Unix(lastPub, 0))).
				Msg("No recent Valkey publish (might be normal)")
		}
	}

	return true
}

// GetMetrics returns current bus metrics.
func (b *valkeyBus) GetMetrics() Metrics {
	lastPubTime := time.Unix(b.lastPublish.Load(), 0)
	var lastPubAgo float64
	if !lastPubTime.IsZero() && lastPubTime.Unix() > 0 {
		lastPubAgo = time.Since(lastPubTime).Seconds()
	} else {
		lastPubAgo = -1
	}

	b.subMu.RLock()
	subscriberCount := len(b.subscribers)
	b.subMu.RUnlock()

	return Metrics{
		Type:             "valkey",
		Healthy:          b.IsHealthy(),
		Channel:          b.channel,
		Subscribers:      subscriberCount,
		PublishErrors:    b.publishErrors.Load(),
		MessagesReceived: b.messagesRecv.Load(),
		LastPublishAgo:   lastPubAgo,
		LastPublishTime:  lastPubTime,
	}
}

// receiveLoop receives messages from Valkey and fans out to local subscribers.
func (b *valkeyBus) receiveLoop() {
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
				b.logger.Warn().Msg("Valkey Pub/Sub channel closed, reconnecting...")
				b.healthy.Store(false)

				if b.reconnect() {
					ch = b.pubsub.Channel()
					b.logger.Info().Msg("Reconnected to Valkey, resuming receive loop")
				} else {
					b.logger.Error().Msg("Failed to reconnect to Valkey, exiting receive loop")
					return
				}
				continue
			}

			var msg Message
			if err := json.Unmarshal([]byte(valkeyMsg.Payload), &msg); err != nil {
				b.logger.Error().
					Err(err).
					Str("payload", valkeyMsg.Payload).
					Msg("Failed to deserialize Valkey message")
				continue
			}

			b.messagesRecv.Add(1)
			b.fanOut(&msg)
		}
	}
}

// fanOut sends a message to all local subscribers.
func (b *valkeyBus) fanOut(msg *Message) {
	select {
	case <-b.ctx.Done():
		return
	default:
	}

	b.subMu.RLock()
	if len(b.subscribers) == 0 {
		b.subMu.RUnlock()
		return
	}
	subscribers := make([]chan *Message, len(b.subscribers))
	copy(subscribers, b.subscribers)
	b.subMu.RUnlock()

	sent := 0
	dropped := 0

	for _, subCh := range subscribers {
		select {
		case <-b.ctx.Done():
			return
		default:
		}

		func() {
			defer func() {
				if r := recover(); r != nil {
					dropped++
				}
			}()

			select {
			case subCh <- msg:
				sent++
			default:
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

// reconnect attempts to resubscribe to Valkey after connection loss.
func (b *valkeyBus) reconnect() bool {
	backoff := 100 * time.Millisecond
	maxBackoff := 30 * time.Second
	maxAttempts := 10

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		select {
		case <-b.ctx.Done():
			return false
		case <-time.After(backoff):
			b.logger.Info().
				Int("attempt", attempt).
				Dur("backoff", backoff).
				Msg("Attempting to reconnect to Valkey Pub/Sub")

			if b.pubsub != nil {
				_ = b.pubsub.Close()
			}

			b.pubsub = b.client.Subscribe(b.ctx, b.channel)

			if err := b.pubsub.Ping(b.ctx); err != nil {
				b.logger.Error().
					Err(err).
					Int("attempt", attempt).
					Msg("Valkey Pub/Sub reconnection failed")

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

// healthCheckLoop periodically pings Valkey to verify connectivity.
func (b *valkeyBus) healthCheckLoop() {
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
				if b.logger.GetLevel() <= zerolog.DebugLevel {
					b.logger.Debug().Msg("Valkey health check passed")
				}
				b.healthy.Store(true)
			}
		}
	}
}
