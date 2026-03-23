package token

import (
	"testing"
	"time"
)

func TestGenerateAndDecodeHMAC(t *testing.T) {
	t.Parallel()

	secret := "test-secret-minimum-32-bytes!!!!"

	tests := []struct {
		name    string
		cfg     GenerateConfig
		wantSub string
	}{
		{
			name: "basic token",
			cfg: GenerateConfig{
				Subject:  "user123",
				TenantID: "acme",
				Secret:   secret,
				TTL:      time.Hour,
			},
			wantSub: "user123",
		},
		{
			name: "with roles and groups",
			cfg: GenerateConfig{
				Subject:  "admin",
				TenantID: "acme",
				Roles:    []string{"admin", "editor"},
				Groups:   []string{"team-a"},
				Secret:   secret,
			},
			wantSub: "admin",
		},
		{
			name: "minimal token",
			cfg: GenerateConfig{
				Secret: secret,
			},
			wantSub: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tokenStr, err := Generate(tt.cfg)
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}

			if tokenStr == "" {
				t.Fatal("empty token")
			}

			// Decode without secret
			decoded, err := Decode(tokenStr)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}

			if !decoded.Valid {
				t.Errorf("expected valid token, got error: %s", decoded.Error)
			}

			if sub, ok := decoded.Claims["sub"]; ok {
				if sub != tt.wantSub {
					t.Errorf("sub = %v, want %v", sub, tt.wantSub)
				}
			} else if tt.wantSub != "" {
				t.Error("expected sub claim")
			}

			// ValidateWithSecret
			verified, err := ValidateWithSecret(tokenStr, secret)
			if err != nil {
				t.Fatalf("ValidateWithSecret: %v", err)
			}

			if !verified.Valid {
				t.Errorf("expected valid with correct secret, got: %s", verified.Error)
			}
		})
	}
}

func TestDecodeExpiredToken(t *testing.T) {
	t.Parallel()

	tokenStr, err := Generate(GenerateConfig{
		Subject: "user",
		Secret:  "test-secret-minimum-32-bytes!!!!",
		TTL:     -time.Hour, // already expired
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	decoded, err := Decode(tokenStr)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if decoded.Valid {
		t.Error("expected expired token to be invalid")
	}
	if decoded.Error != "token expired" {
		t.Errorf("error = %q, want 'token expired'", decoded.Error)
	}
}

func TestValidateWithSecretWrongSecret(t *testing.T) {
	t.Parallel()

	tokenStr, err := Generate(GenerateConfig{
		Subject: "user",
		Secret:  "correct-secret-minimum-32-bytes!",
		TTL:     time.Hour,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	result, err := ValidateWithSecret(tokenStr, "wrong-secret")
	if err != nil {
		t.Fatalf("ValidateWithSecret: %v", err)
	}

	if result.Valid {
		t.Error("expected invalid with wrong secret")
	}
}

func TestGenerateNoSecret(t *testing.T) {
	t.Parallel()

	_, err := Generate(GenerateConfig{
		Subject: "user",
	})
	if err == nil {
		t.Error("expected error without secret or key file")
	}
}

func TestGenerateUnsupportedAlgorithm(t *testing.T) {
	t.Parallel()

	_, err := Generate(GenerateConfig{
		Subject:   "user",
		Secret:    "test-secret",
		Algorithm: "INVALID",
	})
	if err == nil {
		t.Error("expected error for unsupported algorithm")
	}
}
