package provisioning

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/provisioning/eventbus"
	"github.com/klurvio/sukko/internal/shared/auth"
	"github.com/klurvio/sukko/internal/shared/kafka"
	"github.com/klurvio/sukko/internal/shared/types"
)

// Sentinel errors for provisioning operations.
var (
	ErrTenantIDContainsDots      = errors.New("tenant ID must not contain dots")
	ErrTenantIDUnderscorePrefix  = errors.New("tenant ID must not start with underscore")
	ErrTenantDeleted             = errors.New("cannot modify deleted tenant")
	ErrTenantNotActive           = errors.New("tenant is not active")
	ErrTenantNotFound            = errors.New("tenant not found")
	ErrQuotaNotFound             = errors.New("quota not found")
	ErrKeyNotOwnedByTenant       = errors.New("key does not belong to tenant")
	ErrChannelRulesNotConfigured = errors.New("channel rules store not configured")
	ErrRoutingRulesNotConfigured = errors.New("routing rules store not configured")
)

const (
	// msPerDay is the number of milliseconds in one day.
	msPerDay = 24 * 60 * 60 * 1000

	// kafkaRetentionMsKey is the Kafka topic config key for message retention.
	kafkaRetentionMsKey = "retention.ms"
)

// ServiceConfig holds the configuration for the provisioning service.
type ServiceConfig struct {
	TenantStore       TenantStore
	KeyStore          KeyStore
	RoutingRulesStore RoutingRulesStore
	QuotaStore        QuotaStore
	AuditStore        AuditStore
	ChannelRulesStore ChannelRulesStore
	KafkaAdmin        KafkaAdmin
	EventBus          *eventbus.Bus
	Logger            zerolog.Logger

	// Topic configuration
	TopicNamespace     string
	DefaultPartitions  int
	DefaultRetentionMs int64

	// Quota defaults
	MaxTopicsPerTenant  int
	MaxStorageBytes     int64
	DefaultProducerRate int64
	DefaultConsumerRate int64

	// Lifecycle
	DeprovisionGraceDays int

	// MaxRoutingRules limits the number of routing rules per tenant.
	// Wired from env var MAX_ROUTING_RULES (default: 100).
	MaxRoutingRules int
}

// Service provides tenant lifecycle management and provisioning operations.
type Service struct {
	tenants      TenantStore
	keys         KeyStore
	routingRules RoutingRulesStore
	quotas       QuotaStore
	audit        AuditStore
	channelRules ChannelRulesStore
	kafka        KafkaAdmin
	eventBus     *eventbus.Bus
	logger       zerolog.Logger
	config       ServiceConfig
}

// NewService creates a new provisioning Service.
// Returns an error if required stores (TenantStore, KeyStore, QuotaStore,
// AuditStore, KafkaAdmin, EventBus) are nil.
func NewService(cfg ServiceConfig) (*Service, error) {
	if cfg.TenantStore == nil {
		return nil, errors.New("provisioning service: tenant store is required")
	}
	if cfg.KeyStore == nil {
		return nil, errors.New("provisioning service: key store is required")
	}
	if cfg.QuotaStore == nil {
		return nil, errors.New("provisioning service: quota store is required")
	}
	if cfg.AuditStore == nil {
		return nil, errors.New("provisioning service: audit store is required")
	}
	if cfg.KafkaAdmin == nil {
		return nil, errors.New("provisioning service: kafka admin is required")
	}
	if cfg.EventBus == nil {
		return nil, errors.New("provisioning service: event bus is required")
	}

	return &Service{
		tenants:      cfg.TenantStore,
		keys:         cfg.KeyStore,
		routingRules: cfg.RoutingRulesStore,
		quotas:       cfg.QuotaStore,
		audit:        cfg.AuditStore,
		channelRules: cfg.ChannelRulesStore,
		kafka:        cfg.KafkaAdmin,
		eventBus:     cfg.EventBus,
		logger:       cfg.Logger,
		config:       cfg,
	}, nil
}

// Ready checks if the service is ready to handle requests.
// Returns nil if database is accessible, otherwise returns the error.
func (s *Service) Ready(ctx context.Context) error {
	if err := s.tenants.Ping(ctx); err != nil {
		return fmt.Errorf("ping tenant store: %w", err)
	}
	return nil
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

	// FR-008: Tenant IDs must not contain dots (used as channel separator)
	if strings.Contains(req.TenantID, ".") {
		return nil, fmt.Errorf("%w: %s", ErrTenantIDContainsDots, req.TenantID)
	}

	// FR-009: Tenant IDs must not start with underscore (reserved for system)
	if strings.HasPrefix(req.TenantID, "_") {
		return nil, fmt.Errorf("%w: %s", ErrTenantIDUnderscorePrefix, req.TenantID)
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
		// Quotas are non-critical: tenant is usable without quotas (they default to unlimited).
		// Failing the entire CreateTenant for a quota write failure would be worse than
		// operating without quotas. The error is logged at Error level for operator visibility.
		s.logger.Error().Err(err).Str("tenant_id", tenant.ID).Msg("Failed to create quotas")
	}

	response := &CreateTenantResponse{
		Tenant: tenant,
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

	// Audit log
	s.auditLog(ctx, tenant.ID, ActionCreateTenant, Metadata{
		"name":          tenant.Name,
		"consumer_type": tenant.ConsumerType,
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
	tenant, err := s.tenants.Get(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("get tenant: %w", err)
	}
	return tenant, nil
}

// ListTenants returns tenants matching the given options.
func (s *Service) ListTenants(ctx context.Context, opts ListOptions) ([]*Tenant, int, error) {
	tenants, total, err := s.tenants.List(ctx, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("list tenants: %w", err)
	}
	return tenants, total, nil
}

// UpdateTenant updates tenant metadata.
func (s *Service) UpdateTenant(ctx context.Context, tenantID string, req UpdateTenantRequest) (*Tenant, error) {
	tenant, err := s.tenants.Get(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("get tenant: %w", err)
	}

	if tenant.Status == StatusDeleted {
		return nil, ErrTenantDeleted
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
	tenant, err := s.tenants.Get(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("get tenant: %w", err)
	}
	if tenant.Status == StatusDeleted {
		return ErrTenantDeleted
	}
	if tenant.Status != StatusActive {
		return fmt.Errorf("%w: %s", ErrTenantNotActive, tenant.Status)
	}

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
	tenant, err := s.tenants.Get(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("get tenant: %w", err)
	}
	if tenant.Status == StatusDeleted {
		return ErrTenantDeleted
	}
	if tenant.Status != StatusSuspended {
		return fmt.Errorf("%w: current status is %s", ErrTenantNotActive, tenant.Status)
	}

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
	// Validate tenant state before deprovisioning
	tenant, err := s.tenants.Get(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("get tenant: %w", err)
	}
	if tenant.Status == StatusDeleted {
		return ErrTenantDeleted
	}
	if tenant.Status == StatusDeprovisioning {
		return fmt.Errorf("%w: already deprovisioning", ErrTenantNotActive)
	}

	// Update status to deprovisioning
	if err := s.tenants.UpdateStatus(ctx, tenantID, StatusDeprovisioning); err != nil {
		return fmt.Errorf("set deprovisioning status: %w", err)
	}

	// Revoke all keys immediately
	if err := s.keys.RevokeAllForTenant(ctx, tenantID); err != nil {
		s.logger.Error().Err(err).Str("tenant_id", tenantID).Msg("Failed to revoke keys")
	} else {
		s.emitEvent(eventbus.KeysChanged)
	}

	// Set deprovision deadline
	gracePeriod := time.Duration(s.config.DeprovisionGraceDays) * 24 * time.Hour
	deprovisionAt := time.Now().Add(gracePeriod)
	if err := s.tenants.SetDeprovisionAt(ctx, tenantID, &deprovisionAt); err != nil {
		s.logger.Error().Err(err).Str("tenant_id", tenantID).Msg("Failed to set deprovision deadline")
	}

	// Update topic retention to grace period (Kafka will delete after)
	if s.routingRules != nil {
		rules, err := s.routingRules.Get(ctx, tenantID)
		if err != nil {
			s.logger.Warn().Err(err).Str("tenant_id", tenantID).
				Msg("Failed to get routing rules for topic retention update")
		} else {
			gracePeriodMs := int64(s.config.DeprovisionGraceDays) * msPerDay
			for _, suffix := range UniqueTopicSuffixes(rules) {
				topicName := kafka.BuildTopicName(s.config.TopicNamespace, tenantID, suffix)
				if err := s.kafka.SetTopicConfig(ctx, topicName, map[string]string{
					kafkaRetentionMsKey: strconv.FormatInt(gracePeriodMs, 10),
				}); err != nil {
					s.logger.Error().Err(err).
						Str("tenant_id", tenantID).
						Str("topic", topicName).
						Msg("Failed to update topic retention")
				}
			}
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

	s.emitEvent(eventbus.TopicsChanged)
	s.emitEvent(eventbus.TenantConfigChanged)

	return nil
}

// CreateKey registers a new public key for a tenant.
func (s *Service) CreateKey(ctx context.Context, tenantID string, req CreateKeyRequest) (*TenantKey, error) {
	// Verify tenant exists and is active
	tenant, err := s.tenants.Get(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("get tenant: %w", err)
	}
	if tenant.Status != StatusActive {
		return nil, fmt.Errorf("%w: %s", ErrTenantNotActive, tenant.Status)
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
	keys, err := s.keys.ListByTenant(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list keys: %w", err)
	}
	return keys, nil
}

// RevokeKey revokes a key.
func (s *Service) RevokeKey(ctx context.Context, tenantID, keyID string) error {
	// Verify key belongs to tenant
	key, err := s.keys.Get(ctx, keyID)
	if err != nil {
		return fmt.Errorf("get key: %w", err)
	}
	if key.TenantID != tenantID {
		return ErrKeyNotOwnedByTenant
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
	keys, err := s.keys.GetActiveKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("get active keys: %w", err)
	}
	return keys, nil
}

// TopicNamespace returns the configured Kafka topic namespace.
// Used to build full topic names: {namespace}.{tenantID}.{category}
func (s *Service) TopicNamespace() string {
	return s.config.TopicNamespace
}

// GetQuota returns quotas for a tenant.
func (s *Service) GetQuota(ctx context.Context, tenantID string) (*TenantQuota, error) {
	quota, err := s.quotas.Get(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("get quota: %w", err)
	}
	return quota, nil
}

// UpdateQuota updates quotas for a tenant.
func (s *Service) UpdateQuota(ctx context.Context, tenantID string, req UpdateQuotaRequest) (*TenantQuota, error) {
	quota, err := s.quotas.Get(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("get quota: %w", err)
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
	entries, total, err := s.audit.ListByTenant(ctx, tenantID, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("list audit log: %w", err)
	}
	return entries, total, nil
}

// auditLog records an audit entry.
func (s *Service) auditLog(ctx context.Context, tenantID, action string, details Metadata) {
	// Get actor type, defaulting to system if not set
	actorType := auth.GetActorType(ctx)
	if actorType == auth.DefaultActorType {
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
// EventBus is guaranteed non-nil by NewService constructor validation.
func (s *Service) emitEvent(eventType eventbus.EventType) {
	s.eventBus.Publish(eventbus.Event{Type: eventType})
}

// WithActor adds actor information to context.
// This is an alias for auth.WithActor for backwards compatibility.
var WithActor = auth.WithActor

// GetChannelRules retrieves channel rules for a tenant.
func (s *Service) GetChannelRules(ctx context.Context, tenantID string) (*types.TenantChannelRules, error) {
	if s.channelRules == nil {
		return nil, ErrChannelRulesNotConfigured
	}

	rules, err := s.channelRules.Get(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("get channel rules: %w", err)
	}
	return rules, nil
}

// SetChannelRules creates or updates channel rules for a tenant.
func (s *Service) SetChannelRules(ctx context.Context, tenantID string, rules *types.ChannelRules) error {
	if s.channelRules == nil {
		return ErrChannelRulesNotConfigured
	}

	// Verify tenant exists and is active
	tenant, err := s.tenants.Get(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("get tenant: %w", err)
	}
	if tenant.Status != StatusActive {
		return fmt.Errorf("%w: %s", ErrTenantNotActive, tenant.Status)
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
		return ErrChannelRulesNotConfigured
	}

	// Verify rules exist (will return error if not found)
	if _, err := s.channelRules.Get(ctx, tenantID); err != nil {
		return fmt.Errorf("get channel rules: %w", err)
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

// GetRoutingRules retrieves routing rules for a tenant.
func (s *Service) GetRoutingRules(ctx context.Context, tenantID string) ([]TopicRoutingRule, error) {
	if s.routingRules == nil {
		return nil, ErrRoutingRulesNotConfigured
	}

	rules, err := s.routingRules.Get(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("get routing rules: %w", err)
	}
	return rules, nil
}

// SetRoutingRules creates or updates routing rules for a tenant.
func (s *Service) SetRoutingRules(ctx context.Context, tenantID string, rules []TopicRoutingRule) error {
	if s.routingRules == nil {
		return ErrRoutingRulesNotConfigured
	}

	// Verify tenant exists and is active
	tenant, err := s.tenants.Get(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("get tenant: %w", err)
	}
	if tenant.Status != StatusActive {
		return fmt.Errorf("%w: %s", ErrTenantNotActive, tenant.Status)
	}

	// Enforce configurable count limit (default from env var MAX_ROUTING_RULES)
	maxRules := s.config.MaxRoutingRules
	if len(rules) > maxRules {
		return fmt.Errorf("%w: got %d, max %d", ErrTooManyRoutingRules, len(rules), maxRules)
	}

	// Validate rule structure (patterns, suffixes, no placeholders)
	if err := ValidateRoutingRules(rules); err != nil {
		return fmt.Errorf("invalid routing rules: %w", err)
	}

	// Upsert in store
	if err := s.routingRules.Set(ctx, tenantID, rules); err != nil {
		return fmt.Errorf("set routing rules: %w", err)
	}

	s.auditLog(ctx, tenantID, ActionSetRoutingRules, Metadata{
		"rule_count": len(rules),
	})

	s.logger.Info().
		Str("tenant_id", tenantID).
		Int("rule_count", len(rules)).
		Msg("Routing rules set")

	s.emitEvent(eventbus.TenantConfigChanged)
	s.emitEvent(eventbus.TopicsChanged)

	return nil
}

// DeleteRoutingRules deletes routing rules for a tenant.
func (s *Service) DeleteRoutingRules(ctx context.Context, tenantID string) error {
	if s.routingRules == nil {
		return ErrRoutingRulesNotConfigured
	}

	if err := s.routingRules.Delete(ctx, tenantID); err != nil {
		return fmt.Errorf("delete routing rules: %w", err)
	}

	s.auditLog(ctx, tenantID, ActionDeleteRoutingRules, nil)

	s.logger.Info().
		Str("tenant_id", tenantID).
		Msg("Routing rules deleted")

	s.emitEvent(eventbus.TenantConfigChanged)
	s.emitEvent(eventbus.TopicsChanged)

	return nil
}
