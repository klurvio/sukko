package runner

import (
	"context"
	"net/http"
	"time"

	"github.com/klurvio/sukko/cmd/tester/auth"
	"github.com/klurvio/sukko/cmd/tester/metrics"
	"github.com/rs/zerolog"
)

// healthCheckTimeout is the HTTP client timeout for dependency health checks.
const healthCheckTimeout = 5 * time.Second

func runSmoke(ctx context.Context, run *TestRun, logger zerolog.Logger) (*metrics.Report, error) {
	var checks []metrics.CheckResult

	// Check 1: WebSocket connectivity (with retry for key registry cache race)
	start := time.Now()
	client, err := connectWithRetry(ctx, run.Config.GatewayURL, run.authResult.TokenFunc(0), logger)
	connectLatency := time.Since(start)

	if err != nil {
		checks = append(checks, metrics.CheckResult{
			Name:   "connectivity",
			Status: "fail",
			Error:  err.Error(),
		})
	} else {
		checks = append(checks, metrics.CheckResult{
			Name:    "connectivity",
			Status:  "pass",
			Latency: connectLatency.Round(time.Millisecond).String(),
		})
		run.Collector.ConnectionsTotal.Add(1)
		run.Collector.ConnectionsActive.Add(1)
		defer func() {
			_ = client.Close() // best-effort: test cleanup
			run.Collector.ConnectionsActive.Add(-1)
		}()
	}

	// Check 2: Dependency health
	const healthPath = "/health"
	depChecks := []struct {
		name string
		url  string
	}{
		{"provisioning", run.Config.ProvisioningURL + healthPath},
		{"gateway", httpURL(run.Config.GatewayURL) + healthPath},
	}

	httpClient := &http.Client{Timeout: healthCheckTimeout}
	for _, dep := range depChecks {
		start := time.Now()
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, dep.url, http.NoBody)
		if reqErr != nil {
			checks = append(checks, metrics.CheckResult{
				Name:   dep.name + " health",
				Status: "fail",
				Error:  reqErr.Error(),
			})
			continue
		}
		resp, err := httpClient.Do(req)
		latency := time.Since(start)

		if err != nil {
			checks = append(checks, metrics.CheckResult{
				Name:   dep.name + " health",
				Status: "fail",
				Error:  err.Error(),
			})
		} else {
			_ = resp.Body.Close() // response consumed by status code check only
			status := "pass"
			if resp.StatusCode != http.StatusOK {
				status = "fail"
			}
			checks = append(checks, metrics.CheckResult{
				Name:    dep.name + " health",
				Status:  status,
				Latency: latency.Round(time.Millisecond).String(),
			})
		}
	}

	// Check 3: Subscribe + receive
	if client != nil {
		testChannel := smokeTestChannel
		start := time.Now()
		if err := client.Subscribe([]string{testChannel}); err != nil {
			checks = append(checks, metrics.CheckResult{
				Name:   "subscribe",
				Status: "fail",
				Error:  err.Error(),
			})
		} else {
			checks = append(checks, metrics.CheckResult{
				Name:    "subscribe",
				Status:  "pass",
				Latency: time.Since(start).Round(time.Millisecond).String(),
			})
		}
	}

	// Check 4: Publish round-trip — verify actual message delivery
	// Uses PubSubEngine to create a fresh connection with message tracking.
	if run.authResult != nil && run.authResult.ProvClient != nil {
		// Setup: add catch-all routing rule for publish to work.
		// Error ignored: if this fails, the publish round-trip check below will
		// fail with "message not received within timeout" — no silent degradation.
		_ = run.authResult.ProvClient.SetRoutingRules(ctx, run.authResult.TenantID, []map[string]any{
			{"pattern": "*.*", "topic_suffix": "smoke-test"},
		})

		engine := NewPubSubEngine(PubSubEngineConfig{
			GatewayURL: run.Config.GatewayURL,
			Logger:     logger,
		})

		smokeUser, createErr := engine.CreateUser(ctx, run.authResult.Minter, auth.MintOptions{
			ConnIndex: 99,
			Subject:   "smoke-pubsub",
		})
		if createErr != nil {
			checks = append(checks, metrics.CheckResult{
				Name: "publish round-trip", Status: "fail", Error: createErr.Error(),
			})
		} else {
			defer func() { _ = smokeUser.Client.Close() }()

			if subErr := smokeUser.Client.Subscribe([]string{smokeTestChannel}); subErr != nil {
				checks = append(checks, metrics.CheckResult{
					Name: "publish round-trip", Status: "fail", Error: subErr.Error(),
				})
			} else {
				time.Sleep(200 * time.Millisecond) // allow subscription to propagate
				result := engine.PublishAndVerify(ctx, smokeUser, smokeTestChannel, []*TestUser{smokeUser}, []*TestUser{smokeUser})
				if result.Delivered {
					checks = append(checks, metrics.CheckResult{
						Name:    "publish round-trip",
						Status:  "pass",
						Latency: result.Latency.Round(time.Millisecond).String(),
					})
				} else {
					checks = append(checks, metrics.CheckResult{
						Name:   "publish round-trip",
						Status: "fail",
						Error:  "message not received within timeout",
					})
				}
			}
		}
	}

	// Determine overall status
	overallStatus := "pass"
	for _, c := range checks {
		if c.Status == "fail" {
			overallStatus = "fail"
			break
		}
	}

	return &metrics.Report{
		TestType: "smoke",
		Status:   overallStatus,
		Metrics:  run.Collector.Snapshot(),
		Checks:   checks,
	}, nil
}

func httpURL(wsURL string) string {
	if len(wsURL) > 5 && wsURL[:5] == "ws://" {
		return "http://" + wsURL[5:]
	}
	if len(wsURL) > 6 && wsURL[:6] == "wss://" {
		return "https://" + wsURL[6:]
	}
	return wsURL
}
