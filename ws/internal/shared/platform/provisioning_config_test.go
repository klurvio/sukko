package platform

import (
	"strings"
	"testing"
)

func newValidProvisioningConfig() *ProvisioningConfig {
	return &ProvisioningConfig{
		Addr:                   ":8080",
		LogLevel:               "info",
		LogFormat:              "json",
		DatabaseDriver:         "sqlite",
		DatabasePath:           "odin.db",
		AutoMigrate:            true,
		GRPCPort:               9090,
		DBMaxOpenConns:         25,
		DBMaxIdleConns:         5,
		DefaultPartitions:      3,
		DefaultRetentionMs:     604800000,
		MaxTopicsPerTenant:     50,
		MaxPartitionsPerTenant: 200,
		DeprovisionGraceDays:   30,
		APIRateLimitPerMinute:  60,
		ValidNamespaces:        "local,dev,stag,prod",
		TopicNamespaceOverride: "",
		Environment:            "local",
		CORSAllowedOrigins:     []string{"http://localhost:3000"},
		CORSMaxAge:             3600,
	}
}

func TestProvisioningConfig_Validate_Valid(t *testing.T) {
	t.Parallel()
	cfg := newValidProvisioningConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("Valid config should not error: %v", err)
	}
}

func TestProvisioningConfig_Validate_DatabaseDriver(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		driver      string
		dbURL       string
		shouldError bool
	}{
		{"sqlite no url", "sqlite", "", false},
		{"postgres with url", "postgres", "postgres://localhost/db", false},
		{"postgres without url", "postgres", "", true},
		{"invalid driver", "mysql", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidProvisioningConfig()
			cfg.DatabaseDriver = tt.driver
			cfg.DatabaseURL = tt.dbURL
			err := cfg.Validate()
			if tt.shouldError && err == nil {
				t.Error("Should error")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Should not error: %v", err)
			}
		})
	}
}

func TestProvisioningConfig_Validate_AdminToken(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		token       string
		environment string
		shouldError bool
	}{
		{"empty token", "", "prod", false},
		{"long token prod", "this-is-a-long-admin-token-123", "prod", false},
		{"short token prod", "short", "prod", true},
		{"short token dev", "short", "dev", false},
		{"short token local", "short", "local", false},
		{"short token development", "short", "development", false},
		{"exactly 16 chars prod", "exactly16chars!!", "prod", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidProvisioningConfig()
			cfg.AdminToken = tt.token
			cfg.Environment = tt.environment
			err := cfg.Validate()
			if tt.shouldError && err == nil {
				t.Error("Should error")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Should not error: %v", err)
			}
		})
	}
}

func TestProvisioningConfig_Validate_GRPCPort(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		port        int
		shouldError bool
	}{
		{"valid default", 9090, false},
		{"valid min", 1, false},
		{"valid max", 65535, false},
		{"zero", 0, true},
		{"negative", -1, true},
		{"too large", 65536, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidProvisioningConfig()
			cfg.GRPCPort = tt.port
			err := cfg.Validate()
			if tt.shouldError && err == nil {
				t.Error("Should error")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Should not error: %v", err)
			}
		})
	}
}

func TestProvisioningConfig_Validate_ProdOverrideBlocked(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		environment string
		override    string
		shouldError bool
	}{
		{"prod_with_override", "prod", "dev", true},
		{"prod_no_override", "prod", "", false},
		{"dev_with_override", "dev", "prod", false},
		{"dev_no_override", "dev", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidProvisioningConfig()
			cfg.Environment = tt.environment
			cfg.TopicNamespaceOverride = tt.override
			err := cfg.Validate()
			if tt.shouldError {
				if err == nil {
					t.Error("Should error on prod override")
				} else if !strings.Contains(err.Error(), "KAFKA_TOPIC_NAMESPACE_OVERRIDE") {
					t.Errorf("Error should mention KAFKA_TOPIC_NAMESPACE_OVERRIDE: %v", err)
				}
			} else {
				if err != nil {
					t.Errorf("Should not error: %v", err)
				}
			}
		})
	}
}
