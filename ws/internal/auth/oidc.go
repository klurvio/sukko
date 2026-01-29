// Package auth provides JWT authentication for WebSocket connections.
package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog"
)

// OIDC-related sentinel errors.
var (
	ErrOIDCMissingJWKSURL = errors.New("OIDC JWKS URL is required")
)

// OIDCConfig configures OIDC/JWKS-based token validation.
type OIDCConfig struct {
	// IssuerURL is the expected token issuer (e.g., "https://your-app.auth0.com/").
	// Used for issuer-based routing in MultiTenantValidator.
	IssuerURL string

	// JWKSURL is the JWKS endpoint URL (e.g., "https://your-app.auth0.com/.well-known/jwks.json").
	// Required when OIDC is enabled.
	JWKSURL string

	// Audience is the expected audience claim (e.g., "https://api.odin.io").
	// If set, tokens must contain this audience.
	Audience string
}

// Validate checks that required OIDC configuration is present.
func (c *OIDCConfig) Validate() error {
	if c.JWKSURL == "" {
		return ErrOIDCMissingJWKSURL
	}
	return nil
}

// OIDCKeyfuncResult contains the OIDC keyfunc and its cleanup function.
type OIDCKeyfuncResult struct {
	// Keyfunc returns the public key for JWT verification.
	Keyfunc jwt.Keyfunc

	// Close stops background JWKS refresh goroutines.
	// Must be called when the keyfunc is no longer needed.
	Close func()
}

// NewOIDCKeyfunc creates a JWKS-based keyfunc for validating OIDC tokens.
// The returned keyfunc automatically refreshes keys from the JWKS endpoint.
// Caller must call Close() on the result to stop background refresh.
func NewOIDCKeyfunc(ctx context.Context, cfg OIDCConfig, logger zerolog.Logger) (*OIDCKeyfuncResult, error) {
	// Defense in depth: validate config even if caller claims to have validated
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate OIDC config: %w", err)
	}

	logger.Debug().
		Str("jwks_url", cfg.JWKSURL).
		Str("issuer_url", cfg.IssuerURL).
		Msg("Creating OIDC keyfunc")

	// Create a cancellable context for the keyfunc's background refresh
	refreshCtx, cancelRefresh := context.WithCancel(ctx)

	// Create keyfunc with auto-refresh using keyfunc/v3
	// NewDefaultCtx uses the context to manage the HTTP client and refresh lifecycle
	kf, err := keyfunc.NewDefaultCtx(refreshCtx, []string{cfg.JWKSURL})
	if err != nil {
		cancelRefresh() // Clean up context on error
		return nil, fmt.Errorf("create JWKS keyfunc from %s: %w", cfg.JWKSURL, err)
	}

	logger.Info().
		Str("jwks_url", cfg.JWKSURL).
		Msg("OIDC keyfunc created successfully")

	return &OIDCKeyfuncResult{
		Keyfunc: kf.Keyfunc,
		Close: func() {
			cancelRefresh()
			logger.Debug().Msg("OIDC keyfunc closed")
		},
	}, nil
}
