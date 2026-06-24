package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/klurvio/sukko/cmd/tester/metrics"
	"github.com/rs/zerolog"
)

// newValidateAPIKeyRun constructs a minimal TestRun for validateAPIKey unit tests.
func newValidateAPIKeyRun(gatewayURL, apiKey string) *TestRun {
	return &TestRun{
		ID: "test-val-apikey",
		Config: TestConfig{
			Type:       TestValidate,
			Suite:      "api-key",
			GatewayURL: gatewayURL,
			TenantID:   "test-tenant",
			AuthMode:   AuthModeAPIKey,
		},
		Status:             StatusRunning,
		Collector:          metrics.NewCollector(),
		apiKey:             apiKey,
		authUpgradeTimeout: 3 * time.Second, // short timeout for tests; drives Check 3 deny-wait
	}
}

// newWsRestServer returns an httptest.Server that:
//   - Upgrades /ws requests to WebSocket using gobwas.
//   - Parses incoming subscribe frames; sends back an error frame for channels
//     whose name contains "private" (simulating gateway access-control denial).
//   - Responds to POST /api/v1/publish with the provided REST publish status code.
//
// The server accepts connections on http://host:port. For WS connections,
// gobwas ws.Dialer dials TCP directly to host:port and performs the HTTP
// upgrade, so the server works regardless of whether the caller uses
// "ws://" or "http://" scheme in the GatewayURL.
//
// Note: production code's restpublish.NewClient uses run.Config.GatewayURL
// directly. If GatewayURL is "ws://...", Go's http.Client rejects the
// scheme before even connecting. This is a known limitation in
// validate_api_key.go (should use httpURL() for the REST call).
// Tests that exercise the REST path must use an "http://" GatewayURL.
func newWsRestServer(t *testing.T, restPublishStatus int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ws" {
			upgrader := ws.HTTPUpgrader{}
			conn, _, _, err := upgrader.Upgrade(r, w)
			if err != nil {
				return
			}
			defer conn.Close()
			for {
				msg, err := wsutil.ReadClientText(conn)
				if err != nil {
					return
				}
				// Parse the frame and deny private-channel subscriptions.
				var frame struct {
					Type string `json:"type"`
					Data struct {
						Channels []string `json:"channels"`
					} `json:"data"`
				}
				if jsonErr := json.Unmarshal(msg, &frame); jsonErr != nil {
					continue
				}
				if frame.Type == "subscribe" {
					for _, ch := range frame.Data.Channels {
						if strings.Contains(ch, "private") {
							errMsg, _ := json.Marshal(map[string]any{
								"type":    "error",
								"channel": ch,
								"data":    map[string]any{"code": "UNAUTHORIZED"},
							})
							_ = wsutil.WriteServerText(conn, errMsg)
						}
					}
				}
			}
		}
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/publish" {
			w.WriteHeader(restPublishStatus)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestValidateAPIKey_NoAPIKey verifies that validateAPIKey returns a non-nil
// error (not a check result) when run.apiKey is empty.
func TestValidateAPIKey_NoAPIKey(t *testing.T) {
	t.Parallel()

	run := newValidateAPIKeyRun("ws://invalid:9999", "")
	checks, err := validateAPIKey(context.Background(), run, zerolog.Nop())
	if err == nil {
		t.Fatal("expected error when apiKey is empty, got nil")
	}
	if !strings.Contains(err.Error(), "no api key configured") {
		t.Errorf("error = %q, want it to contain 'no api key configured'", err.Error())
	}
	if len(checks) != 0 {
		t.Errorf("expected no checks on early error, got %d", len(checks))
	}
}

// TestValidateAPIKey_HappyPath verifies all four checks pass: the mock accepts
// API-key WS connections (Check 1), allows public-channel subscribe (Check 2),
// sends an error frame for the private channel (Check 3 — gateway denies it),
// and returns 403 for REST publish (Check 4 — gateway blocks API-key REST publish).
func TestValidateAPIKey_HappyPath(t *testing.T) {
	t.Parallel()

	// Use ws:// so gobwas accepts the scheme for the WS upgrade. validateAPIKey
	// applies httpURL() before passing to restpublish.NewClient, converting
	// ws:// → http://, so the REST call also reaches the mock (which returns 403).
	srv := newWsRestServer(t, http.StatusForbidden)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	run := newValidateAPIKeyRun(wsURL, "pk_live_test123")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	checks, err := validateAPIKey(ctx, run, zerolog.Nop())
	if err != nil {
		t.Fatalf("validateAPIKey: %v", err)
	}
	if len(checks) == 0 {
		t.Fatal("expected at least one check result")
	}

	wantPass := []string{"api key accepted", "public channel subscribe", "private channel denied", "api key REST publish blocked"}
	for _, name := range wantPass {
		var found bool
		for _, c := range checks {
			if c.Name == name {
				found = true
				if c.Status != metrics.CheckStatusPass {
					t.Errorf("check %q: status = %q, want %q; error: %s", name, c.Status, metrics.CheckStatusPass, c.Error)
				}
				break
			}
		}
		if !found {
			t.Errorf("check %q not found in results (got %d checks)", name, len(checks))
		}
	}

	if got := run.Collector.ConnectionsAPIKey.Load(); got != 1 {
		t.Errorf("ConnectionsAPIKey = %d, want 1", got)
	}
}

// TestValidateAPIKey_RESTPublishReturns200_IncreasesErrorCounter verifies that
// when the REST publish endpoint returns 200 (unexpected — gateway should return
// 403 for API-key clients), Check 4 fails and RESTPublishErrors is incremented.
//
// This test uses a ws:// GatewayURL. gobwas dials TCP directly so the WS upgrade
// succeeds. validateAPIKey calls httpURL() before passing the URL to
// restpublish.NewClient, converting ws:// → http://, so Go's http.Client can
// also reach the mock. Checks 1–3 pass (WS + mock sends deny for private channel),
// Check 4 fails because the mock returns 200 instead of 403, and RESTPublishErrors
// is incremented to 1.
func TestValidateAPIKey_RESTPublishReturns200_IncreasesErrorCounter(t *testing.T) {
	t.Parallel()

	srv := newWsRestServer(t, http.StatusOK)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	run := newValidateAPIKeyRun(wsURL, "pk_live_test123")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	checks, err := validateAPIKey(ctx, run, zerolog.Nop())
	if err != nil {
		t.Fatalf("validateAPIKey: %v", err)
	}
	if len(checks) == 0 {
		t.Fatal("expected at least one check result")
	}

	var foundRESTCheck bool
	for _, c := range checks {
		if c.Name == "api key REST publish blocked" {
			foundRESTCheck = true
			if c.Status != metrics.CheckStatusFail {
				t.Errorf("check %q: status = %q, want %q", c.Name, c.Status, metrics.CheckStatusFail)
			}
			if got := run.Collector.RESTPublishErrors.Load(); got != 1 {
				t.Errorf("RESTPublishErrors = %d, want 1 (mock returns 200, default branch increments counter)", got)
			}
			break
		}
	}
	if !foundRESTCheck {
		t.Fatal("check 'api key REST publish blocked' not found in results")
	}
}
