// Package gateway provides WebSocket connection handling with authentication,
// proxying to backend servers, and permission-based channel filtering.
package gateway

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/gobwas/ws"
	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog"

	"github.com/Toniq-Labs/odin-ws/internal/auth"
	"github.com/Toniq-Labs/odin-ws/internal/platform"

	// PostgreSQL driver
	_ "github.com/lib/pq"
)

// Gateway handles WebSocket connections, authenticating clients and proxying
// to the ws-server backend with permission-based channel filtering.
type Gateway struct {
	config      *platform.GatewayConfig
	validator   *auth.MultiTenantValidator
	keyRegistry auth.KeyRegistry        // For cleanup on shutdown
	dbConn      *sql.DB                 // For cleanup on shutdown
	oidcCloser  *auth.OIDCKeyfuncResult // For OIDC keyfunc cleanup on shutdown
	permissions *PermissionChecker
	connTracker *TenantConnectionTracker // Per-tenant connection tracking
	logger      zerolog.Logger
}

// New creates a new Gateway instance.
// For multi-tenant mode, this opens a database connection and starts key cache refresh.
// Call Close() to release resources when shutting down.
func New(config *platform.GatewayConfig, logger zerolog.Logger) (*Gateway, error) {
	gw := &Gateway{
		config: config,
		permissions: NewPermissionChecker(
			config.PublicPatterns,
			config.UserScopedPatterns,
			config.GroupScopedPatterns,
		),
		logger: logger.With().Str("component", "gateway").Logger(),
	}

	// Set up per-tenant connection tracking if enabled
	if config.TenantConnectionLimitEnabled {
		gw.connTracker = NewTenantConnectionTracker(config.DefaultTenantConnectionLimit)
		gw.logger.Info().
			Int("default_limit", config.DefaultTenantConnectionLimit).
			Msg("Per-tenant connection limits enabled")
	}

	// Set up validator if auth is enabled
	if config.AuthEnabled {
		if err := gw.setupValidator(); err != nil {
			return nil, fmt.Errorf("setup validator: %w", err)
		}
	}

	return gw, nil
}

// setupValidator configures the multi-tenant JWT validator with asymmetric keys.
func (gw *Gateway) setupValidator() error {
	// Open database connection
	db, err := sql.Open("postgres", gw.config.ProvisioningDBURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}

	// Configure connection pool from config
	db.SetMaxOpenConns(gw.config.DBMaxOpenConns)
	db.SetMaxIdleConns(gw.config.DBMaxIdleConns)
	db.SetConnMaxLifetime(gw.config.DBConnMaxLifetime)
	db.SetConnMaxIdleTime(gw.config.DBConnMaxIdleTime)

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), gw.config.DBPingTimeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("ping database: %w", err)
	}
	gw.dbConn = db

	// Create key registry with background refresh
	keyRegistry, err := auth.NewPostgresKeyRegistry(auth.PostgresKeyRegistryConfig{
		DB:              db,
		RefreshInterval: gw.config.KeyCacheRefreshInterval,
		QueryTimeout:    gw.config.KeyCacheQueryTimeout,
		Logger:          gw.logger.With().Str("component", "key_registry").Logger(),
		Metrics:         &KeyCacheMetricsAdapter{},
	})
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("create key registry: %w", err)
	}
	gw.keyRegistry = keyRegistry

	// Build validator config
	validatorCfg := auth.MultiTenantValidatorConfig{
		KeyRegistry:     keyRegistry,
		RequireTenantID: gw.config.RequireTenantID,
		RequireKeyID:    true,
	}

	// Set up OIDC keyfunc if configured (graceful degradation on failure)
	if gw.config.OIDCEnabled() {
		oidcResult, err := auth.NewOIDCKeyfunc(context.Background(), auth.OIDCConfig{
			IssuerURL: gw.config.OIDCIssuerURL,
			JWKSURL:   gw.config.OIDCJWKSURL,
			Audience:  gw.config.OIDCAudience,
		}, gw.logger.With().Str("component", "oidc").Logger())

		if err != nil {
			// Graceful degradation: log warning but continue without OIDC
			gw.logger.Warn().
				Err(err).
				Str("jwks_url", gw.config.OIDCJWKSURL).
				Msg("Failed to create OIDC keyfunc, continuing without OIDC support")
		} else {
			gw.oidcCloser = oidcResult
			validatorCfg.OIDCKeyfunc = oidcResult.Keyfunc
			validatorCfg.OIDCIssuer = gw.config.OIDCIssuerURL
			validatorCfg.OIDCAudience = gw.config.OIDCAudience

			gw.logger.Info().
				Str("issuer_url", gw.config.OIDCIssuerURL).
				Str("jwks_url", gw.config.OIDCJWKSURL).
				Msg("OIDC support enabled")
		}
	}

	// Create multi-tenant validator
	validator, err := auth.NewMultiTenantValidator(validatorCfg)
	if err != nil {
		if gw.oidcCloser != nil {
			gw.oidcCloser.Close()
		}
		_ = keyRegistry.Close()
		_ = db.Close()
		return fmt.Errorf("create validator: %w", err)
	}
	gw.validator = validator

	gw.logger.Info().
		Dur("cache_refresh_interval", gw.config.KeyCacheRefreshInterval).
		Bool("require_tenant_id", gw.config.RequireTenantID).
		Bool("oidc_enabled", gw.config.OIDCEnabled()).
		Int("db_max_open_conns", gw.config.DBMaxOpenConns).
		Msg("Configured multi-tenant authentication")

	return nil
}

// Close releases resources held by the gateway.
// Should be called during shutdown.
func (gw *Gateway) Close() error {
	var errs []error

	// Close OIDC keyfunc (stops background JWKS refresh)
	if gw.oidcCloser != nil {
		gw.oidcCloser.Close()
	}

	if gw.keyRegistry != nil {
		if err := gw.keyRegistry.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close key registry: %w", err))
		}
	}

	if gw.dbConn != nil {
		if err := gw.dbConn.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close database: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}
	return nil
}

// HandleWebSocket handles incoming WebSocket upgrade requests.
// Authenticates the client, upgrades to WebSocket, and proxies to backend.
func (gw *Gateway) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	remoteAddr := r.RemoteAddr
	ctx := r.Context()

	// Record connection attempt and track disconnect reason for metrics
	RecordConnection()
	closeReason := "normal"
	defer func() {
		RecordDisconnection(closeReason, time.Since(startTime))
	}()

	var claims *auth.Claims

	if gw.config.AuthEnabled {
		authStart := time.Now()
		// Extract token from query parameter or Authorization header
		token := gw.extractToken(r)
		if token == "" {
			RecordAuthValidation("failed", time.Since(authStart))
			closeReason = "no_token"
			gw.logger.Warn().
				Str("remote_addr", remoteAddr).
				Msg("Connection rejected: no token provided")
			http.Error(w, "Unauthorized: token required", http.StatusUnauthorized)
			return
		}

		// Validate JWT token using multi-tenant validator
		var err error
		claims, err = gw.validator.ValidateToken(ctx, token)
		if err != nil {
			RecordAuthValidation("failed", time.Since(authStart))
			closeReason = "invalid_token"
			gw.logger.Warn().
				Err(err).
				Str("remote_addr", remoteAddr).
				Msg("Connection rejected: invalid token")
			http.Error(w, "Unauthorized: invalid token", http.StatusUnauthorized)
			return
		}
		RecordAuthValidation("success", time.Since(authStart))

		gw.logger.Debug().
			Str("principal", claims.Subject).
			Str("tenant_id", claims.TenantID).
			Strs("groups", claims.Groups).
			Str("remote_addr", remoteAddr).
			Msg("Token validated successfully")
	} else {
		RecordAuthValidation("skipped", 0)
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

	// Check per-tenant connection limits
	if gw.connTracker != nil && claims.TenantID != "" {
		if !gw.connTracker.TryAcquire(claims.TenantID) {
			closeReason = "tenant_limit_exceeded"
			gw.logger.Warn().
				Str("tenant_id", claims.TenantID).
				Str("remote_addr", remoteAddr).
				Int64("current_connections", gw.connTracker.GetConnectionCount(claims.TenantID)).
				Int("limit", gw.connTracker.GetLimit(claims.TenantID)).
				Msg("Connection rejected: tenant connection limit exceeded")
			http.Error(w, "Too Many Connections: tenant limit exceeded", http.StatusTooManyRequests)
			return
		}
		// Ensure we release the connection slot on exit
		defer gw.connTracker.Release(claims.TenantID)
	}

	// Upgrade client connection to WebSocket using gobwas/ws
	clientConn, _, _, err := ws.UpgradeHTTP(r, w)
	if err != nil {
		closeReason = "upgrade_failed"
		gw.logger.Warn().
			Err(err).
			Str("remote_addr", remoteAddr).
			Msg("WebSocket upgrade failed")
		return
	}
	defer func() { _ = clientConn.Close() }()

	// Connect to backend ws-server using gobwas/ws
	dialCtx, cancel := context.WithTimeout(ctx, gw.config.DialTimeout)
	defer cancel()

	dialStart := time.Now()
	backendConn, _, _, err := ws.Dial(dialCtx, gw.config.BackendURL)
	if err != nil {
		RecordBackendConnect("failed", time.Since(dialStart))
		closeReason = "backend_unavailable"
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
	RecordBackendConnect("success", time.Since(dialStart))
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
func (gw *Gateway) HandleHealth(w http.ResponseWriter, _ *http.Request) {
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
	mux.HandleFunc("/metrics", HandleMetrics)

	return &http.Server{
		Addr:         fmt.Sprintf(":%d", gw.config.Port),
		Handler:      mux,
		ReadTimeout:  gw.config.ReadTimeout,
		WriteTimeout: gw.config.WriteTimeout,
		IdleTimeout:  gw.config.IdleTimeout,
	}
}
