package alerting

import (
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestNewMultiAlerter(t *testing.T) {
	t.Parallel()
	alerter1 := &mockAlerter{}
	alerter2 := &mockAlerter{}

	multi := NewMultiAlerter(zerolog.Nop(), alerter1, alerter2)

	if multi == nil {
		t.Fatal("NewMultiAlerter should return non-nil")
	}
	if len(multi.alerters) != 2 {
		t.Errorf("Expected 2 alerters, got %d", len(multi.alerters))
	}
}

func TestNewMultiAlerter_Empty(t *testing.T) {
	t.Parallel()
	multi := NewMultiAlerter(zerolog.Nop())

	if multi == nil {
		t.Fatal("NewMultiAlerter should return non-nil even with no alerters")
	}
	if len(multi.alerters) != 0 {
		t.Errorf("Expected 0 alerters, got %d", len(multi.alerters))
	}
}

func TestMultiAlerter_AlertAllAlerters(t *testing.T) {
	t.Parallel()
	alerter1 := &mockAlerter{}
	alerter2 := &mockAlerter{}
	alerter3 := &mockAlerter{}

	multi := NewMultiAlerter(zerolog.Nop(), alerter1, alerter2, alerter3)

	multi.Alert(ERROR, "Test alert", map[string]any{"key": "value"})

	// Give goroutines time to execute
	time.Sleep(50 * time.Millisecond)

	// All alerters should have received the alert
	if alerter1.getAlerts() != 1 {
		t.Errorf("alerter1 should have 1 alert, got %d", alerter1.getAlerts())
	}
	if alerter2.getAlerts() != 1 {
		t.Errorf("alerter2 should have 1 alert, got %d", alerter2.getAlerts())
	}
	if alerter3.getAlerts() != 1 {
		t.Errorf("alerter3 should have 1 alert, got %d", alerter3.getAlerts())
	}
}

func TestMultiAlerter_RunsInGoroutines(t *testing.T) {
	t.Parallel()
	// Create an alerter that blocks
	blockingAlerter := &blockingMockAlerter{
		blockDuration: 100 * time.Millisecond,
	}
	fastAlerter := &mockAlerter{}

	multi := NewMultiAlerter(zerolog.Nop(), blockingAlerter, fastAlerter)

	start := time.Now()
	multi.Alert(ERROR, "Test", nil)
	elapsed := time.Since(start)

	// Should return immediately since alerters run in goroutines
	if elapsed > 10*time.Millisecond {
		t.Errorf("MultiAlerter.Alert should not block, took %v", elapsed)
	}

	// Wait for both to complete
	time.Sleep(150 * time.Millisecond)

	if fastAlerter.getAlerts() != 1 {
		t.Error("fastAlerter should have received alert")
	}
	if blockingAlerter.getAlerts() != 1 {
		t.Error("blockingAlerter should have received alert")
	}
}

func TestMultiAlerter_ImplementsInterface(t *testing.T) {
	t.Parallel()
	var alerter Alerter = NewMultiAlerter(zerolog.Nop(), &mockAlerter{})

	alerter.Alert(ERROR, "Test", nil)
}

type blockingMockAlerter struct {
	mu            sync.Mutex
	alertCount    int
	blockDuration time.Duration
}

func (b *blockingMockAlerter) Alert(_ Level, _ string, _ map[string]any) {
	time.Sleep(b.blockDuration)
	b.mu.Lock()
	b.alertCount++
	b.mu.Unlock()
}

func (b *blockingMockAlerter) getAlerts() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.alertCount
}
