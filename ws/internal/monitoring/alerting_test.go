package monitoring

import (
	"bytes"
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
	t.Parallel()
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
	t.Parallel()
	multi := NewMultiAlerter()

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
	t.Parallel()
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

func (b *blockingAlerter) Alert(_ AuditLevel, _ string, _ map[string]any) {
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
	t.Parallel()
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
	t.Parallel()
	// Track if any HTTP request was made
	var requestMade atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestMade.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create alerter with empty webhook URL
	alerter := NewSlackAlerter("", "#alerts", "AlertBot")

	alerter.Alert(ERROR, "Test", nil)

	// Give time for any potential HTTP request
	time.Sleep(50 * time.Millisecond)

	// Verify NO request was made (empty webhook should skip)
	if requestMade.Load() != 0 {
		t.Error("SlackAlerter with empty webhook should not make HTTP requests")
	}
}

func TestSlackAlerter_SendsToWebhook(t *testing.T) {
	t.Parallel()
	var receivedPayload map[string]any
	var receivedContentType string
	var requestCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
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

	if requestCount.Load() != 1 {
		t.Errorf("Expected 1 request, got %d", requestCount.Load())
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
	t.Parallel()
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
			t.Parallel()
			color := alerter.getColor(tt.level)
			if color != tt.expected {
				t.Errorf("getColor(%s): got %s, want %s", tt.level, color, tt.expected)
			}
		})
	}
}

func TestSlackAlerter_getEmoji(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			emoji := alerter.getEmoji(tt.level)
			if emoji != tt.expected {
				t.Errorf("getEmoji(%s): got %s, want %s", tt.level, emoji, tt.expected)
			}
		})
	}
}

func TestSlackAlerter_IncludesMetadataFields(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	// Create a server that hangs
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(10 * time.Second) // Longer than timeout
	}))
	defer func() {
		server.CloseClientConnections() // Force close hanging connections
		server.Close()
	}()

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
	t.Parallel()
	alerter := NewConsoleAlerter()

	if alerter == nil {
		t.Fatal("NewConsoleAlerter should return non-nil")
	}
}

func TestConsoleAlerter_Alert(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	alerter := NewConsoleAlerterWithWriter(&buf)

	alerter.Alert(ERROR, "Test error message", map[string]any{
		"key": "value",
	})

	output := buf.String()

	// Verify output contains expected content
	if !strings.Contains(output, "ALERT") {
		t.Error("Output should contain 'ALERT'")
	}
	if !strings.Contains(output, "ERROR") {
		t.Error("Output should contain 'ERROR'")
	}
	if !strings.Contains(output, "Test error message") {
		t.Error("Output should contain the message")
	}
	if !strings.Contains(output, "key") {
		t.Error("Output should contain metadata key")
	}
	if !strings.Contains(output, "value") {
		t.Error("Output should contain metadata value")
	}
}

func TestConsoleAlerter_NoMetadata(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	alerter := NewConsoleAlerterWithWriter(&buf)

	alerter.Alert(INFO, "Test message", nil)

	output := buf.String()

	// Verify output contains alert but no "Metadata:" section
	if !strings.Contains(output, "INFO") {
		t.Error("Output should contain 'INFO'")
	}
	if !strings.Contains(output, "Test message") {
		t.Error("Output should contain the message")
	}
	if strings.Contains(output, "Metadata:") {
		t.Error("Output should NOT contain 'Metadata:' when nil metadata passed")
	}
}

func TestConsoleAlerter_EmptyMetadata(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	alerter := NewConsoleAlerterWithWriter(&buf)

	alerter.Alert(INFO, "Test message", map[string]any{})

	output := buf.String()

	// Verify output contains alert but no "Metadata:" section for empty map
	if !strings.Contains(output, "INFO") {
		t.Error("Output should contain 'INFO'")
	}
	if strings.Contains(output, "Metadata:") {
		t.Error("Output should NOT contain 'Metadata:' when empty metadata passed")
	}
}

func TestConsoleAlerter_AllLevels(t *testing.T) {
	t.Parallel()
	levels := []AuditLevel{DEBUG, INFO, WARNING, ERROR, CRITICAL}

	for _, level := range levels {
		t.Run(string(level), func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			alerter := NewConsoleAlerterWithWriter(&buf)

			alerter.Alert(level, "test message", nil)

			output := buf.String()

			// Verify each level is properly formatted in output
			if !strings.Contains(output, string(level)) {
				t.Errorf("Output should contain level %q, got: %s", level, output)
			}
			if !strings.Contains(output, "test message") {
				t.Errorf("Output should contain message, got: %s", output)
			}
		})
	}
}

// =============================================================================
// Alerter Interface Tests
// =============================================================================

func TestAlerterInterface_MockAlerter(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	var alerter Alerter = NewSlackAlerter("", "", "")

	// Should not panic even with empty config
	alerter.Alert(ERROR, "Test", nil)
}

func TestAlerterInterface_ConsoleAlerter(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	var alerter Alerter = NewConsoleAlerterWithWriter(&buf)

	alerter.Alert(WARNING, "Test message", map[string]any{"server": "ws-1"})

	output := buf.String()

	// Verify interface implementation produces expected output
	if !strings.Contains(output, "WARNING") {
		t.Error("Output should contain 'WARNING'")
	}
	if !strings.Contains(output, "Test message") {
		t.Error("Output should contain the message")
	}
	if !strings.Contains(output, "server") {
		t.Error("Output should contain metadata key")
	}
}
