package runner

import (
	"context"
	"testing"
	"time"

	"github.com/klurvio/sukko/cmd/tester/auth"
	"github.com/klurvio/sukko/cmd/tester/metrics"
	"github.com/rs/zerolog"
)

// testAuthResult creates a minimal SetupResult for unit tests that don't
// connect to a real gateway. The keypair and minter are real (local-only);
// only the provisioning client points at a dummy URL.
func testAuthResult(t *testing.T) *auth.SetupResult {
	t.Helper()

	kp, err := auth.GenerateKeypair("testval1")
	if err != nil {
		t.Fatalf("GenerateKeypair: %v", err)
	}
	minter := auth.NewMinter(auth.MinterConfig{
		Keypair:  kp,
		TenantID: "test-tenant",
		Lifetime: 15 * time.Minute,
	})
	return &auth.SetupResult{
		TenantID:   "test-tenant",
		Minter:     minter,
		TokenFunc:  minter.TokenFunc(),
		ProvClient: auth.NewProvisioningClient("http://invalid:9999", "test-token", zerolog.Nop()),
		Cleanup:    func(_ context.Context) {},
	}
}

func TestRunValidate_DefaultSuite(t *testing.T) {
	t.Parallel()

	run := &TestRun{
		ID:         "test-validate-default",
		Config:     TestConfig{Type: TestValidate, GatewayURL: "ws://invalid:9999"},
		Status:     StatusRunning,
		Collector:  metrics.NewCollector(),
		authResult: testAuthResult(t),
	}

	// Default suite is "auth", which requires a real gateway — it will fail,
	// but it should return a report, not a nil pointer panic.
	report, err := runValidate(context.Background(), run, zerolog.Nop())
	if err != nil {
		// Auth validation returns errors as check results, not as err
		t.Fatalf("unexpected error: %v", err)
	}
	if report == nil { //nolint:staticcheck // SA5011: t.Fatal prevents nil deref below
		t.Fatal("expected non-nil report")
	}
	if report.TestType != "validate:auth" { //nolint:staticcheck // SA5011: guarded by t.Fatal above
		t.Errorf("test type = %q, want validate:auth", report.TestType)
	}
}

func TestRunValidate_UnknownSuite(t *testing.T) {
	t.Parallel()

	run := &TestRun{
		ID:        "test-validate-unknown",
		Config:    TestConfig{Type: TestValidate, Suite: "nonexistent"},
		Status:    StatusRunning,
		Collector: metrics.NewCollector(),
	}

	report, err := runValidate(context.Background(), run, zerolog.Nop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Status != "fail" {
		t.Errorf("status = %q, want fail for unknown suite", report.Status)
	}
	if len(report.Checks) == 0 {
		t.Fatal("expected check results")
	}
	if report.Checks[0].Error == "" {
		t.Error("expected error message for unknown suite")
	}
}

func TestRunValidate_IntegrationSuites(t *testing.T) {
	t.Parallel()

	suites := []string{"ordering", "reconnect", "ratelimit"}
	for _, suite := range suites {
		t.Run(suite, func(t *testing.T) {
			t.Parallel()

			run := &TestRun{
				ID:         "test-" + suite,
				Config:     TestConfig{Type: TestValidate, Suite: suite, GatewayURL: "ws://invalid:9999"},
				Status:     StatusRunning,
				Collector:  metrics.NewCollector(),
				authResult: testAuthResult(t),
			}
			report, err := runValidate(context.Background(), run, zerolog.Nop())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if report == nil { //nolint:staticcheck // SA5011: t.Fatal prevents nil deref below
				t.Fatal("expected non-nil report")
			}
			// With invalid gateway URL, all suites should produce check results (not panic)
			if checks := report.Checks; len(checks) == 0 { //nolint:staticcheck // SA5011: guarded by t.Fatal above
				t.Fatal("expected check results")
			} else if checks[0].Status == "" {
				t.Error("expected non-empty check status")
			}
		})
	}
}
