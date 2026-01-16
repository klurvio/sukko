package gateway

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gobwas/ws"
	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog"

	"github.com/Toniq-Labs/odin-ws/internal/auth"
)

// Gateway handles WebSocket connections, authenticating clients and proxying
// to the ws-server backend with permission-based channel filtering.
type Gateway struct {
	config       *Config
	jwtValidator *auth.JWTValidator
	permissions  *PermissionChecker
	logger       zerolog.Logger
}

// New creates a new Gateway instance.
func New(config *Config, logger zerolog.Logger) *Gateway {
	// Only create JWT validator if auth is enabled
	var jwtValidator *auth.JWTValidator
	if config.AuthEnabled {
		jwtValidator = auth.NewJWTValidator(config.JWTSecret)
	}

	return &Gateway{
		config:       config,
		jwtValidator: jwtValidator,
		permissions: NewPermissionChecker(
			config.PublicPatterns,
			config.UserScopedPatterns,
			config.GroupScopedPatterns,
		),
		logger: logger.With().Str("component", "gateway").Logger(),
	}
}

// HandleWebSocket handles incoming WebSocket upgrade requests.
// Authenticates the client, upgrades to WebSocket, and proxies to backend.
func (gw *Gateway) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	remoteAddr := r.RemoteAddr

	var claims *auth.Claims

	if gw.config.AuthEnabled {
		// Extract token from query parameter or Authorization header
		token := gw.extractToken(r)
		if token == "" {
			gw.logger.Warn().
				Str("remote_addr", remoteAddr).
				Msg("Connection rejected: no token provided")
			http.Error(w, "Unauthorized: token required", http.StatusUnauthorized)
			return
		}

		// Validate JWT token
		var err error
		claims, err = gw.jwtValidator.ValidateToken(token)
		if err != nil {
			gw.logger.Warn().
				Err(err).
				Str("remote_addr", remoteAddr).
				Msg("Connection rejected: invalid token")
			http.Error(w, "Unauthorized: invalid token", http.StatusUnauthorized)
			return
		}

		gw.logger.Debug().
			Str("principal", claims.Subject).
			Str("tenant_id", claims.TenantID).
			Strs("groups", claims.Groups).
			Str("remote_addr", remoteAddr).
			Msg("Token validated successfully")
	} else {
		// Auth disabled - create anonymous claims
		claims = &auth.Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject: "anonymous",
			},
			TenantID: "anonymous",
			Groups:   []string{},
		}
		gw.logger.Debug().
			Str("remote_addr", remoteAddr).
			Msg("Auth disabled - allowing anonymous connection")
	}

	// Upgrade client connection to WebSocket using gobwas/ws
	clientConn, _, _, err := ws.UpgradeHTTP(r, w)
	if err != nil {
		gw.logger.Warn().
			Err(err).
			Str("remote_addr", remoteAddr).
			Msg("WebSocket upgrade failed")
		return
	}
	defer func() { _ = clientConn.Close() }()

	// Connect to backend ws-server using gobwas/ws
	ctx, cancel := context.WithTimeout(context.Background(), gw.config.DialTimeout)
	defer cancel()

	backendConn, _, _, err := ws.Dial(ctx, gw.config.BackendURL)
	if err != nil {
		gw.logger.Error().
			Err(err).
			Str("backend_url", gw.config.BackendURL).
			Str("remote_addr", remoteAddr).
			Msg("Failed to connect to backend")

		// Send close frame to client
		closeFrame := ws.NewCloseFrameBody(ws.StatusInternalServerError, "Backend unavailable")
		_ = ws.WriteFrame(clientConn, ws.NewCloseFrame(closeFrame))
		return
	}
	defer func() { _ = backendConn.Close() }()

	gw.logger.Info().
		Str("principal", claims.Subject).
		Str("tenant_id", claims.TenantID).
		Str("remote_addr", remoteAddr).
		Dur("connect_time", time.Since(startTime)).
		Msg("Client connected and proxying to backend")

	// Create and run proxy
	proxy := NewProxy(
		clientConn,
		backendConn,
		claims,
		gw.permissions,
		gw.logger.With().Str("principal", claims.Subject).Logger(),
		gw.config.MessageTimeout,
	)
	proxy.Run()

	gw.logger.Info().
		Str("principal", claims.Subject).
		Dur("session_duration", time.Since(startTime)).
		Msg("Client disconnected")
}

// HandleHealth handles health check requests.
func (gw *Gateway) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok","service":"ws-gateway"}`))
}

// extractToken extracts the JWT token from the request.
// Checks query parameter first, then Authorization header.
func (gw *Gateway) extractToken(r *http.Request) string {
	// Check query parameter
	if token := r.URL.Query().Get("token"); token != "" {
		return token
	}

	// Check Authorization header
	authHeader := r.Header.Get("Authorization")
	if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
		return authHeader[7:]
	}

	return ""
}

// NewServer creates an HTTP server for the gateway.
func (gw *Gateway) NewServer() *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", gw.HandleWebSocket)
	mux.HandleFunc("/health", gw.HandleHealth)

	return &http.Server{
		Addr:         fmt.Sprintf(":%d", gw.config.Port),
		Handler:      mux,
		ReadTimeout:  gw.config.ReadTimeout,
		WriteTimeout: gw.config.WriteTimeout,
		IdleTimeout:  gw.config.IdleTimeout,
	}
}
