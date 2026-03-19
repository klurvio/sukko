package gateway

import (
	"context"

	"github.com/golang-jwt/jwt/v5"

	"github.com/klurvio/sukko/internal/shared/auth"
	"github.com/klurvio/sukko/internal/shared/types"
)

// TokenValidator validates JWT tokens and returns claims.
// The existing auth.MultiTenantValidator satisfies this interface.
type TokenValidator interface {
	ValidateToken(ctx context.Context, tokenString string) (*auth.Claims, error)
}

// TenantRegistry provides tenant lookup for gateway authentication.
// Defined here (consumer) per coding guidelines: "accept interfaces, return concrete types"
type TenantRegistry interface {
	// GetTenantByIssuer returns the tenant ID for an OIDC issuer.
	// Returns types.ErrIssuerNotFound if issuer is not registered.
	GetTenantByIssuer(ctx context.Context, issuerURL string) (string, error)

	// GetOIDCConfig returns the OIDC configuration for a tenant.
	// Returns types.ErrOIDCNotConfigured if not configured.
	GetOIDCConfig(ctx context.Context, tenantID string) (*types.TenantOIDCConfig, error)

	// GetChannelRules returns the channel rules for a tenant.
	// Returns types.ErrChannelRulesNotFound if not configured.
	GetChannelRules(ctx context.Context, tenantID string) (*types.ChannelRules, error)

	// Close releases resources held by the registry.
	Close() error
}

// MultiIssuerValidator provides multi-issuer OIDC token validation.
// Supports dynamic issuer registration and JWKS caching.
type MultiIssuerValidator interface {
	// GetKeyfunc returns a jwt.Keyfunc for the given issuer URL.
	// Creates and caches the keyfunc if not already cached.
	// Returns types.ErrIssuerNotFound if issuer is not registered.
	GetKeyfunc(ctx context.Context, issuerURL string) (jwt.Keyfunc, error)

	// GetTenantByIssuer returns the tenant ID for an issuer URL.
	GetTenantByIssuer(ctx context.Context, issuerURL string) (string, error)

	// Close stops background refresh and releases resources.
	Close() error
}
