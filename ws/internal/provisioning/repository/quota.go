package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Toniq-Labs/odin-ws/internal/provisioning"
)

// PostgresQuotaRepository implements QuotaStore using PostgreSQL.
type PostgresQuotaRepository struct {
	db *sql.DB
}

// NewPostgresQuotaRepository creates a new PostgresQuotaRepository.
func NewPostgresQuotaRepository(db *sql.DB) *PostgresQuotaRepository {
	return &PostgresQuotaRepository{db: db}
}

// Get retrieves quotas for a tenant.
func (r *PostgresQuotaRepository) Get(ctx context.Context, tenantID string) (*provisioning.TenantQuota, error) {
	query := `
		SELECT tenant_id, max_topics, max_partitions, max_storage_bytes,
		       producer_byte_rate, consumer_byte_rate, updated_at
		FROM tenant_quotas
		WHERE tenant_id = $1
	`

	quota := &provisioning.TenantQuota{}
	err := r.db.QueryRowContext(ctx, query, tenantID).Scan(
		&quota.TenantID,
		&quota.MaxTopics,
		&quota.MaxPartitions,
		&quota.MaxStorageBytes,
		&quota.ProducerByteRate,
		&quota.ConsumerByteRate,
		&quota.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("quota not found for tenant: %s", tenantID)
	}
	if err != nil {
		return nil, fmt.Errorf("query quota: %w", err)
	}

	return quota, nil
}

// Create creates quota record for a tenant.
func (r *PostgresQuotaRepository) Create(ctx context.Context, quota *provisioning.TenantQuota) error {
	query := `
		INSERT INTO tenant_quotas (tenant_id, max_topics, max_partitions, max_storage_bytes,
		                           producer_byte_rate, consumer_byte_rate, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	now := time.Now()
	if quota.UpdatedAt.IsZero() {
		quota.UpdatedAt = now
	}

	_, err := r.db.ExecContext(ctx, query,
		quota.TenantID,
		quota.MaxTopics,
		quota.MaxPartitions,
		quota.MaxStorageBytes,
		quota.ProducerByteRate,
		quota.ConsumerByteRate,
		quota.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert quota: %w", err)
	}

	return nil
}

// Update updates quota record for a tenant.
func (r *PostgresQuotaRepository) Update(ctx context.Context, quota *provisioning.TenantQuota) error {
	query := `
		UPDATE tenant_quotas
		SET max_topics = $2, max_partitions = $3, max_storage_bytes = $4,
		    producer_byte_rate = $5, consumer_byte_rate = $6, updated_at = NOW()
		WHERE tenant_id = $1
	`

	result, err := r.db.ExecContext(ctx, query,
		quota.TenantID,
		quota.MaxTopics,
		quota.MaxPartitions,
		quota.MaxStorageBytes,
		quota.ProducerByteRate,
		quota.ConsumerByteRate,
	)
	if err != nil {
		return fmt.Errorf("update quota: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("quota not found for tenant: %s", quota.TenantID)
	}

	return nil
}

// Ensure PostgresQuotaRepository implements QuotaStore.
var _ provisioning.QuotaStore = (*PostgresQuotaRepository)(nil)
