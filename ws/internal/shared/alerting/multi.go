package alerting

// MultiAlerter sends alerts to multiple alerters.
// Example: Send to both Slack and Email.
type MultiAlerter struct {
	alerters []Alerter
}

// NewMultiAlerter creates a MultiAlerter that fans out to multiple alerters.
func NewMultiAlerter(alerters ...Alerter) *MultiAlerter {
	return &MultiAlerter{alerters: alerters}
}

// Alert sends an alert to all configured alerters concurrently.
func (m *MultiAlerter) Alert(level Level, message string, metadata map[string]any) {
	for _, alerter := range m.alerters {
		// Run in goroutine to avoid blocking
		go alerter.Alert(level, message, metadata)
	}
}

// Ensure MultiAlerter implements Alerter.
var _ Alerter = (*MultiAlerter)(nil)
