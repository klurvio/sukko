package kafka

import (
	"testing"
)

// =============================================================================
// ResolveNamespace Tests
// =============================================================================

func TestResolveNamespace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		override    string
		environment string
		expected    string
	}{
		{"override_wins", "prod", "dev", "prod"},
		{"fallback_to_env", "", "dev", "dev"},
		{"both_empty", "", "", ""},
		{"override_normalized", " PROD ", "dev", "prod"},
		{"env_normalized", "", " DEV ", "dev"},
		{"override_empty_string", "", "stg", "stg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ResolveNamespace(tt.override, tt.environment)
			if result != tt.expected {
				t.Errorf("ResolveNamespace(%q, %q) = %q, want %q",
					tt.override, tt.environment, result, tt.expected)
			}
		})
	}
}

// =============================================================================
// TopicToEventType Tests
// =============================================================================

func TestTopicToEventType_NewFormat(t *testing.T) {
	t.Parallel()
	// New format: {namespace}.{tenant}.{category}
	tests := []struct {
		topic    string
		expected string
	}{
		{"prod.odin.trade", "trade"},
		{"dev.odin.liquidity", "liquidity"},
		{"stag.acme.metadata", "metadata"},
		{"local.tenant1.social", "social"},
		{"prod.odin.community", "community"},
		{"dev.acme.creation", "creation"},
		{"stag.tenant2.analytics", "analytics"},
		{"prod.odin.balances", "balances"},
	}

	for _, tt := range tests {
		t.Run(tt.topic, func(t *testing.T) {
			t.Parallel()
			result := TopicToEventType(tt.topic)
			if result != tt.expected {
				t.Errorf("TopicToEventType(%q) = %q, want %q", tt.topic, result, tt.expected)
			}
		})
	}
}

func TestTopicToEventType_LegacyFormat(t *testing.T) {
	t.Parallel()
	// Legacy format: odin.{env}.{category}
	tests := []struct {
		topic    string
		expected string
	}{
		{"odin.dev.trade", "trade"},
		{"odin.local.liquidity", "liquidity"},
		{"odin.staging.metadata", "metadata"},
		{"odin.prod.social", "social"},
		{"odin.dev.community", "community"},
		{"odin.local.creation", "creation"},
		{"odin.staging.analytics", "analytics"},
		{"odin.prod.balances", "balances"},
	}

	for _, tt := range tests {
		t.Run(tt.topic, func(t *testing.T) {
			t.Parallel()
			result := TopicToEventType(tt.topic)
			if result != tt.expected {
				t.Errorf("TopicToEventType(%q) = %q, want %q", tt.topic, result, tt.expected)
			}
		})
	}
}

func TestTopicToEventType_OldLegacyFormat(t *testing.T) {
	t.Parallel()
	// Very old legacy format: odin.{category}
	tests := []struct {
		topic    string
		expected string
	}{
		{"odin.trades", "trades"},
		{"odin.liquidity", "liquidity"},
	}

	for _, tt := range tests {
		t.Run(tt.topic, func(t *testing.T) {
			t.Parallel()
			result := TopicToEventType(tt.topic)
			if result != tt.expected {
				t.Errorf("TopicToEventType(%q) = %q, want %q", tt.topic, result, tt.expected)
			}
		})
	}
}

// =============================================================================
// EventType Constants Tests
// =============================================================================

func TestEventTypeConstants(t *testing.T) {
	t.Parallel()
	// Trading events
	if EventTradeExecuted != "TRADE_EXECUTED" {
		t.Errorf("EventTradeExecuted = %q, want TRADE_EXECUTED", EventTradeExecuted)
	}
	if EventBuyCompleted != "BUY_COMPLETED" {
		t.Errorf("EventBuyCompleted = %q, want BUY_COMPLETED", EventBuyCompleted)
	}
	if EventSellCompleted != "SELL_COMPLETED" {
		t.Errorf("EventSellCompleted = %q, want SELL_COMPLETED", EventSellCompleted)
	}

	// Liquidity events
	if EventLiquidityAdded != "LIQUIDITY_ADDED" {
		t.Errorf("EventLiquidityAdded = %q, want LIQUIDITY_ADDED", EventLiquidityAdded)
	}

	// Creation events
	if EventTokenCreated != "TOKEN_CREATED" {
		t.Errorf("EventTokenCreated = %q, want TOKEN_CREATED", EventTokenCreated)
	}
}
