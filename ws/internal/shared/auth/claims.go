package auth

import (
	"errors"
	"slices"

	"github.com/golang-jwt/jwt/v5"
)

// Sentinel errors for authentication failures
var (
	ErrInvalidToken = errors.New("invalid token")
	ErrTokenExpired = errors.New("token expired")
	ErrMissingToken = errors.New("missing token")
)

// Claims represents the JWT claims structure for multi-tenant authentication.
// Uses standard JWT claims with Subject (sub) as the user/app identifier.
type Claims struct {
	jwt.RegisteredClaims

	// TenantID is the tenant identifier (REQUIRED for multi-tenant mode)
	TenantID string `json:"tenant_id,omitempty"`

	// Attributes are identity attributes for placeholder resolution (e.g., "tier": "premium")
	Attributes map[string]string `json:"attrs,omitempty"`

	// Roles for RBAC (e.g., ["admin", "trader"])
	Roles []string `json:"roles,omitempty"`

	// Groups for group-scoped channel access (e.g., ["vip", "traders"])
	Groups []string `json:"groups,omitempty"`

	// Scopes for permission scopes (e.g., ["read:trades", "write:orders"])
	Scopes []string `json:"scopes,omitempty"`

	// Custom is an extension point for application-specific claims
	Custom map[string]any `json:"custom,omitempty"`
}

// AppID returns the subject (app ID) from the token.
// The app ID identifies the connecting application (e.g., "sukko-web", "trading-bot-1").
func (c *Claims) AppID() string {
	return c.Subject
}

// UserID returns the subject (user identifier) from the token.
// Alias for Subject for clarity in user-centric contexts.
func (c *Claims) UserID() string {
	return c.Subject
}

// Tenant returns the tenant ID from the token.
// The tenant ID identifies the organization/company that owns the app.
func (c *Claims) Tenant() string {
	return c.TenantID
}

// HasRole checks if the claims contain the specified role.
func (c *Claims) HasRole(role string) bool {
	return slices.Contains(c.Roles, role)
}

// HasScope checks if the claims contain the specified scope.
func (c *Claims) HasScope(scope string) bool {
	return slices.Contains(c.Scopes, scope)
}

// HasGroup checks if the claims contain the specified group.
func (c *Claims) HasGroup(group string) bool {
	return slices.Contains(c.Groups, group)
}

// GetAttribute returns the value of the specified attribute, or empty string if not found.
func (c *Claims) GetAttribute(key string) string {
	if c.Attributes == nil {
		return ""
	}
	return c.Attributes[key]
}
