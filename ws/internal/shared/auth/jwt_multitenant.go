// Package auth provides JWT authentication for WebSocket connections.
package auth

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/golang-jwt/jwt/v5"
)

// MultiTenantValidatorConfig configures the MultiTenantValidator.
type MultiTenantValidatorConfig struct {
	// KeyRegistry provides public keys for validation.
	KeyRegistry KeyRegistry

	// RequireTenantID requires tokens to have a tenant_id claim.
	RequireTenantID bool

	// RequireKeyID is accepted for API compatibility but has no effect.
	// Tenant tokens always require a kid header; OIDC tokens use the OIDC keyfunc.
	RequireKeyID bool

	// AllowedAlgorithms restricts which algorithms are accepted.
	// Empty means all supported algorithms are allowed.
	AllowedAlgorithms []string

	// OIDCIssuer is the expected issuer URL for OIDC tokens.
	// Tokens with this issuer are validated via OIDCKeyfunc instead of KeyRegistry.
	OIDCIssuer string

	// OIDCAudience is the expected audience for OIDC tokens.
	// If set, OIDC tokens must contain this audience.
	OIDCAudience string

	// OIDCKeyfunc is the keyfunc from NewOIDCKeyfunc() for OIDC token validation.
	// If nil, OIDC validation is disabled and all tokens use KeyRegistry.
	OIDCKeyfunc jwt.Keyfunc
}

// MultiTenantValidator validates JWTs using tenant-specific public keys.
// Thread-safe for concurrent use.
type MultiTenantValidator struct {
	keyRegistry       KeyRegistry
	requireTenantID   bool
	allowedAlgorithms map[string]bool

	// OIDC support
	oidcIssuer   string
	oidcAudience string
	oidcKeyfunc  jwt.Keyfunc
}

// NewMultiTenantValidator creates a new multi-tenant JWT validator.
func NewMultiTenantValidator(cfg MultiTenantValidatorConfig) (*MultiTenantValidator, error) {
	if cfg.KeyRegistry == nil {
		return nil, errors.New("key registry is required")
	}

	allowedAlgos := make(map[string]bool)
	if len(cfg.AllowedAlgorithms) > 0 {
		for _, alg := range cfg.AllowedAlgorithms {
			allowedAlgos[alg] = true
		}
	} else {
		// Default: allow all supported algorithms
		allowedAlgos["ES256"] = true
		allowedAlgos["RS256"] = true
		allowedAlgos["EdDSA"] = true
	}

	return &MultiTenantValidator{
		keyRegistry:       cfg.KeyRegistry,
		requireTenantID:   cfg.RequireTenantID,
		allowedAlgorithms: allowedAlgos,
		oidcIssuer:        cfg.OIDCIssuer,
		oidcAudience:      cfg.OIDCAudience,
		oidcKeyfunc:       cfg.OIDCKeyfunc,
	}, nil
}

// ValidateToken validates a JWT token string and returns the claims if valid.
// For tenant tokens, the token must have a 'kid' header that identifies the signing key.
// For OIDC tokens (when issuer matches OIDCIssuer), validation uses the OIDC keyfunc.
func (v *MultiTenantValidator) ValidateToken(ctx context.Context, tokenString string) (*Claims, error) {
	if tokenString == "" {
		return nil, ErrMissingToken
	}

	// Track whether this is an OIDC token for post-parse audience validation
	var isOIDCToken bool

	// Derive valid methods from allowedAlgorithms map
	validMethods := make([]string, 0, len(v.allowedAlgorithms))
	for alg := range v.allowedAlgorithms {
		validMethods = append(validMethods, alg)
	}

	// Parse with claims and keyfunc that routes based on issuer
	parser := jwt.NewParser(
		jwt.WithValidMethods(validMethods),
	)

	token, err := parser.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (any, error) {
		// Check if this is an OIDC token by examining the issuer claim
		// The unverified claims are available during keyfunc execution
		claims, ok := token.Claims.(*Claims)
		if ok && v.oidcKeyfunc != nil && v.oidcIssuer != "" && claims.Issuer == v.oidcIssuer {
			// This is an OIDC token - delegate to OIDC keyfunc
			isOIDCToken = true
			return v.oidcKeyfunc(token)
		}

		// Tenant token flow - requires kid header and key registry lookup
		kidRaw, ok := token.Header["kid"]
		if !ok {
			return nil, errors.New("missing kid header")
		}

		kid, ok := kidRaw.(string)
		if !ok || kid == "" {
			return nil, errors.New("invalid kid header")
		}

		// Check algorithm is allowed
		alg, ok := token.Header["alg"].(string)
		if !ok {
			return nil, errors.New("missing alg header")
		}
		if !v.allowedAlgorithms[alg] {
			return nil, fmt.Errorf("algorithm %s not allowed", alg)
		}

		// Look up the key in tenant key registry
		key, err := v.keyRegistry.GetKey(ctx, kid)
		if err != nil {
			if errors.Is(err, ErrKeyNotFound) {
				return nil, fmt.Errorf("key not found: %s", kid)
			}
			if errors.Is(err, ErrKeyRevoked) {
				return nil, fmt.Errorf("key revoked: %s", kid)
			}
			if errors.Is(err, ErrKeyExpired) {
				return nil, fmt.Errorf("key expired: %s", kid)
			}
			return nil, fmt.Errorf("key lookup failed: %w", err)
		}

		// Verify algorithm matches key
		if key.Algorithm != alg {
			return nil, fmt.Errorf("algorithm mismatch: token=%s, key=%s", alg, key.Algorithm)
		}

		return key.PublicKey, nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}

	// Validate audience for OIDC tokens
	if isOIDCToken && v.oidcAudience != "" {
		aud, err := claims.GetAudience()
		if err != nil || !slices.Contains(aud, v.oidcAudience) {
			return nil, fmt.Errorf("%w: expected %s", ErrInvalidAudience, v.oidcAudience)
		}
	}

	// Validate tenant_id if required
	if v.requireTenantID && claims.TenantID == "" {
		return nil, fmt.Errorf("%w: missing tenant_id claim", ErrInvalidToken)
	}

	return claims, nil
}

// ValidateTokenForTenant validates a token and ensures it belongs to the specified tenant.
// This is useful for endpoint-specific validation where the tenant is known.
func (v *MultiTenantValidator) ValidateTokenForTenant(ctx context.Context, tokenString string, expectedTenant string) (*Claims, error) {
	claims, err := v.ValidateToken(ctx, tokenString)
	if err != nil {
		return nil, err
	}

	if claims.TenantID != expectedTenant {
		return nil, fmt.Errorf("%w: tenant mismatch", ErrInvalidToken)
	}

	return claims, nil
}

// ExtractKeyID extracts the key ID from a token without validating it.
// Useful for logging or debugging.
func ExtractKeyID(tokenString string) (string, error) {
	parser := jwt.NewParser()
	token, _, err := parser.ParseUnverified(tokenString, &Claims{})
	if err != nil {
		return "", fmt.Errorf("failed to parse token: %w", err)
	}

	kidRaw, ok := token.Header["kid"]
	if !ok {
		return "", errors.New("missing kid header")
	}

	kid, ok := kidRaw.(string)
	if !ok {
		return "", errors.New("invalid kid header")
	}

	return kid, nil
}

// ExtractTenantID extracts the tenant ID from a token without validating it.
// Useful for routing or logging before validation.
func ExtractTenantID(tokenString string) (string, error) {
	parser := jwt.NewParser()
	token, _, err := parser.ParseUnverified(tokenString, &Claims{})
	if err != nil {
		return "", fmt.Errorf("failed to parse token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok {
		return "", errors.New("invalid claims type")
	}

	return claims.TenantID, nil
}
