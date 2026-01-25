package server

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Toniq-Labs/odin-ws/internal/monitoring"
)

// Maximum message size for client publishes (64KB)
const maxPublishMessageSize = 64 * 1024

// handleClientPublish processes a client publish request.
// It validates the request, checks rate limits, and publishes to Kafka.
//
// Message format:
//
//	{
//	  "type": "publish",
//	  "data": {
//	    "channel": "community.group123.chat",
//	    "data": {"message": "Hello", "sender": "user456"}
//	  }
//	}
//
// The message is published to Kafka with:
//   - Topic: odin.{namespace}.client-events (single ingress topic)
//   - Key: channel string (for partitioning)
//   - Value: raw JSON from client's data field
//   - Headers: client_id (server-verified), source, timestamp
func (s *Server) handleClientPublish(c *Client, data json.RawMessage) {
	// Check if producer is available
	if s.kafkaProducer == nil {
		s.sendPublishError(c, "not_available", "Publishing is not enabled on this server")
		return
	}

	// Parse the publish request
	var pubReq struct {
		Channel string          `json:"channel"`
		Data    json.RawMessage `json:"data"`
	}

	if err := json.Unmarshal(data, &pubReq); err != nil {
		s.logger.Warn().
			Int64("client_id", c.id).
			Err(err).
			Msg("Client sent invalid publish request")
		s.sendPublishError(c, "invalid_request", "Invalid publish request format")
		return
	}

	// Validate channel format (must have at least 2 dot-separated parts)
	if !isValidPublishChannel(pubReq.Channel) {
		s.logger.Warn().
			Int64("client_id", c.id).
			Str("channel", pubReq.Channel).
			Msg("Client sent invalid channel format")
		s.sendPublishError(c, "invalid_channel", "Channel must have format: name.type (e.g., community.chat)")
		return
	}

	// Validate message size
	if len(pubReq.Data) > maxPublishMessageSize {
		s.logger.Warn().
			Int64("client_id", c.id).
			Int("size", len(pubReq.Data)).
			Int("max_size", maxPublishMessageSize).
			Msg("Client publish message too large")
		s.sendPublishError(c, "message_too_large", "Message exceeds maximum size of 64KB")
		return
	}

	// Rate limiting check (uses the same rate limiter as other messages)
	// TODO: Consider separate rate limiter with stricter limits for publishing
	if s.rateLimiter != nil && !s.rateLimiter.CheckLimit(c.id) {
		s.logger.Warn().
			Int64("client_id", c.id).
			Str("channel", pubReq.Channel).
			Msg("Client publish rate limited")
		s.sendPublishError(c, "rate_limited", "Publish rate limit exceeded")
		atomic.AddInt64(&s.stats.RateLimitedMessages, 1)
		monitoring.IncrementRateLimitedMessages()
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
		s.sendPublishError(c, "publish_failed", "Failed to publish message")
		monitoring.IncrementPublishErrors()
		return
	}

	// Success - send acknowledgment
	s.sendPublishAck(c, pubReq.Channel)

	s.logger.Debug().
		Int64("client_id", c.id).
		Str("channel", pubReq.Channel).
		Int("data_size", len(pubReq.Data)).
		Msg("Client message published to Kafka")

	monitoring.IncrementMessagesPublished()
}

// sendPublishAck sends a publish acknowledgment to the client.
func (s *Server) sendPublishAck(c *Client, channel string) {
	ack := map[string]any{
		"type":    "publish_ack",
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
		"type":    "publish_error",
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

// isValidPublishChannel validates that a channel has the correct format.
// Channels must have at least 2 dot-separated parts (e.g., "community.chat").
// Parts cannot be empty.
func isValidPublishChannel(channel string) bool {
	if channel == "" {
		return false
	}

	parts := strings.Split(channel, ".")
	if len(parts) < 2 {
		return false
	}

	// Check that no part is empty
	return !slices.Contains(parts, "")
}
