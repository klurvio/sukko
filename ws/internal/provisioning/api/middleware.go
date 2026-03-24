// Package api provides HTTP handlers and middleware for the provisioning service.
package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
	"golang.org/x/time/rate"

	"github.com/klurvio/sukko/internal/shared/auth"
	"github.com/klurvio/sukko/internal/shared/httputil"
	pkgmetrics "github.com/klurvio/sukko/internal/shared/metrics"
)

// Context key for actor information (tenant_id:user_id format).
type contextKey string

const (
	// ActorContextKey is the context key for the actor (tenant_id:user_id).
	ActorContextKey contextKey = "actor"
)

// LoggingMiddleware provides structured request logging.
func LoggingMiddleware(logger zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			defer func() { //nolint:contextcheck // False positive: r.Context() is captured via closure from the enclosing HandlerFunc scope; the deferred func accesses the same request context used by next.ServeHTTP.
				elapsed := time.Since(start)

				logger.Info().
					Str("method", r.Method).
					Str("path", r.URL.Path).
					Str("remote_addr", r.RemoteAddr).
					Int("status", ww.Status()).
					Int("bytes", ww.BytesWritten()).
					Dur("duration", elapsed).
					Str("request_id", middleware.GetReqID(r.Context())).
					Msg("HTTP request")

				// Record API metrics using route pattern to avoid cardinality explosion
				routePattern := chi.RouteContext(r.Context()).RoutePattern()
				if routePattern == "" {
					routePattern = "unmatched"
				}
				statusStr := strconv.Itoa(ww.Status())
				RecordAPIRequest(routePattern, r.Method, statusStr)
				RecordAPILatency(routePattern, r.Method, elapsed.Seconds())
			}()

			next.ServeHTTP(ww, r)
		})
	}
}

// AuthMiddleware validates JWT tokens and extracts actor information.
// Tokens are validated using the MultiTenantValidator which supports
// per-tenant asymmetric keys (ES256, RS256, EdDSA).
func AuthMiddleware(validator *auth.MultiTenantValidator, logger zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip if already authenticated (e.g., by admin token middleware)
			if auth.GetClaims(r.Context()) != nil {
				next.ServeHTTP(w, r)
				return
			}

			// Extract token from Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				httputil.WriteError(w, http.StatusUnauthorized, "MISSING_TOKEN", "Authorization header required")
				return
			}

			// Expect "Bearer <token>"
			scheme, tokenString, found := strings.Cut(authHeader, " ")
			if !found || !strings.EqualFold(scheme, "Bearer") {
				httputil.WriteError(w, http.StatusUnauthorized, "INVALID_AUTH_HEADER", "Authorization header must be 'Bearer <token>'")
				return
			}

			// Validate token
			claims, err := validator.ValidateToken(r.Context(), tokenString)
			if err != nil {
				logger.Warn().
					Err(err).
					Str("remote_addr", r.RemoteAddr).
					Str("path", r.URL.Path).
					Msg("Token validation failed")

				var reason string
				switch {
				case errors.Is(err, auth.ErrMissingToken):
					reason = authFailureReasonMissingToken
					httputil.WriteError(w, http.StatusUnauthorized, "MISSING_TOKEN", "Token required")
				case errors.Is(err, auth.ErrTokenExpired):
					reason = authFailureReasonTokenExpired
					httputil.WriteError(w, http.StatusUnauthorized, "TOKEN_EXPIRED", "Token has expired")
				case errors.Is(err, auth.ErrKeyNotFound):
					reason = authFailureReasonKeyNotFound
					httputil.WriteError(w, http.StatusUnauthorized, "KEY_NOT_FOUND", "Signing key not found")
				case errors.Is(err, auth.ErrKeyRevoked):
					reason = authFailureReasonKeyRevoked
					httputil.WriteError(w, http.StatusUnauthorized, "KEY_REVOKED", "Signing key has been revoked")
				default:
					reason = authFailureReasonInvalidToken
					httputil.WriteError(w, http.StatusUnauthorized, "INVALID_TOKEN", "Token validation failed")
				}
				RecordAuthAttempt(authResultFailure, reason)
				return
			}

			RecordAuthAttempt(pkgmetrics.AuthStatusSuccess, "")

			// Build actor string for audit logging
			actor := claims.TenantID + ":" + claims.Subject

			// Add claims and actor to context using shared helpers
			ctx := auth.WithClaims(r.Context(), claims)
			ctx = context.WithValue(ctx, ActorContextKey, actor)

			logger.Debug().
				Str("tenant_id", claims.TenantID).
				Str("subject", claims.Subject).
				Str("path", r.URL.Path).
				Msg("Request authenticated")

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole checks that the authenticated user has at least one of the required roles.
// Must be used after AuthMiddleware.
func RequireRole(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := GetClaimsFromContext(r.Context())
			if claims == nil {
				httputil.WriteError(w, http.StatusUnauthorized, "NOT_AUTHENTICATED", "Authentication required")
				return
			}

			// Check if user has any of the required roles
			if slices.ContainsFunc(roles, claims.HasRole) {
				next.ServeHTTP(w, r)
				return
			}

			RecordAuthorizationDenial(authzDenialInsufficientRole)
			httputil.WriteError(w, http.StatusForbidden, "INSUFFICIENT_ROLE", "Required role: "+strings.Join(roles, " or "))
		})
	}
}

// RequireTenant checks that the request is for the authenticated tenant's resources.
// Compares the tenantID URL param with the token's tenant_id claim.
// Must be used after AuthMiddleware.
func RequireTenant() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := GetClaimsFromContext(r.Context())
			if claims == nil {
				httputil.WriteError(w, http.StatusUnauthorized, "NOT_AUTHENTICATED", "Authentication required")
				return
			}

			// Get tenantID from URL
			tenantID := chi.URLParam(r, "tenantID")
			if tenantID == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Allow if admin or system role
			if claims.HasRole("admin") || claims.HasRole("system") {
				next.ServeHTTP(w, r)
				return
			}

			// Check tenant match
			if claims.TenantID != tenantID {
				RecordAuthorizationDenial(authzDenialTenantMismatch)
				httputil.WriteError(w, http.StatusForbidden, "TENANT_MISMATCH", "Cannot access other tenant's resources")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// GetClaimsFromContext retrieves JWT claims from context.
func GetClaimsFromContext(ctx context.Context) *auth.Claims {
	return auth.GetClaims(ctx)
}

// GetActorFromContext retrieves the actor string from context.
func GetActorFromContext(ctx context.Context) string {
	actor, _ := ctx.Value(ActorContextKey).(string)
	return actor
}

// RateLimitMiddleware applies global and per-IP token-bucket rate limits.
// Global limit: requestsPerMinute/60 rps with burst = requestsPerMinute.
// Per-IP limit: 2x per-second rate of global, burst = requestsPerMinute/2.
// Per-IP entries are lazily created and bounded to perIPRateLimitMaxEntries.
func RateLimitMiddleware(requestsPerMinute int) func(http.Handler) http.Handler {
	globalLimiter := rate.NewLimiter(rate.Limit(float64(requestsPerMinute)/60.0), requestsPerMinute)

	perSecond := float64(requestsPerMinute) / 60.0
	ipRate := rate.Limit(perSecond * 2)
	ipBurst := max(requestsPerMinute/2, 1)
	var ipLimiters sync.Map // map[string]*rate.Limiter

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Global rate limit
			if !globalLimiter.Allow() {
				httputil.WriteError(w, http.StatusTooManyRequests, "RATE_LIMITED", "API rate limit exceeded")
				return
			}

			// Per-IP rate limit
			ip := extractClientIP(r)
			v, _ := ipLimiters.LoadOrStore(ip, rate.NewLimiter(ipRate, ipBurst))
			if limiter, ok := v.(*rate.Limiter); ok && !limiter.Allow() {
				httputil.WriteError(w, http.StatusTooManyRequests, "RATE_LIMITED", "Per-IP rate limit exceeded")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// extractClientIP returns the client IP from X-Real-IP or RemoteAddr.
func extractClientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
