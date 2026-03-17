package platform

import (
	"strings"
	"testing"
	"time"
)

func TestBaseConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  BaseConfig
		wantErr bool
	}{
		{
			name:    "valid defaults",
			config:  BaseConfig{LogLevel: "info", LogFormat: "json", Environment: "local"},
			wantErr: false,
		},
		{
			name:    "valid dev environment",
			config:  BaseConfig{LogLevel: "debug", LogFormat: "pretty", Environment: "dev"},
			wantErr: false,
		},
		{
			name:    "valid prod environment",
			config:  BaseConfig{LogLevel: "warn", LogFormat: "json", Environment: "prod"},
			wantErr: false,
		},
		{
			name:    "valid fatal log level",
			config:  BaseConfig{LogLevel: "fatal", LogFormat: "json", Environment: "local"},
			wantErr: false,
		},
		{
			name:    "valid custom environment",
			config:  BaseConfig{LogLevel: "error", LogFormat: "json", Environment: "custom-env"},
			wantErr: false,
		},
		{
			name:    "invalid log format text",
			config:  BaseConfig{LogLevel: "info", LogFormat: "text", Environment: "local"},
			wantErr: true,
		},
		{
			name:    "invalid log level",
			config:  BaseConfig{LogLevel: "invalid", LogFormat: "json", Environment: "local"},
			wantErr: true,
		},
		{
			name:    "invalid log format",
			config:  BaseConfig{LogLevel: "info", LogFormat: "xml", Environment: "local"},
			wantErr: true,
		},
		{
			name:    "empty environment",
			config:  BaseConfig{LogLevel: "info", LogFormat: "json", Environment: ""},
			wantErr: true,
		},
		{
			name:    "whitespace-only environment",
			config:  BaseConfig{LogLevel: "info", LogFormat: "json", Environment: "   "},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("BaseConfig.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestProvisioningClientConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		config        ProvisioningClientConfig
		wantErr       bool
		errorContains string
	}{
		{
			name:    "valid defaults",
			config:  ProvisioningClientConfig{ProvisioningGRPCAddr: "localhost:9090", GRPCReconnectDelay: time.Second, GRPCReconnectMaxDelay: 30 * time.Second},
			wantErr: false,
		},
		{
			name:          "empty addr",
			config:        ProvisioningClientConfig{ProvisioningGRPCAddr: "", GRPCReconnectDelay: time.Second, GRPCReconnectMaxDelay: 30 * time.Second},
			wantErr:       true,
			errorContains: "PROVISIONING_GRPC_ADDR",
		},
		{
			name:          "delay too small",
			config:        ProvisioningClientConfig{ProvisioningGRPCAddr: "localhost:9090", GRPCReconnectDelay: 50 * time.Millisecond, GRPCReconnectMaxDelay: 30 * time.Second},
			wantErr:       true,
			errorContains: "PROVISIONING_GRPC_RECONNECT_DELAY",
		},
		{
			name:    "delay exactly 100ms",
			config:  ProvisioningClientConfig{ProvisioningGRPCAddr: "localhost:9090", GRPCReconnectDelay: 100 * time.Millisecond, GRPCReconnectMaxDelay: 30 * time.Second},
			wantErr: false,
		},
		{
			name:          "max delay less than delay",
			config:        ProvisioningClientConfig{ProvisioningGRPCAddr: "localhost:9090", GRPCReconnectDelay: 5 * time.Second, GRPCReconnectMaxDelay: time.Second},
			wantErr:       true,
			errorContains: "PROVISIONING_GRPC_RECONNECT_MAX_DELAY",
		},
		{
			name:    "equal delay values",
			config:  ProvisioningClientConfig{ProvisioningGRPCAddr: "localhost:9090", GRPCReconnectDelay: 5 * time.Second, GRPCReconnectMaxDelay: 5 * time.Second},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("ProvisioningClientConfig.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errorContains != "" && err != nil && !strings.Contains(err.Error(), tt.errorContains) {
				t.Errorf("error should contain %q, got: %v", tt.errorContains, err)
			}
		})
	}
}

func TestHTTPTimeoutConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		config        HTTPTimeoutConfig
		wantErr       bool
		errorContains string
	}{
		{
			name:    "valid defaults",
			config:  HTTPTimeoutConfig{HTTPReadTimeout: 15 * time.Second, HTTPWriteTimeout: 15 * time.Second, HTTPIdleTimeout: 60 * time.Second},
			wantErr: false,
		},
		{
			name:          "read timeout zero",
			config:        HTTPTimeoutConfig{HTTPReadTimeout: 0, HTTPWriteTimeout: 15 * time.Second, HTTPIdleTimeout: 60 * time.Second},
			wantErr:       true,
			errorContains: "HTTP_READ_TIMEOUT",
		},
		{
			name:          "read timeout too large",
			config:        HTTPTimeoutConfig{HTTPReadTimeout: 121 * time.Second, HTTPWriteTimeout: 15 * time.Second, HTTPIdleTimeout: 60 * time.Second},
			wantErr:       true,
			errorContains: "HTTP_READ_TIMEOUT",
		},
		{
			name:          "write timeout zero",
			config:        HTTPTimeoutConfig{HTTPReadTimeout: 15 * time.Second, HTTPWriteTimeout: 0, HTTPIdleTimeout: 60 * time.Second},
			wantErr:       true,
			errorContains: "HTTP_WRITE_TIMEOUT",
		},
		{
			name:          "write timeout too large",
			config:        HTTPTimeoutConfig{HTTPReadTimeout: 15 * time.Second, HTTPWriteTimeout: 121 * time.Second, HTTPIdleTimeout: 60 * time.Second},
			wantErr:       true,
			errorContains: "HTTP_WRITE_TIMEOUT",
		},
		{
			name:          "idle timeout zero",
			config:        HTTPTimeoutConfig{HTTPReadTimeout: 15 * time.Second, HTTPWriteTimeout: 15 * time.Second, HTTPIdleTimeout: 0},
			wantErr:       true,
			errorContains: "HTTP_IDLE_TIMEOUT",
		},
		{
			name:          "idle timeout too large",
			config:        HTTPTimeoutConfig{HTTPReadTimeout: 15 * time.Second, HTTPWriteTimeout: 15 * time.Second, HTTPIdleTimeout: 301 * time.Second},
			wantErr:       true,
			errorContains: "HTTP_IDLE_TIMEOUT",
		},
		{
			name:    "boundary min values",
			config:  HTTPTimeoutConfig{HTTPReadTimeout: time.Second, HTTPWriteTimeout: time.Second, HTTPIdleTimeout: time.Second},
			wantErr: false,
		},
		{
			name:    "boundary max values",
			config:  HTTPTimeoutConfig{HTTPReadTimeout: 120 * time.Second, HTTPWriteTimeout: 120 * time.Second, HTTPIdleTimeout: 300 * time.Second},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("HTTPTimeoutConfig.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errorContains != "" && err != nil && !strings.Contains(err.Error(), tt.errorContains) {
				t.Errorf("error should contain %q, got: %v", tt.errorContains, err)
			}
		})
	}
}

func TestOIDCConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		config        OIDCConfig
		wantErr       bool
		errorContains string
	}{
		{
			name:    "both empty (disabled)",
			config:  OIDCConfig{},
			wantErr: false,
		},
		{
			name:    "both set",
			config:  OIDCConfig{OIDCIssuerURL: "https://issuer.example.com", OIDCJWKSURL: "https://issuer.example.com/.well-known/jwks.json"},
			wantErr: false,
		},
		{
			name:    "both set with audience",
			config:  OIDCConfig{OIDCIssuerURL: "https://issuer.example.com", OIDCJWKSURL: "https://issuer.example.com/jwks", OIDCAudience: "my-app"},
			wantErr: false,
		},
		{
			name:          "issuer only",
			config:        OIDCConfig{OIDCIssuerURL: "https://issuer.example.com"},
			wantErr:       true,
			errorContains: "OIDC_JWKS_URL",
		},
		{
			name:          "jwks only",
			config:        OIDCConfig{OIDCJWKSURL: "https://issuer.example.com/jwks"},
			wantErr:       true,
			errorContains: "OIDC_ISSUER_URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("OIDCConfig.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errorContains != "" && err != nil && !strings.Contains(err.Error(), tt.errorContains) {
				t.Errorf("error should contain %q, got: %v", tt.errorContains, err)
			}
		})
	}
}

func TestOIDCConfig_OIDCEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config OIDCConfig
		want   bool
	}{
		{"both empty", OIDCConfig{}, false},
		{"both set", OIDCConfig{OIDCIssuerURL: "https://issuer.example.com", OIDCJWKSURL: "https://issuer.example.com/jwks"}, true},
		{"issuer only", OIDCConfig{OIDCIssuerURL: "https://issuer.example.com"}, false},
		{"jwks only", OIDCConfig{OIDCJWKSURL: "https://issuer.example.com/jwks"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.config.OIDCEnabled(); got != tt.want {
				t.Errorf("OIDCConfig.OIDCEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestKafkaNamespaceConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		config        KafkaNamespaceConfig
		environment   string
		wantErr       bool
		errorContains string
	}{
		{
			name:        "valid defaults",
			config:      KafkaNamespaceConfig{ValidNamespaces: "local,dev,stag,prod"},
			environment: "local",
			wantErr:     false,
		},
		{
			name:        "valid with override",
			config:      KafkaNamespaceConfig{ValidNamespaces: "local,dev,stag,prod", KafkaTopicNamespaceOverride: "dev"},
			environment: "dev",
			wantErr:     false,
		},
		{
			name:          "empty valid namespaces",
			config:        KafkaNamespaceConfig{ValidNamespaces: ""},
			environment:   "local",
			wantErr:       true,
			errorContains: "VALID_NAMESPACES",
		},
		{
			name:          "override not in valid set",
			config:        KafkaNamespaceConfig{ValidNamespaces: "local,dev", KafkaTopicNamespaceOverride: "prod"},
			environment:   "dev",
			wantErr:       true,
			errorContains: "KAFKA_TOPIC_NAMESPACE_OVERRIDE",
		},
		{
			name:          "prod with override blocked",
			config:        KafkaNamespaceConfig{ValidNamespaces: "local,dev,stag,prod", KafkaTopicNamespaceOverride: "dev"},
			environment:   "prod",
			wantErr:       true,
			errorContains: "KAFKA_TOPIC_NAMESPACE_OVERRIDE",
		},
		{
			name:        "prod without override ok",
			config:      KafkaNamespaceConfig{ValidNamespaces: "local,dev,stag,prod"},
			environment: "prod",
			wantErr:     false,
		},
		{
			name:          "PROD uppercase blocked",
			config:        KafkaNamespaceConfig{ValidNamespaces: "local,dev,stag,prod", KafkaTopicNamespaceOverride: "dev"},
			environment:   "PROD",
			wantErr:       true,
			errorContains: "KAFKA_TOPIC_NAMESPACE_OVERRIDE",
		},
		{
			name:          "prod whitespace blocked",
			config:        KafkaNamespaceConfig{ValidNamespaces: "local,dev,stag,prod", KafkaTopicNamespaceOverride: "dev"},
			environment:   " prod ",
			wantErr:       true,
			errorContains: "KAFKA_TOPIC_NAMESPACE_OVERRIDE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.config.Validate(tt.environment)
			if (err != nil) != tt.wantErr {
				t.Errorf("KafkaNamespaceConfig.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errorContains != "" && err != nil && !strings.Contains(err.Error(), tt.errorContains) {
				t.Errorf("error should contain %q, got: %v", tt.errorContains, err)
			}
		})
	}
}
