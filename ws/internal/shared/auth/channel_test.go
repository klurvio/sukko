package auth

import (
	"testing"
)

func TestExtractChannelTenant(t *testing.T) {
	t.Parallel()
	tests := []struct {
		channel   string
		separator string
		expected  string
	}{
		{"acme.BTC.trade", ".", "acme"},
		{"globex.notifications", ".", "globex"},
		{"channel", ".", ""},
		{"", ".", ""},
		{"acme/BTC/trade", "/", "acme"},
	}

	for _, tt := range tests {
		t.Run(tt.channel, func(t *testing.T) {
			t.Parallel()
			result := extractChannelTenant(tt.channel, tt.separator)
			if result != tt.expected {
				t.Errorf("extractChannelTenant(%q, %q) = %q, want %q",
					tt.channel, tt.separator, result, tt.expected)
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
