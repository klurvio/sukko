package provisioning

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
)

// ServiceConfig holds the configuration for the provisioning service.
type ServiceConfig struct {
	TenantStore TenantStore
	KeyStore    KeyStore
	TopicStore  TopicStore
	QuotaStore  QuotaStore
	AuditStore  AuditStore
	KafkaAdmin  KafkaAdmin
	Logger      zerolog.Logger

	// Topic configuration
	TopicNamespace     string
	DefaultPartitions  int
	DefaultRetentionMs int64

	// Quota defaults
	MaxTopicsPerTenant int

	// Lifecycle
	DeprovisionGraceDays int
}

// Service provides tenant lifecycle management and provisioning operations.
type Service struct {
	tenants TenantStore
	keys    KeyStore
	topics  TopicStore
	quotas  QuotaStore
	audit   AuditStore
	kafka   KafkaAdmin
	logger  zerolog.Logger
	config  ServiceConfig
}

// NewService creates a new provisioning Service.
func NewService(cfg ServiceConfig) *Service {
	return &Service{
		tenants: cfg.TenantStore,
		keys:    cfg.KeyStore,
		topics:  cfg.TopicStore,
		quotas:  cfg.QuotaStore,
		audit:   cfg.AuditStore,
		kafka:   cfg.KafkaAdmin,
		logger:  cfg.Logger,
		config:  cfg,
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
		MaxStorageBytes:  10 * 1024 * 1024 * 1024, // 10GB
		ProducerByteRate: 10 * 1024 * 1024,        // 10MB/s
		ConsumerByteRate: 50 * 1024 * 1024,        // 50MB/s
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
				response.Topics = append(response.Topics, t.TopicName)
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
		return nil, fmt.Errorf("cannot update deleted tenant")
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

	return tenant, nil
}

// SuspendTenant temporarily disables a tenant.
func (s *Service) SuspendTenant(ctx context.Context, tenantID string) error {
	if err := s.tenants.UpdateStatus(ctx, tenantID, StatusSuspended); err != nil {
		return fmt.Errorf("suspend tenant: %w", err)
	}

	s.auditLog(ctx, tenantID, ActionSuspendTenant, nil)

	s.logger.Info().Str("tenant_id", tenantID).Msg("Tenant suspended")
	return nil
}

// ReactivateTenant reactivates a suspended tenant.
func (s *Service) ReactivateTenant(ctx context.Context, tenantID string) error {
	if err := s.tenants.UpdateStatus(ctx, tenantID, StatusActive); err != nil {
		return fmt.Errorf("reactivate tenant: %w", err)
	}

	s.auditLog(ctx, tenantID, ActionReactivateTenant, nil)

	s.logger.Info().Str("tenant_id", tenantID).Msg("Tenant reactivated")
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
		if err := s.kafka.SetTopicConfig(ctx, topic.TopicName, map[string]string{
			"retention.ms": fmt.Sprintf("%d", gracePeriodMs),
		}); err != nil {
			s.logger.Error().Err(err).
				Str("tenant_id", tenantID).
				Str("topic", topic.TopicName).
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
		return fmt.Errorf("key does not belong to tenant")
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

	return s.createTopicsForTenant(ctx, tenantID, categories)
}

// ListTopics returns all topics for a tenant.
func (s *Service) ListTopics(ctx context.Context, tenantID string) ([]*TenantTopic, error) {
	return s.topics.ListByTenant(ctx, tenantID)
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
		// Create base topic
		baseTopic := s.buildTopicName(tenantID, category)
		if err := s.createSingleTopic(ctx, tenantID, baseTopic, category); err != nil {
			s.logger.Error().Err(err).Str("topic", baseTopic).Msg("Failed to create topic")
			continue
		}
		created = append(created, &TenantTopic{
			TenantID:    tenantID,
			TopicName:   baseTopic,
			Category:    category,
			Partitions:  s.config.DefaultPartitions,
			RetentionMs: s.config.DefaultRetentionMs,
		})

		// Create refined topic
		refinedTopic := baseTopic + ".refined"
		if err := s.createSingleTopic(ctx, tenantID, refinedTopic, category+".refined"); err != nil {
			s.logger.Error().Err(err).Str("topic", refinedTopic).Msg("Failed to create refined topic")
			continue
		}
		created = append(created, &TenantTopic{
			TenantID:    tenantID,
			TopicName:   refinedTopic,
			Category:    category + ".refined",
			Partitions:  s.config.DefaultPartitions,
			RetentionMs: s.config.DefaultRetentionMs,
		})
	}

	// Create ACL for tenant
	if len(created) > 0 {
		acl := ACLBinding{
			Principal:    fmt.Sprintf("User:%s", tenantID),
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

// createSingleTopic creates a single Kafka topic and records it in the database.
func (s *Service) createSingleTopic(ctx context.Context, tenantID, topicName, category string) error {
	// Create in Kafka
	config := map[string]string{
		"retention.ms": fmt.Sprintf("%d", s.config.DefaultRetentionMs),
	}
	if err := s.kafka.CreateTopic(ctx, topicName, s.config.DefaultPartitions, config); err != nil {
		return fmt.Errorf("create kafka topic: %w", err)
	}

	// Record in database
	topic := &TenantTopic{
		TenantID:    tenantID,
		TopicName:   topicName,
		Category:    category,
		Partitions:  s.config.DefaultPartitions,
		RetentionMs: s.config.DefaultRetentionMs,
	}
	if err := s.topics.Create(ctx, topic); err != nil {
		return fmt.Errorf("record topic: %w", err)
	}

	return nil
}

// buildTopicName constructs a topic name from namespace, tenant, and category.
func (s *Service) buildTopicName(tenantID, category string) string {
	return fmt.Sprintf("%s.%s.%s", s.config.TopicNamespace, tenantID, category)
}

// auditLog records an audit entry.
func (s *Service) auditLog(ctx context.Context, tenantID, action string, details Metadata) {
	entry := &AuditEntry{
		TenantID:  tenantID,
		Action:    action,
		Actor:     getActorFromContext(ctx),
		ActorType: getActorTypeFromContext(ctx),
		IPAddress: getIPFromContext(ctx),
		Details:   details,
	}
	if err := s.audit.Log(ctx, entry); err != nil {
		s.logger.Error().Err(err).Str("action", action).Msg("Failed to write audit log")
	}
}

// Context keys for actor information.
type contextKey string

const (
	ctxKeyActor     contextKey = "actor"
	ctxKeyActorType contextKey = "actor_type"
	ctxKeyIP        contextKey = "ip_address"
)

// WithActor adds actor information to context.
func WithActor(ctx context.Context, actor, actorType, ip string) context.Context {
	ctx = context.WithValue(ctx, ctxKeyActor, actor)
	ctx = context.WithValue(ctx, ctxKeyActorType, actorType)
	ctx = context.WithValue(ctx, ctxKeyIP, ip)
	return ctx
}

func getActorFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyActor).(string); ok {
		return v
	}
	return "system"
}

func getActorTypeFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyActorType).(string); ok {
		return v
	}
	return ActorTypeSystem
}

func getIPFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyIP).(string); ok {
		return v
	}
	return ""
}
