package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klurvio/sukko/internal/provisioning"
)

// isDuplicateKeyError detects unique constraint violations from PostgreSQL.
func isDuplicateKeyError(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation
}

// defaultListLimit is the fallback page size when the caller provides no limit.
const defaultListLimit = 50

// TenantRepository implements TenantStore using PostgreSQL via pgxpool.
type TenantRepository struct {
	pool *pgxpool.Pool
}

// NewTenantRepository creates a TenantRepository.
func NewTenantRepository(pool *pgxpool.Pool) *TenantRepository {
	return &TenantRepository{pool: pool}
}

// Ping verifies database connectivity.
func (r *TenantRepository) Ping(ctx context.Context) error {
	if err := r.pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}

// Create creates a new tenant record.
func (r *TenantRepository) Create(ctx context.Context, tenant *provisioning.Tenant) error {
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

	_, err := r.pool.Exec(ctx, query,
		tenant.ID,
		tenant.Name,
		tenant.Status,
		tenant.ConsumerType,
		tenant.Metadata,
		tenant.CreatedAt,
		tenant.UpdatedAt,
	)
	if err != nil {
		if isDuplicateKeyError(err) {
			return fmt.Errorf("%w: %s", provisioning.ErrTenantAlreadyExists, tenant.ID)
		}
		return fmt.Errorf("insert tenant: %w", err)
	}

	return nil
}

// Get retrieves a tenant by ID.
func (r *TenantRepository) Get(ctx context.Context, tenantID string) (*provisioning.Tenant, error) {
	query := `
		SELECT id, name, status, consumer_type, metadata, created_at, updated_at,
		       suspended_at, deprovision_at, deleted_at
		FROM tenants
		WHERE id = $1
	`

	tenant := &provisioning.Tenant{}

	err := r.pool.QueryRow(ctx, query, tenantID).Scan(
		&tenant.ID,
		&tenant.Name,
		&tenant.Status,
		&tenant.ConsumerType,
		&tenant.Metadata,
		&tenant.CreatedAt,
		&tenant.UpdatedAt,
		&tenant.SuspendedAt,
		&tenant.DeprovisionAt,
		&tenant.DeletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", provisioning.ErrTenantNotFound, tenantID)
	}
	if err != nil {
		return nil, fmt.Errorf("query tenant: %w", err)
	}

	return tenant, nil
}

// Update updates an existing tenant record.
func (r *TenantRepository) Update(ctx context.Context, tenant *provisioning.Tenant) error {
	query := `
		UPDATE tenants
		SET name = $2, consumer_type = $3, metadata = $4, updated_at = $5
		WHERE id = $1 AND status != 'deleted'
	`

	now := time.Now()
	result, err := r.pool.Exec(ctx, query,
		tenant.ID,
		tenant.Name,
		tenant.ConsumerType,
		tenant.Metadata,
		now,
	)
	if err != nil {
		return fmt.Errorf("update tenant: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", provisioning.ErrTenantNotFound, tenant.ID)
	}

	return nil
}

// List returns tenants matching the given options.
func (r *TenantRepository) List(ctx context.Context, opts provisioning.ListOptions) ([]*provisioning.Tenant, int, error) {
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
	if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count tenants: %w", err)
	}

	// Get page
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	offset := max(opts.Offset, 0)

	query := fmt.Sprintf(`
		SELECT id, name, status, consumer_type, metadata, created_at, updated_at,
		       suspended_at, deprovision_at, deleted_at
		FROM tenants
		%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argNum, argNum+1)

	args = append(args, limit, offset)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query tenants: %w", err)
	}
	defer rows.Close()

	tenants := []*provisioning.Tenant{}
	for rows.Next() {
		tenant := &provisioning.Tenant{}

		err := rows.Scan(
			&tenant.ID,
			&tenant.Name,
			&tenant.Status,
			&tenant.ConsumerType,
			&tenant.Metadata,
			&tenant.CreatedAt,
			&tenant.UpdatedAt,
			&tenant.SuspendedAt,
			&tenant.DeprovisionAt,
			&tenant.DeletedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan tenant: %w", err)
		}

		tenants = append(tenants, tenant)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate tenants: %w", err)
	}

	return tenants, total, nil
}

// UpdateStatus updates a tenant's status.
func (r *TenantRepository) UpdateStatus(ctx context.Context, tenantID string, status provisioning.TenantStatus) error {
	var query string
	var args []any

	now := time.Now()

	switch status {
	case provisioning.StatusSuspended:
		query = `
			UPDATE tenants
			SET status = $2, suspended_at = $3, updated_at = $3
			WHERE id = $1 AND status = 'active'
		`
		args = []any{tenantID, status, now}
	case provisioning.StatusActive:
		query = `
			UPDATE tenants
			SET status = $2, suspended_at = NULL, updated_at = $3
			WHERE id = $1 AND status IN ('suspended')
		`
		args = []any{tenantID, status, now}
	case provisioning.StatusDeprovisioning:
		query = `
			UPDATE tenants
			SET status = $2, updated_at = $3
			WHERE id = $1 AND status IN ('active', 'suspended')
		`
		args = []any{tenantID, status, now}
	case provisioning.StatusDeleted:
		query = `
			UPDATE tenants
			SET status = $2, deleted_at = $3, updated_at = $3
			WHERE id = $1 AND status = 'deprovisioning'
		`
		args = []any{tenantID, status, now}
	default:
		return fmt.Errorf("invalid status transition to: %s", status)
	}

	result, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", provisioning.ErrTenantNotFound, tenantID)
	}

	return nil
}

// SetDeprovisionAt sets the deprovision deadline for a tenant.
func (r *TenantRepository) SetDeprovisionAt(ctx context.Context, tenantID string, deprovisionAt *provisioning.Time) error {
	query := `
		UPDATE tenants
		SET deprovision_at = $2, updated_at = $3
		WHERE id = $1 AND status = 'deprovisioning'
	`

	now := time.Now()
	result, err := r.pool.Exec(ctx, query, tenantID, deprovisionAt, now)
	if err != nil {
		return fmt.Errorf("set deprovision_at: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", provisioning.ErrTenantNotFound, tenantID)
	}

	return nil
}

// GetTenantsForDeletion returns tenants past their deprovision deadline.
func (r *TenantRepository) GetTenantsForDeletion(ctx context.Context) ([]*provisioning.Tenant, error) {
	query := `
		SELECT id, name, status, consumer_type, metadata, created_at, updated_at,
		       suspended_at, deprovision_at, deleted_at
		FROM tenants
		WHERE status = 'deprovisioning'
		  AND deprovision_at IS NOT NULL
		  AND deprovision_at <= $1
	`

	now := time.Now()
	rows, err := r.pool.Query(ctx, query, now)
	if err != nil {
		return nil, fmt.Errorf("query tenants for deletion: %w", err)
	}
	defer rows.Close()

	tenants := []*provisioning.Tenant{}
	for rows.Next() {
		tenant := &provisioning.Tenant{}

		err := rows.Scan(
			&tenant.ID,
			&tenant.Name,
			&tenant.Status,
			&tenant.ConsumerType,
			&tenant.Metadata,
			&tenant.CreatedAt,
			&tenant.UpdatedAt,
			&tenant.SuspendedAt,
			&tenant.DeprovisionAt,
			&tenant.DeletedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan tenant: %w", err)
		}

		tenants = append(tenants, tenant)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenants: %w", err)
	}

	return tenants, nil
}

// Count returns the number of active (non-deleted) tenants.
func (r *TenantRepository) Count(ctx context.Context) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM tenants WHERE status != $1", provisioning.StatusDeleted).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count tenants: %w", err)
	}
	return count, nil
}

var _ provisioning.TenantStore = (*TenantRepository)(nil)
