package server

import (
	"testing"
)

// =============================================================================
// extractChannel Tests
// =============================================================================

func TestExtractChannel_ValidSubjects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		subject  string
		expected string
	}{
		{
			name:     "BTC trade (2 parts)",
			subject:  "BTC.trade",
			expected: "BTC.trade",
		},
		{
			name:     "ETH liquidity (2 parts)",
			subject:  "ETH.liquidity",
			expected: "ETH.liquidity",
		},
		{
			name:     "SOL metadata (2 parts)",
			subject:  "SOL.metadata",
			expected: "SOL.metadata",
		},
		{
			name:     "User-scoped trade (3 parts)",
			subject:  "BTC.trade.user123",
			expected: "BTC.trade.user123",
		},
		{
			name:     "User-scoped balances (3 parts)",
			subject:  "ETH.balances.user456",
			expected: "ETH.balances.user456",
		},
		{
			name:     "Group-scoped community (3 parts)",
			subject:  "SOL.community.group789",
			expected: "SOL.community.group789",
		},
		{
			name:     "Simple balances (2 parts)",
			subject:  "balances.user123",
			expected: "balances.user123",
		},
		{
			name:     "Simple community (2 parts)",
			subject:  "community.group456",
			expected: "community.group456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := extractChannel(tt.subject)
			if result != tt.expected {
				t.Errorf("extractChannel(%q) = %q, want %q", tt.subject, result, tt.expected)
			}
		})
	}
}

func TestExtractChannel_EdgeCases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		subject  string
		expected string
	}{
		{
			name:     "Long symbol",
			subject:  "VERYLONGSYMBOLNAME.trade",
			expected: "VERYLONGSYMBOLNAME.trade",
		},
		{
			name:     "Lowercase symbol",
			subject:  "btc.trade",
			expected: "btc.trade",
		},
		{
			name:     "Mixed case symbol",
			subject:  "BtC.TrAdE",
			expected: "BtC.TrAdE",
		},
		{
			name:     "4 parts",
			subject:  "a.b.c.d",
			expected: "a.b.c.d",
		},
		{
			name:     "5 parts",
			subject:  "a.b.c.d.e",
			expected: "a.b.c.d.e",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := extractChannel(tt.subject)
			if result != tt.expected {
				t.Errorf("extractChannel(%q) = %q, want %q", tt.subject, result, tt.expected)
			}
		})
	}
}

func TestExtractChannel_InvalidSubjects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		subject string
	}{
		{
			name:    "Empty string",
			subject: "",
		},
		{
			name:    "Single part (no dots)",
			subject: "odin",
		},
		{
			name:    "No dots",
			subject: "nodots",
		},
		{
			name:    "Only dots (empty parts)",
			subject: "...",
		},
		{
			name:    "Empty first part",
			subject: ".trade",
		},
		{
			name:    "Empty second part",
			subject: "BTC.",
		},
		{
			name:    "Empty middle part",
			subject: "BTC..user123",
		},
		{
			name:    "All empty parts",
			subject: "..",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := extractChannel(tt.subject)
			if result != "" {
				t.Errorf("extractChannel(%q) = %q, want empty string", tt.subject, result)
			}
		})
	}
}

func TestExtractChannel_AllEventTypes(t *testing.T) {
	t.Parallel()
	// Test all documented event types
	eventTypes := []string{
		"trade",
		"liquidity",
		"metadata",
		"social",
		"favorites",
		"creation",
		"analytics",
		"balances",
		"community",
	}

	for _, eventType := range eventTypes {
		t.Run(eventType, func(t *testing.T) {
			t.Parallel()
			subject := "BTC." + eventType
			expected := "BTC." + eventType

			result := extractChannel(subject)
			if result != expected {
				t.Errorf("extractChannel(%q) = %q, want %q", subject, result, expected)
			}
		})
	}
}

// =============================================================================
// Benchmark Tests
// =============================================================================

func BenchmarkExtractChannel_TwoParts(b *testing.B) {
	subject := "BTC.trade"
	for b.Loop() {
		_ = extractChannel(subject)
	}
}

func BenchmarkExtractChannel_ThreeParts(b *testing.B) {
	subject := "BTC.trade.user123"
	for b.Loop() {
		_ = extractChannel(subject)
	}
}

func BenchmarkExtractChannel_Invalid(b *testing.B) {
	subject := "nodots"
	for b.Loop() {
		_ = extractChannel(subject)
	}
}

func BenchmarkExtractChannel_LongSubject(b *testing.B) {
	subject := "VERYLONGSYMBOLNAMEWITHMANYCHARACTERS.trade"
	for b.Loop() {
		_ = extractChannel(subject)
	}
}
