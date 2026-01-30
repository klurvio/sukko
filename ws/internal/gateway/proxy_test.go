package gateway

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog"

	"github.com/Toniq-Labs/odin-ws/internal/shared/auth"
)

// testClaims creates auth.Claims with the given subject for testing.
func testClaims(subject string) *auth.Claims {
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: subject,
		},
		TenantID: "test-tenant",
		Groups:   []string{},
	}
	return claims
}

// testClaimsWithGroups creates auth.Claims with subject and groups for testing.
func testClaimsWithGroups(subject string, groups []string) *auth.Claims {
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: subject,
		},
		TenantID: "test-tenant",
		Groups:   groups,
	}
	return claims
}

// newTestProxy creates a Proxy for testing interceptClientMessage.
// Uses nil connections since we're only testing message interception.
func newTestProxy(claims *auth.Claims, publicPatterns, userPatterns, groupPatterns []string) *Proxy {
	pc := NewPermissionChecker(publicPatterns, userPatterns, groupPatterns)
	return &Proxy{
		clientConn:     nil, // Not needed for interception tests
		backendConn:    nil, // Not needed for interception tests
		claims:         claims,
		permissions:    pc,
		logger:         zerolog.Nop(),
		messageTimeout: 60 * time.Second,
	}
}

func TestProxy_InterceptClientMessage_AnonymousBypass(t *testing.T) {
	t.Parallel()
	// Anonymous users should bypass all filtering
	anonClaims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: "anonymous",
		},
	}
	proxy := newTestProxy(anonClaims, []string{"*.trade"}, nil, nil)

	input := `{"type":"subscribe","data":{"channels":["secret.channel","forbidden.data"]}}`
	result, err := proxy.interceptClientMessage([]byte(input))

	if err != nil {
		t.Fatalf("interceptClientMessage() error = %v", err)
	}

	// Should pass through unchanged
	if string(result) != input {
		t.Errorf("Anonymous should pass through unchanged.\nGot:  %s\nWant: %s", result, input)
	}
}

func TestProxy_InterceptClientMessage_NilClaimsBypass(t *testing.T) {
	t.Parallel()
	// Nil claims should bypass filtering
	proxy := newTestProxy(nil, []string{"*.trade"}, nil, nil)

	input := `{"type":"subscribe","data":{"channels":["secret.channel"]}}`
	result, err := proxy.interceptClientMessage([]byte(input))

	if err != nil {
		t.Fatalf("interceptClientMessage() error = %v", err)
	}

	if string(result) != input {
		t.Errorf("Nil claims should pass through unchanged.\nGot:  %s\nWant: %s", result, input)
	}
}

func TestProxy_InterceptClientMessage_NonJSON(t *testing.T) {
	t.Parallel()
	proxy := newTestProxy(testClaims("user456"), []string{"*.trade"}, nil, nil)

	// Non-JSON messages should pass through
	inputs := []string{
		"not json at all",
		"{invalid json",
		"",
		"12345",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			result, _ := proxy.interceptClientMessage([]byte(input))
			if string(result) != input {
				t.Errorf("Non-JSON should pass through unchanged.\nGot:  %s\nWant: %s", result, input)
			}
		})
	}
}

func TestProxy_InterceptClientMessage_NonSubscribe(t *testing.T) {
	t.Parallel()
	proxy := newTestProxy(testClaims("user123"), []string{"*.trade"}, nil, nil)

	// Non-subscribe messages should pass through
	messages := []string{
		`{"type":"ping"}`,
		`{"type":"pong"}`,
		`{"type":"publish","data":{"channel":"test","message":"hello"}}`,
		`{"type":"unsubscribe","data":{"channels":["BTC.trade"]}}`,
	}

	for _, input := range messages {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			result, _ := proxy.interceptClientMessage([]byte(input))
			if string(result) != input {
				t.Errorf("Non-subscribe should pass through unchanged.\nGot:  %s\nWant: %s", result, input)
			}
		})
	}
}

func TestProxy_InterceptClientMessage_AllAllowed(t *testing.T) {
	t.Parallel()
	proxy := newTestProxy(testClaims("user123"), []string{"*.trade", "*.liquidity"}, nil, nil)

	input := `{"type":"subscribe","data":{"channels":["BTC.trade","ETH.liquidity"]}}`
	result, err := proxy.interceptClientMessage([]byte(input))

	if err != nil {
		t.Fatalf("interceptClientMessage() error = %v", err)
	}

	// All channels allowed - original message preserved
	if string(result) != input {
		t.Errorf("All allowed should pass through unchanged.\nGot:  %s\nWant: %s", result, input)
	}
}

func TestProxy_InterceptClientMessage_SomeFiltered(t *testing.T) {
	t.Parallel()
	proxy := newTestProxy(testClaims("user123"), []string{"*.trade"}, nil, nil)

	input := `{"type":"subscribe","data":{"channels":["BTC.trade","secret.channel","ETH.trade"]}}`
	result, err := proxy.interceptClientMessage([]byte(input))

	if err != nil {
		t.Fatalf("interceptClientMessage() error = %v", err)
	}

	// Parse result to verify filtering
	var msg ClientMessage
	if err := json.Unmarshal(result, &msg); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	var data SubscribeData
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		t.Fatalf("Failed to parse data: %v", err)
	}

	// Should have BTC.trade and ETH.trade, but not secret.channel
	expectedChannels := []string{"BTC.trade", "ETH.trade"}
	if len(data.Channels) != len(expectedChannels) {
		t.Errorf("Expected %d channels, got %d: %v", len(expectedChannels), len(data.Channels), data.Channels)
	}

	for i, ch := range expectedChannels {
		if i >= len(data.Channels) || data.Channels[i] != ch {
			t.Errorf("Channel[%d] = %q, want %q", i, data.Channels[i], ch)
		}
	}
}

func TestProxy_InterceptClientMessage_AllFiltered(t *testing.T) {
	t.Parallel()
	proxy := newTestProxy(testClaims("user123"), []string{"*.trade"}, nil, nil)

	input := `{"type":"subscribe","data":{"channels":["secret.channel","forbidden.data"]}}`
	result, err := proxy.interceptClientMessage([]byte(input))

	if err != nil {
		t.Fatalf("interceptClientMessage() error = %v", err)
	}

	// Parse result
	var msg ClientMessage
	if err := json.Unmarshal(result, &msg); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	var data SubscribeData
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		t.Fatalf("Failed to parse data: %v", err)
	}

	// Should have empty channels list
	if len(data.Channels) != 0 {
		t.Errorf("Expected 0 channels when all filtered, got %d: %v", len(data.Channels), data.Channels)
	}
}

func TestProxy_InterceptClientMessage_UserScoped(t *testing.T) {
	t.Parallel()
	proxy := newTestProxy(
		testClaims("user123"),
		[]string{"*.trade"},
		[]string{"balances.{principal}"},
		nil,
	)

	tests := []struct {
		name            string
		channels        []string
		expectedAllowed []string
	}{
		{
			name:            "own balance allowed",
			channels:        []string{"balances.user123"},
			expectedAllowed: []string{"balances.user123"},
		},
		{
			name:            "other user balance denied",
			channels:        []string{"balances.user456"},
			expectedAllowed: []string{},
		},
		{
			name:            "mixed - own allowed, other denied",
			channels:        []string{"balances.user123", "balances.user456"},
			expectedAllowed: []string{"balances.user123"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inputData := SubscribeData{Channels: tt.channels}
			dataBytes, _ := json.Marshal(inputData)
			inputMsg := ClientMessage{Type: "subscribe", Data: dataBytes}
			input, _ := json.Marshal(inputMsg)

			result, err := proxy.interceptClientMessage(input)
			if err != nil {
				t.Fatalf("interceptClientMessage() error = %v", err)
			}

			var msg ClientMessage
			if err := json.Unmarshal(result, &msg); err != nil {
				t.Fatalf("Failed to parse result: %v", err)
			}

			var data SubscribeData
			if err := json.Unmarshal(msg.Data, &data); err != nil {
				t.Fatalf("Failed to parse data: %v", err)
			}

			if len(data.Channels) != len(tt.expectedAllowed) {
				t.Errorf("Expected %d channels, got %d: %v", len(tt.expectedAllowed), len(data.Channels), data.Channels)
			}
		})
	}
}

func TestProxy_InterceptClientMessage_GroupScoped(t *testing.T) {
	t.Parallel()
	proxy := newTestProxy(
		testClaimsWithGroups("user123", []string{"vip", "traders"}),
		[]string{"*.trade"},
		nil,
		[]string{"community.{group_id}"},
	)

	tests := []struct {
		name            string
		channels        []string
		expectedAllowed []string
	}{
		{
			name:            "member group allowed",
			channels:        []string{"community.vip"},
			expectedAllowed: []string{"community.vip"},
		},
		{
			name:            "non-member group denied",
			channels:        []string{"community.whales"},
			expectedAllowed: []string{},
		},
		{
			name:            "mixed member and non-member",
			channels:        []string{"community.vip", "community.whales", "community.traders"},
			expectedAllowed: []string{"community.vip", "community.traders"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inputData := SubscribeData{Channels: tt.channels}
			dataBytes, _ := json.Marshal(inputData)
			inputMsg := ClientMessage{Type: "subscribe", Data: dataBytes}
			input, _ := json.Marshal(inputMsg)

			result, err := proxy.interceptClientMessage(input)
			if err != nil {
				t.Fatalf("interceptClientMessage() error = %v", err)
			}

			var msg ClientMessage
			if err := json.Unmarshal(result, &msg); err != nil {
				t.Fatalf("Failed to parse result: %v", err)
			}

			var data SubscribeData
			if err := json.Unmarshal(msg.Data, &data); err != nil {
				t.Fatalf("Failed to parse data: %v", err)
			}

			if len(data.Channels) != len(tt.expectedAllowed) {
				t.Errorf("Expected %d channels, got %d: %v", len(tt.expectedAllowed), len(data.Channels), data.Channels)
			}
		})
	}
}

func TestProxy_InterceptClientMessage_MalformedSubscribeData(t *testing.T) {
	t.Parallel()
	proxy := newTestProxy(testClaims("user123"), []string{"*.trade"}, nil, nil)

	// Valid subscribe type but malformed data
	input := `{"type":"subscribe","data":"not-an-object"}`
	result, _ := proxy.interceptClientMessage([]byte(input))

	// Should pass through unchanged on parse error
	if string(result) != input {
		t.Errorf("Malformed subscribe data should pass through.\nGot:  %s\nWant: %s", result, input)
	}
}

func TestContains(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		slice []string
		val   string
		want  bool
	}{
		{"found at start", []string{"a", "b", "c"}, "a", true},
		{"found at middle", []string{"a", "b", "c"}, "b", true},
		{"found at end", []string{"a", "b", "c"}, "c", true},
		{"not found", []string{"a", "b", "c"}, "d", false},
		{"empty slice", []string{}, "a", false},
		{"nil slice", nil, "a", false},
		{"empty string found", []string{"", "a"}, "", true},
		{"empty string not in slice", []string{"a", "b"}, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := contains(tt.slice, tt.val)
			if got != tt.want {
				t.Errorf("contains(%v, %q) = %v, want %v", tt.slice, tt.val, got, tt.want)
			}
		})
	}
}

// Benchmark for interceptClientMessage to ensure performance
func BenchmarkInterceptClientMessage_PassThrough(b *testing.B) {
	proxy := newTestProxy(testClaims("user123"), []string{"*.trade"}, nil, nil)
	input := []byte(`{"type":"subscribe","data":{"channels":["BTC.trade","ETH.trade"]}}`)

	for b.Loop() {
		_, _ = proxy.interceptClientMessage(input)
	}
}

func BenchmarkInterceptClientMessage_Filtered(b *testing.B) {
	proxy := newTestProxy(testClaims("user123"), []string{"*.trade"}, nil, nil)
	input := []byte(`{"type":"subscribe","data":{"channels":["BTC.trade","secret.channel","ETH.trade"]}}`)

	for b.Loop() {
		_, _ = proxy.interceptClientMessage(input)
	}
}
