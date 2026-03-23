package main

import (
	"strings"
	"testing"

	"github.com/klurvio/sukko/internal/shared/platform"
)

func TestTesterConfig_Validate(t *testing.T) {
	t.Parallel()

	validConfig := func() TesterConfig {
		return TesterConfig{
			BaseConfig: platform.BaseConfig{
				LogLevel:    "info",
				LogFormat:   "json",
				Environment: "test",
			},
			Port:            8090,
			GatewayURL:      "ws://localhost:3000",
			ProvisioningURL: "http://localhost:8080",
			MessageBackend:  "direct",
		}
	}

	tests := []struct {
		name    string
		modify  func(*TesterConfig)
		wantErr string
	}{
		{
			name:   "valid config",
			modify: func(_ *TesterConfig) {},
		},
		{
			name:    "port zero",
			modify:  func(c *TesterConfig) { c.Port = 0 },
			wantErr: "TESTER_PORT must be between 1 and 65535",
		},
		{
			name:    "port too high",
			modify:  func(c *TesterConfig) { c.Port = 70000 },
			wantErr: "TESTER_PORT must be between 1 and 65535",
		},
		{
			name:    "empty gateway URL",
			modify:  func(c *TesterConfig) { c.GatewayURL = "" },
			wantErr: "GATEWAY_URL is required",
		},
		{
			name:    "empty provisioning URL",
			modify:  func(c *TesterConfig) { c.ProvisioningURL = "" },
			wantErr: "PROVISIONING_URL is required",
		},
		{
			name:    "invalid message backend",
			modify:  func(c *TesterConfig) { c.MessageBackend = "grpc" },
			wantErr: "MESSAGE_BACKEND must be 'direct' or 'kafka'",
		},
		{
			name: "kafka without brokers",
			modify: func(c *TesterConfig) {
				c.MessageBackend = "kafka"
				c.KafkaBrokers = ""
			},
			wantErr: "KAFKA_BROKERS required when MESSAGE_BACKEND=kafka",
		},
		{
			name: "kafka with brokers",
			modify: func(c *TesterConfig) {
				c.MessageBackend = "kafka"
				c.KafkaBrokers = "localhost:9092"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := validConfig()
			tt.modify(&cfg)
			err := cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
			}
		})
	}
}
