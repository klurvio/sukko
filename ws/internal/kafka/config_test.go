package kafka

import (
	"testing"
)

// =============================================================================
// Topic Constants Tests
// =============================================================================

func TestTopicConstants(t *testing.T) {
	// Verify all topic constants have expected values
	tests := []struct {
		name     string
		topic    string
		expected string
	}{
		{"TopicTrades", TopicTrades, "odin.trades"},
		{"TopicLiquidity", TopicLiquidity, "odin.liquidity"},
		{"TopicMetadata", TopicMetadata, "odin.metadata"},
		{"TopicSocial", TopicSocial, "odin.social"},
		{"TopicCommunity", TopicCommunity, "odin.community"},
		{"TopicCreation", TopicCreation, "odin.creation"},
		{"TopicAnalytics", TopicAnalytics, "odin.analytics"},
		{"TopicBalances", TopicBalances, "odin.balances"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.topic != tt.expected {
				t.Errorf("%s = %q, want %q", tt.name, tt.topic, tt.expected)
			}
		})
	}
}

// =============================================================================
// AllTopics Tests
// =============================================================================

func TestAllTopics_ReturnsAllTopics(t *testing.T) {
	topics := AllTopics()

	if len(topics) != 8 {
		t.Errorf("AllTopics() returned %d topics, want 8", len(topics))
	}
}

func TestAllTopics_ContainsExpectedTopics(t *testing.T) {
	topics := AllTopics()
	topicSet := make(map[string]bool)
	for _, topic := range topics {
		topicSet[topic] = true
	}

	expectedTopics := []string{
		TopicTrades,
		TopicLiquidity,
		TopicMetadata,
		TopicSocial,
		TopicCommunity,
		TopicCreation,
		TopicAnalytics,
		TopicBalances,
	}

	for _, expected := range expectedTopics {
		if !topicSet[expected] {
			t.Errorf("AllTopics() missing %s", expected)
		}
	}
}

func TestAllTopics_ReturnsNewSlice(t *testing.T) {
	topics1 := AllTopics()
	topics2 := AllTopics()

	// Modify first slice
	if len(topics1) > 0 {
		topics1[0] = "modified"
	}

	// Second slice should be unaffected
	if topics2[0] == "modified" {
		t.Error("AllTopics() should return a new slice each time")
	}
}

// =============================================================================
// TopicToEventType Tests
// =============================================================================

func TestTopicToEventType_AllTopics(t *testing.T) {
	tests := []struct {
		topic    string
		expected string
	}{
		{TopicTrades, "trade"},
		{TopicLiquidity, "liquidity"},
		{TopicMetadata, "metadata"},
		{TopicSocial, "social"},
		{TopicCommunity, "community"},
		{TopicCreation, "creation"},
		{TopicAnalytics, "analytics"},
		{TopicBalances, "balances"},
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

func TestTopicToEventType_UnknownTopic(t *testing.T) {
	result := TopicToEventType("unknown.topic")
	if result != "unknown" {
		t.Errorf("TopicToEventType(unknown) = %q, want %q", result, "unknown")
	}
}

func TestTopicToEventType_EmptyTopic(t *testing.T) {
	result := TopicToEventType("")
	if result != "unknown" {
		t.Errorf("TopicToEventType('') = %q, want %q", result, "unknown")
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
