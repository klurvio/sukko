package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// createTestToken creates a signed JWT token for testing.
func createTestToken(t *testing.T, key *KeyInfo, privateKey any, claims *Claims) string {
	t.Helper()

	method, err := GetSigningMethod(key.Algorithm)
	if err != nil {
		t.Fatalf("GetSigningMethod failed: %v", err)
	}

	token := jwt.NewWithClaims(method, claims)
	token.Header["kid"] = key.KeyID

	tokenString, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatalf("Failed to sign token: %v", err)
	}

	return tokenString
}

func TestMultiTenantValidator_ValidateToken_ES256(t *testing.T) {
	t.Parallel()
	registry := NewStaticKeyRegistry()
	ecPEM, privateKey := generateTestECKey(t)

	key := &KeyInfo{
		KeyID:        "test-key-1",
		TenantID:     "acme",
		Algorithm:    "ES256",
		PublicKeyPEM: ecPEM,
		IsActive:     true,
	}
	if err := registry.AddKey(key); err != nil {
		t.Fatalf("AddKey failed: %v", err)
	}

	validator, err := NewMultiTenantValidator(MultiTenantValidatorConfig{
		KeyRegistry: registry,
	})
	if err != nil {
		t.Fatalf("NewMultiTenantValidator failed: %v", err)
	}

	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-123",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		TenantID: "acme",
	}

	tokenString := createTestToken(t, key, privateKey, claims)

	ctx := context.Background()
	gotClaims, err := validator.ValidateToken(ctx, tokenString)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}

	if gotClaims.Subject != "user-123" {
		t.Errorf("Subject = %s, want user-123", gotClaims.Subject)
	}
	if gotClaims.TenantID != "acme" {
		t.Errorf("TenantID = %s, want acme", gotClaims.TenantID)
	}
}

func TestMultiTenantValidator_ValidateToken_RS256(t *testing.T) {
	t.Parallel()
	registry := NewStaticKeyRegistry()
	rsaPEM, privateKey := generateTestRSAKey(t)

	key := &KeyInfo{
		KeyID:        "rsa-key-1",
		TenantID:     "globex",
		Algorithm:    "RS256",
		PublicKeyPEM: rsaPEM,
		IsActive:     true,
	}
	if err := registry.AddKey(key); err != nil {
		t.Fatalf("AddKey failed: %v", err)
	}

	validator, err := NewMultiTenantValidator(MultiTenantValidatorConfig{
		KeyRegistry: registry,
	})
	if err != nil {
		t.Fatalf("NewMultiTenantValidator failed: %v", err)
	}

	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "app-456",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		TenantID: "globex",
		Roles:    []string{"admin"},
	}

	tokenString := createTestToken(t, key, privateKey, claims)

	ctx := context.Background()
	gotClaims, err := validator.ValidateToken(ctx, tokenString)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}

	if gotClaims.TenantID != "globex" {
		t.Errorf("TenantID = %s, want globex", gotClaims.TenantID)
	}
	if !gotClaims.HasRole("admin") {
		t.Error("Expected admin role")
	}
}

func TestMultiTenantValidator_ValidateToken_EdDSA(t *testing.T) {
	t.Parallel()
	registry := NewStaticKeyRegistry()
	edPEM, privateKey := generateTestEdKey(t)

	key := &KeyInfo{
		KeyID:        "ed-key-1",
		TenantID:     "initech",
		Algorithm:    "EdDSA",
		PublicKeyPEM: edPEM,
		IsActive:     true,
	}
	if err := registry.AddKey(key); err != nil {
		t.Fatalf("AddKey failed: %v", err)
	}

	validator, err := NewMultiTenantValidator(MultiTenantValidatorConfig{
		KeyRegistry: registry,
	})
	if err != nil {
		t.Fatalf("NewMultiTenantValidator failed: %v", err)
	}

	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "service-789",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		TenantID: "initech",
	}

	tokenString := createTestToken(t, key, privateKey, claims)

	ctx := context.Background()
	gotClaims, err := validator.ValidateToken(ctx, tokenString)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}

	if gotClaims.TenantID != "initech" {
		t.Errorf("TenantID = %s, want initech", gotClaims.TenantID)
	}
}

func TestMultiTenantValidator_ValidateToken_ExpiredToken(t *testing.T) {
	t.Parallel()
	registry := NewStaticKeyRegistry()
	ecPEM, privateKey := generateTestECKey(t)

	key := &KeyInfo{
		KeyID:        "test-key-1",
		TenantID:     "acme",
		Algorithm:    "ES256",
		PublicKeyPEM: ecPEM,
		IsActive:     true,
	}
	if err := registry.AddKey(key); err != nil {
		t.Fatalf("AddKey failed: %v", err)
	}

	validator, err := NewMultiTenantValidator(MultiTenantValidatorConfig{
		KeyRegistry: registry,
	})
	if err != nil {
		t.Fatalf("NewMultiTenantValidator failed: %v", err)
	}

	// Create expired token
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-123",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)), // expired
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		},
		TenantID: "acme",
	}

	tokenString := createTestToken(t, key, privateKey, claims)

	ctx := context.Background()
	_, err = validator.ValidateToken(ctx, tokenString)
	if !errors.Is(err, ErrTokenExpired) {
		t.Errorf("Expected ErrTokenExpired, got %v", err)
	}
}

func TestMultiTenantValidator_ValidateToken_KeyNotFound(t *testing.T) {
	t.Parallel()
	registry := NewStaticKeyRegistry()
	ecPEM, privateKey := generateTestECKey(t)

	// Don't add key to registry
	key := &KeyInfo{
		KeyID:        "unknown-key",
		TenantID:     "acme",
		Algorithm:    "ES256",
		PublicKeyPEM: ecPEM,
		IsActive:     true,
	}

	validator, err := NewMultiTenantValidator(MultiTenantValidatorConfig{
		KeyRegistry: registry,
	})
	if err != nil {
		t.Fatalf("NewMultiTenantValidator failed: %v", err)
	}

	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-123",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		TenantID: "acme",
	}

	tokenString := createTestToken(t, key, privateKey, claims)

	ctx := context.Background()
	_, err = validator.ValidateToken(ctx, tokenString)
	if err == nil {
		t.Error("Expected error for unknown key")
	}
}

func TestMultiTenantValidator_ValidateToken_RevokedKey(t *testing.T) {
	t.Parallel()
	registry := NewStaticKeyRegistry()
	ecPEM, privateKey := generateTestECKey(t)

	past := time.Now().Add(-time.Hour)
	key := &KeyInfo{
		KeyID:        "revoked-key",
		TenantID:     "acme",
		Algorithm:    "ES256",
		PublicKeyPEM: ecPEM,
		IsActive:     true,
		RevokedAt:    &past,
	}
	if err := registry.AddKey(key); err != nil {
		t.Fatalf("AddKey failed: %v", err)
	}

	validator, err := NewMultiTenantValidator(MultiTenantValidatorConfig{
		KeyRegistry: registry,
	})
	if err != nil {
		t.Fatalf("NewMultiTenantValidator failed: %v", err)
	}

	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-123",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		TenantID: "acme",
	}

	tokenString := createTestToken(t, key, privateKey, claims)

	ctx := context.Background()
	_, err = validator.ValidateToken(ctx, tokenString)
	if err == nil {
		t.Error("Expected error for revoked key")
	}
}

func TestMultiTenantValidator_ValidateToken_MissingToken(t *testing.T) {
	t.Parallel()
	registry := NewStaticKeyRegistry()

	validator, err := NewMultiTenantValidator(MultiTenantValidatorConfig{
		KeyRegistry: registry,
	})
	if err != nil {
		t.Fatalf("NewMultiTenantValidator failed: %v", err)
	}

	ctx := context.Background()
	_, err = validator.ValidateToken(ctx, "")
	if !errors.Is(err, ErrMissingToken) {
		t.Errorf("Expected ErrMissingToken, got %v", err)
	}
}

func TestMultiTenantValidator_ValidateToken_RequireTenantID(t *testing.T) {
	t.Parallel()
	registry := NewStaticKeyRegistry()
	ecPEM, privateKey := generateTestECKey(t)

	key := &KeyInfo{
		KeyID:        "test-key-1",
		TenantID:     "acme",
		Algorithm:    "ES256",
		PublicKeyPEM: ecPEM,
		IsActive:     true,
	}
	if err := registry.AddKey(key); err != nil {
		t.Fatalf("AddKey failed: %v", err)
	}

	validator, err := NewMultiTenantValidator(MultiTenantValidatorConfig{
		KeyRegistry:     registry,
		RequireTenantID: true,
	})
	if err != nil {
		t.Fatalf("NewMultiTenantValidator failed: %v", err)
	}

	// Create token WITHOUT tenant_id
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-123",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		// No TenantID
	}

	tokenString := createTestToken(t, key, privateKey, claims)

	ctx := context.Background()
	_, err = validator.ValidateToken(ctx, tokenString)
	if err == nil {
		t.Error("Expected error for missing tenant_id")
	}
}

func TestMultiTenantValidator_ValidateTokenForTenant(t *testing.T) {
	t.Parallel()
	registry := NewStaticKeyRegistry()
	ecPEM, privateKey := generateTestECKey(t)

	key := &KeyInfo{
		KeyID:        "test-key-1",
		TenantID:     "acme",
		Algorithm:    "ES256",
		PublicKeyPEM: ecPEM,
		IsActive:     true,
	}
	if err := registry.AddKey(key); err != nil {
		t.Fatalf("AddKey failed: %v", err)
	}

	validator, err := NewMultiTenantValidator(MultiTenantValidatorConfig{
		KeyRegistry: registry,
	})
	if err != nil {
		t.Fatalf("NewMultiTenantValidator failed: %v", err)
	}

	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-123",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		TenantID: "acme",
	}

	tokenString := createTestToken(t, key, privateKey, claims)
	ctx := context.Background()

	t.Run("matching tenant", func(t *testing.T) {
		t.Parallel()
		_, err := validator.ValidateTokenForTenant(ctx, tokenString, "acme")
		if err != nil {
			t.Errorf("ValidateTokenForTenant failed: %v", err)
		}
	})

	t.Run("non-matching tenant", func(t *testing.T) {
		t.Parallel()
		_, err := validator.ValidateTokenForTenant(ctx, tokenString, "other")
		if err == nil {
			t.Error("Expected error for tenant mismatch")
		}
	})
}

func TestExtractKeyID(t *testing.T) {
	t.Parallel()
	registry := NewStaticKeyRegistry()
	ecPEM, privateKey := generateTestECKey(t)

	key := &KeyInfo{
		KeyID:        "extract-test-key",
		TenantID:     "acme",
		Algorithm:    "ES256",
		PublicKeyPEM: ecPEM,
		IsActive:     true,
	}
	if err := registry.AddKey(key); err != nil {
		t.Fatalf("AddKey failed: %v", err)
	}

	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-123",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		TenantID: "acme",
	}

	tokenString := createTestToken(t, key, privateKey, claims)

	kid, err := ExtractKeyID(tokenString)
	if err != nil {
		t.Fatalf("ExtractKeyID failed: %v", err)
	}

	if kid != "extract-test-key" {
		t.Errorf("kid = %s, want extract-test-key", kid)
	}
}

func TestExtractTenantID(t *testing.T) {
	t.Parallel()
	registry := NewStaticKeyRegistry()
	ecPEM, privateKey := generateTestECKey(t)

	key := &KeyInfo{
		KeyID:        "test-key",
		TenantID:     "acme",
		Algorithm:    "ES256",
		PublicKeyPEM: ecPEM,
		IsActive:     true,
	}
	if err := registry.AddKey(key); err != nil {
		t.Fatalf("AddKey failed: %v", err)
	}

	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-123",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		TenantID: "extracted-tenant",
	}

	tokenString := createTestToken(t, key, privateKey, claims)

	tenantID, err := ExtractTenantID(tokenString)
	if err != nil {
		t.Fatalf("ExtractTenantID failed: %v", err)
	}

	if tenantID != "extracted-tenant" {
		t.Errorf("tenantID = %s, want extracted-tenant", tenantID)
	}
}

// createOIDCTestToken creates a signed JWT token for OIDC testing (without kid header).
func createOIDCTestToken(t *testing.T, privateKey any, alg string, claims *Claims) string {
	t.Helper()

	method, err := GetSigningMethod(alg)
	if err != nil {
		t.Fatalf("GetSigningMethod failed: %v", err)
	}

	token := jwt.NewWithClaims(method, claims)
	// OIDC tokens typically have kid, but we set it for keyfunc lookup
	token.Header["kid"] = "oidc-key-1"

	tokenString, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatalf("Failed to sign token: %v", err)
	}

	return tokenString
}

// mockOIDCKeyfunc creates a keyfunc that returns the given public key.
func mockOIDCKeyfunc(publicKey any) jwt.Keyfunc {
	return func(_ *jwt.Token) (any, error) {
		return publicKey, nil
	}
}

// mockFailingOIDCKeyfunc creates a keyfunc that always returns an error.
func mockFailingOIDCKeyfunc() jwt.Keyfunc {
	return func(_ *jwt.Token) (any, error) {
		return nil, errors.New("OIDC keyfunc failure")
	}
}

func TestMultiTenantValidator_OIDCRouting(t *testing.T) {
	t.Parallel()

	// Set up tenant key registry
	registry := NewStaticKeyRegistry()
	tenantPEM, tenantPrivateKey := generateTestECKey(t)
	tenantKey := &KeyInfo{
		KeyID:        "tenant-key-1",
		TenantID:     "acme",
		Algorithm:    "ES256",
		PublicKeyPEM: tenantPEM,
		IsActive:     true,
	}
	if err := registry.AddKey(tenantKey); err != nil {
		t.Fatalf("AddKey failed: %v", err)
	}

	// Set up OIDC key (RSA for typical IdP)
	oidcPEM, oidcPrivateKey := generateTestRSAKey(t)
	oidcPublicKey, err := ParsePublicKey(oidcPEM, "RS256")
	if err != nil {
		t.Fatalf("ParsePublicKey failed: %v", err)
	}

	const oidcIssuer = "https://auth.example.com/"
	const oidcAudience = "https://api.odin.io"

	tests := []struct {
		name        string
		claims      *Claims
		useOIDCKey  bool
		oidcKeyfunc jwt.Keyfunc
		wantErr     error
		wantTenant  string
	}{
		{
			name: "oidc_token_valid_audience",
			claims: &Claims{
				RegisteredClaims: jwt.RegisteredClaims{
					Subject:   "user-123",
					Issuer:    oidcIssuer,
					Audience:  jwt.ClaimStrings{oidcAudience},
					ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
				},
				TenantID: "acme",
			},
			useOIDCKey:  true,
			oidcKeyfunc: mockOIDCKeyfunc(oidcPublicKey),
			wantErr:     nil,
			wantTenant:  "acme",
		},
		{
			name: "oidc_token_wrong_audience",
			claims: &Claims{
				RegisteredClaims: jwt.RegisteredClaims{
					Subject:   "user-123",
					Issuer:    oidcIssuer,
					Audience:  jwt.ClaimStrings{"https://wrong.audience.com"},
					ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
				},
				TenantID: "acme",
			},
			useOIDCKey:  true,
			oidcKeyfunc: mockOIDCKeyfunc(oidcPublicKey),
			wantErr:     ErrInvalidAudience,
		},
		{
			name: "oidc_token_missing_audience",
			claims: &Claims{
				RegisteredClaims: jwt.RegisteredClaims{
					Subject:   "user-123",
					Issuer:    oidcIssuer,
					ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
				},
				TenantID: "acme",
			},
			useOIDCKey:  true,
			oidcKeyfunc: mockOIDCKeyfunc(oidcPublicKey),
			wantErr:     ErrInvalidAudience,
		},
		{
			name: "tenant_token_different_issuer",
			claims: &Claims{
				RegisteredClaims: jwt.RegisteredClaims{
					Subject:   "user-456",
					Issuer:    "https://other-issuer.com/",
					ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
				},
				TenantID: "acme",
			},
			useOIDCKey:  false,
			oidcKeyfunc: mockOIDCKeyfunc(oidcPublicKey),
			wantErr:     nil,
			wantTenant:  "acme",
		},
		{
			name: "tenant_token_no_issuer",
			claims: &Claims{
				RegisteredClaims: jwt.RegisteredClaims{
					Subject:   "user-789",
					ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
				},
				TenantID: "globex",
			},
			useOIDCKey:  false,
			oidcKeyfunc: mockOIDCKeyfunc(oidcPublicKey),
			wantErr:     nil,
			wantTenant:  "globex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			validator, err := NewMultiTenantValidator(MultiTenantValidatorConfig{
				KeyRegistry:  registry,
				OIDCIssuer:   oidcIssuer,
				OIDCAudience: oidcAudience,
				OIDCKeyfunc:  tt.oidcKeyfunc,
			})
			if err != nil {
				t.Fatalf("NewMultiTenantValidator failed: %v", err)
			}

			var tokenString string
			if tt.useOIDCKey {
				tokenString = createOIDCTestToken(t, oidcPrivateKey, "RS256", tt.claims)
			} else {
				tokenString = createTestToken(t, tenantKey, tenantPrivateKey, tt.claims)
			}

			ctx := context.Background()
			gotClaims, err := validator.ValidateToken(ctx, tokenString)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("ValidateToken() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("ValidateToken() unexpected error: %v", err)
			}

			if gotClaims.TenantID != tt.wantTenant {
				t.Errorf("TenantID = %s, want %s", gotClaims.TenantID, tt.wantTenant)
			}
		})
	}
}

func TestMultiTenantValidator_OIDCDisabled_FallsBackToTenantKeys(t *testing.T) {
	t.Parallel()

	// Set up tenant key registry
	registry := NewStaticKeyRegistry()
	tenantPEM, tenantPrivateKey := generateTestECKey(t)
	tenantKey := &KeyInfo{
		KeyID:        "tenant-key-1",
		TenantID:     "acme",
		Algorithm:    "ES256",
		PublicKeyPEM: tenantPEM,
		IsActive:     true,
	}
	if err := registry.AddKey(tenantKey); err != nil {
		t.Fatalf("AddKey failed: %v", err)
	}

	// Validator with OIDC disabled (OIDCKeyfunc is nil)
	validator, err := NewMultiTenantValidator(MultiTenantValidatorConfig{
		KeyRegistry:  registry,
		OIDCIssuer:   "https://auth.example.com/",
		OIDCAudience: "https://api.odin.io",
		OIDCKeyfunc:  nil, // OIDC disabled
	})
	if err != nil {
		t.Fatalf("NewMultiTenantValidator failed: %v", err)
	}

	// Create a token with the OIDC issuer but sign with tenant key
	// Since OIDCKeyfunc is nil, it should fall back to tenant key registry
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-123",
			Issuer:    "https://auth.example.com/", // Same as OIDCIssuer
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		TenantID: "acme",
	}

	tokenString := createTestToken(t, tenantKey, tenantPrivateKey, claims)

	ctx := context.Background()
	gotClaims, err := validator.ValidateToken(ctx, tokenString)
	if err != nil {
		t.Fatalf("ValidateToken() unexpected error: %v", err)
	}

	if gotClaims.TenantID != "acme" {
		t.Errorf("TenantID = %s, want acme", gotClaims.TenantID)
	}
}

func TestMultiTenantValidator_OIDCKeyfuncFailure_DoesNotBreakTenantKeys(t *testing.T) {
	t.Parallel()

	// Set up tenant key registry
	registry := NewStaticKeyRegistry()
	tenantPEM, tenantPrivateKey := generateTestECKey(t)
	tenantKey := &KeyInfo{
		KeyID:        "tenant-key-1",
		TenantID:     "acme",
		Algorithm:    "ES256",
		PublicKeyPEM: tenantPEM,
		IsActive:     true,
	}
	if err := registry.AddKey(tenantKey); err != nil {
		t.Fatalf("AddKey failed: %v", err)
	}

	// Validator with a failing OIDC keyfunc
	validator, err := NewMultiTenantValidator(MultiTenantValidatorConfig{
		KeyRegistry:  registry,
		OIDCIssuer:   "https://auth.example.com/",
		OIDCAudience: "https://api.odin.io",
		OIDCKeyfunc:  mockFailingOIDCKeyfunc(),
	})
	if err != nil {
		t.Fatalf("NewMultiTenantValidator failed: %v", err)
	}

	// Tenant token (different issuer) should still work
	tenantClaims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-123",
			Issuer:    "https://tenant.example.com/",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		TenantID: "acme",
	}

	tenantToken := createTestToken(t, tenantKey, tenantPrivateKey, tenantClaims)

	ctx := context.Background()
	gotClaims, err := validator.ValidateToken(ctx, tenantToken)
	if err != nil {
		t.Fatalf("ValidateToken() for tenant token unexpected error: %v", err)
	}

	if gotClaims.TenantID != "acme" {
		t.Errorf("TenantID = %s, want acme", gotClaims.TenantID)
	}

	// OIDC token should fail due to keyfunc failure
	oidcClaims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-456",
			Issuer:    "https://auth.example.com/", // Matches OIDCIssuer
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		TenantID: "acme",
	}

	// Need to sign with some key (using tenant key, but it will fail at keyfunc)
	oidcToken := createTestToken(t, tenantKey, tenantPrivateKey, oidcClaims)

	_, err = validator.ValidateToken(ctx, oidcToken)
	if err == nil {
		t.Error("Expected error for OIDC token with failing keyfunc")
	}
}
