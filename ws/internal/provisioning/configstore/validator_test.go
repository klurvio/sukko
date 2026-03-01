package configstore

import (
	"strings"
	"testing"
)

const testPublicKeyPEM = `-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEEVs/o5+uQbTjL3chynL4wXgUg2R9
q9UU8I5mEovUf86QZ7kOBIjJwqnzD1omageEHWwHdBO6B+dFabmdT9POxg==
-----END PUBLIC KEY-----`

func validMinimalConfig() *ConfigFile {
	return &ConfigFile{
		Tenants: []TenantConfig{
			{
				ID:   "test-tenant",
				Name: "Test Tenant",
				Categories: []CategoryConfig{
					{Name: "trade"},
				},
			},
		},
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		modify     func(cfg *ConfigFile)
		wantErr    bool
		errContain string
	}{
		{
			name:    "valid minimal config",
			modify:  func(_ *ConfigFile) {},
			wantErr: false,
		},
		{
			name: "empty tenants",
			modify: func(cfg *ConfigFile) {
				cfg.Tenants = nil
			},
			wantErr:    true,
			errContain: "at least one tenant",
		},
		{
			name: "missing tenant id",
			modify: func(cfg *ConfigFile) {
				cfg.Tenants[0].ID = ""
			},
			wantErr:    true,
			errContain: "id is required",
		},
		{
			name: "invalid tenant id format",
			modify: func(cfg *ConfigFile) {
				cfg.Tenants[0].ID = "AB" // uppercase and too short
			},
			wantErr:    true,
			errContain: "must match",
		},
		{
			name: "duplicate tenant ids",
			modify: func(cfg *ConfigFile) {
				cfg.Tenants = append(cfg.Tenants, TenantConfig{
					ID:         "test-tenant",
					Name:       "Dup",
					Categories: []CategoryConfig{{Name: "x"}},
				})
			},
			wantErr:    true,
			errContain: "duplicate tenant id",
		},
		{
			name: "missing name",
			modify: func(cfg *ConfigFile) {
				cfg.Tenants[0].Name = ""
			},
			wantErr:    true,
			errContain: "name is required",
		},
		{
			name: "invalid consumer type",
			modify: func(cfg *ConfigFile) {
				cfg.Tenants[0].ConsumerType = "invalid"
			},
			wantErr:    true,
			errContain: "consumer_type must be",
		},
		{
			name: "no categories",
			modify: func(cfg *ConfigFile) {
				cfg.Tenants[0].Categories = nil
			},
			wantErr:    true,
			errContain: "at least one category",
		},
		{
			name: "category missing name",
			modify: func(cfg *ConfigFile) {
				cfg.Tenants[0].Categories = []CategoryConfig{{Name: ""}}
			},
			wantErr:    true,
			errContain: "name is required",
		},
		{
			name: "key with invalid algorithm",
			modify: func(cfg *ConfigFile) {
				cfg.Tenants[0].Keys = []KeyConfig{
					{ID: "key-one", Algorithm: "INVALID", PublicKey: testPublicKeyPEM},
				}
			},
			wantErr:    true,
			errContain: "algorithm must be",
		},
		{
			name: "key with missing public key",
			modify: func(cfg *ConfigFile) {
				cfg.Tenants[0].Keys = []KeyConfig{
					{ID: "key-one", Algorithm: "ES256", PublicKey: ""},
				}
			},
			wantErr:    true,
			errContain: "public_key is required",
		},
		{
			name: "key with invalid PEM",
			modify: func(cfg *ConfigFile) {
				cfg.Tenants[0].Keys = []KeyConfig{
					{ID: "key-one", Algorithm: "ES256", PublicKey: "not-a-pem"},
				}
			},
			wantErr:    true,
			errContain: "invalid public_key PEM",
		},
		{
			name: "duplicate key ids across tenants",
			modify: func(cfg *ConfigFile) {
				cfg.Tenants[0].Keys = []KeyConfig{
					{ID: "shared-key", Algorithm: "ES256", PublicKey: testPublicKeyPEM},
				}
				cfg.Tenants = append(cfg.Tenants, TenantConfig{
					ID:         "other-tenant",
					Name:       "Other",
					Categories: []CategoryConfig{{Name: "x"}},
					Keys: []KeyConfig{
						{ID: "shared-key", Algorithm: "ES256", PublicKey: testPublicKeyPEM},
					},
				})
			},
			wantErr:    true,
			errContain: "duplicate key id",
		},
		{
			name: "invalid key id format",
			modify: func(cfg *ConfigFile) {
				cfg.Tenants[0].Keys = []KeyConfig{
					{ID: "X", Algorithm: "ES256", PublicKey: testPublicKeyPEM},
				}
			},
			wantErr:    true,
			errContain: "must match",
		},
		{
			name: "oidc with non-https issuer",
			modify: func(cfg *ConfigFile) {
				cfg.Tenants[0].OIDC = &OIDCConfig{
					IssuerURL: "http://auth.example.com",
				}
			},
			wantErr:    true,
			errContain: "must use https",
		},
		{
			name: "conflicting oidc issuers across tenants",
			modify: func(cfg *ConfigFile) {
				cfg.Tenants[0].OIDC = &OIDCConfig{
					IssuerURL: "https://auth.example.com",
				}
				cfg.Tenants = append(cfg.Tenants, TenantConfig{
					ID:         "other-tenant",
					Name:       "Other",
					Categories: []CategoryConfig{{Name: "x"}},
					OIDC: &OIDCConfig{
						IssuerURL: "https://auth.example.com",
					},
				})
			},
			wantErr:    true,
			errContain: "already used by tenant",
		},
		{
			name: "valid full config with oidc and channel rules",
			modify: func(cfg *ConfigFile) {
				cfg.Tenants[0].Keys = []KeyConfig{
					{ID: "key-one", Algorithm: "ES256", PublicKey: testPublicKeyPEM},
				}
				cfg.Tenants[0].OIDC = &OIDCConfig{
					IssuerURL: "https://auth.example.com",
					JWKSURL:   "https://auth.example.com/.well-known/jwks.json",
					Audience:  "my-api",
				}
				cfg.Tenants[0].ChannelRules = &ChannelRulesConfig{
					PublicChannels:  []string{"*.trade"},
					DefaultChannels: []string{"news"},
				}
				cfg.Tenants[0].Quotas = &QuotaConfig{
					MaxTopics:      10,
					MaxConnections: 100,
				}
			},
			wantErr: false,
		},
		{
			name: "valid consumer type shared",
			modify: func(cfg *ConfigFile) {
				cfg.Tenants[0].ConsumerType = "shared"
			},
			wantErr: false,
		},
		{
			name: "valid consumer type dedicated",
			modify: func(cfg *ConfigFile) {
				cfg.Tenants[0].ConsumerType = "dedicated"
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := validMinimalConfig()
			tt.modify(cfg)

			err := Validate(cfg)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error but got nil")
				}
				if tt.errContain != "" && !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errContain)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}
