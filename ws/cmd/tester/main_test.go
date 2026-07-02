package main

import (
	"testing"
	"time"

	kafkashared "github.com/klurvio/sukko/internal/shared/kafka"
	"github.com/klurvio/sukko/internal/shared/platform"
)

func TestBuildSecurityConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		cfg      platform.MessageBackendConfig
		wantSASL *kafkashared.SASLConfig
		wantTLS  *kafkashared.TLSConfig
	}{
		{
			name: "both disabled returns nil/nil",
			cfg:  platform.MessageBackendConfig{},
		},
		{
			name: "SASL scram-sha-256",
			cfg: platform.MessageBackendConfig{
				KafkaSASLEnabled:   true,
				KafkaSASLMechanism: kafkashared.MechanismSCRAMSHA256,
				KafkaSASLUsername:  "user",
				KafkaSASLPassword:  "pass",
			},
			wantSASL: &kafkashared.SASLConfig{
				Mechanism: kafkashared.MechanismSCRAMSHA256,
				Username:  "user",
				Password:  "pass",
			},
		},
		{
			name: "SASL scram-sha-512",
			cfg: platform.MessageBackendConfig{
				KafkaSASLEnabled:   true,
				KafkaSASLMechanism: kafkashared.MechanismSCRAMSHA512,
				KafkaSASLUsername:  "u2",
				KafkaSASLPassword:  "p2",
			},
			wantSASL: &kafkashared.SASLConfig{
				Mechanism: kafkashared.MechanismSCRAMSHA512,
				Username:  "u2",
				Password:  "p2",
			},
		},
		{
			name: "TLS with CAPath",
			cfg: platform.MessageBackendConfig{
				KafkaTLSEnabled: true,
				KafkaTLSCAPath:  "/etc/kafka/ca.pem",
			},
			wantTLS: &kafkashared.TLSConfig{
				Enabled: true,
				CAPath:  "/etc/kafka/ca.pem",
			},
		},
		{
			name: "TLS insecure",
			cfg: platform.MessageBackendConfig{
				KafkaTLSEnabled:  true,
				KafkaTLSInsecure: true,
			},
			wantTLS: &kafkashared.TLSConfig{
				Enabled:            true,
				InsecureSkipVerify: true,
			},
		},
		{
			name: "both SASL and TLS",
			cfg: platform.MessageBackendConfig{
				KafkaSASLEnabled:   true,
				KafkaSASLMechanism: kafkashared.MechanismSCRAMSHA256,
				KafkaSASLUsername:  "u",
				KafkaSASLPassword:  "p",
				KafkaTLSEnabled:    true,
				KafkaTLSInsecure:   true,
			},
			wantSASL: &kafkashared.SASLConfig{
				Mechanism: kafkashared.MechanismSCRAMSHA256,
				Username:  "u",
				Password:  "p",
			},
			wantTLS: &kafkashared.TLSConfig{
				Enabled:            true,
				InsecureSkipVerify: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotSASL, gotTLS := buildSecurityConfig(tt.cfg)
			if tt.wantSASL == nil {
				if gotSASL != nil {
					t.Errorf("wantSASL nil, got %+v", gotSASL)
				}
			} else {
				if gotSASL == nil {
					t.Fatal("wantSASL non-nil, got nil")
				}
				if gotSASL.Mechanism != tt.wantSASL.Mechanism {
					t.Errorf("Mechanism = %q, want %q", gotSASL.Mechanism, tt.wantSASL.Mechanism)
				}
				if gotSASL.Username != tt.wantSASL.Username {
					t.Errorf("Username = %q, want %q", gotSASL.Username, tt.wantSASL.Username)
				}
				if gotSASL.Password != tt.wantSASL.Password {
					t.Errorf("Password mismatch")
				}
			}
			if tt.wantTLS == nil {
				if gotTLS != nil {
					t.Errorf("wantTLS nil, got %+v", gotTLS)
				}
			} else {
				if gotTLS == nil {
					t.Fatal("wantTLS non-nil, got nil")
				}
				if gotTLS.Enabled != tt.wantTLS.Enabled {
					t.Errorf("TLS.Enabled = %v, want %v", gotTLS.Enabled, tt.wantTLS.Enabled)
				}
				if gotTLS.InsecureSkipVerify != tt.wantTLS.InsecureSkipVerify {
					t.Errorf("TLS.InsecureSkipVerify = %v, want %v", gotTLS.InsecureSkipVerify, tt.wantTLS.InsecureSkipVerify)
				}
				if gotTLS.CAPath != tt.wantTLS.CAPath {
					t.Errorf("TLS.CAPath = %q, want %q", gotTLS.CAPath, tt.wantTLS.CAPath)
				}
			}
		})
	}
}

func TestBuildRunnerConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cfg         TesterConfig
		wantSASLNil bool
		wantTLSNil  bool
	}{
		{
			name: "fields pass through without security",
			cfg: TesterConfig{
				MessageBackendConfig: platform.MessageBackendConfig{
					MessageBackendBase: platform.MessageBackendBase{
						MessageBackend: platform.MessageBackendDirect,
					},
				},
				GatewayURL:      "ws://gw:3000",
				ProvisioningURL: "http://prov:8080",
				JWTLifetime:     5 * time.Minute,
			},
			wantSASLNil: true,
			wantTLSNil:  true,
		},
		{
			name: "Kafka backend fields pass through",
			cfg: TesterConfig{
				MessageBackendConfig: platform.MessageBackendConfig{
					MessageBackendBase: platform.MessageBackendBase{
						MessageBackend: platform.MessageBackendKafka,
						KafkaBrokers:   "broker:9092",
					},
				},
			},
			wantSASLNil: true,
			wantTLSNil:  true,
		},
		{
			name: "SASL and TLS carrier fields populated",
			cfg: TesterConfig{
				MessageBackendConfig: platform.MessageBackendConfig{
					MessageBackendBase: platform.MessageBackendBase{
						MessageBackend: platform.MessageBackendKafka,
						KafkaBrokers:   "broker:9092",
					},
					KafkaSASLEnabled:   true,
					KafkaSASLMechanism: kafkashared.MechanismSCRAMSHA256,
					KafkaSASLUsername:  "user",
					KafkaSASLPassword:  "pass",
					KafkaTLSEnabled:    true,
					KafkaTLSInsecure:   true,
				},
			},
			wantSASLNil: false,
			wantTLSNil:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildRunnerConfig(tt.cfg)
			if got.GatewayURL != tt.cfg.GatewayURL {
				t.Errorf("GatewayURL = %q, want %q", got.GatewayURL, tt.cfg.GatewayURL)
			}
			if got.MessageBackend != tt.cfg.MessageBackend {
				t.Errorf("MessageBackend = %q, want %q", got.MessageBackend, tt.cfg.MessageBackend)
			}
			if tt.wantSASLNil && got.KafkaSASL != nil {
				t.Errorf("KafkaSASL: expected nil, got %+v", got.KafkaSASL)
			}
			if !tt.wantSASLNil && got.KafkaSASL == nil {
				t.Error("KafkaSASL: expected non-nil, got nil")
			}
			if tt.wantTLSNil && got.KafkaTLS != nil {
				t.Errorf("KafkaTLS: expected nil, got %+v", got.KafkaTLS)
			}
			if !tt.wantTLSNil && got.KafkaTLS == nil {
				t.Error("KafkaTLS: expected non-nil, got nil")
			}
		})
	}
}
