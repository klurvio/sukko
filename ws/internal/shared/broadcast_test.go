package shared

import (
	"testing"
)

// =============================================================================
// extractChannel Tests
// =============================================================================

func TestExtractChannel_ValidSubjects(t *testing.T) {
	tests := []struct {
		name     string
		subject  string
		expected string
	}{
		{
			name:     "BTC trade",
			subject:  "odin.token.BTC.trade",
			expected: "BTC.trade",
		},
		{
			name:     "ETH liquidity",
			subject:  "odin.token.ETH.liquidity",
			expected: "ETH.liquidity",
		},
		{
			name:     "SOL metadata",
			subject:  "odin.token.SOL.metadata",
			expected: "SOL.metadata",
		},
		{
			name:     "DOGE social",
			subject:  "odin.token.DOGE.social",
			expected: "DOGE.social",
		},
		{
			name:     "SHIB favorites",
			subject:  "odin.token.SHIB.favorites",
			expected: "SHIB.favorites",
		},
		{
			name:     "PEPE creation",
			subject:  "odin.token.PEPE.creation",
			expected: "PEPE.creation",
		},
		{
			name:     "BONK analytics",
			subject:  "odin.token.BONK.analytics",
			expected: "BONK.analytics",
		},
		{
			name:     "WIF balances",
			subject:  "odin.token.WIF.balances",
			expected: "WIF.balances",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractChannel(tt.subject)
			if result != tt.expected {
				t.Errorf("extractChannel(%q) = %q, want %q", tt.subject, result, tt.expected)
			}
		})
	}
}

func TestExtractChannel_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		subject  string
		expected string
	}{
		{
			name:     "Extra parts (5 parts)",
			subject:  "odin.token.BTC.trade.extra",
			expected: "BTC.trade",
		},
		{
			name:     "Extra parts (6 parts)",
			subject:  "odin.token.ETH.liquidity.more.parts",
			expected: "ETH.liquidity",
		},
		{
			name:     "Long symbol",
			subject:  "odin.token.VERYLONGSYMBOLNAME.trade",
			expected: "VERYLONGSYMBOLNAME.trade",
		},
		{
			name:     "Lowercase symbol",
			subject:  "odin.token.btc.trade",
			expected: "btc.trade",
		},
		{
			name:     "Mixed case symbol",
			subject:  "odin.token.BtC.TrAdE",
			expected: "BtC.TrAdE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractChannel(tt.subject)
			if result != tt.expected {
				t.Errorf("extractChannel(%q) = %q, want %q", tt.subject, result, tt.expected)
			}
		})
	}
}

func TestExtractChannel_InvalidSubjects(t *testing.T) {
	tests := []struct {
		name    string
		subject string
	}{
		{
			name:    "Empty string",
			subject: "",
		},
		{
			name:    "Single part",
			subject: "odin",
		},
		{
			name:    "Two parts",
			subject: "odin.token",
		},
		{
			name:    "Three parts (no event type)",
			subject: "odin.token.BTC",
		},
		{
			name:    "No dots",
			subject: "nodots",
		},
		// Edge cases: subjects with 4+ parts but empty symbol or event type
		// These would return "." without validation, which is invalid
		{
			name:    "Only dots (empty parts)",
			subject: "...",
		},
		{
			name:    "Empty symbol",
			subject: "odin.token..trade",
		},
		{
			name:    "Empty event type",
			subject: "odin.token.BTC.",
		},
		{
			name:    "Empty symbol and event type",
			subject: "odin.token..",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractChannel(tt.subject)
			if result != "" {
				t.Errorf("extractChannel(%q) = %q, want empty string", tt.subject, result)
			}
		})
	}
}

func TestExtractChannel_AllEventTypes(t *testing.T) {
	// Test all 8 documented event types
	eventTypes := []string{
		"trade",
		"liquidity",
		"metadata",
		"social",
		"favorites",
		"creation",
		"analytics",
		"balances",
	}

	for _, eventType := range eventTypes {
		t.Run(eventType, func(t *testing.T) {
			subject := "odin.token.BTC." + eventType
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

func BenchmarkExtractChannel_Valid(b *testing.B) {
	subject := "odin.token.BTC.trade"
	for b.Loop() {
		_ = extractChannel(subject)
	}
}

func BenchmarkExtractChannel_Invalid(b *testing.B) {
	subject := "odin.token.BTC" // Missing event type
	for b.Loop() {
		_ = extractChannel(subject)
	}
}

func BenchmarkExtractChannel_LongSubject(b *testing.B) {
	subject := "odin.token.VERYLONGSYMBOLNAMEWITHMANYCHARACTERS.trade"
	for b.Loop() {
		_ = extractChannel(subject)
	}
}
