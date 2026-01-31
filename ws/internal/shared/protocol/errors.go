package protocol

import "errors"

// PublishErrorCode represents error codes for publish operations.
// These codes are part of the public API and documented in AsyncAPI spec.
type PublishErrorCode string

const (
	// ErrCodeNotAvailable indicates publishing is disabled on this server.
	ErrCodeNotAvailable PublishErrorCode = "not_available"

	// ErrCodeInvalidRequest indicates malformed publish request.
	ErrCodeInvalidRequest PublishErrorCode = "invalid_request"

	// ErrCodeInvalidChannel indicates invalid channel format.
	// Channel must have format: {identifier}.{category} (client) or
	// {tenant}.{identifier}.{category} (internal).
	ErrCodeInvalidChannel PublishErrorCode = "invalid_channel"

	// ErrCodeMessageTooLarge indicates payload exceeds size limit.
	ErrCodeMessageTooLarge PublishErrorCode = "message_too_large"

	// ErrCodeRateLimited indicates publish rate limit exceeded.
	ErrCodeRateLimited PublishErrorCode = "rate_limited"

	// ErrCodePublishFailed indicates Kafka publish failed.
	ErrCodePublishFailed PublishErrorCode = "publish_failed"

	// ErrCodeForbidden indicates not authorized to publish to channel.
	ErrCodeForbidden PublishErrorCode = "forbidden"

	// ErrCodeTopicNotProvisioned indicates the category topic doesn't exist.
	ErrCodeTopicNotProvisioned PublishErrorCode = "topic_not_provisioned"

	// ErrCodeServiceUnavailable indicates Kafka is unavailable (circuit open).
	ErrCodeServiceUnavailable PublishErrorCode = "service_unavailable"
)

// PublishErrorMessages provides human-readable messages for error codes.
var PublishErrorMessages = map[PublishErrorCode]string{
	ErrCodeNotAvailable:        "Publishing is not enabled on this server",
	ErrCodeInvalidRequest:      "Invalid publish request format",
	ErrCodeInvalidChannel:      "Channel must have format: identifier.category",
	ErrCodeMessageTooLarge:     "Message exceeds maximum size limit",
	ErrCodeRateLimited:         "Publish rate limit exceeded",
	ErrCodePublishFailed:       "Failed to publish message",
	ErrCodeForbidden:           "Not authorized to publish to this channel",
	ErrCodeTopicNotProvisioned: "Category is not provisioned for your tenant",
	ErrCodeServiceUnavailable:  "Service temporarily unavailable, please retry",
}

// Sentinel errors for internal use.
// These are used internally and mapped to PublishErrorCode for client responses.
var (
	// ErrInvalidChannel indicates the channel format is invalid.
	ErrInvalidChannel = errors.New("invalid channel format")

	// ErrTopicNotProvisioned indicates the topic doesn't exist.
	ErrTopicNotProvisioned = errors.New("topic not provisioned")

	// ErrServiceUnavailable indicates Kafka is unavailable.
	ErrServiceUnavailable = errors.New("service unavailable")

	// ErrProducerClosed indicates the producer has been closed.
	ErrProducerClosed = errors.New("producer is closed")
)
