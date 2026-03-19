package server

import (
	"net/http"
	"time"

	"github.com/gobwas/ws"
	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/server/metrics"
	"github.com/klurvio/sukko/internal/shared/httputil"
)

// WebSocket upgrade handler
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	clientIP := httputil.GetClientIP(r)

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
	if s.shuttingDown.Load() == 1 {
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
		currentConnections := s.stats.CurrentConnections.Load()
		// DEBUG: Enhanced ResourceGuard rejection logging
		s.logger.Warn().
			Str("client_ip", clientIP).
			Int64("current_connections", currentConnections).
			Int("max_connections", s.maxConns).
			Str("reason", reason).
			Dur("elapsed_ms", time.Since(startTime)).
			Msg("Connection rejected by ResourceGuard")
		metrics.ConnectionsFailed.Inc()
		http.Error(w, "Server overloaded", http.StatusServiceUnavailable)
		return
	}

	// DEBUG: ResourceGuard accepted connection
	// Guard with level check to avoid unnecessary atomic load when debug disabled
	if s.logger.GetLevel() <= zerolog.DebugLevel {
		s.logger.Debug().
			Str("client_ip", clientIP).
			Int64("current_connections", s.stats.CurrentConnections.Load()).
			Int("max_connections", s.maxConns).
			Msg("ResourceGuard accepted connection")
	}

	// NOTE: Authentication is now handled by ws-gateway (proxy layer)
	// ws-server is a dumb broadcaster - no auth logic here
	// Network security is enforced via Kubernetes NetworkPolicy

	// Try to acquire connection slot (non-blocking)
	// Rejects immediately at capacity rather than queueing indefinitely
	select {
	case s.connectionsSem <- struct{}{}:
		// Acquired connection slot
	default:
		s.logger.Warn().
			Str("client_ip", clientIP).
			Int("max_connections", s.maxConns).
			Msg("Connection rejected: at capacity")
		metrics.ConnectionsFailed.Inc()
		http.Error(w, "Server at capacity", http.StatusServiceUnavailable)
		return
	}

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
		s.alertLogger.Error("WebSocketUpgradeFailed", "Failed to upgrade HTTP connection to WebSocket", map[string]any{
			"error":            err.Error(),
			"remoteAddr":       r.RemoteAddr,
			"client_ip":        clientIP,
			"upgrade_duration": upgradeDuration.String(),
			"total_elapsed":    time.Since(startTime).String(),
		})
		metrics.ConnectionsFailed.Inc()
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
	client.id = s.clientCount.Add(1)
	client.remoteAddr = clientIP

	s.clients.Store(client, true)
	s.stats.TotalConnections.Add(1)
	currentConns := s.stats.CurrentConnections.Add(1)

	// Update Prometheus metrics
	metrics.UpdateConnectionMetrics()

	// DEBUG: Client fully initialized, starting pumps
	s.logger.Info().
		Str("client_ip", clientIP).
		Int64("client_id", client.id).
		Int64("current_connections", currentConns).
		Dur("total_setup_time_ms", time.Since(startTime)).
		Msg("Client connected successfully - pumps starting")

	s.wg.Add(2)
	go s.writePump(client)
	go s.readPump(client)
}

// disconnectClient handles client disconnect with proper instrumentation
// Centralizes all disconnect logic to ensure consistent metrics and logging
