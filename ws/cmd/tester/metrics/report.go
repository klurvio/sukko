package metrics

import (
	"encoding/json"
	"fmt"
	"io"
)

// Report is the final test report.
type Report struct {
	TestType string          `json:"test_type"`
	Status   string          `json:"status"` // pass, fail, error
	Metrics  MetricsSnapshot `json:"metrics"`
	Checks   []CheckResult   `json:"checks,omitempty"`
	Errors   []string        `json:"errors,omitempty"`
}

// CheckResult is an individual test check result (for smoke/validate).
type CheckResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // pass, fail, skip
	Latency string `json:"latency,omitempty"`
	Error   string `json:"error,omitempty"`
}

func (r *Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// WriteTable renders the report as a human-readable table. fmt.Fprintf errors
// are intentionally ignored throughout: the writer is typically os.Stdout and
// a write failure there is non-recoverable.
func (r *Report) WriteTable(w io.Writer) {
	fmt.Fprintf(w, "\n=== Test Report: %s ===\n", r.TestType)
	fmt.Fprintf(w, "Status: %s\n", r.Status)
	fmt.Fprintf(w, "Duration: %s\n\n", r.Metrics.Elapsed)

	if len(r.Checks) > 0 {
		fmt.Fprintln(w, "Checks:")
		for _, c := range r.Checks {
			detail := ""
			if c.Latency != "" {
				detail = " (" + c.Latency + ")"
			}
			if c.Error != "" {
				detail = " — " + c.Error
			}
			fmt.Fprintf(w, "  [%s] %s%s\n", c.Status, c.Name, detail)
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, "Metrics:")
	fmt.Fprintf(w, "  Connections: %d active, %d total, %d failed\n",
		r.Metrics.ConnectionsActive, r.Metrics.ConnectionsTotal, r.Metrics.ConnectionsFailed)
	fmt.Fprintf(w, "  Messages: %d sent, %d received, %d dropped\n",
		r.Metrics.MessagesSent, r.Metrics.MessagesReceived, r.Metrics.MessagesDropped)

	lat := r.Metrics.Latency
	if lat.Count > 0 {
		fmt.Fprintf(w, "  Latency: p50=%.1fms p95=%.1fms p99=%.1fms p999=%.1fms\n",
			lat.P50, lat.P95, lat.P99, lat.P999)
	}

	if len(r.Errors) > 0 {
		fmt.Fprintln(w, "\nErrors:")
		for _, e := range r.Errors {
			fmt.Fprintf(w, "  - %s\n", e)
		}
	}
}
