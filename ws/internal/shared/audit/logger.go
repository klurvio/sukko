package audit

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"time"

	"github.com/klurvio/sukko/internal/shared/alerting"
)

// Logger handles structured logging for all auditable events.
// Logs are written as JSON for easy integration with:
//   - Docker logs
//   - Cloud logging (Cloud Run, EKS, etc.)
//   - Log aggregation (Elasticsearch, Loki, Datadog, etc.)
type Logger struct {
	writer   io.Writer
	logger   *log.Logger
	minLevel alerting.Level
	alerter  alerting.Alerter
}

// New creates a new audit logger with the given configuration.
func New(cfg Config) *Logger {
	writer := cfg.Writer
	if writer == nil {
		writer = os.Stdout
	}

	return &Logger{
		writer:   writer,
		logger:   log.New(writer, "", 0), // No prefix, timestamp in JSON
		minLevel: cfg.MinLevel,
		alerter:  cfg.Alerter,
	}
}

// NewWithLevel creates a new audit logger with the specified minimum level.
// Events below minLevel are not logged (for performance).
//
// Example:
//
//	logger := NewWithLevel(alerting.INFO)  // Logs INFO, WARNING, ERROR, CRITICAL (not DEBUG)
func NewWithLevel(minLevel alerting.Level) *Logger {
	return New(Config{MinLevel: minLevel})
}

// NewWithAlerter creates an audit logger with alerter integration.
// Alerter is called for WARNING, ERROR, and CRITICAL events.
func NewWithAlerter(minLevel alerting.Level, alerter alerting.Alerter) *Logger {
	return New(Config{MinLevel: minLevel, Alerter: alerter})
}

// SetAlerter sets the alerter for sending notifications.
// Alerter is called for WARNING, ERROR, and CRITICAL events.
func (l *Logger) SetAlerter(alerter alerting.Alerter) {
	l.alerter = alerter
}

// Log logs an audit event if it meets the minimum level requirement.
// Events are output as JSON to the configured writer.
//
// Example output:
//
//	{"level":"CRITICAL","timestamp":"2025-10-03T12:00:00Z","event":"ServerAtCapacity","message":"All slots occupied","metadata":{"connections":2184}}
func (l *Logger) Log(event Event) {
	// Set timestamp if not already set
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Only log if meets minimum level
	if !l.shouldLog(event.Level) {
		return
	}

	// JSON encode for structured logging
	jsonBytes, err := json.Marshal(event)
	if err != nil {
		// Fallback: Log the error itself
		l.logger.Printf("Failed to marshal audit event: %v", err)
		return
	}

	// Write to configured output
	l.logger.Println(string(jsonBytes))

	// Send alerts for important events
	if l.alerter != nil && event.Level.ShouldAlert() {
		l.alerter.Alert(event.Level, event.Message, event.Metadata)
	}
}

// shouldLog determines if an event should be logged based on its level.
func (l *Logger) shouldLog(level alerting.Level) bool {
	return level.Value() >= l.minLevel.Value()
}

// Debug logs a debug-level event.
func (l *Logger) Debug(event, message string, metadata map[string]any) {
	l.Log(Event{Level: alerting.DEBUG, Event: event, Message: message, Metadata: metadata})
}

// Info logs an info-level event.
func (l *Logger) Info(event, message string, metadata map[string]any) {
	l.Log(Event{Level: alerting.INFO, Event: event, Message: message, Metadata: metadata})
}

// Warning logs a warning-level event.
func (l *Logger) Warning(event, message string, metadata map[string]any) {
	l.Log(Event{Level: alerting.WARNING, Event: event, Message: message, Metadata: metadata})
}

// Error logs an error-level event.
func (l *Logger) Error(event, message string, metadata map[string]any) {
	l.Log(Event{Level: alerting.ERROR, Event: event, Message: message, Metadata: metadata})
}

// Critical logs a critical-level event.
func (l *Logger) Critical(event, message string, metadata map[string]any) {
	l.Log(Event{Level: alerting.CRITICAL, Event: event, Message: message, Metadata: metadata})
}

// WithClientID creates a helper for logging events for a specific client.
func (l *Logger) WithClientID(clientID int64) *ClientLogger {
	return &ClientLogger{
		logger:   l,
		clientID: clientID,
	}
}
