package monitoring

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Alerter interface for sending notifications to external services
// Implementations: Slack, Email, PagerDuty, etc.
type Alerter interface {
	Alert(level AuditLevel, message string, metadata map[string]any)
}

// MultiAlerter sends alerts to multiple alerters
// Example: Send to both Slack and Email
type MultiAlerter struct {
	alerters []Alerter
}

func NewMultiAlerter(alerters ...Alerter) *MultiAlerter {
	return &MultiAlerter{alerters: alerters}
}

func (m *MultiAlerter) Alert(level AuditLevel, message string, metadata map[string]any) {
	for _, alerter := range m.alerters {
		// Run in goroutine to avoid blocking
		go alerter.Alert(level, message, metadata)
	}
}

// SlackAlerter sends alerts to Slack via webhook
type SlackAlerter struct {
	webhookURL string
	channel    string
	username   string
}

func NewSlackAlerter(webhookURL, channel, username string) *SlackAlerter {
	return &SlackAlerter{
		webhookURL: webhookURL,
		channel:    channel,
		username:   username,
	}
}

func (s *SlackAlerter) Alert(level AuditLevel, message string, metadata map[string]any) {
	if s.webhookURL == "" {
		return // Not configured
	}

	color := s.getColor(level)
	emoji := s.getEmoji(level)

	// Build fields from metadata
	fields := []map[string]any{}
	for k, v := range metadata {
		fields = append(fields, map[string]any{
			"title": k,
			"value": fmt.Sprintf("%v", v),
			"short": true,
		})
	}

	payload := map[string]any{
		"username": s.username,
		"channel":  s.channel,
		"text":     fmt.Sprintf("%s *%s Alert*", emoji, level),
		"attachments": []map[string]any{
			{
				"color":     color,
				"title":     message,
				"fields":    fields,
				"timestamp": time.Now().Unix(),
				"footer":    "WebSocket Server",
			},
		},
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return
	}

	// Send to Slack (with timeout)
	client := &http.Client{Timeout: 5 * time.Second}
	_, _ = client.Post(s.webhookURL, "application/json", bytes.NewBuffer(jsonPayload))
	// Ignore errors - don't want alerting to break the server
}

func (s *SlackAlerter) getColor(level AuditLevel) string {
	switch level {
	case CRITICAL:
		return "danger"
	case ERROR:
		return "danger"
	case WARNING:
		return "warning"
	default:
		return "good"
	}
}

func (s *SlackAlerter) getEmoji(level AuditLevel) string {
	switch level {
	case CRITICAL:
		return ":rotating_light:"
	case ERROR:
		return ":x:"
	case WARNING:
		return ":warning:"
	case INFO:
		return ":information_source:"
	default:
		return ":white_check_mark:"
	}
}

// ConsoleAlerter prints alerts to console (for development/testing)
type ConsoleAlerter struct {
	writer io.Writer
}

// NewConsoleAlerter creates a ConsoleAlerter that writes to stdout
func NewConsoleAlerter() *ConsoleAlerter {
	return &ConsoleAlerter{writer: nil} // nil means os.Stdout
}

// NewConsoleAlerterWithWriter creates a ConsoleAlerter with a custom writer (for testing)
func NewConsoleAlerterWithWriter(w io.Writer) *ConsoleAlerter {
	return &ConsoleAlerter{writer: w}
}

func (c *ConsoleAlerter) getWriter() io.Writer {
	if c.writer != nil {
		return c.writer
	}
	return defaultWriter
}

// defaultWriter is os.Stdout, defined as a variable for easy testing
var defaultWriter io.Writer = nil // initialized in init or first use

func init() {
	// Can't import os in init easily, so we handle nil as stdout in getWriter
}

func (c *ConsoleAlerter) Alert(level AuditLevel, message string, metadata map[string]any) {
	w := c.getWriter()
	if w == nil {
		// Default to stdout via fmt (original behavior)
		fmt.Printf("\n🔔 ALERT [%s]: %s\n", level, message)
		if len(metadata) > 0 {
			fmt.Println("  Metadata:")
			for k, v := range metadata {
				fmt.Printf("    %s: %v\n", k, v)
			}
		}
		fmt.Println()
		return
	}

	// Write to custom writer (ignoring errors - alerting should not break the server)
	_, _ = fmt.Fprintf(w, "\n🔔 ALERT [%s]: %s\n", level, message)
	if len(metadata) > 0 {
		_, _ = fmt.Fprintln(w, "  Metadata:")
		for k, v := range metadata {
			_, _ = fmt.Fprintf(w, "    %s: %v\n", k, v)
		}
	}
	_, _ = fmt.Fprintln(w)
}
