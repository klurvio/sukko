// Package gateway provides WebSocket connection handling with authentication,
// proxying to backend servers, and permission-based channel filtering.
package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gobwas/ws"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/shared/auth"
	"github.com/klurvio/sukko/internal/shared/httputil"
	"github.com/klurvio/sukko/internal/shared/license"
	pkgmetrics "github.com/klurvio/sukko/internal/shared/metrics"
	"github.com/klurvio/sukko/internal/shared/platform"
	"github.com/klurvio/sukko/internal/shared/provapi"
	"github.com/klurvio/sukko/internal/shared/version"
)

// Gateway handles WebSocket connections, authenticating clients and proxying
// to the ws-server backend with permission-based channel filtering.
type Gateway struct {
	config    *platform.GatewayConfig
	validator *auth.MultiTenantValidator

	// gRPC stream registries for provisioning data (keys, channel rules, API keys)
	streamKeyRegistry    *provapi.StreamKeyRegistry
	streamChannelRules   *provapi.StreamChannelRulesProvider
	streamAPIKeyRegistry *provapi.StreamAPIKeyRegistry // concrete for State() in health

	apiKeyRegistry       APIKeyLookup // interface for Lookup() + mock injection in tests
	channelRulesProvider ChannelRulesProvider
	permissions          *PermissionChecker
	connTracker          *TenantConnectionTracker // Per-tenant connection tracking
	tenantPermChecker    *TenantPermissionChecker // Per-tenant channel authorization
	logger               zerolog.Logger
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

	// Set up per-tenant channel rules if enabled (requires auth to be enabled first for channel rules provider)
	if config.PerTenantChannelRulesEnabled && gw.channelRulesProvider == nil {
		gw.logger.Warn().
			Bool("per_tenant_channel_rules_enabled", true).
			Bool("auth_enabled", config.AuthEnabled).
			Msg("Per-tenant channel rules enabled but channel rules provider not available (requires auth); feature inactive")
	}
	if config.PerTenantChannelRulesEnabled && gw.channelRulesProvider != nil {
		fallbackRules := DefaultChannelRules(config.FallbackPublicChannels)
		permChecker, err := NewTenantPermissionChecker(
			gw.channelRulesProvider,
			fallbackRules,
			gw.logger.With().Str("component", "tenant_permissions").Logger(),
		)
		if err != nil {
			return nil, fmt.Errorf("create tenant permission checker: %w", err)
		}
		gw.tenantPermChecker = permChecker
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

	// Create gRPC stream-backed API key registry
	apiKeyRegistry, err := provapi.NewStreamAPIKeyRegistry(provapi.StreamAPIKeyRegistryConfig{
		GRPCAddr:          gw.config.ProvisioningGRPCAddr,
		ReconnectDelay:    gw.config.GRPCReconnectDelay,
		ReconnectMaxDelay: gw.config.GRPCReconnectMaxDelay,
		MetricPrefix:      "gateway",
		Logger:            gw.logger.With().Str("component", "api_key_registry").Logger(),
	})
	if err != nil {
		_ = keyRegistry.Close() // best-effort cleanup during construction failure
		return fmt.Errorf("create stream api key registry: %w", err)
	}
	gw.streamAPIKeyRegistry = apiKeyRegistry
	gw.apiKeyRegistry = apiKeyRegistry

	// Create gRPC stream-backed channel rules provider
	channelRulesProvider, err := provapi.NewStreamChannelRulesProvider(provapi.StreamChannelRulesProviderConfig{
		GRPCAddr:          gw.config.ProvisioningGRPCAddr,
		ReconnectDelay:    gw.config.GRPCReconnectDelay,
		ReconnectMaxDelay: gw.config.GRPCReconnectMaxDelay,
		MetricPrefix:      "gateway",
		Logger:            gw.logger.With().Str("component", "channel_rules_provider").Logger(),
	})
	if err != nil {
		_ = apiKeyRegistry.Close() // best-effort cleanup during construction failure
		_ = keyRegistry.Close()    // best-effort cleanup during construction failure
		return fmt.Errorf("create stream channel rules provider: %w", err)
	}
	gw.streamChannelRules = channelRulesProvider
	gw.channelRulesProvider = channelRulesProvider

	// Build validator config
	validatorCfg := auth.MultiTenantValidatorConfig{
		KeyRegistry:     keyRegistry,
		RequireTenantID: gw.config.RequireTenantID,
	}

	// Create multi-tenant validator
	validator, err := auth.NewMultiTenantValidator(validatorCfg)
	if err != nil {
		_ = channelRulesProvider.Close() // best-effort cleanup during construction failure
		_ = apiKeyRegistry.Close()       // best-effort cleanup during construction failure
		_ = keyRegistry.Close()          // best-effort cleanup during construction failure
		return fmt.Errorf("create validator: %w", err)
	}
	gw.validator = validator

	gw.logger.Info().
		Str("provisioning_grpc_addr", gw.config.ProvisioningGRPCAddr).
		Bool("require_tenant_id", gw.config.RequireTenantID).
		Msg("Configured multi-tenant authentication via gRPC streaming")

	return nil
}

// Close releases resources held by the gateway.
// Should be called during shutdown.
func (gw *Gateway) Close() error {
	var errs []error

	// Close gRPC stream registries (stops background streams + closes gRPC connections)
	if gw.streamChannelRules != nil {
		if err := gw.streamChannelRules.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close stream channel rules provider: %w", err))
		}
	}

	if gw.streamAPIKeyRegistry != nil {
		if err := gw.streamAPIKeyRegistry.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close stream api key registry: %w", err))
		}
	}

	if gw.streamKeyRegistry != nil {
		if err := gw.streamKeyRegistry.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close stream key registry: %w", err))
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
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
	closeReason := CloseReasonNormal
	defer func() {
		RecordDisconnection(closeReason, time.Since(startTime))
	}()

	// Resolve identity (principal, tenantID) and auth (claims) separately.
	// When auth disabled: claims is nil, principal/tenantID come from config.
	// When auth enabled: 4-way branching on credentials provided:
	//   - Neither token nor API key → reject
	//   - API key only → public channels, no publish, can escalate via auth refresh
	//   - JWT only → full access per claims
	//   - Both → validate both, verify tenant match
	var claims *auth.Claims
	var principal string
	var tenantID string
	var apiKeyOnly bool
	var apiKeyTenantID string

	if gw.config.AuthEnabled {
		authStart := time.Now()
		token := httputil.ExtractBearerToken(r)
		apiKey := httputil.ExtractAPIKey(r)

		switch {
		case token == "" && apiKey == "":
			// Neither credential provided
			RecordAuthValidation(pkgmetrics.AuthStatusFailed, time.Since(authStart))
			closeReason = CloseReasonNoCredentials
			gw.logger.Warn().
				Str("remote_addr", remoteAddr).
				Msg("Connection rejected: no credentials provided")
			httputil.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "token or api_key required")
			return

		case apiKey != "" && token == "":
			// API key only — public channels, no publish, can escalate via auth refresh
			info, ok := gw.apiKeyRegistry.Lookup(apiKey)
			if !ok {
				RecordAPIKeyAuth(APIKeyAuthInvalid)
				RecordAuthValidation(pkgmetrics.AuthStatusFailed, time.Since(authStart))
				closeReason = CloseReasonInvalidAPIKey
				gw.logger.Warn().
					Str("remote_addr", remoteAddr).
					Msg("Connection rejected: invalid API key")
				httputil.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid api key")
				return
			}
			RecordAPIKeyAuth(APIKeyAuthAccepted)
			RecordAuthValidation(pkgmetrics.AuthStatusSuccess, time.Since(authStart))
			tenantID = info.TenantID
			principal = "anon:" + uuid.NewString()
			apiKeyOnly = true
			apiKeyTenantID = info.TenantID
			// claims remains nil — PermissionChecker allows public channels only

			gw.logger.Debug().
				Str("principal", principal).
				Str("tenant_id", tenantID).
				Str("remote_addr", remoteAddr).
				Msg("API key validated successfully")

		case token != "" && apiKey == "":
			// JWT only (existing flow)
			var err error
			claims, err = gw.validator.ValidateToken(ctx, token)
			if err != nil {
				RecordAuthValidation(pkgmetrics.AuthStatusFailed, time.Since(authStart))
				closeReason = CloseReasonInvalidToken
				gw.logger.Warn().
					Err(err).
					Str("remote_addr", remoteAddr).
					Msg("Connection rejected: invalid token")
				httputil.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid token")
				return
			}
			RecordAuthValidation(pkgmetrics.AuthStatusSuccess, time.Since(authStart))
			principal = claims.Subject
			tenantID = claims.TenantID

			gw.logger.Debug().
				Str("principal", principal).
				Str("tenant_id", tenantID).
				Strs("groups", claims.Groups).
				Str("remote_addr", remoteAddr).
				Msg("Token validated successfully")

		default:
			// Both API key and JWT — validate both, verify tenant match
			info, ok := gw.apiKeyRegistry.Lookup(apiKey)
			if !ok {
				RecordAPIKeyAuth(APIKeyAuthInvalid)
				RecordAuthValidation(pkgmetrics.AuthStatusFailed, time.Since(authStart))
				closeReason = CloseReasonInvalidAPIKey
				gw.logger.Warn().
					Str("remote_addr", remoteAddr).
					Msg("Connection rejected: invalid API key")
				httputil.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid api key")
				return
			}
			RecordAPIKeyAuth(APIKeyAuthAccepted)

			var err error
			claims, err = gw.validator.ValidateToken(ctx, token)
			if err != nil {
				RecordAuthValidation(pkgmetrics.AuthStatusFailed, time.Since(authStart))
				closeReason = CloseReasonInvalidToken
				gw.logger.Warn().
					Err(err).
					Str("remote_addr", remoteAddr).
					Msg("Connection rejected: invalid token")
				httputil.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid token")
				return
			}

			// Verify JWT tenant matches API key tenant
			if claims.TenantID != info.TenantID {
				RecordAuthValidation(pkgmetrics.AuthStatusFailed, time.Since(authStart))
				closeReason = CloseReasonAPIKeyTenantMismatch
				gw.logger.Warn().
					Str("jwt_tenant", claims.TenantID).
					Str("api_key_tenant", info.TenantID).
					Str("remote_addr", remoteAddr).
					Msg("Connection rejected: API key and JWT tenant mismatch")
				httputil.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "api key and token tenant mismatch")
				return
			}
			RecordAuthValidation(pkgmetrics.AuthStatusSuccess, time.Since(authStart))
			principal = claims.Subject
			tenantID = claims.TenantID
			apiKeyTenantID = info.TenantID

			gw.logger.Debug().
				Str("principal", principal).
				Str("tenant_id", tenantID).
				Str("remote_addr", remoteAddr).
				Msg("API key and token validated successfully")
		}
	} else {
		RecordAuthValidation(pkgmetrics.AuthStatusSkipped, 0)
		// No auth = no claims object, apiKeyRegistry is nil
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
			closeReason = CloseReasonTenantLimitExceeded
			gw.logger.Warn().
				Str("tenant_id", tenantID).
				Str("remote_addr", remoteAddr).
				Int64("current_connections", gw.connTracker.GetConnectionCount(tenantID)).
				Int("limit", gw.connTracker.GetLimit(tenantID)).
				Msg("Connection rejected: tenant connection limit exceeded")
			httputil.WriteError(w, http.StatusTooManyRequests, "TENANT_LIMIT_EXCEEDED", "tenant connection limit exceeded")
			return
		}
		// Ensure we release the connection slot on exit
		defer gw.connTracker.Release(tenantID)
	}

	// Upgrade client connection to WebSocket using gobwas/ws
	clientConn, _, _, err := ws.UpgradeHTTP(r, w)
	if err != nil {
		closeReason = CloseReasonUpgradeFailed
		gw.logger.Warn().
			Err(err).
			Str("remote_addr", remoteAddr).
			Msg("WebSocket upgrade failed")
		return
	}
	defer func() { _ = clientConn.Close() }() // best-effort: connection is shutting down

	// Connect to backend ws-server using gobwas/ws
	dialCtx, cancel := context.WithTimeout(ctx, gw.config.DialTimeout)
	defer cancel()

	dialStart := time.Now()
	backendConn, _, _, err := ws.Dial(dialCtx, gw.config.BackendURL)
	if err != nil {
		RecordBackendConnect(pkgmetrics.ResultFailed, time.Since(dialStart))
		closeReason = CloseReasonBackendUnavailable
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
	RecordBackendConnect(pkgmetrics.ResultSuccess, time.Since(dialStart))
	defer func() { _ = backendConn.Close() }() // best-effort: connection is shutting down

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
		Claims:                  claims, // nil when auth disabled or API-key-only
		TenantID:                tenantID,
		Permissions:             gw.permissions,
		Validator:               gw.validator,
		AuthRefreshRateInterval: gw.config.AuthRefreshRateInterval,
		AuthValidationTimeout:   gw.config.AuthValidationTimeout,
		Logger:                  gw.logger.With().Str("principal", principal).Logger(),
		MessageTimeout:          gw.config.MessageTimeout,
		PublishRateLimit:        gw.config.PublishRateLimit,
		PublishBurst:            gw.config.PublishBurst,
		MaxPublishSize:          gw.config.MaxPublishSize,
		MaxFrameSize:            gw.config.MaxFrameSize,
		APIKeyOnly:              apiKeyOnly,
		APIKeyTenantID:          apiKeyTenantID,
	})
	proxy.Run(ctx)

	gw.logger.Info().
		Str("principal", principal).
		Dur("session_duration", time.Since(startTime)).
		Msg("Client disconnected")
}

// streamStatus returns the overall status and per-stream states.
func (gw *Gateway) streamStatus() (status, keysStream, configStream, apiKeysStream string) {
	status = "ok"
	keysStream = "connected"
	configStream = "connected"
	apiKeysStream = "connected"

	if gw.streamKeyRegistry != nil {
		if gw.streamKeyRegistry.State() == provapi.StreamStateDisconnected {
			status = "degraded"
			keysStream = "disconnected"
		}
	} else {
		keysStream = "disabled"
	}
	if gw.streamChannelRules != nil {
		if gw.streamChannelRules.State() == provapi.StreamStateDisconnected {
			status = "degraded"
			configStream = "disconnected"
		}
	} else {
		configStream = "disabled"
	}
	if gw.streamAPIKeyRegistry != nil {
		if gw.streamAPIKeyRegistry.State() == provapi.StreamStateDisconnected {
			status = "degraded"
			apiKeysStream = "disconnected"
		}
	} else {
		apiKeysStream = "disabled"
	}
	return
}

// HandleHealth handles liveness checks. Always returns 200 — the process is alive.
// Use /ready for readiness checks that reflect stream connectivity.
func (gw *Gateway) HandleHealth(w http.ResponseWriter, _ *http.Request) {
	status, keysStream, configStream, apiKeysStream := gw.streamStatus()

	_ = httputil.WriteJSON(w, http.StatusOK, map[string]string{
		"status":                       status,
		"service":                      "ws-gateway",
		"provisioning_keys_stream":     keysStream,
		"provisioning_config_stream":   configStream,
		"provisioning_api_keys_stream": apiKeysStream,
	})
}

// HandleReady handles readiness checks. Returns 503 when streams are degraded,
// signaling Kubernetes to stop routing traffic until connectivity is restored.
func (gw *Gateway) HandleReady(w http.ResponseWriter, _ *http.Request) {
	status, keysStream, configStream, apiKeysStream := gw.streamStatus()

	httpStatus := http.StatusOK
	if status == "degraded" {
		httpStatus = http.StatusServiceUnavailable
	}

	_ = httputil.WriteJSON(w, httpStatus, map[string]string{
		"status":                       status,
		"service":                      "ws-gateway",
		"provisioning_keys_stream":     keysStream,
		"provisioning_config_stream":   configStream,
		"provisioning_api_keys_stream": apiKeysStream,
	})
}

// NewServer creates an HTTP server for the gateway.
func (gw *Gateway) NewServer() *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", gw.HandleWebSocket)
	mux.HandleFunc("/health", gw.HandleHealth)
	mux.HandleFunc("/ready", gw.HandleReady)
	edition := "community"
	if gw.config.EditionManager() != nil {
		edition = gw.config.EditionManager().Edition().String()
	}
	mux.HandleFunc("/version", version.Handler("gateway", edition))
	mux.HandleFunc("/edition", license.EditionHandler(gw.config.EditionManager(), nil))
	mux.HandleFunc("/config", platform.ConfigHandler(gw.config))
	mux.HandleFunc("/metrics", HandleMetrics)

	return &http.Server{
		Addr:         fmt.Sprintf(":%d", gw.config.Port),
		Handler:      mux,
		ReadTimeout:  gw.config.ReadTimeout,
		WriteTimeout: gw.config.WriteTimeout,
		IdleTimeout:  gw.config.IdleTimeout,
	}
}
