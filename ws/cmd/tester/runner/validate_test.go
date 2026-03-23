package runner

import (
	"context"
	"testing"

	"github.com/klurvio/sukko/cmd/tester/metrics"
	"github.com/rs/zerolog"
)

func TestRunValidate_DefaultSuite(t *testing.T) {
	run := &TestRun{
		ID:        "test-validate-default",
		Config:    TestConfig{Type: TestValidate, GatewayURL: "ws://invalid:9999"},
		Status:    StatusRunning,
		Collector: metrics.NewCollector(),
	}

	// Default suite is "auth", which requires a real gateway — it will fail,
	// but it should return a report, not a nil pointer panic.
	report, err := runValidate(context.Background(), run, zerolog.Nop())
	if err != nil {
		// Auth validation returns errors as check results, not as err
		t.Fatalf("unexpected error: %v", err)
	}
	if report == nil {
		t.Fatal("expected non-nil report")
	}
	if report.TestType != "validate:auth" {
		t.Errorf("test type = %q, want validate:auth", report.TestType)
	}
}

func TestRunValidate_UnknownSuite(t *testing.T) {
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

func TestRunValidate_SkipSuites(t *testing.T) {
	suites := []string{"ordering", "reconnect", "ratelimit"}
	for _, suite := range suites {
		t.Run(suite, func(t *testing.T) {
			run := &TestRun{
				ID:        "test-" + suite,
				Config:    TestConfig{Type: TestValidate, Suite: suite},
				Status:    StatusRunning,
				Collector: metrics.NewCollector(),
			}
			report, err := runValidate(context.Background(), run, zerolog.Nop())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(report.Checks) == 0 {
				t.Fatal("expected check results")
			}
			if report.Checks[0].Status != "skip" {
				t.Errorf("status = %q, want skip", report.Checks[0].Status)
			}
		})
	}
}
