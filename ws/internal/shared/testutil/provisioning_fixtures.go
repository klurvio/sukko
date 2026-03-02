package testutil

import (
	"time"

	"github.com/Toniq-Labs/odin-ws/internal/provisioning"
	"github.com/Toniq-Labs/odin-ws/internal/shared/platform"
)

// NewTestProvisioningConfig creates a ProvisioningConfig with safe defaults for testing.
func NewTestProvisioningConfig() *platform.ProvisioningConfig {
	return &platform.ProvisioningConfig{
		Addr:                  "localhost:0",
		LogLevel:              "error",
		LogFormat:             "json",
		DatabaseURL:           "postgres://test:test@localhost:5432/test?sslmode=disable",
		DBMaxOpenConns:        5,
		DBMaxIdleConns:        2,
		DBConnMaxLifetime:      time.Hour,
		TopicNamespaceOverride: "test",
		DefaultPartitions:     3,
		DefaultRetentionMs:    604800000,
		MaxTopicsPerTenant:    50,
		DeprovisionGraceDays:  30,
		APIRateLimitPerMinute: 1000,
		HTTPReadTimeout:       30 * time.Second,
		HTTPWriteTimeout:      30 * time.Second,
		HTTPIdleTimeout:       120 * time.Second,
	}
}

// NewTestTenant creates a test tenant with the given ID.
func NewTestTenant(id string) *provisioning.Tenant {
	now := time.Now()
	return &provisioning.Tenant{
		ID:           id,
		Name:         "Test Tenant " + id,
		Status:       provisioning.StatusActive,
		ConsumerType: provisioning.ConsumerShared,
		Metadata:     provisioning.Metadata{"env": "test"},
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

// NewTestTenantKey creates a test key with the given IDs.
func NewTestTenantKey(keyID, tenantID string) *provisioning.TenantKey {
	now := time.Now()
	return &provisioning.TenantKey{
		KeyID:     keyID,
		TenantID:  tenantID,
		Algorithm: provisioning.AlgorithmES256,
		PublicKey: SampleES256PublicKeyPEM(),
		IsActive:  true,
		CreatedAt: now,
	}
}

// NewTestTenantTopic creates a test topic category.
// Note: Full topic name is built at runtime using kafka.BuildTopicName(namespace, tenantID, category).
func NewTestTenantTopic(tenantID, category string) *provisioning.TenantTopic {
	now := time.Now()
	return &provisioning.TenantTopic{
		TenantID:    tenantID,
		Category:    category,
		Partitions:  3,
		RetentionMs: 604800000,
		CreatedAt:   now,
	}
}

// NewTestTenantQuota creates a test quota.
func NewTestTenantQuota(tenantID string) *provisioning.TenantQuota {
	now := time.Now()
	return &provisioning.TenantQuota{
		TenantID:         tenantID,
		MaxTopics:        50,
		MaxPartitions:    200,
		MaxStorageBytes:  10 * 1024 * 1024 * 1024,
		ProducerByteRate: 10 * 1024 * 1024,
		ConsumerByteRate: 50 * 1024 * 1024,
		UpdatedAt:        now,
	}
}

// SampleES256PublicKeyPEM returns a sample ES256 public key for testing.
// This is a valid PEM-encoded P-256 public key for test purposes only.
func SampleES256PublicKeyPEM() string {
	return `-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEn6jKqjRy/2aBT3c5H8QT2CnLMz7O
nUwZ9KeOJoL8G5FmH6u0L9Pt5TXpR1LW9YXhNO3WL9YqKYL7qfqB5i0b6Q==
-----END PUBLIC KEY-----`
}

// SampleTenantIDs returns valid tenant IDs for testing.
func SampleTenantIDs() []string {
	return []string{
		"acme-corp",
		"beta-test",
		"gamma-labs",
	}
}

// SampleCategories returns valid topic categories for testing.
func SampleCategories() []string {
	return []string{
		"trade",
		"orderbook",
		"ticker",
	}
}
