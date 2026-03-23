package compose

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	healthPollInterval  = 2 * time.Second
	healthClientTimeout = 5 * time.Second
)

// HealthTarget represents a service to check health for.
type HealthTarget struct {
	Name string
	URL  string
}

// WaitForHealth polls health endpoints until all return 200 or the context is canceled.
func WaitForHealth(ctx context.Context, w io.Writer, targets []HealthTarget, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := &http.Client{Timeout: healthClientTimeout}

	pending := make(map[string]HealthTarget)
	for _, t := range targets {
		pending[t.Name] = t
	}

	ticker := time.NewTicker(healthPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			names := make([]string, 0, len(pending))
			for name := range pending {
				names = append(names, name)
			}
			return fmt.Errorf("health check timeout: services still unhealthy: %v", names)
		case <-ticker.C:
			for name, target := range pending {
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.URL, http.NoBody)
				if err != nil {
					continue
				}
				resp, err := client.Do(req)
				if err != nil {
					continue
				}
				_ = resp.Body.Close() // error from closing a read-only HTTP response body is always nil
				if resp.StatusCode == http.StatusOK {
					fmt.Fprintf(w, "  %s: healthy\n", name)
					delete(pending, name)
				}
			}
			if len(pending) == 0 {
				return nil
			}
		}
	}
}
