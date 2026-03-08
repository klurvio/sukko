package provisioning_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/provisioning"
	"github.com/klurvio/sukko/internal/shared/testutil"
	"github.com/klurvio/sukko/internal/shared/types"
)

func newTestService() (*provisioning.Service, *testutil.MockTenantStore, *testutil.MockKeyStore, *testutil.MockKafkaAdmin) {
	tenantStore := testutil.NewMockTenantStore()
	keyStore := testutil.NewMockKeyStore()
	routingRulesStore := testutil.NewMockRoutingRulesStore()
	quotaStore := testutil.NewMockQuotaStore()
	auditStore := testutil.NewMockAuditStore()
	kafkaAdmin := testutil.NewMockKafkaAdmin()

	svc := provisioning.NewService(provisioning.ServiceConfig{
		TenantStore:          tenantStore,
		KeyStore:             keyStore,
		RoutingRulesStore:    routingRulesStore,
		QuotaStore:           quotaStore,
		AuditStore:           auditStore,
		KafkaAdmin:           kafkaAdmin,
		TopicNamespace:       "test",
		DefaultPartitions:    3,
		DefaultRetentionMs:   604800000,
		MaxTopicsPerTenant:   50,
		MaxRoutingRules:      5,
		DeprovisionGraceDays: 30,
		Logger:               zerolog.Nop(),
	})

	return svc, tenantStore, keyStore, kafkaAdmin
}

func TestService_CreateTenant(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		req         provisioning.CreateTenantRequest
		setupMock   func(*testutil.MockTenantStore, *testutil.MockKeyStore)
		wantErr     bool
		errContains string
	}{
		{
			name: "valid tenant without key",
			req: provisioning.CreateTenantRequest{
				TenantID: "acme-corp",
				Name:     "Acme Corporation",
			},
			wantErr: false,
		},
		{
			name: "valid tenant with consumer type",
			req: provisioning.CreateTenantRequest{
				TenantID:     "beta-test",
				Name:         "Beta Test Inc",
				ConsumerType: provisioning.ConsumerDedicated,
			},
			wantErr: false,
		},
		{
			name: "valid tenant with metadata",
			req: provisioning.CreateTenantRequest{
				TenantID: "gamma-labs",
				Name:     "Gamma Labs",
				Metadata: provisioning.Metadata{"env": "prod"},
			},
			wantErr: false,
		},
		{
			name: "invalid tenant ID - uppercase",
			req: provisioning.CreateTenantRequest{
				TenantID: "INVALID",
				Name:     "Invalid Tenant",
			},
			wantErr:     true,
			errContains: "invalid tenant",
		},
		{
			name: "invalid tenant ID - too short",
			req: provisioning.CreateTenantRequest{
				TenantID: "ab",
				Name:     "Too Short",
			},
			wantErr:     true,
			errContains: "invalid tenant",
		},
		{
			name: "duplicate tenant",
			req: provisioning.CreateTenantRequest{
				TenantID: "existing",
				Name:     "Existing Tenant",
			},
			setupMock: func(ts *testutil.MockTenantStore, _ *testutil.MockKeyStore) {
				_ = ts.Create(context.Background(), testutil.NewTestTenant("existing"))
			},
			wantErr:     true,
			errContains: "already exists",
		},
		{
			name: "store error",
			req: provisioning.CreateTenantRequest{
				TenantID: "store-fail",
				Name:     "Store Fail",
			},
			setupMock: func(ts *testutil.MockTenantStore, _ *testutil.MockKeyStore) {
				ts.CreateErr = errors.New("database connection failed")
			},
			wantErr:     true,
			errContains: "database connection",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc, tenantStore, keyStore, _ := newTestService()

			if tt.setupMock != nil {
				tt.setupMock(tenantStore, keyStore)
			}

			resp, err := svc.CreateTenant(context.Background(), tt.req)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if resp == nil {
					t.Error("expected response, got nil")
				}
				if resp != nil && resp.Tenant.ID != tt.req.TenantID {
					t.Errorf("tenant ID mismatch: got %q, want %q", resp.Tenant.ID, tt.req.TenantID)
				}
			}
		})
	}
}

func TestService_GetTenant(t *testing.T) {
	t.Parallel()
	svc, tenantStore, _, _ := newTestService()

	// Setup: create a tenant
	tenant := testutil.NewTestTenant("acme-corp")
	_ = tenantStore.Create(context.Background(), tenant)

	tests := []struct {
		name     string
		tenantID string
		wantErr  bool
	}{
		{
			name:     "existing tenant",
			tenantID: "acme-corp",
			wantErr:  false,
		},
		{
			name:     "non-existing tenant",
			tenantID: "not-found",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := svc.GetTenant(context.Background(), tt.tenantID)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if got == nil {
					t.Error("expected tenant, got nil")
				}
				if got != nil && got.ID != tt.tenantID {
					t.Errorf("tenant ID mismatch: got %q, want %q", got.ID, tt.tenantID)
				}
			}
		})
	}
}

func TestService_SuspendAndReactivateTenant(t *testing.T) {
	t.Parallel()
	svc, tenantStore, _, _ := newTestService()

	// Setup: create a tenant
	tenant := testutil.NewTestTenant("acme-corp")
	_ = tenantStore.Create(context.Background(), tenant)

	// Test suspend
	err := svc.SuspendTenant(context.Background(), "acme-corp")
	if err != nil {
		t.Fatalf("failed to suspend tenant: %v", err)
	}

	// Verify suspended
	got, _ := svc.GetTenant(context.Background(), "acme-corp")
	if got.Status != provisioning.StatusSuspended {
		t.Errorf("expected status %q, got %q", provisioning.StatusSuspended, got.Status)
	}

	// Test reactivate
	err = svc.ReactivateTenant(context.Background(), "acme-corp")
	if err != nil {
		t.Fatalf("failed to reactivate tenant: %v", err)
	}

	// Verify active
	got, _ = svc.GetTenant(context.Background(), "acme-corp")
	if got.Status != provisioning.StatusActive {
		t.Errorf("expected status %q, got %q", provisioning.StatusActive, got.Status)
	}
}

func TestService_DeprovisionTenant(t *testing.T) {
	t.Parallel()
	svc, tenantStore, keyStore, _ := newTestService()

	// Setup: create a tenant with key
	tenant := testutil.NewTestTenant("acme-corp")
	_ = tenantStore.Create(context.Background(), tenant)
	key := testutil.NewTestTenantKey("key-1", "acme-corp")
	_ = keyStore.Create(context.Background(), key)

	// Deprovision
	err := svc.DeprovisionTenant(context.Background(), "acme-corp")
	if err != nil {
		t.Fatalf("failed to deprovision tenant: %v", err)
	}

	// Verify status
	got, _ := svc.GetTenant(context.Background(), "acme-corp")
	if got.Status != provisioning.StatusDeprovisioning {
		t.Errorf("expected status %q, got %q", provisioning.StatusDeprovisioning, got.Status)
	}

	// Verify keys revoked
	keys, _ := svc.ListKeys(context.Background(), "acme-corp")
	for _, k := range keys {
		if k.IsActive {
			t.Errorf("key %q should be revoked", k.KeyID)
		}
	}
}

func TestService_DeprovisionTenant_WithRoutingRules(t *testing.T) {
	t.Parallel()
	svc, tenantStore, _, _ := newTestService()

	// Setup: create tenant and set routing rules
	tenant := testutil.NewTestTenant("acme-corp")
	_ = tenantStore.Create(context.Background(), tenant)
	_ = svc.SetRoutingRules(context.Background(), "acme-corp", []types.TopicRoutingRule{
		{Pattern: "*.trade", TopicSuffix: "trade"},
		{Pattern: "*.orderbook", TopicSuffix: "orderbook"},
	})

	// Deprovision (exercises the retention-update code path for routing rules)
	err := svc.DeprovisionTenant(context.Background(), "acme-corp")
	if err != nil {
		t.Fatalf("failed to deprovision tenant with routing rules: %v", err)
	}

	// Verify status
	got, _ := svc.GetTenant(context.Background(), "acme-corp")
	if got.Status != provisioning.StatusDeprovisioning {
		t.Errorf("expected status %q, got %q", provisioning.StatusDeprovisioning, got.Status)
	}

	// Routing rules should still exist (they're cleaned up later by lifecycle manager)
	rules, err := svc.GetRoutingRules(context.Background(), "acme-corp")
	if err != nil {
		t.Fatalf("expected routing rules to still exist after deprovision: %v", err)
	}
	if len(rules) != 2 {
		t.Errorf("expected 2 routing rules, got %d", len(rules))
	}
}

func TestService_CreateKey(t *testing.T) {
	t.Parallel()
	svc, tenantStore, _, _ := newTestService()

	// Setup: create a tenant
	tenant := testutil.NewTestTenant("acme-corp")
	_ = tenantStore.Create(context.Background(), tenant)

	tests := []struct {
		name        string
		tenantID    string
		req         provisioning.CreateKeyRequest
		wantErr     bool
		errContains string
	}{
		{
			name:     "valid ES256 key",
			tenantID: "acme-corp",
			req: provisioning.CreateKeyRequest{
				KeyID:     "key-1",
				Algorithm: provisioning.AlgorithmES256,
				PublicKey: testutil.SampleES256PublicKeyPEM(),
			},
			wantErr: false,
		},
		{
			name:     "invalid key ID",
			tenantID: "acme-corp",
			req: provisioning.CreateKeyRequest{
				KeyID:     "INVALID_KEY",
				Algorithm: provisioning.AlgorithmES256,
				PublicKey: testutil.SampleES256PublicKeyPEM(),
			},
			wantErr:     true,
			errContains: "invalid key",
		},
		{
			name:     "non-existing tenant",
			tenantID: "not-found",
			req: provisioning.CreateKeyRequest{
				KeyID:     "key-2",
				Algorithm: provisioning.AlgorithmES256,
				PublicKey: testutil.SampleES256PublicKeyPEM(),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			key, err := svc.CreateKey(context.Background(), tt.tenantID, tt.req)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if key == nil {
					t.Error("expected key, got nil")
				}
				if key != nil && key.KeyID != tt.req.KeyID {
					t.Errorf("key ID mismatch: got %q, want %q", key.KeyID, tt.req.KeyID)
				}
			}
		})
	}
}

func TestService_RevokeKey(t *testing.T) {
	t.Parallel()
	svc, tenantStore, keyStore, _ := newTestService()

	// Setup: create tenant and key
	tenant := testutil.NewTestTenant("acme-corp")
	_ = tenantStore.Create(context.Background(), tenant)
	key := testutil.NewTestTenantKey("key-1", "acme-corp")
	_ = keyStore.Create(context.Background(), key)

	// Revoke
	err := svc.RevokeKey(context.Background(), "acme-corp", "key-1")
	if err != nil {
		t.Fatalf("failed to revoke key: %v", err)
	}

	// Verify revoked
	activeKeys, _ := keyStore.GetActiveKeys(context.Background())
	for _, k := range activeKeys {
		if k.KeyID == "key-1" {
			t.Error("key should not be in active keys")
		}
	}
}

func TestService_RevokeKey_WrongTenant(t *testing.T) {
	t.Parallel()
	svc, tenantStore, keyStore, _ := newTestService()

	// Setup: create two tenants with keys
	tenant1 := testutil.NewTestTenant("acme-corp")
	tenant2 := testutil.NewTestTenant("beta-test")
	_ = tenantStore.Create(context.Background(), tenant1)
	_ = tenantStore.Create(context.Background(), tenant2)
	key := testutil.NewTestTenantKey("key-1", "acme-corp")
	_ = keyStore.Create(context.Background(), key)

	// Try to revoke key with wrong tenant
	err := svc.RevokeKey(context.Background(), "beta-test", "key-1")
	if err == nil {
		t.Error("expected error when revoking key for wrong tenant")
	}
	if !strings.Contains(err.Error(), "does not belong") {
		t.Errorf("expected 'does not belong' error, got: %v", err)
	}
}

func TestService_SetRoutingRules(t *testing.T) {
	t.Parallel()
	svc, tenantStore, _, _ := newTestService()

	// Setup: create a tenant
	tenant := testutil.NewTestTenant("acme-corp")
	_ = tenantStore.Create(context.Background(), tenant)

	// Set routing rules
	rules := []types.TopicRoutingRule{
		{Pattern: "*.trade", TopicSuffix: "trade"},
		{Pattern: "*.orderbook", TopicSuffix: "orderbook"},
	}
	err := svc.SetRoutingRules(context.Background(), "acme-corp", rules)
	if err != nil {
		t.Fatalf("failed to set routing rules: %v", err)
	}

	// Verify rules stored
	got, err := svc.GetRoutingRules(context.Background(), "acme-corp")
	if err != nil {
		t.Fatalf("failed to get routing rules: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 rules, got %d", len(got))
	}
}

func TestService_SetRoutingRules_SuspendedTenant(t *testing.T) {
	t.Parallel()
	svc, tenantStore, _, _ := newTestService()

	// Setup: create and suspend a tenant
	tenant := testutil.NewTestTenant("acme-corp")
	tenant.Status = provisioning.StatusSuspended
	_ = tenantStore.Create(context.Background(), tenant)

	// Try to set routing rules
	rules := []types.TopicRoutingRule{
		{Pattern: "*.trade", TopicSuffix: "trade"},
	}
	err := svc.SetRoutingRules(context.Background(), "acme-corp", rules)
	if err == nil {
		t.Error("expected error for suspended tenant")
	}
	if !strings.Contains(err.Error(), "not active") {
		t.Errorf("expected 'not active' error, got: %v", err)
	}
}

func TestService_DeleteRoutingRules(t *testing.T) {
	t.Parallel()
	svc, tenantStore, _, _ := newTestService()

	// Setup: create tenant and set rules
	tenant := testutil.NewTestTenant("acme-corp")
	_ = tenantStore.Create(context.Background(), tenant)
	_ = svc.SetRoutingRules(context.Background(), "acme-corp", []types.TopicRoutingRule{
		{Pattern: "*.trade", TopicSuffix: "trade"},
	})

	// Delete rules
	err := svc.DeleteRoutingRules(context.Background(), "acme-corp")
	if err != nil {
		t.Fatalf("failed to delete routing rules: %v", err)
	}

	// Verify rules gone
	_, err = svc.GetRoutingRules(context.Background(), "acme-corp")
	if err == nil {
		t.Error("expected error after deleting routing rules")
	}
}

func TestService_Ready(t *testing.T) {
	t.Parallel()
	svc, tenantStore, _, _ := newTestService()

	// Test healthy
	err := svc.Ready(context.Background())
	if err != nil {
		t.Errorf("expected Ready to succeed, got: %v", err)
	}

	// Test unhealthy
	tenantStore.PingErr = errors.New("connection refused")
	err = svc.Ready(context.Background())
	if err == nil {
		t.Error("expected Ready to fail with PingErr")
	}
}

func TestService_GetActiveKeys(t *testing.T) {
	t.Parallel()
	svc, tenantStore, keyStore, _ := newTestService()

	// Setup: create tenants and keys
	tenant := testutil.NewTestTenant("acme-corp")
	_ = tenantStore.Create(context.Background(), tenant)

	key1 := testutil.NewTestTenantKey("key-1", "acme-corp")
	key2 := testutil.NewTestTenantKey("key-2", "acme-corp")
	key3 := testutil.NewTestTenantKey("key-3", "acme-corp")
	_ = keyStore.Create(context.Background(), key1)
	_ = keyStore.Create(context.Background(), key2)
	_ = keyStore.Create(context.Background(), key3)

	// Revoke one key
	_ = keyStore.Revoke(context.Background(), "key-2")

	// Get active keys
	activeKeys, err := svc.GetActiveKeys(context.Background())
	if err != nil {
		t.Fatalf("failed to get active keys: %v", err)
	}

	if len(activeKeys) != 2 {
		t.Errorf("expected 2 active keys, got %d", len(activeKeys))
	}
}

func TestService_UpdateQuota(t *testing.T) {
	t.Parallel()
	svc, tenantStore, _, kafkaAdmin := newTestService()

	// Setup: create tenant and quota
	tenant := testutil.NewTestTenant("acme-corp")
	_ = tenantStore.Create(context.Background(), tenant)
	_, _ = svc.CreateTenant(context.Background(), provisioning.CreateTenantRequest{
		TenantID: "quota-test",
		Name:     "Quota Test",
	})

	// Update quota
	newMaxTopics := 100
	newProducerRate := int64(20 * 1024 * 1024)
	req := provisioning.UpdateQuotaRequest{
		MaxTopics:        &newMaxTopics,
		ProducerByteRate: &newProducerRate,
	}

	quota, err := svc.UpdateQuota(context.Background(), "quota-test", req)
	if err != nil {
		t.Fatalf("failed to update quota: %v", err)
	}

	if quota.MaxTopics != newMaxTopics {
		t.Errorf("MaxTopics mismatch: got %d, want %d", quota.MaxTopics, newMaxTopics)
	}
	if quota.ProducerByteRate != newProducerRate {
		t.Errorf("ProducerByteRate mismatch: got %d, want %d", quota.ProducerByteRate, newProducerRate)
	}

	// Verify Kafka quota was set (no-op check for mock)
	_ = kafkaAdmin.GetACLs()
}

func TestService_ListTenants(t *testing.T) {
	t.Parallel()
	svc, tenantStore, _, _ := newTestService()

	// Setup: create multiple tenants
	for i, id := range []string{"acme-corp", "beta-test", "gamma-labs"} {
		tenant := testutil.NewTestTenant(id)
		if i == 2 {
			tenant.Status = provisioning.StatusSuspended
		}
		_ = tenantStore.Create(context.Background(), tenant)
	}

	tests := []struct {
		name      string
		opts      provisioning.ListOptions
		wantCount int
		wantTotal int
	}{
		{
			name:      "all tenants",
			opts:      provisioning.ListOptions{Limit: 10, Offset: 0},
			wantCount: 3,
			wantTotal: 3,
		},
		{
			name:      "with limit",
			opts:      provisioning.ListOptions{Limit: 2, Offset: 0},
			wantCount: 2,
			wantTotal: 3,
		},
		{
			name:      "with offset",
			opts:      provisioning.ListOptions{Limit: 10, Offset: 2},
			wantCount: 1,
			wantTotal: 3,
		},
		{
			name: "filter by status",
			opts: func() provisioning.ListOptions {
				status := provisioning.StatusSuspended
				return provisioning.ListOptions{Limit: 10, Offset: 0, Status: &status}
			}(),
			wantCount: 1,
			wantTotal: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tenants, total, err := svc.ListTenants(context.Background(), tt.opts)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(tenants) != tt.wantCount {
				t.Errorf("tenant count mismatch: got %d, want %d", len(tenants), tt.wantCount)
			}
			if total != tt.wantTotal {
				t.Errorf("total mismatch: got %d, want %d", total, tt.wantTotal)
			}
		})
	}
}

func TestService_GetAuditLog(t *testing.T) {
	t.Parallel()
	svc, tenantStore, _, _ := newTestService()

	// Create a tenant (generates audit entries)
	_, _ = svc.CreateTenant(context.Background(), provisioning.CreateTenantRequest{
		TenantID: "acme-corp",
		Name:     "Acme Corporation",
	})

	// Suspend the tenant (generates another audit entry)
	tenant := testutil.NewTestTenant("acme-corp")
	_ = tenantStore.Create(context.Background(), tenant)
	_ = svc.SuspendTenant(context.Background(), "acme-corp")

	// Get audit log
	entries, total, err := svc.GetAuditLog(context.Background(), "acme-corp", provisioning.ListOptions{
		Limit:  10,
		Offset: 0,
	})
	if err != nil {
		t.Fatalf("failed to get audit log: %v", err)
	}

	if total < 1 {
		t.Error("expected at least one audit entry")
	}
	if len(entries) < 1 {
		t.Error("expected at least one audit entry in results")
	}
}

func TestService_UpdateTenant(t *testing.T) {
	t.Parallel()
	svc, tenantStore, _, _ := newTestService()

	// Setup: create a tenant
	tenant := testutil.NewTestTenant("acme-corp")
	_ = tenantStore.Create(context.Background(), tenant)

	// Update
	newName := "Acme Corp Updated"
	newConsumerType := provisioning.ConsumerDedicated
	updated, err := svc.UpdateTenant(context.Background(), "acme-corp", provisioning.UpdateTenantRequest{
		Name:         &newName,
		ConsumerType: &newConsumerType,
	})
	if err != nil {
		t.Fatalf("failed to update tenant: %v", err)
	}

	if updated.Name != newName {
		t.Errorf("name mismatch: got %q, want %q", updated.Name, newName)
	}
	if updated.ConsumerType != newConsumerType {
		t.Errorf("consumer type mismatch: got %q, want %q", updated.ConsumerType, newConsumerType)
	}
}

func TestService_CreateTenantWithKey(t *testing.T) {
	t.Parallel()
	svc, _, _, _ := newTestService()

	// Create tenant with initial key
	expiresAt := time.Now().Add(365 * 24 * time.Hour)
	req := provisioning.CreateTenantRequest{
		TenantID: "acme-corp",
		Name:     "Acme Corporation",
		PublicKey: &provisioning.CreateKeyRequest{
			KeyID:     "initial-key",
			Algorithm: provisioning.AlgorithmES256,
			PublicKey: testutil.SampleES256PublicKeyPEM(),
			ExpiresAt: &expiresAt,
		},
	}

	resp, err := svc.CreateTenant(context.Background(), req)
	if err != nil {
		t.Fatalf("failed to create tenant: %v", err)
	}

	if resp.Key == nil {
		t.Error("expected key in response")
	}
	if resp.Key != nil && resp.Key.KeyID != "initial-key" {
		t.Errorf("key ID mismatch: got %q, want %q", resp.Key.KeyID, "initial-key")
	}
}

// =============================================================================
// Routing Rules Edge Case Tests (W5)
// =============================================================================

func TestService_SetRoutingRules_ExceedsMaxLimit(t *testing.T) {
	t.Parallel()
	svc, tenantStore, _, _ := newTestService()

	tenant := testutil.NewTestTenant("acme-corp")
	_ = tenantStore.Create(context.Background(), tenant)

	// MaxRoutingRules is set to 5 in newTestService — create 6 rules to exceed it
	rules := make([]types.TopicRoutingRule, 6)
	for i := range rules {
		rules[i] = types.TopicRoutingRule{
			Pattern:     fmt.Sprintf("*.suffix%d", i),
			TopicSuffix: fmt.Sprintf("topic%d", i),
		}
	}

	err := svc.SetRoutingRules(context.Background(), "acme-corp", rules)
	if err == nil {
		t.Fatal("expected error when exceeding MaxRoutingRules, got nil")
	}
	if !errors.Is(err, types.ErrTooManyRoutingRules) {
		t.Errorf("expected ErrTooManyRoutingRules, got: %v", err)
	}
}

func TestService_SetRoutingRules_InvalidRulesPropagated(t *testing.T) {
	t.Parallel()
	svc, tenantStore, _, _ := newTestService()

	tenant := testutil.NewTestTenant("acme-corp")
	_ = tenantStore.Create(context.Background(), tenant)

	// Empty pattern should fail validation
	rules := []types.TopicRoutingRule{
		{Pattern: "", TopicSuffix: "trade"},
	}

	err := svc.SetRoutingRules(context.Background(), "acme-corp", rules)
	if err == nil {
		t.Fatal("expected error for invalid rules, got nil")
	}
	if !strings.Contains(err.Error(), "invalid routing rules") {
		t.Errorf("expected 'invalid routing rules' in error, got: %v", err)
	}
}

func TestService_SetRoutingRules_NilStore(t *testing.T) {
	t.Parallel()

	// Service without routing rules store
	svc := provisioning.NewService(provisioning.ServiceConfig{
		TenantStore:          testutil.NewMockTenantStore(),
		KeyStore:             testutil.NewMockKeyStore(),
		RoutingRulesStore:    nil, // not configured
		QuotaStore:           testutil.NewMockQuotaStore(),
		AuditStore:           testutil.NewMockAuditStore(),
		KafkaAdmin:           testutil.NewMockKafkaAdmin(),
		TopicNamespace:       "test",
		DefaultPartitions:    3,
		DefaultRetentionMs:   604800000,
		MaxTopicsPerTenant:   50,
		MaxRoutingRules:      100,
		DeprovisionGraceDays: 30,
		Logger:               zerolog.Nop(),
	})

	rules := []types.TopicRoutingRule{
		{Pattern: "*.trade", TopicSuffix: "trade"},
	}

	err := svc.SetRoutingRules(context.Background(), "acme-corp", rules)
	if err == nil {
		t.Fatal("expected error for nil routing rules store, got nil")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Errorf("expected 'not configured' error, got: %v", err)
	}

	// Get should also fail gracefully
	_, err = svc.GetRoutingRules(context.Background(), "acme-corp")
	if err == nil {
		t.Fatal("expected error for nil routing rules store on Get, got nil")
	}

	// Delete should also fail gracefully
	err = svc.DeleteRoutingRules(context.Background(), "acme-corp")
	if err == nil {
		t.Fatal("expected error for nil routing rules store on Delete, got nil")
	}
}

func TestService_DeleteRoutingRules_Nonexistent(t *testing.T) {
	t.Parallel()
	svc, tenantStore, _, _ := newTestService()

	tenant := testutil.NewTestTenant("acme-corp")
	_ = tenantStore.Create(context.Background(), tenant)

	// Delete routing rules that don't exist
	err := svc.DeleteRoutingRules(context.Background(), "acme-corp")
	if err == nil {
		t.Fatal("expected error when deleting nonexistent routing rules, got nil")
	}
	if !errors.Is(err, types.ErrRoutingRulesNotFound) {
		t.Errorf("expected ErrRoutingRulesNotFound, got: %v", err)
	}
}

func TestService_SetRoutingRules_NonexistentTenant(t *testing.T) {
	t.Parallel()
	svc, _, _, _ := newTestService()

	rules := []types.TopicRoutingRule{
		{Pattern: "*.trade", TopicSuffix: "trade"},
	}

	err := svc.SetRoutingRules(context.Background(), "nonexistent", rules)
	if err == nil {
		t.Fatal("expected error for nonexistent tenant, got nil")
	}
}

func TestService_CreateTenant_DotsInID(t *testing.T) {
	t.Parallel()
	svc, _, _, _ := newTestService()

	// "acme.corp" is rejected by tenant.Validate() before the dot check
	// because dots are not valid in tenant IDs (lowercase alphanumeric + hyphens only).
	_, err := svc.CreateTenant(context.Background(), provisioning.CreateTenantRequest{
		TenantID: "acme.corp",
		Name:     "Acme Corp",
	})
	if err == nil {
		t.Fatal("expected error for dots in tenant ID, got nil")
	}
	if !strings.Contains(err.Error(), "invalid tenant") {
		t.Errorf("expected 'invalid tenant' error, got: %v", err)
	}
}

func TestService_CreateTenant_UnderscorePrefix(t *testing.T) {
	t.Parallel()
	svc, _, _, _ := newTestService()

	// "_system" is rejected by tenant.Validate() before the underscore-prefix check
	// because underscores/leading underscores are not valid in tenant IDs.
	_, err := svc.CreateTenant(context.Background(), provisioning.CreateTenantRequest{
		TenantID: "_system",
		Name:     "System Tenant",
	})
	if err == nil {
		t.Fatal("expected error for underscore prefix, got nil")
	}
	if !strings.Contains(err.Error(), "invalid tenant") {
		t.Errorf("expected 'invalid tenant' error, got: %v", err)
	}
}
