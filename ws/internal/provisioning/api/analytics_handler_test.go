package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// TestAnalyticsSSEHandler_MaxConnections verifies the semaphore enforces the cap.
func TestAnalyticsSSEHandler_MaxConnections(t *testing.T) {
	t.Parallel()
	h := NewAnalyticsSSEHandler(nil, 1, time.Minute, nil, zerolog.Nop())

	// Manually acquire the one semaphore slot.
	h.sem <- struct{}{}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/analytics/stream", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "TOO_MANY_SSE_CONNECTIONS") {
		t.Errorf("expected TOO_MANY_SSE_CONNECTIONS in body, got %q", rec.Body.String())
	}
}

// TestAnalyticsSSEHandler_ContextCancel verifies the handler exits when the request context is canceled.
func TestAnalyticsSSEHandler_ContextCancel(t *testing.T) {
	t.Parallel()
	// Large FlushInterval so the ticker doesn't fire; only context cancel exits the loop.
	h := NewAnalyticsSSEHandler(nil, 10, 5*time.Second, nil, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/analytics/stream", http.NoBody).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		h.ServeHTTP(rec, req)
		close(done)
	}()

	// Give the handler time to write the initial snapshot, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Handler exited cleanly after context cancel.
	case <-time.After(2 * time.Second):
		t.Error("ServeHTTP did not return within 2s after context cancel")
	}
}

// TestSSEWriteEvent verifies the SSE event wire format: "event: X\ndata: Y\n\n".
func TestSSEWriteEvent(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	if err := sseWriteEvent(rec, rec, "snapshot", map[string]int{"active": 5}); err != nil {
		t.Fatalf("sseWriteEvent error: %v", err)
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "event: snapshot\ndata: ") {
		t.Errorf("unexpected SSE format: %q", body)
	}
	if !strings.Contains(body, `"active":5`) {
		t.Errorf("expected active:5 in data, got %q", body)
	}
	if !strings.HasSuffix(body, "\n\n") {
		t.Errorf("expected double newline at end, got %q", body)
	}
}
