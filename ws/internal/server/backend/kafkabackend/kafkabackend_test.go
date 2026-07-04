package kafkabackend

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/twmb/franz-go/pkg/kerr"

	"github.com/klurvio/sukko/internal/server/backend"
)

// Compile-time interface check.
var _ backend.MessageBackend = (*KafkaBackend)(nil)

// =============================================================================
// SplitBrokers Tests
// =============================================================================

func TestSplitBrokers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "comma-separated brokers",
			input: "broker1:9092,broker2:9092,broker3:9092",
			want:  []string{"broker1:9092", "broker2:9092", "broker3:9092"},
		},
		{
			name:  "single broker",
			input: "broker1:9092",
			want:  []string{"broker1:9092"},
		},
		{
			name:  "empty string",
			input: "",
			want:  nil,
		},
		{
			name:  "whitespace only",
			input: "   ",
			want:  nil,
		},
		{
			name:  "brokers with whitespace",
			input: " broker1:9092 , broker2:9092 , broker3:9092 ",
			want:  []string{"broker1:9092", "broker2:9092", "broker3:9092"},
		},
		{
			name:  "trailing comma",
			input: "broker1:9092,broker2:9092,",
			want:  []string{"broker1:9092", "broker2:9092"},
		},
		{
			name:  "leading comma",
			input: ",broker1:9092",
			want:  []string{"broker1:9092"},
		},
		{
			name:  "multiple commas",
			input: "broker1:9092,,broker2:9092",
			want:  []string{"broker1:9092", "broker2:9092"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := SplitBrokers(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("SplitBrokers(%q) = %v (len %d), want %v (len %d)", tt.input, got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("SplitBrokers(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// =============================================================================
// isTopicAlreadyExistsError Tests
// =============================================================================

func TestIsTopicAlreadyExistsError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "topic already exists",
			err:  kerr.TopicAlreadyExists,
			want: true,
		},
		{
			name: "wrapped topic already exists",
			err:  fmt.Errorf("create topic failed: %w", kerr.TopicAlreadyExists),
			want: true,
		},
		{
			name: "other error",
			err:  kerr.UnknownServerError,
			want: false,
		},
		{
			name: "connection refused",
			err:  errors.New("connection refused"),
			want: false,
		},
		{
			name: "empty error message",
			err:  errors.New(""),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := isTopicAlreadyExistsError(tt.err)
			if got != tt.want {
				t.Errorf("isTopicAlreadyExistsError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// =============================================================================
// New — SASL/TLS passthrough Tests
// =============================================================================

func TestNew_SASLAndTLSPassthrough(t *testing.T) {
	t.Parallel()

	// An unsupported SASL mechanism must propagate as a config error, not a broker error.
	// Environment is set to produce a valid topic namespace and reach the SASL/TLS code path.
	// "oauthbearer" is a real SASL mechanism we deliberately do not support (supported:
	// plain, scram-sha-256, scram-sha-512), so it must hit the unsupported-mechanism path.
	_, err := New(Config{
		Brokers:       []string{"localhost:19092"},
		Environment:   "test",
		SASLEnabled:   true,
		SASLMechanism: "oauthbearer",
	})
	if err == nil {
		t.Fatal("expected error for unsupported SASL mechanism, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported SASL mechanism") {
		t.Fatalf("expected SASL mechanism error, got: %v", err)
	}

	// A missing CA file must propagate as a config error, not a broker error.
	_, err = New(Config{
		Brokers:     []string{"localhost:19092"},
		Environment: "test",
		TLSEnabled:  true,
		TLSCAPath:   "/nonexistent/ca.pem",
	})
	if err == nil {
		t.Fatal("expected error for missing CA file, got nil")
	}
	if !strings.Contains(err.Error(), "failed to read CA certificate") {
		t.Fatalf("expected CA certificate error, got: %v", err)
	}
}

// =============================================================================
// Publish Channel Validation Tests
// =============================================================================

func TestPublish_EmptyChannel(t *testing.T) {
	t.Parallel()

	// Construct a minimal KafkaBackend with nil producer.
	// The empty channel validation runs before producer delegation.
	kb := &KafkaBackend{}

	err := kb.Publish(context.Background(), 1, "test-tenant", "", []byte("data"))
	if err == nil {
		t.Fatal("expected error for empty channel, got nil")
	}
	if !errors.Is(err, backend.ErrPublishFailed) {
		t.Errorf("error = %v, want wrapping %v", err, backend.ErrPublishFailed)
	}
}

func TestPublish_NilProducer(t *testing.T) {
	t.Parallel()

	// Producer is nil but channel is valid — should hit the producer nil check.
	kb := &KafkaBackend{}

	err := kb.Publish(context.Background(), 1, "test-tenant", "test.channel", []byte("data"))
	if err == nil {
		t.Fatal("expected error for nil producer, got nil")
	}
	if !errors.Is(err, backend.ErrPublishFailed) {
		t.Errorf("error = %v, want wrapping %v", err, backend.ErrPublishFailed)
	}
}

// =============================================================================
// IsHealthy Tests
// =============================================================================

func TestIsHealthy_Default(t *testing.T) {
	t.Parallel()

	kb := &KafkaBackend{}
	if kb.IsHealthy() {
		t.Error("expected false for zero-value KafkaBackend, got true")
	}
}

// =============================================================================
// Test Certificate
// =============================================================================
