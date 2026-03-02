package server

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Toniq-Labs/odin-ws/internal/server/metrics"
	"github.com/Toniq-Labs/odin-ws/internal/shared/auth"
	"github.com/Toniq-Labs/odin-ws/internal/shared/protocol"
)

// defaultPublishTimeout is the timeout for backend publish operations.
const defaultPublishTimeout = 5 * time.Second

// handleClientPublish processes a client publish request.
// It validates the request, checks rate limits, and publishes via the message backend.
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
// The channel must already be in internal format (tenant-prefixed) after gateway mapping.
func (s *Server) handleClientPublish(c *Client, data json.RawMessage) {
	// Check if backend is available
	if s.backend == nil {
		s.sendPublishError(c, protocol.ErrCodeNotAvailable, protocol.PublishErrorMessages[protocol.ErrCodeNotAvailable])
		return
	}

	// Parse the publish request
	var pubReq protocol.PublishData

	if err := json.Unmarshal(data, &pubReq); err != nil {
		s.logger.Warn().
			Int64("client_id", c.id).
			Err(err).
			Msg("Client sent invalid publish request")
		s.sendPublishError(c, protocol.ErrCodeInvalidRequest, protocol.PublishErrorMessages[protocol.ErrCodeInvalidRequest])
		return
	}

	// Validate channel format (must have at least 3 dot-separated parts: tenant.identifier.category)
	if !auth.IsValidInternalChannel(pubReq.Channel) {
		s.logger.Warn().
			Int64("client_id", c.id).
			Str("channel", pubReq.Channel).
			Msg("Client sent invalid channel format")
		s.sendPublishError(c, protocol.ErrCodeInvalidChannel, protocol.PublishErrorMessages[protocol.ErrCodeInvalidChannel])
		return
	}

	// Validate message size
	if len(pubReq.Data) > protocol.DefaultMaxPublishSize {
		s.logger.Warn().
			Int64("client_id", c.id).
			Int("size", len(pubReq.Data)).
			Int("max_size", protocol.DefaultMaxPublishSize).
			Msg("Client publish message too large")
		s.sendPublishError(c, protocol.ErrCodeMessageTooLarge, protocol.PublishErrorMessages[protocol.ErrCodeMessageTooLarge])
		return
	}

	// Rate limiting check (uses the same rate limiter as other messages)
	if s.rateLimiter != nil && !s.rateLimiter.CheckLimit(c.id) {
		s.logger.Warn().
			Int64("client_id", c.id).
			Str("channel", pubReq.Channel).
			Msg("Client publish rate limited")
		s.sendPublishError(c, protocol.ErrCodeRateLimited, protocol.PublishErrorMessages[protocol.ErrCodeRateLimited])
		s.stats.RateLimitedMessages.Add(1)
		metrics.IncrementRateLimitedMessages()
		return
	}

	// Publish to backend with timeout
	ctx, cancel := context.WithTimeout(s.ctx, defaultPublishTimeout)
	defer cancel()

	if err := s.backend.Publish(ctx, c.id, pubReq.Channel, pubReq.Data); err != nil {
		s.logger.Error().
			Err(err).
			Int64("client_id", c.id).
			Str("channel", pubReq.Channel).
			Msg("Failed to publish message to backend")

		// Map specific errors to error codes
		code := ErrCodePublishFailed
		switch {
		case errors.Is(err, protocol.ErrInvalidChannel):
			code = protocol.ErrCodeInvalidChannel
		case errors.Is(err, protocol.ErrTopicNotProvisioned):
			code = protocol.ErrCodeTopicNotProvisioned
		case errors.Is(err, protocol.ErrServiceUnavailable):
			code = protocol.ErrCodeServiceUnavailable
		}

		s.sendPublishError(c, code, protocol.PublishErrorMessages[code])
		// Backend-agnostic publish error metrics are recorded inside each backend's Publish()
		return
	}

	// Success - send acknowledgment
	s.sendPublishAck(c, pubReq.Channel)

	s.logger.Debug().
		Int64("client_id", c.id).
		Str("channel", pubReq.Channel).
		Int("data_size", len(pubReq.Data)).
		Msg("Client message published to backend")

	// Backend-agnostic publish metrics are recorded inside each backend's Publish()
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
		case c.send <- RawMsg(data):
			// Ack sent successfully
		default:
			// Client buffer full - skip ack (not critical, message was published)
		}
	}
}

// sendPublishError sends a publish error response to the client.
func (s *Server) sendPublishError(c *Client, code protocol.ErrorCode, message string) {
	s.sendErrorToClient(c, protocol.RespTypePublishError, code, message)
}
