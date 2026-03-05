package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/klurvio/sukko/internal/provisioning"
)

// PostgresTenantRepository implements TenantStore using PostgreSQL.
type PostgresTenantRepository struct {
	db *sql.DB
}

// NewPostgresTenantRepository creates a new PostgresTenantRepository.
func NewPostgresTenantRepository(db *sql.DB) *PostgresTenantRepository {
	return &PostgresTenantRepository{db: db}
}

// Ping verifies database connectivity.
func (r *PostgresTenantRepository) Ping(ctx context.Context) error {
	return r.db.PingContext(ctx)
}

// Create creates a new tenant record.
func (r *PostgresTenantRepository) Create(ctx context.Context, tenant *provisioning.Tenant) error {
	query := `
		INSERT INTO tenants (id, name, status, consumer_type, metadata, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	now := time.Now()
	if tenant.CreatedAt.IsZero() {
		tenant.CreatedAt = now
	}
	if tenant.UpdatedAt.IsZero() {
		tenant.UpdatedAt = now
	}
	if tenant.Status == "" {
		tenant.Status = provisioning.StatusActive
	}
	if tenant.ConsumerType == "" {
		tenant.ConsumerType = provisioning.ConsumerShared
	}
	if tenant.Metadata == nil {
		tenant.Metadata = make(provisioning.Metadata)
	}

	_, err := r.db.ExecContext(ctx, query,
		tenant.ID,
		tenant.Name,
		tenant.Status,
		tenant.ConsumerType,
		tenant.Metadata,
		tenant.CreatedAt,
		tenant.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert tenant: %w", err)
	}

	return nil
}

// Get retrieves a tenant by ID.
func (r *PostgresTenantRepository) Get(ctx context.Context, tenantID string) (*provisioning.Tenant, error) {
	query := `
		SELECT id, name, status, consumer_type, metadata, created_at, updated_at,
		       suspended_at, deprovision_at, deleted_at
		FROM tenants
		WHERE id = $1
	`

	tenant := &provisioning.Tenant{}
	var suspendedAt, deprovisionAt, deletedAt sql.NullTime

	err := r.db.QueryRowContext(ctx, query, tenantID).Scan(
		&tenant.ID,
		&tenant.Name,
		&tenant.Status,
		&tenant.ConsumerType,
		&tenant.Metadata,
		&tenant.CreatedAt,
		&tenant.UpdatedAt,
		&suspendedAt,
		&deprovisionAt,
		&deletedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("tenant not found: %s", tenantID)
	}
	if err != nil {
		return nil, fmt.Errorf("query tenant: %w", err)
	}

	if suspendedAt.Valid {
		tenant.SuspendedAt = &suspendedAt.Time
	}
	if deprovisionAt.Valid {
		tenant.DeprovisionAt = &deprovisionAt.Time
	}
	if deletedAt.Valid {
		tenant.DeletedAt = &deletedAt.Time
	}

	return tenant, nil
}

// Update updates an existing tenant record.
func (r *PostgresTenantRepository) Update(ctx context.Context, tenant *provisioning.Tenant) error {
	query := `
		UPDATE tenants
		SET name = $2, consumer_type = $3, metadata = $4, updated_at = NOW()
		WHERE id = $1 AND status != 'deleted'
	`

	result, err := r.db.ExecContext(ctx, query,
		tenant.ID,
		tenant.Name,
		tenant.ConsumerType,
		tenant.Metadata,
	)
	if err != nil {
		return fmt.Errorf("update tenant: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("tenant not found or deleted: %s", tenant.ID)
	}

	return nil
}

// List returns tenants matching the given options.
func (r *PostgresTenantRepository) List(ctx context.Context, opts provisioning.ListOptions) ([]*provisioning.Tenant, int, error) {
	// Build query with optional status filter
	whereClause := "WHERE status != 'deleted'"
	args := []any{}
	argNum := 1

	if opts.Status != nil {
		whereClause += fmt.Sprintf(" AND status = $%d", argNum)
		args = append(args, *opts.Status)
		argNum++
	}

	// Count total
	countQuery := "SELECT COUNT(*) FROM tenants " + whereClause
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count tenants: %w", err)
	}

	// Get page
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := max(opts.Offset, 0)

	//nolint:gosec // whereClause is built from known conditions using parameterized values, safe from SQL injection
	query := fmt.Sprintf(`
		SELECT id, name, status, consumer_type, metadata, created_at, updated_at,
		       suspended_at, deprovision_at, deleted_at
		FROM tenants
		%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argNum, argNum+1)

	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query tenants: %w", err)
	}
	defer func() { _ = rows.Close() }()

	tenants := []*provisioning.Tenant{}
	for rows.Next() {
		tenant := &provisioning.Tenant{}
		var suspendedAt, deprovisionAt, deletedAt sql.NullTime

		err := rows.Scan(
			&tenant.ID,
			&tenant.Name,
			&tenant.Status,
			&tenant.ConsumerType,
			&tenant.Metadata,
			&tenant.CreatedAt,
			&tenant.UpdatedAt,
			&suspendedAt,
			&deprovisionAt,
			&deletedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan tenant: %w", err)
		}

		if suspendedAt.Valid {
			tenant.SuspendedAt = &suspendedAt.Time
		}
		if deprovisionAt.Valid {
			tenant.DeprovisionAt = &deprovisionAt.Time
		}
		if deletedAt.Valid {
			tenant.DeletedAt = &deletedAt.Time
		}

		tenants = append(tenants, tenant)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate tenants: %w", err)
	}

	return tenants, total, nil
}

// UpdateStatus updates a tenant's status.
func (r *PostgresTenantRepository) UpdateStatus(ctx context.Context, tenantID string, status provisioning.TenantStatus) error {
	var query string
	var args []any

	switch status {
	case provisioning.StatusSuspended:
		query = `
			UPDATE tenants
			SET status = $2, suspended_at = NOW(), updated_at = NOW()
			WHERE id = $1 AND status = 'active'
		`
		args = []any{tenantID, status}
	case provisioning.StatusActive:
		query = `
			UPDATE tenants
			SET status = $2, suspended_at = NULL, updated_at = NOW()
			WHERE id = $1 AND status IN ('suspended')
		`
		args = []any{tenantID, status}
	case provisioning.StatusDeprovisioning:
		query = `
			UPDATE tenants
			SET status = $2, updated_at = NOW()
			WHERE id = $1 AND status IN ('active', 'suspended')
		`
		args = []any{tenantID, status}
	case provisioning.StatusDeleted:
		query = `
			UPDATE tenants
			SET status = $2, deleted_at = NOW(), updated_at = NOW()
			WHERE id = $1 AND status = 'deprovisioning'
		`
		args = []any{tenantID, status}
	default:
		return fmt.Errorf("invalid status transition to: %s", status)
	}

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("tenant not found or invalid status transition: %s", tenantID)
	}

	return nil
}

// SetDeprovisionAt sets the deprovision deadline for a tenant.
func (r *PostgresTenantRepository) SetDeprovisionAt(ctx context.Context, tenantID string, deprovisionAt *provisioning.Time) error {
	query := `
		UPDATE tenants
		SET deprovision_at = $2, updated_at = NOW()
		WHERE id = $1 AND status = 'deprovisioning'
	`

	result, err := r.db.ExecContext(ctx, query, tenantID, deprovisionAt)
	if err != nil {
		return fmt.Errorf("set deprovision_at: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("tenant not found or not in deprovisioning state: %s", tenantID)
	}

	return nil
}

// GetTenantsForDeletion returns tenants past their deprovision deadline.
func (r *PostgresTenantRepository) GetTenantsForDeletion(ctx context.Context) ([]*provisioning.Tenant, error) {
	query := `
		SELECT id, name, status, consumer_type, metadata, created_at, updated_at,
		       suspended_at, deprovision_at, deleted_at
		FROM tenants
		WHERE status = 'deprovisioning'
		  AND deprovision_at IS NOT NULL
		  AND deprovision_at <= NOW()
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query tenants for deletion: %w", err)
	}
	defer func() { _ = rows.Close() }()

	tenants := []*provisioning.Tenant{}
	for rows.Next() {
		tenant := &provisioning.Tenant{}
		var suspendedAt, deprovisionAt, deletedAt sql.NullTime

		err := rows.Scan(
			&tenant.ID,
			&tenant.Name,
			&tenant.Status,
			&tenant.ConsumerType,
			&tenant.Metadata,
			&tenant.CreatedAt,
			&tenant.UpdatedAt,
			&suspendedAt,
			&deprovisionAt,
			&deletedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan tenant: %w", err)
		}

		if suspendedAt.Valid {
			tenant.SuspendedAt = &suspendedAt.Time
		}
		if deprovisionAt.Valid {
			tenant.DeprovisionAt = &deprovisionAt.Time
		}
		if deletedAt.Valid {
			tenant.DeletedAt = &deletedAt.Time
		}

		tenants = append(tenants, tenant)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenants: %w", err)
	}

	return tenants, nil
}

// Ensure PostgresTenantRepository implements TenantStore.
var _ provisioning.TenantStore = (*PostgresTenantRepository)(nil)
