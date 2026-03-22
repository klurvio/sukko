package gateway

import (
	"context"

	"github.com/klurvio/sukko/internal/shared/auth"
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
