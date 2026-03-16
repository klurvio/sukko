package main

import (
	"testing"
	"time"
)

func TestConfig_Validate_ProdProtection(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		allowProd bool
		wantErr   bool
	}{
		{"local allowed", "local", false, false},
		{"dev allowed", "dev", false, false},
		{"stag allowed", "stag", false, false},
		{"prod blocked", "prod", false, true},
		{"PROD blocked", "PROD", false, true},
		{"prod allowed with flag", "prod", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				KafkaBrokers:   "localhost:9092",
				KafkaNamespace: tt.namespace,
				AllowProd:      tt.allowProd,
				TimingMode:     TimingModePoisson,
				PoissonLambda:  100,
				Identifiers:    "BTC",
				Categories:     "trade",
			}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfig_Validate_Namespace(t *testing.T) {
	tests := []struct {
		namespace string
		wantErr   bool
	}{
		{"local", false},
		{"dev", false},
		{"stag", false},
		{"prod", true}, // blocked without AllowProd
		{"invalid", true},
		{"loadtest", true},
	}

	for _, tt := range tests {
		t.Run(tt.namespace, func(t *testing.T) {
			cfg := &Config{
				KafkaBrokers:   "localhost:9092",
				KafkaNamespace: tt.namespace,
				TimingMode:     TimingModePoisson,
				PoissonLambda:  100,
				Identifiers:    "BTC",
				Categories:     "trade",
			}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfig_Validate_TimingModes(t *testing.T) {
	base := Config{
		KafkaBrokers:   "localhost:9092",
		KafkaNamespace: "local",
		Identifiers:    "BTC",
		Categories:     "trade",
	}

	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
	}{
		{
			name: "poisson valid",
			modify: func(c *Config) {
				c.TimingMode = TimingModePoisson
				c.PoissonLambda = 100
			},
		},
		{
			name: "poisson invalid lambda",
			modify: func(c *Config) {
				c.TimingMode = TimingModePoisson
				c.PoissonLambda = 0
			},
			wantErr: true,
		},
		{
			name: "uniform valid",
			modify: func(c *Config) {
				c.TimingMode = TimingModeUniform
				c.MinInterval = 10 * time.Millisecond
				c.MaxInterval = 100 * time.Millisecond
			},
		},
		{
			name: "uniform invalid min > max",
			modify: func(c *Config) {
				c.TimingMode = TimingModeUniform
				c.MinInterval = 100 * time.Millisecond
				c.MaxInterval = 10 * time.Millisecond
			},
			wantErr: true,
		},
		{
			name: "burst valid",
			modify: func(c *Config) {
				c.TimingMode = TimingModeBurst
				c.BurstCount = 10
				c.BurstPause = time.Second
			},
		},
		{
			name: "burst invalid count",
			modify: func(c *Config) {
				c.TimingMode = TimingModeBurst
				c.BurstCount = 0
				c.BurstPause = time.Second
			},
			wantErr: true,
		},
		{
			name: "invalid timing mode",
			modify: func(c *Config) {
				c.TimingMode = "invalid"
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base
			tt.modify(&cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfig_Validate_Channels(t *testing.T) {
	base := Config{
		KafkaBrokers:   "localhost:9092",
		KafkaNamespace: "local",
		TimingMode:     TimingModePoisson,
		PoissonLambda:  100,
	}

	tests := []struct {
		name        string
		channels    string
		identifiers string
		categories  string
		wantErr     bool
	}{
		{"static channels valid", "odin.BTC.trade", "", "", false},
		{"static channels invalid format", "invalid", "", "", true},
		{"dynamic generation valid", "", "BTC,ETH", "trade", false},
		{"dynamic missing identifiers", "", "", "trade", true},
		{"dynamic missing categories", "", "BTC", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base
			cfg.Channels = tt.channels
			cfg.Identifiers = tt.identifiers
			cfg.Categories = tt.categories
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfig_Validate_SASL(t *testing.T) {
	base := Config{
		KafkaBrokers:   "localhost:9092",
		KafkaNamespace: "local",
		TimingMode:     TimingModePoisson,
		PoissonLambda:  100,
		Identifiers:    "BTC",
		Categories:     "trade",
	}

	tests := []struct {
		name      string
		mechanism string
		username  string
		password  string
		wantErr   bool
	}{
		{"no sasl", "", "", "", false},
		{"valid scram-sha-256", "scram-sha-256", "user", "pass", false},
		{"valid scram-sha-512", "scram-sha-512", "user", "pass", false},
		{"missing username", "scram-sha-256", "", "pass", true},
		{"missing password", "scram-sha-256", "user", "", true},
		{"invalid mechanism", "plain", "user", "pass", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base
			cfg.KafkaSASLMechanism = tt.mechanism
			cfg.KafkaSASLUsername = tt.username
			cfg.KafkaSASLPassword = tt.password
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfig_GetBrokers(t *testing.T) {
	cfg := &Config{KafkaBrokers: "broker1:9092, broker2:9092 , broker3:9092"}
	brokers := cfg.GetBrokers()
	expected := []string{"broker1:9092", "broker2:9092", "broker3:9092"}

	if len(brokers) != len(expected) {
		t.Fatalf("expected %d brokers, got %d", len(expected), len(brokers))
	}
	for i, b := range brokers {
		if b != expected[i] {
			t.Errorf("broker[%d] = %q, want %q", i, b, expected[i])
		}
	}
}

func TestConfig_GetChannels(t *testing.T) {
	tests := []struct {
		name     string
		channels string
		expected []string
	}{
		{"empty", "", nil},
		{"single", "odin.BTC.trade", []string{"odin.BTC.trade"}},
		{"multiple", "odin.BTC.trade, odin.ETH.trade", []string{"odin.BTC.trade", "odin.ETH.trade"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Channels: tt.channels}
			channels := cfg.GetChannels()
			if len(channels) != len(tt.expected) {
				t.Fatalf("expected %d channels, got %d", len(tt.expected), len(channels))
			}
			for i, c := range channels {
				if c != tt.expected[i] {
					t.Errorf("channel[%d] = %q, want %q", i, c, tt.expected[i])
				}
			}
		})
	}
}
