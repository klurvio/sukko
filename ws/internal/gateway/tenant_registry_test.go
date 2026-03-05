package gateway

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/shared/types"
)

func TestNoopTenantRegistry_GetTenantByIssuer(t *testing.T) {
	t.Parallel()

	logger := zerolog.Nop()
	registry := NewNoopTenantRegistry(logger)

	tenantID, err := registry.GetTenantByIssuer(context.Background(), "https://auth.example.com")

	if !errors.Is(err, types.ErrIssuerNotFound) {
		t.Errorf("expected ErrIssuerNotFound, got %v", err)
	}
	if tenantID != "" {
		t.Errorf("expected empty tenant ID, got %q", tenantID)
	}
}

func TestNoopTenantRegistry_GetOIDCConfig(t *testing.T) {
	t.Parallel()

	logger := zerolog.Nop()
	registry := NewNoopTenantRegistry(logger)

	config, err := registry.GetOIDCConfig(context.Background(), "test-tenant")

	if !errors.Is(err, types.ErrOIDCNotConfigured) {
		t.Errorf("expected ErrOIDCNotConfigured, got %v", err)
	}
	if config != nil {
		t.Errorf("expected nil config, got %+v", config)
	}
}

func TestNoopTenantRegistry_GetChannelRules(t *testing.T) {
	t.Parallel()

	logger := zerolog.Nop()
	registry := NewNoopTenantRegistry(logger)

	rules, err := registry.GetChannelRules(context.Background(), "test-tenant")

	if !errors.Is(err, types.ErrChannelRulesNotFound) {
		t.Errorf("expected ErrChannelRulesNotFound, got %v", err)
	}
	if rules != nil {
		t.Errorf("expected nil rules, got %+v", rules)
	}
}

func TestNoopTenantRegistry_Close(t *testing.T) {
	t.Parallel()

	logger := zerolog.Nop()
	registry := NewNoopTenantRegistry(logger)

	err := registry.Close()

	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestNoopTenantRegistry_ImplementsInterface(t *testing.T) {
	t.Parallel()

	var _ TenantRegistry = (*NoopTenantRegistry)(nil)
}

// mockTenantRegistry is a test helper that implements TenantRegistry.
type mockTenantRegistry struct {
	tenantsByIssuer map[string]string
	oidcConfigs     map[string]*types.TenantOIDCConfig
	channelRules    map[string]*types.ChannelRules
}

func newMockTenantRegistry() *mockTenantRegistry {
	return &mockTenantRegistry{
		tenantsByIssuer: make(map[string]string),
		oidcConfigs:     make(map[string]*types.TenantOIDCConfig),
		channelRules:    make(map[string]*types.ChannelRules),
	}
}

func (m *mockTenantRegistry) GetTenantByIssuer(_ context.Context, issuerURL string) (string, error) {
	if tenantID, ok := m.tenantsByIssuer[issuerURL]; ok {
		return tenantID, nil
	}
	return "", types.ErrIssuerNotFound
}

func (m *mockTenantRegistry) GetOIDCConfig(_ context.Context, tenantID string) (*types.TenantOIDCConfig, error) {
	if config, ok := m.oidcConfigs[tenantID]; ok {
		return config, nil
	}
	return nil, types.ErrOIDCNotConfigured
}

func (m *mockTenantRegistry) GetChannelRules(_ context.Context, tenantID string) (*types.ChannelRules, error) {
	if rules, ok := m.channelRules[tenantID]; ok {
		return rules, nil
	}
	return nil, types.ErrChannelRulesNotFound
}

func (m *mockTenantRegistry) Close() error {
	return nil
}

var _ TenantRegistry = (*mockTenantRegistry)(nil)

func TestMockTenantRegistry_GetTenantByIssuer(t *testing.T) {
	t.Parallel()

	registry := newMockTenantRegistry()
	registry.tenantsByIssuer["https://auth.acme.com"] = "acme"

	tests := []struct {
		name      string
		issuerURL string
		wantID    string
		wantErr   error
	}{
		{
			name:      "found",
			issuerURL: "https://auth.acme.com",
			wantID:    "acme",
			wantErr:   nil,
		},
		{
			name:      "not found",
			issuerURL: "https://unknown.com",
			wantID:    "",
			wantErr:   types.ErrIssuerNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tenantID, err := registry.GetTenantByIssuer(context.Background(), tt.issuerURL)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("expected error %v, got %v", tt.wantErr, err)
			}
			if tenantID != tt.wantID {
				t.Errorf("expected tenant ID %q, got %q", tt.wantID, tenantID)
			}
		})
	}
}

func TestMockTenantRegistry_GetChannelRules(t *testing.T) {
	t.Parallel()

	registry := newMockTenantRegistry()
	registry.channelRules["acme"] = &types.ChannelRules{
		Public: []string{"*.public"},
		GroupMappings: map[string][]string{
			"traders": {"*.trade", "*.liquidity"},
		},
	}

	tests := []struct {
		name     string
		tenantID string
		wantErr  error
	}{
		{
			name:     "found",
			tenantID: "acme",
			wantErr:  nil,
		},
		{
			name:     "not found",
			tenantID: "unknown",
			wantErr:  types.ErrChannelRulesNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rules, err := registry.GetChannelRules(context.Background(), tt.tenantID)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("expected error %v, got %v", tt.wantErr, err)
			}
			if tt.wantErr == nil && rules == nil {
				t.Error("expected rules, got nil")
			}
		})
	}
}
