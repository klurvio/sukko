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
	Latency           *stats.Histogram
	mu                sync.RWMutex
	startTime         time.Time
}

func NewCollector() *Collector {
	return &Collector{
		Latency:   stats.NewHistogram(),
		startTime: time.Now(),
	}
}

// MetricsSnapshot is a serializable snapshot of current metrics.
type MetricsSnapshot struct {
	Timestamp         time.Time      `json:"timestamp"`
	Elapsed           string         `json:"elapsed"`
	ConnectionsActive int64          `json:"connections_active"`
	ConnectionsFailed int64          `json:"connections_failed"`
	ConnectionsTotal  int64          `json:"connections_total"`
	MessagesSent      int64          `json:"messages_sent"`
	MessagesReceived  int64          `json:"messages_received"`
	MessagesDropped   int64          `json:"messages_dropped"`
	ErrorsTotal       int64          `json:"errors_total"`
	Latency           stats.Snapshot `json:"latency"`
}

func (c *Collector) Snapshot() MetricsSnapshot {
	c.mu.RLock()
	elapsed := time.Since(c.startTime).Round(time.Second).String()
	c.mu.RUnlock()

	return MetricsSnapshot{
		Timestamp:         time.Now(),
		Elapsed:           elapsed,
		ConnectionsActive: c.ConnectionsActive.Load(),
		ConnectionsFailed: c.ConnectionsFailed.Load(),
		ConnectionsTotal:  c.ConnectionsTotal.Load(),
		MessagesSent:      c.MessagesSent.Load(),
		MessagesReceived:  c.MessagesReceived.Load(),
		MessagesDropped:   c.MessagesDropped.Load(),
		ErrorsTotal:       c.ErrorsTotal.Load(),
		Latency:           c.Latency.Snapshot(),
	}
}

func (c *Collector) Reset() {
	c.ConnectionsActive.Store(0)
	c.ConnectionsFailed.Store(0)
	c.ConnectionsTotal.Store(0)
	c.MessagesSent.Store(0)
	c.MessagesReceived.Store(0)
	c.MessagesDropped.Store(0)
	c.ErrorsTotal.Store(0)
	c.Latency.Reset()
	c.mu.Lock()
	c.startTime = time.Now()
	c.mu.Unlock()
}
