//go:build integration

package kafka

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"

	"github.com/Toniq-Labs/odin-ws/internal/provisioning"
)

// getTestBrokers returns brokers for integration testing.
// Uses KAFKA_BROKERS env var if set (for local dev with docker-compose),
// otherwise starts a Redpanda testcontainer.
func getTestBrokers(t *testing.T, ctx context.Context) (string, func()) {
	if brokers := os.Getenv("KAFKA_BROKERS"); brokers != "" {
		t.Logf("Using external Kafka brokers: %s", brokers)
		return brokers, func() {} // No cleanup needed for external
	}

	t.Log("Starting Redpanda testcontainer...")
	container, err := redpanda.Run(ctx,
		"docker.redpanda.com/redpandadata/redpanda:v24.1.1",
		redpanda.WithAutoCreateTopics(),
	)
	require.NoError(t, err, "Failed to start Redpanda container")

	broker, err := container.KafkaSeedBroker(ctx)
	require.NoError(t, err, "Failed to get Kafka seed broker")

	t.Logf("Redpanda started at: %s", broker)

	cleanup := func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("Warning: failed to terminate container: %v", err)
		}
	}

	return broker, cleanup
}

func TestAdmin_Integration_CreateTopic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	broker, cleanup := getTestBrokers(t, ctx)
	defer cleanup()

	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()

	admin, err := NewAdmin(AdminConfig{
		Brokers: []string{broker},
		Timeout: 30 * time.Second,
		Logger:  logger,
	})
	require.NoError(t, err, "Failed to create admin")
	defer admin.Close()

	topicName := "test-topic-" + time.Now().Format("20060102150405")

	// Test CreateTopic
	t.Run("create new topic", func(t *testing.T) {
		err := admin.CreateTopic(ctx, topicName, 3, map[string]string{
			"retention.ms": "86400000", // 1 day
		})
		assert.NoError(t, err)
	})

	// Test TopicExists
	t.Run("topic exists after creation", func(t *testing.T) {
		exists, err := admin.TopicExists(ctx, topicName)
		assert.NoError(t, err)
		assert.True(t, exists)
	})

	// Test CreateTopic idempotency
	t.Run("create existing topic is idempotent", func(t *testing.T) {
		err := admin.CreateTopic(ctx, topicName, 3, nil)
		assert.NoError(t, err, "Creating existing topic should not error")
	})

	// Test SetTopicConfig
	t.Run("update topic config", func(t *testing.T) {
		err := admin.SetTopicConfig(ctx, topicName, map[string]string{
			"retention.ms": "172800000", // 2 days
		})
		assert.NoError(t, err)
	})

	// Test DeleteTopic
	t.Run("delete topic", func(t *testing.T) {
		err := admin.DeleteTopic(ctx, topicName)
		assert.NoError(t, err)
	})

	// Test TopicExists after deletion
	t.Run("topic does not exist after deletion", func(t *testing.T) {
		// May need a small delay for Kafka to process deletion
		time.Sleep(500 * time.Millisecond)

		exists, err := admin.TopicExists(ctx, topicName)
		assert.NoError(t, err)
		assert.False(t, exists)
	})

	// Test DeleteTopic idempotency
	t.Run("delete non-existent topic is idempotent", func(t *testing.T) {
		err := admin.DeleteTopic(ctx, "non-existent-topic-xyz")
		assert.NoError(t, err, "Deleting non-existent topic should not error")
	})
}

func TestAdmin_Integration_ACLs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	broker, cleanup := getTestBrokers(t, ctx)
	defer cleanup()

	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()

	admin, err := NewAdmin(AdminConfig{
		Brokers: []string{broker},
		Timeout: 30 * time.Second,
		Logger:  logger,
	})
	require.NoError(t, err, "Failed to create admin")
	defer admin.Close()

	tenantID := "test-tenant"
	principal := provisioning.FormatPrincipal(tenantID)

	// Test CreateACL for topics
	t.Run("create topic ACL with prefix", func(t *testing.T) {
		acl := provisioning.ACLBinding{
			Principal:    principal,
			ResourceType: provisioning.ACLResourceTopic,
			ResourceName: "main." + tenantID,
			PatternType:  provisioning.ACLPatternPrefixed,
			Operation:    provisioning.ACLOpAll,
			Permission:   provisioning.ACLPermissionAllow,
		}
		err := admin.CreateACL(ctx, acl)
		assert.NoError(t, err)
	})

	// Test CreateACL for consumer groups
	t.Run("create group ACL with prefix", func(t *testing.T) {
		acl := provisioning.ACLBinding{
			Principal:    principal,
			ResourceType: provisioning.ACLResourceGroup,
			ResourceName: tenantID,
			PatternType:  provisioning.ACLPatternPrefixed,
			Operation:    provisioning.ACLOpRead,
			Permission:   provisioning.ACLPermissionAllow,
		}
		err := admin.CreateACL(ctx, acl)
		assert.NoError(t, err)
	})

	// Test CreateACL with literal pattern
	t.Run("create literal ACL", func(t *testing.T) {
		acl := provisioning.ACLBinding{
			Principal:    principal,
			ResourceType: provisioning.ACLResourceTopic,
			ResourceName: "specific-topic",
			PatternType:  provisioning.ACLPatternLiteral,
			Operation:    provisioning.ACLOpRead,
			Permission:   provisioning.ACLPermissionAllow,
		}
		err := admin.CreateACL(ctx, acl)
		assert.NoError(t, err)
	})

	// Test DeleteACL
	t.Run("delete ACL", func(t *testing.T) {
		acl := provisioning.ACLBinding{
			Principal:    principal,
			ResourceType: provisioning.ACLResourceTopic,
			ResourceName: "specific-topic",
			PatternType:  provisioning.ACLPatternLiteral,
			Operation:    provisioning.ACLOpRead,
			Permission:   provisioning.ACLPermissionAllow,
		}
		err := admin.DeleteACL(ctx, acl)
		assert.NoError(t, err)
	})
}

func TestAdmin_Integration_Quotas(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	broker, cleanup := getTestBrokers(t, ctx)
	defer cleanup()

	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()

	admin, err := NewAdmin(AdminConfig{
		Brokers: []string{broker},
		Timeout: 30 * time.Second,
		Logger:  logger,
	})
	require.NoError(t, err, "Failed to create admin")
	defer admin.Close()

	tenantID := "quota-test-tenant"

	t.Run("set producer and consumer quotas", func(t *testing.T) {
		quota := provisioning.QuotaConfig{
			ProducerByteRate: 10 * 1024 * 1024, // 10 MB/s
			ConsumerByteRate: 50 * 1024 * 1024, // 50 MB/s
		}
		err := admin.SetQuota(ctx, tenantID, quota)
		assert.NoError(t, err)
	})

	t.Run("set only producer quota", func(t *testing.T) {
		quota := provisioning.QuotaConfig{
			ProducerByteRate: 5 * 1024 * 1024, // 5 MB/s
		}
		err := admin.SetQuota(ctx, tenantID, quota)
		assert.NoError(t, err)
	})

	t.Run("empty quota does nothing", func(t *testing.T) {
		quota := provisioning.QuotaConfig{}
		err := admin.SetQuota(ctx, tenantID, quota)
		assert.NoError(t, err)
	})
}

func TestAdmin_Integration_MultiTenantScenario(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	broker, cleanup := getTestBrokers(t, ctx)
	defer cleanup()

	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()

	admin, err := NewAdmin(AdminConfig{
		Brokers: []string{broker},
		Timeout: 30 * time.Second,
		Logger:  logger,
	})
	require.NoError(t, err, "Failed to create admin")
	defer admin.Close()

	// Simulate provisioning multiple tenants
	tenants := []string{"acme", "globex", "initech"}
	namespace := "main"

	for _, tenantID := range tenants {
		t.Run("provision tenant "+tenantID, func(t *testing.T) {
			principal := provisioning.FormatPrincipal(tenantID)

			// Create tenant-specific topic
			topicName := namespace + "." + tenantID + ".trade"
			err := admin.CreateTopic(ctx, topicName, 3, map[string]string{
				"retention.ms": "604800000", // 7 days
			})
			require.NoError(t, err, "Failed to create topic for tenant %s", tenantID)

			// Create topic ACL (prefixed)
			topicACL := provisioning.ACLBinding{
				Principal:    principal,
				ResourceType: provisioning.ACLResourceTopic,
				ResourceName: namespace + "." + tenantID,
				PatternType:  provisioning.ACLPatternPrefixed,
				Operation:    provisioning.ACLOpAll,
				Permission:   provisioning.ACLPermissionAllow,
			}
			err = admin.CreateACL(ctx, topicACL)
			require.NoError(t, err, "Failed to create topic ACL for tenant %s", tenantID)

			// Create consumer group ACL (prefixed)
			groupACL := provisioning.ACLBinding{
				Principal:    principal,
				ResourceType: provisioning.ACLResourceGroup,
				ResourceName: tenantID,
				PatternType:  provisioning.ACLPatternPrefixed,
				Operation:    provisioning.ACLOpAll,
				Permission:   provisioning.ACLPermissionAllow,
			}
			err = admin.CreateACL(ctx, groupACL)
			require.NoError(t, err, "Failed to create group ACL for tenant %s", tenantID)

			// Set quotas
			quota := provisioning.QuotaConfig{
				ProducerByteRate: 10 * 1024 * 1024,
				ConsumerByteRate: 50 * 1024 * 1024,
			}
			err = admin.SetQuota(ctx, tenantID, quota)
			require.NoError(t, err, "Failed to set quotas for tenant %s", tenantID)

			// Verify topic exists
			exists, err := admin.TopicExists(ctx, topicName)
			require.NoError(t, err)
			assert.True(t, exists, "Topic should exist for tenant %s", tenantID)
		})
	}

	// Cleanup - deprovision one tenant
	t.Run("deprovision tenant acme", func(t *testing.T) {
		tenantID := "acme"
		topicName := namespace + "." + tenantID + ".trade"

		err := admin.DeleteTopic(ctx, topicName)
		assert.NoError(t, err)

		// Note: ACL cleanup would be done via DeleteACL calls
		// In production, you'd iterate through all ACLs for this tenant
	})
}
