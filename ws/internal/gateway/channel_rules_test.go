package gateway

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/shared/types"
)

func TestNoopChannelRulesProvider_GetChannelRules(t *testing.T) {
	t.Parallel()

	logger := zerolog.Nop()
	provider := NewNoopChannelRulesProvider(logger)

	rules, err := provider.GetChannelRules(context.Background(), "test-tenant")

	if !errors.Is(err, types.ErrChannelRulesNotFound) {
		t.Errorf("expected ErrChannelRulesNotFound, got %v", err)
	}
	if rules != nil {
		t.Errorf("expected nil rules, got %+v", rules)
	}
}

func TestNoopChannelRulesProvider_Close(t *testing.T) {
	t.Parallel()

	logger := zerolog.Nop()
	provider := NewNoopChannelRulesProvider(logger)

	err := provider.Close()

	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestNoopChannelRulesProvider_ImplementsInterface(t *testing.T) {
	t.Parallel()

	var _ ChannelRulesProvider = (*NoopChannelRulesProvider)(nil)
}

// mockChannelRulesProvider is a test helper that implements ChannelRulesProvider.
type mockChannelRulesProvider struct {
	channelRules map[string]*types.ChannelRules
}

func newMockChannelRulesProvider() *mockChannelRulesProvider {
	return &mockChannelRulesProvider{
		channelRules: make(map[string]*types.ChannelRules),
	}
}

func (m *mockChannelRulesProvider) GetChannelRules(_ context.Context, tenantID string) (*types.ChannelRules, error) {
	if rules, ok := m.channelRules[tenantID]; ok {
		return rules, nil
	}
	return nil, types.ErrChannelRulesNotFound
}

func (m *mockChannelRulesProvider) Close() error {
	return nil
}

var _ ChannelRulesProvider = (*mockChannelRulesProvider)(nil)

func TestMockChannelRulesProvider_GetChannelRules(t *testing.T) {
	t.Parallel()

	provider := newMockChannelRulesProvider()
	provider.channelRules["acme"] = &types.ChannelRules{
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
			rules, err := provider.GetChannelRules(context.Background(), tt.tenantID)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("expected error %v, got %v", tt.wantErr, err)
			}
			if tt.wantErr == nil && rules == nil {
				t.Error("expected rules, got nil")
			}
		})
	}
}
