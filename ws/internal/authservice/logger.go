package authservice

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// Logger provides structured logging for auth service events.
// Thread-safe for concurrent use.
type Logger struct {
	logger zerolog.Logger
}

// NewLogger creates a new logger that writes to the given writer.
// Output is JSON-formatted for easy parsing by log aggregation systems.
func NewLogger(w io.Writer) *Logger {
	logger := zerolog.New(w).With().
		Timestamp().
		Str("service", "auth-service").
		Logger()

	return &Logger{logger: logger}
}

// LogAuthSuccess logs a successful token issuance.
func (l *Logger) LogAuthSuccess(r *http.Request, tenantID, appID string, expiresAt time.Time, latency time.Duration) {
	l.logger.Info().
		Str("event", "auth_success").
		Str("tenant_id", tenantID).
		Str("app_id", appID).
		Str("client_ip", getClientIP(r)).
		Time("expires_at", expiresAt).
		Dur("latency_ms", latency).
		Msg("token issued successfully")
}

// LogAuthFailure logs a failed authentication attempt.
func (l *Logger) LogAuthFailure(r *http.Request, appID, reason string) {
	l.logger.Warn().
		Str("event", "auth_failure").
		Str("app_id", appID).
		Str("client_ip", getClientIP(r)).
		Str("reason", reason).
		Msg("authentication failed")
}

// LogInvalidRequest logs a malformed or invalid request.
func (l *Logger) LogInvalidRequest(r *http.Request, reason string) {
	l.logger.Warn().
		Str("event", "invalid_request").
		Str("client_ip", getClientIP(r)).
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Str("reason", reason).
		Msg("invalid request")
}

// LogServiceError logs an internal service error.
func (l *Logger) LogServiceError(r *http.Request, appID string, err error) {
	l.logger.Error().
		Str("event", "service_error").
		Str("app_id", appID).
		Str("client_ip", getClientIP(r)).
		Err(err).
		Msg("internal service error")
}

// LogConfigLoaded logs when configuration is successfully loaded.
func (l *Logger) LogConfigLoaded(tenantCount, appCount int) {
	l.logger.Info().
		Str("event", "config_loaded").
		Int("tenant_count", tenantCount).
		Int("app_count", appCount).
		Msg("configuration loaded")
}

// LogStartup logs service startup.
func (l *Logger) LogStartup(addr string) {
	l.logger.Info().
		Str("event", "startup").
		Str("addr", addr).
		Msg("auth service starting")
}

// getClientIP extracts the client IP from the request.
// Handles X-Forwarded-For header for proxied requests.
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (set by load balancers/proxies)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP (original client)
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	return r.RemoteAddr
}
