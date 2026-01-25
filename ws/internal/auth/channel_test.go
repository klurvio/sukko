package auth

import (
	"testing"
)

func TestNewChannelMapper(t *testing.T) {
	t.Run("with default config", func(t *testing.T) {
		m := NewChannelMapper(DefaultChannelConfig())
		if m == nil {
			t.Fatal("expected non-nil mapper")
		}
		if m.config.Separator != "." {
			t.Errorf("expected separator '.', got %q", m.config.Separator)
		}
		if !m.config.TenantImplicit {
			t.Error("expected TenantImplicit to be true")
		}
	})

	t.Run("with empty separator defaults to dot", func(t *testing.T) {
		m := NewChannelMapper(ChannelConfig{Separator: ""})
		if m.config.Separator != "." {
			t.Errorf("expected separator '.', got %q", m.config.Separator)
		}
	})

	t.Run("with custom separator", func(t *testing.T) {
		m := NewChannelMapper(ChannelConfig{Separator: "/"})
		if m.config.Separator != "/" {
			t.Errorf("expected separator '/', got %q", m.config.Separator)
		}
	})
}

func TestChannelMapper_MapToInternal(t *testing.T) {
	m := NewChannelMapper(DefaultChannelConfig())

	tests := []struct {
		name          string
		claims        *Claims
		clientChannel string
		expected      string
	}{
		{
			name:          "with tenant",
			claims:        &Claims{TenantID: "acme"},
			clientChannel: "BTC.trade",
			expected:      "acme.BTC.trade",
		},
		{
			name:          "user scoped channel",
			claims:        &Claims{TenantID: "acme"},
			clientChannel: "user123.balances",
			expected:      "acme.user123.balances",
		},
		{
			name:          "simple channel",
			claims:        &Claims{TenantID: "globex"},
			clientChannel: "notifications",
			expected:      "globex.notifications",
		},
		{
			name:          "nil claims returns as-is",
			claims:        nil,
			clientChannel: "BTC.trade",
			expected:      "BTC.trade",
		},
		{
			name:          "empty tenant returns as-is",
			claims:        &Claims{TenantID: ""},
			clientChannel: "BTC.trade",
			expected:      "BTC.trade",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.MapToInternal(tt.claims, tt.clientChannel)
			if result != tt.expected {
				t.Errorf("MapToInternal(%v, %q) = %q, want %q",
					tt.claims, tt.clientChannel, result, tt.expected)
			}
		})
	}
}

func TestChannelMapper_MapToInternal_TenantExplicit(t *testing.T) {
	m := NewChannelMapper(ChannelConfig{
		Separator:      ".",
		TenantImplicit: false, // Tenant explicit in channel
	})

	claims := &Claims{TenantID: "acme"}
	result := m.MapToInternal(claims, "acme.BTC.trade")

	// Should not add tenant prefix when TenantImplicit is false
	if result != "acme.BTC.trade" {
		t.Errorf("expected no modification when TenantImplicit=false, got %q", result)
	}
}

func TestChannelMapper_MapToClient(t *testing.T) {
	m := NewChannelMapper(DefaultChannelConfig())

	tests := []struct {
		name            string
		internalChannel string
		expected        string
	}{
		{
			name:            "strips tenant prefix",
			internalChannel: "acme.BTC.trade",
			expected:        "BTC.trade",
		},
		{
			name:            "user scoped channel",
			internalChannel: "acme.user123.balances",
			expected:        "user123.balances",
		},
		{
			name:            "single part after tenant",
			internalChannel: "globex.notifications",
			expected:        "notifications",
		},
		{
			name:            "no separator returns as-is",
			internalChannel: "channel",
			expected:        "channel",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.MapToClient(tt.internalChannel)
			if result != tt.expected {
				t.Errorf("MapToClient(%q) = %q, want %q",
					tt.internalChannel, result, tt.expected)
			}
		})
	}
}

func TestChannelMapper_MapToClientWithTenant(t *testing.T) {
	m := NewChannelMapper(DefaultChannelConfig())

	tests := []struct {
		name            string
		internalChannel string
		expectedClient  string
		expectedTenant  string
	}{
		{
			name:            "extracts both",
			internalChannel: "acme.BTC.trade",
			expectedClient:  "BTC.trade",
			expectedTenant:  "acme",
		},
		{
			name:            "different tenant",
			internalChannel: "globex.ETH.liquidity",
			expectedClient:  "ETH.liquidity",
			expectedTenant:  "globex",
		},
		{
			name:            "no separator",
			internalChannel: "channel",
			expectedClient:  "channel",
			expectedTenant:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, tenant := m.MapToClientWithTenant(tt.internalChannel)
			if client != tt.expectedClient {
				t.Errorf("MapToClientWithTenant(%q) client = %q, want %q",
					tt.internalChannel, client, tt.expectedClient)
			}
			if tenant != tt.expectedTenant {
				t.Errorf("MapToClientWithTenant(%q) tenant = %q, want %q",
					tt.internalChannel, tenant, tt.expectedTenant)
			}
		})
	}
}

func TestChannelMapper_ExtractTenant(t *testing.T) {
	m := NewChannelMapper(DefaultChannelConfig())

	tests := []struct {
		channel  string
		expected string
	}{
		{"acme.BTC.trade", "acme"},
		{"globex.notifications", "globex"},
		{"channel", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.channel, func(t *testing.T) {
			result := m.ExtractTenant(tt.channel)
			if result != tt.expected {
				t.Errorf("ExtractTenant(%q) = %q, want %q", tt.channel, result, tt.expected)
			}
		})
	}
}

func TestChannelMapper_ValidateChannelAccess(t *testing.T) {
	m := NewChannelMapper(DefaultChannelConfig())

	tests := []struct {
		name             string
		claims           *Claims
		internalChannel  string
		crossTenantRoles []string
		expected         bool
	}{
		{
			name:            "same tenant allowed",
			claims:          &Claims{TenantID: "acme"},
			internalChannel: "acme.BTC.trade",
			expected:        true,
		},
		{
			name:            "different tenant denied",
			claims:          &Claims{TenantID: "acme"},
			internalChannel: "globex.BTC.trade",
			expected:        false,
		},
		{
			name:            "nil claims allowed (auth disabled)",
			claims:          nil,
			internalChannel: "acme.BTC.trade",
			expected:        true,
		},
		{
			name:            "empty tenant allowed (auth disabled)",
			claims:          &Claims{TenantID: ""},
			internalChannel: "acme.BTC.trade",
			expected:        true,
		},
		{
			name:            "shared channel allowed (no tenant prefix)",
			claims:          &Claims{TenantID: "acme"},
			internalChannel: "system",
			expected:        true,
		},
		{
			name:             "cross-tenant role allowed",
			claims:           &Claims{TenantID: "acme", Roles: []string{"admin"}},
			internalChannel:  "globex.BTC.trade",
			crossTenantRoles: []string{"admin", "system"},
			expected:         true,
		},
		{
			name:             "non-cross-tenant role denied",
			claims:           &Claims{TenantID: "acme", Roles: []string{"user"}},
			internalChannel:  "globex.BTC.trade",
			crossTenantRoles: []string{"admin", "system"},
			expected:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.ValidateChannelAccess(tt.claims, tt.internalChannel, tt.crossTenantRoles)
			if result != tt.expected {
				t.Errorf("ValidateChannelAccess() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestChannelMapper_ParseChannel(t *testing.T) {
	m := NewChannelMapper(DefaultChannelConfig())

	tests := []struct {
		channel        string
		expectedTenant string
		expectedParts  []string
	}{
		{
			channel:        "acme.BTC.trade",
			expectedTenant: "acme",
			expectedParts:  []string{"BTC", "trade"},
		},
		{
			channel:        "globex.user123.balances",
			expectedTenant: "globex",
			expectedParts:  []string{"user123", "balances"},
		},
		{
			channel:        "acme.notifications",
			expectedTenant: "acme",
			expectedParts:  []string{"notifications"},
		},
		{
			channel:        "single",
			expectedTenant: "single",
			expectedParts:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.channel, func(t *testing.T) {
			result := m.ParseChannel(tt.channel)

			if result.Tenant != tt.expectedTenant {
				t.Errorf("ParseChannel(%q).Tenant = %q, want %q",
					tt.channel, result.Tenant, tt.expectedTenant)
			}

			if len(result.Parts) != len(tt.expectedParts) {
				t.Errorf("ParseChannel(%q).Parts = %v, want %v",
					tt.channel, result.Parts, tt.expectedParts)
				return
			}

			for i, part := range result.Parts {
				if part != tt.expectedParts[i] {
					t.Errorf("ParseChannel(%q).Parts[%d] = %q, want %q",
						tt.channel, i, part, tt.expectedParts[i])
				}
			}

			if result.Original != tt.channel {
				t.Errorf("ParseChannel(%q).Original = %q, want %q",
					tt.channel, result.Original, tt.channel)
			}
		})
	}
}

func TestChannelMapper_BuildInternalChannel(t *testing.T) {
	m := NewChannelMapper(DefaultChannelConfig())

	tests := []struct {
		tenant   string
		parts    []string
		expected string
	}{
		{"acme", []string{"BTC", "trade"}, "acme.BTC.trade"},
		{"globex", []string{"notifications"}, "globex.notifications"},
		{"", []string{"BTC", "trade"}, "BTC.trade"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := m.BuildInternalChannel(tt.tenant, tt.parts...)
			if result != tt.expected {
				t.Errorf("BuildInternalChannel(%q, %v) = %q, want %q",
					tt.tenant, tt.parts, result, tt.expected)
			}
		})
	}
}

func TestIsSharedChannel(t *testing.T) {
	sharedPatterns := []string{"system.*", "broadcast.*", "*.global"}

	tests := []struct {
		channel  string
		expected bool
	}{
		{"system.notifications", true},
		{"system.alerts", true},
		{"broadcast.all", true},
		{"acme.global", true},
		{"acme.BTC.trade", false},
		{"user.notifications", false},
		{"system", false}, // Exact "system" doesn't match "system.*"
	}

	for _, tt := range tests {
		t.Run(tt.channel, func(t *testing.T) {
			result := IsSharedChannel(tt.channel, sharedPatterns)
			if result != tt.expected {
				t.Errorf("IsSharedChannel(%q) = %v, want %v", tt.channel, result, tt.expected)
			}
		})
	}
}

func TestMatchSimplePattern(t *testing.T) {
	tests := []struct {
		pattern  string
		value    string
		expected bool
	}{
		// Exact match
		{"system.broadcast", "system.broadcast", true},
		{"system.broadcast", "system.other", false},

		// Trailing wildcard
		{"system.*", "system.notifications", true},
		{"system.*", "system.alerts", true},
		{"system.*", "other.notifications", false},
		{"system.*", "system", false},

		// Leading wildcard
		{"*.broadcast", "system.broadcast", true},
		{"*.broadcast", "acme.broadcast", true},
		{"*.broadcast", "system.notifications", false},
	}

	for _, tt := range tests {
		name := tt.pattern + "_" + tt.value
		t.Run(name, func(t *testing.T) {
			result := matchSimplePattern(tt.pattern, tt.value)
			if result != tt.expected {
				t.Errorf("matchSimplePattern(%q, %q) = %v, want %v",
					tt.pattern, tt.value, result, tt.expected)
			}
		})
	}
}

func TestDefaultChannelConfig(t *testing.T) {
	config := DefaultChannelConfig()

	if config.Separator != "." {
		t.Errorf("expected separator '.', got %q", config.Separator)
	}

	if !config.TenantImplicit {
		t.Error("expected TenantImplicit to be true")
	}
}

func TestChannelMapper_RoundTrip(t *testing.T) {
	m := NewChannelMapper(DefaultChannelConfig())
	claims := &Claims{TenantID: "acme"}

	// Test round-trip: client → internal → client
	clientChannels := []string{
		"BTC.trade",
		"user123.balances",
		"vip.community",
		"notifications",
	}

	for _, clientChannel := range clientChannels {
		t.Run(clientChannel, func(t *testing.T) {
			internal := m.MapToInternal(claims, clientChannel)
			roundTrip := m.MapToClient(internal)

			if roundTrip != clientChannel {
				t.Errorf("round-trip failed: %q → %q → %q",
					clientChannel, internal, roundTrip)
			}
		})
	}
}
