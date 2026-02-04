package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Toniq-Labs/odin-ws/internal/provisioning"
)

// PostgresTopicRepository implements TopicStore using PostgreSQL.
// Uses tenant_categories table - full topic names are built at runtime.
type PostgresTopicRepository struct {
	db *sql.DB
}

// NewPostgresTopicRepository creates a new PostgresTopicRepository.
func NewPostgresTopicRepository(db *sql.DB) *PostgresTopicRepository {
	return &PostgresTopicRepository{db: db}
}

// Create records a provisioned topic category.
func (r *PostgresTopicRepository) Create(ctx context.Context, topic *provisioning.TenantTopic) error {
	query := `
		INSERT INTO tenant_categories (tenant_id, category, partitions, retention_ms, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (tenant_id, category) DO NOTHING
		RETURNING id
	`

	now := time.Now()
	if topic.CreatedAt.IsZero() {
		topic.CreatedAt = now
	}

	err := r.db.QueryRowContext(ctx, query,
		topic.TenantID,
		topic.Category,
		topic.Partitions,
		topic.RetentionMs,
		topic.CreatedAt,
	).Scan(&topic.ID)
	if err != nil {
		// ON CONFLICT DO NOTHING returns no rows, which causes sql.ErrNoRows
		// This is expected when the category already exists
		if err == sql.ErrNoRows {
			return nil
		}
		return fmt.Errorf("insert category: %w", err)
	}

	return nil
}

// ListByTenant returns all topic categories for a tenant.
func (r *PostgresTopicRepository) ListByTenant(ctx context.Context, tenantID string) ([]*provisioning.TenantTopic, error) {
	query := `
		SELECT id, tenant_id, category, partitions, retention_ms, created_at, deleted_at
		FROM tenant_categories
		WHERE tenant_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query categories: %w", err)
	}
	defer func() { _ = rows.Close() }()

	topics := []*provisioning.TenantTopic{}
	for rows.Next() {
		topic := &provisioning.TenantTopic{}
		var deletedAt sql.NullTime

		err := rows.Scan(
			&topic.ID,
			&topic.TenantID,
			&topic.Category,
			&topic.Partitions,
			&topic.RetentionMs,
			&topic.CreatedAt,
			&deletedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan category: %w", err)
		}

		if deletedAt.Valid {
			topic.DeletedAt = &deletedAt.Time
		}

		topics = append(topics, topic)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate categories: %w", err)
	}

	return topics, nil
}

// MarkDeleted marks a topic category as deleted.
func (r *PostgresTopicRepository) MarkDeleted(ctx context.Context, tenantID, category string) error {
	query := `
		UPDATE tenant_categories
		SET deleted_at = NOW()
		WHERE tenant_id = $1 AND category = $2 AND deleted_at IS NULL
	`

	result, err := r.db.ExecContext(ctx, query, tenantID, category)
	if err != nil {
		return fmt.Errorf("mark category deleted: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("category not found or already deleted: %s/%s", tenantID, category)
	}

	return nil
}

// CountByTenant returns the number of active topic categories for a tenant.
func (r *PostgresTopicRepository) CountByTenant(ctx context.Context, tenantID string) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM tenant_categories
		WHERE tenant_id = $1 AND deleted_at IS NULL
	`

	var count int
	err := r.db.QueryRowContext(ctx, query, tenantID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count categories: %w", err)
	}

	return count, nil
}

// CountPartitionsByTenant returns the total partitions for a tenant.
func (r *PostgresTopicRepository) CountPartitionsByTenant(ctx context.Context, tenantID string) (int, error) {
	query := `
		SELECT COALESCE(SUM(partitions), 0)
		FROM tenant_categories
		WHERE tenant_id = $1 AND deleted_at IS NULL
	`

	var count int
	err := r.db.QueryRowContext(ctx, query, tenantID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count partitions: %w", err)
	}

	return count, nil
}

// Ensure PostgresTopicRepository implements TopicStore.
var _ provisioning.TopicStore = (*PostgresTopicRepository)(nil)
