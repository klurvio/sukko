package provisioning

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/rs/zerolog"

	"github.com/Toniq-Labs/odin-ws/internal/provisioning/eventbus"
	"github.com/Toniq-Labs/odin-ws/internal/shared/auth"
	"github.com/Toniq-Labs/odin-ws/internal/shared/kafka"
	"github.com/Toniq-Labs/odin-ws/internal/shared/types"
)

// ServiceConfig holds the configuration for the provisioning service.
type ServiceConfig struct {
	TenantStore       TenantStore
	KeyStore          KeyStore
	TopicStore        TopicStore
	QuotaStore        QuotaStore
	AuditStore        AuditStore
	OIDCConfigStore   OIDCConfigStore
	ChannelRulesStore ChannelRulesStore
	KafkaAdmin        KafkaAdmin
	EventBus          *eventbus.Bus
	Logger            zerolog.Logger

	// Topic configuration
	TopicNamespace     string
	DefaultPartitions  int
	DefaultRetentionMs int64

	// Quota defaults
	MaxTopicsPerTenant   int
	MaxStorageBytes      int64
	DefaultProducerRate  int64
	DefaultConsumerRate  int64

	// Lifecycle
	DeprovisionGraceDays int
}

// Service provides tenant lifecycle management and provisioning operations.
type Service struct {
	tenants      TenantStore
	keys         KeyStore
	topics       TopicStore
	quotas       QuotaStore
	audit        AuditStore
	oidcConfigs  OIDCConfigStore
	channelRules ChannelRulesStore
	kafka        KafkaAdmin
	eventBus     *eventbus.Bus
	logger       zerolog.Logger
	config       ServiceConfig
}

// NewService creates a new provisioning Service.
func NewService(cfg ServiceConfig) *Service {
	return &Service{
		tenants:      cfg.TenantStore,
		keys:         cfg.KeyStore,
		topics:       cfg.TopicStore,
		quotas:       cfg.QuotaStore,
		audit:        cfg.AuditStore,
		oidcConfigs:  cfg.OIDCConfigStore,
		channelRules: cfg.ChannelRulesStore,
		kafka:        cfg.KafkaAdmin,
		eventBus:     cfg.EventBus,
		logger:       cfg.Logger,
		config:       cfg,
	}
}

// Ready checks if the service is ready to handle requests.
// Returns nil if database is accessible, otherwise returns the error.
func (s *Service) Ready(ctx context.Context) error {
	return s.tenants.Ping(ctx)
}

// CreateTenant creates a new tenant with optional initial key and topics.
func (s *Service) CreateTenant(ctx context.Context, req CreateTenantRequest) (*CreateTenantResponse, error) {
	// Validate tenant
	tenant := &Tenant{
		ID:           req.TenantID,
		Name:         req.Name,
		Status:       StatusActive,
		ConsumerType: req.ConsumerType,
		Metadata:     req.Metadata,
	}

	if tenant.ConsumerType == "" {
		tenant.ConsumerType = ConsumerShared
	}

	if err := tenant.Validate(); err != nil {
		return nil, fmt.Errorf("invalid tenant: %w", err)
	}

	// Create tenant record
	if err := s.tenants.Create(ctx, tenant); err != nil {
		return nil, fmt.Errorf("create tenant: %w", err)
	}

	// Create default quotas
	quota := &TenantQuota{
		TenantID:         tenant.ID,
		MaxTopics:        s.config.MaxTopicsPerTenant,
		MaxPartitions:    s.config.MaxTopicsPerTenant * s.config.DefaultPartitions,
		MaxStorageBytes:  s.config.MaxStorageBytes,
		ProducerByteRate: s.config.DefaultProducerRate,
		ConsumerByteRate: s.config.DefaultConsumerRate,
	}
	if err := s.quotas.Create(ctx, quota); err != nil {
		s.logger.Error().Err(err).Str("tenant_id", tenant.ID).Msg("Failed to create quotas")
		// Don't fail - quotas are not critical
	}

	response := &CreateTenantResponse{
		Tenant: tenant,
		Topics: []string{},
	}

	// Register initial key if provided
	if req.PublicKey != nil {
		key := &TenantKey{
			KeyID:     req.PublicKey.KeyID,
			TenantID:  tenant.ID,
			Algorithm: req.PublicKey.Algorithm,
			PublicKey: req.PublicKey.PublicKey,
			ExpiresAt: req.PublicKey.ExpiresAt,
		}
		if err := key.Validate(); err != nil {
			return nil, fmt.Errorf("invalid key: %w", err)
		}

		if err := s.keys.Create(ctx, key); err != nil {
			return nil, fmt.Errorf("create key: %w", err)
		}
		response.Key = key
	}

	// Create initial topics if categories provided
	if len(req.Categories) > 0 {
		topics, err := s.createTopicsForTenant(ctx, tenant.ID, req.Categories)
		if err != nil {
			s.logger.Error().Err(err).Str("tenant_id", tenant.ID).Msg("Failed to create topics")
			// Don't fail - topics can be created later
		} else {
			for _, t := range topics {
				// Build topic name at runtime for response
				topicName := kafka.BuildTopicName(s.config.TopicNamespace, tenant.ID, t.Category)
				response.Topics = append(response.Topics, topicName)
			}
		}
	}

	// Audit log
	s.auditLog(ctx, tenant.ID, ActionCreateTenant, Metadata{
		"name":          tenant.Name,
		"consumer_type": tenant.ConsumerType,
		"categories":    req.Categories,
	})

	s.logger.Info().
		Str("tenant_id", tenant.ID).
		Str("name", tenant.Name).
		Msg("Tenant created")

	s.emitEvent(eventbus.TopicsChanged)
	s.emitEvent(eventbus.TenantConfigChanged)

	return response, nil
}

// GetTenant retrieves a tenant by ID.
func (s *Service) GetTenant(ctx context.Context, tenantID string) (*Tenant, error) {
	return s.tenants.Get(ctx, tenantID)
}

// ListTenants returns tenants matching the given options.
func (s *Service) ListTenants(ctx context.Context, opts ListOptions) ([]*Tenant, int, error) {
	return s.tenants.List(ctx, opts)
}

// UpdateTenant updates tenant metadata.
func (s *Service) UpdateTenant(ctx context.Context, tenantID string, req UpdateTenantRequest) (*Tenant, error) {
	tenant, err := s.tenants.Get(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	if tenant.Status == StatusDeleted {
		return nil, errors.New("cannot update deleted tenant")
	}

	if req.Name != nil {
		tenant.Name = *req.Name
	}
	if req.ConsumerType != nil {
		tenant.ConsumerType = *req.ConsumerType
	}
	if req.Metadata != nil {
		tenant.Metadata = req.Metadata
	}

	if err := s.tenants.Update(ctx, tenant); err != nil {
		return nil, fmt.Errorf("update tenant: %w", err)
	}

	s.auditLog(ctx, tenantID, ActionUpdateTenant, Metadata{
		"name":          tenant.Name,
		"consumer_type": tenant.ConsumerType,
	})

	s.emitEvent(eventbus.TenantConfigChanged)

	return tenant, nil
}

// SuspendTenant temporarily disables a tenant.
func (s *Service) SuspendTenant(ctx context.Context, tenantID string) error {
	if err := s.tenants.UpdateStatus(ctx, tenantID, StatusSuspended); err != nil {
		return fmt.Errorf("suspend tenant: %w", err)
	}

	s.auditLog(ctx, tenantID, ActionSuspendTenant, nil)

	s.logger.Info().Str("tenant_id", tenantID).Msg("Tenant suspended")

	s.emitEvent(eventbus.TopicsChanged)
	s.emitEvent(eventbus.TenantConfigChanged)

	return nil
}

// ReactivateTenant reactivates a suspended tenant.
func (s *Service) ReactivateTenant(ctx context.Context, tenantID string) error {
	if err := s.tenants.UpdateStatus(ctx, tenantID, StatusActive); err != nil {
		return fmt.Errorf("reactivate tenant: %w", err)
	}

	s.auditLog(ctx, tenantID, ActionReactivateTenant, nil)

	s.logger.Info().Str("tenant_id", tenantID).Msg("Tenant reactivated")

	s.emitEvent(eventbus.TopicsChanged)
	s.emitEvent(eventbus.TenantConfigChanged)

	return nil
}

// DeprovisionTenant initiates tenant deletion with grace period.
func (s *Service) DeprovisionTenant(ctx context.Context, tenantID string) error {
	// Update status to deprovisioning
	if err := s.tenants.UpdateStatus(ctx, tenantID, StatusDeprovisioning); err != nil {
		return fmt.Errorf("set deprovisioning status: %w", err)
	}

	// Revoke all keys immediately
	if err := s.keys.RevokeAllForTenant(ctx, tenantID); err != nil {
		s.logger.Error().Err(err).Str("tenant_id", tenantID).Msg("Failed to revoke keys")
	}

	// Set deprovision deadline
	gracePeriod := time.Duration(s.config.DeprovisionGraceDays) * 24 * time.Hour
	deprovisionAt := time.Now().Add(gracePeriod)
	if err := s.tenants.SetDeprovisionAt(ctx, tenantID, &deprovisionAt); err != nil {
		s.logger.Error().Err(err).Str("tenant_id", tenantID).Msg("Failed to set deprovision deadline")
	}

	// Update topic retention to grace period (Kafka will delete after)
	topics, _ := s.topics.ListByTenant(ctx, tenantID)
	gracePeriodMs := int64(s.config.DeprovisionGraceDays * 24 * 60 * 60 * 1000)
	for _, topic := range topics {
		// Build topic name at runtime
		topicName := kafka.BuildTopicName(s.config.TopicNamespace, tenantID, topic.Category)
		if err := s.kafka.SetTopicConfig(ctx, topicName, map[string]string{
			"retention.ms": strconv.FormatInt(gracePeriodMs, 10),
		}); err != nil {
			s.logger.Error().Err(err).
				Str("tenant_id", tenantID).
				Str("topic", topicName).
				Msg("Failed to update topic retention")
		}
	}

	s.auditLog(ctx, tenantID, ActionDeprovisionTenant, Metadata{
		"grace_days":     s.config.DeprovisionGraceDays,
		"deprovision_at": deprovisionAt,
	})

	s.logger.Info().
		Str("tenant_id", tenantID).
		Time("deprovision_at", deprovisionAt).
		Msg("Tenant deprovisioning initiated")

	return nil
}

// CreateKey registers a new public key for a tenant.
func (s *Service) CreateKey(ctx context.Context, tenantID string, req CreateKeyRequest) (*TenantKey, error) {
	// Verify tenant exists and is active
	tenant, err := s.tenants.Get(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if tenant.Status != StatusActive {
		return nil, fmt.Errorf("tenant is not active: %s", tenant.Status)
	}

	key := &TenantKey{
		KeyID:     req.KeyID,
		TenantID:  tenantID,
		Algorithm: req.Algorithm,
		PublicKey: req.PublicKey,
		ExpiresAt: req.ExpiresAt,
	}

	if err := key.Validate(); err != nil {
		return nil, fmt.Errorf("invalid key: %w", err)
	}

	if err := s.keys.Create(ctx, key); err != nil {
		return nil, fmt.Errorf("create key: %w", err)
	}

	s.auditLog(ctx, tenantID, ActionCreateKey, Metadata{
		"key_id":    key.KeyID,
		"algorithm": key.Algorithm,
	})

	s.logger.Info().
		Str("tenant_id", tenantID).
		Str("key_id", key.KeyID).
		Msg("Key created")

	s.emitEvent(eventbus.KeysChanged)

	return key, nil
}

// ListKeys returns all keys for a tenant.
func (s *Service) ListKeys(ctx context.Context, tenantID string) ([]*TenantKey, error) {
	return s.keys.ListByTenant(ctx, tenantID)
}

// RevokeKey revokes a key.
func (s *Service) RevokeKey(ctx context.Context, tenantID, keyID string) error {
	// Verify key belongs to tenant
	key, err := s.keys.Get(ctx, keyID)
	if err != nil {
		return err
	}
	if key.TenantID != tenantID {
		return errors.New("key does not belong to tenant")
	}

	if err := s.keys.Revoke(ctx, keyID); err != nil {
		return fmt.Errorf("revoke key: %w", err)
	}

	s.auditLog(ctx, tenantID, ActionRevokeKey, Metadata{
		"key_id": keyID,
	})

	s.logger.Info().
		Str("tenant_id", tenantID).
		Str("key_id", keyID).
		Msg("Key revoked")

	s.emitEvent(eventbus.KeysChanged)

	return nil
}

// GetActiveKeys returns all active keys (for WS Gateway cache refresh).
func (s *Service) GetActiveKeys(ctx context.Context) ([]*TenantKey, error) {
	return s.keys.GetActiveKeys(ctx)
}

// CreateTopics creates topics for a tenant.
func (s *Service) CreateTopics(ctx context.Context, tenantID string, categories []string) ([]*TenantTopic, error) {
	// Verify tenant exists and is active
	tenant, err := s.tenants.Get(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if tenant.Status != StatusActive {
		return nil, fmt.Errorf("tenant is not active: %s", tenant.Status)
	}

	topics, err := s.createTopicsForTenant(ctx, tenantID, categories)
	if err != nil {
		return nil, err
	}

	s.emitEvent(eventbus.TopicsChanged)

	return topics, nil
}

// ListTopics returns all topic categories for a tenant.
// Note: TenantTopic.Category contains the category name. To get the full topic name,
// use kafka.BuildTopicName(service.TopicNamespace(), tenantID, category).
func (s *Service) ListTopics(ctx context.Context, tenantID string) ([]*TenantTopic, error) {
	return s.topics.ListByTenant(ctx, tenantID)
}

// TopicNamespace returns the configured Kafka topic namespace.
// Used to build full topic names: {namespace}.{tenantID}.{category}
func (s *Service) TopicNamespace() string {
	return s.config.TopicNamespace
}

// GetQuota returns quotas for a tenant.
func (s *Service) GetQuota(ctx context.Context, tenantID string) (*TenantQuota, error) {
	return s.quotas.Get(ctx, tenantID)
}

// UpdateQuota updates quotas for a tenant.
func (s *Service) UpdateQuota(ctx context.Context, tenantID string, req UpdateQuotaRequest) (*TenantQuota, error) {
	quota, err := s.quotas.Get(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	if req.MaxTopics != nil {
		quota.MaxTopics = *req.MaxTopics
	}
	if req.MaxPartitions != nil {
		quota.MaxPartitions = *req.MaxPartitions
	}
	if req.MaxStorageBytes != nil {
		quota.MaxStorageBytes = *req.MaxStorageBytes
	}
	if req.ProducerByteRate != nil {
		quota.ProducerByteRate = *req.ProducerByteRate
	}
	if req.ConsumerByteRate != nil {
		quota.ConsumerByteRate = *req.ConsumerByteRate
	}

	if err := s.quotas.Update(ctx, quota); err != nil {
		return nil, fmt.Errorf("update quota: %w", err)
	}

	// Apply Kafka quotas
	if err := s.kafka.SetQuota(ctx, tenantID, QuotaConfig{
		ProducerByteRate: quota.ProducerByteRate,
		ConsumerByteRate: quota.ConsumerByteRate,
	}); err != nil {
		s.logger.Error().Err(err).Str("tenant_id", tenantID).Msg("Failed to apply Kafka quotas")
	}

	s.auditLog(ctx, tenantID, ActionUpdateQuota, Metadata{
		"max_topics":         quota.MaxTopics,
		"max_partitions":     quota.MaxPartitions,
		"producer_byte_rate": quota.ProducerByteRate,
		"consumer_byte_rate": quota.ConsumerByteRate,
	})

	return quota, nil
}

// GetAuditLog returns audit entries for a tenant.
func (s *Service) GetAuditLog(ctx context.Context, tenantID string, opts ListOptions) ([]*AuditEntry, int, error) {
	return s.audit.ListByTenant(ctx, tenantID, opts)
}

// createTopicsForTenant creates topics for the given categories.
func (s *Service) createTopicsForTenant(ctx context.Context, tenantID string, categories []string) ([]*TenantTopic, error) {
	created := []*TenantTopic{}

	for _, category := range categories {
		if err := s.createSingleTopic(ctx, tenantID, category); err != nil {
			topicName := kafka.BuildTopicName(s.config.TopicNamespace, tenantID, category)
			s.logger.Error().Err(err).Str("topic", topicName).Msg("Failed to create topic")
			continue
		}
		created = append(created, &TenantTopic{
			TenantID:    tenantID,
			Category:    category,
			Partitions:  s.config.DefaultPartitions,
			RetentionMs: s.config.DefaultRetentionMs,
		})
	}

	// Create ACL for tenant
	if len(created) > 0 {
		acl := ACLBinding{
			Principal:    "User:" + tenantID,
			ResourceType: "TOPIC",
			ResourceName: fmt.Sprintf("%s.%s.", s.config.TopicNamespace, tenantID),
			PatternType:  "PREFIXED",
			Operation:    "ALL",
			Permission:   "ALLOW",
		}
		if err := s.kafka.CreateACL(ctx, acl); err != nil {
			s.logger.Error().Err(err).Str("tenant_id", tenantID).Msg("Failed to create ACL")
		}
	}

	return created, nil
}

// createSingleTopic creates a single Kafka topic and records the category in the database.
// Topic name is built at runtime using kafka.BuildTopicName(namespace, tenantID, category).
func (s *Service) createSingleTopic(ctx context.Context, tenantID, category string) error {
	// Build topic name at runtime
	topicName := kafka.BuildTopicName(s.config.TopicNamespace, tenantID, category)

	// Create in Kafka
	config := map[string]string{
		"retention.ms": strconv.FormatInt(s.config.DefaultRetentionMs, 10),
	}
	if err := s.kafka.CreateTopic(ctx, topicName, s.config.DefaultPartitions, config); err != nil {
		return fmt.Errorf("create kafka topic: %w", err)
	}

	// Record category in database (not full topic name)
	topic := &TenantTopic{
		TenantID:    tenantID,
		Category:    category,
		Partitions:  s.config.DefaultPartitions,
		RetentionMs: s.config.DefaultRetentionMs,
	}
	if err := s.topics.Create(ctx, topic); err != nil {
		return fmt.Errorf("record category: %w", err)
	}

	return nil
}

// auditLog records an audit entry.
func (s *Service) auditLog(ctx context.Context, tenantID, action string, details Metadata) {
	// Get actor type, defaulting to "system" if not set
	actorType := auth.GetActorType(ctx)
	if actorType == "system" {
		actorType = ActorTypeSystem
	}

	entry := &AuditEntry{
		TenantID:  tenantID,
		Action:    action,
		Actor:     auth.GetActor(ctx),
		ActorType: actorType,
		IPAddress: auth.GetClientIPFromContext(ctx),
		Details:   details,
	}
	if err := s.audit.Log(ctx, entry); err != nil {
		s.logger.Error().Err(err).Str("action", action).Msg("Failed to write audit log")
	}
}

// emitEvent publishes a provisioning change event to the event bus.
// Nil-guarded — does nothing if no event bus is configured.
func (s *Service) emitEvent(eventType eventbus.EventType) {
	if s.eventBus == nil {
		return
	}
	s.eventBus.Publish(eventbus.Event{Type: eventType})
}

// WithActor adds actor information to context.
// This is an alias for auth.WithActor for backwards compatibility.
var WithActor = auth.WithActor

// CreateOIDCConfig creates OIDC configuration for a tenant.
func (s *Service) CreateOIDCConfig(ctx context.Context, config *types.TenantOIDCConfig) error {
	if s.oidcConfigs == nil {
		return errors.New("OIDC config store not configured")
	}

	// Verify tenant exists and is active
	tenant, err := s.tenants.Get(ctx, config.TenantID)
	if err != nil {
		return err
	}
	if tenant.Status != StatusActive {
		return fmt.Errorf("tenant is not active: %s", tenant.Status)
	}

	// Validate config
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid OIDC config: %w", err)
	}

	// Create in store
	if err := s.oidcConfigs.Create(ctx, config); err != nil {
		return fmt.Errorf("create OIDC config: %w", err)
	}

	s.auditLog(ctx, config.TenantID, ActionCreateOIDCConfig, Metadata{
		"issuer_url": config.IssuerURL,
		"audience":   config.Audience,
	})

	s.logger.Info().
		Str("tenant_id", config.TenantID).
		Str("issuer_url", config.IssuerURL).
		Msg("OIDC config created")

	s.emitEvent(eventbus.TenantConfigChanged)

	return nil
}

// GetOIDCConfig retrieves OIDC configuration for a tenant.
func (s *Service) GetOIDCConfig(ctx context.Context, tenantID string) (*types.TenantOIDCConfig, error) {
	if s.oidcConfigs == nil {
		return nil, errors.New("OIDC config store not configured")
	}

	return s.oidcConfigs.Get(ctx, tenantID)
}

// UpdateOIDCConfig updates OIDC configuration for a tenant.
func (s *Service) UpdateOIDCConfig(ctx context.Context, config *types.TenantOIDCConfig) error {
	if s.oidcConfigs == nil {
		return errors.New("OIDC config store not configured")
	}

	// Validate config
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid OIDC config: %w", err)
	}

	// Update in store
	if err := s.oidcConfigs.Update(ctx, config); err != nil {
		return fmt.Errorf("update OIDC config: %w", err)
	}

	s.auditLog(ctx, config.TenantID, ActionUpdateOIDCConfig, Metadata{
		"issuer_url": config.IssuerURL,
		"audience":   config.Audience,
		"enabled":    config.Enabled,
	})

	s.logger.Info().
		Str("tenant_id", config.TenantID).
		Str("issuer_url", config.IssuerURL).
		Bool("enabled", config.Enabled).
		Msg("OIDC config updated")

	s.emitEvent(eventbus.TenantConfigChanged)

	return nil
}

// DeleteOIDCConfig deletes OIDC configuration for a tenant.
func (s *Service) DeleteOIDCConfig(ctx context.Context, tenantID string) error {
	if s.oidcConfigs == nil {
		return errors.New("OIDC config store not configured")
	}

	// Get existing to verify it exists and for audit log
	existing, err := s.oidcConfigs.Get(ctx, tenantID)
	if err != nil {
		return err
	}

	// Delete from store
	if err := s.oidcConfigs.Delete(ctx, tenantID); err != nil {
		return fmt.Errorf("delete OIDC config: %w", err)
	}

	s.auditLog(ctx, tenantID, ActionDeleteOIDCConfig, Metadata{
		"issuer_url": existing.IssuerURL,
	})

	s.logger.Info().
		Str("tenant_id", tenantID).
		Msg("OIDC config deleted")

	s.emitEvent(eventbus.TenantConfigChanged)

	return nil
}

// GetChannelRules retrieves channel rules for a tenant.
func (s *Service) GetChannelRules(ctx context.Context, tenantID string) (*types.TenantChannelRules, error) {
	if s.channelRules == nil {
		return nil, errors.New("channel rules store not configured")
	}

	return s.channelRules.Get(ctx, tenantID)
}

// SetChannelRules creates or updates channel rules for a tenant.
func (s *Service) SetChannelRules(ctx context.Context, tenantID string, rules *types.ChannelRules) error {
	if s.channelRules == nil {
		return errors.New("channel rules store not configured")
	}

	// Verify tenant exists and is active
	tenant, err := s.tenants.Get(ctx, tenantID)
	if err != nil {
		return err
	}
	if tenant.Status != StatusActive {
		return fmt.Errorf("tenant is not active: %s", tenant.Status)
	}

	// Validate rules
	if err := rules.Validate(); err != nil {
		return fmt.Errorf("invalid channel rules: %w", err)
	}

	// Upsert in store
	if err := s.channelRules.Update(ctx, tenantID, rules); err != nil {
		return fmt.Errorf("set channel rules: %w", err)
	}

	s.auditLog(ctx, tenantID, ActionSetChannelRules, Metadata{
		"public_patterns": len(rules.Public),
		"group_mappings":  len(rules.GroupMappings),
	})

	s.logger.Info().
		Str("tenant_id", tenantID).
		Int("public_patterns", len(rules.Public)).
		Int("group_mappings", len(rules.GroupMappings)).
		Msg("Channel rules set")

	s.emitEvent(eventbus.TenantConfigChanged)

	return nil
}

// DeleteChannelRules deletes channel rules for a tenant.
func (s *Service) DeleteChannelRules(ctx context.Context, tenantID string) error {
	if s.channelRules == nil {
		return errors.New("channel rules store not configured")
	}

	// Verify rules exist (will return error if not found)
	if _, err := s.channelRules.Get(ctx, tenantID); err != nil {
		return err
	}

	// Delete from store
	if err := s.channelRules.Delete(ctx, tenantID); err != nil {
		return fmt.Errorf("delete channel rules: %w", err)
	}

	s.auditLog(ctx, tenantID, ActionDeleteChannelRules, nil)

	s.logger.Info().
		Str("tenant_id", tenantID).
		Msg("Channel rules deleted")

	s.emitEvent(eventbus.TenantConfigChanged)

	return nil
}
