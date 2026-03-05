package audit

import (
	"time"

	"github.com/klurvio/sukko/internal/shared/alerting"
)

// Event represents a single auditable event in the system.
// All events are logged as JSON for easy parsing by log aggregation tools.
type Event struct {
	Level     alerting.Level `json:"level"`
	Timestamp time.Time      `json:"timestamp"`
	Event     string         `json:"event"`               // Event type: "AuthSuccess", "RateLimited", etc.
	ClientID  *int64         `json:"client_id,omitempty"` // Optional client ID
	Message   string         `json:"message"`             // Human-readable description
	Metadata  map[string]any `json:"metadata,omitempty"`  // Additional context
}

// Common event type constants for consistency across services.
const (
	// Authentication events
	EventAuthSuccess = "AuthSuccess"
	EventAuthFailure = "AuthFailure"
	EventAuthExpired = "AuthExpired"

	// Authorization events
	EventAccessDenied  = "AccessDenied"
	EventAccessGranted = "AccessGranted"

	// Rate limiting events
	EventRateLimited = "RateLimited"

	// Connection events
	EventConnectionEstablished = "ConnectionEstablished"
	EventConnectionClosed      = "ConnectionClosed"
	EventSlowClientDisconnect  = "SlowClientDisconnect"

	// Capacity events
	EventServerAtCapacity   = "ServerAtCapacity"
	EventCapacityRejection  = "CapacityRejection"
	EventCapacityRecovered  = "CapacityRecovered"
	EventMemoryThreshold    = "MemoryThreshold"
	EventCPUThreshold       = "CPUThreshold"
	EventGoroutineThreshold = "GoroutineThreshold"

	// Tenant events
	EventTenantCreated     = "TenantCreated"
	EventTenantUpdated     = "TenantUpdated"
	EventTenantDeleted     = "TenantDeleted"
	EventTenantSuspended   = "TenantSuspended"
	EventTenantReactivated = "TenantReactivated"

	// API events
	EventAPIRequest     = "APIRequest"
	EventAPIError       = "APIError"
	EventValidationFail = "ValidationFail"

	// System events
	EventServerStarted  = "ServerStarted"
	EventServerShutdown = "ServerShutdown"
	EventConfigChange   = "ConfigChange"
	EventHealthCheck    = "HealthCheck"

	// Kafka events
	EventKafkaConnected    = "KafkaConnected"
	EventKafkaDisconnected = "KafkaDisconnected"
	EventKafkaError        = "KafkaError"
)
