package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"
)

// Generator creates random channels and messages following the asyncapi spec.
//
// Topic format: {namespace}.{tenant}.{category}  (e.g., local.odin.trade)
// Key format:   {tenant}.{identifier}.{category} (e.g., odin.BTC.trade)
//
// The key becomes the WebSocket channel that clients subscribe to.
type Generator struct {
	cfg      *Config
	channels []string // resolved list of channels (keys)
	rng      *rand.Rand
}

// NewGenerator creates a new message generator.
func NewGenerator(cfg *Config) *Generator {
	g := &Generator{
		cfg: cfg,
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	g.channels = g.resolveChannels()
	return g
}

// resolveChannels builds the list of channels from config.
// Channels follow the format: {tenant}.{identifier}.{category}
func (g *Generator) resolveChannels() []string {
	// Use static channels if provided (like wsloadtest CHANNELS)
	if static := g.cfg.GetChannels(); len(static) > 0 {
		return static
	}

	// Generate from pattern and components
	identifiers := g.cfg.GetIdentifiers()
	categories := g.cfg.GetCategories()
	tenant := g.cfg.TenantID
	pattern := g.cfg.ChannelPattern
	if pattern == "" {
		pattern = "{tenant}.{identifier}.{category}"
	}

	channels := make([]string, 0, len(identifiers)*len(categories))
	for _, identifier := range identifiers {
		for _, category := range categories {
			// Apply pattern substitution
			channel := pattern
			channel = strings.ReplaceAll(channel, "{tenant}", tenant)
			channel = strings.ReplaceAll(channel, "{identifier}", identifier)
			channel = strings.ReplaceAll(channel, "{category}", category)
			channels = append(channels, channel)
		}
	}
	return channels
}

// Channels returns the list of channels this generator will use.
func (g *Generator) Channels() []string {
	return g.channels
}

// Message represents a Kafka message to be published.
type Message struct {
	Topic   string          // Kafka topic: {namespace}.{tenant}.{category}
	Key     string          // Kafka key (= channel): {tenant}.{identifier}.{category}
	Payload json.RawMessage // JSON payload (opaque, forwarded as-is to clients)
}

// NextMessage generates a random message for a random channel.
func (g *Generator) NextMessage() (*Message, error) {
	if len(g.channels) == 0 {
		return nil, fmt.Errorf("no channels configured")
	}

	// Pick random channel (this becomes the Kafka key)
	channel := g.channels[g.rng.Intn(len(g.channels))]

	// Parse channel to determine topic
	// Channel: {tenant}.{identifier}.{category}
	// Topic:   {namespace}.{tenant}.{category}
	topic, err := g.channelToTopic(channel)
	if err != nil {
		return nil, err
	}

	// Generate random payload
	payload := g.generatePayload(channel)
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	return &Message{
		Topic:   topic,
		Key:     channel,
		Payload: payloadBytes,
	}, nil
}

// channelToTopic converts a channel to its corresponding Kafka topic.
// Channel: {tenant}.{identifier}.{category} -> Topic: {namespace}.{tenant}.{category}
func (g *Generator) channelToTopic(channel string) (string, error) {
	parts := strings.Split(channel, ".")
	if len(parts) < 3 {
		return "", fmt.Errorf("invalid channel format: %s", channel)
	}

	tenant := parts[0]
	category := parts[len(parts)-1]

	// Topic format: {namespace}.{tenant}.{category}
	return fmt.Sprintf("%s.%s.%s", g.cfg.KafkaNamespace, tenant, category), nil
}

// generatePayload creates a random payload based on the channel category.
func (g *Generator) generatePayload(channel string) map[string]interface{} {
	parts := strings.Split(channel, ".")
	category := parts[len(parts)-1]
	identifier := ""
	if len(parts) >= 2 {
		identifier = parts[1]
	}

	// Base payload with timestamp
	payload := map[string]interface{}{
		"timestamp": time.Now().UnixMilli(),
	}

	// Add category-specific data
	switch category {
	case "trade":
		payload["token"] = identifier
		payload["price"] = g.randomPrice()
		payload["volume"] = g.randomAmount()
		payload["side"] = g.randomSide()
	case "liquidity":
		payload["token"] = identifier
		payload["poolId"] = fmt.Sprintf("pool_%d", g.rng.Intn(100))
		payload["liquidity"] = fmt.Sprintf("%d", g.rng.Intn(10000000))
	case "orderbook":
		payload["token"] = identifier
		payload["bids"] = g.randomOrders(5)
		payload["asks"] = g.randomOrders(5)
	case "balances":
		payload["address"] = g.randomAddress()
		payload["balance"] = fmt.Sprintf("%d", g.rng.Intn(1000000))
	default:
		payload["data"] = g.randomHex(32)
	}

	return payload
}

func (g *Generator) randomPrice() float64 {
	return float64(g.rng.Intn(100000)) + g.rng.Float64()
}

func (g *Generator) randomAmount() float64 {
	return float64(g.rng.Intn(1000)) + g.rng.Float64()
}

func (g *Generator) randomSide() string {
	if g.rng.Intn(2) == 0 {
		return "buy"
	}
	return "sell"
}

func (g *Generator) randomOrders(n int) []map[string]interface{} {
	orders := make([]map[string]interface{}, n)
	for i := 0; i < n; i++ {
		orders[i] = map[string]interface{}{
			"price":  g.randomPrice(),
			"amount": g.randomAmount(),
		}
	}
	return orders
}

func (g *Generator) randomAddress() string {
	return "0x" + g.randomHex(40)
}

func (g *Generator) randomHex(length int) string {
	const chars = "0123456789abcdef"
	b := make([]byte, length)
	for i := range b {
		b[i] = chars[g.rng.Intn(len(chars))]
	}
	return string(b)
}
