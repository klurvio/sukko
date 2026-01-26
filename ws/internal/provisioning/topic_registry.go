package provisioning

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Toniq-Labs/odin-ws/internal/kafka"
)

// TopicRegistry implements kafka.TenantRegistry using PostgreSQL.
// It queries the provisioning database to provide tenant topic information
// for the multi-tenant consumer pool.
//
// Thread Safety: All methods are safe for concurrent use (uses database/sql).
type TopicRegistry struct {
	db *sql.DB
}

// NewTopicRegistry creates a new TopicRegistry backed by PostgreSQL.
func NewTopicRegistry(db *sql.DB) *TopicRegistry {
	return &TopicRegistry{db: db}
}

// GetSharedTenantTopics returns all topics for active shared-mode tenants.
// Topics are in the format: {namespace}.{tenant_id}.{category}
//
// Only returns topics for tenants where:
//   - status = 'active'
//   - consumer_type = 'shared'
//   - topic is not deleted
func (r *TopicRegistry) GetSharedTenantTopics(ctx context.Context, namespace string) ([]string, error) {
	query := `
		SELECT tt.topic_name
		FROM tenant_topics tt
		JOIN tenants t ON t.id = tt.tenant_id
		WHERE t.status = 'active'
		  AND t.consumer_type = 'shared'
		  AND tt.deleted_at IS NULL
		  AND tt.topic_name LIKE $1
		ORDER BY tt.topic_name
	`

	// Filter topics by namespace prefix (e.g., "main.%")
	namespacePrefix := namespace + ".%"

	rows, err := r.db.QueryContext(ctx, query, namespacePrefix)
	if err != nil {
		return nil, fmt.Errorf("query shared tenant topics: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var topics []string
	for rows.Next() {
		var topicName string
		if err := rows.Scan(&topicName); err != nil {
			return nil, fmt.Errorf("scan topic name: %w", err)
		}
		topics = append(topics, topicName)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate topics: %w", err)
	}

	return topics, nil
}

// GetDedicatedTenants returns tenants that require dedicated consumers.
// Each returned tenant includes their complete topic list.
//
// Only returns tenants where:
//   - status = 'active'
//   - consumer_type = 'dedicated'
func (r *TopicRegistry) GetDedicatedTenants(ctx context.Context, namespace string) ([]kafka.TenantTopics, error) {
	// First, get all dedicated tenants
	tenantsQuery := `
		SELECT id
		FROM tenants
		WHERE status = 'active'
		  AND consumer_type = 'dedicated'
		ORDER BY id
	`

	rows, err := r.db.QueryContext(ctx, tenantsQuery)
	if err != nil {
		return nil, fmt.Errorf("query dedicated tenants: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tenantIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan tenant id: %w", err)
		}
		tenantIDs = append(tenantIDs, id)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenants: %w", err)
	}

	// For each dedicated tenant, get their topics
	namespacePrefix := namespace + ".%"
	topicsQuery := `
		SELECT topic_name
		FROM tenant_topics
		WHERE tenant_id = $1
		  AND deleted_at IS NULL
		  AND topic_name LIKE $2
		ORDER BY topic_name
	`

	result := make([]kafka.TenantTopics, 0, len(tenantIDs))
	for _, tenantID := range tenantIDs {
		topics, err := r.getTopicsForTenant(ctx, topicsQuery, tenantID, namespacePrefix)
		if err != nil {
			return nil, err
		}

		// Only include tenants with topics
		if len(topics) > 0 {
			result = append(result, kafka.TenantTopics{
				TenantID: tenantID,
				Topics:   topics,
			})
		}
	}

	return result, nil
}

// getTopicsForTenant retrieves topics for a specific tenant.
func (r *TopicRegistry) getTopicsForTenant(ctx context.Context, query, tenantID, namespacePrefix string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, query, tenantID, namespacePrefix)
	if err != nil {
		return nil, fmt.Errorf("query topics for tenant %s: %w", tenantID, err)
	}
	defer func() { _ = rows.Close() }()

	var topics []string
	for rows.Next() {
		var topicName string
		if err := rows.Scan(&topicName); err != nil {
			return nil, fmt.Errorf("scan topic for tenant %s: %w", tenantID, err)
		}
		topics = append(topics, topicName)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate topics for tenant %s: %w", tenantID, err)
	}

	return topics, nil
}

// Ensure TopicRegistry implements kafka.TenantRegistry.
var _ kafka.TenantRegistry = (*TopicRegistry)(nil)
