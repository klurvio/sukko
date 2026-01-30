// Package audit provides structured audit logging for security-relevant events.
// All events are logged as JSON for easy parsing by log aggregation tools.
package audit

import "github.com/Toniq-Labs/odin-ws/pkg/alerting"

// ClientLogger is a helper for logging events related to a specific client.
// It automatically includes the client ID in all logged events.
type ClientLogger struct {
	logger   *Logger
	clientID int64
}

// Log logs an event with the client ID attached.
func (c *ClientLogger) Log(event Event) {
	event.ClientID = &c.clientID
	c.logger.Log(event)
}

// Debug logs a debug-level event for this client.
func (c *ClientLogger) Debug(event, message string, metadata map[string]any) {
	c.logger.Log(Event{
		Level:    alerting.DEBUG,
		Event:    event,
		ClientID: &c.clientID,
		Message:  message,
		Metadata: metadata,
	})
}

// Info logs an info-level event for this client.
func (c *ClientLogger) Info(event, message string, metadata map[string]any) {
	c.logger.Log(Event{
		Level:    alerting.INFO,
		Event:    event,
		ClientID: &c.clientID,
		Message:  message,
		Metadata: metadata,
	})
}

// Warning logs a warning-level event for this client.
func (c *ClientLogger) Warning(event, message string, metadata map[string]any) {
	c.logger.Log(Event{
		Level:    alerting.WARNING,
		Event:    event,
		ClientID: &c.clientID,
		Message:  message,
		Metadata: metadata,
	})
}

// Error logs an error-level event for this client.
func (c *ClientLogger) Error(event, message string, metadata map[string]any) {
	c.logger.Log(Event{
		Level:    alerting.ERROR,
		Event:    event,
		ClientID: &c.clientID,
		Message:  message,
		Metadata: metadata,
	})
}

// Critical logs a critical-level event for this client.
func (c *ClientLogger) Critical(event, message string, metadata map[string]any) {
	c.logger.Log(Event{
		Level:    alerting.CRITICAL,
		Event:    event,
		ClientID: &c.clientID,
		Message:  message,
		Metadata: metadata,
	})
}

// ClientID returns the client ID for this logger.
func (c *ClientLogger) ClientID() int64 {
	return c.clientID
}
