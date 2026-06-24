package runner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/klurvio/sukko/cmd/tester/auth"
	"github.com/klurvio/sukko/cmd/tester/metrics"
	"github.com/rs/zerolog"
)

// newValidateUpgradeRun constructs a minimal TestRun for validateUpgrade unit tests.
func newValidateUpgradeRun(gatewayURL, apiKey string, minter *auth.Minter) *TestRun {
	return &TestRun{
		ID: "test-val-upgrade",
		Config: TestConfig{
			Type:       TestValidate,
			Suite:      "upgrade",
			GatewayURL: gatewayURL,
			TenantID:   "test-tenant",
			AuthMode:   AuthModeUpgrade,
		},
		Status:    StatusRunning,
		Collector: metrics.NewCollector(),
		authResult: &auth.SetupResult{
			TenantID: "test-tenant",
			Minter:   minter,
		},
		apiKey:             apiKey,
		authUpgradeTimeout: 100 * time.Millisecond, // short timeout so tests complete quickly
	}
}

// TestValidateUpgrade_NoAPIKey verifies that validateUpgrade returns a non-nil
// error (not check results) when run.apiKey is empty.
func TestValidateUpgrade_NoAPIKey(t *testing.T) {
	t.Parallel()

	minter := testMinter(t)
	run := newValidateUpgradeRun("ws://invalid:9999", "", minter)
	checks, err := validateUpgrade(context.Background(), run, zerolog.Nop())
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

// TestValidateUpgrade_NilMinter verifies that validateUpgrade returns a non-nil
// error when run.authResult.Minter is nil (api-key-only setup, which skips
// keypair registration).
func TestValidateUpgrade_NilMinter(t *testing.T) {
	t.Parallel()

	run := newValidateUpgradeRun("ws://invalid:9999", "pk_live_test123", nil)
	checks, err := validateUpgrade(context.Background(), run, zerolog.Nop())
	if err == nil {
		t.Fatal("expected error when Minter is nil, got nil")
	}
	if !strings.Contains(err.Error(), "Minter is nil") {
		t.Errorf("error = %q, want it to contain 'Minter is nil'", err.Error())
	}
	if len(checks) != 0 {
		t.Errorf("expected no checks on early error, got %d", len(checks))
	}
}

// TestValidateUpgrade_BasicHappyPath verifies that when run.apiKey is set and
// run.authResult.Minter is non-nil, validateUpgrade returns (checks, nil) even
// if the WS connection fails (unreachable gateway). The function returns check
// results with fail status, but the function-level error is nil.
func TestValidateUpgrade_BasicHappyPath(t *testing.T) {
	t.Parallel()

	minter := testMinter(t)
	// Use an unreachable gateway URL — the WS connect will fail but the function
	// should return check results (not a function-level error).
	run := newValidateUpgradeRun("ws://127.0.0.1:1", "pk_live_test123", minter)
	checks, err := validateUpgrade(context.Background(), run, zerolog.Nop())
	if err != nil {
		t.Fatalf("validateUpgrade returned function-level error: %v (expected nil — failures should be in check results)", err)
	}
	if len(checks) == 0 {
		t.Fatal("expected at least one check result (fail check for WS connect)")
	}
	// The first check should be "api key initial connect" with fail status.
	if checks[0].Name != "api key initial connect" {
		t.Errorf("checks[0].Name = %q, want %q", checks[0].Name, "api key initial connect")
	}
	if checks[0].Status != metrics.CheckStatusFail {
		t.Errorf("checks[0].Status = %q, want fail (WS connection to :1 should fail)", checks[0].Status)
	}
}

// testMinter creates a real Minter backed by a freshly-generated keypair.
// Used by upgrade tests that need a non-nil Minter without a provisioning server.
func testMinter(t *testing.T) *auth.Minter {
	t.Helper()
	kp, err := auth.GenerateKeypair("test-upgrade-kp")
	if err != nil {
		t.Fatalf("GenerateKeypair: %v", err)
	}
	return auth.NewMinter(auth.MinterConfig{
		Keypair:  kp,
		TenantID: "test-tenant",
		Lifetime: 15 * time.Minute,
	})
}
