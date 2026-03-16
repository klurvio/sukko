package types

import (
	"errors"
	"strings"
	"testing"
)

func TestTenantOIDCConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		config    TenantOIDCConfig
		wantErr   error
		errSubstr string
	}{
		{
			name: "valid_config_minimal",
			config: TenantOIDCConfig{
				TenantID:  "acme-corp",
				IssuerURL: "https://acme.auth0.com/",
				Enabled:   true,
			},
			wantErr: nil,
		},
		{
			name: "valid_config_full",
			config: TenantOIDCConfig{
				TenantID:  "acme-corp",
				IssuerURL: "https://acme.auth0.com/",
				JWKSURL:   "https://acme.auth0.com/.well-known/jwks.json",
				Audience:  "https://api.sukko.io",
				Enabled:   true,
			},
			wantErr: nil,
		},
		{
			name: "missing_issuer_url",
			config: TenantOIDCConfig{
				TenantID: "acme-corp",
				Enabled:  true,
			},
			wantErr: ErrIssuerURLRequired,
		},
		{
			name: "issuer_url_not_https",
			config: TenantOIDCConfig{
				TenantID:  "acme-corp",
				IssuerURL: "http://acme.auth0.com/",
				Enabled:   true,
			},
			wantErr:   ErrInvalidIssuerURL,
			errSubstr: "HTTPS",
		},
		{
			name: "issuer_url_no_host",
			config: TenantOIDCConfig{
				TenantID:  "acme-corp",
				IssuerURL: "https:///path",
				Enabled:   true,
			},
			wantErr:   ErrInvalidIssuerURL,
			errSubstr: "host",
		},
		{
			name: "issuer_url_too_long",
			config: TenantOIDCConfig{
				TenantID:  "acme-corp",
				IssuerURL: "https://example.com/" + strings.Repeat("a", MaxIssuerURLLength),
				Enabled:   true,
			},
			wantErr: ErrIssuerURLTooLong,
		},
		{
			name: "jwks_url_not_https",
			config: TenantOIDCConfig{
				TenantID:  "acme-corp",
				IssuerURL: "https://acme.auth0.com/",
				JWKSURL:   "http://acme.auth0.com/.well-known/jwks.json",
				Enabled:   true,
			},
			wantErr:   ErrInvalidJWKSURL,
			errSubstr: "HTTPS",
		},
		{
			name: "jwks_url_no_host",
			config: TenantOIDCConfig{
				TenantID:  "acme-corp",
				IssuerURL: "https://acme.auth0.com/",
				JWKSURL:   "https:///jwks.json",
				Enabled:   true,
			},
			wantErr:   ErrInvalidJWKSURL,
			errSubstr: "host",
		},
		{
			name: "audience_too_long",
			config: TenantOIDCConfig{
				TenantID:  "acme-corp",
				IssuerURL: "https://acme.auth0.com/",
				Audience:  strings.Repeat("a", MaxAudienceLength+1),
				Enabled:   true,
			},
			wantErr: ErrAudienceTooLong,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.config.Validate()

			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
				return
			}

			if err == nil {
				t.Errorf("Validate() expected error %v, got nil", tt.wantErr)
				return
			}

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Validate() error = %v, want %v", err, tt.wantErr)
			}

			if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
				t.Errorf("Validate() error message %q should contain %q", err.Error(), tt.errSubstr)
			}
		})
	}
}

func TestTenantOIDCConfig_GetJWKSURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		config   TenantOIDCConfig
		expected string
	}{
		{
			name: "explicit_jwks_url",
			config: TenantOIDCConfig{
				IssuerURL: "https://acme.auth0.com/",
				JWKSURL:   "https://custom.example.com/jwks.json",
			},
			expected: "https://custom.example.com/jwks.json",
		},
		{
			name: "default_jwks_url_with_trailing_slash",
			config: TenantOIDCConfig{
				IssuerURL: "https://acme.auth0.com/",
			},
			expected: "https://acme.auth0.com/.well-known/jwks.json",
		},
		{
			name: "default_jwks_url_without_trailing_slash",
			config: TenantOIDCConfig{
				IssuerURL: "https://acme.auth0.com",
			},
			expected: "https://acme.auth0.com/.well-known/jwks.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.config.GetJWKSURL()
			if got != tt.expected {
				t.Errorf("GetJWKSURL() = %q, want %q", got, tt.expected)
			}
		})
	}
}
