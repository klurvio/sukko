// Package api provides HTTP handlers for the provisioning service.
package api

import (
	"net/http"

	"github.com/Toniq-Labs/odin-ws/internal/provisioning"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
)

// RouterConfig holds configuration for the HTTP router.
type RouterConfig struct {
	Service   *provisioning.Service
	Logger    zerolog.Logger
	RateLimit int // requests per minute
}

// NewRouter creates a new HTTP router with all provisioning endpoints.
func NewRouter(cfg RouterConfig) http.Handler {
	r := chi.NewRouter()

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
	r.Get("/metrics", h.Metrics)

	// API v1 routes
	r.Route("/api/v1", func(r chi.Router) {
		// TODO: Add auth middleware in Phase 4
		// r.Use(AuthMiddleware(keyRegistry))

		// Tenant management
		r.Route("/tenants", func(r chi.Router) {
			r.Post("/", h.CreateTenant)
			r.Get("/", h.ListTenants)

			r.Route("/{tenantID}", func(r chi.Router) {
				r.Get("/", h.GetTenant)
				r.Patch("/", h.UpdateTenant)
				r.Delete("/", h.DeprovisionTenant)

				// Lifecycle actions
				r.Post("/suspend", h.SuspendTenant)
				r.Post("/reactivate", h.ReactivateTenant)

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
				r.Patch("/quotas", h.UpdateQuota)

				// Audit log
				r.Get("/audit", h.GetAuditLog)
			})
		})

		// Active keys endpoint (for WS Gateway)
		r.Get("/keys/active", h.GetActiveKeys)
	})

	return r
}
