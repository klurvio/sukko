package grpcserver_test

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	provisioningv1 "github.com/klurvio/sukko/gen/proto/sukko/provisioning/v1"
	"github.com/klurvio/sukko/internal/provisioning"
	"github.com/klurvio/sukko/internal/provisioning/eventbus"
	"github.com/klurvio/sukko/internal/provisioning/grpcserver"
	"github.com/klurvio/sukko/internal/shared/testutil"
	"github.com/klurvio/sukko/internal/shared/types"
)

const bufSize = 1024 * 1024

// testPublicKeyPEM is a valid EC P-256 public key for tests.
const testPublicKeyPEM = `-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEEVs/o5+uQbTjL3chynL4wXgUg2R9
q9UU8I5mEovUf86QZ7kOBIjJwqnzD1omageEHWwHdBO6B+dFabmdT9POxg==
-----END PUBLIC KEY-----`

// mockOIDCConfigStore is a minimal in-memory OIDCConfigStore for gRPC server tests.
type mockOIDCConfigStore struct {
	mu      sync.RWMutex
	configs map[string]*types.TenantOIDCConfig
}

func newMockOIDCConfigStore() *mockOIDCConfigStore {
	return &mockOIDCConfigStore{configs: make(map[string]*types.TenantOIDCConfig)}
}

func (m *mockOIDCConfigStore) Create(_ context.Context, config *types.TenantOIDCConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configs[config.TenantID] = config
	return nil
}

func (m *mockOIDCConfigStore) Get(_ context.Context, tenantID string) (*types.TenantOIDCConfig, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if cfg, ok := m.configs[tenantID]; ok {
		return cfg, nil
	}
	return nil, types.ErrOIDCNotConfigured
}

func (m *mockOIDCConfigStore) GetByIssuer(_ context.Context, issuerURL string) (*types.TenantOIDCConfig, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, cfg := range m.configs {
		if cfg.IssuerURL == issuerURL {
			return cfg, nil
		}
	}
	return nil, types.ErrIssuerNotFound
}

func (m *mockOIDCConfigStore) Update(_ context.Context, config *types.TenantOIDCConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configs[config.TenantID] = config
	return nil
}

func (m *mockOIDCConfigStore) Delete(_ context.Context, tenantID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.configs, tenantID)
	return nil
}

func (m *mockOIDCConfigStore) ListEnabled(_ context.Context) ([]*types.TenantOIDCConfig, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*types.TenantOIDCConfig
	for _, cfg := range m.configs {
		if cfg.Enabled {
			result = append(result, cfg)
		}
	}
	return result, nil
}

// mockChannelRulesStore is a minimal in-memory ChannelRulesStore for gRPC server tests.
type mockChannelRulesStore struct {
	mu    sync.RWMutex
	rules map[string]*types.TenantChannelRules
}

func newMockChannelRulesStore() *mockChannelRulesStore {
	return &mockChannelRulesStore{rules: make(map[string]*types.TenantChannelRules)}
}

func (m *mockChannelRulesStore) Create(_ context.Context, tenantID string, rules *types.ChannelRules) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rules[tenantID] = &types.TenantChannelRules{
		TenantID: tenantID,
		Rules:    *rules,
	}
	return nil
}

func (m *mockChannelRulesStore) Get(_ context.Context, tenantID string) (*types.TenantChannelRules, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if r, ok := m.rules[tenantID]; ok {
		return r, nil
	}
	return nil, types.ErrChannelRulesNotFound
}

func (m *mockChannelRulesStore) GetRules(_ context.Context, tenantID string) (*types.ChannelRules, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if r, ok := m.rules[tenantID]; ok {
		return &r.Rules, nil
	}
	return nil, types.ErrChannelRulesNotFound
}

func (m *mockChannelRulesStore) Update(_ context.Context, tenantID string, rules *types.ChannelRules) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rules[tenantID] = &types.TenantChannelRules{
		TenantID: tenantID,
		Rules:    *rules,
	}
	return nil
}

func (m *mockChannelRulesStore) Delete(_ context.Context, tenantID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rules, tenantID)
	return nil
}

func (m *mockChannelRulesStore) List(_ context.Context) ([]*types.TenantChannelRules, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*types.TenantChannelRules
	for _, r := range m.rules {
		result = append(result, r)
	}
	return result, nil
}

// testEnv holds all test infrastructure.
type testEnv struct {
	bus    *eventbus.Bus
	client provisioningv1.ProvisioningInternalServiceClient
	cancel context.CancelFunc
	conn   *grpc.ClientConn
}

func (te *testEnv) close() {
	te.cancel()
	te.conn.Close()
}

// setupTestEnv creates a gRPC server with mock-store-backed service on bufconn.
func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()

	logger := zerolog.Nop()

	// Build mock stores and seed test data
	tenantStore := testutil.NewMockTenantStore()
	keyStore := testutil.NewMockKeyStore()
	topicStore := testutil.NewMockTopicStore()
	quotaStore := testutil.NewMockQuotaStore()
	auditStore := testutil.NewMockAuditStore()
	oidcStore := newMockOIDCConfigStore()
	channelRulesStore := newMockChannelRulesStore()

	ctx := context.Background()

	// Seed tenant
	tenant := testutil.NewTestTenant("test-tenant")
	if err := tenantStore.Create(ctx, tenant); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	// Seed key
	key := testutil.NewTestTenantKey("key-one", "test-tenant")
	key.Algorithm = "ES256"
	key.PublicKey = testPublicKeyPEM
	if err := keyStore.Create(ctx, key); err != nil {
		t.Fatalf("seed key: %v", err)
	}

	// Seed topic category
	topic := testutil.NewTestTenantTopic("test-tenant", "trade")
	if err := topicStore.Create(ctx, topic); err != nil {
		t.Fatalf("seed topic: %v", err)
	}

	// Seed OIDC config
	if err := oidcStore.Create(ctx, &types.TenantOIDCConfig{
		TenantID:  "test-tenant",
		IssuerURL: "https://auth.example.com",
		Audience:  "my-api",
		Enabled:   true,
	}); err != nil {
		t.Fatalf("seed OIDC: %v", err)
	}

	// Seed channel rules
	if err := channelRulesStore.Create(ctx, "test-tenant", &types.ChannelRules{
		Public:  []string{"*.trade"},
		Default: []string{"news"},
	}); err != nil {
		t.Fatalf("seed channel rules: %v", err)
	}

	bus := eventbus.New(logger)

	svc := provisioning.NewService(provisioning.ServiceConfig{
		TenantStore:       tenantStore,
		KeyStore:          keyStore,
		TopicStore:        topicStore,
		QuotaStore:        quotaStore,
		AuditStore:        auditStore,
		OIDCConfigStore:   oidcStore,
		ChannelRulesStore: channelRulesStore,
		KafkaAdmin:        testutil.NewMockKafkaAdmin(),
		EventBus:          bus,
		Logger:            logger,
		TopicNamespace:    "test",
	})

	srv := grpcserver.NewServer(svc, bus, logger)

	// Create bufconn listener
	lis := bufconn.Listen(bufSize)

	gs := grpc.NewServer()
	provisioningv1.RegisterProvisioningInternalServiceServer(gs, srv)

	go func() {
		if err := gs.Serve(lis); err != nil {
			// Server stopped
		}
	}()

	_, cancel := context.WithCancel(context.Background())

	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		cancel()
		t.Fatalf("dial bufconn: %v", err)
	}

	client := provisioningv1.NewProvisioningInternalServiceClient(conn)

	t.Cleanup(func() {
		cancel()
		conn.Close()
		gs.Stop()
	})

	return &testEnv{
		bus:    bus,
		client: client,
		cancel: cancel,
		conn:   conn,
	}
}

func TestWatchKeys_Snapshot(t *testing.T) {
	env := setupTestEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := env.client.WatchKeys(ctx, &provisioningv1.WatchKeysRequest{})
	if err != nil {
		t.Fatalf("WatchKeys() error = %v", err)
	}

	resp, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv() error = %v", err)
	}

	if !resp.IsSnapshot {
		t.Error("first message should be a snapshot")
	}

	if len(resp.Keys) != 1 {
		t.Fatalf("expected 1 key in snapshot, got %d", len(resp.Keys))
	}

	key := resp.Keys[0]
	if key.KeyId != "key-one" {
		t.Errorf("key ID = %q, want %q", key.KeyId, "key-one")
	}
	if key.TenantId != "test-tenant" {
		t.Errorf("key tenant ID = %q, want %q", key.TenantId, "test-tenant")
	}
	if key.Algorithm != "ES256" {
		t.Errorf("key algorithm = %q, want %q", key.Algorithm, "ES256")
	}
}

func TestWatchKeys_Delta(t *testing.T) {
	env := setupTestEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := env.client.WatchKeys(ctx, &provisioningv1.WatchKeysRequest{})
	if err != nil {
		t.Fatalf("WatchKeys() error = %v", err)
	}

	// Receive snapshot
	_, err = stream.Recv()
	if err != nil {
		t.Fatalf("Recv snapshot error = %v", err)
	}

	// Publish KeysChanged event
	env.bus.Publish(eventbus.Event{Type: eventbus.KeysChanged})

	// Receive delta
	resp, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv delta error = %v", err)
	}

	if resp.IsSnapshot {
		t.Error("second message should be a delta, not snapshot")
	}

	if len(resp.Keys) != 1 {
		t.Errorf("expected 1 key in delta, got %d", len(resp.Keys))
	}
}

func TestWatchTenantConfig_Snapshot(t *testing.T) {
	env := setupTestEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := env.client.WatchTenantConfig(ctx, &provisioningv1.WatchTenantConfigRequest{})
	if err != nil {
		t.Fatalf("WatchTenantConfig() error = %v", err)
	}

	resp, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv() error = %v", err)
	}

	if !resp.IsSnapshot {
		t.Error("first message should be a snapshot")
	}

	if len(resp.Tenants) != 1 {
		t.Fatalf("expected 1 tenant config in snapshot, got %d", len(resp.Tenants))
	}

	tc := resp.Tenants[0]
	if tc.TenantId != "test-tenant" {
		t.Errorf("tenant ID = %q, want %q", tc.TenantId, "test-tenant")
	}

	if tc.Oidc == nil {
		t.Fatal("expected OIDC config, got nil")
	}
	if tc.Oidc.IssuerUrl != "https://auth.example.com" {
		t.Errorf("issuer URL = %q, want %q", tc.Oidc.IssuerUrl, "https://auth.example.com")
	}

	if tc.ChannelRules == nil {
		t.Fatal("expected channel rules, got nil")
	}
	if len(tc.ChannelRules.PublicChannels) != 1 || tc.ChannelRules.PublicChannels[0] != "*.trade" {
		t.Errorf("public channels = %v, want [*.trade]", tc.ChannelRules.PublicChannels)
	}
}

func TestWatchTopics_Snapshot(t *testing.T) {
	env := setupTestEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := env.client.WatchTopics(ctx, &provisioningv1.WatchTopicsRequest{
		Namespace: "test",
	})
	if err != nil {
		t.Fatalf("WatchTopics() error = %v", err)
	}

	resp, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv() error = %v", err)
	}

	if !resp.IsSnapshot {
		t.Error("first message should be a snapshot")
	}

	// Default consumer type is shared, so topics should be in SharedTopics
	if len(resp.SharedTopics) != 1 {
		t.Fatalf("expected 1 shared topic, got %d", len(resp.SharedTopics))
	}

	// Topic name should follow the pattern: namespace.tenantID.category
	expectedTopic := "test.test-tenant.trade"
	if resp.SharedTopics[0] != expectedTopic {
		t.Errorf("shared topic = %q, want %q", resp.SharedTopics[0], expectedTopic)
	}
}

func TestStream_ContextCancellation(t *testing.T) {
	env := setupTestEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	stream, err := env.client.WatchKeys(ctx, &provisioningv1.WatchKeysRequest{})
	if err != nil {
		cancel()
		t.Fatalf("WatchKeys() error = %v", err)
	}

	// Receive snapshot
	_, err = stream.Recv()
	if err != nil {
		cancel()
		t.Fatalf("Recv snapshot error = %v", err)
	}

	// Cancel context
	cancel()

	// Next Recv should return error
	_, err = stream.Recv()
	if err == nil {
		t.Error("expected error after context cancellation")
	}
}
