package testutil

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/klurvio/sukko/internal/provisioning"
	"github.com/klurvio/sukko/internal/shared/types"
)

// MockTenantStore is an in-memory mock implementation of TenantStore.
type MockTenantStore struct {
	mu      sync.RWMutex
	tenants map[string]*provisioning.Tenant

	// Error injection for testing error paths
	CreateErr           error
	GetErr              error
	UpdateErr           error
	UpdateStatusErr     error
	SetDeprovisionAtErr error
	PingErr             error
}

// NewMockTenantStore creates a new MockTenantStore.
func NewMockTenantStore() *MockTenantStore {
	return &MockTenantStore{
		tenants: make(map[string]*provisioning.Tenant),
	}
}

// Ping implements TenantStore.Ping for testing.
func (m *MockTenantStore) Ping(_ context.Context) error {
	return m.PingErr
}

// Create implements TenantStore.Create for testing.
func (m *MockTenantStore) Create(_ context.Context, tenant *provisioning.Tenant) error {
	if m.CreateErr != nil {
		return m.CreateErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.tenants[tenant.ID]; exists {
		return errors.New("tenant already exists")
	}
	t := *tenant
	t.CreatedAt = time.Now()
	t.UpdatedAt = time.Now()
	m.tenants[tenant.ID] = &t
	return nil
}

// Get implements TenantStore.Get for testing.
func (m *MockTenantStore) Get(_ context.Context, tenantID string) (*provisioning.Tenant, error) {
	if m.GetErr != nil {
		return nil, m.GetErr
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tenants[tenantID]
	if !ok {
		return nil, errors.New("tenant not found")
	}
	return t, nil
}

// Update implements TenantStore.Update for testing.
func (m *MockTenantStore) Update(_ context.Context, tenant *provisioning.Tenant) error {
	if m.UpdateErr != nil {
		return m.UpdateErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.tenants[tenant.ID]; !exists {
		return errors.New("tenant not found")
	}
	tenant.UpdatedAt = time.Now()
	m.tenants[tenant.ID] = tenant
	return nil
}

// List implements TenantStore.List for testing.
func (m *MockTenantStore) List(_ context.Context, opts provisioning.ListOptions) ([]*provisioning.Tenant, int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*provisioning.Tenant, 0, len(m.tenants))
	for _, t := range m.tenants {
		if opts.Status != nil && t.Status != *opts.Status {
			continue
		}
		result = append(result, t)
	}
	total := len(result)
	if opts.Offset >= len(result) {
		return []*provisioning.Tenant{}, total, nil
	}
	end := min(opts.Offset+opts.Limit, len(result))
	return result[opts.Offset:end], total, nil
}

// UpdateStatus implements TenantStore.UpdateStatus for testing.
func (m *MockTenantStore) UpdateStatus(_ context.Context, tenantID string, status provisioning.TenantStatus) error {
	if m.UpdateStatusErr != nil {
		return m.UpdateStatusErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tenants[tenantID]
	if !ok {
		return errors.New("tenant not found")
	}
	t.Status = status
	t.UpdatedAt = time.Now()
	if status == provisioning.StatusSuspended {
		now := time.Now()
		t.SuspendedAt = &now
	}
	return nil
}

// SetDeprovisionAt implements TenantStore.SetDeprovisionAt for testing.
func (m *MockTenantStore) SetDeprovisionAt(_ context.Context, tenantID string, deprovisionAt *provisioning.Time) error {
	if m.SetDeprovisionAtErr != nil {
		return m.SetDeprovisionAtErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tenants[tenantID]
	if !ok {
		return errors.New("tenant not found")
	}
	if deprovisionAt != nil {
		tt := *deprovisionAt
		t.DeprovisionAt = &tt
	}
	return nil
}

// GetTenantsForDeletion implements TenantStore.GetTenantsForDeletion for testing.
func (m *MockTenantStore) GetTenantsForDeletion(_ context.Context) ([]*provisioning.Tenant, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*provisioning.Tenant
	now := time.Now()
	for _, t := range m.tenants {
		if t.Status == provisioning.StatusDeprovisioning && t.DeprovisionAt != nil && t.DeprovisionAt.Before(now) {
			result = append(result, t)
		}
	}
	return result, nil
}

// MockKeyStore is an in-memory mock implementation of KeyStore.
type MockKeyStore struct {
	mu   sync.RWMutex
	keys map[string]*provisioning.TenantKey

	CreateErr             error
	GetErr                error
	RevokeErr             error
	RevokeAllForTenantErr error
}

// NewMockKeyStore creates a new MockKeyStore.
func NewMockKeyStore() *MockKeyStore {
	return &MockKeyStore{
		keys: make(map[string]*provisioning.TenantKey),
	}
}

// Create implements KeyStore.Create for testing.
func (m *MockKeyStore) Create(_ context.Context, key *provisioning.TenantKey) error {
	if m.CreateErr != nil {
		return m.CreateErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.keys[key.KeyID]; exists {
		return errors.New("key already exists")
	}
	k := *key
	k.CreatedAt = time.Now()
	k.IsActive = true
	m.keys[key.KeyID] = &k
	return nil
}

// Get implements KeyStore.Get for testing.
func (m *MockKeyStore) Get(_ context.Context, keyID string) (*provisioning.TenantKey, error) {
	if m.GetErr != nil {
		return nil, m.GetErr
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	k, ok := m.keys[keyID]
	if !ok {
		return nil, errors.New("key not found")
	}
	return k, nil
}

// ListByTenant implements KeyStore.ListByTenant for testing.
func (m *MockKeyStore) ListByTenant(_ context.Context, tenantID string) ([]*provisioning.TenantKey, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*provisioning.TenantKey
	for _, k := range m.keys {
		if k.TenantID == tenantID {
			result = append(result, k)
		}
	}
	return result, nil
}

// Revoke implements KeyStore.Revoke for testing.
func (m *MockKeyStore) Revoke(_ context.Context, keyID string) error {
	if m.RevokeErr != nil {
		return m.RevokeErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.keys[keyID]
	if !ok {
		return errors.New("key not found")
	}
	now := time.Now()
	k.IsActive = false
	k.RevokedAt = &now
	return nil
}

// RevokeAllForTenant implements KeyStore.RevokeAllForTenant for testing.
func (m *MockKeyStore) RevokeAllForTenant(_ context.Context, tenantID string) error {
	if m.RevokeAllForTenantErr != nil {
		return m.RevokeAllForTenantErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for _, k := range m.keys {
		if k.TenantID == tenantID && k.IsActive {
			k.IsActive = false
			k.RevokedAt = &now
		}
	}
	return nil
}

// GetActiveKeys implements KeyStore.GetActiveKeys for testing.
func (m *MockKeyStore) GetActiveKeys(_ context.Context) ([]*provisioning.TenantKey, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*provisioning.TenantKey
	now := time.Now()
	for _, k := range m.keys {
		if k.IsActive && (k.ExpiresAt == nil || k.ExpiresAt.After(now)) && k.RevokedAt == nil {
			result = append(result, k)
		}
	}
	return result, nil
}

// MockRoutingRulesStore is an in-memory mock implementation of RoutingRulesStore.
type MockRoutingRulesStore struct {
	mu    sync.RWMutex
	rules map[string][]provisioning.TopicRoutingRule // key: tenantID

	GetErr    error
	SetErr    error
	DeleteErr error
}

// NewMockRoutingRulesStore creates a new MockRoutingRulesStore.
func NewMockRoutingRulesStore() *MockRoutingRulesStore {
	return &MockRoutingRulesStore{
		rules: make(map[string][]provisioning.TopicRoutingRule),
	}
}

// Get implements RoutingRulesStore.Get for testing.
func (m *MockRoutingRulesStore) Get(_ context.Context, tenantID string) ([]provisioning.TopicRoutingRule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.GetErr != nil {
		return nil, m.GetErr
	}
	rules, ok := m.rules[tenantID]
	if !ok {
		return nil, provisioning.ErrRoutingRulesNotFound
	}
	return rules, nil
}

// Set implements RoutingRulesStore.Set for testing.
func (m *MockRoutingRulesStore) Set(_ context.Context, tenantID string, rules []provisioning.TopicRoutingRule) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.SetErr != nil {
		return m.SetErr
	}
	m.rules[tenantID] = rules
	return nil
}

// Delete implements RoutingRulesStore.Delete for testing.
func (m *MockRoutingRulesStore) Delete(_ context.Context, tenantID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.DeleteErr != nil {
		return m.DeleteErr
	}
	if _, ok := m.rules[tenantID]; !ok {
		return provisioning.ErrRoutingRulesNotFound
	}
	delete(m.rules, tenantID)
	return nil
}

// ListAll implements RoutingRulesStore.ListAll for testing.
func (m *MockRoutingRulesStore) ListAll(_ context.Context) (map[string][]provisioning.TopicRoutingRule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string][]provisioning.TopicRoutingRule, len(m.rules))
	for k, v := range m.rules {
		copied := make([]provisioning.TopicRoutingRule, len(v))
		copy(copied, v)
		result[k] = copied
	}
	return result, nil
}

// MockQuotaStore is an in-memory mock implementation of QuotaStore.
type MockQuotaStore struct {
	mu     sync.RWMutex
	quotas map[string]*provisioning.TenantQuota

	CreateErr error
	GetErr    error
	UpdateErr error
}

// NewMockQuotaStore creates a new MockQuotaStore.
func NewMockQuotaStore() *MockQuotaStore {
	return &MockQuotaStore{
		quotas: make(map[string]*provisioning.TenantQuota),
	}
}

// Create implements QuotaStore.Create for testing.
func (m *MockQuotaStore) Create(_ context.Context, quota *provisioning.TenantQuota) error {
	if m.CreateErr != nil {
		return m.CreateErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	q := *quota
	q.UpdatedAt = time.Now()
	m.quotas[quota.TenantID] = &q
	return nil
}

// Get implements QuotaStore.Get for testing.
func (m *MockQuotaStore) Get(_ context.Context, tenantID string) (*provisioning.TenantQuota, error) {
	if m.GetErr != nil {
		return nil, m.GetErr
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	q, ok := m.quotas[tenantID]
	if !ok {
		return nil, errors.New("quota not found")
	}
	return q, nil
}

// Update implements QuotaStore.Update for testing.
func (m *MockQuotaStore) Update(_ context.Context, quota *provisioning.TenantQuota) error {
	if m.UpdateErr != nil {
		return m.UpdateErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	quota.UpdatedAt = time.Now()
	m.quotas[quota.TenantID] = quota
	return nil
}

// MockAuditStore is an in-memory mock implementation of AuditStore.
type MockAuditStore struct {
	mu      sync.RWMutex
	entries []*provisioning.AuditEntry

	LogErr error
}

// NewMockAuditStore creates a new MockAuditStore.
func NewMockAuditStore() *MockAuditStore {
	return &MockAuditStore{
		entries: []*provisioning.AuditEntry{},
	}
}

// Log implements AuditStore.Log for testing.
func (m *MockAuditStore) Log(_ context.Context, entry *provisioning.AuditEntry) error {
	if m.LogErr != nil {
		return m.LogErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e := *entry
	e.ID = int64(len(m.entries) + 1)
	e.CreatedAt = time.Now()
	m.entries = append(m.entries, &e)
	return nil
}

// ListByTenant implements AuditStore.ListByTenant for testing.
func (m *MockAuditStore) ListByTenant(_ context.Context, tenantID string, opts provisioning.ListOptions) ([]*provisioning.AuditEntry, int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*provisioning.AuditEntry
	for _, e := range m.entries {
		if e.TenantID == tenantID {
			result = append(result, e)
		}
	}
	total := len(result)
	if opts.Offset >= len(result) {
		return []*provisioning.AuditEntry{}, total, nil
	}
	end := min(opts.Offset+opts.Limit, len(result))
	return result[opts.Offset:end], total, nil
}

// GetEntries returns all recorded audit entries (for test assertions).
func (m *MockAuditStore) GetEntries() []*provisioning.AuditEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.entries
}

// MockKafkaAdmin is a mock implementation of KafkaAdmin.
type MockKafkaAdmin struct {
	mu     sync.RWMutex
	topics map[string]bool
	acls   []provisioning.ACLBinding
	quotas map[string]provisioning.QuotaConfig

	CreateTopicErr    error
	DeleteTopicErr    error
	SetTopicConfigErr error
	CreateACLErr      error
	SetQuotaErr       error
}

// NewMockKafkaAdmin creates a new MockKafkaAdmin.
func NewMockKafkaAdmin() *MockKafkaAdmin {
	return &MockKafkaAdmin{
		topics: make(map[string]bool),
		acls:   []provisioning.ACLBinding{},
		quotas: make(map[string]provisioning.QuotaConfig),
	}
}

// CreateTopic implements KafkaAdmin.CreateTopic for testing.
func (m *MockKafkaAdmin) CreateTopic(_ context.Context, name string, _ int, _ map[string]string) error {
	if m.CreateTopicErr != nil {
		return m.CreateTopicErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.topics[name] = true
	return nil
}

// DeleteTopic implements KafkaAdmin.DeleteTopic for testing.
func (m *MockKafkaAdmin) DeleteTopic(_ context.Context, name string) error {
	if m.DeleteTopicErr != nil {
		return m.DeleteTopicErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.topics, name)
	return nil
}

// TopicExists implements KafkaAdmin.TopicExists for testing.
func (m *MockKafkaAdmin) TopicExists(_ context.Context, name string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.topics[name]
	return ok, nil
}

// SetTopicConfig implements KafkaAdmin.SetTopicConfig for testing.
func (m *MockKafkaAdmin) SetTopicConfig(_ context.Context, _ string, _ map[string]string) error {
	if m.SetTopicConfigErr != nil {
		return m.SetTopicConfigErr
	}
	return nil
}

// CreateACL implements KafkaAdmin.CreateACL for testing.
func (m *MockKafkaAdmin) CreateACL(_ context.Context, acl provisioning.ACLBinding) error {
	if m.CreateACLErr != nil {
		return m.CreateACLErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.acls = append(m.acls, acl)
	return nil
}

// DeleteACL implements KafkaAdmin.DeleteACL for testing.
func (m *MockKafkaAdmin) DeleteACL(_ context.Context, _ provisioning.ACLBinding) error {
	return nil
}

// SetQuota implements KafkaAdmin.SetQuota for testing.
func (m *MockKafkaAdmin) SetQuota(_ context.Context, tenantID string, quota provisioning.QuotaConfig) error {
	if m.SetQuotaErr != nil {
		return m.SetQuotaErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.quotas[tenantID] = quota
	return nil
}

// GetTopics returns all created topics (for test assertions).
func (m *MockKafkaAdmin) GetTopics() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]string, 0, len(m.topics))
	for name := range m.topics {
		result = append(result, name)
	}
	return result
}

// GetACLs returns all created ACLs (for test assertions).
func (m *MockKafkaAdmin) GetACLs() []provisioning.ACLBinding {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.acls
}

// MockChannelRulesStore is an in-memory mock implementation of ChannelRulesStore.
type MockChannelRulesStore struct {
	mu    sync.RWMutex
	rules map[string]*types.TenantChannelRules

	CreateErr error
	GetErr    error
	UpdateErr error
	DeleteErr error
}

// NewMockChannelRulesStore creates a new MockChannelRulesStore.
func NewMockChannelRulesStore() *MockChannelRulesStore {
	return &MockChannelRulesStore{rules: make(map[string]*types.TenantChannelRules)}
}

// Create stores channel rules for a tenant.
func (m *MockChannelRulesStore) Create(_ context.Context, tenantID string, rules *types.ChannelRules) error {
	if m.CreateErr != nil {
		return m.CreateErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rules[tenantID] = &types.TenantChannelRules{
		TenantID:  tenantID,
		Rules:     *rules,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	return nil
}

// Get returns channel rules for a tenant.
func (m *MockChannelRulesStore) Get(_ context.Context, tenantID string) (*types.TenantChannelRules, error) {
	if m.GetErr != nil {
		return nil, m.GetErr
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.rules[tenantID]
	if !ok {
		return nil, types.ErrChannelRulesNotFound
	}
	return r, nil
}

// GetRules returns just the rules portion for a tenant.
func (m *MockChannelRulesStore) GetRules(_ context.Context, tenantID string) (*types.ChannelRules, error) {
	if m.GetErr != nil {
		return nil, m.GetErr
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.rules[tenantID]
	if !ok {
		return nil, types.ErrChannelRulesNotFound
	}
	return &r.Rules, nil
}

// Update replaces channel rules for a tenant.
func (m *MockChannelRulesStore) Update(_ context.Context, tenantID string, rules *types.ChannelRules) error {
	if m.UpdateErr != nil {
		return m.UpdateErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	existing, ok := m.rules[tenantID]
	if ok {
		existing.Rules = *rules
		existing.UpdatedAt = now
	} else {
		m.rules[tenantID] = &types.TenantChannelRules{
			TenantID:  tenantID,
			Rules:     *rules,
			CreatedAt: now,
			UpdatedAt: now,
		}
	}
	return nil
}

// Delete removes channel rules for a tenant.
func (m *MockChannelRulesStore) Delete(_ context.Context, tenantID string) error {
	if m.DeleteErr != nil {
		return m.DeleteErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rules[tenantID]; !ok {
		return types.ErrChannelRulesNotFound
	}
	delete(m.rules, tenantID)
	return nil
}

// List returns all stored channel rules.
func (m *MockChannelRulesStore) List(_ context.Context) ([]*types.TenantChannelRules, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*types.TenantChannelRules, 0, len(m.rules))
	for _, r := range m.rules {
		result = append(result, r)
	}
	return result, nil
}
