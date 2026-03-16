package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Toniq-Labs/odin-ws/internal/provisioning"
)

// PostgresRoutingRulesRepository implements RoutingRulesStore using PostgreSQL/SQLite.
type PostgresRoutingRulesRepository struct {
	db *sql.DB
}

// NewPostgresRoutingRulesRepository creates a new PostgresRoutingRulesRepository.
func NewPostgresRoutingRulesRepository(db *sql.DB) *PostgresRoutingRulesRepository {
	return &PostgresRoutingRulesRepository{db: db}
}

// Get retrieves routing rules for a tenant.
func (r *PostgresRoutingRulesRepository) Get(ctx context.Context, tenantID string) ([]provisioning.TopicRoutingRule, error) {
	query := `SELECT rules FROM tenant_routing_rules WHERE tenant_id = $1`

	var rulesJSON []byte
	err := r.db.QueryRowContext(ctx, query, tenantID).Scan(&rulesJSON)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, provisioning.ErrRoutingRulesNotFound
		}
		return nil, fmt.Errorf("query routing rules: %w", err)
	}

	var rules []provisioning.TopicRoutingRule
	if err := json.Unmarshal(rulesJSON, &rules); err != nil {
		return nil, fmt.Errorf("unmarshal routing rules: %w", err)
	}

	return rules, nil
}

// Set creates or updates routing rules for a tenant (upsert).
func (r *PostgresRoutingRulesRepository) Set(ctx context.Context, tenantID string, rules []provisioning.TopicRoutingRule) error {
	rulesJSON, err := json.Marshal(rules)
	if err != nil {
		return fmt.Errorf("marshal routing rules: %w", err)
	}

	now := time.Now()
	query := `
		INSERT INTO tenant_routing_rules (tenant_id, rules, created_at, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tenant_id) DO UPDATE SET rules = $2, updated_at = $4
	`

	_, err = r.db.ExecContext(ctx, query, tenantID, rulesJSON, now, now)
	if err != nil {
		return fmt.Errorf("upsert routing rules: %w", err)
	}

	return nil
}

// Delete deletes routing rules for a tenant.
func (r *PostgresRoutingRulesRepository) Delete(ctx context.Context, tenantID string) error {
	query := `DELETE FROM tenant_routing_rules WHERE tenant_id = $1`

	result, err := r.db.ExecContext(ctx, query, tenantID)
	if err != nil {
		return fmt.Errorf("delete routing rules: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}
	if rows == 0 {
		return provisioning.ErrRoutingRulesNotFound
	}

	return nil
}

// ListAll returns routing rules for all tenants.
func (r *PostgresRoutingRulesRepository) ListAll(ctx context.Context) (map[string][]provisioning.TopicRoutingRule, error) {
	query := `SELECT tenant_id, rules FROM tenant_routing_rules`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query all routing rules: %w", err)
	}
	defer func() { _ = rows.Close() }() // Close error non-actionable on read-only result set

	result := make(map[string][]provisioning.TopicRoutingRule)
	for rows.Next() {
		var tenantID string
		var rulesJSON []byte

		if err := rows.Scan(&tenantID, &rulesJSON); err != nil {
			return nil, fmt.Errorf("scan routing rules: %w", err)
		}

		var rules []provisioning.TopicRoutingRule
		if err := json.Unmarshal(rulesJSON, &rules); err != nil {
			return nil, fmt.Errorf("unmarshal routing rules for tenant %s: %w", tenantID, err)
		}

		result[tenantID] = rules
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate routing rules: %w", err)
	}

	return result, nil
}

// Ensure PostgresRoutingRulesRepository implements RoutingRulesStore.
var _ provisioning.RoutingRulesStore = (*PostgresRoutingRulesRepository)(nil)
