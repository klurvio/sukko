package jetstreambackend

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/klurvio/sukko/internal/server/backend"
)

// Compile-time interface check.
var _ backend.MessageBackend = (*JetStreamBackend)(nil)

// =============================================================================
// SplitURLs Tests
// =============================================================================

func TestSplitURLs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "comma-separated URLs",
			input: "nats://host1:4222,nats://host2:4222,nats://host3:4222",
			want:  []string{"nats://host1:4222", "nats://host2:4222", "nats://host3:4222"},
		},
		{
			name:  "single URL",
			input: "nats://localhost:4222",
			want:  []string{"nats://localhost:4222"},
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
			name:  "URLs with whitespace",
			input: " nats://host1:4222 , nats://host2:4222 ",
			want:  []string{"nats://host1:4222", "nats://host2:4222"},
		},
		{
			name:  "trailing comma",
			input: "nats://host1:4222,nats://host2:4222,",
			want:  []string{"nats://host1:4222", "nats://host2:4222"},
		},
		{
			name:  "leading comma",
			input: ",nats://host1:4222",
			want:  []string{"nats://host1:4222"},
		},
		{
			name:  "multiple commas",
			input: "nats://host1:4222,,nats://host2:4222",
			want:  []string{"nats://host1:4222", "nats://host2:4222"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := SplitURLs(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("SplitURLs(%q) = %v (len %d), want %v (len %d)", tt.input, got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("SplitURLs(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// =============================================================================
// streamName Tests
// =============================================================================

func TestStreamName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		namespace string
		tenantID  string
		want      string
	}{
		{
			name:      "simple namespace and tenant",
			namespace: "dev",
			tenantID:  "acme",
			want:      "SUKKO_DEV_ACME",
		},
		{
			name:      "hyphenated namespace",
			namespace: "my-namespace",
			tenantID:  "tenant1",
			want:      "SUKKO_MY_NAMESPACE_TENANT1",
		},
		{
			name:      "hyphenated tenant",
			namespace: "prod",
			tenantID:  "my-tenant",
			want:      "SUKKO_PROD_MY_TENANT",
		},
		{
			name:      "both hyphenated",
			namespace: "ns-one",
			tenantID:  "tenant-two",
			want:      "SUKKO_NS_ONE_TENANT_TWO",
		},
		{
			name:      "already uppercase",
			namespace: "PROD",
			tenantID:  "ACME",
			want:      "SUKKO_PROD_ACME",
		},
		{
			name:      "mixed case",
			namespace: "Dev",
			tenantID:  "Acme",
			want:      "SUKKO_DEV_ACME",
		},
		{
			name:      "empty namespace",
			namespace: "",
			tenantID:  "acme",
			want:      "SUKKO__ACME",
		},
		{
			name:      "empty tenant",
			namespace: "dev",
			tenantID:  "",
			want:      "SUKKO_DEV_",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			jsb := &JetStreamBackend{
				namespace: tt.namespace,
			}
			got := jsb.streamName(tt.tenantID)
			if got != tt.want {
				t.Errorf("streamName(%q) = %q, want %q", tt.tenantID, got, tt.want)
			}
		})
	}
}

// =============================================================================
// Constants Validation Tests
// =============================================================================

func TestConstants_Defaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		got   time.Duration
		check func(time.Duration) bool
		desc  string
	}{
		{
			name:  "defaultMaxAge is 24h",
			got:   defaultMaxAge,
			check: func(d time.Duration) bool { return d == 24*time.Hour },
			desc:  "24h",
		},
		{
			name:  "defaultReconnectWait is positive",
			got:   defaultReconnectWait,
			check: func(d time.Duration) bool { return d > 0 },
			desc:  "> 0",
		},
		{
			name:  "defaultAckWait is positive",
			got:   defaultAckWait,
			check: func(d time.Duration) bool { return d > 0 },
			desc:  "> 0",
		},
		{
			name:  "defaultRefreshInterval is positive",
			got:   defaultRefreshInterval,
			check: func(d time.Duration) bool { return d > 0 },
			desc:  "> 0",
		},
		{
			name:  "defaultReplayFetchWait is positive",
			got:   defaultReplayFetchWait,
			check: func(d time.Duration) bool { return d > 0 },
			desc:  "> 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if !tt.check(tt.got) {
				t.Errorf("%s = %v, want %s", tt.name, tt.got, tt.desc)
			}
		})
	}
}

func TestConstants_MaxDeliver(t *testing.T) {
	t.Parallel()

	if defaultMaxDeliver < 1 {
		t.Errorf("defaultMaxDeliver = %d, want >= 1", defaultMaxDeliver)
	}
}

// =============================================================================
// Publish Channel Validation Tests
// =============================================================================

func TestPublish_EmptyChannel(t *testing.T) {
	t.Parallel()

	// Construct a minimal JetStreamBackend with nil js.
	// The channel validation runs before js.Publish().
	jsb := &JetStreamBackend{}

	err := jsb.Publish(context.Background(), 1, "", []byte("data"))
	if err == nil {
		t.Fatal("expected error for empty channel, got nil")
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

	jsb := &JetStreamBackend{}
	// Zero-value: healthy is false, conn is nil → IsHealthy should be false
	if jsb.IsHealthy() {
		t.Error("expected false for zero-value JetStreamBackend, got true")
	}
}
