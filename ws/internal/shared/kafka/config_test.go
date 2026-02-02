package kafka

import (
	"testing"
)

// Test environment for consistent topic naming
const configTestEnv = "test"

// =============================================================================
// Topic Base Constants Tests
// =============================================================================

func TestTopicBaseConstants(t *testing.T) {
	t.Parallel()
	// Verify all topic base constants have expected values
	tests := []struct {
		name     string
		base     string
		expected string
	}{
		{"TopicBaseTrade", TopicBaseTrade, "trade"},
		{"TopicBaseLiquidity", TopicBaseLiquidity, "liquidity"},
		{"TopicBaseMetadata", TopicBaseMetadata, "metadata"},
		{"TopicBaseSocial", TopicBaseSocial, "social"},
		{"TopicBaseCommunity", TopicBaseCommunity, "community"},
		{"TopicBaseCreation", TopicBaseCreation, "creation"},
		{"TopicBaseAnalytics", TopicBaseAnalytics, "analytics"},
		{"TopicBaseBalances", TopicBaseBalances, "balances"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.base != tt.expected {
				t.Errorf("%s = %q, want %q", tt.name, tt.base, tt.expected)
			}
		})
	}
}

// =============================================================================
// NormalizeEnv Tests
// =============================================================================

func TestNormalizeEnv(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    string
		expected string
	}{
		// Pass-through behavior (no mapping, just normalize)
		{"local", "local"},
		{"dev", "dev"},
		{"stag", "stag"},
		{"staging", "staging"}, // pass-through (no mapping)
		{"prod", "prod"},
		{"custom", "custom"},
		{"main", "main"},

		// Empty string defaults to local
		{"", "local"},

		// Whitespace trimming
		{"  dev  ", "dev"},
		{"  prod  ", "prod"},

		// Lowercase conversion
		{"DEV", "dev"},
		{"PROD", "prod"},
		{"LOCAL", "local"},
		{"STAG", "stag"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			result := NormalizeEnv(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeEnv(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// =============================================================================
// GetTopic Tests
// =============================================================================

func TestGetTopic(t *testing.T) {
	t.Parallel()
	tests := []struct {
		env      string
		base     string
		expected string
	}{
		// Standard namespaces (configured via Helm)
		{"local", TopicBaseTrade, "odin.local.trade"},
		{"dev", TopicBaseLiquidity, "odin.dev.liquidity"},
		{"stag", TopicBaseMetadata, "odin.stag.metadata"},
		{"staging", TopicBaseSocial, "odin.staging.social"}, // pass-through
		{"prod", TopicBaseBalances, "odin.prod.balances"},

		// Custom/pass-through namespaces
		{"main", TopicBaseBalances, "odin.main.balances"},
		{"custom", TopicBaseTrade, "odin.custom.trade"},

		// Empty defaults to local
		{"", TopicBaseTrade, "odin.local.trade"},

		// Case normalization
		{"PROD", TopicBaseTrade, "odin.prod.trade"},
		{"DEV", TopicBaseLiquidity, "odin.dev.liquidity"},
	}

	for _, tt := range tests {
		t.Run(tt.env+"_"+tt.base, func(t *testing.T) {
			t.Parallel()
			result := GetTopic(tt.env, tt.base)
			if result != tt.expected {
				t.Errorf("GetTopic(%q, %q) = %q, want %q", tt.env, tt.base, result, tt.expected)
			}
		})
	}
}

// =============================================================================
// AllTopicBases Tests
// =============================================================================

func TestAllTopicBases_ReturnsAllBases(t *testing.T) {
	t.Parallel()
	bases := AllTopicBases()

	if len(bases) != 8 {
		t.Errorf("AllTopicBases() returned %d bases, want 8", len(bases))
	}
}

func TestAllTopicBases_ContainsExpectedBases(t *testing.T) {
	t.Parallel()
	bases := AllTopicBases()
	baseSet := make(map[string]bool)
	for _, base := range bases {
		baseSet[base] = true
	}

	expectedBases := []string{
		TopicBaseTrade,
		TopicBaseLiquidity,
		TopicBaseMetadata,
		TopicBaseSocial,
		TopicBaseCommunity,
		TopicBaseCreation,
		TopicBaseAnalytics,
		TopicBaseBalances,
	}

	for _, expected := range expectedBases {
		if !baseSet[expected] {
			t.Errorf("AllTopicBases() missing %s", expected)
		}
	}
}

// =============================================================================
// AllTopics Tests
// =============================================================================

func TestAllTopics_ReturnsAllTopics(t *testing.T) {
	t.Parallel()
	topics := AllTopics(configTestEnv)

	if len(topics) != 8 {
		t.Errorf("AllTopics() returned %d topics, want 8", len(topics))
	}
}

func TestAllTopics_ContainsExpectedTopics(t *testing.T) {
	t.Parallel()
	topics := AllTopics(configTestEnv)
	topicSet := make(map[string]bool)
	for _, topic := range topics {
		topicSet[topic] = true
	}

	expectedTopics := []string{
		GetTopic(configTestEnv, TopicBaseTrade),
		GetTopic(configTestEnv, TopicBaseLiquidity),
		GetTopic(configTestEnv, TopicBaseMetadata),
		GetTopic(configTestEnv, TopicBaseSocial),
		GetTopic(configTestEnv, TopicBaseCommunity),
		GetTopic(configTestEnv, TopicBaseCreation),
		GetTopic(configTestEnv, TopicBaseAnalytics),
		GetTopic(configTestEnv, TopicBaseBalances),
	}

	for _, expected := range expectedTopics {
		if !topicSet[expected] {
			t.Errorf("AllTopics() missing %s", expected)
		}
	}
}

// =============================================================================
// TopicToEventType Tests
// =============================================================================

func TestTopicToEventType_RegularTopics(t *testing.T) {
	t.Parallel()
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

func TestTopicToEventType_LegacyTopics(t *testing.T) {
	t.Parallel()
	// Legacy topic names without environment prefix
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

// =============================================================================
// EventTypeToTopicBase Tests
// =============================================================================

func TestEventTypeToTopicBase(t *testing.T) {
	t.Parallel()
	tests := []struct {
		eventType EventType
		expected  string
	}{
		{EventTradeExecuted, TopicBaseTrade},
		{EventBuyCompleted, TopicBaseTrade},
		{EventSellCompleted, TopicBaseTrade},
		{EventLiquidityAdded, TopicBaseLiquidity},
		{EventLiquidityRemoved, TopicBaseLiquidity},
		{EventMetadataUpdated, TopicBaseMetadata},
		{EventTwitterVerified, TopicBaseSocial},
		{EventCommentPosted, TopicBaseCommunity},
		{EventTokenCreated, TopicBaseCreation},
		{EventPriceDeltaUpdated, TopicBaseAnalytics},
		{EventBalanceUpdated, TopicBaseBalances},
	}

	for _, tt := range tests {
		t.Run(string(tt.eventType), func(t *testing.T) {
			t.Parallel()
			result := EventTypeToTopicBase(tt.eventType)
			if result != tt.expected {
				t.Errorf("EventTypeToTopicBase(%q) = %q, want %q", tt.eventType, result, tt.expected)
			}
		})
	}
}

func TestEventTypeToTopicBase_Unknown(t *testing.T) {
	t.Parallel()
	// Unknown event types should default to trade topic
	result := EventTypeToTopicBase(EventType("UNKNOWN_EVENT"))
	if result != TopicBaseTrade {
		t.Errorf("EventTypeToTopicBase(UNKNOWN) = %q, want %q", result, TopicBaseTrade)
	}
}
