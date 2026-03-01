package grpcserver_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	provisioningv1 "github.com/Toniq-Labs/odin-ws/gen/proto/odin/provisioning/v1"
	"github.com/Toniq-Labs/odin-ws/internal/provisioning"
	"github.com/Toniq-Labs/odin-ws/internal/provisioning/configstore"
	"github.com/Toniq-Labs/odin-ws/internal/provisioning/eventbus"
	"github.com/Toniq-Labs/odin-ws/internal/provisioning/grpcserver"
)

const bufSize = 1024 * 1024

// testPublicKeyPEM is a valid EC P-256 public key for tests.
const testPublicKeyPEM = `-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEEVs/o5+uQbTjL3chynL4wXgUg2R9
q9UU8I5mEovUf86QZ7kOBIjJwqnzD1omageEHWwHdBO6B+dFabmdT9POxg==
-----END PUBLIC KEY-----`

// noopKafkaAdmin satisfies provisioning.KafkaAdmin for tests.
type noopKafkaAdmin struct{}

func (n *noopKafkaAdmin) CreateTopic(_ context.Context, _ string, _ int, _ map[string]string) error {
	return nil
}
func (n *noopKafkaAdmin) DeleteTopic(_ context.Context, _ string) error    { return nil }
func (n *noopKafkaAdmin) TopicExists(_ context.Context, _ string) (bool, error) { return true, nil }
func (n *noopKafkaAdmin) SetTopicConfig(_ context.Context, _ string, _ map[string]string) error {
	return nil
}
func (n *noopKafkaAdmin) CreateACL(_ context.Context, _ provisioning.ACLBinding) error { return nil }
func (n *noopKafkaAdmin) DeleteACL(_ context.Context, _ provisioning.ACLBinding) error { return nil }
func (n *noopKafkaAdmin) SetQuota(_ context.Context, _ string, _ provisioning.QuotaConfig) error {
	return nil
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

// setupTestEnv creates a gRPC server with a configstore-backed service on bufconn.
func setupTestEnv(t *testing.T, cfg *configstore.ConfigFile) *testEnv {
	t.Helper()

	logger := zerolog.Nop()

	// Build in-memory stores from config
	stores := configstore.BuildStores(cfg, logger)

	bus := eventbus.New(logger)

	svc := provisioning.NewService(provisioning.ServiceConfig{
		TenantStore:       stores.TenantStore(),
		KeyStore:          stores.KeyStore(),
		TopicStore:        stores.TopicStore(),
		QuotaStore:        stores.QuotaStore(),
		AuditStore:        stores.AuditStore(),
		OIDCConfigStore:   stores.OIDCConfigStore(),
		ChannelRulesStore: stores.ChannelRulesStore(),
		KafkaAdmin:        &noopKafkaAdmin{},
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

func testConfigWithTenant() *configstore.ConfigFile {
	active := true
	return &configstore.ConfigFile{
		Tenants: []configstore.TenantConfig{
			{
				ID:   "test-tenant",
				Name: "Test Tenant",
				Categories: []configstore.CategoryConfig{
					{Name: "trade", Partitions: 3},
				},
				Keys: []configstore.KeyConfig{
					{
						ID:        "key-one",
						Algorithm: "ES256",
						PublicKey: testPublicKeyPEM,
						Active:    &active,
					},
				},
				OIDC: &configstore.OIDCConfig{
					IssuerURL: "https://auth.example.com",
					Audience:  "my-api",
					Enabled:   &active,
				},
				ChannelRules: &configstore.ChannelRulesConfig{
					PublicChannels:  []string{"*.trade"},
					DefaultChannels: []string{"news"},
				},
			},
		},
	}
}

func TestWatchKeys_Snapshot(t *testing.T) {
	env := setupTestEnv(t, testConfigWithTenant())

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
	env := setupTestEnv(t, testConfigWithTenant())

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
	env := setupTestEnv(t, testConfigWithTenant())

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
	env := setupTestEnv(t, testConfigWithTenant())

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
	env := setupTestEnv(t, testConfigWithTenant())

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
