package gateway

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/klurvio/sukko/internal/shared/auth"
	"github.com/klurvio/sukko/internal/shared/httputil"
	pkgmetrics "github.com/klurvio/sukko/internal/shared/metrics"
)

// Sentinel errors for authentication failures.
// Callers map these to HTTP status codes:
//   - ErrNoCredentials → 401
//   - ErrInvalidToken → 401
//   - ErrInvalidAPIKey → 401
//   - ErrTenantMismatch → 403
var (
	ErrNoCredentials  = errors.New("no credentials provided")
	ErrInvalidToken   = errors.New("invalid token")
	ErrInvalidAPIKey  = errors.New("invalid API key")
	ErrTenantMismatch = errors.New("API key and token tenant mismatch")
)

// authResult holds the validated identity from authenticateRequest.
type authResult struct {
	Claims         *auth.Claims // nil for API-key-only or auth-disabled
	Principal      string       // User identity (JWT subject or "anon:uuid")
	TenantID       string       // Tenant from JWT or API key
	APIKeyOnly     bool         // True if only API key was provided (no JWT)
	APIKeyTenantID string       // API key tenant (for cross-validation)
}

// authenticateRequest validates credentials from the HTTP request.
// Checks all credential sources: query param ?token=, Authorization header, X-API-Key header.
//
// This function MUST NOT write to http.ResponseWriter — it returns an error
// and lets the caller decide the HTTP response.
//
// Auth metrics (RecordAuthValidation) are recorded inside this function.
// Permission checking stays OUT — each handler applies its own permission logic.
func (gw *Gateway) authenticateRequest(ctx context.Context, r *http.Request) (*authResult, error) {
	if !gw.config.AuthEnabled {
		RecordAuthValidation(pkgmetrics.AuthStatusSkipped, 0)
		return &authResult{
			Principal: "anonymous",
			TenantID:  gw.config.DefaultTenantID,
		}, nil
	}

	authStart := time.Now()
	token := httputil.ExtractBearerToken(r)
	apiKey := httputil.ExtractAPIKey(r)

	switch {
	case token == "" && apiKey == "":
		RecordAuthValidation(pkgmetrics.AuthStatusFailed, time.Since(authStart))
		gw.logger.Warn().
			Str("remote_addr", r.RemoteAddr).
			Msg("Request rejected: no credentials provided")
		return nil, ErrNoCredentials

	case apiKey != "" && token == "":
		// API key only — public channels, no publish
		info, ok := gw.apiKeyRegistry.Lookup(apiKey)
		if !ok {
			RecordAPIKeyAuth(APIKeyAuthInvalid)
			RecordAuthValidation(pkgmetrics.AuthStatusFailed, time.Since(authStart))
			gw.logger.Warn().
				Str("remote_addr", r.RemoteAddr).
				Msg("Request rejected: invalid API key")
			return nil, ErrInvalidAPIKey
		}
		RecordAPIKeyAuth(APIKeyAuthAccepted)
		RecordAuthValidation(pkgmetrics.AuthStatusSuccess, time.Since(authStart))

		result := &authResult{
			TenantID:       info.TenantID,
			Principal:      "anon:" + uuid.NewString(),
			APIKeyOnly:     true,
			APIKeyTenantID: info.TenantID,
		}
		gw.logger.Debug().
			Str("principal", result.Principal).
			Str("tenant_id", result.TenantID).
			Str("remote_addr", r.RemoteAddr).
			Msg("API key validated successfully")
		return result, nil

	case token != "" && apiKey == "":
		// JWT only
		claims, err := gw.validator.ValidateToken(ctx, token)
		if err != nil {
			RecordAuthValidation(pkgmetrics.AuthStatusFailed, time.Since(authStart))
			gw.logger.Warn().
				Err(err).
				Str("remote_addr", r.RemoteAddr).
				Msg("Request rejected: invalid token")
			return nil, ErrInvalidToken
		}
		RecordAuthValidation(pkgmetrics.AuthStatusSuccess, time.Since(authStart))

		result := &authResult{
			Claims:    claims,
			Principal: claims.Subject,
			TenantID:  claims.TenantID,
		}
		gw.logger.Debug().
			Str("principal", result.Principal).
			Str("tenant_id", result.TenantID).
			Strs("groups", claims.Groups).
			Str("remote_addr", r.RemoteAddr).
			Msg("Token validated successfully")
		return result, nil

	default:
		// Both API key and JWT — validate both, verify tenant match
		info, ok := gw.apiKeyRegistry.Lookup(apiKey)
		if !ok {
			RecordAPIKeyAuth(APIKeyAuthInvalid)
			RecordAuthValidation(pkgmetrics.AuthStatusFailed, time.Since(authStart))
			gw.logger.Warn().
				Str("remote_addr", r.RemoteAddr).
				Msg("Request rejected: invalid API key")
			return nil, ErrInvalidAPIKey
		}
		RecordAPIKeyAuth(APIKeyAuthAccepted)

		claims, err := gw.validator.ValidateToken(ctx, token)
		if err != nil {
			RecordAuthValidation(pkgmetrics.AuthStatusFailed, time.Since(authStart))
			gw.logger.Warn().
				Err(err).
				Str("remote_addr", r.RemoteAddr).
				Msg("Request rejected: invalid token")
			return nil, ErrInvalidToken
		}

		// Verify JWT tenant matches API key tenant
		if claims.TenantID != info.TenantID {
			RecordAuthValidation(pkgmetrics.AuthStatusFailed, time.Since(authStart))
			gw.logger.Warn().
				Str("jwt_tenant", claims.TenantID).
				Str("api_key_tenant", info.TenantID).
				Str("remote_addr", r.RemoteAddr).
				Msg("Request rejected: API key and JWT tenant mismatch")
			return nil, ErrTenantMismatch
		}
		RecordAuthValidation(pkgmetrics.AuthStatusSuccess, time.Since(authStart))

		result := &authResult{
			Claims:         claims,
			Principal:      claims.Subject,
			TenantID:       claims.TenantID,
			APIKeyTenantID: info.TenantID,
		}
		gw.logger.Debug().
			Str("principal", result.Principal).
			Str("tenant_id", result.TenantID).
			Str("remote_addr", r.RemoteAddr).
			Msg("API key and token validated successfully")
		return result, nil
	}
}
