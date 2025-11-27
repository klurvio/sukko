package monitoring

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// =============================================================================
// MultiAlerter Tests
// =============================================================================

func TestNewMultiAlerter(t *testing.T) {
	alerter1 := &mockAlerter{}
	alerter2 := &mockAlerter{}

	multi := NewMultiAlerter(alerter1, alerter2)

	if multi == nil {
		t.Fatal("NewMultiAlerter should return non-nil")
	}
	if len(multi.alerters) != 2 {
		t.Errorf("Expected 2 alerters, got %d", len(multi.alerters))
	}
}

func TestNewMultiAlerter_Empty(t *testing.T) {
	multi := NewMultiAlerter()

	if multi == nil {
		t.Fatal("NewMultiAlerter should return non-nil even with no alerters")
	}
	if len(multi.alerters) != 0 {
		t.Errorf("Expected 0 alerters, got %d", len(multi.alerters))
	}
}

func TestMultiAlerter_AlertAllAlerters(t *testing.T) {
	alerter1 := &mockAlerter{}
	alerter2 := &mockAlerter{}
	alerter3 := &mockAlerter{}

	multi := NewMultiAlerter(alerter1, alerter2, alerter3)

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
	// Create an alerter that blocks
	blockingAlerter := &blockingAlerter{
		blockDuration: 100 * time.Millisecond,
	}
	fastAlerter := &mockAlerter{}

	multi := NewMultiAlerter(blockingAlerter, fastAlerter)

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

type blockingAlerter struct {
	mu            sync.Mutex
	alertCount    int
	blockDuration time.Duration
}

func (b *blockingAlerter) Alert(level AuditLevel, message string, metadata map[string]any) {
	time.Sleep(b.blockDuration)
	b.mu.Lock()
	b.alertCount++
	b.mu.Unlock()
}

func (b *blockingAlerter) getAlerts() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.alertCount
}

// =============================================================================
// SlackAlerter Tests
// =============================================================================

func TestNewSlackAlerter(t *testing.T) {
	alerter := NewSlackAlerter("https://hooks.slack.com/services/test", "#alerts", "AlertBot")

	if alerter == nil {
		t.Fatal("NewSlackAlerter should return non-nil")
	}
	if alerter.webhookURL != "https://hooks.slack.com/services/test" {
		t.Errorf("webhookURL mismatch: %s", alerter.webhookURL)
	}
	if alerter.channel != "#alerts" {
		t.Errorf("channel mismatch: %s", alerter.channel)
	}
	if alerter.username != "AlertBot" {
		t.Errorf("username mismatch: %s", alerter.username)
	}
}

func TestSlackAlerter_SkipsEmptyWebhook(t *testing.T) {
	alerter := NewSlackAlerter("", "#alerts", "AlertBot")

	// Should not panic or error
	alerter.Alert(ERROR, "Test", nil)
}

func TestSlackAlerter_SendsToWebhook(t *testing.T) {
	var receivedPayload map[string]any
	var receivedContentType string
	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		receivedContentType = r.Header.Get("Content-Type")

		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedPayload)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	alerter := NewSlackAlerter(server.URL, "#test-channel", "TestBot")

	alerter.Alert(ERROR, "Test alert message", map[string]any{
		"server":      "ws-1",
		"connections": 5000,
	})

	// Give time for HTTP request
	time.Sleep(50 * time.Millisecond)

	if atomic.LoadInt32(&requestCount) != 1 {
		t.Errorf("Expected 1 request, got %d", requestCount)
	}

	if receivedContentType != "application/json" {
		t.Errorf("Content-Type: got %s, want application/json", receivedContentType)
	}

	// Verify payload structure
	if receivedPayload["username"] != "TestBot" {
		t.Errorf("username: got %v, want TestBot", receivedPayload["username"])
	}
	if receivedPayload["channel"] != "#test-channel" {
		t.Errorf("channel: got %v, want #test-channel", receivedPayload["channel"])
	}
	if !strings.Contains(receivedPayload["text"].(string), "ERROR Alert") {
		t.Errorf("text should contain ERROR Alert: %v", receivedPayload["text"])
	}
}

func TestSlackAlerter_getColor(t *testing.T) {
	alerter := &SlackAlerter{}

	tests := []struct {
		level    AuditLevel
		expected string
	}{
		{CRITICAL, "danger"},
		{ERROR, "danger"},
		{WARNING, "warning"},
		{INFO, "good"},
		{DEBUG, "good"},
	}

	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			color := alerter.getColor(tt.level)
			if color != tt.expected {
				t.Errorf("getColor(%s): got %s, want %s", tt.level, color, tt.expected)
			}
		})
	}
}

func TestSlackAlerter_getEmoji(t *testing.T) {
	alerter := &SlackAlerter{}

	tests := []struct {
		level    AuditLevel
		expected string
	}{
		{CRITICAL, ":rotating_light:"},
		{ERROR, ":x:"},
		{WARNING, ":warning:"},
		{INFO, ":information_source:"},
		{DEBUG, ":white_check_mark:"},
	}

	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			emoji := alerter.getEmoji(tt.level)
			if emoji != tt.expected {
				t.Errorf("getEmoji(%s): got %s, want %s", tt.level, emoji, tt.expected)
			}
		})
	}
}

func TestSlackAlerter_IncludesMetadataFields(t *testing.T) {
	var receivedPayload map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	alerter := NewSlackAlerter(server.URL, "#test", "Bot")

	alerter.Alert(WARNING, "Test message", map[string]any{
		"server":      "ws-1",
		"connections": 5000,
	})

	time.Sleep(50 * time.Millisecond)

	// Check attachments contain fields
	attachments, ok := receivedPayload["attachments"].([]any)
	if !ok || len(attachments) == 0 {
		t.Fatal("Should have attachments")
	}

	attachment := attachments[0].(map[string]any)
	fields, ok := attachment["fields"].([]any)
	if !ok {
		t.Fatal("Attachment should have fields")
	}

	// Should have 2 fields from metadata
	if len(fields) != 2 {
		t.Errorf("Expected 2 fields, got %d", len(fields))
	}
}

func TestSlackAlerter_Timeout(t *testing.T) {
	// Create a server that hangs
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Second) // Longer than timeout
	}))
	defer server.Close()

	alerter := NewSlackAlerter(server.URL, "#test", "Bot")

	start := time.Now()
	alerter.Alert(ERROR, "Test", nil)
	elapsed := time.Since(start)

	// Should timeout around 5 seconds
	if elapsed > 6*time.Second {
		t.Errorf("Should timeout at ~5s, took %v", elapsed)
	}
}

// =============================================================================
// ConsoleAlerter Tests
// =============================================================================

func TestNewConsoleAlerter(t *testing.T) {
	alerter := NewConsoleAlerter()

	if alerter == nil {
		t.Fatal("NewConsoleAlerter should return non-nil")
	}
}

func TestConsoleAlerter_Alert(t *testing.T) {
	alerter := NewConsoleAlerter()

	// Just verify it doesn't panic
	alerter.Alert(ERROR, "Test error message", map[string]any{
		"key": "value",
	})
}

func TestConsoleAlerter_NoMetadata(t *testing.T) {
	alerter := NewConsoleAlerter()

	// Just verify it doesn't panic with nil metadata
	alerter.Alert(INFO, "Test message", nil)
}

func TestConsoleAlerter_EmptyMetadata(t *testing.T) {
	alerter := NewConsoleAlerter()

	// Verify it doesn't panic with empty metadata
	alerter.Alert(INFO, "Test message", map[string]any{})
}

func TestConsoleAlerter_AllLevels(t *testing.T) {
	alerter := NewConsoleAlerter()
	levels := []AuditLevel{DEBUG, INFO, WARNING, ERROR, CRITICAL}

	for _, level := range levels {
		t.Run(string(level), func(t *testing.T) {
			// Verify it doesn't panic for any level
			alerter.Alert(level, "test", nil)
		})
	}
}

// =============================================================================
// Alerter Interface Tests
// =============================================================================

func TestAlerterInterface_MockAlerter(t *testing.T) {
	var alerter Alerter = &mockAlerter{}

	alerter.Alert(ERROR, "Test", nil)

	// Interface should work
	if mock, ok := alerter.(*mockAlerter); ok {
		if mock.getAlerts() != 1 {
			t.Error("Alert should have been called")
		}
	}
}

func TestAlerterInterface_SlackAlerter(t *testing.T) {
	var alerter Alerter = NewSlackAlerter("", "", "")

	// Should not panic even with empty config
	alerter.Alert(ERROR, "Test", nil)
}

func TestAlerterInterface_ConsoleAlerter(t *testing.T) {
	var alerter Alerter = NewConsoleAlerter()

	// Just verify it doesn't panic and satisfies the interface
	alerter.Alert(WARNING, "Test", nil)
}
