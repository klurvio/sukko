package history_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/prometheus/client_golang/prometheus"
	prometheustestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rs/zerolog"
	valkey "github.com/valkey-io/valkey-go"

	"github.com/klurvio/sukko/internal/server/broadcast"
	"github.com/klurvio/sukko/internal/server/history"
	"github.com/klurvio/sukko/internal/shared/platform"
)

func noopLogger() zerolog.Logger { return zerolog.Nop() }

// testWriterOpts allows tests to override individual config fields.
type testWriterOpts struct {
	podID                 string
	lockTTLMs             int64
	heartbeatInterval     time.Duration
	restartInitialBackoff time.Duration
	restartMaxBackoff     time.Duration
	cmdTimeout            time.Duration
	writerBuffer          int
	pipelineBatch         int
	maxConsecLockFailures int
	env                   string
}

// defaultTestOpts returns safe, fast defaults suitable for unit tests.
func defaultTestOpts() testWriterOpts {
	return testWriterOpts{
		podID:                 "test-pod",
		lockTTLMs:             2000,
		heartbeatInterval:     50 * time.Millisecond,
		restartInitialBackoff: 10 * time.Millisecond,
		restartMaxBackoff:     100 * time.Millisecond,
		cmdTimeout:            500 * time.Millisecond,
		writerBuffer:          256,
		pipelineBatch:         50,
		maxConsecLockFailures: 3,
		env:                   "test",
	}
}

// newTestMiniredis creates a miniredis instance that is cleaned up at test end.
func newTestMiniredis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	mr := miniredis.NewMiniRedis()
	if err := mr.Start(); err != nil {
		t.Fatalf("miniredis start: %v", err)
	}
	t.Cleanup(mr.Close)
	return mr
}

// newTestValkeyClient creates a valkey-go client pointing at the given miniredis instance.
func newTestValkeyClient(t *testing.T, mr *miniredis.Miniredis) valkey.Client {
	t.Helper()
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:  []string{mr.Addr()},
		DisableCache: true,
	})
	if err != nil {
		t.Fatalf("valkey.NewClient: %v", err)
	}
	t.Cleanup(client.Close)
	return client
}

// newTestWriter creates a HistoryWriter backed by miniredis.
// Returns the writer, its cancel func, and the mock bus.
func newTestWriter(t *testing.T, mr *miniredis.Miniredis, opts testWriterOpts) (*history.Writer, context.CancelFunc, *mockBus) {
	t.Helper()

	cfg := &platform.ServerConfig{
		PodID:                              opts.podID,
		HistoryWriterLockTTLMs:             opts.lockTTLMs,
		HistoryWriterHeartbeatInterval:     opts.heartbeatInterval,
		HistoryWriterRestartInitialBackoff: opts.restartInitialBackoff,
		HistoryWriterRestartMaxBackoff:     opts.restartMaxBackoff,
		HistoryValkeyCmdTimeout:            opts.cmdTimeout,
		HistoryWriterBuffer:                opts.writerBuffer,
		HistoryWriterPipelineBatch:         opts.pipelineBatch,
		HistoryMaxConsecutiveLockFailures:  opts.maxConsecLockFailures,
		HistoryBufferDepth:                 100,
		HistoryTTL:                         24 * time.Hour,
	}

	bus := newMockBus()
	client := newTestValkeyClient(t, mr)

	ctx, cancel := context.WithCancel(context.Background())
	reg := prometheus.NewRegistry()

	w := history.NewWriter(ctx, cfg, bus, client, noopLogger(), reg, opts.env)
	t.Cleanup(cancel)
	return w, cancel, bus
}

// mockBus is a minimal broadcast.Bus implementation for testing.
type mockBus struct {
	mu              sync.Mutex
	subs            map[<-chan *broadcast.Message]chan *broadcast.Message
	healthy         bool
	publishLog      []*broadcast.Message
	lastDropCounter *atomic.Uint64
}

func newMockBus() *mockBus {
	return &mockBus{
		subs:    make(map[<-chan *broadcast.Message]chan *broadcast.Message),
		healthy: true,
	}
}

func (b *mockBus) Subscribe(bufSize int) (<-chan *broadcast.Message, *atomic.Uint64) {
	ch := make(chan *broadcast.Message, bufSize)
	dc := &atomic.Uint64{}
	b.mu.Lock()
	b.subs[ch] = ch
	b.lastDropCounter = dc
	b.mu.Unlock()
	return ch, dc
}

// simulateDrops increments the most-recently-returned subscription drop counter by n,
// simulating bus-level message drops for use in delta-tracking tests.
func (b *mockBus) simulateDrops(n uint64) {
	b.mu.Lock()
	dc := b.lastDropCounter
	b.mu.Unlock()
	if dc != nil {
		dc.Add(n)
	}
}

func (b *mockBus) Unsubscribe(ch <-chan *broadcast.Message) error {
	b.mu.Lock()
	delete(b.subs, ch)
	b.mu.Unlock()
	return nil
}

func (b *mockBus) Publish(msg *broadcast.Message) {
	b.mu.Lock()
	b.publishLog = append(b.publishLog, msg)
	for _, sub := range b.subs {
		select {
		case sub <- msg:
		default:
		}
	}
	b.mu.Unlock()
}

func (b *mockBus) Run()                                  {}
func (b *mockBus) Shutdown()                             {}
func (b *mockBus) ShutdownWithContext(_ context.Context) {}
func (b *mockBus) IsHealthy() bool                       { b.mu.Lock(); defer b.mu.Unlock(); return b.healthy }
func (b *mockBus) GetMetrics() broadcast.Metrics         { return broadcast.Metrics{} }

func (b *mockBus) setHealthy(v bool) {
	b.mu.Lock()
	b.healthy = v
	b.mu.Unlock()
}

// fanOut pushes a broadcast.Message directly to all subscribers on the bus.
func (b *mockBus) fanOut(msg *broadcast.Message) {
	b.mu.Lock()
	for _, sub := range b.subs {
		select {
		case sub <- msg:
		default:
		}
	}
	b.mu.Unlock()
}

// syncWaitGroup wraps sync.WaitGroup to match the wg.Go(func()) API expected by Run().
type syncWaitGroup struct {
	wg sync.WaitGroup
}

func (w *syncWaitGroup) Go(fn func()) {
	w.wg.Go(func() {
		fn()
	})
}

func (w *syncWaitGroup) Wait() { w.wg.Wait() }

// metricCounterValue reads the current value of a prometheus.Counter.
func metricCounterValue(_ *testing.T, c prometheus.Counter) float64 {
	return prometheustestutil.ToFloat64(c)
}
