// Package gateway provides WebSocket connection handling with authentication,
// proxying to backend servers, and permission-based channel filtering.
package gateway

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gobwas/ws"
	"github.com/rs/zerolog"

	"github.com/Toniq-Labs/odin-ws/internal/shared/auth"
	"github.com/Toniq-Labs/odin-ws/internal/shared/httputil"
	"github.com/Toniq-Labs/odin-ws/internal/shared/platform"
	"github.com/Toniq-Labs/odin-ws/internal/shared/provapi"
	"github.com/Toniq-Labs/odin-ws/internal/shared/version"
)

// Gateway handles WebSocket connections, authenticating clients and proxying
// to the ws-server backend with permission-based channel filtering.
type Gateway struct {
	config    *platform.GatewayConfig
	validator *auth.MultiTenantValidator

	// gRPC stream registries for provisioning data (keys, OIDC, channel rules)
	streamKeyRegistry    *provapi.StreamKeyRegistry
	streamTenantRegistry *provapi.StreamTenantRegistry

	oidcCloser  *auth.OIDCKeyfuncResult // For OIDC keyfunc cleanup on shutdown
	permissions *PermissionChecker
	connTracker *TenantConnectionTracker // Per-tenant connection tracking
	logger      zerolog.Logger

	// Multi-issuer OIDC support (optional, enabled via config)
	tenantRegistry    TenantRegistry           // Points to streamTenantRegistry (interface view)
	multiIssuerOIDC   *MultiIssuerOIDC         // Dynamic JWKS management per issuer
	tenantPermChecker *TenantPermissionChecker // Per-tenant channel authorization
}

// New creates a new Gateway instance.
// For multi-tenant mode, this connects to the provisioning service via gRPC streaming.
// Call Close() to release resources when shutting down.
func New(config *platform.GatewayConfig, logger zerolog.Logger) (*Gateway, error) {
	gw := &Gateway{
		config: config,
		logger: logger.With().Str("component", "gateway").Logger(),
	}

	// Only create permission checker when auth is enabled (used for channel filtering)
	if config.AuthEnabled {
		gw.permissions = NewPermissionChecker(
			config.PublicPatterns,
			config.UserScopedPatterns,
			config.GroupScopedPatterns,
		)
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
	} else {
		gw.logger.Warn().
			Str("default_tenant_id", config.DefaultTenantID).
			Msg("Auth disabled — all connections treated as anonymous, routed to default tenant")
	}

	// Set up per-tenant channel rules if enabled (requires auth to be enabled first for tenant registry)
	if config.PerTenantChannelRulesEnabled && gw.tenantRegistry != nil {
		fallbackRules := DefaultChannelRules(config.FallbackPublicChannels)
		gw.tenantPermChecker = NewTenantPermissionChecker(
			gw.tenantRegistry,
			fallbackRules,
			gw.logger.With().Str("component", "tenant_permissions").Logger(),
		)
		gw.logger.Info().
			Int("fallback_patterns", len(config.FallbackPublicChannels)).
			Msg("Per-tenant channel rules enabled")
	}

	return gw, nil
}

// setupValidator configures the multi-tenant JWT validator with asymmetric keys.
// Keys and tenant configs are streamed from the provisioning service via gRPC.
func (gw *Gateway) setupValidator() error {
	// Create gRPC stream-backed key registry
	keyRegistry, err := provapi.NewStreamKeyRegistry(provapi.StreamKeyRegistryConfig{
		GRPCAddr:          gw.config.ProvisioningGRPCAddr,
		ReconnectDelay:    gw.config.GRPCReconnectDelay,
		ReconnectMaxDelay: gw.config.GRPCReconnectMaxDelay,
		MetricPrefix:      "gateway",
		Logger:            gw.logger.With().Str("component", "key_registry").Logger(),
	})
	if err != nil {
		return fmt.Errorf("create stream key registry: %w", err)
	}
	gw.streamKeyRegistry = keyRegistry

	// Create gRPC stream-backed tenant registry (for OIDC configs + channel rules)
	tenantRegistry, err := provapi.NewStreamTenantRegistry(provapi.StreamTenantRegistryConfig{
		GRPCAddr:          gw.config.ProvisioningGRPCAddr,
		ReconnectDelay:    gw.config.GRPCReconnectDelay,
		ReconnectMaxDelay: gw.config.GRPCReconnectMaxDelay,
		MetricPrefix:      "gateway",
		Logger:            gw.logger.With().Str("component", "tenant_registry").Logger(),
	})
	if err != nil {
		_ = keyRegistry.Close()
		return fmt.Errorf("create stream tenant registry: %w", err)
	}
	gw.streamTenantRegistry = tenantRegistry
	gw.tenantRegistry = tenantRegistry

	// Build validator config
	validatorCfg := auth.MultiTenantValidatorConfig{
		KeyRegistry:     keyRegistry,
		RequireTenantID: gw.config.RequireTenantID,
		RequireKeyID:    true,
	}

	// Set up multi-issuer OIDC if enabled
	if gw.config.MultiIssuerOIDCEnabled {
		if err := gw.setupMultiIssuerOIDC(); err != nil {
			// Graceful degradation: log warning but continue without multi-issuer
			gw.logger.Warn().
				Err(err).
				Msg("Failed to setup multi-issuer OIDC, continuing with single-issuer or tenant keys only")
		}
	}

	// Set up single-issuer OIDC keyfunc if configured (graceful degradation on failure)
	// This is the legacy single-issuer mode, used when multi-issuer is not enabled
	if gw.config.OIDCEnabled() && !gw.config.MultiIssuerOIDCEnabled {
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
				Msg("OIDC support enabled (single-issuer mode)")
		}
	}

	// Create multi-tenant validator
	validator, err := auth.NewMultiTenantValidator(validatorCfg)
	if err != nil {
		if gw.oidcCloser != nil {
			gw.oidcCloser.Close()
		}
		_ = tenantRegistry.Close()
		_ = keyRegistry.Close()
		return fmt.Errorf("create validator: %w", err)
	}
	gw.validator = validator

	gw.logger.Info().
		Str("provisioning_grpc_addr", gw.config.ProvisioningGRPCAddr).
		Bool("require_tenant_id", gw.config.RequireTenantID).
		Bool("oidc_enabled", gw.config.OIDCEnabled()).
		Bool("multi_issuer_oidc_enabled", gw.config.MultiIssuerOIDCEnabled).
		Msg("Configured multi-tenant authentication via gRPC streaming")

	return nil
}

// setupMultiIssuerOIDC sets up the MultiIssuerOIDC component using the gRPC
// stream-backed tenant registry (already created in setupValidator).
func (gw *Gateway) setupMultiIssuerOIDC() error {
	// Create MultiIssuerOIDC for dynamic JWKS management
	multiOIDC, err := NewMultiIssuerOIDC(MultiIssuerOIDCConfig{
		Registry:         gw.streamTenantRegistry,
		KeyfuncCacheTTL:  gw.config.OIDCKeyfuncCacheTTL,
		JWKSFetchTimeout: gw.config.JWKSFetchTimeout,
		Logger:           gw.logger.With().Str("component", "multi_issuer_oidc").Logger(),
	})
	if err != nil {
		return fmt.Errorf("create multi-issuer OIDC: %w", err)
	}
	gw.multiIssuerOIDC = multiOIDC

	gw.logger.Info().
		Dur("keyfunc_cache_ttl", gw.config.OIDCKeyfuncCacheTTL).
		Dur("jwks_fetch_timeout", gw.config.JWKSFetchTimeout).
		Msg("Multi-issuer OIDC enabled with gRPC stream-backed TenantRegistry")

	return nil
}

// Close releases resources held by the gateway.
// Should be called during shutdown.
func (gw *Gateway) Close() error {
	var errs []error

	// Close multi-issuer OIDC (stops background JWKS refresh for all issuers)
	if gw.multiIssuerOIDC != nil {
		if err := gw.multiIssuerOIDC.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close multi-issuer OIDC: %w", err))
		}
	}

	// Close OIDC keyfunc (stops background JWKS refresh for single-issuer mode)
	if gw.oidcCloser != nil {
		gw.oidcCloser.Close()
	}

	// Close gRPC stream registries (stops background streams + closes gRPC connections)
	if gw.streamTenantRegistry != nil {
		if err := gw.streamTenantRegistry.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close stream tenant registry: %w", err))
		}
	}

	if gw.streamKeyRegistry != nil {
		if err := gw.streamKeyRegistry.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close stream key registry: %w", err))
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

	// Resolve identity (principal, tenantID) and auth (claims) separately.
	// When auth disabled: claims is nil, principal/tenantID come from config.
	// When auth enabled: all three come from the validated JWT.
	var claims *auth.Claims
	var principal string
	var tenantID string

	if gw.config.AuthEnabled {
		authStart := time.Now()
		// Extract token from query parameter or Authorization header
		token := httputil.ExtractBearerToken(r)
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

		principal = claims.Subject
		tenantID = claims.TenantID

		gw.logger.Debug().
			Str("principal", principal).
			Str("tenant_id", tenantID).
			Strs("groups", claims.Groups).
			Str("remote_addr", remoteAddr).
			Msg("Token validated successfully")
	} else {
		RecordAuthValidation("skipped", 0)
		// No auth = no claims object
		claims = nil
		principal = "anonymous"
		tenantID = gw.config.DefaultTenantID

		gw.logger.Debug().
			Str("principal", principal).
			Str("tenant_id", tenantID).
			Str("remote_addr", remoteAddr).
			Msg("Auth disabled - allowing anonymous connection")
	}

	// Check per-tenant connection limits
	if gw.connTracker != nil && tenantID != "" {
		if !gw.connTracker.TryAcquire(tenantID) {
			closeReason = "tenant_limit_exceeded"
			gw.logger.Warn().
				Str("tenant_id", tenantID).
				Str("remote_addr", remoteAddr).
				Int64("current_connections", gw.connTracker.GetConnectionCount(tenantID)).
				Int("limit", gw.connTracker.GetLimit(tenantID)).
				Msg("Connection rejected: tenant connection limit exceeded")
			http.Error(w, "Too Many Connections: tenant limit exceeded", http.StatusTooManyRequests)
			return
		}
		// Ensure we release the connection slot on exit
		defer gw.connTracker.Release(tenantID)
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
		Str("principal", principal).
		Str("tenant_id", tenantID).
		Str("remote_addr", remoteAddr).
		Dur("connect_time", time.Since(startTime)).
		Msg("Client connected and proxying to backend")

	// Create and run proxy
	proxy := NewProxy(ProxyConfig{
		ClientConn:              clientConn,
		BackendConn:             backendConn,
		AuthEnabled:             gw.config.AuthEnabled,
		Claims:                  claims, // nil when auth disabled — proxy won't use it
		TenantID:                tenantID,
		Permissions:             gw.permissions,
		Validator:               gw.validator,
		AuthRefreshRateInterval: gw.config.AuthRefreshRateInterval,
		Logger:                  gw.logger.With().Str("principal", principal).Logger(),
		MessageTimeout:          gw.config.MessageTimeout,
		PublishRateLimit:        gw.config.PublishRateLimit,
		PublishBurst:            gw.config.PublishBurst,
		MaxPublishSize:          gw.config.MaxPublishSize,
	})
	proxy.Run()

	gw.logger.Info().
		Str("principal", principal).
		Dur("session_duration", time.Since(startTime)).
		Msg("Client disconnected")
}

// HandleHealth handles health check requests.
// Reports stream states for provisioning registries.
func (gw *Gateway) HandleHealth(w http.ResponseWriter, _ *http.Request) {
	status := "ok"
	keysStream := "connected"
	configStream := "connected"

	if gw.streamKeyRegistry != nil && gw.streamKeyRegistry.State() == 0 {
		status = "degraded"
		keysStream = "disconnected"
	}
	if gw.streamTenantRegistry != nil && gw.streamTenantRegistry.State() == 0 {
		status = "degraded"
		configStream = "disconnected"
	}

	// Always return 200 to avoid unnecessary restarts during transient disconnects
	_ = httputil.WriteJSON(w, http.StatusOK, map[string]string{
		"status":                    status,
		"service":                   "ws-gateway",
		"provisioning_keys_stream":  keysStream,
		"provisioning_config_stream": configStream,
	})
}

// NewServer creates an HTTP server for the gateway.
func (gw *Gateway) NewServer() *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", gw.HandleWebSocket)
	mux.HandleFunc("/health", gw.HandleHealth)
	mux.HandleFunc("/version", version.Handler("gateway"))
	mux.HandleFunc("/metrics", HandleMetrics)

	return &http.Server{
		Addr:         fmt.Sprintf(":%d", gw.config.Port),
		Handler:      mux,
		ReadTimeout:  gw.config.ReadTimeout,
		WriteTimeout: gw.config.WriteTimeout,
		IdleTimeout:  gw.config.IdleTimeout,
	}
}
