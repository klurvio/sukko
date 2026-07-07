package runner

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/klurvio/sukko/cmd/tester/metrics"
	"github.com/klurvio/sukko/cmd/tester/restpublish"
	testerws "github.com/klurvio/sukko/cmd/tester/ws"
	"github.com/klurvio/sukko/internal/shared/logging"
	"github.com/rs/zerolog"
)

// validateAPIKey runs the api-key validation suite.
// Validates that an API-key-only client can subscribe to public channels,
// is denied private channels, and cannot REST-publish.
// Uses run.apiKey (set by execute()) — does not call CreateAPIKey internally.
func validateAPIKey(ctx context.Context, run *TestRun, logger zerolog.Logger) ([]metrics.CheckResult, error) {
	var checks []metrics.CheckResult

	if run.apiKey == "" {
		return nil, errors.New("validateAPIKey: no api key configured (set TESTER_API_KEY or pass api_key in request)")
	}

	// errCh receives application-layer error frames (type=="error") from the gateway.
	// Buffered(1) so the ReadLoop goroutine is never blocked sending to it (§VII channels rule).
	errCh := make(chan testerws.Message, 1)

	// Check 1: Connect with API key
	client, err := testerws.Connect(ctx, testerws.ConnectConfig{
		GatewayURL: run.Config.GatewayURL,
		APIKey:     run.apiKey,
		Logger:     logger.With().Str("suite", "api-key").Logger(),
		OnMessage: func(msg testerws.Message) {
			if msg.Type == "error" {
				select {
				case errCh <- msg:
				default:
				}
				return
			}
			run.Collector.MessagesReceived.Add(1)
		},
	})
	if err != nil {
		checks = append(checks, metrics.CheckResult{
			Name:   "api key accepted",
			Status: metrics.CheckStatusFail,
			Error:  fmt.Sprintf("connect: %v", err),
		})
		return checks, nil
	}
	run.Collector.ConnectionsAPIKey.Add(1)
	run.Collector.ConnectionsTotal.Add(1)
	run.Collector.ConnectionsActive.Add(1)
	var readWg sync.WaitGroup
	readWg.Go(func() {
		defer logging.RecoverPanic(logger, "validate_api_key_readloop", nil)
		_, _ = client.ReadLoop(ctx)
	})
	defer func() {
		_ = client.Close()
		readWg.Wait()
		run.Collector.ConnectionsActive.Add(-1)
	}()

	checks = append(checks, metrics.CheckResult{
		Name:   "api key accepted",
		Status: metrics.CheckStatusPass,
	})

	// Check 2: Subscribe to public channel
	if err := client.Subscribe([]string{tenantChannel(run.Config.TenantID, validatePublicChannel)}); err != nil {
		checks = append(checks, metrics.CheckResult{
			Name:   "public channel subscribe",
			Status: metrics.CheckStatusFail,
			Error:  fmt.Sprintf("subscribe: %v", err),
		})
		return checks, nil
	}
	checks = append(checks, metrics.CheckResult{
		Name:   "public channel subscribe",
		Status: metrics.CheckStatusPass,
	})

	// Check 3: Subscribe to private channel — expect the gateway to deny with an error frame.
	// A transport-level error means unexpected disconnect (fail immediately).
	// Correct behavior: connection stays open, server sends type=="error".
	privateChannel := run.Config.TenantID + privateChannelSuffix
	if subErr := client.Subscribe([]string{privateChannel}); subErr != nil {
		checks = append(checks, metrics.CheckResult{
			Name:   "private channel denied",
			Status: metrics.CheckStatusFail,
			Error:  fmt.Sprintf("unexpected transport error on private subscribe: %v", subErr),
		})
	} else {
		denyTimeout := run.authUpgradeTimeout
		if denyTimeout <= 0 {
			denyTimeout = 3 * time.Second
		}
		denyCtx, denyCancel := context.WithTimeout(ctx, denyTimeout)
		select {
		case <-errCh:
			denyCancel()
			checks = append(checks, metrics.CheckResult{
				Name:   "private channel denied",
				Status: metrics.CheckStatusPass,
			})
		case <-denyCtx.Done():
			denyCancel()
			if ctx.Err() != nil {
				// Parent context canceled (test stopped) — not a genuine check failure.
				return checks, nil
			}
			checks = append(checks, metrics.CheckResult{
				Name:   "private channel denied",
				Status: metrics.CheckStatusFail,
				Error:  "timed out waiting for gateway to deny private channel subscription",
			})
		}
	}

	// Check 4: REST publish with API key — expect HTTP 403 FORBIDDEN.
	// API keys cannot REST-publish; the gateway rejects with 403.
	// 403 here is the expected success outcome — do NOT increment RESTPublishErrors.
	restStatus, _, restErr := restpublish.NewClient(httpURL(run.Config.GatewayURL)).PublishRaw(
		ctx, validPublishBody, restpublish.AuthConfig{APIKey: run.apiKey}, "application/json",
	)
	switch {
	case restErr != nil:
		checks = append(checks, metrics.CheckResult{
			Name:   "api key REST publish blocked",
			Status: metrics.CheckStatusFail,
			Error:  fmt.Sprintf("REST publish request failed: %v", restErr),
		})
	case restStatus == http.StatusForbidden:
		checks = append(checks, metrics.CheckResult{
			Name:   "api key REST publish blocked",
			Status: metrics.CheckStatusPass,
		})
	default:
		run.Collector.RESTPublishErrors.Add(1)
		checks = append(checks, metrics.CheckResult{
			Name:   "api key REST publish blocked",
			Status: metrics.CheckStatusFail,
			Error:  fmt.Sprintf("expected HTTP 403, got %d", restStatus),
		})
	}

	return checks, nil
}
