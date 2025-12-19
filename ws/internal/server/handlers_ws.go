package server

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Toniq-Labs/odin-ws/internal/monitoring"
	"github.com/gobwas/ws"
	"github.com/rs/zerolog"
)

// WebSocket upgrade handler
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	clientIP := getClientIP(r)

	// DEBUG: Log incoming WebSocket upgrade request
	s.logger.Debug().
		Str("remote_addr", r.RemoteAddr).
		Str("client_ip", clientIP).
		Str("user_agent", r.Header.Get("User-Agent")).
		Str("origin", r.Header.Get("Origin")).
		Str("upgrade", r.Header.Get("Upgrade")).
		Str("connection", r.Header.Get("Connection")).
		Str("sec_websocket_version", r.Header.Get("Sec-WebSocket-Version")).
		Str("sec_websocket_key", r.Header.Get("Sec-WebSocket-Key")).
		Msg("WebSocket upgrade request received")

	// Reject new connections during graceful shutdown
	// Client receives: HTTP 503 "Server is shutting down"
	// See: docs/API_REJECTION_RESPONSES.md (Scenario 1)
	if atomic.LoadInt32(&s.shuttingDown) == 1 {
		s.logger.Debug().
			Str("client_ip", clientIP).
			Msg("Connection rejected: server shutting down")
		http.Error(w, "Server is shutting down", http.StatusServiceUnavailable)
		return
	}

	// Connection rate limiting (DoS protection)
	// IMPORTANT: Skip rate limiting for internal LoadBalancer traffic (127.0.0.1)
	// The LoadBalancer proxies external clients to shards via localhost,
	// and we don't want to rate limit our own internal connections.
	// Client receives: HTTP 429 "Rate limit exceeded" (non-localhost only)
	// See: docs/API_REJECTION_RESPONSES.md (Scenario 2)
	if s.connectionRateLimiter != nil && clientIP != "127.0.0.1" {
		if !s.connectionRateLimiter.CheckConnectionAllowed(clientIP) {
			s.logger.Warn().
				Str("client_ip", clientIP).
				Dur("elapsed_ms", time.Since(startTime)).
				Msg("Connection rejected: rate limit exceeded")
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}
	}

	// ResourceGuard admission control - static limits with safety checks
	// Checks: goroutine limit, CPU usage, memory usage, connection count
	// Client receives: HTTP 503 "Server overloaded"
	// See: docs/API_REJECTION_RESPONSES.md (Scenario 3)
	shouldAccept, reason := s.resourceGuard.ShouldAcceptConnection()
	if !shouldAccept {
		currentConnections := atomic.LoadInt64(&s.stats.CurrentConnections)
		// DEBUG: Enhanced ResourceGuard rejection logging
		s.logger.Warn().
			Str("client_ip", clientIP).
			Int64("current_connections", currentConnections).
			Int("max_connections", s.config.MaxConnections).
			Str("reason", reason).
			Dur("elapsed_ms", time.Since(startTime)).
			Msg("Connection rejected by ResourceGuard")
		monitoring.ConnectionsFailed.Inc()
		http.Error(w, "Server overloaded", http.StatusServiceUnavailable)
		return
	}

	// DEBUG: ResourceGuard accepted connection
	// Guard with level check to avoid unnecessary atomic load when debug disabled
	if s.logger.GetLevel() <= zerolog.DebugLevel {
		s.logger.Debug().
			Str("client_ip", clientIP).
			Int64("current_connections", atomic.LoadInt64(&s.stats.CurrentConnections)).
			Int("max_connections", s.config.MaxConnections).
			Msg("ResourceGuard accepted connection")
	}

	// JWT Authentication (when enabled)
	// Extract token from query parameter and validate
	// Client receives: HTTP 401 "Unauthorized" if token is missing/invalid
	// See: docs/API_REJECTION_RESPONSES.md (Scenario 4 - Auth)
	var appID string
	var tenantID string
	var tokenExpiry time.Time
	if s.config.AuthEnabled && s.jwtValidator != nil {
		token := r.URL.Query().Get("token")

		// Log auth attempt
		if s.authAuditLog != nil {
			s.authAuditLog.LogAuthAttempt(clientIP, clientIP)
		}

		claims, err := s.jwtValidator.ValidateToken(token)
		if err != nil {
			// Log auth failure
			if s.authAuditLog != nil {
				s.authAuditLog.LogAuthFailed(clientIP, clientIP, err.Error())
			}

			s.logger.Warn().
				Str("client_ip", clientIP).
				Str("error", err.Error()).
				Dur("elapsed_ms", time.Since(startTime)).
				Msg("Connection rejected: authentication failed")
			monitoring.ConnectionsFailed.Inc()
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		appID = claims.AppID()
		tenantID = claims.Tenant()
		tokenExpiry = claims.ExpiresAt.Time

		// Log auth success
		if s.authAuditLog != nil {
			s.authAuditLog.LogAuthSuccess(clientIP, appID, clientIP, tokenExpiry)
		}

		s.logger.Debug().
			Str("client_ip", clientIP).
			Str("app_id", appID).
			Str("tenant_id", tenantID).
			Time("token_expiry", tokenExpiry).
			Msg("Authentication successful")
	}

	// Try to acquire connection slot (blocking, no timeout)
	// In multi-core mode with LoadBalancer, capacity control happens at LB level
	// This semaphore just ensures we don't exceed shard capacity
	s.connectionsSem <- struct{}{}

	// DEBUG: Log before upgrade attempt
	upgradeStart := time.Now()
	s.logger.Debug().
		Str("client_ip", clientIP).
		Dur("pre_upgrade_elapsed_ms", time.Since(startTime)).
		Msg("Attempting WebSocket upgrade")

	conn, _, _, err := ws.UpgradeHTTP(r, w)
	upgradeDuration := time.Since(upgradeStart)

	if err != nil {
		<-s.connectionsSem // Release slot
		// DEBUG: Enhanced upgrade failure logging
		s.auditLogger.Error("WebSocketUpgradeFailed", "Failed to upgrade HTTP connection to WebSocket", map[string]any{
			"error":            err.Error(),
			"remoteAddr":       r.RemoteAddr,
			"client_ip":        clientIP,
			"upgrade_duration": upgradeDuration.String(),
			"total_elapsed":    time.Since(startTime).String(),
		})
		monitoring.ConnectionsFailed.Inc()
		s.logger.Error().
			Err(err).
			Str("client_ip", clientIP).
			Dur("upgrade_duration_ms", upgradeDuration).
			Dur("total_elapsed_ms", time.Since(startTime)).
			Str("user_agent", r.Header.Get("User-Agent")).
			Str("sec_websocket_version", r.Header.Get("Sec-WebSocket-Version")).
			Msg("WebSocket upgrade failed")
		return
	}

	// DEBUG: Successful upgrade logging
	s.logger.Debug().
		Str("client_ip", clientIP).
		Dur("upgrade_duration_ms", upgradeDuration).
		Dur("total_elapsed_ms", time.Since(startTime)).
		Msg("WebSocket upgrade successful")

	client := s.connections.Get()
	client.conn = conn
	client.server = s
	client.id = atomic.AddInt64(&s.clientCount, 1)
	client.remoteAddr = clientIP

	// Set authentication fields (only if auth is enabled)
	if s.config.AuthEnabled && appID != "" {
		client.appID = appID
		client.tenantID = tenantID
		client.authenticated = true
		client.tokenExpiry = tokenExpiry

		// Register client with token monitor for expiry tracking
		if s.tokenMonitor != nil {
			clientIDStr := fmt.Sprintf("%d", client.id)
			s.tokenMonitor.TrackClient(clientIDStr, appID, clientIP, tokenExpiry)
		}
	}

	s.clients.Store(client, true)
	atomic.AddInt64(&s.stats.TotalConnections, 1)
	currentConns := atomic.AddInt64(&s.stats.CurrentConnections, 1)

	// Update Prometheus metrics
	monitoring.UpdateConnectionMetrics(s)

	// DEBUG: Client fully initialized, starting pumps
	s.logger.Info().
		Str("client_ip", clientIP).
		Int64("client_id", client.id).
		Int64("current_connections", currentConns).
		Dur("total_setup_time_ms", time.Since(startTime)).
		Msg("Client connected successfully - pumps starting")

	go s.writePump(client)
	go s.readPump(client)
}

// getClientIP extracts the client IP from the request.
// Checks X-Forwarded-For header first (for load balancers/proxies),
// then falls back to RemoteAddr.
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (standard for load balancers)
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		// Use first IP in the chain (client IP)
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// If split fails, return as-is (might be just IP without port)
		return r.RemoteAddr
	}
	return ip
}

// disconnectClient handles client disconnect with proper instrumentation
// Centralizes all disconnect logic to ensure consistent metrics and logging
