package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/cmd/tester/auth"
	"github.com/klurvio/sukko/cmd/tester/publisher"
	testerws "github.com/klurvio/sukko/cmd/tester/ws"
	"github.com/klurvio/sukko/internal/shared/logging"
)

// defaultDeliveryTimeout is the maximum time to wait for message delivery.
const defaultDeliveryTimeout = 5 * time.Second

// misrouteGraceWindow is the pause after all expected receivers have the message, giving a
// misrouted copy time to arrive at a must-NOT-receive user before buildResult snapshots
// MisroutedTo. This is the grace window the tenant-isolation negative assertion relies on
// (NFR-003): its value MUST NOT be reduced.
const misrouteGraceWindow = 100 * time.Millisecond

// Delivery-liveness warmup bounds (see waitForDeliveryLive). Sized for the Kafka cold-start
// window: a freshly-provisioned tenant's ws-server consumer joins its topic at AtEnd only after a
// control-plane propagation delay (WatchTopics delta → topic create → AddConsumeTopics), so early
// publishes are dropped. Direct mode returns within one interval.
const (
	deliveryWarmupTimeout  = 15 * time.Second
	deliveryWarmupInterval = 250 * time.Millisecond
)

// waitForDeliveryLive proves the full publish→(produce→consume→)broadcast loop is live for `channel`
// before a caller publishes a measured sequence, then resets the user's received tracker so the
// warmup does not pollute the caller's counts. Backend-agnostic: in direct mode the first probe
// round-trips within one interval; in Kafka mode it absorbs the new-tenant consumer cold-start.
//
// It REPUBLISHES the probe each interval rather than publishing once: in Kafka mode the first
// probe(s) may be produced before the consumer joins the tenant topic (AtEnd) and thus be dropped,
// so a single probe can be lost. Receiving any probe means the loop is live. Returns an error
// (never a silent skip) if the loop is not live within deliveryWarmupTimeout.
func waitForDeliveryLive(ctx context.Context, user *TestUser, channel string, logger zerolog.Logger) error {
	warmupID := "warmup-" + uuid.NewString()
	payload, _ := json.Marshal(map[string]any{"msg_id": warmupID}) // literal map of primitives cannot fail
	publish := func() error {
		if err := user.Client.Publish(channel, payload); err != nil {
			return fmt.Errorf("publish warmup probe on %s: %w", channel, err)
		}
		return nil
	}
	return probeUntilLive(ctx, deliveryWarmupTimeout, deliveryWarmupInterval,
		publish, func() bool { return user.HasReceived(warmupID) }, user.ClearReceived, logger)
}

// probeUntilLive is the transport-agnostic core of waitForDeliveryLive, split out so its retry /
// timeout / reset contract is unit-testable without a live WebSocket (publish/received/reset are
// injected). It publishes once, then on each interval checks for receipt (→ reset + return nil) or
// republishes. Returns an error on timeout or ctx cancel, or if publish fails.
func probeUntilLive(ctx context.Context, timeout, interval time.Duration, publish func() error, received func() bool, reset func(), logger zerolog.Logger) error {
	if err := publish(); err != nil {
		return err
	}

	deadline := time.After(timeout)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			return fmt.Errorf("delivery loop not live after %s (no warmup probe received)", timeout)
		case <-ctx.Done():
			return fmt.Errorf("delivery warmup canceled: %w", ctx.Err())
		case <-ticker.C:
			if received() {
				reset()
				logger.Debug().Msg("delivery loop live (warmup probe received)")
				return nil
			}
			if err := publish(); err != nil {
				return err
			}
		}
	}
}

// PubSubEngine coordinates publishers and subscribers for delivery verification.
// Tracks message IDs (UUIDs) to verify that the right messages reach the right subscribers.
type PubSubEngine struct {
	gatewayURL string
	logger     zerolog.Logger
	timeout    time.Duration
}

// PubSubEngineConfig configures the pub-sub engine.
type PubSubEngineConfig struct {
	GatewayURL string
	Logger     zerolog.Logger
	Timeout    time.Duration // delivery timeout; defaults to 5s
}

// NewPubSubEngine creates a new delivery verification engine.
func NewPubSubEngine(cfg PubSubEngineConfig) *PubSubEngine {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultDeliveryTimeout
	}
	return &PubSubEngine{
		gatewayURL: cfg.GatewayURL,
		logger:     cfg.Logger.With().Str("component", "pubsub_engine").Logger(),
		timeout:    timeout,
	}
}

// TestUser is a WebSocket client with specific JWT claims for scoping tests.
type TestUser struct {
	Subject       string
	Groups        []string
	Token         string
	Client        *testerws.Client
	mu            sync.RWMutex
	received      map[string]receivedMsg // message ID → receipt info
	receivedOrder []string               // message IDs in arrival order
}

type receivedMsg struct {
	channel string
	at      time.Time
}

// HasReceived returns whether the user received a message with the given ID.
func (u *TestUser) HasReceived(msgID string) bool {
	u.mu.RLock()
	defer u.mu.RUnlock()
	_, ok := u.received[msgID]
	return ok
}

// ReceivedCount returns the number of messages received.
func (u *TestUser) ReceivedCount() int {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return len(u.received)
}

// ReceivedOrder returns message IDs in the order they arrived.
func (u *TestUser) ReceivedOrder() []string {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return slices.Clone(u.receivedOrder)
}

// AsPublisher returns a Publisher that publishes via this user's WebSocket connection.
// Used for authorization + delivery testing — the gateway checks this user's JWT claims.
func (u *TestUser) AsPublisher() publisher.Publisher {
	return publisher.NewClientPublisher(u.Client)
}

// ClearReceived resets the received message tracker. Call between test checks.
func (u *TestUser) ClearReceived() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.received = make(map[string]receivedMsg)
	u.receivedOrder = u.receivedOrder[:0]
}

// acceptsDeliveryEnvelope reports whether a WebSocket message type is a delivered payload
// worth tracking for a delivery check. Delivered broadcasts arrive as {"type":"message",...}
// (the server's BroadcastEnvelope); peer-forwarded publishes as {"type":"publish",...} —
// accept BOTH, otherwise delivered messages are silently ignored and every delivery check
// reports "missing" despite successful delivery. Shared by every suite that asserts WS
// delivery (do not reimplement the predicate per-suite).
func acceptsDeliveryEnvelope(msgType string) bool {
	return msgType == "publish" || msgType == "message"
}

// onMessage is the callback for incoming WebSocket messages.
// Called from the client's read loop goroutine — writes are mutex-protected.
func (u *TestUser) onMessage(msg testerws.Message) {
	if !acceptsDeliveryEnvelope(msg.Type) {
		return
	}
	var payload struct {
		MsgID string `json:"msg_id"`
	}
	if err := json.Unmarshal(msg.Data, &payload); err != nil || payload.MsgID == "" {
		return // not a tracked message
	}

	u.mu.Lock()
	defer u.mu.Unlock()
	u.received[payload.MsgID] = receivedMsg{channel: msg.Channel, at: time.Now()}
	u.receivedOrder = append(u.receivedOrder, payload.MsgID)
}

// CreateUser mints a JWT with custom claims, connects to the gateway, and starts tracking messages.
func (e *PubSubEngine) CreateUser(ctx context.Context, minter *auth.Minter, opts auth.MintOptions) (*TestUser, error) {
	token, err := minter.MintWithClaims(opts)
	if err != nil {
		return nil, fmt.Errorf("mint token for %s: %w", opts.Subject, err)
	}

	user := &TestUser{
		Subject:  opts.Subject,
		Groups:   opts.Groups,
		Token:    token,
		received: make(map[string]receivedMsg),
	}

	// Retry the connect: a freshly-minted user's signing key may not have propagated
	// to the gateway's key-registry cache yet (provisioning pushes it over the gRPC
	// stream asynchronously), so the first attempt can 401. connectWithRetry absorbs
	// that key-registry cache race.
	client, err := connectWithRetry(ctx, e.gatewayURL, token, e.logger, user.onMessage)
	if err != nil {
		return nil, fmt.Errorf("connect user %s: %w", opts.Subject, err)
	}
	user.Client = client

	// Start ReadLoop so onMessage callback fires for incoming messages.
	// Without this, PublishAndVerify delivery checks would time out.
	// Goroutine lifecycle: bounded to client connection — exits when client.Close() is called.
	go func() {
		defer logging.RecoverPanic(e.logger, "pubsub-engine-read-loop", nil)
		_, _ = client.ReadLoop(ctx)
	}()

	return user, nil
}

// DeliveryResult captures the outcome of a publish-and-verify check.
type DeliveryResult struct {
	Channel     string
	MessageID   string
	ExpectedBy  []string // subjects that should have received
	ReceivedBy  []string // subjects that actually received
	MisroutedTo []string // subjects that received but shouldn't have
	Missing     []string // subjects that should have received but didn't
	Delivered   bool
	Latency     time.Duration
}

// PublishAndVerify publishes a message with a UUID and verifies delivery.
// The pub argument determines the publish path:
//   - TestUser.AsPublisher() → publishes via user's WS connection (tests auth+delivery)
//   - KafkaPublisher → publishes via backend (tests delivery only)
//
// expectedReceivers are users that SHOULD get the message.
// allUsers includes ALL connected users (expected + those that should NOT receive).
func (e *PubSubEngine) PublishAndVerify(
	ctx context.Context,
	pub publisher.Publisher,
	channel string,
	expectedReceivers []*TestUser,
	allUsers []*TestUser,
) DeliveryResult {
	msgID := uuid.NewString()
	payload, _ := json.Marshal(map[string]any{ // json.Marshal on literal map of primitives cannot fail
		"msg_id": msgID,
		"ts":     time.Now().UnixMilli(),
	})

	start := time.Now()

	// Publish via the provided publisher
	if err := pub.Publish(ctx, channel, payload); err != nil {
		return DeliveryResult{
			Channel:   channel,
			MessageID: msgID,
			Delivered: false,
		}
	}

	// Build expected set
	expectedSet := make(map[string]bool, len(expectedReceivers))
	for _, u := range expectedReceivers {
		expectedSet[u.Subject] = true
	}

	// Wait for delivery
	deadline := time.After(e.timeout)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			return e.buildResult(channel, msgID, start, expectedSet, allUsers)
		case <-ctx.Done():
			return e.buildResult(channel, msgID, start, expectedSet, allUsers)
		case <-ticker.C:
			// Check if all expected receivers got the message
			allReceived := true
			for _, u := range expectedReceivers {
				if !u.HasReceived(msgID) {
					allReceived = false
					break
				}
			}
			if allReceived {
				// Small grace period for misrouted messages to arrive
				time.Sleep(misrouteGraceWindow)
				return e.buildResult(channel, msgID, start, expectedSet, allUsers)
			}
		}
	}
}

func (e *PubSubEngine) buildResult(
	channel, msgID string,
	start time.Time,
	expectedSet map[string]bool,
	allUsers []*TestUser,
) DeliveryResult {
	result := DeliveryResult{
		Channel:   channel,
		MessageID: msgID,
		Latency:   time.Since(start),
		Delivered: true,
	}

	for _, u := range allUsers {
		got := u.HasReceived(msgID)
		expected := expectedSet[u.Subject]

		if expected {
			result.ExpectedBy = append(result.ExpectedBy, u.Subject)
		}

		if got {
			result.ReceivedBy = append(result.ReceivedBy, u.Subject)
		}

		if got && !expected {
			result.MisroutedTo = append(result.MisroutedTo, u.Subject)
		}

		if !got && expected {
			result.Missing = append(result.Missing, u.Subject)
			result.Delivered = false
		}
	}

	return result
}
