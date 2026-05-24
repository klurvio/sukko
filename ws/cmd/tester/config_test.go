package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	provauth "github.com/klurvio/sukko/internal/provisioning/auth"
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
			Port:             8090,
			GatewayURL:       "ws://localhost:3000",
			ProvisioningURL:  "http://localhost:8080",
			MessageBackend:   "direct",
			AdminKeyID:       provauth.BootstrapAdminKeyID, // mirrors envDefault:"bootstrap-0"
			JWTLifetime:      15 * time.Minute,
			JWTRefreshBefore: 2 * time.Minute,
			KeyExpiry:        24 * time.Hour,
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
			name:    "invalid message backend grpc",
			modify:  func(c *TesterConfig) { c.MessageBackend = "grpc" },
			wantErr: "MESSAGE_BACKEND must be 'direct', 'kafka', or 'nats'",
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
		{
			name:    "JWT lifetime zero",
			modify:  func(c *TesterConfig) { c.JWTLifetime = 0 },
			wantErr: "TESTER_JWT_LIFETIME must be positive",
		},
		{
			name:    "JWT refresh before >= lifetime",
			modify:  func(c *TesterConfig) { c.JWTRefreshBefore = 15 * time.Minute },
			wantErr: "TESTER_JWT_REFRESH_BEFORE must be positive and less than",
		},
		{
			name:    "JWT refresh before zero",
			modify:  func(c *TesterConfig) { c.JWTRefreshBefore = 0 },
			wantErr: "TESTER_JWT_REFRESH_BEFORE must be positive and less than",
		},
		{
			name:    "key expiry less than JWT lifetime",
			modify:  func(c *TesterConfig) { c.KeyExpiry = 1 * time.Minute },
			wantErr: "TESTER_KEY_EXPIRY",
		},
		{
			name: "nats with URLs",
			modify: func(c *TesterConfig) {
				c.MessageBackend = "nats"
				c.NATSJetStreamURLs = "nats://localhost:4222"
			},
		},
		{
			name: "nats without URLs",
			modify: func(c *TesterConfig) {
				c.MessageBackend = "nats"
				c.NATSJetStreamURLs = ""
			},
			wantErr: "NATS_JETSTREAM_URLS required when MESSAGE_BACKEND=nats",
		},
		{
			name:   "admin key file not set",
			modify: func(_ *TesterConfig) {},
			// AdminKeyFile="" is valid — local dev mode
		},
		{
			name: "admin key file valid",
			modify: func(c *TesterConfig) {
				_, priv, err := ed25519.GenerateKey(rand.Reader)
				if err != nil {
					return
				}
				path := filepath.Join(os.TempDir(), "test-admin-valid.key")
				_ = os.WriteFile(path, []byte(priv), 0o600)
				c.AdminKeyFile = path
			},
		},
		{
			name: "admin key file missing path",
			modify: func(c *TesterConfig) {
				c.AdminKeyFile = "/nonexistent/path/to/key.bin"
			},
			wantErr: "/nonexistent/path/to/key.bin",
		},
		{
			name: "admin key file invalid bytes",
			modify: func(c *TesterConfig) {
				path := filepath.Join(os.TempDir(), "test-admin-short.key")
				_ = os.WriteFile(path, make([]byte, 32), 0o600) // 32 bytes, should be 64
				c.AdminKeyFile = path
			},
			wantErr: "must be 64 bytes",
		},
		{
			name: "admin key file set but key id empty",
			modify: func(c *TesterConfig) {
				_, priv, err := ed25519.GenerateKey(rand.Reader)
				if err != nil {
					return
				}
				path := filepath.Join(os.TempDir(), "test-admin-empty-kid.key")
				_ = os.WriteFile(path, []byte(priv), 0o600)
				c.AdminKeyFile = path
				c.AdminKeyID = ""
			},
			wantErr: "TESTER_ADMIN_KEY_ID must not be empty",
		},
		{
			name: "admin key ID too long",
			modify: func(c *TesterConfig) {
				_, priv, err := ed25519.GenerateKey(rand.Reader)
				if err != nil {
					return
				}
				path := filepath.Join(os.TempDir(), "test-admin-long-kid.key")
				_ = os.WriteFile(path, []byte(priv), 0o600)
				c.AdminKeyFile = path
				c.AdminKeyID = strings.Repeat("a", 64) // 64 chars, max is 63
			},
			wantErr: "TESTER_ADMIN_KEY_ID",
		},
		{
			name: "admin key ID starts with digit",
			modify: func(c *TesterConfig) {
				_, priv, err := ed25519.GenerateKey(rand.Reader)
				if err != nil {
					return
				}
				path := filepath.Join(os.TempDir(), "test-admin-digit-kid.key")
				_ = os.WriteFile(path, []byte(priv), 0o600)
				c.AdminKeyFile = path
				c.AdminKeyID = "1-invalid"
			},
			wantErr: "TESTER_ADMIN_KEY_ID",
		},
		{
			name: "admin key ID uppercase rejected",
			modify: func(c *TesterConfig) {
				_, priv, err := ed25519.GenerateKey(rand.Reader)
				if err != nil {
					return
				}
				path := filepath.Join(os.TempDir(), "test-admin-upper-kid.key")
				_ = os.WriteFile(path, []byte(priv), 0o600)
				c.AdminKeyFile = path
				c.AdminKeyID = "Bootstrap-0" // uppercase B rejected
			},
			wantErr: "TESTER_ADMIN_KEY_ID",
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

// TestTesterConfig_DefaultAdminKeyID_EqualsBootstrapConstant verifies that the
// TESTER_ADMIN_KEY_ID env default matches the canonical BootstrapAdminKeyID constant.
// This prevents the tester from silently using a wrong default in remote mode.
func TestTesterConfig_DefaultAdminKeyID_EqualsBootstrapConstant(t *testing.T) {
	t.Parallel()

	// A zero-value TesterConfig uses Go's zero values, not env defaults.
	// We compare the constant values directly to ensure they stay in sync.
	const envDefault = "bootstrap-0"
	if provauth.BootstrapAdminKeyID != envDefault {
		t.Errorf("BootstrapAdminKeyID = %q, want %q (must match TESTER_ADMIN_KEY_ID envDefault)",
			provauth.BootstrapAdminKeyID, envDefault)
	}
}
