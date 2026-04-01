package runner

import (
	"errors"
	"testing"

	"github.com/rs/zerolog"
)

func newTestRunner() *Runner {
	return New(Config{
		GatewayURL:      "ws://localhost:3000",
		ProvisioningURL: "http://localhost:8080",
		Token:           "test-token",
		MessageBackend:  "direct",
	}, zerolog.Nop())
}

func TestNew(t *testing.T) {
	t.Parallel()

	r := newTestRunner()
	if r == nil || r.tests == nil {
		t.Fatal("expected non-nil runner with initialized tests map")
	}
}

func TestRunner_Get_NotFound(t *testing.T) {
	t.Parallel()

	r := newTestRunner()
	_, err := r.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent test")
	}
	if !errors.Is(err, ErrTestNotFound) {
		t.Errorf("expected ErrTestNotFound, got %v", err)
	}
}

func TestRunner_Stop_NotFound(t *testing.T) {
	t.Parallel()

	r := newTestRunner()
	err := r.Stop("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent test")
	}
	if !errors.Is(err, ErrTestNotFound) {
		t.Errorf("expected ErrTestNotFound, got %v", err)
	}
}

func TestRunner_Start_DuplicateID(t *testing.T) {
	t.Parallel()

	r := newTestRunner()

	// Start a test — it will fail to connect but that's OK for this test
	cfg := TestConfig{
		Type:     TestSmoke,
		Duration: "1s",
	}
	_, err := r.Start("test-1", cfg)
	if err != nil {
		t.Fatalf("first Start: %v", err)
	}

	// Try to start with same ID
	_, err = r.Start("test-1", cfg)
	if err == nil {
		t.Fatal("expected error for duplicate test ID")
	}
	if !errors.Is(err, ErrTestAlreadyExists) {
		t.Errorf("expected ErrTestAlreadyExists, got %v", err)
	}

	// Cleanup: stop and wait
	_ = r.Stop("test-1")
	r.Wait()
}

func TestRunner_Start_DefaultsFilled(t *testing.T) {
	t.Parallel()

	r := newTestRunner()

	cfg := TestConfig{
		Type: TestSmoke,
	}
	run, err := r.Start("test-defaults", cfg)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Verify defaults were filled from runner config
	if run.Config.GatewayURL != "ws://localhost:3000" {
		t.Errorf("GatewayURL = %q, want %q", run.Config.GatewayURL, "ws://localhost:3000")
	}
	if run.Config.ProvisioningURL != "http://localhost:8080" {
		t.Errorf("ProvisioningURL = %q, want %q", run.Config.ProvisioningURL, "http://localhost:8080")
	}
	if run.Config.Token != "test-token" {
		t.Errorf("Token = %q, want %q", run.Config.Token, "test-token")
	}
	if run.Config.MessageBackend != "direct" {
		t.Errorf("MessageBackend = %q, want %q", run.Config.MessageBackend, "direct")
	}

	_ = r.Stop("test-defaults")
	r.Wait()
}

func TestRunner_Start_StatusRunning(t *testing.T) {
	t.Parallel()

	r := newTestRunner()
	run, err := r.Start("test-status", TestConfig{Type: TestSmoke})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if run.Status != StatusRunning {
		t.Errorf("Status = %q, want %q", run.Status, StatusRunning)
	}
	if run.Collector == nil {
		t.Error("expected non-nil Collector")
	}

	_ = r.Stop("test-status")
	r.Wait()
}

func TestRunner_Get_AfterStart(t *testing.T) {
	t.Parallel()

	r := newTestRunner()
	_, err := r.Start("test-get", TestConfig{Type: TestSmoke})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	run, err := r.Get("test-get")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if run.ID != "test-get" {
		t.Errorf("ID = %q, want %q", run.ID, "test-get")
	}

	_ = r.Stop("test-get")
	r.Wait()
}

func TestTestRun_StatusSnapshot(t *testing.T) {
	t.Parallel()

	r := newTestRunner()
	run, err := r.Start("test-snap", TestConfig{Type: TestSmoke})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	status, report := run.StatusSnapshot()
	if status != StatusRunning {
		t.Errorf("Status = %q, want %q", status, StatusRunning)
	}
	if report != nil {
		t.Error("expected nil report while running")
	}

	_ = r.Stop("test-snap")
	r.Wait()
}

func TestTestType_Constants(t *testing.T) {
	t.Parallel()

	// Verify test type constants are defined
	types := []TestType{TestSmoke, TestLoad, TestStress, TestSoak, TestValidate}
	for _, tt := range types {
		if tt == "" {
			t.Error("found empty test type constant")
		}
	}
}

func TestTestStatus_Constants(t *testing.T) {
	t.Parallel()

	statuses := []TestStatus{StatusPending, StatusRunning, StatusComplete, StatusFailed, StatusStopped}
	for _, s := range statuses {
		if s == "" {
			t.Error("found empty status constant")
		}
	}
}
