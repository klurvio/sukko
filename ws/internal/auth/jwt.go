// Package auth provides JWT authentication for WebSocket connections.
// It handles token validation, issuance, and session management.
package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Sentinel errors for authentication failures
var (
	ErrInvalidToken = errors.New("invalid token")
	ErrTokenExpired = errors.New("token expired")
	ErrMissingToken = errors.New("missing token")
)

// Claims represents the JWT claims structure.
// Uses standard JWT claims with Subject (sub) as the app ID or user principal.
type Claims struct {
	jwt.RegisteredClaims
	TenantID string   `json:"tenant_id,omitempty"`
	Groups   []string `json:"groups,omitempty"` // Group memberships for group-scoped channel access
}

// AppID returns the subject (app ID) from the token.
// The app ID identifies the connecting application (e.g., "odin-web", "trading-bot-1").
func (c *Claims) AppID() string {
	return c.Subject
}

// Tenant returns the tenant ID from the token.
// The tenant ID identifies the organization/company that owns the app.
func (c *Claims) Tenant() string {
	return c.TenantID
}

// JWTValidator handles JWT token validation and issuance.
// Thread-safe for concurrent use.
type JWTValidator struct {
	secret []byte
}

// NewJWTValidator creates a new JWT validator with the given secret.
// The secret should be at least 32 bytes for HS256.
func NewJWTValidator(secret string) *JWTValidator {
	return &JWTValidator{secret: []byte(secret)}
}

// ValidateToken validates a JWT token string and returns the claims if valid.
// Returns ErrMissingToken if token is empty.
// Returns ErrTokenExpired if token has expired.
// Returns ErrInvalidToken for all other validation failures.
func (v *JWTValidator) ValidateToken(tokenString string) (*Claims, error) {
	if tokenString == "" {
		return nil, ErrMissingToken
	}

	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
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
		return "", time.Time{}, err
	}

	return tokenString, expiresAt, nil
}
