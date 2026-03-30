package metrics

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/klurvio/sukko/cmd/tester/stats"
)

// Collector tracks test execution metrics with atomic operations.
type Collector struct {
	ConnectionsActive atomic.Int64
	ConnectionsFailed atomic.Int64
	ConnectionsTotal  atomic.Int64
	MessagesSent      atomic.Int64
	MessagesReceived  atomic.Int64
	MessagesDropped   atomic.Int64
	ErrorsTotal       atomic.Int64
	AuthRefreshTotal  atomic.Int64
	AuthRefreshFailed atomic.Int64
	AuthErrors        atomic.Int64
	Latency           *stats.Histogram
	mu                sync.RWMutex
	startTime         time.Time
}

// NewCollector creates a Collector with zeroed counters and a fresh start time.
func NewCollector() *Collector {
	return &Collector{
		Latency:   stats.NewHistogram(),
		startTime: time.Now(),
	}
}

// Snapshot is a serializable snapshot of current metrics.
type Snapshot struct {
	Timestamp         time.Time      `json:"timestamp"`
	Elapsed           string         `json:"elapsed"`
	ConnectionsActive int64          `json:"connections_active"`
	ConnectionsFailed int64          `json:"connections_failed"`
	ConnectionsTotal  int64          `json:"connections_total"`
	MessagesSent      int64          `json:"messages_sent"`
	MessagesReceived  int64          `json:"messages_received"`
	MessagesDropped   int64          `json:"messages_dropped"`
	ErrorsTotal       int64          `json:"errors_total"`
	AuthRefreshTotal  int64          `json:"auth_refresh_total"`
	AuthRefreshFailed int64          `json:"auth_refresh_failed"`
	AuthErrors        int64          `json:"auth_errors"`
	Latency           stats.Snapshot `json:"latency"`
}

// Snapshot returns a point-in-time copy of all collected metrics.
func (c *Collector) Snapshot() Snapshot {
	c.mu.RLock()
	elapsed := time.Since(c.startTime).Round(time.Second).String()
	c.mu.RUnlock()

	return Snapshot{
		Timestamp:         time.Now(),
		Elapsed:           elapsed,
		ConnectionsActive: c.ConnectionsActive.Load(),
		ConnectionsFailed: c.ConnectionsFailed.Load(),
		ConnectionsTotal:  c.ConnectionsTotal.Load(),
		MessagesSent:      c.MessagesSent.Load(),
		MessagesReceived:  c.MessagesReceived.Load(),
		MessagesDropped:   c.MessagesDropped.Load(),
		ErrorsTotal:       c.ErrorsTotal.Load(),
		AuthRefreshTotal:  c.AuthRefreshTotal.Load(),
		AuthRefreshFailed: c.AuthRefreshFailed.Load(),
		AuthErrors:        c.AuthErrors.Load(),
		Latency:           c.Latency.Snapshot(),
	}
}

// Reset zeroes all counters and restarts the elapsed timer.
func (c *Collector) Reset() {
	c.ConnectionsActive.Store(0)
	c.ConnectionsFailed.Store(0)
	c.ConnectionsTotal.Store(0)
	c.MessagesSent.Store(0)
	c.MessagesReceived.Store(0)
	c.MessagesDropped.Store(0)
	c.ErrorsTotal.Store(0)
	c.AuthRefreshTotal.Store(0)
	c.AuthRefreshFailed.Store(0)
	c.AuthErrors.Store(0)
	c.Latency.Reset()
	c.mu.Lock()
	c.startTime = time.Now()
	c.mu.Unlock()
}
