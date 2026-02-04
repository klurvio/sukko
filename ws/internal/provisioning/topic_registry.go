package provisioning

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Toniq-Labs/odin-ws/internal/shared/kafka"
)

// TopicRegistry implements kafka.TenantRegistry using PostgreSQL.
// It queries the provisioning database to provide tenant topic information
// for the multi-tenant consumer pool.
//
// Topic names are built at runtime using kafka.BuildTopicName() with the
// namespace parameter. The database stores only categories (without namespace),
// making KAFKA_TOPIC_NAMESPACE the single source of truth for namespace.
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
// Topics are built at runtime: {namespace}.{tenant_id}.{category}
//
// Only returns topics for tenants where:
//   - status = 'active'
//   - consumer_type = 'shared'
//   - category is not deleted
func (r *TopicRegistry) GetSharedTenantTopics(ctx context.Context, namespace string) ([]string, error) {
	query := `
		SELECT t.id, c.category
		FROM tenants t
		JOIN tenant_categories c ON c.tenant_id = t.id
		WHERE t.status = 'active'
		  AND t.consumer_type = 'shared'
		  AND c.deleted_at IS NULL
		ORDER BY t.id, c.category
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query shared tenant categories: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var topics []string
	for rows.Next() {
		var tenantID, category string
		if err := rows.Scan(&tenantID, &category); err != nil {
			return nil, fmt.Errorf("scan tenant category: %w", err)
		}
		// Build topic name at runtime using shared function
		topic := kafka.BuildTopicName(namespace, tenantID, category)
		topics = append(topics, topic)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate categories: %w", err)
	}

	return topics, nil
}

// GetDedicatedTenants returns tenants that require dedicated consumers.
// Each returned tenant includes their complete topic list built at runtime.
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

	// For each dedicated tenant, get their categories and build topics
	categoriesQuery := `
		SELECT category
		FROM tenant_categories
		WHERE tenant_id = $1
		  AND deleted_at IS NULL
		ORDER BY category
	`

	result := make([]kafka.TenantTopics, 0, len(tenantIDs))
	for _, tenantID := range tenantIDs {
		topics, err := r.getCategoriesForTenant(ctx, categoriesQuery, namespace, tenantID)
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

// getCategoriesForTenant retrieves categories for a specific tenant and builds topic names.
func (r *TopicRegistry) getCategoriesForTenant(ctx context.Context, query, namespace, tenantID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query categories for tenant %s: %w", tenantID, err)
	}
	defer func() { _ = rows.Close() }()

	var topics []string
	for rows.Next() {
		var category string
		if err := rows.Scan(&category); err != nil {
			return nil, fmt.Errorf("scan category for tenant %s: %w", tenantID, err)
		}
		// Build topic name at runtime using shared function
		topic := kafka.BuildTopicName(namespace, tenantID, category)
		topics = append(topics, topic)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate categories for tenant %s: %w", tenantID, err)
	}

	return topics, nil
}

// Ensure TopicRegistry implements kafka.TenantRegistry.
var _ kafka.TenantRegistry = (*TopicRegistry)(nil)
