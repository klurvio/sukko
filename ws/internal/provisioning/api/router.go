// Package api provides HTTP handlers for the provisioning service.
package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/provisioning"
	"github.com/klurvio/sukko/internal/shared/auth"
	"github.com/klurvio/sukko/internal/shared/license"
	"github.com/klurvio/sukko/internal/shared/profiling"
	"github.com/klurvio/sukko/internal/shared/version"
)

// RouterConfig holds configuration for the HTTP router.
type RouterConfig struct {
	Service   *provisioning.Service
	Logger    zerolog.Logger
	RateLimit int // requests per minute

	// AuthEnabled enables JWT authentication for API endpoints.
	// When enabled, Validator must be provided.
	AuthEnabled bool

	// Validator validates JWT tokens. Required when AuthEnabled is true.
	Validator *auth.MultiTenantValidator

	// AdminAuth provides admin token authentication middleware.
	// When set, admin token auth is checked before JWT auth.
	AdminAuth *AdminAuth

	// CORS configuration
	CORSAllowedOrigins []string // Allowed origins (e.g., ["http://localhost:3000"])
	CORSMaxAge         int      // Preflight cache duration in seconds

	// ConfigHandler serves the /config endpoint (set via platform.ConfigHandler)
	ConfigHandler http.HandlerFunc

	// EditionManager for the /edition endpoint (expiry-aware).
	EditionManager *license.Manager

	// PprofEnabled registers /debug/pprof/ handlers when true.
	// Disabled by default (Constitution IX: debug endpoints must be opt-in).
	PprofEnabled bool
}

// NewRouter creates a new HTTP router with all provisioning endpoints.
func NewRouter(cfg RouterConfig) (http.Handler, error) {
	if cfg.AuthEnabled && cfg.Validator == nil {
		return nil, errors.New("router: Validator is required when AuthEnabled is true")
	}

	r := chi.NewRouter()

	// CORS middleware - must be first so preflight OPTIONS requests succeed without auth
	if len(cfg.CORSAllowedOrigins) > 0 {
		r.Use(cors.Handler(cors.Options{
			AllowedOrigins:   cfg.CORSAllowedOrigins,
			AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
			AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-ID"},
			ExposedHeaders:   []string{"X-Request-ID"},
			AllowCredentials: true,
			MaxAge:           cfg.CORSMaxAge,
		}))
	}

	// Middleware stack
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(LoggingMiddleware(cfg.Logger))
	r.Use(middleware.Recoverer)
	r.Use(middleware.SetHeader("Content-Type", "application/json"))

	// Create handler
	h, err := NewHandler(cfg.Service, cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("create handler: %w", err)
	}

	// Health endpoints (no auth required)
	r.Get("/health", h.Health)
	r.Get("/ready", h.Ready)
	r.Get("/version", version.Handler("provisioning"))
	r.Get("/edition", license.EditionHandler(cfg.EditionManager, func(ctx context.Context) *license.EditionUsage {
		count, err := cfg.Service.CountTenants(ctx)
		if err != nil {
			return nil // usage unavailable — edition info still returned without tenant count
		}
		return &license.EditionUsage{Tenants: &count}
	}))
	if cfg.ConfigHandler != nil {
		r.Get("/config", cfg.ConfigHandler)
	}
	r.Get("/metrics", h.Metrics)

	// Register pprof endpoints if enabled (Constitution IX: opt-in only)
	profiling.InitPprof(func(pattern string, handler func(http.ResponseWriter, *http.Request)) {
		r.HandleFunc(pattern, handler)
	}, cfg.PprofEnabled, cfg.Logger)

	// API v1 routes
	r.Route("/api/v1", func(r chi.Router) {
		// Apply rate limiting if configured
		if cfg.RateLimit > 0 {
			r.Use(RateLimitMiddleware(cfg.RateLimit))
		}

		// Apply admin token auth first (falls through to JWT on mismatch)
		if cfg.AdminAuth != nil {
			r.Use(cfg.AdminAuth.Middleware())
		}

		// Apply JWT auth middleware if enabled
		if cfg.AuthEnabled && cfg.Validator != nil {
			r.Use(AuthMiddleware(cfg.Validator, cfg.Logger))
		}

		// Tenant management - requires admin role when auth is enabled
		r.Route("/tenants", func(r chi.Router) {
			// Admin-only operations
			r.Group(func(r chi.Router) {
				if cfg.AuthEnabled {
					r.Use(RequireRole("admin", "system"))
				}
				r.Post("/", h.CreateTenant)
				r.Get("/", h.ListTenants)
			})

			r.Route("/{tenantID}", func(r chi.Router) {
				// Tenant isolation - users can only access their own tenant
				if cfg.AuthEnabled {
					r.Use(RequireTenant())
				}

				r.Get("/", h.GetTenant)
				r.Patch("/", h.UpdateTenant)

				// Admin-only operations
				r.Group(func(r chi.Router) {
					if cfg.AuthEnabled {
						r.Use(RequireRole("admin", "system"))
					}
					r.Delete("/", h.DeprovisionTenant)
				})

				// Tenant lifecycle — requires Pro
				r.Group(func(r chi.Router) {
					r.Use(RequireFeature(cfg.EditionManager, license.TenantLifecycleManager))
					if cfg.AuthEnabled {
						r.Use(RequireRole("admin", "system"))
					}
					r.Post("/suspend", h.SuspendTenant)
					r.Post("/reactivate", h.ReactivateTenant)
				})

				// Key management (JWT signing keys)
				r.Route("/keys", func(r chi.Router) {
					r.Post("/", h.CreateKey)
					r.Get("/", h.ListKeys)
					r.Delete("/{keyID}", h.RevokeKey)
				})

				// API key management
				r.Route("/api-keys", func(r chi.Router) {
					r.Post("/", h.CreateAPIKey)
					r.Get("/", h.ListAPIKeys)
					r.Delete("/{keyID}", h.RevokeAPIKey)
				})

				// Routing rules management (admin-only for set/delete)
				r.Route("/routing-rules", func(r chi.Router) {
					r.Get("/", h.GetRoutingRules)
					r.Group(func(r chi.Router) {
						if cfg.AuthEnabled {
							r.Use(RequireRole("admin", "system"))
						}
						r.Put("/", h.SetRoutingRules)
						r.Delete("/", h.DeleteRoutingRules)
					})
				})

				// Quota management — requires Pro
				r.Group(func(r chi.Router) {
					r.Use(RequireFeature(cfg.EditionManager, license.PerTenantConfigurableQuotas))
					r.Get("/quotas", h.GetQuota)
					r.Group(func(r chi.Router) {
						if cfg.AuthEnabled {
							r.Use(RequireRole("admin", "system"))
						}
						r.Patch("/quotas", h.UpdateQuota)
					})
				})

				// Audit log — requires Enterprise
				r.Group(func(r chi.Router) {
					r.Use(RequireFeature(cfg.EditionManager, license.AuditLogging))
					r.Get("/audit", h.GetAuditLog)
				})

				// Channel rules (admin-only for set/delete)
				r.Route("/channel-rules", func(r chi.Router) {
					r.Get("/", h.GetChannelRules)
					r.Group(func(r chi.Router) {
						if cfg.AuthEnabled {
							r.Use(RequireRole("admin", "system"))
						}
						r.Put("/", h.SetChannelRules)
						r.Delete("/", h.DeleteChannelRules)
					})
				})

				// Test access endpoint
				r.Post("/test-access", h.TestAccess)
			})
		})

		// Active keys endpoint (for WS Gateway) - requires system role
		r.Group(func(r chi.Router) {
			if cfg.AuthEnabled {
				r.Use(RequireRole("system", "admin"))
			}
			r.Get("/keys/active", h.GetActiveKeys)
			r.Get("/api-keys/active", h.GetActiveAPIKeys)
		})
	})

	return r, nil
}
