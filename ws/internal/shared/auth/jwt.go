// Package auth provides JWT authentication for WebSocket connections.
// It handles token validation, issuance, and session management.
package auth

import (
	"errors"
	"fmt"
	"slices"
	"time"

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

// JWTValidator handles JWT token validation and issuance.
// Thread-safe for concurrent use.
type JWTValidator struct {
	secret         []byte
	expectedIssuer string // Optional: if set, validates iss claim
}

// JWTValidatorConfig configures the JWTValidator.
type JWTValidatorConfig struct {
	// Secret is the HMAC signing key (required, should be at least 32 bytes for HS256).
	Secret string

	// Issuer is the expected iss claim value. If empty, issuer verification is skipped.
	Issuer string
}

// NewJWTValidator creates a new JWT validator with the given secret.
// The secret should be at least 32 bytes for HS256.
func NewJWTValidator(secret string) (*JWTValidator, error) {
	return NewJWTValidatorWithConfig(JWTValidatorConfig{Secret: secret})
}

// NewJWTValidatorWithConfig creates a new JWT validator with full configuration.
func NewJWTValidatorWithConfig(cfg JWTValidatorConfig) (*JWTValidator, error) {
	if cfg.Secret == "" {
		return nil, errors.New("JWT validator: secret is required")
	}
	return &JWTValidator{
		secret:         []byte(cfg.Secret),
		expectedIssuer: cfg.Issuer,
	}, nil
}

// ValidateToken validates a JWT token string and returns the claims if valid.
// Returns ErrMissingToken if token is empty.
// Returns ErrTokenExpired if token has expired.
// Returns ErrInvalidToken for all other validation failures.
func (v *JWTValidator) ValidateToken(tokenString string) (*Claims, error) {
	if tokenString == "" {
		return nil, ErrMissingToken
	}

	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (any, error) {
		// Validate signing method is HMAC
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return v.secret, nil
	})

	if err != nil {
		// Check for specific JWT errors
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}

	// Verify issuer if configured (Constitution IX: JWT validation MUST verify issuer)
	if v.expectedIssuer != "" {
		issuer, _ := claims.GetIssuer()
		if issuer != v.expectedIssuer {
			return nil, fmt.Errorf("%w: issuer %q not allowed", ErrInvalidToken, issuer)
		}
	}

	return claims, nil
}

// IssueToken creates a new JWT token for an app with the specified expiry duration.
// Returns the token string, expiry time, and any error.
func (v *JWTValidator) IssueToken(appID string, expiry time.Duration) (string, time.Time, error) {
	return v.IssueTokenWithTenant(appID, "", expiry)
}

// IssueTokenWithTenant creates a new JWT token for an app with tenant ID and expiry duration.
// Returns the token string, expiry time, and any error.
func (v *JWTValidator) IssueTokenWithTenant(appID, tenantID string, expiry time.Duration) (string, time.Time, error) {
	if appID == "" {
		return "", time.Time{}, errors.New("issue token: appID is required")
	}
	expiresAt := time.Now().Add(expiry)
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   appID,
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		TenantID: tenantID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(v.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}

	return tokenString, expiresAt, nil
}
