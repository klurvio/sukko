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

// respTypeSubscribeError is the server→client frame type sent when a subscribe is rejected
// (e.g. every requested channel was filtered out by the gateway as unauthorized). Defined
// locally: the tester treats frame types as bare strings (ws/client.go) and imports no
// internal/server code — a local const avoids tester→server coupling (§X/§XVIII).
const respTypeSubscribeError = "subscribe_error"

// validateAPIKey runs the api-key validation suite.
// Validates that an API-key-only client can subscribe to public channels,
// is denied private channels, and cannot REST-publish.
// Uses run.apiKey (set by execute()) — does not call CreateAPIKey internally.
func validateAPIKey(ctx context.Context, run *TestRun, logger zerolog.Logger) ([]metrics.CheckResult, error) {
	var checks []metrics.CheckResult

	if run.apiKey == "" {
		return nil, errors.New("validateAPIKey: no api key configured (set TESTER_API_KEY or pass api_key in request)")
	}

	// Seed precise channel rules for this run's tenant. An API-key-only client has nil JWT
	// claims, so the gateway authorizes its subscribes against rules.Public *only*
	// (permissions_tenant.go). With the default permissive public:["*"] the private channel
	// would be allowed and check 3 ("private channel denied") could never be exercised — so
	// narrow public to just the suite's public channel. Nil-guarded like
	// seedDefaultChannelRules; a failure surfaces as a failing check, not a hard error
	// (the tester's error contract — §XVIII with validate.go).
	if run.authResult != nil && run.authResult.ProvClient != nil {
		if err := run.authResult.ProvClient.SetChannelRules(ctx, run.Config.TenantID, map[string]any{
			"public":         []string{validatePublicChannel},
			"default":        []string{"*"},
			"publish_public": []string{"*"},
		}); err != nil {
			checks = append(checks, metrics.CheckResult{
				Name:   "seed channel rules",
				Status: metrics.CheckStatusFail,
				Error:  fmt.Sprintf("seed channel rules: %v", err),
			})
			return checks, nil
		}
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
			// A denied subscribe surfaces as a subscribe_error frame (the gateway filters the
			// unauthorized channel, leaving an empty subscribe, and the server replies
			// subscribe_error). Match both it and generic error frames (§XVII protocol match).
			if msg.Type == "error" || msg.Type == respTypeSubscribeError {
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

	// Check 3: Subscribe to private channel — expect the gateway to deny it. The gateway
	// filters the unauthorized channel out, leaving an empty subscribe, and the server
	// replies with a subscribe_error frame (captured on errCh). A transport-level error
	// means unexpected disconnect (fail immediately).
	// Drain any stale frames first so a leftover error/subscribe_error cannot false-pass.
	for drained := false; !drained; {
		select {
		case <-errCh:
		default:
			drained = true
		}
	}
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
