// Package kafka provides Kafka/Redpanda consumer integration with resource-aware
// message processing.
package kafka

import (
	"context"
)

// TenantRegistry provides tenant topic information for consumers.
// This interface abstracts the provisioning database from the consumer pool,
// allowing the pool to query for tenant topics without direct DB dependencies.
//
// Thread Safety: Implementations must be safe for concurrent use.
type TenantRegistry interface {
	// GetSharedTenantTopics returns all topics for tenants using shared consumer mode.
	// Topics are returned in the format: {namespace}.{tenant_id}.{category}
	// Only active tenants with consumer_type="shared" are included.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout
	//   - namespace: Topic namespace (e.g., "main", "develop")
	//
	// Returns:
	//   - []string: List of topic names for all shared tenants
	//   - error: If the database query fails
	GetSharedTenantTopics(ctx context.Context, namespace string) ([]string, error)

	// GetDedicatedTenants returns tenants that require dedicated consumers.
	// Each dedicated tenant gets its own consumer group for complete isolation.
	// Only active tenants with consumer_type="dedicated" are included.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout
	//   - namespace: Topic namespace (e.g., "main", "develop")
	//
	// Returns:
	//   - []TenantTopics: List of tenant IDs with their topic lists
	//   - error: If the database query fails
	GetDedicatedTenants(ctx context.Context, namespace string) ([]TenantTopics, error)
}

// TenantTopics represents a tenant and its associated Kafka topics.
// Used by dedicated consumers to isolate tenant traffic.
type TenantTopics struct {
	// TenantID is the unique identifier for the tenant (e.g., "acme")
	TenantID string

	// Topics is the list of Kafka topics for this tenant
	// Format: {namespace}.{tenant_id}.{category} (e.g., "main.acme.trade")
	Topics []string
}
