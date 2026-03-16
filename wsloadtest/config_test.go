package main

import (
	"strings"
	"testing"
	"time"
)

func TestConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  Config
		wantErr string
	}{
		{
			name:    "empty_ws_url",
			config:  Config{WSURL: ""},
			wantErr: "WS_URL is required",
		},
		{
			name:    "invalid_target_connections",
			config:  Config{WSURL: "ws://localhost", TargetConnections: 0, RampRate: 1},
			wantErr: "TARGET_CONNECTIONS must be >= 1",
		},
		{
			name:    "invalid_ramp_rate",
			config:  Config{WSURL: "ws://localhost", TargetConnections: 1, RampRate: 0},
			wantErr: "RAMP_RATE must be >= 1",
		},
		{
			name:    "invalid_subscription_mode",
			config:  Config{WSURL: "ws://localhost", TargetConnections: 1, RampRate: 1, Channels: []string{"sukko.BTC.trade"}, SubscriptionMode: "invalid"},
			wantErr: "SUBSCRIPTION_MODE must be all/single/random",
		},
		{
			name:    "empty_channels",
			config:  Config{WSURL: "ws://localhost", TargetConnections: 1, RampRate: 1, Channels: []string{}, SubscriptionMode: "all"},
			wantErr: "CHANNELS is required",
		},
		{
			name:    "channels_per_client_zero",
			config:  Config{WSURL: "ws://localhost", TargetConnections: 1, RampRate: 1, Channels: []string{"sukko.BTC.trade"}, SubscriptionMode: "random", ChannelsPerClient: 0},
			wantErr: "CHANNELS_PER_CLIENT must be >= 1",
		},
		{
			name:    "channels_per_client_exceeds_channels",
			config:  Config{WSURL: "ws://localhost", TargetConnections: 1, RampRate: 1, Channels: []string{"sukko.BTC.trade"}, SubscriptionMode: "random", ChannelsPerClient: 5},
			wantErr: "CHANNELS_PER_CLIENT (5) cannot exceed number of channels (1)",
		},
		{
			name:    "invalid_log_level",
			config:  Config{WSURL: "ws://localhost", TargetConnections: 1, RampRate: 1, Channels: []string{"sukko.BTC.trade"}, SubscriptionMode: "all", LogLevel: "invalid", PongWait: 60 * time.Second, PingPeriod: 45 * time.Second},
			wantErr: "invalid log level",
		},
		{
			name:    "ping_period_equals_pong_wait",
			config:  Config{WSURL: "ws://localhost", TargetConnections: 1, RampRate: 1, Channels: []string{"sukko.BTC.trade"}, SubscriptionMode: "all", LogLevel: "info", PongWait: 60 * time.Second, PingPeriod: 60 * time.Second},
			wantErr: "WS_PING_PERIOD",
		},
		{
			name:    "ping_period_exceeds_pong_wait",
			config:  Config{WSURL: "ws://localhost", TargetConnections: 1, RampRate: 1, Channels: []string{"sukko.BTC.trade"}, SubscriptionMode: "all", LogLevel: "info", PongWait: 60 * time.Second, PingPeriod: 90 * time.Second},
			wantErr: "WS_PING_PERIOD",
		},
		{
			name: "valid_config",
			config: Config{
				WSURL:             "ws://localhost",
				TargetConnections: 100,
				RampRate:          10,
				Channels:          []string{"sukko.BTC.trade"},
				TenantID:          "sukko",
				SubscriptionMode:  "random",
				ChannelsPerClient: 1,
				LogLevel:          "info",
				PongWait:          120 * time.Second,
				PingPeriod:        90 * time.Second,
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.config.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %v, want containing %q", err, tt.wantErr)
				}
			}
		})
	}
}

func TestConfig_ValidateChannels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		channels []string
		wantErr  string
	}{
		{
			name:     "valid_channels",
			channels: []string{"sukko.BTC.trade", "sukko.ETH.liquidity"},
			wantErr:  "",
		},
		{
			name:     "valid_aggregate_channel",
			channels: []string{"sukko.all.trade"},
			wantErr:  "",
		},
		{
			name:     "invalid_two_parts",
			channels: []string{"BTC.trade"},
			wantErr:  "must have format {tenant}.{identifier}.{category}",
		},
		{
			name:     "invalid_one_part",
			channels: []string{"trade"},
			wantErr:  "must have format {tenant}.{identifier}.{category}",
		},
		{
			name:     "mixed_valid_invalid",
			channels: []string{"sukko.BTC.trade", "invalid"},
			wantErr:  "must have format {tenant}.{identifier}.{category}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &Config{
				WSURL:             "ws://localhost",
				TargetConnections: 1,
				RampRate:          1,
				Channels:          tt.channels,
				SubscriptionMode:  "all",
				LogLevel:          "info",
				PongWait:          120 * time.Second,
				PingPeriod:        90 * time.Second,
			}
			err := cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %v, want containing %q", err, tt.wantErr)
				}
			}
		})
	}
}

func TestConfig_ValidateSubscriptionModes(t *testing.T) {
	t.Parallel()

	validModes := []string{"all", "single", "random"}
	for _, mode := range validModes {
		t.Run("valid_mode_"+mode, func(t *testing.T) {
			t.Parallel()
			cfg := &Config{
				WSURL:             "ws://localhost",
				TargetConnections: 1,
				RampRate:          1,
				Channels:          []string{"sukko.BTC.trade"},
				SubscriptionMode:  mode,
				ChannelsPerClient: 1,
				LogLevel:          "info",
				PongWait:          120 * time.Second,
				PingPeriod:        90 * time.Second,
			}
			if err := cfg.Validate(); err != nil {
				t.Errorf("unexpected error for mode %s: %v", mode, err)
			}
		})
	}
}

func TestValidateLogLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		level   string
		wantErr bool
	}{
		{"trace", false},
		{"debug", false},
		{"info", false},
		{"warn", false},
		{"error", false},
		{"fatal", false},
		{"panic", false},
		{"invalid", true},
		{"INFO", true}, // case sensitive
		{"", true},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			t.Parallel()
			err := validateLogLevel(tt.level)
			if tt.wantErr && err == nil {
				t.Errorf("expected error for level %q", tt.level)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error for level %q: %v", tt.level, err)
			}
		})
	}
}

func TestConfig_GetLogLevel(t *testing.T) {
	t.Parallel()

	cfg := &Config{LogLevel: "debug"}
	level := cfg.GetLogLevel()
	if level.String() != "debug" {
		t.Errorf("GetLogLevel() = %s, want debug", level.String())
	}

	cfg.LogLevel = "invalid"
	level = cfg.GetLogLevel()
	if level.String() != "info" {
		t.Errorf("GetLogLevel() with invalid = %s, want info (default)", level.String())
	}
}
