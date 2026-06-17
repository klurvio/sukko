package platform

import (
	"testing"
	"time"
)

func newValidWebhookWorkerConfig() *WebhookWorkerConfig {
	return &WebhookWorkerConfig{
		BaseConfig: BaseConfig{
			LogLevel:    "info",
			LogFormat:   "json",
			Environment: "local",
		},
		WebhookWorkerGRPCAddr: "localhost:9091",
		GRPCReconnectDelay:    time.Second,
		GRPCReconnectMaxDelay: 30 * time.Second,
		CredentialsConfig: CredentialsConfig{
			CredentialsEncryptionKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		},
		Port:              8083,
		WorkerConcurrency: 50,
		RetryQueueSize:    10000,
		DeliveryTimeout:   10 * time.Second,
		CacheTTL:          30 * time.Second,
		WebhookAllowHTTP:  false,
		ValkeyConfig:      ValkeyClientConfig{Addrs: []string{"localhost:6379"}},
		InternalToken:     "test-internal-token-that-is-at-least-32-chars",
	}
}

func TestWebhookWorkerConfig_Validate_Valid(t *testing.T) {
	t.Parallel()
	cfg := newValidWebhookWorkerConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("valid config should not error: %v", err)
	}
}

func TestWebhookWorkerConfig_Validate_WorkerConcurrency(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		concurrency int
		wantErr     bool
	}{
		{"zero", 0, true},
		{"min valid", 1, false},
		{"max valid", 500, false},
		{"above max", 501, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidWebhookWorkerConfig()
			cfg.WorkerConcurrency = tt.concurrency
			err := cfg.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestWebhookWorkerConfig_Validate_RetryQueueSize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		size    int
		wantErr bool
	}{
		{"below min", 99, true},
		{"min valid", 100, false},
		{"max valid", 100_000, false},
		{"above max", 100_001, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidWebhookWorkerConfig()
			cfg.RetryQueueSize = tt.size
			err := cfg.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestWebhookWorkerConfig_Validate_DeliveryTimeout(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		timeout time.Duration
		wantErr bool
	}{
		{"zero", 0, true},
		{"below min", 999 * time.Millisecond, true},
		{"min valid", time.Second, false},
		{"max valid", 30 * time.Second, false},
		{"above max", 31 * time.Second, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidWebhookWorkerConfig()
			cfg.DeliveryTimeout = tt.timeout
			err := cfg.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestWebhookWorkerConfig_Validate_CacheTTL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		ttl     time.Duration
		wantErr bool
	}{
		{"below min", 4 * time.Second, true},
		{"min valid", 5 * time.Second, false},
		{"max valid", 5 * time.Minute, false},
		{"above max", 5*time.Minute + time.Second, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidWebhookWorkerConfig()
			cfg.CacheTTL = tt.ttl
			err := cfg.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestWebhookWorkerConfig_Validate_Port(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		port    int
		wantErr bool
	}{
		{"zero", 0, true},
		{"min valid", 1, false},
		{"max valid", MaxPort, false},
		{"above max", MaxPort + 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidWebhookWorkerConfig()
			cfg.Port = tt.port
			err := cfg.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestWebhookWorkerConfig_Validate_WebhookAllowHTTP(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		allowHTTP   bool
		environment string
		wantErr     bool
	}{
		{"false in prod — ok", false, "prod", false},
		{"true in local — ok", true, "local", false},
		{"true in prod — error", true, "prod", true},
		{"true in dev — error", true, "dev", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidWebhookWorkerConfig()
			cfg.WebhookAllowHTTP = tt.allowHTTP
			cfg.Environment = tt.environment
			err := cfg.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestWebhookWorkerConfig_Validate_CredentialsEncryptionKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{"empty", "", true},
		{"valid hex", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", false},
		{"too short hex", "0123456789abcdef", true},
		{"garbage", "not-a-key", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidWebhookWorkerConfig()
			cfg.CredentialsEncryptionKey = tt.key
			err := cfg.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestWebhookWorkerConfig_Validate_InternalToken(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		token   string
		wantErr bool
	}{
		{"empty", "", true},
		{"too short (31 chars)", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", true},
		{"minimum valid (32 chars)", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false},
		{"long token", "test-internal-token-that-is-at-least-32-chars", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidWebhookWorkerConfig()
			cfg.InternalToken = tt.token
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestWebhookWorkerConfig_Validate_ValkeyAddrs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		addrs   []string
		wantErr bool
	}{
		{"nil addrs", nil, true},
		{"empty slice", []string{}, true},
		{"whitespace only", []string{"   "}, true},
		{"valid addr", []string{"localhost:6379"}, false},
		{"multiple addrs", []string{"host1:6379", "host2:6379"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidWebhookWorkerConfig()
			cfg.ValkeyConfig.Addrs = tt.addrs
			err := cfg.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestWebhookWorkerConfig_Validate_WebhookWorkerGRPCAddr(t *testing.T) {
	t.Parallel()
	cfg := newValidWebhookWorkerConfig()
	cfg.WebhookWorkerGRPCAddr = ""
	if err := cfg.Validate(); err == nil {
		t.Error("empty WEBHOOK_WORKER_PROVISIONING_GRPC_ADDR should error")
	}
}
