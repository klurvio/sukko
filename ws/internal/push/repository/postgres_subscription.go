package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/lib/pq"
)

// postgresSubscriptionRepo implements SubscriptionRepository using PostgreSQL.
type postgresSubscriptionRepo struct {
	db *sql.DB
}

// NewPostgresSubscriptionRepository creates a SubscriptionRepository backed by PostgreSQL.
// Channels are stored as a TEXT[] array column.
func NewPostgresSubscriptionRepository(db *sql.DB) SubscriptionRepository {
	return &postgresSubscriptionRepo{db: db}
}

// Create inserts a new push subscription and returns its ID via RETURNING.
func (r *postgresSubscriptionRepo) Create(ctx context.Context, sub *PushSubscription) (int64, error) {
	query := `
		INSERT INTO push_subscriptions (tenant_id, principal, platform, token, endpoint, p256dh_key, auth_secret, channels)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id
	`

	var id int64
	err := r.db.QueryRowContext(ctx, query,
		sub.TenantID,
		sub.Principal,
		sub.Platform,
		sub.Token,
		sub.Endpoint,
		sub.P256dhKey,
		sub.AuthSecret,
		pq.Array(sub.Channels),
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert push subscription: %w", err)
	}

	return id, nil
}

// Delete removes a push subscription by ID.
func (r *postgresSubscriptionRepo) Delete(ctx context.Context, id int64, tenantID string) error {
	query := `DELETE FROM push_subscriptions WHERE id = $1 AND tenant_id = $2`

	_, err := r.db.ExecContext(ctx, query, id, tenantID)
	if err != nil {
		return fmt.Errorf("delete push subscription: %w", err)
	}

	return nil
}

// DeleteByToken removes a push subscription matching the tenant ID and device token or endpoint.
// For FCM/APNs, matches the token column. For Web Push, matches the endpoint column.
func (r *postgresSubscriptionRepo) DeleteByToken(ctx context.Context, tenantID, token string) error {
	query := `DELETE FROM push_subscriptions WHERE tenant_id = $1 AND (token = $2 OR endpoint = $2)`

	_, err := r.db.ExecContext(ctx, query, tenantID, token)
	if err != nil {
		return fmt.Errorf("delete push subscription by token: %w", err)
	}

	return nil
}

// FindByTenant returns all push subscriptions for the given tenant.
func (r *postgresSubscriptionRepo) FindByTenant(ctx context.Context, tenantID string) ([]PushSubscription, error) {
	query := `
		SELECT id, tenant_id, principal, platform, token, endpoint, p256dh_key, auth_secret, channels, created_at, last_success_at
		FROM push_subscriptions
		WHERE tenant_id = $1
	`

	rows, err := r.db.QueryContext(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query push subscriptions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var subs []PushSubscription
	for rows.Next() {
		var sub PushSubscription
		var lastSuccess sql.NullTime

		err := rows.Scan(
			&sub.ID,
			&sub.TenantID,
			&sub.Principal,
			&sub.Platform,
			&sub.Token,
			&sub.Endpoint,
			&sub.P256dhKey,
			&sub.AuthSecret,
			pq.Array(&sub.Channels),
			&sub.CreatedAt,
			&lastSuccess,
		)
		if err != nil {
			return nil, fmt.Errorf("scan push subscription: %w", err)
		}

		if lastSuccess.Valid {
			sub.LastSuccessAt = &lastSuccess.Time
		}

		subs = append(subs, sub)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate push subscriptions: %w", err)
	}

	return subs, nil
}

// UpdateLastSuccess sets the last_success_at timestamp to the current time.
func (r *postgresSubscriptionRepo) UpdateLastSuccess(ctx context.Context, id int64) error {
	query := `UPDATE push_subscriptions SET last_success_at = $1 WHERE id = $2`

	_, err := r.db.ExecContext(ctx, query, time.Now(), id)
	if err != nil {
		return fmt.Errorf("update last success: %w", err)
	}

	return nil
}
