// Package auth provides JWT authentication for WebSocket connections.
package auth

import (
	"io"
	"time"

	"github.com/rs/zerolog"
)

// AuditEvent represents the type of authentication event.
type AuditEvent string

const (
	EventAuthAttempt       AuditEvent = "auth_attempt"
	EventAuthSuccess       AuditEvent = "auth_success"
	EventAuthFailed        AuditEvent = "auth_failed"
	EventAuthRefreshed     AuditEvent = "auth_refreshed"
	EventAuthRefreshFailed AuditEvent = "auth_refresh_failed"
	EventTokenExpired      AuditEvent = "token_expired"
	EventTokenExpiring     AuditEvent = "token_expiring"
	EventTokenIssued       AuditEvent = "token_issued"
	EventAuthDisconnect    AuditEvent = "auth_disconnect"
)

// AuditLogger provides structured logging for authentication events.
// Thread-safe for concurrent use.
type AuditLogger struct {
	logger zerolog.Logger
}

// NewAuditLogger creates a new audit logger that writes to the given writer.
// Output is JSON-formatted for easy parsing by log aggregation systems.
func NewAuditLogger(w io.Writer) *AuditLogger {
	logger := zerolog.New(w).With().
		Timestamp().
		Str("component", "auth").
		Logger()

	return &AuditLogger{logger: logger}
}

// LogAuthAttempt logs an authentication attempt.
func (a *AuditLogger) LogAuthAttempt(clientID, remoteAddr string) {
	a.logger.Info().
		Str("event", string(EventAuthAttempt)).
		Str("client_id", clientID).
		Str("remote_addr", remoteAddr).
		Msg("authentication attempt")
}

// LogAuthSuccess logs a successful authentication.
func (a *AuditLogger) LogAuthSuccess(clientID, appID, remoteAddr string, tokenExpiry time.Time) {
	a.logger.Info().
		Str("event", string(EventAuthSuccess)).
		Str("client_id", clientID).
		Str("app_id", appID).
		Str("remote_addr", remoteAddr).
		Time("token_expiry", tokenExpiry).
		Msg("authentication successful")
}

// LogAuthFailed logs a failed authentication attempt.
func (a *AuditLogger) LogAuthFailed(clientID, remoteAddr, reason string) {
	a.logger.Warn().
		Str("event", string(EventAuthFailed)).
		Str("client_id", clientID).
		Str("remote_addr", remoteAddr).
		Str("reason", reason).
		Msg("authentication failed")
}

// LogAuthRefreshed logs a successful token refresh.
func (a *AuditLogger) LogAuthRefreshed(appID string, newExpiry time.Time) {
	a.logger.Info().
		Str("event", string(EventAuthRefreshed)).
		Str("app_id", appID).
		Time("new_expiry", newExpiry).
		Msg("token refreshed")
}

// LogAuthRefreshFailed logs a failed token refresh attempt.
func (a *AuditLogger) LogAuthRefreshFailed(appID, reason string) {
	a.logger.Warn().
		Str("event", string(EventAuthRefreshFailed)).
		Str("app_id", appID).
		Str("reason", reason).
		Msg("token refresh failed")
}

// LogTokenIssued logs when a new token is issued.
func (a *AuditLogger) LogTokenIssued(appID string, expiresAt time.Time) {
	a.logger.Info().
		Str("event", string(EventTokenIssued)).
		Str("app_id", appID).
		Time("expires_at", expiresAt).
		Msg("token issued")
}

// LogTokenExpired logs when a token has expired and the connection is being closed.
func (a *AuditLogger) LogTokenExpired(clientID, appID, remoteAddr string) {
	a.logger.Warn().
		Str("event", string(EventTokenExpired)).
		Str("client_id", clientID).
		Str("app_id", appID).
		Str("remote_addr", remoteAddr).
		Msg("token expired, disconnecting client")
}

// LogTokenExpiring logs when a token is about to expire and client is being warned.
func (a *AuditLogger) LogTokenExpiring(clientID, appID, remoteAddr string, expiresIn time.Duration) {
	a.logger.Info().
		Str("event", string(EventTokenExpiring)).
		Str("client_id", clientID).
		Str("app_id", appID).
		Str("remote_addr", remoteAddr).
		Dur("expires_in", expiresIn).
		Msg("token expiring soon")
}

// LogAuthDisconnect logs when a client is disconnected due to authentication issues.
func (a *AuditLogger) LogAuthDisconnect(clientID, appID, remoteAddr, reason string) {
	a.logger.Info().
		Str("event", string(EventAuthDisconnect)).
		Str("client_id", clientID).
		Str("app_id", appID).
		Str("remote_addr", remoteAddr).
		Str("reason", reason).
		Msg("client disconnected")
}
