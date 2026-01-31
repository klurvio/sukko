package server

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/Toniq-Labs/odin-ws/internal/server/metrics"
	"github.com/Toniq-Labs/odin-ws/internal/shared/protocol"
)

// handleClientPublish processes a client publish request.
// It validates the request, checks rate limits, and publishes to Kafka.
//
// Message format:
//
//	{
//	  "type": "publish",
//	  "data": {
//	    "channel": "acme.BTC.trade",  // Internal format (tenant-prefixed from gateway)
//	    "data": {"message": "Hello", "sender": "user456"}
//	  }
//	}
//
// The message is published to Kafka with:
//   - Topic: {namespace}.{tenant}.{category} (extracted from channel)
//   - Key: full channel string (for partitioning - same channel = same partition = ordering)
//   - Value: raw JSON from client's data field
//   - Headers: client_id (server-verified), source, timestamp
//
// The channel must already be in internal format (tenant-prefixed) after gateway mapping.
func (s *Server) handleClientPublish(c *Client, data json.RawMessage) {
	// Check if producer is available
	if s.kafkaProducer == nil {
		s.sendPublishError(c, string(protocol.ErrCodeNotAvailable), protocol.PublishErrorMessages[protocol.ErrCodeNotAvailable])
		return
	}

	// Parse the publish request
	var pubReq protocol.PublishData

	if err := json.Unmarshal(data, &pubReq); err != nil {
		s.logger.Warn().
			Int64("client_id", c.id).
			Err(err).
			Msg("Client sent invalid publish request")
		s.sendPublishError(c, string(protocol.ErrCodeInvalidRequest), protocol.PublishErrorMessages[protocol.ErrCodeInvalidRequest])
		return
	}

	// Validate channel format (must have at least 3 dot-separated parts: tenant.identifier.category)
	if !isValidPublishChannel(pubReq.Channel) {
		s.logger.Warn().
			Int64("client_id", c.id).
			Str("channel", pubReq.Channel).
			Msg("Client sent invalid channel format")
		s.sendPublishError(c, string(protocol.ErrCodeInvalidChannel), protocol.PublishErrorMessages[protocol.ErrCodeInvalidChannel])
		return
	}

	// Validate message size
	if len(pubReq.Data) > protocol.DefaultMaxPublishSize {
		s.logger.Warn().
			Int64("client_id", c.id).
			Int("size", len(pubReq.Data)).
			Int("max_size", protocol.DefaultMaxPublishSize).
			Msg("Client publish message too large")
		s.sendPublishError(c, string(protocol.ErrCodeMessageTooLarge), protocol.PublishErrorMessages[protocol.ErrCodeMessageTooLarge])
		return
	}

	// Rate limiting check (uses the same rate limiter as other messages)
	if s.rateLimiter != nil && !s.rateLimiter.CheckLimit(c.id) {
		s.logger.Warn().
			Int64("client_id", c.id).
			Str("channel", pubReq.Channel).
			Msg("Client publish rate limited")
		s.sendPublishError(c, string(protocol.ErrCodeRateLimited), protocol.PublishErrorMessages[protocol.ErrCodeRateLimited])
		s.stats.RateLimitedMessages.Add(1)
		metrics.IncrementRateLimitedMessages()
		return
	}

	// Publish to Kafka with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.kafkaProducer.Publish(ctx, c.id, pubReq.Channel, pubReq.Data); err != nil {
		s.logger.Error().
			Err(err).
			Int64("client_id", c.id).
			Str("channel", pubReq.Channel).
			Msg("Failed to publish message to Kafka")

		// Map specific errors to error codes
		code := protocol.ErrCodePublishFailed
		switch {
		case errors.Is(err, protocol.ErrInvalidChannel):
			code = protocol.ErrCodeInvalidChannel
		case errors.Is(err, protocol.ErrTopicNotProvisioned):
			code = protocol.ErrCodeTopicNotProvisioned
		case errors.Is(err, protocol.ErrServiceUnavailable):
			code = protocol.ErrCodeServiceUnavailable
		}

		s.sendPublishError(c, string(code), protocol.PublishErrorMessages[code])
		metrics.IncrementPublishErrors()
		return
	}

	// Success - send acknowledgment
	s.sendPublishAck(c, pubReq.Channel)

	s.logger.Debug().
		Int64("client_id", c.id).
		Str("channel", pubReq.Channel).
		Int("data_size", len(pubReq.Data)).
		Msg("Client message published to Kafka")

	metrics.IncrementMessagesPublished()
}

// sendPublishAck sends a publish acknowledgment to the client.
func (s *Server) sendPublishAck(c *Client, channel string) {
	ack := map[string]any{
		"type":    protocol.RespTypePublishAck,
		"channel": channel,
		"status":  "accepted",
	}

	if data, err := json.Marshal(ack); err == nil {
		select {
		case c.send <- data:
			// Ack sent successfully
		default:
			// Client buffer full - skip ack (not critical, message was published)
		}
	}
}

// sendPublishError sends a publish error response to the client.
func (s *Server) sendPublishError(c *Client, code, message string) {
	errResp := map[string]any{
		"type":    protocol.RespTypePublishError,
		"code":    code,
		"message": message,
	}

	if data, err := json.Marshal(errResp); err == nil {
		select {
		case c.send <- data:
			// Error sent
		default:
			// Client buffer full
		}
	}
}

// isValidPublishChannel validates that a channel has the correct internal format.
// Internal channels must have at least 3 dot-separated parts: {tenant}.{identifier}.{category}
// Parts cannot be empty.
//
// Examples:
//   - "acme.BTC.trade" → valid (tenant=acme, identifier=BTC, category=trade)
//   - "acme.user123.balances" → valid
//   - "BTC.trade" → invalid (only 2 parts, not tenant-prefixed)
func isValidPublishChannel(channel string) bool {
	if channel == "" {
		return false
	}

	parts := strings.Split(channel, ".")
	if len(parts) < protocol.MinInternalChannelParts {
		return false
	}

	// Check that no part is empty
	return !slices.Contains(parts, "")
}
