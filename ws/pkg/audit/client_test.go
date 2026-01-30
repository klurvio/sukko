package audit

import (
	"strings"
	"testing"

	"github.com/Toniq-Labs/odin-ws/pkg/alerting"
)

func TestClientLogger_IncludesClientID(t *testing.T) {
	t.Parallel()
	logger, buf := testLogger(alerting.DEBUG)
	clientLogger := logger.WithClientID(99999)

	clientLogger.Info("TestEvent", "test message", nil)

	output := buf.String()
	if !strings.Contains(output, `"client_id":99999`) {
		t.Errorf("Should contain client_id: %s", output)
	}
}

func TestClientLogger_AllLevels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		level    alerting.Level
		expected string
	}{
		{"Debug", alerting.DEBUG, `"level":"DEBUG"`},
		{"Info", alerting.INFO, `"level":"INFO"`},
		{"Warning", alerting.WARNING, `"level":"WARNING"`},
		{"Error", alerting.ERROR, `"level":"ERROR"`},
		{"Critical", alerting.CRITICAL, `"level":"CRITICAL"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			logger, buf := testLogger(alerting.DEBUG)
			clientLogger := logger.WithClientID(12345)

			// Call the appropriate method based on level
			switch tt.level {
			case alerting.DEBUG:
				clientLogger.Debug("TestEvent", "test message", nil)
			case alerting.INFO:
				clientLogger.Info("TestEvent", "test message", nil)
			case alerting.WARNING:
				clientLogger.Warning("TestEvent", "test message", nil)
			case alerting.ERROR:
				clientLogger.Error("TestEvent", "test message", nil)
			case alerting.CRITICAL:
				clientLogger.Critical("TestEvent", "test message", nil)
			}

			output := buf.String()
			if !strings.Contains(output, tt.expected) {
				t.Errorf("Should contain %s: %s", tt.expected, output)
			}
			if !strings.Contains(output, `"client_id":12345`) {
				t.Errorf("Should contain client_id: %s", output)
			}
		})
	}
}

func TestClientLogger_WithMetadata(t *testing.T) {
	t.Parallel()
	logger, buf := testLogger(alerting.DEBUG)
	clientLogger := logger.WithClientID(12345)

	clientLogger.Info("TestEvent", "test message", map[string]any{
		"custom_field": "custom_value",
	})

	output := buf.String()
	if !strings.Contains(output, `"custom_field"`) {
		t.Errorf("Should contain metadata: %s", output)
	}
}

func TestClientLogger_Log(t *testing.T) {
	t.Parallel()
	logger, buf := testLogger(alerting.DEBUG)
	clientLogger := logger.WithClientID(54321)

	clientLogger.Log(Event{
		Level:   alerting.WARNING,
		Event:   EventSlowClientDisconnect,
		Message: "Client too slow",
		Metadata: map[string]any{
			"buffer_size": 1000,
		},
	})

	output := buf.String()
	if !strings.Contains(output, `"client_id":54321`) {
		t.Errorf("Should contain client_id: %s", output)
	}
	if !strings.Contains(output, `"level":"WARNING"`) {
		t.Errorf("Should contain level: %s", output)
	}
	if !strings.Contains(output, `"buffer_size"`) {
		t.Errorf("Should contain metadata: %s", output)
	}
}

func TestClientLogger_ClientID(t *testing.T) {
	t.Parallel()
	logger, _ := testLogger(alerting.DEBUG)
	clientLogger := logger.WithClientID(98765)

	if clientLogger.ClientID() != 98765 {
		t.Errorf("ClientID: got %d, want 98765", clientLogger.ClientID())
	}
}

func TestClientLogger_RespectsMinLevel(t *testing.T) {
	t.Parallel()
	logger, buf := testLogger(alerting.WARNING)
	clientLogger := logger.WithClientID(12345)

	// DEBUG and INFO should not be logged
	clientLogger.Debug("TestEvent", "debug", nil)
	clientLogger.Info("TestEvent", "info", nil)

	if buf.Len() > 0 {
		t.Errorf("Should not log DEBUG/INFO when minLevel is WARNING: %s", buf.String())
	}

	// WARNING should be logged
	clientLogger.Warning("TestEvent", "warning", nil)
	if !strings.Contains(buf.String(), `"level":"WARNING"`) {
		t.Errorf("Should log WARNING: %s", buf.String())
	}
}
