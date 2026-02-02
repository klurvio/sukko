package types

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TenantOIDCConfig represents OIDC configuration for a tenant.
// Used by provisioning (storage) and gateway (validation).
type TenantOIDCConfig struct {
	// TenantID is the tenant this OIDC config belongs to.
	TenantID string `json:"tenant_id"`

	// IssuerURL is the OIDC issuer URL (e.g., "https://acme.auth0.com/").
	// This is used to match the "iss" claim in incoming JWTs.
	IssuerURL string `json:"issuer_url"`

	// JWKSURL is the JWKS endpoint URL for fetching public keys.
	// If empty, defaults to {IssuerURL}/.well-known/jwks.json
	JWKSURL string `json:"jwks_url,omitempty"`

	// Audience is the expected "aud" claim in JWTs.
	// If set, tokens must contain this audience.
	Audience string `json:"audience,omitempty"`

	// Enabled indicates if this OIDC config is currently active.
	Enabled bool `json:"enabled"`

	// CreatedAt is when the config was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the config was last updated.
	UpdatedAt time.Time `json:"updated_at"`
}

// Validate validates the OIDC config. Defense in depth.
func (c *TenantOIDCConfig) Validate() error {
	// Issuer URL validation
	if c.IssuerURL == "" {
		return ErrIssuerURLRequired
	}

	if len(c.IssuerURL) > MaxIssuerURLLength {
		return fmt.Errorf("%w: max %d characters", ErrIssuerURLTooLong, MaxIssuerURLLength)
	}

	issuerURL, err := url.Parse(c.IssuerURL)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidIssuerURL, err)
	}

	if issuerURL.Scheme != "https" {
		return fmt.Errorf("%w: must use HTTPS", ErrInvalidIssuerURL)
	}

	if issuerURL.Host == "" {
		return fmt.Errorf("%w: must have a host", ErrInvalidIssuerURL)
	}

	// JWKS URL validation (if provided)
	if c.JWKSURL != "" {
		if len(c.JWKSURL) > MaxIssuerURLLength {
			return fmt.Errorf("%w: max %d characters", ErrInvalidJWKSURL, MaxIssuerURLLength)
		}

		jwksURL, err := url.Parse(c.JWKSURL)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidJWKSURL, err)
		}

		if jwksURL.Scheme != "https" {
			return fmt.Errorf("%w: must use HTTPS", ErrInvalidJWKSURL)
		}

		if jwksURL.Host == "" {
			return fmt.Errorf("%w: must have a host", ErrInvalidJWKSURL)
		}
	}

	// Audience validation
	if c.Audience != "" && len(c.Audience) > MaxAudienceLength {
		return fmt.Errorf("%w: max %d characters", ErrAudienceTooLong, MaxAudienceLength)
	}

	return nil
}

// GetJWKSURL returns the JWKS URL, defaulting to standard OIDC discovery path.
func (c *TenantOIDCConfig) GetJWKSURL() string {
	if c.JWKSURL != "" {
		return c.JWKSURL
	}
	// Default to standard OIDC discovery path
	// Ensure we don't double up slashes
	issuer := strings.TrimSuffix(c.IssuerURL, "/")
	return issuer + "/.well-known/jwks.json"
}
