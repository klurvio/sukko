package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Toniq-Labs/odin-ws/internal/provisioning"
)

// PostgresTopicRepository implements TopicStore using PostgreSQL.
type PostgresTopicRepository struct {
	db *sql.DB
}

// NewPostgresTopicRepository creates a new PostgresTopicRepository.
func NewPostgresTopicRepository(db *sql.DB) *PostgresTopicRepository {
	return &PostgresTopicRepository{db: db}
}

// Create records a provisioned topic.
func (r *PostgresTopicRepository) Create(ctx context.Context, topic *provisioning.TenantTopic) error {
	query := `
		INSERT INTO tenant_topics (tenant_id, topic_name, category, partitions, retention_ms, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`

	now := time.Now()
	if topic.CreatedAt.IsZero() {
		topic.CreatedAt = now
	}

	err := r.db.QueryRowContext(ctx, query,
		topic.TenantID,
		topic.TopicName,
		topic.Category,
		topic.Partitions,
		topic.RetentionMs,
		topic.CreatedAt,
	).Scan(&topic.ID)
	if err != nil {
		return fmt.Errorf("insert topic: %w", err)
	}

	return nil
}

// ListByTenant returns all topics for a tenant.
func (r *PostgresTopicRepository) ListByTenant(ctx context.Context, tenantID string) ([]*provisioning.TenantTopic, error) {
	query := `
		SELECT id, tenant_id, topic_name, category, partitions, retention_ms, created_at, deleted_at
		FROM tenant_topics
		WHERE tenant_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query topics: %w", err)
	}
	defer func() { _ = rows.Close() }()

	topics := []*provisioning.TenantTopic{}
	for rows.Next() {
		topic := &provisioning.TenantTopic{}
		var deletedAt sql.NullTime

		err := rows.Scan(
			&topic.ID,
			&topic.TenantID,
			&topic.TopicName,
			&topic.Category,
			&topic.Partitions,
			&topic.RetentionMs,
			&topic.CreatedAt,
			&deletedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan topic: %w", err)
		}

		if deletedAt.Valid {
			topic.DeletedAt = &deletedAt.Time
		}

		topics = append(topics, topic)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate topics: %w", err)
	}

	return topics, nil
}

// MarkDeleted marks a topic as deleted.
func (r *PostgresTopicRepository) MarkDeleted(ctx context.Context, topicName string) error {
	query := `
		UPDATE tenant_topics
		SET deleted_at = NOW()
		WHERE topic_name = $1 AND deleted_at IS NULL
	`

	result, err := r.db.ExecContext(ctx, query, topicName)
	if err != nil {
		return fmt.Errorf("mark topic deleted: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("topic not found or already deleted: %s", topicName)
	}

	return nil
}

// CountByTenant returns the number of active topics for a tenant.
func (r *PostgresTopicRepository) CountByTenant(ctx context.Context, tenantID string) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM tenant_topics
		WHERE tenant_id = $1 AND deleted_at IS NULL
	`

	var count int
	err := r.db.QueryRowContext(ctx, query, tenantID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count topics: %w", err)
	}

	return count, nil
}

// CountPartitionsByTenant returns the total partitions for a tenant.
func (r *PostgresTopicRepository) CountPartitionsByTenant(ctx context.Context, tenantID string) (int, error) {
	query := `
		SELECT COALESCE(SUM(partitions), 0)
		FROM tenant_topics
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
