package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Toniq-Labs/odin-ws/internal/provisioning"
	"github.com/Toniq-Labs/odin-ws/internal/shared/types"
)

// PostgresOIDCConfigRepository implements OIDCConfigStore using PostgreSQL.
type PostgresOIDCConfigRepository struct {
	db *sql.DB
}

// NewPostgresOIDCConfigRepository creates a new PostgresOIDCConfigRepository.
func NewPostgresOIDCConfigRepository(db *sql.DB) *PostgresOIDCConfigRepository {
	return &PostgresOIDCConfigRepository{db: db}
}

// Create creates a new OIDC configuration for a tenant.
func (r *PostgresOIDCConfigRepository) Create(ctx context.Context, config *types.TenantOIDCConfig) error {
	query := `
		INSERT INTO tenant_oidc_config (tenant_id, issuer_url, jwks_url, audience, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
	`

	_, err := r.db.ExecContext(ctx, query,
		config.TenantID,
		config.IssuerURL,
		nullString(config.JWKSURL),
		nullString(config.Audience),
		config.Enabled,
	)
	if err != nil {
		// Check for unique constraint violation (issuer already exists)
		if isUniqueViolation(err) {
			return fmt.Errorf("create OIDC config: %w", types.ErrIssuerAlreadyExists)
		}
		return fmt.Errorf("create OIDC config: %w", err)
	}

	return nil
}

// Get retrieves OIDC configuration by tenant ID.
func (r *PostgresOIDCConfigRepository) Get(ctx context.Context, tenantID string) (*types.TenantOIDCConfig, error) {
	query := `
		SELECT tenant_id, issuer_url, jwks_url, audience, enabled, created_at, updated_at
		FROM tenant_oidc_config
		WHERE tenant_id = $1
	`

	config := &types.TenantOIDCConfig{}
	var jwksURL, audience sql.NullString

	err := r.db.QueryRowContext(ctx, query, tenantID).Scan(
		&config.TenantID,
		&config.IssuerURL,
		&jwksURL,
		&audience,
		&config.Enabled,
		&config.CreatedAt,
		&config.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, types.ErrOIDCNotConfigured
	}
	if err != nil {
		return nil, fmt.Errorf("query OIDC config: %w", err)
	}

	if jwksURL.Valid {
		config.JWKSURL = jwksURL.String
	}
	if audience.Valid {
		config.Audience = audience.String
	}

	return config, nil
}

// GetByIssuer retrieves OIDC configuration by issuer URL.
// This is the hot path for token validation - lookup tenant from issuer.
func (r *PostgresOIDCConfigRepository) GetByIssuer(ctx context.Context, issuerURL string) (*types.TenantOIDCConfig, error) {
	query := `
		SELECT tenant_id, issuer_url, jwks_url, audience, enabled, created_at, updated_at
		FROM tenant_oidc_config
		WHERE issuer_url = $1 AND enabled = true
	`

	config := &types.TenantOIDCConfig{}
	var jwksURL, audience sql.NullString

	err := r.db.QueryRowContext(ctx, query, issuerURL).Scan(
		&config.TenantID,
		&config.IssuerURL,
		&jwksURL,
		&audience,
		&config.Enabled,
		&config.CreatedAt,
		&config.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, types.ErrIssuerNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query OIDC config by issuer %s: %w", issuerURL, err)
	}

	if jwksURL.Valid {
		config.JWKSURL = jwksURL.String
	}
	if audience.Valid {
		config.Audience = audience.String
	}

	return config, nil
}

// Update updates an existing OIDC configuration.
func (r *PostgresOIDCConfigRepository) Update(ctx context.Context, config *types.TenantOIDCConfig) error {
	query := `
		UPDATE tenant_oidc_config
		SET issuer_url = $2, jwks_url = $3, audience = $4, enabled = $5, updated_at = NOW()
		WHERE tenant_id = $1
	`

	result, err := r.db.ExecContext(ctx, query,
		config.TenantID,
		config.IssuerURL,
		nullString(config.JWKSURL),
		nullString(config.Audience),
		config.Enabled,
	)
	if err != nil {
		// Check for unique constraint violation (issuer already exists for another tenant)
		if isUniqueViolation(err) {
			return fmt.Errorf("update OIDC config: %w", types.ErrIssuerAlreadyExists)
		}
		return fmt.Errorf("update OIDC config: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}
	if rows == 0 {
		return types.ErrOIDCNotConfigured
	}

	return nil
}

// Delete deletes OIDC configuration for a tenant.
func (r *PostgresOIDCConfigRepository) Delete(ctx context.Context, tenantID string) error {
	query := `DELETE FROM tenant_oidc_config WHERE tenant_id = $1`

	result, err := r.db.ExecContext(ctx, query, tenantID)
	if err != nil {
		return fmt.Errorf("delete OIDC config: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}
	if rows == 0 {
		return types.ErrOIDCNotConfigured
	}

	return nil
}

// ListEnabled returns all enabled OIDC configurations.
// Used by gateway to build issuer→tenant cache.
func (r *PostgresOIDCConfigRepository) ListEnabled(ctx context.Context) ([]*types.TenantOIDCConfig, error) {
	query := `
		SELECT tenant_id, issuer_url, jwks_url, audience, enabled, created_at, updated_at
		FROM tenant_oidc_config
		WHERE enabled = true
		ORDER BY created_at ASC
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query enabled OIDC configs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	configs := []*types.TenantOIDCConfig{}
	for rows.Next() {
		config := &types.TenantOIDCConfig{}
		var jwksURL, audience sql.NullString

		err := rows.Scan(
			&config.TenantID,
			&config.IssuerURL,
			&jwksURL,
			&audience,
			&config.Enabled,
			&config.CreatedAt,
			&config.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan OIDC config: %w", err)
		}

		if jwksURL.Valid {
			config.JWKSURL = jwksURL.String
		}
		if audience.Valid {
			config.Audience = audience.String
		}

		configs = append(configs, config)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate OIDC configs: %w", err)
	}

	return configs, nil
}

// nullString returns a sql.NullString for optional string values.
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// isUniqueViolation checks if the error is a unique constraint violation.
// PostgreSQL error code 23505 = unique_violation
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// Check for pq driver error or pgx driver error
	errStr := err.Error()
	return errStr == "pq: duplicate key value violates unique constraint" ||
		containsSubstring(errStr, "SQLSTATE 23505") ||
		containsSubstring(errStr, "duplicate key value violates unique constraint") ||
		containsSubstring(errStr, "violates unique constraint")
}

// containsSubstring is a simple substring check without importing strings.
func containsSubstring(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Ensure PostgresOIDCConfigRepository implements OIDCConfigStore.
var _ provisioning.OIDCConfigStore = (*PostgresOIDCConfigRepository)(nil)
