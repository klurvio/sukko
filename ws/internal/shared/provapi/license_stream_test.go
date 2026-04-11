package provapi

import (
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/shared/license"
)

func TestNewStreamLicenseWatcher_Validation(t *testing.T) {
	t.Parallel()

	logger := zerolog.Nop()
	mgr, _ := license.NewManager("", logger)

	tests := []struct {
		name    string
		cfg     StreamLicenseWatcherConfig
		wantErr string
	}{
		{
			name:    "missing GRPCAddr",
			cfg:     StreamLicenseWatcherConfig{Manager: mgr, Logger: logger},
			wantErr: "GRPCAddr is required",
		},
		{
			name:    "missing Manager",
			cfg:     StreamLicenseWatcherConfig{GRPCAddr: "localhost:9090", Logger: logger},
			wantErr: "Manager is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w, err := NewStreamLicenseWatcher(tt.cfg)
			if err == nil {
				_ = w.Close()
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if got := err.Error(); !strings.Contains(got, tt.wantErr) {
				t.Errorf("error = %q, want substring %q", got, tt.wantErr)
			}
		})
	}
}

func TestNewStreamLicenseWatcher_Defaults(t *testing.T) {
	t.Parallel()

	logger := zerolog.Nop()
	mgr, _ := license.NewManager("", logger)

	// Use an address that won't connect (lazy dial) — we just test config defaults
	w, err := NewStreamLicenseWatcher(StreamLicenseWatcherConfig{
		GRPCAddr: "localhost:19999",
		Manager:  mgr,
		Logger:   logger,
		// Leave ReconnectDelay, ReconnectMaxDelay, MetricPrefix as zero
	})
	if err != nil {
		t.Fatalf("NewStreamLicenseWatcher() error = %v", err)
	}
	defer func() { _ = w.Close() }()

	if w.config.ReconnectDelay != 5*time.Second {
		t.Errorf("ReconnectDelay = %v, want 5s", w.config.ReconnectDelay)
	}
	if w.config.ReconnectMaxDelay != 2*time.Minute {
		t.Errorf("ReconnectMaxDelay = %v, want 2m", w.config.ReconnectMaxDelay)
	}
	if w.config.MetricPrefix != "unknown" {
		t.Errorf("MetricPrefix = %q, want %q", w.config.MetricPrefix, "unknown")
	}
}

func TestStreamLicenseWatcher_CloseStopsGoroutine(t *testing.T) {
	t.Parallel()

	logger := zerolog.Nop()
	mgr, _ := license.NewManager("", logger)

	w, err := NewStreamLicenseWatcher(StreamLicenseWatcherConfig{
		GRPCAddr:     "localhost:19999",
		Manager:      mgr,
		MetricPrefix: "test_close",
		Logger:       logger,
	})
	if err != nil {
		t.Fatalf("NewStreamLicenseWatcher() error = %v", err)
	}

	// Close should not hang — it cancels the context and waits for the goroutine
	done := make(chan struct{})
	go func() {
		_ = w.Close()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(5 * time.Second):
		t.Fatal("Close() did not return within 5 seconds — goroutine leak")
	}
}

func TestStreamLicenseWatcher_OnReloadCallback(t *testing.T) {
	t.Parallel()

	logger := zerolog.Nop()
	mgr, _ := license.NewManager("", logger)

	var called bool
	w, err := NewStreamLicenseWatcher(StreamLicenseWatcherConfig{
		GRPCAddr:     "localhost:19999",
		Manager:      mgr,
		MetricPrefix: "test_onreload",
		Logger:       logger,
		OnReload:     func() { called = true },
	})
	if err != nil {
		t.Fatalf("NewStreamLicenseWatcher() error = %v", err)
	}
	defer func() { _ = w.Close() }()

	// Verify callback is stored in config
	if w.config.OnReload == nil {
		t.Fatal("OnReload callback was not stored in config")
	}

	// Verify nil OnReload is safe (no panic)
	w2, err := NewStreamLicenseWatcher(StreamLicenseWatcherConfig{
		GRPCAddr:     "localhost:19998",
		Manager:      mgr,
		MetricPrefix: "test_onreload_nil",
		Logger:       logger,
		// OnReload not set — nil
	})
	if err != nil {
		t.Fatalf("NewStreamLicenseWatcher() with nil OnReload error = %v", err)
	}
	defer func() { _ = w2.Close() }()

	if w2.config.OnReload != nil {
		t.Error("OnReload should be nil when not set")
	}

	_ = called // callback would be invoked by streamLoop on successful reload (requires live gRPC server)
}

func TestStreamLicenseWatcher_InitialStateDisconnected(t *testing.T) {
	t.Parallel()

	logger := zerolog.Nop()
	mgr, _ := license.NewManager("", logger)

	w, err := NewStreamLicenseWatcher(StreamLicenseWatcherConfig{
		GRPCAddr:     "localhost:19999",
		Manager:      mgr,
		MetricPrefix: "test_state",
		Logger:       logger,
	})
	if err != nil {
		t.Fatalf("NewStreamLicenseWatcher() error = %v", err)
	}
	defer func() { _ = w.Close() }()

	// Initial state should be disconnected (0) since no server is running
	if got := w.State(); got != StreamStateDisconnected {
		t.Errorf("initial State() = %d, want %d (disconnected)", got, StreamStateDisconnected)
	}
}
