package audit

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/klurvio/sukko/internal/shared/alerting"
)

func TestEventConstants(t *testing.T) {
	t.Parallel()

	// Verify common event constants are defined
	events := []string{
		EventAuthSuccess,
		EventAuthFailure,
		EventAccessDenied,
		EventRateLimited,
		EventConnectionEstablished,
		EventSlowClientDisconnect,
		EventServerAtCapacity,
		EventTenantCreated,
		EventServerStarted,
	}

	for _, e := range events {
		if e == "" {
			t.Errorf("Event constant should not be empty")
		}
	}
}

func TestEvent_JSONMarshaling(t *testing.T) {
	t.Parallel()
	clientID := int64(12345)
	event := Event{
		Level:     alerting.WARNING,
		Timestamp: time.Date(2025, 10, 3, 12, 0, 0, 0, time.UTC),
		Event:     EventSlowClientDisconnect,
		ClientID:  &clientID,
		Message:   "Client disconnected due to slow consumption",
		Metadata: map[string]any{
			"pending_messages": 500,
			"buffer_full":      true,
		},
	}

	jsonBytes, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Failed to marshal Event: %v", err)
	}

	output := string(jsonBytes)

	// Verify required fields
	if !strings.Contains(output, `"level":"WARNING"`) {
		t.Errorf("JSON should contain level: %s", output)
	}
	if !strings.Contains(output, `"event":"SlowClientDisconnect"`) {
		t.Errorf("JSON should contain event: %s", output)
	}
	if !strings.Contains(output, `"client_id":12345`) {
		t.Errorf("JSON should contain client_id: %s", output)
	}
	if !strings.Contains(output, `"message"`) {
		t.Errorf("JSON should contain message: %s", output)
	}
}

func TestEvent_OmitsNilClientID(t *testing.T) {
	t.Parallel()
	event := Event{
		Level:   alerting.INFO,
		Event:   EventServerStarted,
		Message: "Server started successfully",
	}

	jsonBytes, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Failed to marshal Event: %v", err)
	}

	output := string(jsonBytes)

	// client_id should be omitted when nil
	if strings.Contains(output, "client_id") {
		t.Errorf("JSON should omit nil client_id: %s", output)
	}
}

func TestEvent_OmitsEmptyMetadata(t *testing.T) {
	t.Parallel()
	event := Event{
		Level:   alerting.INFO,
		Event:   EventServerStarted,
		Message: "Server started successfully",
	}

	jsonBytes, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Failed to marshal Event: %v", err)
	}

	output := string(jsonBytes)

	// metadata should be omitted when nil
	if strings.Contains(output, "metadata") {
		t.Errorf("JSON should omit nil metadata: %s", output)
	}
}

func TestEvent_UnmarshalRoundTrip(t *testing.T) {
	t.Parallel()
	clientID := int64(42)
	original := Event{
		Level:     alerting.ERROR,
		Timestamp: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Event:     EventAuthFailure,
		ClientID:  &clientID,
		Message:   "Invalid token",
		Metadata:  map[string]any{"reason": "expired"},
	}

	jsonBytes, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var decoded Event
	if err := json.Unmarshal(jsonBytes, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if decoded.Level != original.Level {
		t.Errorf("Level mismatch: got %s, want %s", decoded.Level, original.Level)
	}
	if decoded.Event != original.Event {
		t.Errorf("Event mismatch: got %s, want %s", decoded.Event, original.Event)
	}
	if *decoded.ClientID != *original.ClientID {
		t.Errorf("ClientID mismatch: got %d, want %d", *decoded.ClientID, *original.ClientID)
	}
}
