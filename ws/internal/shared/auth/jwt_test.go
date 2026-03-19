package auth

import (
	"errors"
	"testing"
	"time"
)

func TestNewJWTValidator(t *testing.T) {
	t.Parallel()
	secret := "test-secret-key-at-least-32-bytes"
	v := NewJWTValidator(secret)

	if v == nil {
		t.Fatal("expected non-nil validator")
	}
}

func TestIssueAndValidateToken(t *testing.T) {
	t.Parallel()
	secret := "test-secret-key-at-least-32-bytes"
	v := NewJWTValidator(secret)

	appID := "sukko-web"
	expiry := 1 * time.Hour

	// Issue token
	token, expiresAt, err := v.IssueToken(appID, expiry)
	if err != nil {
		t.Fatalf("failed to issue token: %v", err)
	}

	if token == "" {
		t.Error("expected non-empty token")
	}

	// Check expiry is approximately correct (within a few seconds)
	expectedExpiry := time.Now().Add(expiry)
	if expiresAt.Before(expectedExpiry.Add(-5*time.Second)) || expiresAt.After(expectedExpiry.Add(5*time.Second)) {
		t.Errorf("expiry time mismatch: got %v, expected around %v", expiresAt, expectedExpiry)
	}

	// Validate token
	claims, err := v.ValidateToken(token)
	if err != nil {
		t.Fatalf("failed to validate token: %v", err)
	}

	if claims.AppID() != appID {
		t.Errorf("appID mismatch: got %q, want %q", claims.AppID(), appID)
	}
}

func TestValidateToken_MissingToken(t *testing.T) {
	t.Parallel()
	v := NewJWTValidator("test-secret-key-at-least-32-bytes")

	_, err := v.ValidateToken("")
	if !errors.Is(err, ErrMissingToken) {
		t.Errorf("expected ErrMissingToken, got %v", err)
	}
}

func TestValidateToken_InvalidToken(t *testing.T) {
	t.Parallel()
	v := NewJWTValidator("test-secret-key-at-least-32-bytes")

	_, err := v.ValidateToken("invalid.token.here")
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("expected ErrInvalidToken, got %v", err)
	}
}

func TestValidateToken_WrongSecret(t *testing.T) {
	t.Parallel()
	v1 := NewJWTValidator("secret-one-at-least-32-bytes-long")
	v2 := NewJWTValidator("secret-two-at-least-32-bytes-long")

	// Issue token with v1
	token, _, err := v1.IssueToken("test-app", 1*time.Hour)
	if err != nil {
		t.Fatalf("failed to issue token: %v", err)
	}

	// Try to validate with v2
	_, err = v2.ValidateToken(token)
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("expected ErrInvalidToken for wrong secret, got %v", err)
	}
}

func TestValidateToken_ExpiredToken(t *testing.T) {
	t.Parallel()
	v := NewJWTValidator("test-secret-key-at-least-32-bytes")

	// Issue token that expires immediately
	token, _, err := v.IssueToken("test-app", -1*time.Hour) // negative expiry = already expired
	if err != nil {
		t.Fatalf("failed to issue token: %v", err)
	}

	_, err = v.ValidateToken(token)
	if !errors.Is(err, ErrTokenExpired) {
		t.Errorf("expected ErrTokenExpired, got %v", err)
	}
}

func TestClaimsAppID(t *testing.T) {
	t.Parallel()
	v := NewJWTValidator("test-secret-key-at-least-32-bytes")

	testCases := []struct {
		appID string
	}{
		{"sukko-web"},
		{"trading-bot-1"},
		{"mobile-app"},
		{"service-account"},
	}

	for _, tc := range testCases {
		t.Run(tc.appID, func(t *testing.T) {
			t.Parallel()
			token, _, err := v.IssueToken(tc.appID, 1*time.Hour)
			if err != nil {
				t.Fatalf("failed to issue token: %v", err)
			}

			claims, err := v.ValidateToken(token)
			if err != nil {
				t.Fatalf("failed to validate token: %v", err)
			}

			if claims.AppID() != tc.appID {
				t.Errorf("appID mismatch: got %q, want %q", claims.AppID(), tc.appID)
			}
		})
	}
}

func TestIssueTokenWithTenant(t *testing.T) {
	t.Parallel()
	v := NewJWTValidator("test-secret-key-at-least-32-bytes")

	testCases := []struct {
		name     string
		appID    string
		tenantID string
	}{
		{"with tenant", "acme-web", "acme-corp"},
		{"empty tenant", "legacy-app", ""},
		{"multi-word tenant", "trading-bot", "beta-finance-ltd"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			token, expiresAt, err := v.IssueTokenWithTenant(tc.appID, tc.tenantID, 1*time.Hour)
			if err != nil {
				t.Fatalf("failed to issue token: %v", err)
			}

			if token == "" {
				t.Error("expected non-empty token")
			}

			// Validate token
			claims, err := v.ValidateToken(token)
			if err != nil {
				t.Fatalf("failed to validate token: %v", err)
			}

			if claims.AppID() != tc.appID {
				t.Errorf("appID mismatch: got %q, want %q", claims.AppID(), tc.appID)
			}

			if claims.Tenant() != tc.tenantID {
				t.Errorf("tenantID mismatch: got %q, want %q", claims.Tenant(), tc.tenantID)
			}

			// Check expiry is approximately correct
			expectedExpiry := time.Now().Add(1 * time.Hour)
			if expiresAt.Before(expectedExpiry.Add(-5*time.Second)) || expiresAt.After(expectedExpiry.Add(5*time.Second)) {
				t.Errorf("expiry time mismatch: got %v, expected around %v", expiresAt, expectedExpiry)
			}
		})
	}
}

func TestIssueTokenDelegatesToIssueTokenWithTenant(t *testing.T) {
	t.Parallel()
	v := NewJWTValidator("test-secret-key-at-least-32-bytes")

	// IssueToken should create a token with empty tenant ID
	token, _, err := v.IssueToken("test-app", 1*time.Hour)
	if err != nil {
		t.Fatalf("failed to issue token: %v", err)
	}

	claims, err := v.ValidateToken(token)
	if err != nil {
		t.Fatalf("failed to validate token: %v", err)
	}

	if claims.AppID() != "test-app" {
		t.Errorf("appID mismatch: got %q, want %q", claims.AppID(), "test-app")
	}

	// Tenant should be empty when using IssueToken
	if claims.Tenant() != "" {
		t.Errorf("expected empty tenant for IssueToken, got %q", claims.Tenant())
	}
}
