package provisioning

import "context"

// NoopKafkaAdmin is a no-op implementation of KafkaAdmin for Phase 1.
// It records operations in memory but doesn't actually create Kafka resources.
// Replace with real implementation in Phase 2.
type NoopKafkaAdmin struct {
	topics map[string]bool
	acls   []ACLBinding
}

// NewNoopKafkaAdmin creates a new NoopKafkaAdmin.
func NewNoopKafkaAdmin() *NoopKafkaAdmin {
	return &NoopKafkaAdmin{
		topics: make(map[string]bool),
		acls:   []ACLBinding{},
	}
}

// CreateTopic records a topic creation (no-op).
func (n *NoopKafkaAdmin) CreateTopic(ctx context.Context, name string, partitions int, config map[string]string) error {
	n.topics[name] = true
	return nil
}

// DeleteTopic records a topic deletion (no-op).
func (n *NoopKafkaAdmin) DeleteTopic(ctx context.Context, name string) error {
	delete(n.topics, name)
	return nil
}

// TopicExists checks if a topic was recorded (no-op).
func (n *NoopKafkaAdmin) TopicExists(ctx context.Context, name string) (bool, error) {
	_, ok := n.topics[name]
	return ok, nil
}

// SetTopicConfig is a no-op.
func (n *NoopKafkaAdmin) SetTopicConfig(ctx context.Context, name string, config map[string]string) error {
	return nil
}

// CreateACL records an ACL creation (no-op).
func (n *NoopKafkaAdmin) CreateACL(ctx context.Context, acl ACLBinding) error {
	n.acls = append(n.acls, acl)
	return nil
}

// DeleteACL is a no-op.
func (n *NoopKafkaAdmin) DeleteACL(ctx context.Context, acl ACLBinding) error {
	return nil
}

// SetQuota is a no-op.
func (n *NoopKafkaAdmin) SetQuota(ctx context.Context, tenantID string, quota QuotaConfig) error {
	return nil
}

// Ensure NoopKafkaAdmin implements KafkaAdmin.
var _ KafkaAdmin = (*NoopKafkaAdmin)(nil)
