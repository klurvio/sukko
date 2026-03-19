// Package testutil provides test fixtures and helpers for the provisioning service.
package testutil

import (
	"time"

	"github.com/klurvio/sukko/internal/provisioning"
)

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

// SampleES256PublicKeyPEM returns a sample ES256 public key for testing.
// This is a valid PEM-encoded P-256 public key for test purposes only.
func SampleES256PublicKeyPEM() string {
	return `-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEn6jKqjRy/2aBT3c5H8QT2CnLMz7O
nUwZ9KeOJoL8G5FmH6u0L9Pt5TXpR1LW9YXhNO3WL9YqKYL7qfqB5i0b6Q==
-----END PUBLIC KEY-----`
}
