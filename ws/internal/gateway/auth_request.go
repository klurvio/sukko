package gateway

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/shared/auth"
	"github.com/klurvio/sukko/internal/shared/httputil"
	pkgmetrics "github.com/klurvio/sukko/internal/shared/metrics"
)

// Sentinel errors for authentication failures.
// Callers map these to HTTP status codes:
//   - ErrNoCredentials → 401
//   - ErrInvalidToken → 401
//   - ErrInvalidAPIKey → 401
//   - ErrTenantMismatch → 401 (WS close reason CloseReasonAPIKeyTenantMismatch)
var (
	ErrNoCredentials = errors.New("no credentials provided")
	ErrInvalidToken  = errors.New("invalid token")
	ErrInvalidAPIKey = errors.New("invalid API key")
	// ErrTenantMismatch aliases the shared sentinel (§X — single definition in
	// shared/auth). It covers both the JWT-claim-vs-signing-key binding failure
	// (from ValidateToken) and the JWT-tenant-vs-API-key-tenant mismatch below.
	ErrTenantMismatch = auth.ErrTenantMismatch
	ErrTokenRevoked   = errors.New("token revoked")
	ErrMissingJTI     = errors.New("missing jti claim")
)

// Auth-failure metric method labels for tenant-mismatch rejections (§I named
// constants; §XVIII branch-distinct, matching the jwt_revoked / jwt+api_key_revoked
// convention).
const (
	authMethodJWTTenantMismatch       = "jwt_tenant_mismatch"
	authMethodJWTAPIKeyTenantMismatch = "jwt+api_key_tenant_mismatch"
)

// logTenantMismatch Warn-logs a tenant-binding rejection with the structured
// fields carried by *auth.TenantMismatchError (kid, claimed slug, resolved claim
// UUID, key owning UUID). The raw error is never returned to the client — the
// owning tenant must not leak (§IX) — so callers return the generic sentinel.
func logTenantMismatch(logger zerolog.Logger, err error, remoteAddr string) {
	evt := logger.Warn().Str("remote_addr", remoteAddr)
	var tmErr *auth.TenantMismatchError
	if errors.As(err, &tmErr) {
		evt = evt.
			Str("kid", tmErr.KID).
			Str("claimed_slug", tmErr.ClaimedSlug).
			Str("claimed_uuid", tmErr.ClaimedUUID).
			Str("key_uuid", tmErr.KeyUUID)
	}
	evt.Msg("Request rejected: JWT tenant does not match signing key")
}

// authResult holds the validated identity from authenticateRequest.
type authResult struct {
	Claims         *auth.Claims // nil for API-key-only or auth-disabled
	Principal      string       // User identity (JWT subject or "anon:uuid")
	TenantID       string       // Tenant from JWT or API key
	APIKeyOnly     bool         // True if only API key was provided (no JWT)
	APIKeyTenantID string       // API key tenant (for cross-validation)
	AuthMethod     string       // "none", "api_key", "jwt", "jwt+api_key" — for metrics and logging
	APIKeyID       string       // Stable database ID of the API key (APIKeyInfo.KeyID); empty if no API key was used
	UserID         string       // JWT subject claim; empty for API-key-only auth
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
	authStart := time.Now()
	token := httputil.ExtractBearerToken(r)
	apiKey := httputil.ExtractAPIKey(r)

	switch {
	case token == "" && apiKey == "":
		RecordAuthValidation(pkgmetrics.AuthStatusFailed, "none", time.Since(authStart))
		gw.logger.Warn().
			Str("remote_addr", r.RemoteAddr).
			Msg("Request rejected: no credentials provided")
		return nil, ErrNoCredentials

	case apiKey != "" && token == "":
		// API key only — public channels, no publish
		info, ok := gw.apiKeyRegistry.Lookup(apiKey)
		if !ok {
			RecordAPIKeyAuth(APIKeyAuthInvalid)
			RecordAuthValidation(pkgmetrics.AuthStatusFailed, "api_key", time.Since(authStart))
			gw.logger.Warn().
				Str("remote_addr", r.RemoteAddr).
				Msg("Request rejected: invalid API key")
			return nil, ErrInvalidAPIKey
		}
		RecordAPIKeyAuth(APIKeyAuthAccepted)
		RecordAuthValidation(pkgmetrics.AuthStatusSuccess, "api_key", time.Since(authStart))

		result := &authResult{
			TenantID:       info.TenantID,
			Principal:      "anon:" + uuid.NewString(),
			APIKeyOnly:     true,
			APIKeyTenantID: info.TenantID,
			AuthMethod:     "api_key",
			APIKeyID:       info.KeyID,
			// UserID is empty for API-key-only auth (no JWT subject to forward).
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
			if errors.Is(err, auth.ErrTenantMismatch) {
				// Cross-tenant token: signed by one tenant's key but claiming
				// another (#158). Distinct label + structured detail server-side.
				RecordAuthValidation(pkgmetrics.AuthStatusFailed, authMethodJWTTenantMismatch, time.Since(authStart))
				logTenantMismatch(gw.logger, err, r.RemoteAddr)
				return nil, ErrTenantMismatch
			}
			RecordAuthValidation(pkgmetrics.AuthStatusFailed, "jwt", time.Since(authStart))
			gw.logger.Warn().
				Err(err).
				Str("remote_addr", r.RemoteAddr).
				Msg("Request rejected: invalid token")
			return nil, ErrInvalidToken
		}
		RecordAuthValidation(pkgmetrics.AuthStatusSuccess, "jwt", time.Since(authStart))

		// jti mandatory (FR-001a)
		if claims.ID == "" {
			RecordAuthValidation(pkgmetrics.AuthStatusFailed, "jwt_missing_jti", time.Since(authStart))
			return nil, ErrMissingJTI
		}

		// Revocation check (FR-002) — skip if registry not configured
		if gw.revocationRegistry != nil && gw.revocationRegistry.IsRevoked(
			claims.ID, claims.Subject, claims.TenantID, claims.IssuedAt.Unix(),
		) {
			RecordAuthValidation(pkgmetrics.AuthStatusFailed, "jwt_revoked", time.Since(authStart))
			return nil, ErrTokenRevoked
		}

		result := &authResult{
			Claims:     claims,
			Principal:  claims.Subject,
			TenantID:   claims.TenantID,
			AuthMethod: "jwt",
			UserID:     claims.Subject,
			// APIKeyID is empty for JWT-only auth.
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
			RecordAuthValidation(pkgmetrics.AuthStatusFailed, "jwt+api_key", time.Since(authStart))
			gw.logger.Warn().
				Str("remote_addr", r.RemoteAddr).
				Msg("Request rejected: invalid API key")
			return nil, ErrInvalidAPIKey
		}
		RecordAPIKeyAuth(APIKeyAuthAccepted)

		claims, err := gw.validator.ValidateToken(ctx, token)
		if err != nil {
			RecordAuthValidation(pkgmetrics.AuthStatusFailed, "jwt+api_key", time.Since(authStart))
			gw.logger.Warn().
				Err(err).
				Str("remote_addr", r.RemoteAddr).
				Msg("Request rejected: invalid token")
			return nil, ErrInvalidToken
		}

		// Verify JWT tenant matches API key tenant. The core binding already bound
		// the claim to the signing key's owning tenant UUID (claims.ResolvedTenantUUID);
		// compare that UUID against the API key's tenant UUID (info.TenantID) — both
		// UUIDs, so this is a like-for-like check (the old claims.TenantID slug vs
		// info.TenantID UUID comparison never matched).
		if claims.ResolvedTenantUUID != info.TenantID {
			RecordAuthValidation(pkgmetrics.AuthStatusFailed, authMethodJWTAPIKeyTenantMismatch, time.Since(authStart))
			gw.logger.Warn().
				Str("jwt_tenant_uuid", claims.ResolvedTenantUUID).
				Str("api_key_tenant", info.TenantID).
				Str("remote_addr", r.RemoteAddr).
				Msg("Request rejected: API key and JWT tenant mismatch")
			return nil, ErrTenantMismatch
		}
		RecordAuthValidation(pkgmetrics.AuthStatusSuccess, "jwt+api_key", time.Since(authStart))

		// jti mandatory (FR-001a)
		if claims.ID == "" {
			RecordAuthValidation(pkgmetrics.AuthStatusFailed, "jwt+api_key_missing_jti", time.Since(authStart))
			return nil, ErrMissingJTI
		}

		// Revocation check (FR-002) — skip if registry not configured
		if gw.revocationRegistry != nil && gw.revocationRegistry.IsRevoked(
			claims.ID, claims.Subject, claims.TenantID, claims.IssuedAt.Unix(),
		) {
			RecordAuthValidation(pkgmetrics.AuthStatusFailed, "jwt+api_key_revoked", time.Since(authStart))
			return nil, ErrTokenRevoked
		}

		result := &authResult{
			Claims:         claims,
			Principal:      claims.Subject,
			TenantID:       claims.TenantID,
			APIKeyTenantID: info.TenantID,
			AuthMethod:     "jwt+api_key",
			APIKeyID:       info.KeyID,
			UserID:         claims.Subject,
		}
		gw.logger.Debug().
			Str("principal", result.Principal).
			Str("tenant_id", result.TenantID).
			Str("remote_addr", r.RemoteAddr).
			Msg("API key and token validated successfully")
		return result, nil
	}
}
