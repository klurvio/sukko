package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/rs/zerolog"
)

// HealthChecker checks the health of the target server
type HealthChecker struct {
	url      string
	interval time.Duration
	client   *http.Client
	logger   zerolog.Logger
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(url string, interval time.Duration, logger zerolog.Logger) *HealthChecker {
	return &HealthChecker{
		url:      url,
		interval: interval,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
		logger: logger,
	}
}

// Check performs a single health check
func (h *HealthChecker) Check(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("health request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned status %d", resp.StatusCode)
	}

	return nil
}

// Run runs the health checker in a loop until context is cancelled
func (h *HealthChecker) Run(ctx context.Context) {
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := h.Check(ctx); err != nil {
				h.logger.Warn().Err(err).Str("url", h.url).Msg("Health check failed")
			} else {
				h.logger.Debug().Str("url", h.url).Msg("Health check passed")
			}
		}
	}
}

// WaitForHealthy waits for the server to become healthy or times out
func (h *HealthChecker) WaitForHealthy(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for server to become healthy")
		case <-ticker.C:
			if err := h.Check(ctx); err == nil {
				return nil
			}
		}
	}
}
