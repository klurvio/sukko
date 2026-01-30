package alerting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// SlackAlerter sends alerts to Slack via webhook.
type SlackAlerter struct {
	webhookURL  string
	channel     string
	username    string
	serviceName string
	environment string
	httpClient  *http.Client
}

// SlackConfig holds configuration for SlackAlerter.
type SlackConfig struct {
	WebhookURL  string
	Channel     string
	Username    string
	ServiceName string
	Environment string
	Timeout     time.Duration
}

// NewSlackAlerter creates a Slack alerter with the given webhook configuration.
func NewSlackAlerter(webhookURL, channel, username string) *SlackAlerter {
	return &SlackAlerter{
		webhookURL: webhookURL,
		channel:    channel,
		username:   username,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

// NewSlackAlerterWithConfig creates a Slack alerter with full configuration.
func NewSlackAlerterWithConfig(cfg SlackConfig) *SlackAlerter {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	return &SlackAlerter{
		webhookURL:  cfg.WebhookURL,
		channel:     cfg.Channel,
		username:    cfg.Username,
		serviceName: cfg.ServiceName,
		environment: cfg.Environment,
		httpClient:  &http.Client{Timeout: timeout},
	}
}

// Alert sends an alert to Slack via webhook.
func (s *SlackAlerter) Alert(level Level, message string, metadata map[string]any) {
	if s.webhookURL == "" {
		return // Not configured
	}

	color := s.getColor(level)
	emoji := s.getEmoji(level)

	// Build fields from metadata
	fields := []map[string]any{}

	// Add service context if configured
	if s.serviceName != "" {
		fields = append(fields, map[string]any{
			"title": "Service",
			"value": s.serviceName,
			"short": true,
		})
	}
	if s.environment != "" {
		fields = append(fields, map[string]any{
			"title": "Environment",
			"value": s.environment,
			"short": true,
		})
	}

	// Add metadata fields
	for k, v := range metadata {
		fields = append(fields, map[string]any{
			"title": k,
			"value": fmt.Sprintf("%v", v),
			"short": true,
		})
	}

	footer := "Alert Service"
	if s.serviceName != "" {
		footer = s.serviceName
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
				"footer":    footer,
			},
		},
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return
	}

	// Send to Slack (with timeout)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webhookURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	// Ignore errors - don't want alerting to break the service
	if err == nil && resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
}

func (s *SlackAlerter) getColor(level Level) string {
	switch level {
	case CRITICAL, ERROR:
		return "danger"
	case WARNING:
		return "warning"
	case DEBUG, INFO:
		return "good"
	default:
		return "good"
	}
}

func (s *SlackAlerter) getEmoji(level Level) string {
	switch level {
	case CRITICAL:
		return ":rotating_light:"
	case ERROR:
		return ":x:"
	case WARNING:
		return ":warning:"
	case INFO:
		return ":information_source:"
	case DEBUG:
		return ":white_check_mark:"
	default:
		return ":white_check_mark:"
	}
}

// Ensure SlackAlerter implements Alerter.
var _ Alerter = (*SlackAlerter)(nil)
