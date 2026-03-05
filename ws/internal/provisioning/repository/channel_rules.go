package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/klurvio/sukko/internal/provisioning"
	"github.com/klurvio/sukko/internal/shared/types"
)

// PostgresChannelRulesRepository implements ChannelRulesStore using PostgreSQL.
type PostgresChannelRulesRepository struct {
	db *sql.DB
}

// NewPostgresChannelRulesRepository creates a new PostgresChannelRulesRepository.
func NewPostgresChannelRulesRepository(db *sql.DB) *PostgresChannelRulesRepository {
	return &PostgresChannelRulesRepository{db: db}
}

// Create creates channel rules for a tenant.
func (r *PostgresChannelRulesRepository) Create(ctx context.Context, tenantID string, rules *types.ChannelRules) error {
	rulesJSON, err := json.Marshal(rules)
	if err != nil {
		return fmt.Errorf("marshal rules: %w", err)
	}

	query := `
		INSERT INTO tenant_channel_rules (tenant_id, rules, created_at, updated_at)
		VALUES ($1, $2, NOW(), NOW())
	`

	_, err = r.db.ExecContext(ctx, query, tenantID, rulesJSON)
	if err != nil {
		return fmt.Errorf("create channel rules: %w", err)
	}

	return nil
}

// Get retrieves channel rules by tenant ID.
func (r *PostgresChannelRulesRepository) Get(ctx context.Context, tenantID string) (*types.TenantChannelRules, error) {
	query := `
		SELECT tenant_id, rules, created_at, updated_at
		FROM tenant_channel_rules
		WHERE tenant_id = $1
	`

	tcr := &types.TenantChannelRules{}
	var rulesJSON []byte

	err := r.db.QueryRowContext(ctx, query, tenantID).Scan(
		&tcr.TenantID,
		&rulesJSON,
		&tcr.CreatedAt,
		&tcr.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, types.ErrChannelRulesNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query channel rules: %w", err)
	}

	if err := json.Unmarshal(rulesJSON, &tcr.Rules); err != nil {
		return nil, fmt.Errorf("unmarshal rules: %w", err)
	}

	return tcr, nil
}

// GetRules retrieves just the channel rules (not the wrapper) by tenant ID.
// This is a convenience method for the common use case.
func (r *PostgresChannelRulesRepository) GetRules(ctx context.Context, tenantID string) (*types.ChannelRules, error) {
	tcr, err := r.Get(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return &tcr.Rules, nil
}

// Update updates channel rules for a tenant (upsert).
func (r *PostgresChannelRulesRepository) Update(ctx context.Context, tenantID string, rules *types.ChannelRules) error {
	rulesJSON, err := json.Marshal(rules)
	if err != nil {
		return fmt.Errorf("marshal rules: %w", err)
	}

	// Use upsert (INSERT ON CONFLICT) to handle both create and update
	query := `
		INSERT INTO tenant_channel_rules (tenant_id, rules, created_at, updated_at)
		VALUES ($1, $2, NOW(), NOW())
		ON CONFLICT (tenant_id)
		DO UPDATE SET rules = $2, updated_at = NOW()
	`

	_, err = r.db.ExecContext(ctx, query, tenantID, rulesJSON)
	if err != nil {
		return fmt.Errorf("update channel rules: %w", err)
	}

	return nil
}

// Delete deletes channel rules for a tenant.
func (r *PostgresChannelRulesRepository) Delete(ctx context.Context, tenantID string) error {
	query := `DELETE FROM tenant_channel_rules WHERE tenant_id = $1`

	result, err := r.db.ExecContext(ctx, query, tenantID)
	if err != nil {
		return fmt.Errorf("delete channel rules: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}
	if rows == 0 {
		return types.ErrChannelRulesNotFound
	}

	return nil
}

// List returns all channel rules (used by gateway to build cache).
func (r *PostgresChannelRulesRepository) List(ctx context.Context) ([]*types.TenantChannelRules, error) {
	query := `
		SELECT tenant_id, rules, created_at, updated_at
		FROM tenant_channel_rules
		ORDER BY created_at ASC
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query channel rules: %w", err)
	}
	defer func() { _ = rows.Close() }()

	results := []*types.TenantChannelRules{}
	for rows.Next() {
		tcr := &types.TenantChannelRules{}
		var rulesJSON []byte

		err := rows.Scan(
			&tcr.TenantID,
			&rulesJSON,
			&tcr.CreatedAt,
			&tcr.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan channel rules: %w", err)
		}

		if err := json.Unmarshal(rulesJSON, &tcr.Rules); err != nil {
			return nil, fmt.Errorf("unmarshal rules for tenant %s: %w", tcr.TenantID, err)
		}

		results = append(results, tcr)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate channel rules: %w", err)
	}

	return results, nil
}

// Ensure PostgresChannelRulesRepository implements ChannelRulesStore.
var _ provisioning.ChannelRulesStore = (*PostgresChannelRulesRepository)(nil)
