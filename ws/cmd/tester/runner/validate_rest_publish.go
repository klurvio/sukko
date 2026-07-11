package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/cmd/tester/metrics"
	"github.com/klurvio/sukko/cmd/tester/restpublish"
	"github.com/klurvio/sukko/cmd/tester/sse"
	testerws "github.com/klurvio/sukko/cmd/tester/ws"
	"github.com/klurvio/sukko/internal/shared/logging"
)

// oversizedBodySize exceeds the default GATEWAY_MAX_PUBLISH_SIZE (65536 bytes).
const oversizedBodySize = 65537

func validateRestPublish(ctx context.Context, run *TestRun, logger zerolog.Logger) ([]metrics.CheckResult, error) {
	provClient := run.authResult.ProvClient
	tenantID := run.authResult.TenantID
	sseChannel := tenantChannel(tenantID, validateSSEChannel)
	token := run.authResult.TokenFunc(0)

	// Step 0: Set routing rules and channel rules
	if err := provClient.SetRoutingRules(ctx, tenantID, testRoutingRules); err != nil {
		return []metrics.CheckResult{{Name: "setup routing rules", Status: "fail", Error: err.Error()}}, nil
	}
	_ = provClient.SetChannelRules(ctx, tenantID, testChannelRules)

	restClient := restpublish.NewClient(httpURL(run.Config.GatewayURL))
	auth := restpublish.AuthConfig{Token: token}

	// Routing rules reach ws-server's producer via an async gRPC snapshot stream. Post-#179
	// a publish before that snapshot arrives is a retryable 409 (no rule yet) or 503 (not
	// synced) — probe until routable so the delivery checks below aren't flaky.
	if err := waitForRoutable(ctx, restClient, sseChannel, auth); err != nil {
		return []metrics.CheckResult{{Name: "routing rules propagate", Status: "fail", Error: err.Error()}}, nil
	}

	var checks []metrics.CheckResult

	// Step 1: Connect WS subscriber + subscribe, wait for propagation
	var wsReceived struct {
		mu    sync.Mutex
		msgID string
	}

	wsClient, err := connectWithRetry(ctx, run.Config.GatewayURL, token, logger, func(msg testerws.Message) {
		if msg.Type != "publish" {
			return
		}
		var payload struct {
			MsgID string `json:"msg_id"`
		}
		if err := json.Unmarshal(msg.Data, &payload); err != nil || payload.MsgID == "" {
			return
		}
		wsReceived.mu.Lock()
		wsReceived.msgID = payload.MsgID
		wsReceived.mu.Unlock()
	})
	if err != nil {
		return []metrics.CheckResult{{
			Name: "ws subscriber connect", Status: "fail", Error: err.Error(),
		}}, nil
	}
	defer func() { _ = wsClient.Close() }()

	// Start ReadLoop so OnMessage fires
	go func() {
		defer logging.RecoverPanic(logger, "rest-publish-ws-readloop", nil)
		_, _ = wsClient.ReadLoop(ctx)
	}()

	if err := wsClient.Subscribe([]string{sseChannel}); err != nil {
		return []metrics.CheckResult{{
			Name: "ws subscribe", Status: "fail", Error: err.Error(),
		}}, nil
	}

	time.Sleep(500 * time.Millisecond) // subscription propagation

	// Step 2: Publish via REST → verify WS receives
	msgID := uuid.NewString()
	payload, _ := json.Marshal(map[string]any{
		"msg_id": msgID,
		"ts":     time.Now().UnixMilli(),
	})

	start := time.Now()
	_, pubErr := restClient.Publish(ctx, restpublish.Request{
		Channel: sseChannel,
		Data:    payload,
	}, auth)
	if pubErr != nil {
		run.Collector.RESTPublishErrors.Add(1)
		checks = append(checks, metrics.CheckResult{
			Name: "rest publish → ws receive", Status: "fail",
			Error: fmt.Sprintf("rest publish: %v", pubErr),
		})
	} else {
		run.Collector.RESTPublishSuccess.Add(1)

		// Wait for WS to receive the message
		received := waitForWSMessage(&wsReceived, msgID, defaultDeliveryTimeout)
		if received {
			checks = append(checks, metrics.CheckResult{
				Name: "rest publish → ws receive", Status: "pass",
				Latency: time.Since(start).Round(time.Millisecond).String(),
			})
		} else {
			checks = append(checks, metrics.CheckResult{
				Name: "rest publish → ws receive", Status: "fail",
				Error: "ws subscriber did not receive message within timeout",
			})
		}
	}

	// Step 3: Connect SSE subscriber, publish via REST → verify SSE receives
	sseClient, _, sseErr := sse.Connect(ctx, sse.ConnectConfig{
		GatewayURL: httpURL(run.Config.GatewayURL),
		Channels:   []string{validateSSEChannel},
		Token:      token,
		Logger:     logger,
	})
	if sseErr != nil {
		checks = append(checks, metrics.CheckResult{
			Name: "rest publish → sse receive", Status: "fail",
			Error: fmt.Sprintf("sse connect: %v", sseErr),
		})
	} else {
		defer func() { _ = sseClient.Close() }()

		time.Sleep(500 * time.Millisecond) // subscription propagation

		msgID2 := uuid.NewString()
		payload2, _ := json.Marshal(map[string]any{
			"msg_id": msgID2,
			"ts":     time.Now().UnixMilli(),
		})

		start2 := time.Now()
		_, pubErr2 := restClient.Publish(ctx, restpublish.Request{
			Channel: sseChannel,
			Data:    payload2,
		}, auth)
		if pubErr2 != nil {
			run.Collector.RESTPublishErrors.Add(1)
			checks = append(checks, metrics.CheckResult{
				Name: "rest publish → sse receive", Status: "fail",
				Error: fmt.Sprintf("rest publish: %v", pubErr2),
			})
		} else {
			run.Collector.RESTPublishSuccess.Add(1)

			// Read SSE events until the one carrying msgID2 arrives (or timeout). Filtering
			// by msg_id is required so unrelated traffic (e.g. the routing probe) cannot
			// vacuously satisfy this check.
			readCtx, cancel := context.WithTimeout(ctx, defaultDeliveryTimeout)
			received := false
			var readErr error
			for {
				var event *sse.Event
				event, readErr = sseClient.ReadEvent(readCtx)
				if readErr != nil {
					break
				}
				var payload struct {
					MsgID string `json:"msg_id"`
				}
				if json.Unmarshal([]byte(event.Data), &payload) == nil && payload.MsgID == msgID2 {
					received = true
					break
				}
			}
			cancel()

			if received {
				run.Collector.SSEMessagesReceived.Add(1)
				checks = append(checks, metrics.CheckResult{
					Name: "rest publish → sse receive", Status: "pass",
					Latency: time.Since(start2).Round(time.Millisecond).String(),
				})
			} else {
				checks = append(checks, metrics.CheckResult{
					Name: "rest publish → sse receive", Status: "fail",
					Error: fmt.Sprintf("sse subscriber did not receive msg_id=%s within timeout: %v", msgID2, readErr),
				})
			}
		}
	}

	// Step 4: Verify response format
	resp, err := restClient.Publish(ctx, restpublish.Request{
		Channel: sseChannel,
		Data:    json.RawMessage(`{"check":"format"}`),
	}, auth)
	if err != nil {
		checks = append(checks, metrics.CheckResult{
			Name: "response format", Status: "fail", Error: err.Error(),
		})
	} else {
		if resp.Status == "accepted" && resp.Channel == sseChannel {
			run.Collector.RESTPublishSuccess.Add(1)
			checks = append(checks, metrics.CheckResult{
				Name: "response format", Status: "pass",
			})
		} else {
			checks = append(checks, metrics.CheckResult{
				Name: "response format", Status: "fail",
				Error: fmt.Sprintf("status=%q channel=%q, want accepted/%s", resp.Status, resp.Channel, sseChannel),
			})
		}
	}

	// Steps 5-10: Error cases using PublishRaw
	errorCases := []struct {
		name        string
		body        []byte
		contentType string
		authCfg     restpublish.AuthConfig
		wantStatus  int
	}{
		{
			name:        "missing channel → 400",
			body:        []byte(`{"data":{"x":1}}`),
			contentType: "application/json",
			authCfg:     auth,
			wantStatus:  400,
		},
		{
			name:        "missing data → 400",
			body:        []byte(`{"channel":"general.test"}`),
			contentType: "application/json",
			authCfg:     auth,
			wantStatus:  400,
		},
		{
			name:        "invalid JSON → 400",
			body:        []byte(`not valid json`),
			contentType: "application/json",
			authCfg:     auth,
			wantStatus:  400,
		},
		{
			name:        "oversized body → 413",
			body:        []byte(`{"channel":"general.test","data":"` + strings.Repeat("x", oversizedBodySize) + `"}`),
			contentType: "application/json",
			authCfg:     auth,
			wantStatus:  413,
		},
		{
			name:        "no auth → 401",
			body:        []byte(`{"channel":"general.test","data":{"x":1}}`),
			contentType: "application/json",
			authCfg:     restpublish.AuthConfig{},
			wantStatus:  401,
		},
		{
			name:        "wrong content-type → 400",
			body:        []byte(`{"channel":"general.test","data":{"x":1}}`),
			contentType: "text/plain",
			authCfg:     auth,
			wantStatus:  400,
		},
	}

	for _, tc := range errorCases {
		status, _, rawErr := restClient.PublishRaw(ctx, tc.body, tc.authCfg, tc.contentType)
		if rawErr != nil {
			checks = append(checks, metrics.CheckResult{
				Name: tc.name, Status: "fail",
				Error: fmt.Sprintf("transport error: %v", rawErr),
			})
			continue
		}

		if status == tc.wantStatus {
			checks = append(checks, metrics.CheckResult{
				Name: tc.name, Status: "pass",
			})
		} else {
			run.Collector.RESTPublishErrors.Add(1)
			checks = append(checks, metrics.CheckResult{
				Name: tc.name, Status: "fail",
				Error: fmt.Sprintf("got HTTP %d, want %d", status, tc.wantStatus),
			})
		}
	}

	return checks, nil
}

// waitForRoutable publishes a probe message until the gateway accepts it, absorbing the
// routing-rules propagation window. Post-#179 an early publish is a retryable 409 (no
// applicable rule synced yet) or 503 (snapshot not received); any other status is a real
// failure and returns immediately. Bounded to ~2s.
func waitForRoutable(ctx context.Context, c *restpublish.Client, channel string, auth restpublish.AuthConfig) error {
	body, _ := json.Marshal(map[string]any{
		"channel": channel,
		"data":    map[string]any{"probe": true},
	})
	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	var lastStatus int
	for {
		status, _, err := c.PublishRaw(ctx, body, auth, "application/json")
		if err == nil && status < 300 {
			return nil
		}
		lastStatus = status
		// Only 409/503 are the retryable propagation states; anything else is a real failure.
		if status != http.StatusConflict && status != http.StatusServiceUnavailable {
			if err != nil {
				return fmt.Errorf("routing probe transport error (status=%d): %w", status, err)
			}
			return fmt.Errorf("routing probe rejected with status %d", status)
		}
		select {
		case <-deadline:
			return fmt.Errorf("routing rules did not propagate within timeout (last status=%d)", lastStatus)
		case <-ticker.C:
		}
	}
}

// waitForWSMessage polls until the WS subscriber receives the expected message ID.
func waitForWSMessage(received *struct {
	mu    sync.Mutex
	msgID string
}, expectedID string, timeout time.Duration) bool {
	deadline := time.After(timeout)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			return false
		case <-ticker.C:
			received.mu.Lock()
			got := received.msgID
			received.mu.Unlock()
			if got == expectedID {
				return true
			}
		}
	}
}
