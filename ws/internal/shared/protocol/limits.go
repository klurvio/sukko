package protocol

// Default size limits for WebSocket messages.
// These can be overridden via environment variables in each component's config.
const (
	// DefaultMaxPublishSize is the default maximum size for publish payloads (64KB).
	// Gateway: GATEWAY_MAX_PUBLISH_SIZE
	DefaultMaxPublishSize = 64 * 1024

	// DefaultSendBufferSize is the default size for client send buffers.
	DefaultSendBufferSize = 256
)

// Default rate limits for publish operations.
// These can be overridden via environment variables in each component's config.
// Gateway: GATEWAY_PUBLISH_RATE_LIMIT, GATEWAY_PUBLISH_BURST
const (
	// DefaultPublishRateLimit is the default messages per second limit.
	DefaultPublishRateLimit = 10.0

	// DefaultPublishBurst is the default burst capacity.
	DefaultPublishBurst = 100
)

// Channel format constants define the minimum number of parts in channel names.
const (
	// MinClientChannelParts is the minimum parts for client channels: {identifier}.{category}
	MinClientChannelParts = 2

	// MinInternalChannelParts is the minimum parts for internal channels: {tenant}.{identifier}.{category}
	MinInternalChannelParts = 3
)
