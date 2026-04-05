// Package repository provides push subscription storage backed by PostgreSQL (pgxpool).
package repository

import (
	"context"
	"time"
)

// PushSubscription represents a device/browser push notification registration.
type PushSubscription struct {
	ID            int64
	TenantID      string
	Principal     string
	Platform      string   // "web", "android", "ios"
	Token         string   // FCM/APNs token (empty for web)
	Endpoint      string   // Web Push endpoint URL (empty for android/ios)
	P256dhKey     string   // Web Push ECDH public key
	AuthSecret    string   // Web Push auth secret
	Channels      []string // Channel patterns WITH tenant prefix
	CreatedAt     time.Time
	LastSuccessAt *time.Time // nullable
}

// SubscriptionRepository defines the operations for managing push subscriptions.
type SubscriptionRepository interface {
	// Create inserts a new push subscription and returns its ID.
	Create(ctx context.Context, sub *PushSubscription) (int64, error)

	// Delete removes a push subscription by ID, scoped to tenant for isolation.
	Delete(ctx context.Context, id int64, tenantID string) error

	// DeleteByToken removes a push subscription by tenant ID and device token.
	DeleteByToken(ctx context.Context, tenantID, token string) error

	// FindByTenant returns all push subscriptions for a given tenant.
	FindByTenant(ctx context.Context, tenantID string) ([]PushSubscription, error)

	// UpdateLastSuccess updates the last_success_at timestamp to now.
	UpdateLastSuccess(ctx context.Context, id int64) error
}
