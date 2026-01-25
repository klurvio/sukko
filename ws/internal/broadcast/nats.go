package broadcast

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
)

// natsBus implements Bus using NATS Core Pub/Sub.
// Uses fire-and-forget messaging for sub-millisecond latency.
type natsBus struct {
	conn *nats.Conn
	sub  *nats.Subscription

	subject    string
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
var _ Bus = (*natsBus)(nil)

// newNATSBus creates a new NATS-based broadcast bus.
func newNATSBus(cfg Config, logger zerolog.Logger) (*natsBus, error) {
	ncfg := cfg.NATS

	// Validate configuration
	if len(ncfg.URLs) == 0 {
		return nil, errors.New("nats: at least one URL is required (NATS_URLS)")
	}
	if ncfg.Subject == "" {
		ncfg.Subject = "ws.broadcast"
	}
	if ncfg.ReconnectWait == 0 {
		ncfg.ReconnectWait = 2 * time.Second
	}
	if ncfg.MaxReconnects == 0 {
		ncfg.MaxReconnects = -1 // Unlimited
	}

	busLogger := logger.With().Str("component", "broadcast_bus").Str("backend", "nats").Logger()

	// Build NATS connection options
	opts := []nats.Option{
		nats.ReconnectWait(ncfg.ReconnectWait),
		nats.MaxReconnects(ncfg.MaxReconnects),
		nats.ReconnectBufSize(5 * 1024 * 1024), // 5MB buffer for reconnection
		nats.PingInterval(10 * time.Second),
		nats.MaxPingsOutstanding(3),
	}

	// Set client name if provided
	if ncfg.Name != "" {
		opts = append(opts, nats.Name(ncfg.Name))
	} else {
		opts = append(opts, nats.Name("odin-ws-broadcast"))
	}

	// Authentication options
	if ncfg.Token != "" {
		opts = append(opts, nats.Token(ncfg.Token))
	} else if ncfg.User != "" && ncfg.Password != "" {
		opts = append(opts, nats.UserInfo(ncfg.User, ncfg.Password))
	}

	// Connection event handlers
	opts = append(opts,
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			if err != nil {
				busLogger.Warn().Err(err).Msg("NATS disconnected")
			} else {
				busLogger.Info().Msg("NATS disconnected (clean)")
			}
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			busLogger.Info().
				Str("url", nc.ConnectedUrl()).
				Msg("NATS reconnected")
		}),
		nats.ClosedHandler(func(nc *nats.Conn) {
			busLogger.Info().Msg("NATS connection closed")
		}),
		nats.ErrorHandler(func(nc *nats.Conn, sub *nats.Subscription, err error) {
			busLogger.Error().
				Err(err).
				Msg("NATS async error")
		}),
	)

	// Build server URL(s)
	var serverURL string
	if ncfg.ClusterMode && len(ncfg.URLs) > 1 {
		// Cluster mode: comma-separated URLs
		busLogger.Info().
			Str("mode", "cluster").
			Strs("urls", ncfg.URLs).
			Msg("Connecting to NATS cluster")
		serverURL = ""
		var serverURLSb120 strings.Builder
		for i, url := range ncfg.URLs {
			if i > 0 {
				serverURLSb120.WriteString(",")
			}
			serverURLSb120.WriteString(url)
		}
		serverURL += serverURLSb120.String()
	} else {
		// Single server mode
		busLogger.Info().
			Str("mode", "single").
			Str("url", ncfg.URLs[0]).
			Msg("Connecting to NATS server")
		serverURL = ncfg.URLs[0]
	}

	// Connect to NATS
	nc, err := nats.Connect(serverURL, opts...)
	if err != nil {
		return nil, fmt.Errorf("nats: failed to connect: %w", err)
	}

	busLogger.Info().
		Str("subject", ncfg.Subject).
		Str("connected_url", nc.ConnectedUrl()).
		Int("buffer_size", cfg.BufferSize).
		Msg("Successfully connected to NATS")

	// Create context for lifecycle management
	busCtx, busCancel := context.WithCancel(context.Background())

	bus := &natsBus{
		conn:            nc,
		subject:         ncfg.Subject,
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

// Publish sends a message to NATS (broadcasts to all instances).
func (b *natsBus) Publish(msg *Message) {
	payload, err := json.Marshal(msg)
	if err != nil {
		b.logger.Error().
			Err(err).
			Str("subject", msg.Subject).
			Msg("Failed to serialize broadcast message")
		b.publishErrors.Add(1)
		return
	}

	// Fire-and-forget publish (sub-millisecond)
	if err := b.conn.Publish(b.subject, payload); err != nil {
		b.logger.Error().
			Err(err).
			Str("subject", b.subject).
			Str("msg_subject", msg.Subject).
			Msg("Failed to publish message to NATS")

		b.publishErrors.Add(1)
		b.healthy.Store(false)
		return
	}

	b.lastPublish.Store(time.Now().Unix())
	b.healthy.Store(true)
}

// Subscribe returns a channel for receiving broadcast messages.
func (b *natsBus) Subscribe() <-chan *Message {
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
func (b *natsBus) Run() {
	// Subscribe to NATS subject with message handler
	var err error
	b.sub, err = b.conn.Subscribe(b.subject, func(natsMsg *nats.Msg) {
		var msg Message
		if err := json.Unmarshal(natsMsg.Data, &msg); err != nil {
			b.logger.Error().
				Err(err).
				Str("data", string(natsMsg.Data)).
				Msg("Failed to deserialize NATS message")
			return
		}

		b.messagesRecv.Add(1)
		b.fanOut(&msg)
	})
	if err != nil {
		b.logger.Error().Err(err).Msg("Failed to subscribe to NATS subject")
		return
	}

	b.logger.Info().
		Str("subject", b.subject).
		Int("subscribers", len(b.subscribers)).
		Msg("BroadcastBus started (NATS Pub/Sub)")

	// Start health check loop
	b.wg.Add(1)
	go b.healthCheckLoop()
}

// Shutdown gracefully stops the bus with default timeout.
func (b *natsBus) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), b.shutdownTimeout)
	defer cancel()
	b.ShutdownWithContext(ctx)
}

// ShutdownWithContext gracefully stops the bus with custom context.
func (b *natsBus) ShutdownWithContext(ctx context.Context) {
	b.logger.Info().Msg("Shutting down BroadcastBus")

	// Signal goroutines to stop
	b.cancel()

	// Unsubscribe from NATS
	if b.sub != nil {
		if err := b.sub.Drain(); err != nil {
			b.logger.Error().Err(err).Msg("Failed to drain NATS subscription")
		}
	}

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

	// Close NATS connection
	if b.conn != nil {
		// Drain ensures all pending messages are sent
		if err := b.conn.Drain(); err != nil {
			b.logger.Error().Err(err).Msg("Failed to drain NATS connection")
			b.conn.Close()
		}
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

// IsHealthy returns true if the NATS connection is operational.
func (b *natsBus) IsHealthy() bool {
	if !b.healthy.Load() {
		return false
	}

	// Check NATS connection status
	if b.conn == nil || !b.conn.IsConnected() {
		return false
	}

	return true
}

// GetMetrics returns current bus metrics.
func (b *natsBus) GetMetrics() Metrics {
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
		Type:             "nats",
		Healthy:          b.IsHealthy(),
		Channel:          b.subject,
		Subscribers:      subscriberCount,
		PublishErrors:    b.publishErrors.Load(),
		MessagesReceived: b.messagesRecv.Load(),
		LastPublishAgo:   lastPubAgo,
		LastPublishTime:  lastPubTime,
	}
}

// fanOut sends a message to all local subscribers.
func (b *natsBus) fanOut(msg *Message) {
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

// healthCheckLoop periodically checks NATS connection health.
func (b *natsBus) healthCheckLoop() {
	defer b.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-b.ctx.Done():
			return
		case <-ticker.C:
			if b.conn == nil || !b.conn.IsConnected() {
				b.logger.Error().Msg("NATS health check failed: not connected")
				b.healthy.Store(false)
			} else {
				// Flush to ensure connection is active
				if err := b.conn.FlushTimeout(5 * time.Second); err != nil {
					b.logger.Error().Err(err).Msg("NATS health check failed: flush timeout")
					b.healthy.Store(false)
				} else {
					if b.logger.GetLevel() <= zerolog.DebugLevel {
						b.logger.Debug().
							Str("connected_url", b.conn.ConnectedUrl()).
							Msg("NATS health check passed")
					}
					b.healthy.Store(true)
				}
			}
		}
	}
}
