package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"
)

func TestOIDCConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     OIDCConfig
		wantErr error
	}{
		{
			name: "valid_config_with_all_fields",
			cfg: OIDCConfig{
				IssuerURL: "https://auth.example.com/",
				JWKSURL:   "https://auth.example.com/.well-known/jwks.json",
				Audience:  "https://api.example.com",
			},
			wantErr: nil,
		},
		{
			name: "valid_config_minimal",
			cfg: OIDCConfig{
				JWKSURL: "https://auth.example.com/.well-known/jwks.json",
			},
			wantErr: nil,
		},
		{
			name: "missing_jwks_url",
			cfg: OIDCConfig{
				IssuerURL: "https://auth.example.com/",
				Audience:  "https://api.example.com",
			},
			wantErr: ErrOIDCMissingJWKSURL,
		},
		{
			name:    "empty_config",
			cfg:     OIDCConfig{},
			wantErr: ErrOIDCMissingJWKSURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.cfg.Validate()

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Errorf("Validate() unexpected error = %v", err)
			}
		})
	}
}

func TestNewOIDCKeyfunc_MissingJWKSURL(t *testing.T) {
	t.Parallel()

	logger := zerolog.Nop()
	ctx := context.Background()

	cfg := OIDCConfig{
		IssuerURL: "https://auth.example.com/",
		// JWKSURL intentionally empty
	}

	result, err := NewOIDCKeyfunc(ctx, cfg, logger)

	if result != nil {
		t.Error("Expected nil result for missing JWKS URL")
	}

	if !errors.Is(err, ErrOIDCMissingJWKSURL) {
		t.Errorf("Expected ErrOIDCMissingJWKSURL, got %v", err)
	}
}

func TestNewOIDCKeyfunc_InvalidJWKSURL(t *testing.T) {
	t.Parallel()

	logger := zerolog.Nop()
	ctx := context.Background()

	cfg := OIDCConfig{
		IssuerURL: "https://auth.example.com/",
		JWKSURL:   "not-a-valid-url",
	}

	result, err := NewOIDCKeyfunc(ctx, cfg, logger)

	if result != nil {
		t.Error("Expected nil result for invalid JWKS URL")
	}

	if err == nil {
		t.Error("Expected error for invalid JWKS URL")
	}
}
