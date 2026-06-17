package webhook

import "time"

// webhookRetrySchedule is the fixed backoff schedule for retriable delivery failures.
// Index 0 = delay before 1st retry, index 4 = delay before 5th retry.
// For max_retries > 5, retries beyond index 4 reuse the last entry (10m).
// Constitution §IV exception: application-layer webhook delivery uses a fixed schedule
// (not exponential) — matching industry standard (Stripe, GitHub, Pusher, Ably).
// See CLAUDE.md §IV amendment and spec FR-006.
var webhookRetrySchedule = [5]time.Duration{
	1 * time.Second,
	5 * time.Second,
	30 * time.Second,
	2 * time.Minute,
	10 * time.Minute,
}

// retryDelay returns the backoff duration for the given retry attempt number (1-indexed).
// attempt=1 → webhookRetrySchedule[0], attempt≥6 → webhookRetrySchedule[4].
// Bounds-checked to prevent index-out-of-range on any max_retries value.
func retryDelay(attempt int) time.Duration {
	idx := max(attempt-1, 0)
	if idx >= len(webhookRetrySchedule) {
		idx = len(webhookRetrySchedule) - 1
	}
	return webhookRetrySchedule[idx]
}

const (
	// WebhookDegradedRetryInterval is the delay between delivery attempts
	// once a webhook has exhausted all retries and entered degraded status.
	// Retries continue indefinitely at this interval until operator intervention.
	WebhookDegradedRetryInterval = time.Hour

	// WebhookTestResponsePreviewBytes is the maximum response body bytes
	// returned in the POST /webhooks/{id}/test API response preview.
	WebhookTestResponsePreviewBytes = 512

	// WebhookDowngradeGracePeriod is the time after a license downgrade
	// before all webhooks are suspended. This is a product policy constant —
	// not an operator-configurable env var (Constitution §XV KISS).
	WebhookDowngradeGracePeriod = 24 * time.Hour
)

// Rate-limit constants for POST /webhooks/{id}/test endpoint.
// Exported so the test handler in provisioning/api can import them from this package (§X).
const (
	WebhookTestRateLimitPerWebhook = 10
	WebhookTestRateLimitPerTenant  = 30
	WebhookTestRateLimitWindow     = 60 * time.Second
	WebhookTestRLKeyPrefix         = "webhook_test_rl:"
	WebhookTestRLTenantKeyPrefix   = "webhook_test_rl_tenant:"
)
