package metrics

import (
	"testing"
	"time"
)

func TestNewCollector(t *testing.T) {
	t.Parallel()

	c := NewCollector()
	if c == nil {
		t.Fatal("expected non-nil collector")
	}
	snap := c.Snapshot()
	if snap.ConnectionsActive != 0 || snap.MessagesSent != 0 || snap.ErrorsTotal != 0 {
		t.Errorf("expected zero initial metrics, got %+v", snap)
	}
}

func TestCollector_AtomicCounters(t *testing.T) {
	t.Parallel()

	c := NewCollector()

	c.ConnectionsActive.Add(5)
	c.ConnectionsTotal.Add(10)
	c.ConnectionsFailed.Add(2)
	c.MessagesSent.Add(100)
	c.MessagesReceived.Add(95)
	c.MessagesDropped.Add(3)
	c.ErrorsTotal.Add(1)

	snap := c.Snapshot()
	if snap.ConnectionsActive != 5 {
		t.Errorf("ConnectionsActive = %d, want 5", snap.ConnectionsActive)
	}
	if snap.ConnectionsTotal != 10 {
		t.Errorf("ConnectionsTotal = %d, want 10", snap.ConnectionsTotal)
	}
	if snap.ConnectionsFailed != 2 {
		t.Errorf("ConnectionsFailed = %d, want 2", snap.ConnectionsFailed)
	}
	if snap.MessagesSent != 100 {
		t.Errorf("MessagesSent = %d, want 100", snap.MessagesSent)
	}
	if snap.MessagesReceived != 95 {
		t.Errorf("MessagesReceived = %d, want 95", snap.MessagesReceived)
	}
	if snap.MessagesDropped != 3 {
		t.Errorf("MessagesDropped = %d, want 3", snap.MessagesDropped)
	}
	if snap.ErrorsTotal != 1 {
		t.Errorf("ErrorsTotal = %d, want 1", snap.ErrorsTotal)
	}
}

func TestCollector_AuthCounters(t *testing.T) {
	t.Parallel()

	c := NewCollector()
	c.AuthRefreshTotal.Add(10)
	c.AuthRefreshFailed.Add(2)
	c.AuthErrors.Add(3)

	snap := c.Snapshot()
	if snap.AuthRefreshTotal != 10 {
		t.Errorf("AuthRefreshTotal = %d, want 10", snap.AuthRefreshTotal)
	}
	if snap.AuthRefreshFailed != 2 {
		t.Errorf("AuthRefreshFailed = %d, want 2", snap.AuthRefreshFailed)
	}
	if snap.AuthErrors != 3 {
		t.Errorf("AuthErrors = %d, want 3", snap.AuthErrors)
	}

	c.Reset()
	snap = c.Snapshot()
	if snap.AuthRefreshTotal != 0 || snap.AuthRefreshFailed != 0 || snap.AuthErrors != 0 {
		t.Error("auth counters not zeroed after reset")
	}
}

func TestCollector_Latency(t *testing.T) {
	t.Parallel()

	c := NewCollector()
	c.Latency.Record(10 * time.Millisecond)
	c.Latency.Record(20 * time.Millisecond)

	snap := c.Snapshot()
	if snap.Latency.Count != 2 {
		t.Errorf("expected latency count=2, got %d", snap.Latency.Count)
	}
}

func TestCollector_Elapsed(t *testing.T) {
	t.Parallel()

	c := NewCollector()
	// Snapshot should have a non-empty elapsed string
	time.Sleep(5 * time.Millisecond)
	snap := c.Snapshot()
	if snap.Elapsed == "" {
		t.Error("expected non-empty elapsed")
	}
}

func TestCollector_Reset(t *testing.T) {
	t.Parallel()

	c := NewCollector()
	c.ConnectionsActive.Add(5)
	c.MessagesSent.Add(100)
	c.Latency.Record(10 * time.Millisecond)

	c.Reset()
	snap := c.Snapshot()
	if snap.ConnectionsActive != 0 {
		t.Errorf("expected ConnectionsActive=0 after reset, got %d", snap.ConnectionsActive)
	}
	if snap.MessagesSent != 0 {
		t.Errorf("expected MessagesSent=0 after reset, got %d", snap.MessagesSent)
	}
	if snap.Latency.Count != 0 {
		t.Errorf("expected latency count=0 after reset, got %d", snap.Latency.Count)
	}
}

func TestCollector_SnapshotTimestamp(t *testing.T) {
	t.Parallel()

	c := NewCollector()
	before := time.Now()
	snap := c.Snapshot()
	after := time.Now()

	if snap.Timestamp.Before(before) || snap.Timestamp.After(after) {
		t.Errorf("timestamp %v not between %v and %v", snap.Timestamp, before, after)
	}
}

func TestCollector_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	c := NewCollector()
	done := make(chan struct{})

	go func() {
		for range 1000 {
			c.ConnectionsActive.Add(1)
			c.MessagesSent.Add(1)
		}
		close(done)
	}()

	// Read snapshots concurrently
	for range 100 {
		_ = c.Snapshot()
	}

	<-done
	snap := c.Snapshot()
	if snap.ConnectionsActive != 1000 {
		t.Errorf("expected ConnectionsActive=1000, got %d", snap.ConnectionsActive)
	}
}
