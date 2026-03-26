package platform

import (
	"strings"
	"testing"
	"time"

	"github.com/klurvio/sukko/internal/shared/license"
	"github.com/rs/zerolog"
)

func newValidProvisioningConfig() *ProvisioningConfig {
	return &ProvisioningConfig{
		BaseConfig: BaseConfig{
			LogLevel:    "info",
			LogFormat:   "json",
			Environment: "local",
		},
		KafkaNamespaceConfig: KafkaNamespaceConfig{
			ValidNamespaces:             "local,dev,stag,prod",
			KafkaTopicNamespaceOverride: "",
		},
		HTTPTimeoutConfig: HTTPTimeoutConfig{
			HTTPReadTimeout:  15 * time.Second,
			HTTPWriteTimeout: 15 * time.Second,
			HTTPIdleTimeout:  60 * time.Second,
		},
		Addr:                       ":8080",
		DatabaseDriver:             "sqlite",
		DatabasePath:               "sukko.db",
		AutoMigrate:                true,
		GRPCPort:                   9090,
		DBMaxOpenConns:             25,
		DBMaxIdleConns:             5,
		DBConnMaxLifetime:          5 * time.Minute,
		DefaultPartitions:          3,
		DefaultRetentionMs:         604800000,
		MaxTopicsPerTenant:         50,
		MaxPartitionsPerTenant:     200,
		MaxStorageBytes:            10737418240,
		ProducerByteRate:           10485760,
		ConsumerByteRate:           52428800,
		MaxRoutingRules:            100,
		DeprovisionGraceDays:       30,
		LifecycleCheckInterval:     time.Hour,
		LifecycleManagerEnabled:    true,
		APIRateLimitPerMinute:      60,
		AdminAuthFailureThreshold:  10,
		AdminAuthBlockDuration:     60 * time.Second,
		AdminAuthCleanupInterval:   5 * time.Minute,
		AdminAuthCleanupMaxAge:     2 * time.Minute,
		KeyRegistryRefreshInterval: time.Minute,
		KeyRegistryQueryTimeout:    5 * time.Second,
		ShutdownTimeout:            30 * time.Second,
		CORSAllowedOrigins:         []string{"http://localhost:3000"},
		CORSMaxAge:                 3600,
		MaxTenantsFetchLimit:       10000,
		DeletionTimeout:            5 * time.Minute,
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

func TestProvisioningConfig_Validate_MaxRoutingRules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		value       int
		shouldError bool
	}{
		{"valid default", 100, false},
		{"valid min", 1, false},
		{"valid large", 1000, false},
		{"zero", 0, true},
		{"negative", -1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidProvisioningConfig()
			cfg.MaxRoutingRules = tt.value
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
			cfg.KafkaTopicNamespaceOverride = tt.override
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

func TestProvisioningConfig_Validate_DBConnMaxLifetime(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		driver      string
		dbURL       string
		lifetime    time.Duration
		shouldError bool
	}{
		{"postgres valid default", "postgres", "postgres://localhost/db", 5 * time.Minute, false},
		{"postgres exactly 1m", "postgres", "postgres://localhost/db", time.Minute, false},
		{"postgres below 1m", "postgres", "postgres://localhost/db", 30 * time.Second, true},
		{"postgres zero", "postgres", "postgres://localhost/db", 0, true},
		{"sqlite ignores low value", "sqlite", "", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidProvisioningConfig()
			cfg.DatabaseDriver = tt.driver
			cfg.DatabaseURL = tt.dbURL
			cfg.DBConnMaxLifetime = tt.lifetime
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

func TestProvisioningConfig_Validate_RetentionMs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		value       int64
		shouldError bool
	}{
		{"valid default 7 days", 604800000, false},
		{"valid exactly 60000", 60000, false},
		{"below minimum", 59999, true},
		{"zero", 0, true},
		{"negative", -1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidProvisioningConfig()
			cfg.DefaultRetentionMs = tt.value
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

func TestProvisioningConfig_Validate_Quotas(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		maxStorage     int64
		producerRate   int64
		consumerRate   int64
		shouldError    bool
		errorSubstring string
	}{
		{"valid defaults", 10737418240, 10485760, 52428800, false, ""},
		{"storage exactly 1MB", 1048576, 10485760, 52428800, false, ""},
		{"storage below 1MB", 1048575, 10485760, 52428800, true, "MAX_STORAGE_BYTES"},
		{"storage zero", 0, 10485760, 52428800, true, "MAX_STORAGE_BYTES"},
		{"producer rate exactly 1024", 10737418240, 1024, 52428800, false, ""},
		{"producer rate below 1024", 10737418240, 1023, 52428800, true, "PRODUCER_BYTE_RATE"},
		{"producer rate zero", 10737418240, 0, 52428800, true, "PRODUCER_BYTE_RATE"},
		{"consumer rate exactly 1024", 10737418240, 10485760, 1024, false, ""},
		{"consumer rate below 1024", 10737418240, 10485760, 1023, true, "CONSUMER_BYTE_RATE"},
		{"consumer rate zero", 10737418240, 10485760, 0, true, "CONSUMER_BYTE_RATE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidProvisioningConfig()
			cfg.MaxStorageBytes = tt.maxStorage
			cfg.ProducerByteRate = tt.producerRate
			cfg.ConsumerByteRate = tt.consumerRate
			err := cfg.Validate()
			if tt.shouldError {
				if err == nil {
					t.Error("Should error")
				} else if tt.errorSubstring != "" && !strings.Contains(err.Error(), tt.errorSubstring) {
					t.Errorf("Error should contain %q, got: %v", tt.errorSubstring, err)
				}
			} else {
				if err != nil {
					t.Errorf("Should not error: %v", err)
				}
			}
		})
	}
}

func TestProvisioningConfig_Validate_AdminAuthSettings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		threshold       int
		blockDuration   time.Duration
		cleanupInterval time.Duration
		cleanupMaxAge   time.Duration
		shouldError     bool
		errorSubstring  string
	}{
		{"valid defaults", 10, 60 * time.Second, 5 * time.Minute, 2 * time.Minute, false, ""},
		{"threshold exactly 1", 1, 60 * time.Second, 5 * time.Minute, 2 * time.Minute, false, ""},
		{"threshold zero", 0, 60 * time.Second, 5 * time.Minute, 2 * time.Minute, true, "ADMIN_AUTH_FAILURE_THRESHOLD"},
		{"threshold negative", -1, 60 * time.Second, 5 * time.Minute, 2 * time.Minute, true, "ADMIN_AUTH_FAILURE_THRESHOLD"},
		{"block duration exactly 1s", 10, time.Second, 5 * time.Minute, 2 * time.Minute, false, ""},
		{"block duration below 1s", 10, 500 * time.Millisecond, 5 * time.Minute, 2 * time.Minute, true, "ADMIN_AUTH_BLOCK_DURATION"},
		{"block duration zero", 10, 0, 5 * time.Minute, 2 * time.Minute, true, "ADMIN_AUTH_BLOCK_DURATION"},
		{"cleanup interval exactly 1s", 10, 60 * time.Second, time.Second, 2 * time.Minute, false, ""},
		{"cleanup interval below 1s", 10, 60 * time.Second, 500 * time.Millisecond, 2 * time.Minute, true, "ADMIN_AUTH_CLEANUP_INTERVAL"},
		{"cleanup max age exactly 1s", 10, 60 * time.Second, 5 * time.Minute, time.Second, false, ""},
		{"cleanup max age below 1s", 10, 60 * time.Second, 5 * time.Minute, 500 * time.Millisecond, true, "ADMIN_AUTH_CLEANUP_MAX_AGE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidProvisioningConfig()
			cfg.AdminAuthFailureThreshold = tt.threshold
			cfg.AdminAuthBlockDuration = tt.blockDuration
			cfg.AdminAuthCleanupInterval = tt.cleanupInterval
			cfg.AdminAuthCleanupMaxAge = tt.cleanupMaxAge
			err := cfg.Validate()
			if tt.shouldError {
				if err == nil {
					t.Error("Should error")
				} else if tt.errorSubstring != "" && !strings.Contains(err.Error(), tt.errorSubstring) {
					t.Errorf("Error should contain %q, got: %v", tt.errorSubstring, err)
				}
			} else {
				if err != nil {
					t.Errorf("Should not error: %v", err)
				}
			}
		})
	}
}

func TestProvisioningConfig_Validate_HTTPTimeouts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		readTimeout    time.Duration
		writeTimeout   time.Duration
		idleTimeout    time.Duration
		shouldError    bool
		errorSubstring string
	}{
		{"valid defaults", 15 * time.Second, 15 * time.Second, 60 * time.Second, false, ""},
		{"read exactly 1s", time.Second, 15 * time.Second, 60 * time.Second, false, ""},
		{"read exactly 120s", 120 * time.Second, 15 * time.Second, 60 * time.Second, false, ""},
		{"read below 1s", 500 * time.Millisecond, 15 * time.Second, 60 * time.Second, true, "HTTP_READ_TIMEOUT"},
		{"read above 120s", 121 * time.Second, 15 * time.Second, 60 * time.Second, true, "HTTP_READ_TIMEOUT"},
		{"read zero", 0, 15 * time.Second, 60 * time.Second, true, "HTTP_READ_TIMEOUT"},
		{"write exactly 1s", 15 * time.Second, time.Second, 60 * time.Second, false, ""},
		{"write exactly 120s", 15 * time.Second, 120 * time.Second, 60 * time.Second, false, ""},
		{"write below 1s", 15 * time.Second, 500 * time.Millisecond, 60 * time.Second, true, "HTTP_WRITE_TIMEOUT"},
		{"write above 120s", 15 * time.Second, 121 * time.Second, 60 * time.Second, true, "HTTP_WRITE_TIMEOUT"},
		{"idle exactly 1s", 15 * time.Second, 15 * time.Second, time.Second, false, ""},
		{"idle exactly 300s", 15 * time.Second, 15 * time.Second, 300 * time.Second, false, ""},
		{"idle below 1s", 15 * time.Second, 15 * time.Second, 500 * time.Millisecond, true, "HTTP_IDLE_TIMEOUT"},
		{"idle above 300s", 15 * time.Second, 15 * time.Second, 301 * time.Second, true, "HTTP_IDLE_TIMEOUT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidProvisioningConfig()
			cfg.HTTPReadTimeout = tt.readTimeout
			cfg.HTTPWriteTimeout = tt.writeTimeout
			cfg.HTTPIdleTimeout = tt.idleTimeout
			err := cfg.Validate()
			if tt.shouldError {
				if err == nil {
					t.Error("Should error")
				} else if tt.errorSubstring != "" && !strings.Contains(err.Error(), tt.errorSubstring) {
					t.Errorf("Error should contain %q, got: %v", tt.errorSubstring, err)
				}
			} else {
				if err != nil {
					t.Errorf("Should not error: %v", err)
				}
			}
		})
	}
}

func TestProvisioningConfig_Validate_KeyRegistry(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		refresh        time.Duration
		queryTimeout   time.Duration
		shouldError    bool
		errorSubstring string
	}{
		{"valid defaults", time.Minute, 5 * time.Second, false, ""},
		{"refresh exactly 1s", time.Second, 5 * time.Second, false, ""},
		{"refresh below 1s", 500 * time.Millisecond, 5 * time.Second, true, "KEY_REGISTRY_REFRESH_INTERVAL"},
		{"refresh zero", 0, 5 * time.Second, true, "KEY_REGISTRY_REFRESH_INTERVAL"},
		{"query timeout exactly 1s", time.Minute, time.Second, false, ""},
		{"query timeout below 1s", time.Minute, 500 * time.Millisecond, true, "KEY_REGISTRY_QUERY_TIMEOUT"},
		{"query timeout zero", time.Minute, 0, true, "KEY_REGISTRY_QUERY_TIMEOUT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidProvisioningConfig()
			cfg.KeyRegistryRefreshInterval = tt.refresh
			cfg.KeyRegistryQueryTimeout = tt.queryTimeout
			err := cfg.Validate()
			if tt.shouldError {
				if err == nil {
					t.Error("Should error")
				} else if tt.errorSubstring != "" && !strings.Contains(err.Error(), tt.errorSubstring) {
					t.Errorf("Error should contain %q, got: %v", tt.errorSubstring, err)
				}
			} else {
				if err != nil {
					t.Errorf("Should not error: %v", err)
				}
			}
		})
	}
}

func TestProvisioningConfig_Validate_ShutdownTimeout(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		timeout     time.Duration
		shouldError bool
	}{
		{"valid default", 30 * time.Second, false},
		{"exactly 1s", time.Second, false},
		{"large value", 5 * time.Minute, false},
		{"below 1s", 500 * time.Millisecond, true},
		{"zero", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidProvisioningConfig()
			cfg.ShutdownTimeout = tt.timeout
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

func TestProvisioningConfig_Validate_LifecycleCheckInterval(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		enabled     bool
		interval    time.Duration
		shouldError bool
	}{
		{"enabled valid default", true, time.Hour, false},
		{"enabled exactly 1m", true, time.Minute, false},
		{"enabled below 1m", true, 30 * time.Second, true},
		{"enabled zero", true, 0, true},
		{"disabled ignores low value", false, 0, false},
		{"disabled ignores below 1m", false, 30 * time.Second, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidProvisioningConfig()
			cfg.LifecycleManagerEnabled = tt.enabled
			cfg.LifecycleCheckInterval = tt.interval
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

func TestProvisioningConfig_Validate_MaxTenantsFetchLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		limit       int
		shouldError bool
	}{
		{"valid default", 100, false},
		{"valid large", 10000, false},
		{"valid min", 1, false},
		{"zero", 0, true},
		{"negative", -1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidProvisioningConfig()
			cfg.MaxTenantsFetchLimit = tt.limit
			err := cfg.Validate()
			if tt.shouldError {
				if err == nil {
					t.Error("Should error")
				} else if !strings.Contains(err.Error(), "PROVISIONING_MAX_TENANTS_FETCH_LIMIT") {
					t.Errorf("Error should mention PROVISIONING_MAX_TENANTS_FETCH_LIMIT: %v", err)
				}
			} else if err != nil {
				t.Errorf("Should not error: %v", err)
			}
		})
	}
}

func TestProvisioningConfig_Validate_DeletionTimeout(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		timeout     time.Duration
		shouldError bool
	}{
		{"valid default", 30 * time.Second, false},
		{"valid large", 5 * time.Minute, false},
		{"zero", 0, true},
		{"negative", -1 * time.Second, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidProvisioningConfig()
			cfg.DeletionTimeout = tt.timeout
			err := cfg.Validate()
			if tt.shouldError {
				if err == nil {
					t.Error("Should error")
				} else if !strings.Contains(err.Error(), "PROVISIONING_DELETION_TIMEOUT") {
					t.Errorf("Error should mention PROVISIONING_DELETION_TIMEOUT: %v", err)
				}
			} else if err != nil {
				t.Errorf("Should not error: %v", err)
			}
		})
	}
}

// --- Edition Gate Tests ---
// MUST NOT use t.Parallel() — tests share license.SetPublicKeyForTesting.

func setProvisioningEditionManager(t *testing.T, cfg *ProvisioningConfig, edition license.Edition) {
	t.Helper()
	priv, pub := license.GenerateTestKeyPair()
	license.SetPublicKeyForTesting(pub)
	claims := license.Claims{
		Edition: edition,
		Org:     "test",
		Exp:     time.Now().Add(time.Hour).Unix(),
	}
	key := license.SignTestLicense(claims, priv)
	mgr, err := license.NewManager(key, zerolog.Nop())
	if err != nil {
		t.Fatalf("create test license manager: %v", err)
	}
	cfg.editionManager = mgr
}

//nolint:paralleltest // shares license.SetPublicKeyForTesting via setEditionManager helper
func TestProvisioningConfig_Validate_EditionGates_Community(t *testing.T) {
	cfg := newValidProvisioningConfig()
	cfg.DatabaseDriver = "postgres"
	cfg.DatabaseURL = "postgres://localhost/test"
	setProvisioningEditionManager(t, cfg, license.Community)

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Community should reject postgres")
	}
	if !strings.Contains(err.Error(), "DATABASE_DRIVER=postgres") {
		t.Errorf("error %q should mention DATABASE_DRIVER=postgres", err.Error())
	}
}

//nolint:paralleltest // shares license.SetPublicKeyForTesting via setEditionManager helper
func TestProvisioningConfig_Validate_EditionGates_ProAccepts(t *testing.T) {
	cfg := newValidProvisioningConfig()
	cfg.DatabaseDriver = "postgres"
	cfg.DatabaseURL = "postgres://localhost/test"
	setProvisioningEditionManager(t, cfg, license.Pro)

	if err := cfg.Validate(); err != nil {
		t.Errorf("Pro should accept postgres: %v", err)
	}
}

//nolint:paralleltest // shares license.SetPublicKeyForTesting via setEditionManager helper
func TestProvisioningConfig_Validate_EditionGates_CommunityAcceptsSqlite(t *testing.T) {
	cfg := newValidProvisioningConfig()
	cfg.DatabaseDriver = "sqlite"
	setProvisioningEditionManager(t, cfg, license.Community)

	if err := cfg.Validate(); err != nil {
		t.Errorf("Community should accept sqlite: %v", err)
	}
}
