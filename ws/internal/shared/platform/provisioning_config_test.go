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
		DatabaseURL:            "postgres://user:pass@localhost:5432/provisioning?sslmode=disable",
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
		Environment:            "development",
		CORSAllowedOrigins:     []string{"http://localhost:3000"},
		CORSMaxAge:             3600,
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
