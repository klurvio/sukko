// Package shared provides core WebSocket server functionality.
// This file contains adapter implementations for the interfaces defined in interfaces.go.
// Adapters bridge external libraries (zerolog, monitoring, limits) to internal interfaces
// enabling dependency injection and testability.
package server

import (
	"time"

	"github.com/rs/zerolog"

	"github.com/Toniq-Labs/odin-ws/internal/monitoring"
)

// =============================================================================
// Zerolog Adapter (implements Logger interface)
// =============================================================================

// ZerologAdapter wraps zerolog.Logger to implement the Logger interface.
type ZerologAdapter struct {
	logger zerolog.Logger
}

// NewZerologAdapter creates a Logger from a zerolog.Logger.
func NewZerologAdapter(logger zerolog.Logger) *ZerologAdapter {
	return &ZerologAdapter{logger: logger}
}

func (z *ZerologAdapter) Debug() LogEvent { return &ZerologEventAdapter{event: z.logger.Debug()} }
func (z *ZerologAdapter) Info() LogEvent  { return &ZerologEventAdapter{event: z.logger.Info()} }
func (z *ZerologAdapter) Warn() LogEvent  { return &ZerologEventAdapter{event: z.logger.Warn()} }
func (z *ZerologAdapter) Error() LogEvent { return &ZerologEventAdapter{event: z.logger.Error()} }

func (z *ZerologAdapter) Printf(format string, v ...any) {
	z.logger.Printf(format, v...)
}

// ZerologEventAdapter wraps zerolog.Event to implement LogEvent.
type ZerologEventAdapter struct {
	event *zerolog.Event
}

func (e *ZerologEventAdapter) Err(err error) LogEvent {
	e.event = e.event.Err(err)
	return e
}

func (e *ZerologEventAdapter) Int64(key string, val int64) LogEvent {
	e.event = e.event.Int64(key, val)
	return e
}

func (e *ZerologEventAdapter) Int(key string, val int) LogEvent {
	e.event = e.event.Int(key, val)
	return e
}

func (e *ZerologEventAdapter) Str(key string, val string) LogEvent {
	e.event = e.event.Str(key, val)
	return e
}

func (e *ZerologEventAdapter) Interface(key string, val any) LogEvent {
	e.event = e.event.Interface(key, val)
	return e
}

func (e *ZerologEventAdapter) Msg(msg string) {
	e.event.Msg(msg)
}

// =============================================================================
// Audit Logger Adapter (implements AuditLogger interface)
// =============================================================================

// AuditLoggerAdapter wraps monitoring.AuditLogger to implement AuditLogger interface.
type AuditLoggerAdapter struct {
	logger *monitoring.AuditLogger
}

// NewAuditLoggerAdapter creates an AuditLogger from monitoring.AuditLogger.
func NewAuditLoggerAdapter(logger *monitoring.AuditLogger) *AuditLoggerAdapter {
	return &AuditLoggerAdapter{logger: logger}
}

func (a *AuditLoggerAdapter) Warning(event, message string, metadata map[string]any) {
	a.logger.Warning(event, message, metadata)
}

func (a *AuditLoggerAdapter) Info(event, message string, metadata map[string]any) {
	a.logger.Info(event, message, metadata)
}

func (a *AuditLoggerAdapter) Critical(event, message string, metadata map[string]any) {
	a.logger.Critical(event, message, metadata)
}

// =============================================================================
// Rate Limiter Adapter (implements RateLimiter interface)
// =============================================================================

// RateLimiterImpl is the interface that limits.RateLimiter implements.
type RateLimiterImpl interface {
	CheckLimit(clientID int64) bool
}

// RateLimiterAdapter wraps limits.RateLimiter to implement RateLimiter interface.
type RateLimiterAdapter struct {
	limiter RateLimiterImpl
}

// NewRateLimiterAdapter creates a RateLimiter from limits.RateLimiter.
func NewRateLimiterAdapter(limiter RateLimiterImpl) *RateLimiterAdapter {
	return &RateLimiterAdapter{limiter: limiter}
}

func (r *RateLimiterAdapter) CheckLimit(clientID int64) bool {
	return r.limiter.CheckLimit(clientID)
}

// =============================================================================
// Real Clock Implementation (implements Clock interface)
// =============================================================================

// RealClock implements Clock using the standard time package.
type RealClock struct{}

func (c *RealClock) Now() time.Time {
	return time.Now()
}

func (c *RealClock) NewTicker(d time.Duration) Ticker {
	return &RealTicker{ticker: time.NewTicker(d)}
}

func (c *RealClock) After(d time.Duration) <-chan time.Time {
	return time.After(d)
}

// RealTicker wraps time.Ticker to implement the Ticker interface.
type RealTicker struct {
	ticker *time.Ticker
}

func (t *RealTicker) C() <-chan time.Time {
	return t.ticker.C
}

func (t *RealTicker) Stop() {
	t.ticker.Stop()
}
