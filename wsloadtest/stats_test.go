package main

import (
	"testing"
)

func TestNewStats(t *testing.T) {
	t.Parallel()

	stats := NewStats()

	if stats.GetPhase() != PhaseRamping {
		t.Errorf("initial phase = %s, want %s", stats.GetPhase(), PhaseRamping)
	}
	if stats.ActiveConnections.Load() != 0 {
		t.Errorf("initial ActiveConnections = %d, want 0", stats.ActiveConnections.Load())
	}
}

func TestStats_ConnectionMetrics(t *testing.T) {
	t.Parallel()

	stats := NewStats()

	// Simulate connections
	stats.ActiveConnections.Add(1)
	stats.TotalCreated.Add(1)

	if got := stats.ActiveConnections.Load(); got != 1 {
		t.Errorf("ActiveConnections = %d, want 1", got)
	}

	// Simulate disconnect
	stats.ActiveConnections.Add(-1)

	if got := stats.ActiveConnections.Load(); got != 0 {
		t.Errorf("ActiveConnections after disconnect = %d, want 0", got)
	}

	// Total created should still be 1
	if got := stats.TotalCreated.Load(); got != 1 {
		t.Errorf("TotalCreated = %d, want 1", got)
	}
}

func TestStats_SubscriptionMetrics(t *testing.T) {
	t.Parallel()

	stats := NewStats()

	// Simulate subscription flow
	stats.SubscriptionsSent.Add(1)
	if got := stats.GetSubscriptionsPending(); got != 1 {
		t.Errorf("SubscriptionsPending = %d, want 1", got)
	}

	stats.SubscriptionsConfirmed.Add(1)
	if got := stats.GetSubscriptionsPending(); got != 0 {
		t.Errorf("SubscriptionsPending after confirm = %d, want 0", got)
	}

	// Simulate failed subscription
	stats.SubscriptionsSent.Add(1)
	stats.SubscriptionsFailed.Add(1)
	if got := stats.GetSubscriptionsPending(); got != 0 {
		t.Errorf("SubscriptionsPending after failure = %d, want 0", got)
	}
}

func TestStats_RecordError(t *testing.T) {
	t.Parallel()

	stats := NewStats()

	stats.RecordError("connection_failed")
	stats.RecordError("connection_failed")
	stats.RecordError("timeout")

	counts := stats.GetErrorCounts()

	if counts["connection_failed"] != 2 {
		t.Errorf("connection_failed count = %d, want 2", counts["connection_failed"])
	}
	if counts["timeout"] != 1 {
		t.Errorf("timeout count = %d, want 1", counts["timeout"])
	}
}

func TestStats_RecordChannelMessage(t *testing.T) {
	t.Parallel()

	stats := NewStats()

	stats.RecordChannelMessage("odin.BTC.trade")
	stats.RecordChannelMessage("odin.BTC.trade")
	stats.RecordChannelMessage("odin.ETH.trade")

	counts := stats.GetChannelCounts()

	if counts["odin.BTC.trade"] != 2 {
		t.Errorf("odin.BTC.trade count = %d, want 2", counts["odin.BTC.trade"])
	}
	if counts["odin.ETH.trade"] != 1 {
		t.Errorf("odin.ETH.trade count = %d, want 1", counts["odin.ETH.trade"])
	}

	// Total messages should be 3
	if got := stats.MessagesReceived.Load(); got != 3 {
		t.Errorf("MessagesReceived = %d, want 3", got)
	}
}

func TestStats_Phase(t *testing.T) {
	t.Parallel()

	stats := NewStats()

	if stats.GetPhase() != PhaseRamping {
		t.Errorf("initial phase = %s, want %s", stats.GetPhase(), PhaseRamping)
	}

	stats.SetPhase(PhaseSustaining)
	if stats.GetPhase() != PhaseSustaining {
		t.Errorf("phase after SetPhase = %s, want %s", stats.GetPhase(), PhaseSustaining)
	}

	// RampCompleteTime should be set
	if stats.RampCompleteTime.IsZero() {
		t.Error("RampCompleteTime should be set when entering sustaining phase")
	}

	stats.SetPhase(PhaseCompleted)
	if stats.GetPhase() != PhaseCompleted {
		t.Errorf("phase after SetPhase = %s, want %s", stats.GetPhase(), PhaseCompleted)
	}
}

func TestStats_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	stats := NewStats()
	done := make(chan struct{})

	// Simulate concurrent access
	for i := 0; i < 100; i++ {
		go func() {
			stats.ActiveConnections.Add(1)
			stats.TotalCreated.Add(1)
			stats.MessagesReceived.Add(1)
			stats.RecordError("test_error")
			stats.RecordChannelMessage("odin.BTC.trade")
			done <- struct{}{}
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 100; i++ {
		<-done
	}

	if got := stats.TotalCreated.Load(); got != 100 {
		t.Errorf("TotalCreated after concurrent = %d, want 100", got)
	}
	if got := stats.MessagesReceived.Load(); got != 200 { // 100 from Add + 100 from RecordChannelMessage
		t.Errorf("MessagesReceived after concurrent = %d, want 200", got)
	}
}
