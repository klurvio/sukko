package server

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/klurvio/sukko/internal/server/backend"
	"github.com/klurvio/sukko/internal/server/messaging"
	"github.com/klurvio/sukko/internal/server/metrics"
	"github.com/klurvio/sukko/internal/shared/protocol"
)

// Client message handlers
func (s *Server) handleClientMessage(c *Client, data []byte) {
	// Parse outer message structure
	var req protocol.ClientMessage

	if err := json.Unmarshal(data, &req); err != nil {
		s.logger.Warn().
			Int64("client_id", c.id).
			Err(err).
			Msg("Client sent invalid JSON")
		s.sendErrorToClient(c, MsgTypeError, protocol.ErrCodeInvalidJSON, "Message is not valid JSON")
		return
	}

	switch req.Type {
	case MsgTypeReconnect:
		// BACKEND-BASED RECONNECTION
		// Client reconnecting after disconnect, requesting missed messages from message backend.
		// Supports Kafka offset-based replay and JetStream sequence-based replay.
		// Direct mode has no replay (returns immediately).
		s.handleReconnect(c, req.Data)

	case MsgTypeHeartbeat:
		// Client keep-alive ping
		// Some older browsers/libraries don't support WebSocket ping/pong
		// So they send application-level heartbeats
		//
		// We respond with current server time (helps detect clock skew)
		pong := map[string]any{
			"type": MsgTypePong,
			"ts":   time.Now().UnixMilli(),
		}

		if data, err := json.Marshal(pong); err == nil {
			select {
			case c.send <- RawMsg(data):
				// Sent
			default:
				// If can't send pong, client buffer is full
				// Don't worry about it, regular heartbeat will fail eventually
			}
		}

	case protocol.MsgTypeSubscribe:
		// Client subscribing to hierarchical channels (tenant.symbol.eventType)
		// Message format: {"type": "subscribe", "data": {"channels": ["sukko.BTC.trade", "sukko.ETH.trade", "sukko.BTC.analytics"]}}
		//
		// Channel format: "{TENANT}.{SYMBOL}.{EVENT_TYPE}"
		// - TENANT: Tenant identifier (sukko, acme, etc.)
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
		// - Trading client: ["sukko.BTC.trade", "sukko.ETH.trade"] - Only price updates
		// - Dashboard: ["sukko.BTC.trade", "sukko.BTC.analytics", "sukko.ETH.trade", "sukko.ETH.analytics"] - Prices + metrics
		// - Social app: ["sukko.BTC.social", "sukko.ETH.social"] - Only comments/reactions
		//
		// Performance impact (10K clients, 200 tokens, 12 msg/sec):
		// - Without filtering: 12 × 8 events × 10K = 960K writes/sec (CPU overload)
		// - With hierarchical: 12 × avg 2K subscribers = 24K writes/sec (CPU <30%)
		// - Result: 40x reduction in broadcast overhead
		var subReq protocol.SubscribeData

		if err := json.Unmarshal(req.Data, &subReq); err != nil {
			s.logger.Warn().
				Int64("client_id", c.id).
				Err(err).
				Msg("Client sent invalid subscribe request")
			s.sendErrorToClient(c, RespTypeSubscribeError, protocol.ErrCodeInvalidRequest, "Invalid subscribe request format")
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
			"type":       RespTypeSubscriptionAck,
			"subscribed": subReq.Channels,
			"count":      c.subscriptions.Count(),
		}

		if data, err := json.Marshal(ack); err == nil {
			select {
			case c.send <- RawMsg(data):
				// Ack sent successfully
			default:
				// Client buffer full - skip ack (not critical)
			}
		}

	case protocol.MsgTypeUnsubscribe:
		// Client unsubscribing from channels
		// Message format: {"type": "unsubscribe", "data": {"channels": ["BTC"]}}
		var unsubReq UnsubscribeData

		if err := json.Unmarshal(req.Data, &unsubReq); err != nil {
			s.logger.Warn().
				Int64("client_id", c.id).
				Err(err).
				Msg("Client sent invalid unsubscribe request")
			s.sendErrorToClient(c, RespTypeUnsubscribeError, protocol.ErrCodeInvalidRequest, "Invalid unsubscribe request format")
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
			"type":         RespTypeUnsubscriptionAck,
			"unsubscribed": unsubReq.Channels,
			"count":        c.subscriptions.Count(),
		}

		if data, err := json.Marshal(ack); err == nil {
			select {
			case c.send <- RawMsg(data):
				// Ack sent successfully
			default:
				// Client buffer full - skip ack (not critical)
			}
		}

	case protocol.MsgTypePublish:
		// Client publishing a message via the message backend
		// Message format: {"type": "publish", "data": {"channel": "community.group123.chat", "data": {...}}}
		//
		// This enables bidirectional WebSocket messaging:
		// - Clients can publish messages through the configured backend
		// - Messages are routed to subscribers via the backend's consume loop
		//
		// Security: Authentication is handled by ws-gateway before requests reach here.
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

// handleReconnect handles client reconnection using the message backend's replay capability.
func (s *Server) handleReconnect(c *Client, data []byte) {
	var reconnectReq struct {
		ClientID   string           `json:"client_id"`   // Persistent client identifier
		LastOffset map[string]int64 `json:"last_offset"` // Last position per topic/stream
	}

	if err := json.Unmarshal(data, &reconnectReq); err != nil {
		s.logger.Warn().
			Int64("client_id", c.id).
			Err(err).
			Msg("Client sent invalid reconnect request")

		s.sendErrorToClient(c, RespTypeReconnectError, protocol.ErrCodeInvalidRequest, "Invalid reconnect request format")
		return
	}

	s.logger.Info().
		Int64("client_id", c.id).
		Str("persistent_id", reconnectReq.ClientID).
		Interface("last_offsets", reconnectReq.LastOffset).
		Msg("Client requesting message replay")

	// Check if backend is available for replay
	if s.backend == nil {
		s.logger.Warn().
			Int64("client_id", c.id).
			Msg("Message replay requested but no backend available")

		s.sendErrorToClient(c, RespTypeReconnectError, protocol.ErrCodeNotAvailable, "Message replay not available (no backend configured)")
		return
	}

	// Get client's subscriptions for filtering
	subscriptions := c.subscriptions.List()

	// Create context with timeout for replay operation
	ctx, cancel := context.WithTimeout(s.ctx, s.config.ReplayTimeout)
	defer cancel()

	// Perform replay from backend
	replayedMsgs, err := s.backend.Replay(ctx, backend.ReplayRequest{
		Positions:     reconnectReq.LastOffset,
		MaxMessages:   s.config.MaxReplayMessages,
		Subscriptions: subscriptions,
	})

	if err != nil {
		s.logger.Error().
			Int64("client_id", c.id).
			Err(err).
			Msg("Failed to replay messages from backend")

		s.sendErrorToClient(c, RespTypeReconnectError, protocol.ErrCodeReplayFailed, "Message replay failed")
		return
	}

	// Send replayed messages to client
	replayedCount := 0
replayLoop:
	for _, msg := range replayedMsgs {
		// Wrap in message envelope with sequence number
		envelope := &messaging.MessageEnvelope{
			Type:      MsgTypeMessage,
			Seq:       c.seqGen.Next(),
			Timestamp: time.Now().UnixMilli(),
			Channel:   msg.Subject,
			Priority:  messaging.PriorityNormal,
			Data:      json.RawMessage(msg.Data),
		}

		envelopeData, err := envelope.Serialize()
		if err != nil {
			s.logger.Warn().
				Err(err).
				Int64("client_id", c.id).
				Str("channel", msg.Subject).
				Msg("Failed to serialize replay message")
			continue
		}
		select {
		case c.send <- RawMsg(envelopeData):
			replayedCount++
		default:
			s.logger.Warn().
				Int64("client_id", c.id).
				Msg("Client send buffer full during replay, stopping")
			break replayLoop
		}
	}

	// Send acknowledgment with replay statistics
	ackMsg := map[string]any{
		"type":              RespTypeReconnectAck,
		"status":            "completed",
		"messages_replayed": replayedCount,
		"message":           fmt.Sprintf("Replayed %d missed messages", replayedCount),
	}

	if ackData, err := json.Marshal(ackMsg); err == nil {
		select {
		case c.send <- RawMsg(ackData):
		default:
		}
	}

	s.logger.Info().
		Int64("client_id", c.id).
		Int("messages_replayed", replayedCount).
		Msg("Message replay completed successfully")

	// Increment reconnect counter for monitoring
	s.stats.MessageReplayRequests.Add(1)
	metrics.IncrementReplayRequests()
}

// sendErrorToClient sends an error response to the client.
// Used for all error types (message parsing, subscribe, unsubscribe, reconnect, publish).
func (s *Server) sendErrorToClient(c *Client, errType string, code protocol.ErrorCode, message string) {
	errResp := map[string]any{
		"type":    errType,
		"code":    code,
		"message": message,
	}

	if data, err := json.Marshal(errResp); err == nil {
		select {
		case c.send <- RawMsg(data):
			// Error sent
		default:
			// Client buffer full - non-blocking per graceful degradation guidelines
		}
	}
}
