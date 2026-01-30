package server

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Toniq-Labs/odin-ws/internal/server/metrics"
	"github.com/Toniq-Labs/odin-ws/internal/shared/messaging"
)

// Client message handlers
func (s *Server) handleClientMessage(c *Client, data []byte) {
	// Parse outer message structure
	var req struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}

	if err := json.Unmarshal(data, &req); err != nil {
		s.logger.Warn().
			Int64("client_id", c.id).
			Err(err).
			Msg("Client sent invalid JSON")
		return
	}

	switch req.Type {
	case "reconnect":
		// KAFKA-BASED RECONNECTION (replaces old in-memory replay buffer)
		// Client reconnecting after disconnect, requesting missed messages from Kafka
		//
		// Message format: {"type": "reconnect", "data": {"client_id": "abc123", "last_offset": 12345}}
		//
		// This uses Kafka's offset tracking for proper message replay:
		// - Survives server restarts (offsets in Kafka, not RAM)
		// - 7 days of history (not just 40 seconds)
		// - Zero RAM overhead (no duplicate message storage)
		// - Scales horizontally (any server can replay from Kafka)
		//
		// Implementation: See handleKafkaReconnect() below
		s.handleKafkaReconnect(c, req.Data)

	case "heartbeat":
		// Client keep-alive ping
		// Some older browsers/libraries don't support WebSocket ping/pong
		// So they send application-level heartbeats
		//
		// We respond with current server time (helps detect clock skew)
		pong := map[string]any{
			"type": "pong",
			"ts":   time.Now().UnixMilli(),
		}

		if data, err := json.Marshal(pong); err == nil {
			select {
			case c.send <- data:
				// Sent
			default:
				// If can't send pong, client buffer is full
				// Don't worry about it, regular heartbeat will fail eventually
			}
		}

	case "subscribe":
		// Client subscribing to hierarchical channels (symbol.eventType)
		// Message format: {"type": "subscribe", "data": {"channels": ["BTC.trade", "ETH.trade", "BTC.analytics"]}}
		//
		// Channel format: "{SYMBOL}.{EVENT_TYPE}"
		// - SYMBOL: Token symbol (BTC, ETH, SOL, etc.)
		// - EVENT_TYPE: One of 8 types (trade, liquidity, metadata, social, favorites, creation, analytics, balances)
		//
		// Benefits:
		// - Granular control: Subscribe only to event types you need
		// - Reduced bandwidth: Trading clients don't receive social/metadata events
		// - Reduced CPU: 8x fewer messages per subscribed symbol vs symbol-only subscription
		// - Better UX: Clients see only relevant updates
		//
		// Example use cases:
		// - Trading client: ["BTC.trade", "ETH.trade"] - Only price updates
		// - Dashboard: ["BTC.trade", "BTC.analytics", "ETH.trade", "ETH.analytics"] - Prices + metrics
		// - Social app: ["BTC.social", "ETH.social"] - Only comments/reactions
		//
		// Performance impact (10K clients, 200 tokens, 12 msg/sec):
		// - Without filtering: 12 × 8 events × 10K = 960K writes/sec (CPU overload)
		// - With hierarchical: 12 × avg 2K subscribers = 24K writes/sec (CPU <30%)
		// - Result: 40x reduction in broadcast overhead
		var subReq struct {
			Channels []string `json:"channels"` // List of hierarchical channels (e.g., "BTC.trade")
		}

		if err := json.Unmarshal(req.Data, &subReq); err != nil {
			s.logger.Warn().
				Int64("client_id", c.id).
				Err(err).
				Msg("Client sent invalid subscribe request")
			return
		}

		// Add subscriptions to client's local set
		c.subscriptions.AddMultiple(subReq.Channels)

		// Add to global subscription index for fast broadcast targeting
		s.subscriptionIndex.AddMultiple(subReq.Channels, c)

		s.logger.Info().
			Int64("client_id", c.id).
			Int("channel_count", len(subReq.Channels)).
			Strs("channels", subReq.Channels).
			Msg("Client subscribed")

		// Send acknowledgment to client
		ack := map[string]any{
			"type":       "subscription_ack",
			"subscribed": subReq.Channels,
			"count":      c.subscriptions.Count(),
		}

		if data, err := json.Marshal(ack); err == nil {
			select {
			case c.send <- data:
				// Ack sent successfully
			default:
				// Client buffer full - skip ack (not critical)
			}
		}

	case "unsubscribe":
		// Client unsubscribing from channels
		// Message format: {"type": "unsubscribe", "data": {"channels": ["BTC"]}}
		var unsubReq struct {
			Channels []string `json:"channels"` // List of channels to unsubscribe from
		}

		if err := json.Unmarshal(req.Data, &unsubReq); err != nil {
			s.logger.Warn().
				Int64("client_id", c.id).
				Err(err).
				Msg("Client sent invalid unsubscribe request")
			return
		}

		// Remove subscriptions from client's local set
		c.subscriptions.RemoveMultiple(unsubReq.Channels)

		// Remove from global subscription index
		s.subscriptionIndex.RemoveMultiple(unsubReq.Channels, c)

		s.logger.Info().
			Int64("client_id", c.id).
			Int("channel_count", len(unsubReq.Channels)).
			Strs("channels", unsubReq.Channels).
			Msg("Client unsubscribed")

		// Send acknowledgment to client
		ack := map[string]any{
			"type":         "unsubscription_ack",
			"unsubscribed": unsubReq.Channels,
			"count":        c.subscriptions.Count(),
		}

		if data, err := json.Marshal(ack); err == nil {
			select {
			case c.send <- data:
				// Ack sent successfully
			default:
				// Client buffer full - skip ack (not critical)
			}
		}

	case "publish":
		// Client publishing a message to Kafka
		// Message format: {"type": "publish", "data": {"channel": "community.group123.chat", "data": {...}}}
		//
		// This enables bidirectional WebSocket messaging:
		// - Clients can publish messages to Kafka topics
		// - Messages are stored with channel as key for partitioning
		// - Other clients subscribed to the channel receive the message via Kafka consumer
		//
		// Security: Authentication is handled by ws-gateway before requests reach here.
		// The client_id in Kafka headers is the server-verified identity, not client-claimed.
		s.handleClientPublish(c, req.Data)

	default:
		// Unknown message type
		// Log but don't disconnect (might be future feature we haven't implemented yet)
		s.logger.Warn().
			Int64("client_id", c.id).
			Str("message_type", req.Type).
			Msg("Client sent unknown message type")
	}
}

// handleHealth provides enhanced health checks with detailed status
// Returns: healthy, degraded (warnings), or unhealthy (errors)

func (s *Server) handleKafkaReconnect(c *Client, data []byte) {
	var reconnectReq struct {
		ClientID   string           `json:"client_id"`   // Persistent client identifier
		LastOffset map[string]int64 `json:"last_offset"` // Last Kafka offset per topic
	}

	if err := json.Unmarshal(data, &reconnectReq); err != nil {
		s.logger.Warn().
			Int64("client_id", c.id).
			Err(err).
			Msg("Client sent invalid reconnect request")

		// Send error response
		errorMsg := map[string]any{
			"type":    "reconnect_error",
			"message": "Invalid reconnect request format",
		}
		if errorData, err := json.Marshal(errorMsg); err == nil {
			select {
			case c.send <- errorData:
			default:
			}
		}
		return
	}

	s.logger.Info().
		Int64("client_id", c.id).
		Str("persistent_id", reconnectReq.ClientID).
		Interface("last_offsets", reconnectReq.LastOffset).
		Msg("Client requesting Kafka-based reconnection")

	// Check if Kafka consumer is available for replay
	if s.kafkaConsumer == nil {
		s.logger.Warn().
			Int64("client_id", c.id).
			Msg("Kafka replay requested but no consumer available")

		errorMsg := map[string]any{
			"type":    "reconnect_error",
			"message": "Message replay not available (no Kafka consumer)",
		}
		if errorData, err := json.Marshal(errorMsg); err == nil {
			select {
			case c.send <- errorData:
			default:
			}
		}
		return
	}

	// Get client's subscriptions for filtering
	subscriptions := c.subscriptions.List()

	// Create context with timeout for replay operation (5 seconds max)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Perform replay from Kafka
	replayedMsgs, err := s.kafkaConsumer.ReplayFromOffsets(
		ctx,
		reconnectReq.LastOffset,
		100,           // Max 100 messages to prevent overwhelming client
		subscriptions, // Only send messages for subscribed channels
	)

	if err != nil {
		s.logger.Warn().
			Int64("client_id", c.id).
			Err(err).
			Msg("Failed to replay messages from Kafka")

		errorMsg := map[string]any{
			"type":    "reconnect_error",
			"message": fmt.Sprintf("Failed to replay messages: %v", err),
		}
		if errorData, err := json.Marshal(errorMsg); err == nil {
			select {
			case c.send <- errorData:
			default:
			}
		}
		return
	}

	// Send replayed messages to client
	replayedCount := 0
	for _, msg := range replayedMsgs {
		// Wrap in message envelope with sequence number
		envelope := &messaging.MessageEnvelope{
			Type:      "message",       // Standard type for broadcast messages
			Seq:       c.seqGen.Next(), // Generate unique sequence number for this client
			Timestamp: time.Now().UnixMilli(),
			Channel:   msg.Subject, // Channel from Kafka Key (e.g., "BTC.trade")
			Priority:  messaging.PriorityNormal,
			Data:      json.RawMessage(msg.Data),
		}

		envelopeData, err := envelope.Serialize()
		if err == nil {
			select {
			case c.send <- envelopeData:
				replayedCount++
			default:
				s.logger.Warn().
					Int64("client_id", c.id).
					Msg("Client send buffer full during replay")
				break
			}
		}
	}

	// Send acknowledgment with replay statistics
	ackMsg := map[string]any{
		"type":              "reconnect_ack",
		"status":            "completed",
		"messages_replayed": replayedCount,
		"message":           fmt.Sprintf("Replayed %d missed messages", replayedCount),
	}

	if ackData, err := json.Marshal(ackMsg); err == nil {
		select {
		case c.send <- ackData:
		default:
		}
	}

	s.logger.Info().
		Int64("client_id", c.id).
		Int("messages_replayed", replayedCount).
		Msg("Kafka replay completed successfully")

	// Increment reconnect counter for monitoring
	s.stats.MessageReplayRequests.Add(1)
	metrics.IncrementReplayRequests()
}
