package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/prometheus/common/expfmt"
)

// metricsHTTPClient is a dedicated client for Prometheus scrapes with a bounded timeout.
var metricsHTTPClient = &http.Client{Timeout: editionHTTPTimeout}

// revocationReconnectBackoffs is the retry backoff schedule for HTTP revocation API calls.
// Callers use withRetry with this schedule for transient 429/5xx errors.
var revocationReconnectBackoffs = []time.Duration{500 * time.Millisecond, 1 * time.Second, 2 * time.Second}

// revocationSettlementWindow is the time the stress runner waits after mass-revocation for
// gateway state to fully settle before verifying Prometheus counter deltas.
const revocationSettlementWindow = 500 * time.Millisecond

// defaultSoakRevocationsPerCycle is the number of connections revoked per soak cycle when
// RevocationsPerCycle is 0. Not configurable via env var per NFR-002.
const defaultSoakRevocationsPerCycle = 10

// revokeRequest is the JSON body for POST /api/v1/tenants/{tenantID}/tokens/revoke.
// Exp is the Unix timestamp for the gateway pruner TTL (S4 only — omit for normal revocations).
type revokeRequest struct {
	Sub string `json:"sub,omitempty"`
	JTI string `json:"jti,omitempty"`
	Exp *int64 `json:"exp,omitempty"`
}

// revokeTokenWithClient sends a token revocation request using the provided HTTP client.
// Returns (statusCode, nil) on any completed HTTP exchange — callers must inspect the status code.
// Returns (0, err) on network/request-build errors only.
func revokeTokenWithClient(ctx context.Context, client *http.Client, baseURL, token, tenantID string, req revokeRequest) (int, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return 0, fmt.Errorf("revokeToken: marshal: %w", err)
	}
	// baseURL is normalized to http(s):// (httpURL is a no-op on an already-http URL). The revoke
	// route is served ONLY by the provisioning admin API — the gateway does not proxy it. The validate
	// suite passes the provisioning base URL (correct). soak/stress still pass the gateway URL, so their
	// revokes hit an unregistered gateway route (404); that pre-existing mis-targeting is tracked in #199.
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		httpURL(baseURL)+"/api/v1/tenants/"+tenantID+"/tokens/revoke",
		bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("revokeToken: new request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(httpReq)
	if err != nil {
		return 0, fmt.Errorf("revokeToken: do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // drain body
	return resp.StatusCode, nil
}

// revokeToken sends a token revocation request to the provisioning admin API
// (POST /api/v1/tenants/{tenantID}/tokens/revoke — not proxied by the gateway).
func revokeToken(ctx context.Context, baseURL, token, tenantID string, req revokeRequest) (int, error) {
	return revokeTokenWithClient(ctx, &http.Client{Timeout: editionHTTPTimeout}, baseURL, token, tenantID, req)
}

// isErrorRateExceeded returns true when the error rate strictly exceeds the threshold.
// When total is 0, the rate is 0.0 and the function returns false.
func isErrorRateExceeded(errors, total int, threshold float64) bool {
	if total == 0 {
		return false
	}
	return float64(errors)/float64(total) > threshold
}

// pollGaugesUntil polls scrapeFn at interval until predicate(value) is true or timeout fires.
// Returns the first value that satisfies the predicate, or an error.
// scrapeFn is injectable for testing — use scrapeGatewayMetrics in production.
func pollGaugesUntil(
	ctx context.Context,
	scrapeFn func(ctx context.Context) (map[string]float64, error),
	metricName string,
	predicate func(float64) bool,
	interval, timeout time.Duration,
) (float64, error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	var lastErr error
	for {
		select {
		case <-ctx.Done():
			return 0, fmt.Errorf("poll metric %s: %w", metricName, ctx.Err())
		case <-deadline.C:
			if lastErr != nil {
				return 0, fmt.Errorf("timeout after %s, last scrape error: %w", timeout, lastErr)
			}
			return 0, fmt.Errorf("timeout after %s: %s did not satisfy predicate", timeout, metricName)
		case <-ticker.C:
			gauges, err := scrapeFn(ctx)
			if err != nil {
				lastErr = err
				continue
			}
			v := gauges[metricName]
			if predicate(v) {
				return v, nil
			}
		}
	}
}

// scrapeGatewayMetrics fetches and parses the Prometheus text-format /metrics endpoint.
// Returns a flat map of metric_name → total value (summing across ALL label combinations for
// CounterVec/GaugeVec families). Uses expfmt for correct label-aware parsing — do not use a
// naive line scanner.
func scrapeGatewayMetrics(ctx context.Context, metricsURL string) (map[string]float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metricsURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("scrapeGatewayMetrics: build request: %w", err)
	}
	req.Header.Set("Accept", string(expfmt.NewFormat(expfmt.TypeTextPlain)))

	resp, err := metricsHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("scrapeGatewayMetrics: do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scrapeGatewayMetrics: HTTP %d from %s", resp.StatusCode, metricsURL)
	}

	var parser expfmt.TextParser
	mfs, err := parser.TextToMetricFamilies(resp.Body)
	// expfmt returns partial results + a non-nil error for trailing comment/EOF lines
	// (common in real exporters). Only fail when the result is completely empty.
	if err != nil && len(mfs) == 0 {
		return nil, fmt.Errorf("scrapeGatewayMetrics: parse: %w", err)
	}

	result := make(map[string]float64, len(mfs))
	for name, mf := range mfs {
		var total float64
		for _, m := range mf.GetMetric() {
			if c := m.GetCounter(); c != nil {
				total += c.GetValue()
			} else if g := m.GetGauge(); g != nil {
				total += g.GetValue()
			}
		}
		result[name] = total
	}
	return result, nil
}

// Readiness-gate bounds for the revocation stream (FR-007).
const (
	revocationStreamReadyTimeout  = 30 * time.Second
	revocationStreamReadyInterval = 500 * time.Millisecond
)

// revocationStreamStateMetric is the gateway gauge that reports 1 when the WatchTokenRevocations
// stream is connected (0 when disconnected/Unimplemented).
const revocationStreamStateMetric = "gateway_token_revocation_stream_state"

// waitForRevocationStreamReady blocks until the gateway's revocation stream reports connected (== 1),
// so the suite never revokes before the gateway has received its snapshot (FR-007 — the cold-start
// guard, mirroring the tenant-isolation warmup). Reuses the existing expfmt-based scrape/poll helpers
// (no new metric parser). Returns an error on timeout or if the metrics endpoint is unreachable.
// metricsBaseURL is the gateway metrics BASE (e.g. http://ws-gateway:3000); the "/metrics" path is
// appended here, matching how soak_revocation/stress_revocation use run.Config.GatewayMetricsURL.
func waitForRevocationStreamReady(ctx context.Context, metricsBaseURL string) error {
	_, err := pollGaugesUntil(ctx,
		func(ctx context.Context) (map[string]float64, error) {
			return scrapeGatewayMetrics(ctx, metricsBaseURL+"/metrics")
		},
		revocationStreamStateMetric,
		func(v float64) bool { return v == 1 },
		revocationStreamReadyInterval, revocationStreamReadyTimeout,
	)
	return err
}
