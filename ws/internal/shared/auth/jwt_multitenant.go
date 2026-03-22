// Package auth provides JWT authentication for WebSocket connections.
package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

// MultiTenantValidatorConfig configures the MultiTenantValidator.
type MultiTenantValidatorConfig struct {
	// KeyRegistry provides public keys for validation.
	KeyRegistry KeyRegistry

	// RequireTenantID requires tokens to have a tenant_id claim.
	RequireTenantID bool

	// AllowedAlgorithms restricts which algorithms are accepted.
	// Empty means all supported algorithms are allowed.
	AllowedAlgorithms []string

	// AllowedIssuers restricts which token issuers (iss claim) are accepted.
	// Empty means issuer verification is skipped (for backward compatibility).
	AllowedIssuers []string
}

// MultiTenantValidator validates JWTs using tenant-specific public keys.
// Thread-safe for concurrent use.
type MultiTenantValidator struct {
	keyRegistry       KeyRegistry
	requireTenantID   bool
	allowedAlgorithms map[string]bool
	allowedIssuers    map[string]bool
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

	allowedIssuers := make(map[string]bool, len(cfg.AllowedIssuers))
	for _, iss := range cfg.AllowedIssuers {
		allowedIssuers[iss] = true
	}

	return &MultiTenantValidator{
		keyRegistry:       cfg.KeyRegistry,
		requireTenantID:   cfg.RequireTenantID,
		allowedAlgorithms: allowedAlgos,
		allowedIssuers:    allowedIssuers,
	}, nil
}

// ValidateToken validates a JWT token string and returns the claims if valid.
// The token must have a 'kid' header that identifies the signing key in the tenant key registry.
func (v *MultiTenantValidator) ValidateToken(ctx context.Context, tokenString string) (*Claims, error) {
	if tokenString == "" {
		return nil, ErrMissingToken
	}

	// Derive valid methods from allowedAlgorithms map
	validMethods := make([]string, 0, len(v.allowedAlgorithms))
	for alg := range v.allowedAlgorithms {
		validMethods = append(validMethods, alg)
	}

	// Parse with claims and keyfunc that looks up tenant keys
	parser := jwt.NewParser(
		jwt.WithValidMethods(validMethods),
	)

	token, err := parser.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (any, error) {
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

	// Validate issuer if configured
	if len(v.allowedIssuers) > 0 {
		issuer, _ := claims.GetIssuer()
		if issuer == "" || !v.allowedIssuers[issuer] {
			return nil, fmt.Errorf("%w: issuer %q not allowed", ErrInvalidToken, issuer)
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
func (v *MultiTenantValidator) ValidateTokenForTenant(ctx context.Context, tokenString, expectedTenant string) (*Claims, error) {
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
