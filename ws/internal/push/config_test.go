package push

import (
	"strings"
	"testing"
	"time"

	"github.com/klurvio/sukko/internal/shared/platform"
)

// validConfig returns a Config with all valid defaults for testing.
// Each test case mutates one field to test a specific validation rule.
func validConfig() Config {
	return Config{
		BaseConfig: platform.BaseConfig{
			LogLevel:    "info",
			LogFormat:   "json",
			Environment: "local",
		},
		ProvisioningClientConfig: platform.ProvisioningClientConfig{
			ProvisioningGRPCAddr:  "localhost:9090",
			GRPCReconnectDelay:    1 * time.Second,
			GRPCReconnectMaxDelay: 30 * time.Second,
		},
		MessageBackendConfig: platform.MessageBackendConfig{
			MessageBackend:        "kafka",
			KafkaBrokers:          "localhost:19092",
			NATSJetStreamReplicas: 1,
			NATSJetStreamMaxAge:   24 * time.Hour,
		},
		KafkaNamespaceConfig: platform.KafkaNamespaceConfig{
			ValidNamespaces: "local,dev,stag,prod",
		},
		DatabaseDriver: "sqlite",
		DatabasePath:   "test.db",
		WorkerPoolSize: 200,
		JobQueueSize:   10000,
		GRPCPort:       3008,
		HTTPPort:       3009,
		DefaultTTL:     2419200,
		DefaultUrgency: "normal",
		MaxRetries:     3,
	}
}

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr string // Substring expected in error message; empty = no error
	}{
		{
			name:    "valid config with all defaults",
			modify:  func(_ *Config) {},
			wantErr: "",
		},
		{
			name: "direct message backend rejected",
			modify: func(c *Config) {
				c.MessageBackend = "direct"
			},
			wantErr: "push notifications require kafka or nats",
		},
		{
			name: "invalid database driver",
			modify: func(c *Config) {
				c.DatabaseDriver = "mysql"
			},
			wantErr: "invalid",
		},
		{
			name: "postgres driver with empty database URL",
			modify: func(c *Config) {
				c.DatabaseDriver = "postgres"
				c.DatabaseURL = ""
			},
			wantErr: "PUSH_DATABASE_URL is required",
		},
		{
			name: "sqlite driver with empty database path",
			modify: func(c *Config) {
				c.DatabaseDriver = "sqlite"
				c.DatabasePath = ""
			},
			wantErr: "PUSH_DATABASE_PATH is required",
		},
		{
			name: "worker pool size zero",
			modify: func(c *Config) {
				c.WorkerPoolSize = 0
			},
			wantErr: "PUSH_WORKER_POOL_SIZE must be >= 1",
		},
		{
			name: "job queue size zero",
			modify: func(c *Config) {
				c.JobQueueSize = 0
			},
			wantErr: "PUSH_JOB_QUEUE_SIZE must be >= 1",
		},
		{
			name: "gRPC port zero",
			modify: func(c *Config) {
				c.GRPCPort = 0
			},
			wantErr: "PUSH_GRPC_PORT must be > 0",
		},
		{
			name: "HTTP port zero",
			modify: func(c *Config) {
				c.HTTPPort = 0
			},
			wantErr: "PUSH_HTTP_PORT must be > 0",
		},
		{
			name: "invalid default urgency",
			modify: func(c *Config) {
				c.DefaultUrgency = "critical"
			},
			wantErr: "PUSH_DEFAULT_URGENCY",
		},
		{
			name: "negative max retries",
			modify: func(c *Config) {
				c.MaxRetries = -1
			},
			wantErr: "PUSH_MAX_RETRIES must be >= 0",
		},
		{
			name: "nats message backend is valid",
			modify: func(c *Config) {
				c.MessageBackend = "nats"
				c.NATSJetStreamURLs = "nats://localhost:4222"
			},
			wantErr: "",
		},
		{
			name: "postgres driver with valid URL passes",
			modify: func(c *Config) {
				c.DatabaseDriver = "postgres"
				c.DatabaseURL = "postgres://localhost:5432/push"
			},
			wantErr: "",
		},
		{
			name: "max retries zero is valid",
			modify: func(c *Config) {
				c.MaxRetries = 0
			},
			wantErr: "",
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
					t.Fatalf("expected no error, got: %v", err)
				}
				return
			}

			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}
