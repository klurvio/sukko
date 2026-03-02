package kafkabackend

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/twmb/franz-go/pkg/kerr"

	"github.com/Toniq-Labs/odin-ws/internal/server/backend"
	"github.com/Toniq-Labs/odin-ws/internal/shared/kafka"
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
// buildKgoOpts Tests
// =============================================================================

func TestBuildKgoOpts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		brokers []string
		sasl    *kafka.SASLConfig
		tls     *kafka.TLSConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "brokers only",
			brokers: []string{"broker1:9092", "broker2:9092"},
			sasl:    nil,
			tls:     nil,
			wantErr: false,
		},
		{
			name:    "with SASL scram-sha-256",
			brokers: []string{"broker1:9092"},
			sasl: &kafka.SASLConfig{
				Mechanism: "scram-sha-256",
				Username:  "user",
				Password:  "pass",
			},
			tls:     nil,
			wantErr: false,
		},
		{
			name:    "with SASL scram-sha-512",
			brokers: []string{"broker1:9092"},
			sasl: &kafka.SASLConfig{
				Mechanism: "scram-sha-512",
				Username:  "user",
				Password:  "pass",
			},
			tls:     nil,
			wantErr: false,
		},
		{
			name:    "unsupported SASL mechanism",
			brokers: []string{"broker1:9092"},
			sasl: &kafka.SASLConfig{
				Mechanism: "plain",
				Username:  "user",
				Password:  "pass",
			},
			tls:     nil,
			wantErr: true,
			errMsg:  "unsupported SASL mechanism",
		},
		{
			name:    "TLS enabled without CA",
			brokers: []string{"broker1:9092"},
			sasl:    nil,
			tls: &kafka.TLSConfig{
				Enabled:            true,
				InsecureSkipVerify: false,
			},
			wantErr: false,
		},
		{
			name:    "TLS with missing CA file",
			brokers: []string{"broker1:9092"},
			sasl:    nil,
			tls: &kafka.TLSConfig{
				Enabled: true,
				CAPath:  "/nonexistent/ca.pem",
			},
			wantErr: true,
			errMsg:  "failed to read CA certificate",
		},
		{
			name:    "TLS disabled (no-op)",
			brokers: []string{"broker1:9092"},
			sasl:    nil,
			tls: &kafka.TLSConfig{
				Enabled: false,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts, err := buildKgoOpts(tt.brokers, tt.sasl, tt.tls)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error = %q, want containing %q", err.Error(), tt.errMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(opts) == 0 {
				t.Fatal("expected at least one option (seed brokers)")
			}
		})
	}
}

func TestBuildKgoOpts_TLSInvalidPEM(t *testing.T) {
	t.Parallel()

	tmpFile := t.TempDir() + "/invalid-ca.pem"
	if err := os.WriteFile(tmpFile, []byte("not-a-valid-pem"), 0o600); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	_, err := buildKgoOpts(
		[]string{"broker1:9092"},
		nil,
		&kafka.TLSConfig{
			Enabled: true,
			CAPath:  tmpFile,
		},
	)
	if err == nil {
		t.Fatal("expected error for invalid PEM, got nil")
	}
	if !strings.Contains(err.Error(), "failed to parse CA certificate") {
		t.Errorf("error = %q, want containing 'failed to parse CA certificate'", err.Error())
	}
}

func TestBuildKgoOpts_TLSValidPEM(t *testing.T) {
	t.Parallel()

	tmpFile := t.TempDir() + "/valid-ca.pem"
	if err := os.WriteFile(tmpFile, testCACert, 0o600); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	opts, err := buildKgoOpts(
		[]string{"broker1:9092"},
		nil,
		&kafka.TLSConfig{
			Enabled: true,
			CAPath:  tmpFile,
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Expect at least 2 opts: seed brokers + TLS dial config
	if len(opts) < 2 {
		t.Errorf("expected at least 2 options (seed brokers + TLS), got %d", len(opts))
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

	err := kb.Publish(context.Background(), 1, "", []byte("data"))
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

	err := kb.Publish(context.Background(), 1, "test.channel", []byte("data"))
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

// testCACert is a self-signed test certificate for TLS tests.
var testCACert = []byte(`-----BEGIN CERTIFICATE-----
MIIDATCCAemgAwIBAgIUPwru35BdNF7VD/F7jZJDg5LsMdwwDQYJKoZIhvcNAQEL
BQAwDzENMAsGA1UEAwwEdGVzdDAgFw0yNjAzMDIwMzQ3NTBaGA8yMTI2MDIwNjAz
NDc1MFowDzENMAsGA1UEAwwEdGVzdDCCASIwDQYJKoZIhvcNAQEBBQADggEPADCC
AQoCggEBAJT8PxfwA7IljdLiWW+cRfY46ZtEdj7F/DuPUeyex1qGwgG7JI1GX7Yq
HeJDitzidLeYuh344B6u/FOz0hLlao7F6TfEh+5s64oq5PMbERSUB4gnAeMxoiNn
CwnsStJkow63vO5fROt/iKom4HME9bwQKX6yQ15bVEuVKp1jQUl+4fTwC0zEidWy
DS03SY+shxReR7hCzPJn08wff0fQh5/eDC7Fm3FNni7pziDMRz29V1oiTLC0gpf2
MihX1xJOa8UwHDCibhT0ul9CigZUilsw6g+sxpZxEh62/j25KhaM+OrkHdHQqSWJ
Z8ENo9X549GWslaDf7X+F7MLHXO8FlUCAwEAAaNTMFEwHQYDVR0OBBYEFHVLs+b8
CRpT0PfQnvytJg/pxUCbMB8GA1UdIwQYMBaAFHVLs+b8CRpT0PfQnvytJg/pxUCb
MA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZIhvcNAQELBQADggEBAJGllt8UelJhQ5K8
UznrDPvGU5qN/sGU31wT9yRpkVK2xoJg0Ut85YdXxzIvregZ6mN5dTggn71Gcgfv
L5szyuG9Zay0FS2cpEfCtE/tx8l8TfFfXPjdV7ZAxxvsXzFn0VtXmQB6PWG6NZ5w
hN40woLC9YRcKub6mwsczaiHzgGOUfJnAfQZJkZ0PuMxYPVMsVm2lmkvpPTpjS+5
4Ja1bi84dbyvYh14LFGwuxv1HM5o3TuvmqQ27M628cfHi53hWZiEsd9QiMtYk+mT
MdjXH9epWrYcjDUAb3mVWWP0CR3yiQx3yKQX9V+CZaEbeBRBmzDGnJhKowkeqOHp
Y8uBy4Q=
-----END CERTIFICATE-----`)
