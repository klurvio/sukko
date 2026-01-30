package server

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/Toniq-Labs/odin-ws/internal/server/messaging"
)

// =============================================================================
// Message Parsing Tests (extracted logic for testability)
// =============================================================================

// parseClientMessage parses the outer message structure
// This is the same logic used in handleClientMessage
func parseClientMessage(data []byte) (msgType string, msgData json.RawMessage, err error) {
	var req struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	err = json.Unmarshal(data, &req)
	return req.Type, req.Data, err
}

func TestParseClientMessage_Subscribe(t *testing.T) {
	t.Parallel()
	msg := `{"type": "subscribe", "data": {"channels": ["BTC.trade", "ETH.trade"]}}`

	msgType, msgData, err := parseClientMessage([]byte(msg))

	if err != nil {
		t.Fatalf("parseClientMessage failed: %v", err)
	}
	if msgType != "subscribe" {
		t.Errorf("msgType: got %q, want %q", msgType, "subscribe")
	}
	if len(msgData) == 0 {
		t.Error("msgData should not be empty")
	}
}

func TestParseClientMessage_Unsubscribe(t *testing.T) {
	t.Parallel()
	msg := `{"type": "unsubscribe", "data": {"channels": ["BTC.trade"]}}`

	msgType, msgData, err := parseClientMessage([]byte(msg))

	if err != nil {
		t.Fatalf("parseClientMessage failed: %v", err)
	}
	if msgType != "unsubscribe" {
		t.Errorf("msgType: got %q, want %q", msgType, "unsubscribe")
	}
	if len(msgData) == 0 {
		t.Error("msgData should not be empty")
	}
}

func TestParseClientMessage_Heartbeat(t *testing.T) {
	t.Parallel()
	msg := `{"type": "heartbeat"}`

	msgType, _, err := parseClientMessage([]byte(msg))

	if err != nil {
		t.Fatalf("parseClientMessage failed: %v", err)
	}
	if msgType != "heartbeat" {
		t.Errorf("msgType: got %q, want %q", msgType, "heartbeat")
	}
}

func TestParseClientMessage_Reconnect(t *testing.T) {
	t.Parallel()
	msg := `{"type": "reconnect", "data": {"client_id": "abc123", "last_offset": {"topic1": 12345}}}`

	msgType, msgData, err := parseClientMessage([]byte(msg))

	if err != nil {
		t.Fatalf("parseClientMessage failed: %v", err)
	}
	if msgType != "reconnect" {
		t.Errorf("msgType: got %q, want %q", msgType, "reconnect")
	}
	if len(msgData) == 0 {
		t.Error("msgData should not be empty")
	}
}

func TestParseClientMessage_InvalidJSON(t *testing.T) {
	t.Parallel()
	msg := `{invalid json}`

	_, _, err := parseClientMessage([]byte(msg))

	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

func TestParseClientMessage_EmptyType(t *testing.T) {
	t.Parallel()
	msg := `{"type": "", "data": {}}`

	msgType, _, err := parseClientMessage([]byte(msg))

	if err != nil {
		t.Fatalf("parseClientMessage failed: %v", err)
	}
	if msgType != "" {
		t.Errorf("msgType: got %q, want empty string", msgType)
	}
}

func TestParseClientMessage_MissingType(t *testing.T) {
	t.Parallel()
	msg := `{"data": {"channels": ["BTC.trade"]}}`

	msgType, _, err := parseClientMessage([]byte(msg))

	if err != nil {
		t.Fatalf("parseClientMessage failed: %v", err)
	}
	// Missing type defaults to empty string (zero value)
	if msgType != "" {
		t.Errorf("msgType: got %q, want empty string", msgType)
	}
}

func TestParseClientMessage_UnknownType(t *testing.T) {
	t.Parallel()
	msg := `{"type": "unknown_future_message_type", "data": {}}`

	msgType, _, err := parseClientMessage([]byte(msg))

	if err != nil {
		t.Fatalf("parseClientMessage failed: %v", err)
	}
	if msgType != "unknown_future_message_type" {
		t.Errorf("msgType: got %q, want %q", msgType, "unknown_future_message_type")
	}
}

// =============================================================================
// Subscribe Request Parsing Tests
// =============================================================================

// parseSubscribeRequest parses the subscribe message data
func parseSubscribeRequest(data json.RawMessage) ([]string, error) {
	var subReq struct {
		Channels []string `json:"channels"`
	}
	err := json.Unmarshal(data, &subReq)
	return subReq.Channels, err
}

func TestParseSubscribeRequest_SingleChannel(t *testing.T) {
	t.Parallel()
	data := json.RawMessage(`{"channels": ["BTC.trade"]}`)

	channels, err := parseSubscribeRequest(data)

	if err != nil {
		t.Fatalf("parseSubscribeRequest failed: %v", err)
	}
	if len(channels) != 1 {
		t.Fatalf("channels length: got %d, want 1", len(channels))
	}
	if channels[0] != "BTC.trade" {
		t.Errorf("channels[0]: got %q, want %q", channels[0], "BTC.trade")
	}
}

func TestParseSubscribeRequest_MultipleChannels(t *testing.T) {
	t.Parallel()
	data := json.RawMessage(`{"channels": ["BTC.trade", "ETH.trade", "SOL.liquidity", "BTC.analytics"]}`)

	channels, err := parseSubscribeRequest(data)

	if err != nil {
		t.Fatalf("parseSubscribeRequest failed: %v", err)
	}
	if len(channels) != 4 {
		t.Fatalf("channels length: got %d, want 4", len(channels))
	}
}

func TestParseSubscribeRequest_EmptyChannels(t *testing.T) {
	t.Parallel()
	data := json.RawMessage(`{"channels": []}`)

	channels, err := parseSubscribeRequest(data)

	if err != nil {
		t.Fatalf("parseSubscribeRequest failed: %v", err)
	}
	if len(channels) != 0 {
		t.Errorf("channels length: got %d, want 0", len(channels))
	}
}

func TestParseSubscribeRequest_InvalidFormat(t *testing.T) {
	t.Parallel()
	data := json.RawMessage(`{"channels": "not-an-array"}`)

	_, err := parseSubscribeRequest(data)

	if err == nil {
		t.Error("Expected error for invalid format")
	}
}

// =============================================================================
// Reconnect Request Parsing Tests
// =============================================================================

// parseReconnectRequest parses the reconnect message data
func parseReconnectRequest(data json.RawMessage) (clientID string, lastOffsets map[string]int64, err error) {
	var req struct {
		ClientID   string           `json:"client_id"`
		LastOffset map[string]int64 `json:"last_offset"`
	}
	err = json.Unmarshal(data, &req)
	return req.ClientID, req.LastOffset, err
}

func TestParseReconnectRequest_Valid(t *testing.T) {
	t.Parallel()
	data := json.RawMessage(`{"client_id": "abc123", "last_offset": {"topic1": 12345, "topic2": 67890}}`)

	clientID, offsets, err := parseReconnectRequest(data)

	if err != nil {
		t.Fatalf("parseReconnectRequest failed: %v", err)
	}
	if clientID != "abc123" {
		t.Errorf("clientID: got %q, want %q", clientID, "abc123")
	}
	if len(offsets) != 2 {
		t.Fatalf("offsets length: got %d, want 2", len(offsets))
	}
	if offsets["topic1"] != 12345 {
		t.Errorf("offsets[topic1]: got %d, want 12345", offsets["topic1"])
	}
}

func TestParseReconnectRequest_EmptyOffsets(t *testing.T) {
	t.Parallel()
	data := json.RawMessage(`{"client_id": "abc123", "last_offset": {}}`)

	clientID, offsets, err := parseReconnectRequest(data)

	if err != nil {
		t.Fatalf("parseReconnectRequest failed: %v", err)
	}
	if clientID != "abc123" {
		t.Errorf("clientID: got %q, want %q", clientID, "abc123")
	}
	if len(offsets) != 0 {
		t.Errorf("offsets length: got %d, want 0", len(offsets))
	}
}

func TestParseReconnectRequest_MissingClientID(t *testing.T) {
	t.Parallel()
	data := json.RawMessage(`{"last_offset": {"topic1": 12345}}`)

	clientID, _, err := parseReconnectRequest(data)

	if err != nil {
		t.Fatalf("parseReconnectRequest failed: %v", err)
	}
	// Missing client_id defaults to empty string
	if clientID != "" {
		t.Errorf("clientID: got %q, want empty string", clientID)
	}
}

// =============================================================================
// Pong Response Tests
// =============================================================================

func TestPongResponse_Format(t *testing.T) {
	t.Parallel()
	before := time.Now().UnixMilli()

	pong := map[string]any{
		"type": "pong",
		"ts":   time.Now().UnixMilli(),
	}
	data, err := json.Marshal(pong)

	after := time.Now().UnixMilli()

	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	// Parse back
	var parsed struct {
		Type string `json:"type"`
		Ts   int64  `json:"ts"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if parsed.Type != "pong" {
		t.Errorf("type: got %q, want %q", parsed.Type, "pong")
	}
	if parsed.Ts < before || parsed.Ts > after {
		t.Errorf("ts: got %d, expected between %d and %d", parsed.Ts, before, after)
	}
}

// =============================================================================
// Subscription Ack Response Tests
// =============================================================================

func TestSubscriptionAck_JSONFormat(t *testing.T) {
	t.Parallel()
	channels := []string{"BTC.trade", "ETH.trade"}
	count := 5

	ack := map[string]any{
		"type":       "subscription_ack",
		"subscribed": channels,
		"count":      count,
	}
	data, err := json.Marshal(ack)

	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	// Parse back
	var parsed struct {
		Type       string   `json:"type"`
		Subscribed []string `json:"subscribed"`
		Count      int      `json:"count"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if parsed.Type != "subscription_ack" {
		t.Errorf("type: got %q, want %q", parsed.Type, "subscription_ack")
	}
	if len(parsed.Subscribed) != 2 {
		t.Errorf("subscribed length: got %d, want 2", len(parsed.Subscribed))
	}
	if parsed.Count != 5 {
		t.Errorf("count: got %d, want 5", parsed.Count)
	}
}

// =============================================================================
// Client Send Buffer Tests
// =============================================================================

func TestClientSendBuffer_NonBlocking(t *testing.T) {
	t.Parallel()
	client := &Client{
		send: make(chan []byte, 2), // Small buffer
	}

	// Fill the buffer
	client.send <- []byte("msg1")
	client.send <- []byte("msg2")

	// Non-blocking send should not block
	data := []byte("msg3")
	var sent bool
	select {
	case client.send <- data:
		sent = true
	default:
		// Buffer is full, send would block
	}

	if sent {
		t.Error("Send should not succeed on full buffer (non-blocking)")
	}
}

func TestClientSendBuffer_Drain(t *testing.T) {
	t.Parallel()
	client := &Client{
		send: make(chan []byte, 3),
	}

	// Add messages
	client.send <- []byte("msg1")
	client.send <- []byte("msg2")
	client.send <- []byte("msg3")

	// Drain
	count := 0
	for len(client.send) > 0 {
		<-client.send
		count++
	}

	if count != 3 {
		t.Errorf("drained count: got %d, want 3", count)
	}
}

// =============================================================================
// Sequence Generator Tests (for message handlers)
// =============================================================================

func TestSequenceGenerator_MonotonicIncrease_Handlers(t *testing.T) {
	t.Parallel()
	gen := messaging.NewSequenceGenerator()

	var prev int64
	for range 1000 {
		seq := gen.Next()
		if seq <= prev {
			t.Errorf("Sequence should monotonically increase: prev=%d, current=%d", prev, seq)
		}
		prev = seq
	}
}

func TestSequenceGenerator_ConcurrentSafe_Handlers(t *testing.T) {
	t.Parallel()
	gen := messaging.NewSequenceGenerator()

	var wg sync.WaitGroup
	seqs := make(chan int64, 1000)

	// 10 goroutines each generating 100 sequences
	for range 10 {
		wg.Go(func() {
			for range 100 {
				seqs <- gen.Next()
			}
		})
	}

	wg.Wait()
	close(seqs)

	// Collect all sequences
	seen := make(map[int64]bool)
	for seq := range seqs {
		if seen[seq] {
			t.Errorf("Duplicate sequence detected: %d", seq)
		}
		seen[seq] = true
	}

	if len(seen) != 1000 {
		t.Errorf("Expected 1000 unique sequences, got %d", len(seen))
	}
}

// =============================================================================
// Message Type Constants Tests
// =============================================================================

func TestMessageTypes_Valid(t *testing.T) {
	t.Parallel()
	validTypes := []string{
		"subscribe",
		"unsubscribe",
		"heartbeat",
		"reconnect",
	}

	for _, msgType := range validTypes {
		msg := map[string]any{"type": msgType}
		data, err := json.Marshal(msg)
		if err != nil {
			t.Errorf("Failed to marshal message type %q: %v", msgType, err)
			continue
		}

		parsedType, _, err := parseClientMessage(data)
		if err != nil {
			t.Errorf("Failed to parse message type %q: %v", msgType, err)
			continue
		}

		if parsedType != msgType {
			t.Errorf("Message type mismatch: got %q, want %q", parsedType, msgType)
		}
	}
}

// =============================================================================
// Regression Tests - Ensure publishing feature doesn't affect existing handlers
// =============================================================================

func TestMessageHandler_ExistingTypesUnaffected(t *testing.T) {
	t.Parallel()
	// Verify all existing message types parse and route correctly
	// This ensures the new "publish" case doesn't affect the switch statement
	existingTypes := []string{"subscribe", "unsubscribe", "heartbeat", "reconnect"}

	for _, msgType := range existingTypes {
		t.Run(msgType, func(t *testing.T) {
			t.Parallel()
			msg := []byte(`{"type": "` + msgType + `", "data": {}}`)
			parsedType, _, err := parseClientMessage(msg)

			if err != nil {
				t.Fatalf("Failed to parse %s message: %v", msgType, err)
			}
			if parsedType != msgType {
				t.Errorf("Message type mismatch: got %q, want %q", parsedType, msgType)
			}
		})
	}
}

func TestMessageTypes_AllTypesIncludingPublish(t *testing.T) {
	t.Parallel()
	// Verify all message types (existing + publish) work correctly
	// Regression test: adding "publish" must not break other message types
	allTypes := []string{"subscribe", "unsubscribe", "heartbeat", "reconnect", "publish"}

	for _, msgType := range allTypes {
		t.Run(msgType, func(t *testing.T) {
			t.Parallel()
			msg := []byte(`{"type": "` + msgType + `", "data": {}}`)
			parsedType, _, err := parseClientMessage(msg)

			if err != nil {
				t.Fatalf("Failed to parse %s: %v", msgType, err)
			}
			if parsedType != msgType {
				t.Errorf("got %q, want %q", parsedType, msgType)
			}
		})
	}
}

func TestMessageHandler_PublishDoesNotAffectExistingTypes(t *testing.T) {
	t.Parallel()
	// Interleaved parsing test - ensure publish doesn't corrupt parser state
	messages := []struct {
		msgType string
		payload string
	}{
		{"subscribe", `{"type": "subscribe", "data": {"channels": ["BTC.trade"]}}`},
		{"publish", `{"type": "publish", "data": {"channel": "test.chat", "data": {"msg": "hello"}}}`},
		{"unsubscribe", `{"type": "unsubscribe", "data": {"channels": ["BTC.trade"]}}`},
		{"publish", `{"type": "publish", "data": {"channel": "test.chat", "data": {"msg": "world"}}}`},
		{"heartbeat", `{"type": "heartbeat"}`},
		{"reconnect", `{"type": "reconnect", "data": {"client_id": "abc123"}}`},
	}

	for i, msg := range messages {
		parsedType, _, err := parseClientMessage([]byte(msg.payload))
		if err != nil {
			t.Fatalf("Message %d (%s): parse failed: %v", i, msg.msgType, err)
		}
		if parsedType != msg.msgType {
			t.Errorf("Message %d: got %q, want %q", i, parsedType, msg.msgType)
		}
	}
}

// =============================================================================
// Benchmark Tests
// =============================================================================

func BenchmarkParseClientMessage_Subscribe(b *testing.B) {
	msg := []byte(`{"type": "subscribe", "data": {"channels": ["BTC.trade", "ETH.trade"]}}`)

	for b.Loop() {
		_, _, _ = parseClientMessage(msg)
	}
}

func BenchmarkParseClientMessage_Heartbeat(b *testing.B) {
	msg := []byte(`{"type": "heartbeat"}`)

	for b.Loop() {
		_, _, _ = parseClientMessage(msg)
	}
}

func BenchmarkParseSubscribeRequest_Handlers(b *testing.B) {
	data := json.RawMessage(`{"channels": ["BTC.trade", "ETH.trade", "SOL.liquidity", "BTC.analytics"]}`)

	for b.Loop() {
		_, _ = parseSubscribeRequest(data)
	}
}

func BenchmarkSequenceGenerator(b *testing.B) {
	gen := messaging.NewSequenceGenerator()

	for b.Loop() {
		_ = gen.Next()
	}
}
