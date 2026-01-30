package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"

	"github.com/Toniq-Labs/odin-ws/internal/shared/auth"
)

// Context keys for auth information.
type contextKey string

const (
	// ClaimsContextKey is the context key for JWT claims.
	ClaimsContextKey contextKey = "claims"
	// ActorContextKey is the context key for the actor (tenant_id:user_id).
	ActorContextKey contextKey = "actor"
)

// LoggingMiddleware provides structured request logging.
func LoggingMiddleware(logger zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			defer func() { //nolint:contextcheck // Context is captured from r which is in scope
				logger.Info().
					Str("method", r.Method).
					Str("path", r.URL.Path).
					Str("remote_addr", r.RemoteAddr).
					Int("status", ww.Status()).
					Int("bytes", ww.BytesWritten()).
					Dur("duration", time.Since(start)).
					Str("request_id", middleware.GetReqID(r.Context())).
					Msg("HTTP request")
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
			// Extract token from Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				writeError(w, http.StatusUnauthorized, "MISSING_TOKEN", "Authorization header required")
				return
			}

			// Expect "Bearer <token>"
			scheme, tokenString, found := strings.Cut(authHeader, " ")
			if !found || !strings.EqualFold(scheme, "Bearer") {
				writeError(w, http.StatusUnauthorized, "INVALID_AUTH_HEADER", "Authorization header must be 'Bearer <token>'")
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

				switch {
				case errors.Is(err, auth.ErrMissingToken):
					writeError(w, http.StatusUnauthorized, "MISSING_TOKEN", "Token required")
				case errors.Is(err, auth.ErrTokenExpired):
					writeError(w, http.StatusUnauthorized, "TOKEN_EXPIRED", "Token has expired")
				case errors.Is(err, auth.ErrKeyNotFound):
					writeError(w, http.StatusUnauthorized, "KEY_NOT_FOUND", "Signing key not found")
				case errors.Is(err, auth.ErrKeyRevoked):
					writeError(w, http.StatusUnauthorized, "KEY_REVOKED", "Signing key has been revoked")
				default:
					writeError(w, http.StatusUnauthorized, "INVALID_TOKEN", "Token validation failed")
				}
				return
			}

			// Build actor string for audit logging
			actor := claims.TenantID + ":" + claims.Subject

			// Add claims and actor to context
			ctx := context.WithValue(r.Context(), ClaimsContextKey, claims)
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
				writeError(w, http.StatusUnauthorized, "NOT_AUTHENTICATED", "Authentication required")
				return
			}

			// Check if user has any of the required roles
			if slices.ContainsFunc(roles, claims.HasRole) {
				next.ServeHTTP(w, r)
				return
			}

			writeError(w, http.StatusForbidden, "INSUFFICIENT_ROLE", "Required role: "+strings.Join(roles, " or "))
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
				writeError(w, http.StatusUnauthorized, "NOT_AUTHENTICATED", "Authentication required")
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
				writeError(w, http.StatusForbidden, "TENANT_MISMATCH", "Cannot access other tenant's resources")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// GetClaimsFromContext retrieves JWT claims from context.
func GetClaimsFromContext(ctx context.Context) *auth.Claims {
	claims, _ := ctx.Value(ClaimsContextKey).(*auth.Claims)
	return claims
}

// GetActorFromContext retrieves the actor string from context.
func GetActorFromContext(ctx context.Context) string {
	actor, _ := ctx.Value(ActorContextKey).(string)
	return actor
}
