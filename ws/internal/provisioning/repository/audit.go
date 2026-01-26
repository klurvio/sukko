// Package repository provides database repositories for the provisioning system.
package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Toniq-Labs/odin-ws/internal/provisioning"
)

// PostgresAuditRepository implements AuditStore using PostgreSQL.
type PostgresAuditRepository struct {
	db *sql.DB
}

// NewPostgresAuditRepository creates a new PostgresAuditRepository.
func NewPostgresAuditRepository(db *sql.DB) *PostgresAuditRepository {
	return &PostgresAuditRepository{db: db}
}

// Log records an audit entry.
func (r *PostgresAuditRepository) Log(ctx context.Context, entry *provisioning.AuditEntry) error {
	query := `
		INSERT INTO provisioning_audit (tenant_id, action, actor, actor_type, ip_address, details, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`

	now := time.Now()
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	if entry.ActorType == "" {
		entry.ActorType = provisioning.ActorTypeUser
	}
	if entry.Details == nil {
		entry.Details = make(provisioning.Metadata)
	}

	// Convert empty tenant_id to NULL
	var tenantID any
	if entry.TenantID != "" {
		tenantID = entry.TenantID
	}

	// Convert empty IP address to NULL
	var ipAddress any
	if entry.IPAddress != "" {
		ipAddress = entry.IPAddress
	}

	err := r.db.QueryRowContext(ctx, query,
		tenantID,
		entry.Action,
		entry.Actor,
		entry.ActorType,
		ipAddress,
		entry.Details,
		entry.CreatedAt,
	).Scan(&entry.ID)
	if err != nil {
		return fmt.Errorf("insert audit entry: %w", err)
	}

	return nil
}

// ListByTenant returns audit entries for a tenant.
func (r *PostgresAuditRepository) ListByTenant(ctx context.Context, tenantID string, opts provisioning.ListOptions) ([]*provisioning.AuditEntry, int, error) {
	// Count total
	countQuery := `SELECT COUNT(*) FROM provisioning_audit WHERE tenant_id = $1`
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, tenantID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count audit entries: %w", err)
	}

	// Get page
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := max(opts.Offset, 0)

	query := `
		SELECT id, tenant_id, action, actor, actor_type, ip_address, details, created_at
		FROM provisioning_audit
		WHERE tenant_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.db.QueryContext(ctx, query, tenantID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query audit entries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	entries := []*provisioning.AuditEntry{}
	for rows.Next() {
		entry := &provisioning.AuditEntry{}
		var tenantIDNull sql.NullString
		var ipAddressNull sql.NullString

		err := rows.Scan(
			&entry.ID,
			&tenantIDNull,
			&entry.Action,
			&entry.Actor,
			&entry.ActorType,
			&ipAddressNull,
			&entry.Details,
			&entry.CreatedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan audit entry: %w", err)
		}

		if tenantIDNull.Valid {
			entry.TenantID = tenantIDNull.String
		}
		if ipAddressNull.Valid {
			entry.IPAddress = ipAddressNull.String
		}

		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate audit entries: %w", err)
	}

	return entries, total, nil
}

// Ensure PostgresAuditRepository implements AuditStore.
var _ provisioning.AuditStore = (*PostgresAuditRepository)(nil)
