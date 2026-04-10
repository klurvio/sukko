package runner

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/cmd/tester/metrics"
	"github.com/klurvio/sukko/cmd/tester/restpublish"
)

// pushTestChannel is the channel pattern used by push validation (with tenant prefix).
const pushTestChannel = "general.push-test"

func validatePush(ctx context.Context, run *TestRun, logger zerolog.Logger) ([]metrics.CheckResult, error) { //nolint:unparam // matches validate suite function signature
	provClient := run.authResult.ProvClient
	tenantID := run.authResult.TenantID
	token := run.authResult.TokenFunc(0)
	gwURL := run.Config.GatewayURL

	var checks []metrics.CheckResult
	var deviceIDs []int64 // track for cleanup

	// Cleanup: reverse creation order — subscriptions, channels, credentials.
	defer func() { //nolint:contextcheck // intentional: use background context so cleanup survives parent cancellation
		cleanupCtx := context.Background()
		for _, id := range deviceIDs {
			if _, err := pushUnsubscribe(cleanupCtx, gwURL, token, id); err != nil {
				logger.Debug().Err(err).Int64("device_id", id).Msg("push cleanup: unsubscribe failed")
			}
		}
		if err := provClient.DeletePushChannels(cleanupCtx, tenantID); err != nil {
			logger.Debug().Err(err).Msg("push cleanup: delete channels failed")
		}
		if err := provClient.DeletePushCredentials(cleanupCtx, tenantID, "vapid"); err != nil {
			logger.Debug().Err(err).Msg("push cleanup: delete credentials failed")
		}
	}()

	// Step 0: Health probe — verify push infrastructure is available
	_, vapidStatus, vapidErr := pushGetVAPIDKey(ctx, gwURL, token)
	if vapidErr != nil {
		switch vapidStatus {
		case 503:
			return []metrics.CheckResult{{
				Name: "push available", Status: "skip",
				Error: "push service not deployed (503)",
			}}, nil
		case 403:
			return []metrics.CheckResult{{
				Name: "push available", Status: "skip",
				Error: "push requires Enterprise edition (403)",
			}}, nil
		default:
			return []metrics.CheckResult{{
				Name: "push available", Status: "fail",
				Error: fmt.Sprintf("VAPID key probe failed: HTTP %d: %v", vapidStatus, vapidErr),
			}}, nil
		}
	}
	checks = append(checks, metrics.CheckResult{
		Name: "push available", Status: "pass",
	})

	// Step 1: Setup — set VAPID credentials + channel config
	// Note: GetVAPIDKey in step 0 auto-generates credentials. SetPushCredentials overwrites via upsert.
	vapidCreds := `{"public_key":"BGxGBnqX_test_key","private_key":"OISg2_test_private"}`
	if err := provClient.SetPushCredentials(ctx, tenantID, "vapid", vapidCreds); err != nil {
		checks = append(checks, metrics.CheckResult{
			Name: "credential create", Status: "fail", Error: err.Error(),
		})
	} else {
		checks = append(checks, metrics.CheckResult{
			Name: "credential create", Status: "pass",
		})
	}

	pushChannel := tenantID + "." + pushTestChannel
	if err := provClient.SetPushChannels(ctx, tenantID, []string{pushChannel}, 2419200, "normal"); err != nil {
		checks = append(checks, metrics.CheckResult{
			Name: "channel config create", Status: "fail", Error: err.Error(),
		})
	} else {
		checks = append(checks, metrics.CheckResult{
			Name: "channel config create", Status: "pass",
		})
	}

	// Step 2: Register web subscription
	subID := uuid.NewString()[:8]
	deviceID, _, subErr := pushSubscribe(ctx, gwURL, token, pushSubscribeRequest{
		Platform:   "web",
		Endpoint:   "https://push.example.com/test-" + subID,
		P256dhKey:  "BNcRdreALRFXTkOOUHK1EtK2wtaz5Ry4YfYCA_0QTpQtUbVlUls0VJXg7A8u-Ts1XbjhazAkj7I99e8p8aE48",
		AuthSecret: "tBHItJI5svbpC7CIKw9w2A",
		Channels:   []string{pushChannel},
	})
	if subErr != nil {
		checks = append(checks, metrics.CheckResult{
			Name: "push subscribe", Status: "fail", Error: subErr.Error(),
		})
	} else {
		deviceIDs = append(deviceIDs, deviceID)
		checks = append(checks, metrics.CheckResult{
			Name: "push subscribe", Status: "pass",
		})
	}

	// Step 3: Publish message — verify 200 accepted
	restClient := restpublish.NewClient(gwURL)
	msgPayload, _ := json.Marshal(map[string]any{ // json.Marshal on literal map of primitives cannot fail
		"msg_id": "push-test-" + uuid.NewString()[:8],
		"title":  "Test Push",
		"body":   "Push validation test message",
	})
	_, pubErr := restClient.Publish(ctx, restpublish.Request{
		Channel: pushChannel,
		Data:    msgPayload,
	}, restpublish.AuthConfig{Token: token})
	if pubErr != nil {
		checks = append(checks, metrics.CheckResult{
			Name: "push publish accepted", Status: "fail", Error: pubErr.Error(),
		})
	} else {
		checks = append(checks, metrics.CheckResult{
			Name: "push publish accepted", Status: "pass",
		})
	}

	// Step 4: Unregister subscription
	if deviceID != 0 {
		_, unsubErr := pushUnsubscribe(ctx, gwURL, token, deviceID)
		if unsubErr != nil {
			checks = append(checks, metrics.CheckResult{
				Name: "push unsubscribe", Status: "fail", Error: unsubErr.Error(),
			})
		} else {
			// Remove from cleanup list since we already unsubscribed
			deviceIDs = deviceIDs[:0]
			checks = append(checks, metrics.CheckResult{
				Name: "push unsubscribe", Status: "pass",
			})
		}
	}

	// Step 5: Channel config CRUD — create, get, delete, get (404)
	testChannel2 := tenantID + ".push-crud-test.*"
	if err := provClient.SetPushChannels(ctx, tenantID, []string{testChannel2}, 86400, "high"); err != nil {
		checks = append(checks, metrics.CheckResult{
			Name: "channel config crud create", Status: "fail", Error: err.Error(),
		})
	} else {
		checks = append(checks, metrics.CheckResult{
			Name: "channel config crud create", Status: "pass",
		})

		// Get — verify patterns match
		body, err := provClient.GetPushChannels(ctx, tenantID)
		if err != nil {
			checks = append(checks, metrics.CheckResult{
				Name: "channel config get", Status: "fail", Error: err.Error(),
			})
		} else {
			var cfg struct {
				Patterns []string `json:"patterns"`
			}
			if jsonErr := json.Unmarshal(body, &cfg); jsonErr != nil || len(cfg.Patterns) == 0 {
				checks = append(checks, metrics.CheckResult{
					Name: "channel config get", Status: "fail",
					Error: fmt.Sprintf("unexpected response: %s", string(body)),
				})
			} else {
				checks = append(checks, metrics.CheckResult{
					Name: "channel config get", Status: "pass",
				})
			}
		}

		// Delete
		if err := provClient.DeletePushChannels(ctx, tenantID); err != nil {
			checks = append(checks, metrics.CheckResult{
				Name: "channel config delete", Status: "fail", Error: err.Error(),
			})
		} else {
			checks = append(checks, metrics.CheckResult{
				Name: "channel config delete", Status: "pass",
			})

			// Get after delete — expect error (404)
			_, err := provClient.GetPushChannels(ctx, tenantID)
			if err != nil {
				checks = append(checks, metrics.CheckResult{
					Name: "channel config delete verify", Status: "pass",
				})
			} else {
				checks = append(checks, metrics.CheckResult{
					Name: "channel config delete verify", Status: "fail",
					Error: "expected 404 after delete, got 200",
				})
			}
		}
	}

	// Step 6: Credential CRUD — create, delete
	testCreds := `{"public_key":"test_crud_pub","private_key":"test_crud_priv"}`
	if err := provClient.SetPushCredentials(ctx, tenantID, "vapid", testCreds); err != nil {
		checks = append(checks, metrics.CheckResult{
			Name: "credential crud create", Status: "fail", Error: err.Error(),
		})
	} else {
		checks = append(checks, metrics.CheckResult{
			Name: "credential crud create", Status: "pass",
		})

		if err := provClient.DeletePushCredentials(ctx, tenantID, "vapid"); err != nil {
			checks = append(checks, metrics.CheckResult{
				Name: "credential delete", Status: "fail", Error: err.Error(),
			})
		} else {
			checks = append(checks, metrics.CheckResult{
				Name: "credential delete", Status: "pass",
			})
		}
	}

	// Step 7 (P2): Multi-platform — register web, android, ios
	multiPlatforms := []struct {
		name     string
		platform string
		req      pushSubscribeRequest
	}{
		{
			name:     "multi-platform web",
			platform: "web",
			req: pushSubscribeRequest{
				Platform:   "web",
				Endpoint:   "https://push.example.com/multi-" + uuid.NewString()[:8],
				P256dhKey:  "BNcRdreALRFXTkOOUHK1EtK2wtaz5Ry4YfYCA_0QTpQtUbVlUls0VJXg7A8u-Ts1XbjhazAkj7I99e8p8aE48",
				AuthSecret: "tBHItJI5svbpC7CIKw9w2A",
				Channels:   []string{pushChannel},
			},
		},
		{
			name:     "multi-platform android",
			platform: "android",
			req: pushSubscribeRequest{
				Platform: "android",
				Token:    "fake-fcm-token-" + uuid.NewString()[:8],
				Channels: []string{pushChannel},
			},
		},
		{
			name:     "multi-platform ios",
			platform: "ios",
			req: pushSubscribeRequest{
				Platform: "ios",
				Token:    "fake-apns-token-" + uuid.NewString()[:8],
				Channels: []string{pushChannel},
			},
		},
	}

	// Re-create channel config for multi-platform test (was deleted in step 5)
	_ = provClient.SetPushChannels(ctx, tenantID, []string{pushChannel}, 2419200, "normal")
	_ = provClient.SetPushCredentials(ctx, tenantID, "vapid", vapidCreds)

	for _, mp := range multiPlatforms {
		id, _, err := pushSubscribe(ctx, gwURL, token, mp.req)
		if err != nil {
			checks = append(checks, metrics.CheckResult{
				Name: mp.name, Status: "fail", Error: err.Error(),
			})
		} else {
			deviceIDs = append(deviceIDs, id)
			checks = append(checks, metrics.CheckResult{
				Name: mp.name, Status: "pass",
			})
		}
	}

	return checks, nil
}
