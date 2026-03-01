// Package api provides HTTP handlers for the provisioning service.
package api //nolint:revive // api is a common package name for HTTP handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/rs/zerolog"

	"github.com/Toniq-Labs/odin-ws/internal/provisioning"
	"github.com/Toniq-Labs/odin-ws/internal/shared/auth"
	"github.com/Toniq-Labs/odin-ws/internal/shared/version"
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
}

// NewRouter creates a new HTTP router with all provisioning endpoints.
func NewRouter(cfg RouterConfig) http.Handler {
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
	h := NewHandler(cfg.Service, cfg.Logger)

	// Health endpoints (no auth required)
	r.Get("/health", h.Health)
	r.Get("/ready", h.Ready)
	r.Get("/version", version.Handler("provisioning"))
	r.Get("/metrics", h.Metrics)

	// API v1 routes
	r.Route("/api/v1", func(r chi.Router) {
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
					r.Post("/suspend", h.SuspendTenant)
					r.Post("/reactivate", h.ReactivateTenant)
				})

				// Key management
				r.Route("/keys", func(r chi.Router) {
					r.Post("/", h.CreateKey)
					r.Get("/", h.ListKeys)
					r.Delete("/{keyID}", h.RevokeKey)
				})

				// Topic management
				r.Route("/topics", func(r chi.Router) {
					r.Post("/", h.CreateTopics)
					r.Get("/", h.ListTopics)
				})

				// Quota management
				r.Get("/quotas", h.GetQuota)
				r.Group(func(r chi.Router) {
					if cfg.AuthEnabled {
						r.Use(RequireRole("admin", "system"))
					}
					r.Patch("/quotas", h.UpdateQuota)
				})

				// Audit log
				r.Get("/audit", h.GetAuditLog)

				// OIDC configuration (admin-only for create/update/delete)
				r.Route("/oidc", func(r chi.Router) {
					r.Get("/", h.GetOIDCConfig)
					r.Group(func(r chi.Router) {
						if cfg.AuthEnabled {
							r.Use(RequireRole("admin", "system"))
						}
						r.Post("/", h.CreateOIDCConfig)
						r.Put("/", h.UpdateOIDCConfig)
						r.Delete("/", h.DeleteOIDCConfig)
					})
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
		})
	})

	return r
}
