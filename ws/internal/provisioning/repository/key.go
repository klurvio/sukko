package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/klurvio/sukko/internal/provisioning"
)

// PostgresKeyRepository implements KeyStore using PostgreSQL.
type PostgresKeyRepository struct {
	db *sql.DB
}

// NewPostgresKeyRepository creates a new PostgresKeyRepository.
func NewPostgresKeyRepository(db *sql.DB) *PostgresKeyRepository {
	return &PostgresKeyRepository{db: db}
}

// Create creates a new key record.
func (r *PostgresKeyRepository) Create(ctx context.Context, key *provisioning.TenantKey) error {
	query := `
		INSERT INTO tenant_keys (key_id, tenant_id, algorithm, public_key, is_active, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	now := time.Now()
	if key.CreatedAt.IsZero() {
		key.CreatedAt = now
	}
	key.IsActive = true

	_, err := r.db.ExecContext(ctx, query,
		key.KeyID,
		key.TenantID,
		key.Algorithm,
		key.PublicKey,
		key.IsActive,
		key.CreatedAt,
		key.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("insert key: %w", err)
	}

	return nil
}

// Get retrieves a key by ID.
func (r *PostgresKeyRepository) Get(ctx context.Context, keyID string) (*provisioning.TenantKey, error) {
	query := `
		SELECT key_id, tenant_id, algorithm, public_key, is_active, created_at, expires_at, revoked_at
		FROM tenant_keys
		WHERE key_id = $1
	`

	key := &provisioning.TenantKey{}
	var expiresAt, revokedAt sql.NullTime

	err := r.db.QueryRowContext(ctx, query, keyID).Scan(
		&key.KeyID,
		&key.TenantID,
		&key.Algorithm,
		&key.PublicKey,
		&key.IsActive,
		&key.CreatedAt,
		&expiresAt,
		&revokedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("%w: %s", provisioning.ErrKeyNotFound, keyID)
	}
	if err != nil {
		return nil, fmt.Errorf("query key: %w", err)
	}

	if expiresAt.Valid {
		key.ExpiresAt = &expiresAt.Time
	}
	if revokedAt.Valid {
		key.RevokedAt = &revokedAt.Time
	}

	return key, nil
}

// ListByTenant returns keys for a tenant with pagination.
func (r *PostgresKeyRepository) ListByTenant(ctx context.Context, tenantID string, opts provisioning.ListOptions) ([]*provisioning.TenantKey, int, error) {
	// Count total
	var total int
	countQuery := `SELECT COUNT(*) FROM tenant_keys WHERE tenant_id = $1`
	if err := r.db.QueryRowContext(ctx, countQuery, tenantID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count keys: %w", err)
	}

	query := `
		SELECT key_id, tenant_id, algorithm, public_key, is_active, created_at, expires_at, revoked_at
		FROM tenant_keys
		WHERE tenant_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.db.QueryContext(ctx, query, tenantID, opts.Limit, opts.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query keys: %w", err)
	}
	defer func() { _ = rows.Close() }()

	keys := []*provisioning.TenantKey{}
	for rows.Next() {
		key := &provisioning.TenantKey{}
		var expiresAt, revokedAt sql.NullTime

		err := rows.Scan(
			&key.KeyID,
			&key.TenantID,
			&key.Algorithm,
			&key.PublicKey,
			&key.IsActive,
			&key.CreatedAt,
			&expiresAt,
			&revokedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan key: %w", err)
		}

		if expiresAt.Valid {
			key.ExpiresAt = &expiresAt.Time
		}
		if revokedAt.Valid {
			key.RevokedAt = &revokedAt.Time
		}

		keys = append(keys, key)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate keys: %w", err)
	}

	return keys, total, nil
}

// Revoke revokes a key by setting its revoked_at timestamp.
func (r *PostgresKeyRepository) Revoke(ctx context.Context, keyID string) error {
	query := `
		UPDATE tenant_keys
		SET is_active = false, revoked_at = $2
		WHERE key_id = $1 AND is_active = true
	`

	now := time.Now()
	result, err := r.db.ExecContext(ctx, query, keyID, now)
	if err != nil {
		return fmt.Errorf("revoke key: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("%w: %s", provisioning.ErrKeyNotFound, keyID)
	}

	return nil
}

// RevokeAllForTenant revokes all active keys for a tenant.
func (r *PostgresKeyRepository) RevokeAllForTenant(ctx context.Context, tenantID string) error {
	query := `
		UPDATE tenant_keys
		SET is_active = false, revoked_at = $2
		WHERE tenant_id = $1 AND is_active = true
	`

	now := time.Now()
	_, err := r.db.ExecContext(ctx, query, tenantID, now)
	if err != nil {
		return fmt.Errorf("revoke keys for tenant: %w", err)
	}

	return nil
}

// GetActiveKeys returns all active, non-expired, non-revoked keys.
// Used by WS Gateway to refresh its key cache.
func (r *PostgresKeyRepository) GetActiveKeys(ctx context.Context) ([]*provisioning.TenantKey, error) {
	query := `
		SELECT key_id, tenant_id, algorithm, public_key, is_active, created_at, expires_at, revoked_at
		FROM tenant_keys
		WHERE is_active = true
		  AND revoked_at IS NULL
		  AND (expires_at IS NULL OR expires_at > $1)
	`

	now := time.Now()
	rows, err := r.db.QueryContext(ctx, query, now)
	if err != nil {
		return nil, fmt.Errorf("query active keys: %w", err)
	}
	defer func() { _ = rows.Close() }()

	keys := []*provisioning.TenantKey{}
	for rows.Next() {
		key := &provisioning.TenantKey{}
		var expiresAt, revokedAt sql.NullTime

		err := rows.Scan(
			&key.KeyID,
			&key.TenantID,
			&key.Algorithm,
			&key.PublicKey,
			&key.IsActive,
			&key.CreatedAt,
			&expiresAt,
			&revokedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan key: %w", err)
		}

		if expiresAt.Valid {
			key.ExpiresAt = &expiresAt.Time
		}
		if revokedAt.Valid {
			key.RevokedAt = &revokedAt.Time
		}

		keys = append(keys, key)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate keys: %w", err)
	}

	return keys, nil
}

// Ensure PostgresKeyRepository implements KeyStore.
var _ provisioning.KeyStore = (*PostgresKeyRepository)(nil)
