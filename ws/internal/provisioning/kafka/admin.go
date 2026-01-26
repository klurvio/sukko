// Package kafka provides Kafka/Redpanda admin operations for tenant provisioning.
package kafka

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/scram"

	"github.com/Toniq-Labs/odin-ws/internal/provisioning"
)

// AdminConfig holds configuration for the Kafka admin client.
type AdminConfig struct {
	// Brokers is the list of Kafka/Redpanda broker addresses.
	Brokers []string

	// Timeout is the default timeout for admin operations.
	Timeout time.Duration

	// SASL authentication (nil = no auth).
	SASL *SASLConfig

	// TLS encryption (nil = no TLS).
	TLS *TLSConfig

	// Logger for structured logging.
	Logger zerolog.Logger
}

// SASLConfig holds SASL authentication configuration.
type SASLConfig struct {
	Mechanism string // "scram-sha-256" or "scram-sha-512"
	Username  string
	Password  string
}

// TLSConfig holds TLS encryption configuration.
type TLSConfig struct {
	Enabled            bool
	InsecureSkipVerify bool
	CAPath             string
}

// Admin implements provisioning.KafkaAdmin using franz-go/kadm.
type Admin struct {
	client  *kgo.Client
	admin   *kadm.Client
	timeout time.Duration
	logger  zerolog.Logger
}

// NewAdmin creates a new Kafka admin client.
func NewAdmin(cfg AdminConfig) (*Admin, error) {
	if len(cfg.Brokers) == 0 {
		return nil, errors.New("at least one broker is required")
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	// Build client options
	opts := []kgo.Opt{
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ClientID("odin-provisioning-admin"),
		kgo.RequestTimeoutOverhead(timeout),
	}

	// Add SASL authentication if configured
	if cfg.SASL != nil {
		mechanism := scram.Auth{
			User: cfg.SASL.Username,
			Pass: cfg.SASL.Password,
		}

		switch cfg.SASL.Mechanism {
		case "scram-sha-256":
			opts = append(opts, kgo.SASL(mechanism.AsSha256Mechanism()))
			cfg.Logger.Info().
				Str("mechanism", "SCRAM-SHA-256").
				Str("username", cfg.SASL.Username).
				Msg("Kafka admin SASL authentication enabled")
		case "scram-sha-512":
			opts = append(opts, kgo.SASL(mechanism.AsSha512Mechanism()))
			cfg.Logger.Info().
				Str("mechanism", "SCRAM-SHA-512").
				Str("username", cfg.SASL.Username).
				Msg("Kafka admin SASL authentication enabled")
		default:
			return nil, fmt.Errorf("unsupported SASL mechanism: %s", cfg.SASL.Mechanism)
		}
	}

	// Add TLS encryption if configured
	if cfg.TLS != nil && cfg.TLS.Enabled {
		tlsCfg := &tls.Config{
			InsecureSkipVerify: cfg.TLS.InsecureSkipVerify, //nolint:gosec // Controlled by configuration for dev/testing environments
			MinVersion:         tls.VersionTLS12,
		}

		if cfg.TLS.CAPath != "" {
			caCert, err := os.ReadFile(cfg.TLS.CAPath)
			if err != nil {
				return nil, fmt.Errorf("failed to read CA certificate: %w", err)
			}
			caCertPool := x509.NewCertPool()
			if !caCertPool.AppendCertsFromPEM(caCert) {
				return nil, errors.New("failed to parse CA certificate")
			}
			tlsCfg.RootCAs = caCertPool
		}

		opts = append(opts, kgo.DialTLSConfig(tlsCfg))
		cfg.Logger.Info().
			Bool("insecure_skip_verify", cfg.TLS.InsecureSkipVerify).
			Msg("Kafka admin TLS encryption enabled")
	}

	// Create kgo client
	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka client: %w", err)
	}

	// Ping to verify connection
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := client.Ping(ctx); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to connect to Kafka: %w", err)
	}

	// Create kadm admin client
	adminClient := kadm.NewClient(client)

	cfg.Logger.Info().
		Strs("brokers", cfg.Brokers).
		Msg("Kafka admin client connected")

	return &Admin{
		client:  client,
		admin:   adminClient,
		timeout: timeout,
		logger:  cfg.Logger,
	}, nil
}

// Close closes the admin client connection.
func (a *Admin) Close() {
	if a.client != nil {
		a.client.Close()
	}
}

// CreateTopic creates a new Kafka topic.
func (a *Admin) CreateTopic(ctx context.Context, name string, partitions int, config map[string]string) error {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	// Convert config map to kadm format (pointers)
	configs := make(map[string]*string)
	for k, v := range config {
		val := v
		configs[k] = &val
	}

	resp, err := a.admin.CreateTopics(ctx, int32(partitions), int16(-1), configs, name) //nolint:gosec // Partition count validated externally, safe conversion
	if err != nil {
		return fmt.Errorf("create topic request failed: %w", err)
	}

	// Check for topic-level errors
	for _, tr := range resp {
		if tr.Err != nil {
			// Ignore "topic already exists" error
			if errors.Is(tr.Err, kerr.TopicAlreadyExists) {
				a.logger.Debug().
					Str("topic", name).
					Msg("Topic already exists, skipping creation")
				return nil
			}
			return fmt.Errorf("create topic %q failed: %w", tr.Topic, tr.Err)
		}
	}

	a.logger.Info().
		Str("topic", name).
		Int("partitions", partitions).
		Msg("Topic created")

	return nil
}

// DeleteTopic deletes a Kafka topic.
func (a *Admin) DeleteTopic(ctx context.Context, name string) error {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	resp, err := a.admin.DeleteTopics(ctx, name)
	if err != nil {
		return fmt.Errorf("delete topic request failed: %w", err)
	}

	for _, tr := range resp {
		if tr.Err != nil {
			// Ignore "topic not found" error
			if errors.Is(tr.Err, kerr.UnknownTopicOrPartition) {
				a.logger.Debug().
					Str("topic", name).
					Msg("Topic does not exist, skipping deletion")
				return nil
			}
			return fmt.Errorf("delete topic %q failed: %w", tr.Topic, tr.Err)
		}
	}

	a.logger.Info().
		Str("topic", name).
		Msg("Topic deleted")

	return nil
}

// TopicExists checks if a topic exists.
func (a *Admin) TopicExists(ctx context.Context, name string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	topics, err := a.admin.ListTopics(ctx, name)
	if err != nil {
		return false, fmt.Errorf("list topics failed: %w", err)
	}

	_, exists := topics[name]
	return exists, nil
}

// SetTopicConfig updates topic configuration.
func (a *Admin) SetTopicConfig(ctx context.Context, name string, config map[string]string) error {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	// Build alter configs request
	alterConfigs := make([]kadm.AlterConfig, 0, len(config))
	for k, v := range config {
		val := v
		alterConfigs = append(alterConfigs, kadm.AlterConfig{
			Name:  k,
			Value: &val,
		})
	}

	resp, err := a.admin.AlterTopicConfigs(ctx, alterConfigs, name)
	if err != nil {
		return fmt.Errorf("alter topic config request failed: %w", err)
	}

	for _, r := range resp {
		if r.Err != nil {
			return fmt.Errorf("alter topic config %q failed: %w", r.Name, r.Err)
		}
	}

	a.logger.Info().
		Str("topic", name).
		Int("config_count", len(config)).
		Msg("Topic config updated")

	return nil
}

// CreateACL creates an ACL for a tenant.
// Supports topic and group resources with prefixed patterns for multi-tenant isolation.
func (a *Admin) CreateACL(ctx context.Context, acl provisioning.ACLBinding) error {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	// Build ACL using ACLBuilder
	builder := kadm.NewACLs()

	// Set principal (permission)
	if acl.Permission == "DENY" {
		builder = builder.Deny(acl.Principal)
	} else {
		builder = builder.Allow(acl.Principal)
	}

	// Set pattern type
	switch acl.PatternType {
	case "PREFIXED":
		builder = builder.ResourcePatternType(kadm.ACLPatternPrefixed)
	case "LITERAL":
		builder = builder.ResourcePatternType(kadm.ACLPatternLiteral)
	default:
		builder = builder.ResourcePatternType(kadm.ACLPatternLiteral)
	}

	// Set operation
	op := a.mapOperation(acl.Operation)
	builder = builder.Operations(op)

	// Set resource
	switch acl.ResourceType {
	case "TOPIC":
		builder = builder.Topics(acl.ResourceName)
	case "GROUP":
		builder = builder.Groups(acl.ResourceName)
	case "CLUSTER":
		builder = builder.Clusters()
	case "TRANSACTIONAL_ID":
		builder = builder.TransactionalIDs(acl.ResourceName)
	default:
		builder = builder.Topics(acl.ResourceName)
	}

	results, err := a.admin.CreateACLs(ctx, builder)
	if err != nil {
		return fmt.Errorf("create ACL request failed: %w", err)
	}

	for _, r := range results {
		if r.Err != nil {
			return fmt.Errorf("create ACL failed: %w", r.Err)
		}
	}

	a.logger.Info().
		Str("principal", acl.Principal).
		Str("resource_type", acl.ResourceType).
		Str("resource_name", acl.ResourceName).
		Str("pattern_type", acl.PatternType).
		Str("operation", acl.Operation).
		Msg("ACL created")

	return nil
}

// DeleteACL deletes an ACL.
func (a *Admin) DeleteACL(ctx context.Context, acl provisioning.ACLBinding) error {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	// Build ACL filter for deletion
	builder := kadm.NewACLs()

	if acl.Permission == "DENY" {
		builder = builder.Deny(acl.Principal)
	} else {
		builder = builder.Allow(acl.Principal)
	}

	switch acl.PatternType {
	case "PREFIXED":
		builder = builder.ResourcePatternType(kadm.ACLPatternPrefixed)
	case "LITERAL":
		builder = builder.ResourcePatternType(kadm.ACLPatternLiteral)
	default:
		builder = builder.ResourcePatternType(kadm.ACLPatternLiteral)
	}

	op := a.mapOperation(acl.Operation)
	builder = builder.Operations(op)

	switch acl.ResourceType {
	case "TOPIC":
		builder = builder.Topics(acl.ResourceName)
	case "GROUP":
		builder = builder.Groups(acl.ResourceName)
	}

	results, err := a.admin.DeleteACLs(ctx, builder)
	if err != nil {
		return fmt.Errorf("delete ACL request failed: %w", err)
	}

	for _, r := range results {
		if r.Err != nil {
			return fmt.Errorf("delete ACL failed: %w", r.Err)
		}
	}

	a.logger.Info().
		Str("principal", acl.Principal).
		Str("resource_name", acl.ResourceName).
		Msg("ACL deleted")

	return nil
}

// SetQuota sets resource quotas for a tenant principal.
func (a *Admin) SetQuota(ctx context.Context, tenantID string, quota provisioning.QuotaConfig) error {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	// Build quota alterations
	alterations := []kadm.AlterClientQuotaOp{}

	if quota.ProducerByteRate > 0 {
		alterations = append(alterations, kadm.AlterClientQuotaOp{
			Key:   "producer_byte_rate",
			Value: float64(quota.ProducerByteRate),
		})
	}

	if quota.ConsumerByteRate > 0 {
		alterations = append(alterations, kadm.AlterClientQuotaOp{
			Key:   "consumer_byte_rate",
			Value: float64(quota.ConsumerByteRate),
		})
	}

	if len(alterations) == 0 {
		return nil
	}

	// Apply quotas to the tenant's user principal
	resp, err := a.admin.AlterClientQuotas(ctx, []kadm.AlterClientQuotaEntry{
		{
			Entity: []kadm.ClientQuotaEntityComponent{
				{Type: "user", Name: &tenantID},
			},
			Ops: alterations,
		},
	})
	if err != nil {
		return fmt.Errorf("alter quotas request failed: %w", err)
	}

	for _, r := range resp {
		if r.Err != nil {
			return fmt.Errorf("alter quotas failed: %w", r.Err)
		}
	}

	a.logger.Info().
		Str("tenant_id", tenantID).
		Int64("producer_byte_rate", quota.ProducerByteRate).
		Int64("consumer_byte_rate", quota.ConsumerByteRate).
		Msg("Quotas updated")

	return nil
}

// mapOperation maps string operation to kadm ACLOperation.
func (a *Admin) mapOperation(s string) kadm.ACLOperation {
	switch s {
	case "ALL":
		return kadm.OpAll
	case "READ":
		return kadm.OpRead
	case "WRITE":
		return kadm.OpWrite
	case "CREATE":
		return kadm.OpCreate
	case "DELETE":
		return kadm.OpDelete
	case "ALTER":
		return kadm.OpAlter
	case "DESCRIBE":
		return kadm.OpDescribe
	case "CLUSTER_ACTION":
		return kadm.OpClusterAction
	case "DESCRIBE_CONFIGS":
		return kadm.OpDescribeConfigs
	case "ALTER_CONFIGS":
		return kadm.OpAlterConfigs
	case "IDEMPOTENT_WRITE":
		return kadm.OpIdempotentWrite
	default:
		return kadm.OpAll
	}
}

// Ensure Admin implements provisioning.KafkaAdmin.
var _ provisioning.KafkaAdmin = (*Admin)(nil)
