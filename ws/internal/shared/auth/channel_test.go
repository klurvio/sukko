package auth

import (
	"testing"
)

func TestNewChannelMapper(t *testing.T) {
	t.Parallel()
	t.Run("with default config", func(t *testing.T) {
		t.Parallel()
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
		t.Parallel()
		m := NewChannelMapper(ChannelConfig{Separator: ""})
		if m.config.Separator != "." {
			t.Errorf("expected separator '.', got %q", m.config.Separator)
		}
	})

	t.Run("with custom separator", func(t *testing.T) {
		t.Parallel()
		m := NewChannelMapper(ChannelConfig{Separator: "/"})
		if m.config.Separator != "/" {
			t.Errorf("expected separator '/', got %q", m.config.Separator)
		}
	})
}

func TestChannelMapper_MapToInternal(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			result := m.MapToInternal(tt.claims, tt.clientChannel)
			if result != tt.expected {
				t.Errorf("MapToInternal(%v, %q) = %q, want %q",
					tt.claims, tt.clientChannel, result, tt.expected)
			}
		})
	}
}

func TestChannelMapper_MapToInternal_TenantExplicit(t *testing.T) {
	t.Parallel()
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

func TestChannelMapper_MapToInternalWithTenant(t *testing.T) {
	t.Parallel()
	m := NewChannelMapper(DefaultChannelConfig())

	tests := []struct {
		name          string
		tenantID      string
		clientChannel string
		expected      string
	}{
		{
			name:          "normal mapping",
			tenantID:      "odin",
			clientChannel: "BTC.trade",
			expected:      "odin.BTC.trade",
		},
		{
			name:          "user scoped channel",
			tenantID:      "acme",
			clientChannel: "user123.balances",
			expected:      "acme.user123.balances",
		},
		{
			name:          "single part channel",
			tenantID:      "odin",
			clientChannel: "notifications",
			expected:      "odin.notifications",
		},
		{
			name:          "empty tenantID returns as-is",
			tenantID:      "",
			clientChannel: "BTC.trade",
			expected:      "BTC.trade",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := m.MapToInternalWithTenant(tt.tenantID, tt.clientChannel)
			if result != tt.expected {
				t.Errorf("MapToInternalWithTenant(%q, %q) = %q, want %q",
					tt.tenantID, tt.clientChannel, result, tt.expected)
			}
		})
	}
}

func TestChannelMapper_MapToInternalWithTenant_TenantExplicit(t *testing.T) {
	t.Parallel()
	m := NewChannelMapper(ChannelConfig{
		Separator:      ".",
		TenantImplicit: false, // Tenant explicit in channel
	})

	// Should not add tenant prefix when TenantImplicit is false
	result := m.MapToInternalWithTenant("acme", "BTC.trade")
	if result != "BTC.trade" {
		t.Errorf("expected no modification when TenantImplicit=false, got %q", result)
	}
}

func TestChannelMapper_MapToClient(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			result := m.MapToClient(tt.internalChannel)
			if result != tt.expected {
				t.Errorf("MapToClient(%q) = %q, want %q",
					tt.internalChannel, result, tt.expected)
			}
		})
	}
}

func TestChannelMapper_MapToClientWithTenant(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
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
	t.Parallel()
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
			t.Parallel()
			result := m.ExtractTenant(tt.channel)
			if result != tt.expected {
				t.Errorf("ExtractTenant(%q) = %q, want %q", tt.channel, result, tt.expected)
			}
		})
	}
}

func TestChannelMapper_ValidateChannelAccess(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			result := m.ValidateChannelAccess(tt.claims, tt.internalChannel, tt.crossTenantRoles)
			if result != tt.expected {
				t.Errorf("ValidateChannelAccess() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestChannelMapper_ParseChannel(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
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
	t.Parallel()
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
			t.Parallel()
			result := m.BuildInternalChannel(tt.tenant, tt.parts...)
			if result != tt.expected {
				t.Errorf("BuildInternalChannel(%q, %v) = %q, want %q",
					tt.tenant, tt.parts, result, tt.expected)
			}
		})
	}
}

func TestIsSharedChannel(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			result := IsSharedChannel(tt.channel, sharedPatterns)
			if result != tt.expected {
				t.Errorf("IsSharedChannel(%q) = %v, want %v", tt.channel, result, tt.expected)
			}
		})
	}
}

func TestMatchWildcard_SharedChannel(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			result := MatchWildcard(tt.pattern, tt.value)
			if result != tt.expected {
				t.Errorf("MatchWildcard(%q, %q) = %v, want %v",
					tt.pattern, tt.value, result, tt.expected)
			}
		})
	}
}

func TestDefaultChannelConfig(t *testing.T) {
	t.Parallel()
	config := DefaultChannelConfig()

	if config.Separator != "." {
		t.Errorf("expected separator '.', got %q", config.Separator)
	}

	if !config.TenantImplicit {
		t.Error("expected TenantImplicit to be true")
	}
}

func TestChannelMapper_RoundTrip(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			internal := m.MapToInternal(claims, clientChannel)
			roundTrip := m.MapToClient(internal)

			if roundTrip != clientChannel {
				t.Errorf("round-trip failed: %q → %q → %q",
					clientChannel, internal, roundTrip)
			}
		})
	}
}

func TestValidateInternalChannel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		channel string
		wantErr bool
	}{
		{
			name:    "valid 3-part channel",
			channel: "acme.BTC.trade",
			wantErr: false,
		},
		{
			name:    "valid 4-part channel",
			channel: "acme.user123.private.balances",
			wantErr: false,
		},
		{
			name:    "empty channel",
			channel: "",
			wantErr: true,
		},
		{
			name:    "only 2 parts",
			channel: "BTC.trade",
			wantErr: true,
		},
		{
			name:    "only 1 part",
			channel: "trade",
			wantErr: true,
		},
		{
			name:    "empty first part",
			channel: ".BTC.trade",
			wantErr: true,
		},
		{
			name:    "empty middle part",
			channel: "acme..trade",
			wantErr: true,
		},
		{
			name:    "empty last part",
			channel: "acme.BTC.",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateInternalChannel(tt.channel)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateInternalChannel(%q) error = %v, wantErr %v", tt.channel, err, tt.wantErr)
			}
		})
	}
}

func TestIsValidInternalChannel(t *testing.T) {
	t.Parallel()
	// Valid channels
	if !IsValidInternalChannel("acme.BTC.trade") {
		t.Error("IsValidInternalChannel('acme.BTC.trade') = false, want true")
	}

	// Invalid channels
	if IsValidInternalChannel("BTC.trade") {
		t.Error("IsValidInternalChannel('BTC.trade') = true, want false")
	}
}

func TestParseInternalChannel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		channel      string
		wantTenant   string
		wantCategory string
		wantErr      bool
	}{
		{
			name:         "valid 3-part channel",
			channel:      "acme.BTC.trade",
			wantTenant:   "acme",
			wantCategory: "trade",
			wantErr:      false,
		},
		{
			name:         "valid 4-part channel",
			channel:      "acme.user123.private.balances",
			wantTenant:   "acme",
			wantCategory: "balances",
			wantErr:      false,
		},
		{
			name:    "invalid channel (2 parts)",
			channel: "BTC.trade",
			wantErr: true,
		},
		{
			name:    "empty channel",
			channel: "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tenant, category, err := ParseInternalChannel(tt.channel)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseInternalChannel(%q) error = %v, wantErr %v", tt.channel, err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			if tenant != tt.wantTenant {
				t.Errorf("ParseInternalChannel(%q) tenant = %q, want %q", tt.channel, tenant, tt.wantTenant)
			}
			if category != tt.wantCategory {
				t.Errorf("ParseInternalChannel(%q) category = %q, want %q", tt.channel, category, tt.wantCategory)
			}
		})
	}
}
