// Package jetstreambackend provides the NATS JetStream implementation of the
// MessageBackend interface. It uses persistent streams with sequence-based
// replay, multi-tenant stream isolation, and registry-driven topic discovery.
package jetstreambackend

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/server/backend"
	"github.com/klurvio/sukko/internal/server/broadcast"
	"github.com/klurvio/sukko/internal/server/metrics"
	"github.com/klurvio/sukko/internal/shared/logging"
	"github.com/klurvio/sukko/internal/shared/types"
)

// Default configuration constants.
const (
	defaultReconnectWait   = 2 * time.Second
	defaultMaxDeliver      = 3
	defaultAckWait         = 30 * time.Second
	defaultRefreshTimeout  = 30 * time.Second
	defaultRefreshInterval = 60 * time.Second
	defaultReplayFetchWait = 2 * time.Second
	defaultMaxAge          = 24 * time.Hour
)

// JetStreamBackend uses NATS JetStream for persistent message streams
// with sequence-based replay. It supports multi-tenant stream isolation
// via registry-driven topic discovery.
type JetStreamBackend struct {
	conn *nats.Conn
	js   jetstream.JetStream
	bus  broadcast.Bus

	registry  types.TenantRegistry
	namespace string
	replicas  int
	maxAge    time.Duration

	// streamsMu and consumersMu are independent — never held simultaneously.
	// refreshStreams uses a two-phase pattern: lock → check → unlock → I/O → lock → insert.
	streams   map[string]jetstream.Stream
	streamsMu sync.RWMutex

	consumers   map[string]jetstream.ConsumeContext
	consumersMu sync.Mutex

	refreshInterval time.Duration
	logger          zerolog.Logger
	healthy         atomic.Bool

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Config holds configuration for the JetStream backend.
type Config struct {
	// NATS connection
	URLs     []string // NATS server URLs
	Token    string   // Auth token
	User     string   // Username
	Password string   // Password

	// TLS
	TLSEnabled  bool
	TLSInsecure bool
	TLSCAPath   string

	// Stream configuration
	Replicas  int           // Stream replicas (default: 1)
	MaxAge    time.Duration // Message retention (default: 24h)
	Namespace string        // Topic namespace (e.g., "prod", "dev")

	// Dependencies
	BroadcastBus    broadcast.Bus
	Registry        types.TenantRegistry
	RefreshInterval time.Duration // How often to refresh streams from registry

	// Logging
	LogLevel  string
	LogFormat string
}

// New creates a new JetStream backend with the provided configuration.
// It connects to NATS and obtains a JetStream context. Streams are NOT
// created here — that happens in Start() after registry is available.
func New(cfg Config) (*JetStreamBackend, error) {
	if len(cfg.URLs) == 0 {
		return nil, errors.New("jetstream backend: at least one NATS URL is required")
	}
	if cfg.BroadcastBus == nil {
		return nil, errors.New("jetstream backend: broadcast bus is required")
	}

	logger := logging.NewLogger(logging.LoggerConfig{
		Level:       logging.LogLevel(cfg.LogLevel),
		Format:      logging.LogFormat(cfg.LogFormat),
		ServiceName: "ws-server",
	}).With().Str("component", "jetstream-backend").Logger()

	if cfg.Registry == nil {
		logger.Warn().Msg("No tenant registry configured — streams will not be auto-discovered")
	}

	// Build NATS connection options
	opts := []nats.Option{
		nats.Name("sukko-jetstream"),
		nats.ReconnectWait(defaultReconnectWait),
		nats.MaxReconnects(-1), // Reconnect forever
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			if err != nil {
				logger.Warn().Err(err).Msg("NATS JetStream disconnected")
			}
		}),
		nats.ReconnectHandler(func(_ *nats.Conn) {
			logger.Info().Msg("NATS JetStream reconnected")
		}),
	}

	// Auth: token or user/password
	if cfg.Token != "" {
		opts = append(opts, nats.Token(cfg.Token))
	} else if cfg.User != "" {
		opts = append(opts, nats.UserInfo(cfg.User, cfg.Password))
	}

	// TLS
	if cfg.TLSEnabled {
		tc := &tls.Config{
			InsecureSkipVerify: cfg.TLSInsecure, //nolint:gosec // Controlled by configuration for dev/testing environments
			MinVersion:         tls.VersionTLS12,
		}
		if cfg.TLSCAPath != "" {
			caCert, err := os.ReadFile(cfg.TLSCAPath)
			if err != nil {
				return nil, fmt.Errorf("jetstream backend: read CA cert: %w", err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(caCert) {
				return nil, fmt.Errorf("jetstream backend: parse CA cert from %s", cfg.TLSCAPath)
			}
			tc.RootCAs = pool
		}
		opts = append(opts, nats.Secure(tc))
	}

	// Connect to NATS
	urlStr := strings.Join(cfg.URLs, ",")
	conn, err := nats.Connect(urlStr, opts...)
	if err != nil {
		return nil, fmt.Errorf("jetstream backend: connect to NATS: %w", err)
	}

	// Get JetStream context
	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("jetstream backend: create JetStream context: %w", err)
	}

	replicas := cfg.Replicas
	if replicas < 1 {
		replicas = 1
	}
	maxAge := cfg.MaxAge
	if maxAge == 0 {
		maxAge = defaultMaxAge
	}
	refreshInterval := cfg.RefreshInterval
	if refreshInterval == 0 {
		refreshInterval = defaultRefreshInterval
	}

	ctx, cancel := context.WithCancel(context.Background())

	jsb := &JetStreamBackend{
		conn:            conn,
		js:              js,
		bus:             cfg.BroadcastBus,
		registry:        cfg.Registry,
		namespace:       cfg.Namespace,
		replicas:        replicas,
		maxAge:          maxAge,
		streams:         make(map[string]jetstream.Stream),
		consumers:       make(map[string]jetstream.ConsumeContext),
		refreshInterval: refreshInterval,
		logger:          logger,
		ctx:             ctx,
		cancel:          cancel,
	}

	logger.Info().
		Str("urls", urlStr).
		Int("replicas", replicas).
		Dur("max_age", maxAge).
		Msg("NATS JetStream backend created")

	return jsb, nil
}

// Start begins the JetStream backend's stream management loop.
// It creates initial streams from registry and starts the refresh loop.
func (jsb *JetStreamBackend) Start(ctx context.Context) error {
	// Initial stream refresh — use caller's ctx so startup timeout is respected
	if err := jsb.refreshStreams(ctx); err != nil {
		jsb.logger.Warn().Err(err).Msg("Initial stream refresh failed (will retry)")
	}

	// Start refresh loop
	jsb.wg.Add(1)
	go func() {
		defer logging.RecoverPanic(jsb.logger, "jetstream_refresh_loop", nil)
		defer jsb.wg.Done()
		jsb.refreshLoop()
	}()

	jsb.healthy.Store(true)
	metrics.SetBackendHealthy("nats", true)
	jsb.logger.Info().Msg("JetStream backend started")
	return nil
}

// Publish sends a message to the JetStream stream for the given channel.
// The channel format is "tenant.symbol.eventType" which maps to a NATS subject.
func (jsb *JetStreamBackend) Publish(ctx context.Context, clientID int64, channel string, data []byte) error {
	if channel == "" {
		return fmt.Errorf("%w: channel is required", backend.ErrPublishFailed)
	}

	// Use the channel directly as the NATS subject (dot-separated hierarchy)
	subject := channel

	start := time.Now()
	_, err := jsb.js.Publish(ctx, subject, data)
	metrics.RecordBackendPublishLatency("nats", time.Since(start).Seconds())
	if err != nil {
		metrics.RecordBackendPublishError("nats")
		return fmt.Errorf("jetstream publish: %w", err)
	}

	metrics.RecordBackendPublish("nats")
	jsb.logger.Debug().
		Int64("client_id", clientID).
		Str("channel", channel).
		Int("data_size", len(data)).
		Msg("Published to JetStream")

	return nil
}

// Replay returns messages from the specified sequences for client reconnection.
// For each stream/position entry, it creates an ordered consumer starting from
// sequence+1 and fetches up to MaxMessages.
func (jsb *JetStreamBackend) Replay(ctx context.Context, req backend.ReplayRequest) ([]backend.ReplayMessage, error) {
	metrics.RecordBackendReplayRequest("nats")

	if len(req.Positions) == 0 {
		return nil, nil
	}

	var allMessages []backend.ReplayMessage
	remaining := req.MaxMessages
	if remaining <= 0 {
		remaining = backend.DefaultMaxReplayMessages
	}

	// Build subscription filter for matching
	subSet := make(map[string]bool, len(req.Subscriptions))
	for _, s := range req.Subscriptions {
		subSet[s] = true
	}

	streamsAttempted := 0
	streamsFailed := 0
	for streamName, lastSeq := range req.Positions {
		if remaining <= 0 {
			break
		}

		// Check if caller has timed out before starting next stream fetch
		if err := ctx.Err(); err != nil {
			jsb.logger.Debug().Err(err).Msg("Replay cancelled by caller")
			break
		}

		jsb.streamsMu.RLock()
		stream, exists := jsb.streams[streamName]
		jsb.streamsMu.RUnlock()

		if !exists {
			jsb.logger.Debug().
				Str("stream", streamName).
				Msg("Stream not found for replay, skipping")
			streamsFailed++
			continue
		}
		streamsAttempted++

		// Validate sequence before uint64 cast (negative values wrap to huge numbers)
		if lastSeq < 0 {
			jsb.logger.Warn().
				Str("stream", streamName).
				Int64("last_seq", lastSeq).
				Msg("Invalid negative sequence for replay, skipping")
			streamsFailed++
			continue
		}

		// Create ordered consumer starting after the last sequence
		consumer, err := stream.OrderedConsumer(ctx, jetstream.OrderedConsumerConfig{
			DeliverPolicy: jetstream.DeliverByStartSequencePolicy,
			OptStartSeq:   uint64(lastSeq) + 1,
		})
		if err != nil {
			jsb.logger.Warn().
				Err(err).
				Str("stream", streamName).
				Int64("from_seq", lastSeq+1).
				Msg("Failed to create replay consumer")
			streamsFailed++
			continue
		}

		// Fetch messages
		msgs, err := consumer.Fetch(remaining, jetstream.FetchMaxWait(defaultReplayFetchWait))
		if err != nil && !errors.Is(err, jetstream.ErrNoMessages) {
			jsb.logger.Warn().
				Err(err).
				Str("stream", streamName).
				Msg("Error fetching replay messages")
			streamsFailed++
			continue
		}

		for msg := range msgs.Messages() {
			subject := msg.Subject()

			// Filter by subscriptions if provided
			if len(subSet) > 0 && !subSet[subject] {
				continue
			}

			allMessages = append(allMessages, backend.ReplayMessage{
				Subject: subject,
				Data:    msg.Data(),
			})
			remaining--

			if remaining <= 0 {
				break
			}
		}

		// Check for errors that occurred during fetch iteration (network issues, timeouts)
		if fetchErr := msgs.Error(); fetchErr != nil && !errors.Is(fetchErr, jetstream.ErrNoMessages) {
			jsb.logger.Warn().
				Err(fetchErr).
				Str("stream", streamName).
				Msg("Error during replay fetch iteration")
		}
	}

	jsb.logger.Debug().
		Int("streams_attempted", streamsAttempted).
		Int("streams_failed", streamsFailed).
		Int("messages_replayed", len(allMessages)).
		Msg("Replay completed")
	metrics.RecordBackendReplayMessages("nats", len(allMessages))
	return allMessages, nil
}

// IsHealthy returns true if the NATS connection is active.
func (jsb *JetStreamBackend) IsHealthy() bool {
	return jsb.healthy.Load() && jsb.conn != nil && jsb.conn.IsConnected()
}

// Shutdown gracefully stops the JetStream backend.
// Cancels context, waits for goroutines, stops consumers, drains connection.
func (jsb *JetStreamBackend) Shutdown(ctx context.Context) error {
	jsb.healthy.Store(false)
	metrics.SetBackendHealthy("nats", false)
	jsb.cancel()

	// Wait for refresh loop to stop (with timeout from caller's ctx)
	done := make(chan struct{})
	go func() {
		defer logging.RecoverPanic(jsb.logger, "shutdown_wait", nil)
		jsb.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		// Goroutines exited cleanly
	case <-ctx.Done():
		jsb.logger.Warn().Msg("Shutdown timed out waiting for goroutines")
	}

	// Multi-step cleanup: continue on individual failures (Constitution IV)
	var errs []error

	// Stop all consumers
	jsb.consumersMu.Lock()
	for name, cc := range jsb.consumers {
		cc.Stop()
		jsb.logger.Debug().Str("consumer", name).Msg("Stopped JetStream consumer")
	}
	jsb.consumers = make(map[string]jetstream.ConsumeContext)
	jsb.consumersMu.Unlock()

	// Drain NATS connection (flushes pending messages)
	if err := jsb.conn.Drain(); err != nil {
		jsb.logger.Warn().Err(err).Msg("Error draining NATS connection")
		errs = append(errs, fmt.Errorf("jetstream backend: drain NATS connection: %w", err))
	}

	jsb.logger.Info().Msg("JetStream backend shut down")
	return errors.Join(errs...)
}

// refreshLoop periodically refreshes streams from the registry.
func (jsb *JetStreamBackend) refreshLoop() {
	ticker := time.NewTicker(jsb.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-jsb.ctx.Done():
			return
		case <-ticker.C:
			if err := jsb.refreshStreams(jsb.ctx); err != nil {
				jsb.logger.Warn().Err(err).Msg("Stream refresh failed")
			}
		}
	}
}

// refreshStreams discovers tenant topics from registry and ensures streams exist.
// For each tenant, it creates or updates a stream with the appropriate subject filter.
func (jsb *JetStreamBackend) refreshStreams(parentCtx context.Context) error {
	if jsb.registry == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(parentCtx, defaultRefreshTimeout)
	defer cancel()

	// Get shared topics
	sharedTopics, err := jsb.registry.GetSharedTenantTopics(ctx, jsb.namespace)
	if err != nil {
		return fmt.Errorf("get shared topics: %w", err)
	}

	// Get dedicated tenants
	dedicatedTenants, err := jsb.registry.GetDedicatedTenants(ctx, jsb.namespace)
	if err != nil {
		return fmt.Errorf("get dedicated tenants: %w", err)
	}

	// Collect all unique tenant IDs from topics
	// Topics are in format: namespace.tenant.category
	tenantStreams := make(map[string][]string) // streamName → subjects

	// From shared topics, group by tenant
	for _, topic := range sharedTopics {
		parts := strings.SplitN(topic, ".", 3)
		if len(parts) < 2 {
			continue
		}
		tenantID := parts[1]
		streamName := jsb.streamName(tenantID)
		// Use wildcard subject to capture all categories for this tenant
		subject := parts[0] + "." + tenantID + ".>"
		tenantStreams[streamName] = []string{subject}
	}

	// From dedicated tenants
	for _, dt := range dedicatedTenants {
		streamName := jsb.streamName(dt.TenantID)
		if len(dt.Topics) > 0 {
			parts := strings.SplitN(dt.Topics[0], ".", 3)
			if len(parts) >= 2 {
				subject := parts[0] + "." + dt.TenantID + ".>"
				tenantStreams[streamName] = []string{subject}
			}
		}
	}

	// Phase 1: Determine which streams are missing (read lock only)
	jsb.streamsMu.RLock()
	type pendingStream struct {
		name     string
		subjects []string
	}
	var missing []pendingStream
	for name, subjects := range tenantStreams {
		if _, exists := jsb.streams[name]; !exists {
			missing = append(missing, pendingStream{name: name, subjects: subjects})
		}
	}
	jsb.streamsMu.RUnlock()

	if len(missing) == 0 {
		return nil
	}

	// Phase 2: Create streams without holding any lock (network I/O)
	// Constitution VII: Mutexes MUST NOT be held across I/O operations
	type createdStream struct {
		name     string
		subjects []string
		stream   jetstream.Stream
	}
	var created []createdStream

	for _, m := range missing {
		stream, err := jsb.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
			Name:      m.name,
			Subjects:  m.subjects,
			MaxAge:    jsb.maxAge,
			Replicas:  jsb.replicas,
			Retention: jetstream.LimitsPolicy,
			Storage:   jetstream.FileStorage,
		})
		if err != nil {
			jsb.logger.Warn().
				Err(err).
				Str("stream", m.name).
				Strs("subjects", m.subjects).
				Msg("Failed to create/update JetStream stream")
			continue
		}

		jsb.logger.Info().
			Str("stream", m.name).
			Strs("subjects", m.subjects).
			Msg("JetStream stream created/updated")

		created = append(created, createdStream{name: m.name, subjects: m.subjects, stream: stream})
	}

	// Phase 3: Insert under write lock (no I/O under lock)
	var actuallyCreated []createdStream
	jsb.streamsMu.Lock()
	for _, c := range created {
		if _, exists := jsb.streams[c.name]; exists {
			continue // Another goroutine created it while we were unlocked
		}
		jsb.streams[c.name] = c.stream
		actuallyCreated = append(actuallyCreated, c)
	}
	jsb.streamsMu.Unlock()

	// Phase 4: Start consumers only for streams we actually inserted
	for _, c := range actuallyCreated {
		jsb.startStreamConsumer(c.name, c.stream)
	}

	return nil
}

// startStreamConsumer starts a push-based consumer for a stream that routes
// messages to the broadcast bus. Performs NATS I/O (CreateOrUpdateConsumer,
// Consume) outside of locks per Constitution VII.
func (jsb *JetStreamBackend) startStreamConsumer(name string, stream jetstream.Stream) {
	// Phase 1: Stop existing consumer under lock (Stop is non-blocking)
	jsb.consumersMu.Lock()
	if existing, ok := jsb.consumers[name]; ok {
		existing.Stop()
		delete(jsb.consumers, name)
	}
	jsb.consumersMu.Unlock()

	// Phase 2: Create consumer without holding any lock (network I/O)
	consumerName := "sukko-" + name
	consumer, err := stream.CreateOrUpdateConsumer(jsb.ctx, jetstream.ConsumerConfig{
		Name:          consumerName,
		Durable:       consumerName,
		DeliverPolicy: jetstream.DeliverNewPolicy,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    defaultMaxDeliver,
		AckWait:       defaultAckWait,
	})
	if err != nil {
		jsb.logger.Error().
			Err(err).
			Str("stream", name).
			Str("consumer", consumerName).
			Msg("Failed to create JetStream consumer")
		return
	}

	// Start consuming messages (network I/O — still no lock held)
	cc, err := consumer.Consume(func(msg jetstream.Msg) {
		// Route message to broadcast bus
		jsb.bus.Publish(&broadcast.Message{
			Subject: msg.Subject(),
			Payload: msg.Data(),
		})
		metrics.RecordBackendConsume("nats")

		// Acknowledge message
		if err := msg.Ack(); err != nil {
			jsb.logger.Warn().
				Err(err).
				Str("subject", msg.Subject()).
				Msg("Failed to ack JetStream message")
		}
	})
	if err != nil {
		jsb.logger.Error().
			Err(err).
			Str("stream", name).
			Msg("Failed to start JetStream consume")
		return
	}

	// Phase 3: Insert under lock (no I/O)
	jsb.consumersMu.Lock()
	jsb.consumers[name] = cc
	jsb.consumersMu.Unlock()

	jsb.logger.Info().
		Str("stream", name).
		Str("consumer", consumerName).
		Msg("JetStream consumer started")
}

// streamName builds a stream name for a tenant.
// Format: SUKKO_{namespace}_{tenantID} (uppercase, hyphens replaced with underscores).
func (jsb *JetStreamBackend) streamName(tenantID string) string {
	ns := strings.ToUpper(strings.ReplaceAll(jsb.namespace, "-", "_"))
	tid := strings.ToUpper(strings.ReplaceAll(tenantID, "-", "_"))
	return "SUKKO_" + ns + "_" + tid
}

// SplitURLs splits a comma-separated NATS URL string into individual addresses.
func SplitURLs(urls string) []string {
	var result []string
	for u := range strings.SplitSeq(urls, ",") {
		trimmed := strings.TrimSpace(u)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// Compile-time interface check.
var _ backend.MessageBackend = (*JetStreamBackend)(nil)
