package main

import (
	"testing"

	"github.com/klurvio/sukko/internal/shared/license"
)

func TestCheckBackendMismatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		backend string
		edition license.Edition
		want    bool
	}{
		// Kafka backend — requires Pro+
		{name: "kafka+community", backend: "kafka", edition: license.Community, want: true},
		{name: "kafka+pro", backend: "kafka", edition: license.Pro, want: false},
		{name: "kafka+enterprise", backend: "kafka", edition: license.Enterprise, want: false},

		// NATS JetStream backend — requires Pro+
		{name: "nats+community", backend: "nats", edition: license.Community, want: true},
		{name: "nats+pro", backend: "nats", edition: license.Pro, want: false},
		{name: "nats+enterprise", backend: "nats", edition: license.Enterprise, want: false},

		// Direct backend — always compatible
		{name: "direct+community", backend: "direct", edition: license.Community, want: false},
		{name: "direct+pro", backend: "direct", edition: license.Pro, want: false},
		{name: "direct+enterprise", backend: "direct", edition: license.Enterprise, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := checkBackendMismatch(tt.backend, tt.edition); got != tt.want {
				t.Errorf("checkBackendMismatch(%q, %q) = %v, want %v", tt.backend, tt.edition, got, tt.want)
			}
		})
	}
}
