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

// onMessage is the callback for incoming WebSocket messages.
// Called from the client's read loop goroutine — writes are mutex-protected.
func (u *TestUser) onMessage(msg testerws.Message) {
	if msg.Type != "publish" {
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

	client, err := testerws.Connect(ctx, testerws.ConnectConfig{
		GatewayURL: e.gatewayURL,
		Token:      token,
		Logger:     e.logger,
		OnMessage:  user.onMessage,
	})
	if err != nil {
		return nil, fmt.Errorf("connect user %s: %w", opts.Subject, err)
	}
	user.Client = client

	// Start ReadLoop so onMessage callback fires for incoming messages.
	// Without this, PublishAndVerify delivery checks would time out.
	// Goroutine lifecycle: bounded to client connection — exits when client.Close() is called.
	go func() {
		defer logging.RecoverPanic(e.logger, "pubsub-engine-read-loop", nil)
		client.ReadLoop(ctx)
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
//   - KafkaPublisher/NATSPublisher → publishes via backend (tests delivery only)
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
				time.Sleep(100 * time.Millisecond)
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
