package provisioning_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/Toniq-Labs/odin-ws/internal/provisioning"
	"github.com/Toniq-Labs/odin-ws/internal/testutil"
)

func newTestService() (*provisioning.Service, *testutil.MockTenantStore, *testutil.MockKeyStore, *testutil.MockKafkaAdmin) {
	tenantStore := testutil.NewMockTenantStore()
	keyStore := testutil.NewMockKeyStore()
	topicStore := testutil.NewMockTopicStore()
	quotaStore := testutil.NewMockQuotaStore()
	auditStore := testutil.NewMockAuditStore()
	kafkaAdmin := testutil.NewMockKafkaAdmin()

	svc := provisioning.NewService(provisioning.ServiceConfig{
		TenantStore:          tenantStore,
		KeyStore:             keyStore,
		TopicStore:           topicStore,
		QuotaStore:           quotaStore,
		AuditStore:           auditStore,
		KafkaAdmin:           kafkaAdmin,
		TopicNamespace:       "test",
		DefaultPartitions:    3,
		DefaultRetentionMs:   604800000,
		MaxTopicsPerTenant:   50,
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
			name: "valid tenant with categories",
			req: provisioning.CreateTenantRequest{
				TenantID:   "gamma-labs",
				Name:       "Gamma Labs",
				Categories: []string{"trade", "orderbook"},
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

func TestService_CreateTopics(t *testing.T) {
	t.Parallel()
	svc, tenantStore, _, kafkaAdmin := newTestService()

	// Setup: create a tenant
	tenant := testutil.NewTestTenant("acme-corp")
	_ = tenantStore.Create(context.Background(), tenant)

	// Create topics
	topics, err := svc.CreateTopics(context.Background(), "acme-corp", []string{"trade", "orderbook"})
	if err != nil {
		t.Fatalf("failed to create topics: %v", err)
	}

	// Verify topics created (base + refined)
	if len(topics) != 4 {
		t.Errorf("expected 4 topics (2 base + 2 refined), got %d", len(topics))
	}

	// Verify Kafka topics
	kafkaTopics := kafkaAdmin.GetTopics()
	if len(kafkaTopics) != 4 {
		t.Errorf("expected 4 Kafka topics, got %d", len(kafkaTopics))
	}

	// Verify ACLs created
	acls := kafkaAdmin.GetACLs()
	if len(acls) == 0 {
		t.Error("expected ACLs to be created")
	}
}

func TestService_CreateTopics_SuspendedTenant(t *testing.T) {
	t.Parallel()
	svc, tenantStore, _, _ := newTestService()

	// Setup: create and suspend a tenant
	tenant := testutil.NewTestTenant("acme-corp")
	tenant.Status = provisioning.StatusSuspended
	_ = tenantStore.Create(context.Background(), tenant)

	// Try to create topics
	_, err := svc.CreateTopics(context.Background(), "acme-corp", []string{"trade"})
	if err == nil {
		t.Error("expected error for suspended tenant")
	}
	if !strings.Contains(err.Error(), "not active") {
		t.Errorf("expected 'not active' error, got: %v", err)
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
