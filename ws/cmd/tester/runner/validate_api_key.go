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

// Private-channel deny-wait tuning (#216). Deliberately named constants, not env vars: an
// internal test-driver robustness bound has no per-deployment tuning need (same reasoning as
// the hardcoded webhook retry schedule). Decoupled from TESTER_AUTH_UPGRADE_TIMEOUT — this
// wait has no auth-upgrade handshake; borrowing that knob was a dual-purpose value (§XV).
const (
	// apiKeyDenyDeadline bounds the whole deny wait. Preserves the previous effective 10s
	// bound, so the common prompt-deny case is not slower than before.
	apiKeyDenyDeadline = 10 * time.Second
	// apiKeyDenyRetryInterval is the fixed re-subscribe cadence. A send happens only while
	// send_time + interval ≤ deadline, so every attempt's deny has a full round-trip window
	// before the deadline — at production values that is exactly 3 attempts (t=0, 3s, 6s;
	// the t=9s send is suppressed). Fixed interval (not exponential backoff) is intentional:
	// §IV's backoff mandate covers infrastructure reconnection, not a test-driver poll, and a
	// predictable cadence keeps the window arithmetic and the deterministic unit tests sound.
	apiKeyDenyRetryInterval = 3 * time.Second
)

// privateDenyOutcome classifies how the private-channel deny wait ended.
type privateDenyOutcome int

const (
	denyOutcomeDenied         privateDenyOutcome = iota // a deny frame arrived — the check's success signal
	denyOutcomeTimedOut                                 // deadline elapsed with no deny — hard fail
	denyOutcomeTransportError                           // a subscribe write failed with no pending deny and a live parent ctx — hard fail
	denyOutcomeCancelled                                // parent context canceled — clean short-circuit, no result recorded
)

// waitForPrivateDeny sends the private-channel subscribe and waits for the gateway's
// asynchronous deny frame, re-sending on a bounded fixed interval so a single slow or lost
// deny round-trip under load cannot flake the check (#216). Re-subscribing to an unauthorized
// channel is idempotent — every attempt is filtered and yields another deny frame.
//
// deadline and interval come from the TestRun fields (seeded unconditionally in execute()
// from the consts above; tests inject small values) — they are authoritative, so there is
// deliberately NO <= 0 fallback here.
func waitForPrivateDeny(ctx context.Context, subscribe func() error, errCh <-chan testerws.Message, deadline, interval time.Duration, logger zerolog.Logger) (privateDenyOutcome, error) {
	start := time.Now()
	denyCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	// transportExit is the single exit path for a failed subscribe write (initial or retry).
	// It performs exactly ONE non-blocking errCh receive — the only permitted mid-wait
	// non-blocking read (FR-008): a buffered deny wins over the transport error because the
	// check has already observed its success signal; consuming it is the desired outcome, so
	// the discard hazard that forbids mid-wait drains does not apply. Then the parent-ctx
	// guard: a write that failed because the battery is tearing down must not record a
	// spurious FAIL.
	transportExit := func(sendErr error) (privateDenyOutcome, error) {
		select {
		case <-errCh:
			return denyOutcomeDenied, nil
		default:
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return denyOutcomeCancelled, fmt.Errorf("private deny wait canceled: %w", ctxErr) // outcome is the discriminator; the wrapped ctx error is informational
		}
		return denyOutcomeTransportError, sendErr
	}

	attempt := 1
	logger.Debug().Int("attempt", attempt).Msg("private deny wait: subscribe sent")
	if err := subscribe(); err != nil {
		return transportExit(err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-errCh:
			logger.Debug().Int("attempt", attempt).Bool("saved_by_retry", attempt > 1).
				Dur("elapsed", time.Since(start)).Msg("private deny received")
			return denyOutcomeDenied, nil
		case <-ticker.C:
			// Windowed send rule: only re-subscribe while this attempt's deny still has a
			// full round-trip window before the deadline — a send at deadline−ε would just
			// reproduce the flake on the final attempt.
			if elapsed := time.Since(start); elapsed+interval <= deadline {
				attempt++
				logger.Debug().Int("attempt", attempt).Dur("elapsed", elapsed).Msg("private deny wait: re-subscribe sent")
				if err := subscribe(); err != nil {
					return transportExit(err)
				}
			}
		case <-denyCtx.Done():
			// Deny-wins at the boundary too: a deny buffered exactly as the deadline fires must
			// not be discarded by select's pseudo-random tie-break (that would reintroduce a
			// narrow #216). Probe errCh before declaring timeout — same principle as transportExit.
			select {
			case <-errCh:
				return denyOutcomeDenied, nil
			default:
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return denyOutcomeCancelled, fmt.Errorf("private deny wait canceled: %w", ctxErr) // outcome is the discriminator; the wrapped ctx error is informational
			}
			logger.Warn().Int("attempts", attempt).Dur("elapsed", time.Since(start)).
				Msg("private deny wait: deadline elapsed with no deny")
			return denyOutcomeTimedOut, nil
		}
	}
}

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

	suiteLogger := logger.With().Str("suite", "api-key").Logger()

	// Check 1: Connect with API key
	client, err := testerws.Connect(ctx, testerws.ConnectConfig{
		GatewayURL: run.Config.GatewayURL,
		APIKey:     run.apiKey,
		Logger:     suiteLogger,
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
	// replies with a subscribe_error frame (captured on errCh). The deny is asynchronous, so
	// a single subscribe-then-wait flaked under load (#216) — waitForPrivateDeny re-sends the
	// subscribe on a bounded interval until a deny arrives or the deadline elapses.
	// Drain any stale frames first so a leftover error/subscribe_error cannot false-pass.
	// The drain runs exactly once, HERE, never mid-wait — a mid-wait discard-drain could race
	// a real deny into its default branch and throw it away (the deny-wins probe inside
	// waitForPrivateDeny is the only permitted mid-wait non-blocking receive).
	for drained := false; !drained; {
		select {
		case <-errCh:
		default:
			drained = true
		}
	}
	privateChannel := run.Config.TenantID + privateChannelSuffix
	outcome, denyErr := waitForPrivateDeny(ctx,
		func() error { return client.Subscribe([]string{privateChannel}) },
		errCh, run.apiKeyDenyDeadline, run.apiKeyDenyRetryInterval, suiteLogger)
	switch outcome {
	case denyOutcomeDenied:
		checks = append(checks, metrics.CheckResult{
			Name:   "private channel denied",
			Status: metrics.CheckStatusPass,
		})
	case denyOutcomeCancelled:
		// Parent context canceled (test stopped) — not a genuine check failure.
		return checks, nil
	case denyOutcomeTransportError:
		checks = append(checks, metrics.CheckResult{
			Name:   "private channel denied",
			Status: metrics.CheckStatusFail,
			Error:  fmt.Sprintf("unexpected transport error on private subscribe: %v", denyErr),
		})
	case denyOutcomeTimedOut:
		checks = append(checks, metrics.CheckResult{
			Name:   "private channel denied",
			Status: metrics.CheckStatusFail,
			Error:  "timed out waiting for gateway to deny private channel subscription",
		})
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
