package protocol

import "errors"

// ErrorCode represents machine-readable error codes for all error response types.
// Used across error, subscribe_error, unsubscribe_error, publish_error,
// and reconnect_error responses.
type ErrorCode string

// Shared error codes used by both gateway and server.
// Server-only error codes (ErrCodeInvalidJSON, ErrCodePublishFailed, ErrCodeReplayFailed)
// live in ws/internal/server/protocol.go.
const (
	// General error codes (used across multiple response types).

	// ErrCodeInvalidRequest indicates a malformed request.
	// Used by subscribe_error, unsubscribe_error, reconnect_error, publish_error.
	ErrCodeInvalidRequest ErrorCode = "invalid_request"

	// ErrCodeNotAvailable indicates the requested feature is not available.
	// Used by publish_error and reconnect_error.
	ErrCodeNotAvailable ErrorCode = "not_available"

	// Publish-specific error codes.

	// ErrCodeInvalidChannel indicates invalid channel format.
	// Channel must have format: {tenant}.{identifier}.{category}
	ErrCodeInvalidChannel ErrorCode = "invalid_channel"

	// ErrCodeMessageTooLarge indicates payload exceeds size limit.
	ErrCodeMessageTooLarge ErrorCode = "message_too_large"

	// ErrCodeRateLimited indicates publish rate limit exceeded.
	ErrCodeRateLimited ErrorCode = "rate_limited"

	// ErrCodeForbidden indicates not authorized to publish to channel.
	ErrCodeForbidden ErrorCode = "forbidden"

	// ErrCodeTopicNotProvisioned indicates the category topic doesn't exist.
	ErrCodeTopicNotProvisioned ErrorCode = "topic_not_provisioned"

	// ErrCodeServiceUnavailable indicates Kafka is unavailable (circuit open).
	ErrCodeServiceUnavailable ErrorCode = "service_unavailable"
)

// PublishErrorMessages provides human-readable messages for publish error codes.
var PublishErrorMessages = map[ErrorCode]string{
	ErrCodeNotAvailable:        "Publishing is not enabled on this server",
	ErrCodeInvalidRequest:      "Invalid publish request format",
	ErrCodeInvalidChannel:      "Channel must have format: tenant.identifier.category",
	ErrCodeMessageTooLarge:     "Message exceeds maximum size limit",
	ErrCodeRateLimited:         "Publish rate limit exceeded",
	"publish_failed":           "Failed to publish message",
	ErrCodeForbidden:           "Not authorized to publish to this channel",
	ErrCodeTopicNotProvisioned: "Category is not provisioned for your tenant",
	ErrCodeServiceUnavailable:  "Service temporarily unavailable, please retry",
}

// Sentinel errors for internal use.
// These are used internally and mapped to ErrorCode for client responses.
var (
	// ErrInvalidChannel indicates the channel format is invalid.
	ErrInvalidChannel = errors.New("invalid channel format")

	// ErrTopicNotProvisioned indicates the topic doesn't exist.
	ErrTopicNotProvisioned = errors.New("topic not provisioned")

	// ErrServiceUnavailable indicates Kafka is unavailable.
	ErrServiceUnavailable = errors.New("service unavailable")
)
