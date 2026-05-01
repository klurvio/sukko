package gateway

import (
	"context"

	"github.com/klurvio/sukko/internal/shared/auth"
	"github.com/klurvio/sukko/internal/shared/provapi"
	"github.com/klurvio/sukko/internal/shared/types"
)

// TokenValidator validates JWT tokens and returns claims.
// The existing auth.MultiTenantValidator satisfies this interface.
type TokenValidator interface {
	ValidateToken(ctx context.Context, tokenString string) (*auth.Claims, error)
}

// ChannelRulesProvider provides per-tenant channel rules for the gateway.
// Defined here (consumer) per coding guidelines: "accept interfaces, return concrete types"
type ChannelRulesProvider interface {
	// GetChannelRules returns the channel rules for a tenant.
	// Returns types.ErrChannelRulesNotFound if not configured.
	GetChannelRules(ctx context.Context, tenantID string) (*types.ChannelRules, error)

	// Close releases resources held by the provider.
	Close() error
}

// APIKeyLookup provides O(1) API key validation for gateway connections.
// Implemented by StreamAPIKeyRegistry; defined here to enable mock injection in tests.
type APIKeyLookup interface {
	// Lookup returns the API key info for the given key string.
	// Returns false if the key is not found or not active.
	Lookup(apiKey string) (*provapi.APIKeyInfo, bool)

	// Close releases resources held by the registry.
	Close() error
}

// licenseWatcher is used by Gateway for health reporting and shutdown.
// Satisfied by *provapi.StreamLicenseWatcher; mock-injectable in tests.
type licenseWatcher interface {
	State() int32
	Close() error
}
