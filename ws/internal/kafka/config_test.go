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
	tests := []struct {
		input    string
		expected string
	}{
		{"development", "local"},
		{"local", "local"},
		{"", "local"},
		{"develop", "dev"},
		{"dev", "dev"},
		{"staging", "staging"},
		{"stage", "staging"},
		{"production", "prod"},
		{"prod", "prod"},
		{"custom", "custom"},
		{"  dev  ", "dev"}, // with whitespace
		{"DEV", "dev"},     // uppercase
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
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
	tests := []struct {
		env      string
		base     string
		expected string
	}{
		{"local", TopicBaseTrade, "odin.local.trade"},
		{"dev", TopicBaseLiquidity, "odin.dev.liquidity"},
		{"staging", TopicBaseMetadata, "odin.staging.metadata"},
		{"prod", TopicBaseBalances, "odin.prod.balances"},
		{"development", TopicBaseTrade, "odin.local.trade"}, // normalizes to local
		{"production", TopicBaseTrade, "odin.prod.trade"},   // normalizes to prod
	}

	for _, tt := range tests {
		t.Run(tt.env+"_"+tt.base, func(t *testing.T) {
			result := GetTopic(tt.env, tt.base)
			if result != tt.expected {
				t.Errorf("GetTopic(%q, %q) = %q, want %q", tt.env, tt.base, result, tt.expected)
			}
		})
	}
}

// =============================================================================
// GetRefinedTopic Tests
// =============================================================================

func TestGetRefinedTopic(t *testing.T) {
	tests := []struct {
		env      string
		base     string
		expected string
	}{
		{"local", TopicBaseTrade, "odin.local.trade.refined"},
		{"dev", TopicBaseLiquidity, "odin.dev.liquidity.refined"},
		{"staging", TopicBaseMetadata, "odin.staging.metadata.refined"},
		{"prod", TopicBaseBalances, "odin.prod.balances.refined"},
	}

	for _, tt := range tests {
		t.Run(tt.env+"_"+tt.base, func(t *testing.T) {
			result := GetRefinedTopic(tt.env, tt.base)
			if result != tt.expected {
				t.Errorf("GetRefinedTopic(%q, %q) = %q, want %q", tt.env, tt.base, result, tt.expected)
			}
		})
	}
}

// =============================================================================
// AllTopicBases Tests
// =============================================================================

func TestAllTopicBases_ReturnsAllBases(t *testing.T) {
	bases := AllTopicBases()

	if len(bases) != 8 {
		t.Errorf("AllTopicBases() returned %d bases, want 8", len(bases))
	}
}

func TestAllTopicBases_ContainsExpectedBases(t *testing.T) {
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
	topics := AllTopics(configTestEnv)

	if len(topics) != 8 {
		t.Errorf("AllTopics() returned %d topics, want 8", len(topics))
	}
}

func TestAllTopics_ContainsExpectedTopics(t *testing.T) {
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
// AllRefinedTopics Tests
// =============================================================================

func TestAllRefinedTopics_ReturnsAllRefinedTopics(t *testing.T) {
	topics := AllRefinedTopics(configTestEnv)

	if len(topics) != 8 {
		t.Errorf("AllRefinedTopics() returned %d topics, want 8", len(topics))
	}
}

func TestAllRefinedTopics_ContainsRefinedSuffix(t *testing.T) {
	topics := AllRefinedTopics(configTestEnv)

	for _, topic := range topics {
		if len(topic) < 8 || topic[len(topic)-8:] != ".refined" {
			t.Errorf("AllRefinedTopics() topic %s does not have .refined suffix", topic)
		}
	}
}

// =============================================================================
// AllTopicsWithRefined Tests
// =============================================================================

func TestAllTopicsWithRefined_ReturnsDoubleTopics(t *testing.T) {
	topics := AllTopicsWithRefined(configTestEnv)

	// Should return 2x the number of base topics (regular + refined)
	if len(topics) != 16 {
		t.Errorf("AllTopicsWithRefined() returned %d topics, want 16", len(topics))
	}
}

// =============================================================================
// TopicToEventType Tests
// =============================================================================

func TestTopicToEventType_RegularTopics(t *testing.T) {
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
			result := TopicToEventType(tt.topic)
			if result != tt.expected {
				t.Errorf("TopicToEventType(%q) = %q, want %q", tt.topic, result, tt.expected)
			}
		})
	}
}

func TestTopicToEventType_RefinedTopics(t *testing.T) {
	tests := []struct {
		topic    string
		expected string
	}{
		{"odin.dev.trade.refined", "trade"},
		{"odin.local.liquidity.refined", "liquidity"},
		{"odin.staging.metadata.refined", "metadata"},
		{"odin.prod.balances.refined", "balances"},
	}

	for _, tt := range tests {
		t.Run(tt.topic, func(t *testing.T) {
			result := TopicToEventType(tt.topic)
			if result != tt.expected {
				t.Errorf("TopicToEventType(%q) = %q, want %q", tt.topic, result, tt.expected)
			}
		})
	}
}

func TestTopicToEventType_LegacyTopics(t *testing.T) {
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
			result := EventTypeToTopicBase(tt.eventType)
			if result != tt.expected {
				t.Errorf("EventTypeToTopicBase(%q) = %q, want %q", tt.eventType, result, tt.expected)
			}
		})
	}
}

func TestEventTypeToTopicBase_Unknown(t *testing.T) {
	// Unknown event types should default to trade topic
	result := EventTypeToTopicBase(EventType("UNKNOWN_EVENT"))
	if result != TopicBaseTrade {
		t.Errorf("EventTypeToTopicBase(UNKNOWN) = %q, want %q", result, TopicBaseTrade)
	}
}
