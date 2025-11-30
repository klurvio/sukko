package monitoring

import (
	"encoding/json"
	"log"
	"os"
	"time"
)

// AuditLevel represents the severity of an audit event
// Follows standard logging levels used by most monitoring systems
type AuditLevel string

const (
	DEBUG    AuditLevel = "DEBUG"    // Detailed debug information
	INFO     AuditLevel = "INFO"     // Normal operations
	WARNING  AuditLevel = "WARNING"  // Warning but service continues
	ERROR    AuditLevel = "ERROR"    // Error occurred, may affect some users
	CRITICAL AuditLevel = "CRITICAL" // Critical issue, service degraded/down
)

// AuditEvent represents a single auditable event in the system
// All events are logged as JSON for easy parsing by log aggregation tools
type AuditEvent struct {
	Level     AuditLevel     `json:"level"`
	Timestamp time.Time      `json:"timestamp"`
	Event     string         `json:"event"`               // Event type: "ServerAtCapacity", "SlowClientDisconnected", etc.
	ClientID  *int64         `json:"client_id,omitempty"` // Optional client ID
	Message   string         `json:"message"`             // Human-readable description
	Metadata  map[string]any `json:"metadata,omitempty"`  // Additional context
}

// AuditLogger handles structured logging for all auditable events
// Logs are written to stdout in JSON format for easy integration with:
// - Docker logs
// - Cloud logging (Cloud Run, EKS, etc.)
// - Log aggregation (Elasticsearch, Datadog, etc.)
type AuditLogger struct {
	logger   *log.Logger
	minLevel AuditLevel
	alerter  Alerter // Optional: Send alerts for critical events
}

// NewAuditLogger creates a new audit logger with specified minimum level
// Events below minLevel are not logged (for performance)
//
// Example:
//
//	logger := NewAuditLogger(INFO)  // Logs INFO, WARNING, ERROR, CRITICAL (not DEBUG)
func NewAuditLogger(minLevel AuditLevel) *AuditLogger {
	return &AuditLogger{
		logger:   log.New(os.Stdout, "", 0), // No prefix, we include timestamp in JSON
		minLevel: minLevel,
	}
}

// SetAlerter sets the alerter for sending notifications
// Alerter is called for WARNING, ERROR, and CRITICAL events
func (a *AuditLogger) SetAlerter(alerter Alerter) {
	a.alerter = alerter
}

// Log logs an audit event if it meets the minimum level requirement
// Events are output as JSON to stdout
//
// Example output:
//
//	{"level":"CRITICAL","timestamp":"2025-10-03T12:00:00Z","event":"ServerAtCapacity","message":"All slots occupied","metadata":{"connections":2184}}
func (a *AuditLogger) Log(event AuditEvent) {
	// Set timestamp if not already set
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Only log if meets minimum level
	if !a.shouldLog(event.Level) {
		return
	}

	// JSON encode for structured logging
	jsonBytes, err := json.Marshal(event)
	if err != nil {
		// Fallback: Log the error itself
		a.logger.Printf("Failed to marshal audit event: %v", err)
		return
	}

	// Write to stdout (captured by Docker/Cloud Run)
	a.logger.Println(string(jsonBytes))

	// Send alerts for important events
	if a.alerter != nil && (event.Level == WARNING || event.Level == ERROR || event.Level == CRITICAL) {
		a.alerter.Alert(event.Level, event.Message, event.Metadata)
	}
}

// shouldLog determines if an event should be logged based on its level
func (a *AuditLogger) shouldLog(level AuditLevel) bool {
	levels := map[AuditLevel]int{
		DEBUG:    0,
		INFO:     1,
		WARNING:  2,
		ERROR:    3,
		CRITICAL: 4,
	}

	return levels[level] >= levels[a.minLevel]
}

// Convenience methods for common log levels

// Debug logs a debug-level event
func (a *AuditLogger) Debug(event, message string, metadata map[string]any) {
	a.Log(AuditEvent{Level: DEBUG, Event: event, Message: message, Metadata: metadata})
}

// Info logs an info-level event
func (a *AuditLogger) Info(event, message string, metadata map[string]any) {
	a.Log(AuditEvent{Level: INFO, Event: event, Message: message, Metadata: metadata})
}

// Warning logs a warning-level event
func (a *AuditLogger) Warning(event, message string, metadata map[string]any) {
	a.Log(AuditEvent{Level: WARNING, Event: event, Message: message, Metadata: metadata})
}

// Error logs an error-level event
func (a *AuditLogger) Error(event, message string, metadata map[string]any) {
	a.Log(AuditEvent{Level: ERROR, Event: event, Message: message, Metadata: metadata})
}

// Critical logs a critical-level event
func (a *AuditLogger) Critical(event, message string, metadata map[string]any) {
	a.Log(AuditEvent{Level: CRITICAL, Event: event, Message: message, Metadata: metadata})
}

// WithClientID creates a helper for logging events for a specific client
func (a *AuditLogger) WithClientID(clientID int64) *ClientLogger {
	return &ClientLogger{
		auditLogger: a,
		clientID:    clientID,
	}
}

// ClientLogger is a helper for logging events related to a specific client
type ClientLogger struct {
	auditLogger *AuditLogger
	clientID    int64
}

func (c *ClientLogger) Debug(event, message string, metadata map[string]any) {
	c.auditLogger.Log(AuditEvent{
		Level:    DEBUG,
		Event:    event,
		ClientID: &c.clientID,
		Message:  message,
		Metadata: metadata,
	})
}

func (c *ClientLogger) Info(event, message string, metadata map[string]any) {
	c.auditLogger.Log(AuditEvent{
		Level:    INFO,
		Event:    event,
		ClientID: &c.clientID,
		Message:  message,
		Metadata: metadata,
	})
}

func (c *ClientLogger) Warning(event, message string, metadata map[string]any) {
	c.auditLogger.Log(AuditEvent{
		Level:    WARNING,
		Event:    event,
		ClientID: &c.clientID,
		Message:  message,
		Metadata: metadata,
	})
}

func (c *ClientLogger) Error(event, message string, metadata map[string]any) {
	c.auditLogger.Log(AuditEvent{
		Level:    ERROR,
		Event:    event,
		ClientID: &c.clientID,
		Message:  message,
		Metadata: metadata,
	})
}

func (c *ClientLogger) Critical(event, message string, metadata map[string]any) {
	c.auditLogger.Log(AuditEvent{
		Level:    CRITICAL,
		Event:    event,
		ClientID: &c.clientID,
		Message:  message,
		Metadata: metadata,
	})
}
