package configstore

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/Toniq-Labs/odin-ws/internal/provisioning"
	"github.com/Toniq-Labs/odin-ws/internal/shared/types"
)

// ErrReadOnlyMode is returned by write operations when running in config file mode.
var ErrReadOnlyMode = errors.New("write operations are not available in config file mode")

// ConfigStores holds all in-memory data backed by a parsed config file.
// Access individual stores via the typed accessor methods.
type ConfigStores struct {
	mu           sync.RWMutex
	tenants      map[string]*provisioning.Tenant
	keys         map[string]*provisioning.TenantKey
	keysByTenant map[string][]*provisioning.TenantKey
	topics       map[string][]*provisioning.TenantTopic
	quotas       map[string]*provisioning.TenantQuota
	oidcConfigs  map[string]*types.TenantOIDCConfig
	issuerIndex  map[string]*types.TenantOIDCConfig // issuer URL → config
	channelRules map[string]*types.TenantChannelRules
	logger       zerolog.Logger
}

// BuildStores creates ConfigStores from a parsed and validated config file.
func BuildStores(cfg *ConfigFile, logger zerolog.Logger) *ConfigStores {
	now := time.Now()

	s := &ConfigStores{
		tenants:      make(map[string]*provisioning.Tenant, len(cfg.Tenants)),
		keys:         make(map[string]*provisioning.TenantKey),
		keysByTenant: make(map[string][]*provisioning.TenantKey),
		topics:       make(map[string][]*provisioning.TenantTopic),
		quotas:       make(map[string]*provisioning.TenantQuota),
		oidcConfigs:  make(map[string]*types.TenantOIDCConfig),
		issuerIndex:  make(map[string]*types.TenantOIDCConfig),
		channelRules: make(map[string]*types.TenantChannelRules),
		logger:       logger,
	}

	for _, tc := range cfg.Tenants {
		consumerType := provisioning.ConsumerShared
		if tc.ConsumerType == "dedicated" {
			consumerType = provisioning.ConsumerDedicated
		}

		s.tenants[tc.ID] = &provisioning.Tenant{
			ID:           tc.ID,
			Name:         tc.Name,
			Status:       provisioning.StatusActive,
			ConsumerType: consumerType,
			Metadata:     provisioning.Metadata(tc.Metadata),
			CreatedAt:    now,
			UpdatedAt:    now,
		}

		// Keys
		var tenantKeys []*provisioning.TenantKey
		for _, kc := range tc.Keys {
			active := true
			if kc.Active != nil {
				active = *kc.Active
			}
			key := &provisioning.TenantKey{
				KeyID:     kc.ID,
				TenantID:  tc.ID,
				Algorithm: provisioning.Algorithm(kc.Algorithm),
				PublicKey: kc.PublicKey,
				IsActive:  active,
				CreatedAt: now,
			}
			if kc.ExpiresAtUnix != nil && *kc.ExpiresAtUnix > 0 {
				t := time.Unix(*kc.ExpiresAtUnix, 0)
				key.ExpiresAt = &t
			}
			s.keys[kc.ID] = key
			tenantKeys = append(tenantKeys, key)
		}
		s.keysByTenant[tc.ID] = tenantKeys

		// Categories → topics
		var tenantTopics []*provisioning.TenantTopic
		for _, cat := range tc.Categories {
			tenantTopics = append(tenantTopics, &provisioning.TenantTopic{
				TenantID:    tc.ID,
				Category:    cat.Name,
				Partitions:  cat.Partitions,
				RetentionMs: cat.RetentionMs,
				CreatedAt:   now,
			})
		}
		s.topics[tc.ID] = tenantTopics

		// Quotas
		if tc.Quotas != nil {
			s.quotas[tc.ID] = &provisioning.TenantQuota{
				TenantID:         tc.ID,
				MaxTopics:        tc.Quotas.MaxTopics,
				MaxPartitions:    tc.Quotas.MaxPartitions,
				MaxStorageBytes:  tc.Quotas.MaxStorageBytes,
				ProducerByteRate: tc.Quotas.ProducerByteRate,
				ConsumerByteRate: tc.Quotas.ConsumerByteRate,
				MaxConnections:   tc.Quotas.MaxConnections,
				UpdatedAt:        now,
			}
		}

		// OIDC config
		if tc.OIDC != nil {
			enabled := true
			if tc.OIDC.Enabled != nil {
				enabled = *tc.OIDC.Enabled
			}
			oidcCfg := &types.TenantOIDCConfig{
				TenantID:  tc.ID,
				IssuerURL: tc.OIDC.IssuerURL,
				JWKSURL:   tc.OIDC.JWKSURL,
				Audience:  tc.OIDC.Audience,
				Enabled:   enabled,
				CreatedAt: now,
				UpdatedAt: now,
			}
			s.oidcConfigs[tc.ID] = oidcCfg
			if enabled {
				s.issuerIndex[tc.OIDC.IssuerURL] = oidcCfg
			}
		}

		// Channel rules
		if tc.ChannelRules != nil {
			s.channelRules[tc.ID] = &types.TenantChannelRules{
				TenantID: tc.ID,
				Rules: types.ChannelRules{
					Public:        tc.ChannelRules.PublicChannels,
					GroupMappings: tc.ChannelRules.GroupMappings,
					Default:       tc.ChannelRules.DefaultChannels,
				},
				CreatedAt: now,
				UpdatedAt: now,
			}
		}
	}

	return s
}

// TenantStore returns a provisioning.TenantStore backed by the config.
func (s *ConfigStores) TenantStore() provisioning.TenantStore { return &tenantStore{s} }

// KeyStore returns a provisioning.KeyStore backed by the config.
func (s *ConfigStores) KeyStore() provisioning.KeyStore { return &keyStore{s} }

// TopicStore returns a provisioning.TopicStore backed by the config.
func (s *ConfigStores) TopicStore() provisioning.TopicStore { return &topicStore{s} }

// QuotaStore returns a provisioning.QuotaStore backed by the config.
func (s *ConfigStores) QuotaStore() provisioning.QuotaStore { return &quotaStore{s} }

// AuditStore returns a provisioning.AuditStore backed by the config.
func (s *ConfigStores) AuditStore() provisioning.AuditStore { return &auditStore{s} }

// OIDCConfigStore returns a provisioning.OIDCConfigStore backed by the config.
func (s *ConfigStores) OIDCConfigStore() provisioning.OIDCConfigStore { return &oidcConfigStore{s} }

// ChannelRulesStore returns a provisioning.ChannelRulesStore backed by the config.
func (s *ConfigStores) ChannelRulesStore() provisioning.ChannelRulesStore {
	return &channelRulesStore{s}
}

// ---------- TenantStore ----------

type tenantStore struct{ s *ConfigStores }

var _ provisioning.TenantStore = (*tenantStore)(nil)

func (t *tenantStore) Ping(_ context.Context) error { return nil }

func (t *tenantStore) Create(_ context.Context, _ *provisioning.Tenant) error {
	return ErrReadOnlyMode
}

func (t *tenantStore) Get(_ context.Context, tenantID string) (*provisioning.Tenant, error) {
	t.s.mu.RLock()
	defer t.s.mu.RUnlock()
	tenant, ok := t.s.tenants[tenantID]
	if !ok {
		return nil, errors.New("tenant not found")
	}
	return tenant, nil
}

func (t *tenantStore) Update(_ context.Context, _ *provisioning.Tenant) error {
	return ErrReadOnlyMode
}

func (t *tenantStore) List(_ context.Context, opts provisioning.ListOptions) ([]*provisioning.Tenant, int, error) {
	t.s.mu.RLock()
	defer t.s.mu.RUnlock()

	var all []*provisioning.Tenant
	for _, tenant := range t.s.tenants {
		if opts.Status != nil && tenant.Status != *opts.Status {
			continue
		}
		all = append(all, tenant)
	}

	total := len(all)
	start := opts.Offset
	if start > total {
		start = total
	}
	end := total
	if opts.Limit > 0 && start+opts.Limit < end {
		end = start + opts.Limit
	}

	return all[start:end], total, nil
}

func (t *tenantStore) UpdateStatus(_ context.Context, _ string, _ provisioning.TenantStatus) error {
	return ErrReadOnlyMode
}

func (t *tenantStore) SetDeprovisionAt(_ context.Context, _ string, _ *provisioning.Time) error {
	return ErrReadOnlyMode
}

func (t *tenantStore) GetTenantsForDeletion(_ context.Context) ([]*provisioning.Tenant, error) {
	return nil, nil
}

// ---------- KeyStore ----------

type keyStore struct{ s *ConfigStores }

var _ provisioning.KeyStore = (*keyStore)(nil)

func (k *keyStore) Create(_ context.Context, _ *provisioning.TenantKey) error {
	return ErrReadOnlyMode
}

func (k *keyStore) Get(_ context.Context, keyID string) (*provisioning.TenantKey, error) {
	k.s.mu.RLock()
	defer k.s.mu.RUnlock()
	key, ok := k.s.keys[keyID]
	if !ok {
		return nil, errors.New("key not found")
	}
	return key, nil
}

func (k *keyStore) ListByTenant(_ context.Context, tenantID string) ([]*provisioning.TenantKey, error) {
	k.s.mu.RLock()
	defer k.s.mu.RUnlock()
	return k.s.keysByTenant[tenantID], nil
}

func (k *keyStore) Revoke(_ context.Context, _ string) error {
	return ErrReadOnlyMode
}

func (k *keyStore) RevokeAllForTenant(_ context.Context, _ string) error {
	return ErrReadOnlyMode
}

func (k *keyStore) GetActiveKeys(_ context.Context) ([]*provisioning.TenantKey, error) {
	k.s.mu.RLock()
	defer k.s.mu.RUnlock()

	now := time.Now()
	var active []*provisioning.TenantKey
	for _, key := range k.s.keys {
		if !key.IsActive || key.RevokedAt != nil {
			continue
		}
		if key.ExpiresAt != nil && key.ExpiresAt.Before(now) {
			continue
		}
		active = append(active, key)
	}
	return active, nil
}

// ---------- TopicStore ----------

type topicStore struct{ s *ConfigStores }

var _ provisioning.TopicStore = (*topicStore)(nil)

func (ts *topicStore) Create(_ context.Context, _ *provisioning.TenantTopic) error {
	return ErrReadOnlyMode
}

func (ts *topicStore) ListByTenant(_ context.Context, tenantID string) ([]*provisioning.TenantTopic, error) {
	ts.s.mu.RLock()
	defer ts.s.mu.RUnlock()
	return ts.s.topics[tenantID], nil
}

func (ts *topicStore) MarkDeleted(_ context.Context, _, _ string) error {
	return ErrReadOnlyMode
}

func (ts *topicStore) CountByTenant(_ context.Context, tenantID string) (int, error) {
	ts.s.mu.RLock()
	defer ts.s.mu.RUnlock()
	return len(ts.s.topics[tenantID]), nil
}

func (ts *topicStore) CountPartitionsByTenant(_ context.Context, tenantID string) (int, error) {
	ts.s.mu.RLock()
	defer ts.s.mu.RUnlock()
	total := 0
	for _, topic := range ts.s.topics[tenantID] {
		total += topic.Partitions
	}
	return total, nil
}

// ---------- QuotaStore ----------

type quotaStore struct{ s *ConfigStores }

var _ provisioning.QuotaStore = (*quotaStore)(nil)

func (q *quotaStore) Get(_ context.Context, tenantID string) (*provisioning.TenantQuota, error) {
	q.s.mu.RLock()
	defer q.s.mu.RUnlock()
	quota, ok := q.s.quotas[tenantID]
	if !ok {
		return nil, errors.New("quota not found")
	}
	return quota, nil
}

func (q *quotaStore) Create(_ context.Context, _ *provisioning.TenantQuota) error {
	return ErrReadOnlyMode
}

func (q *quotaStore) Update(_ context.Context, _ *provisioning.TenantQuota) error {
	return ErrReadOnlyMode
}

// ---------- AuditStore ----------

type auditStore struct{ s *ConfigStores }

var _ provisioning.AuditStore = (*auditStore)(nil)

func (a *auditStore) Log(_ context.Context, entry *provisioning.AuditEntry) error {
	a.s.logger.Debug().
		Str("tenant_id", entry.TenantID).
		Str("action", entry.Action).
		Str("actor", entry.Actor).
		Msg("audit entry (config mode, not persisted)")
	return nil
}

func (a *auditStore) ListByTenant(_ context.Context, _ string, _ provisioning.ListOptions) ([]*provisioning.AuditEntry, int, error) {
	return nil, 0, nil
}

// ---------- OIDCConfigStore ----------

type oidcConfigStore struct{ s *ConfigStores }

var _ provisioning.OIDCConfigStore = (*oidcConfigStore)(nil)

func (o *oidcConfigStore) Create(_ context.Context, _ *types.TenantOIDCConfig) error {
	return ErrReadOnlyMode
}

func (o *oidcConfigStore) Get(_ context.Context, tenantID string) (*types.TenantOIDCConfig, error) {
	o.s.mu.RLock()
	defer o.s.mu.RUnlock()
	c, ok := o.s.oidcConfigs[tenantID]
	if !ok {
		return nil, types.ErrOIDCNotConfigured
	}
	return c, nil
}

func (o *oidcConfigStore) GetByIssuer(_ context.Context, issuerURL string) (*types.TenantOIDCConfig, error) {
	o.s.mu.RLock()
	defer o.s.mu.RUnlock()
	c, ok := o.s.issuerIndex[issuerURL]
	if !ok {
		return nil, types.ErrIssuerNotFound
	}
	return c, nil
}

func (o *oidcConfigStore) Update(_ context.Context, _ *types.TenantOIDCConfig) error {
	return ErrReadOnlyMode
}

func (o *oidcConfigStore) Delete(_ context.Context, _ string) error {
	return ErrReadOnlyMode
}

func (o *oidcConfigStore) ListEnabled(_ context.Context) ([]*types.TenantOIDCConfig, error) {
	o.s.mu.RLock()
	defer o.s.mu.RUnlock()
	var result []*types.TenantOIDCConfig
	for _, c := range o.s.oidcConfigs {
		if c.Enabled {
			result = append(result, c)
		}
	}
	return result, nil
}

// ---------- ChannelRulesStore ----------

type channelRulesStore struct{ s *ConfigStores }

var _ provisioning.ChannelRulesStore = (*channelRulesStore)(nil)

func (cr *channelRulesStore) Create(_ context.Context, _ string, _ *types.ChannelRules) error {
	return ErrReadOnlyMode
}

func (cr *channelRulesStore) Get(_ context.Context, tenantID string) (*types.TenantChannelRules, error) {
	cr.s.mu.RLock()
	defer cr.s.mu.RUnlock()
	r, ok := cr.s.channelRules[tenantID]
	if !ok {
		return nil, types.ErrChannelRulesNotFound
	}
	return r, nil
}

func (cr *channelRulesStore) GetRules(_ context.Context, tenantID string) (*types.ChannelRules, error) {
	cr.s.mu.RLock()
	defer cr.s.mu.RUnlock()
	r, ok := cr.s.channelRules[tenantID]
	if !ok {
		return nil, types.ErrChannelRulesNotFound
	}
	return &r.Rules, nil
}

func (cr *channelRulesStore) Update(_ context.Context, _ string, _ *types.ChannelRules) error {
	return ErrReadOnlyMode
}

func (cr *channelRulesStore) Delete(_ context.Context, _ string) error {
	return ErrReadOnlyMode
}

func (cr *channelRulesStore) List(_ context.Context) ([]*types.TenantChannelRules, error) {
	cr.s.mu.RLock()
	defer cr.s.mu.RUnlock()
	var result []*types.TenantChannelRules
	for _, r := range cr.s.channelRules {
		result = append(result, r)
	}
	return result, nil
}
