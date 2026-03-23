package commands

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const testHTTPTimeout = 30 * time.Second

var (
	testConnections int
	testDuration    string
	testPublishRate int
	testRampRate    int
	testSuite       string
	testTesterURL   string
	testFollow      bool
)

func init() {
	testCmd.AddCommand(testSmokeCmd, testLoadCmd, testStressCmd, testSoakCmd, testValidateCmd)

	// Common flags on all test subcommands
	for _, cmd := range []*cobra.Command{testLoadCmd, testStressCmd, testSoakCmd} {
		cmd.Flags().IntVar(&testConnections, "connections", 10, "Number of WebSocket connections")
		cmd.Flags().StringVar(&testDuration, "duration", "30s", "Test duration (e.g., 30s, 5m, 1h)")
		cmd.Flags().IntVar(&testPublishRate, "publish-rate", 10, "Messages per second")
		cmd.Flags().IntVar(&testRampRate, "ramp-rate", 5, "Connections per second during ramp-up")
	}

	testValidateCmd.Flags().StringVar(&testSuite, "suite", "auth", "Validation suite (auth, channels, ordering)")

	// Tester URL on all subcommands
	for _, cmd := range []*cobra.Command{testSmokeCmd, testLoadCmd, testStressCmd, testSoakCmd, testValidateCmd} {
		cmd.Flags().StringVar(&testTesterURL, "tester-url", "", "Tester service URL (overrides context)")
		cmd.Flags().BoolVarP(&testFollow, "follow", "f", false, "Stream metrics in real-time")
	}

	rootCmd.AddCommand(testCmd)
}

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Run tests against the Sukko platform",
	Long:  "Run smoke, load, stress, soak, or validation tests via the sukko-tester service.",
}

var testSmokeCmd = &cobra.Command{
	Use:   "smoke",
	Short: "Quick connectivity and health check",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runTest(cmd, "smoke", nil)
	},
}

var testLoadCmd = &cobra.Command{
	Use:   "load",
	Short: "Sustained load test with configurable connections",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runTest(cmd, "load", map[string]any{
			"connections":  testConnections,
			"duration":     testDuration,
			"publish_rate": testPublishRate,
			"ramp_rate":    testRampRate,
		})
	},
}

var testStressCmd = &cobra.Command{
	Use:   "stress",
	Short: "Push to maximum capacity and find limits",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runTest(cmd, "stress", map[string]any{
			"connections":  testConnections,
			"duration":     testDuration,
			"publish_rate": testPublishRate,
			"ramp_rate":    testRampRate,
		})
	},
}

var testSoakCmd = &cobra.Command{
	Use:   "soak",
	Short: "Long-running stability test",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runTest(cmd, "soak", map[string]any{
			"connections":  testConnections,
			"duration":     testDuration,
			"publish_rate": testPublishRate,
		})
	},
}

var testValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Run validation suites (auth, channels, ordering)",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runTest(cmd, "validate", map[string]any{
			"suite": testSuite,
		})
	},
}

func runTest(cmd *cobra.Command, testType string, extra map[string]any) error {
	testerURL := resolveTesterURL(testTesterURL)
	_, tok := resolveClientConfig()

	body := map[string]any{"type": testType}
	for k, v := range extra {
		body[k] = v
	}

	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	client := &http.Client{Timeout: testHTTPTimeout}

	req, err := http.NewRequestWithContext(cmd.Context(), http.MethodPost, testerURL+"/api/v1/tests", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("start test: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		var errResp map[string]string
		json.NewDecoder(resp.Body).Decode(&errResp) //nolint:errcheck // best-effort error decode
		msg := errResp["message"]
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("start test failed: %s", msg)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	testID, _ := result["id"].(string)
	if testID == "" {
		return fmt.Errorf("server response missing test ID")
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Test started: %s (type: %s)\n", testID, testType)

	if !testFollow {
		if output == "json" {
			return printJSON(result)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Use --follow to stream metrics, or run:\n  sukko test status --id %s\n", testID)
		return nil
	}

	// Stream metrics via SSE
	return streamTestMetrics(cmd, testerURL, testID, tok)
}

func streamTestMetrics(cmd *cobra.Command, testerURL, testID, tok string) error {
	req, err := http.NewRequestWithContext(cmd.Context(), http.MethodGet, testerURL+"/api/v1/tests/"+url.PathEscape(testID)+"/metrics", nil)
	if err != nil {
		return fmt.Errorf("create stream request: %w", err)
	}
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	client := &http.Client{Timeout: 0} // no timeout for SSE
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connect to metrics stream: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("metrics stream returned %s", resp.Status)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event: complete") {
			// Next data line is the final report
			if scanner.Scan() {
				reportLine := scanner.Text()
				reportData := strings.TrimPrefix(reportLine, "data: ")
				if output == "json" {
					fmt.Fprintln(cmd.OutOrStdout(), reportData)
				} else {
					printTestReport(cmd, reportData)
				}
			}
			return nil
		}

		if strings.HasPrefix(line, "data: ") {
			metricsData := strings.TrimPrefix(line, "data: ")
			if output == "json" {
				fmt.Fprintln(cmd.OutOrStdout(), metricsData)
			} else {
				printMetricsLine(cmd, metricsData)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read metrics stream: %w", err)
	}
	return nil
}

func printMetricsLine(cmd *cobra.Command, data string) {
	var m map[string]any
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		return // best-effort display: malformed metrics line is non-fatal
	}

	elapsed, _ := m["elapsed"].(string)
	conns, _ := m["connections_active"].(float64)
	sent, _ := m["messages_sent"].(float64)
	recv, _ := m["messages_received"].(float64)
	errors, _ := m["errors_total"].(float64)

	fmt.Fprintf(cmd.OutOrStdout(), "\r[%s] conns=%d sent=%d recv=%d errors=%d",
		elapsed, int(conns), int(sent), int(recv), int(errors))
}

func printTestReport(cmd *cobra.Command, data string) {
	var report map[string]any
	if err := json.Unmarshal([]byte(data), &report); err != nil {
		fmt.Fprintln(cmd.OutOrStdout(), "\n"+data)
		return
	}

	status, _ := report["status"].(string)
	testType, _ := report["test_type"].(string)

	fmt.Fprintf(cmd.OutOrStdout(), "\n\n=== Test Report: %s ===\n", testType)

	statusColor := colorGreen
	if status != "pass" {
		statusColor = colorRed
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Status: %s%s%s\n", statusColor, status, colorReset)

	if checks, ok := report["checks"].([]any); ok && len(checks) > 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "\nChecks:")
		for _, c := range checks {
			check, ok := c.(map[string]any)
			if !ok {
				continue
			}
			name, _ := check["name"].(string)
			checkStatus, _ := check["status"].(string)
			indicator := colorGreen + "PASS" + colorReset
			if checkStatus != "pass" {
				indicator = colorRed + "FAIL" + colorReset
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  [%s] %s", indicator, name)
			if latency, ok := check["latency"].(string); ok && latency != "" {
				fmt.Fprintf(cmd.OutOrStdout(), " (%s)", latency)
			}
			if errMsg, ok := check["error"].(string); ok && errMsg != "" {
				fmt.Fprintf(cmd.OutOrStdout(), " — %s", errMsg)
			}
			fmt.Fprintln(cmd.OutOrStdout())
		}
	}

	if metrics, ok := report["metrics"].(map[string]any); ok {
		fmt.Fprintln(cmd.OutOrStdout(), "\nMetrics:")
		connsActive, _ := metrics["connections_active"].(float64)
		connsTotal, _ := metrics["connections_total"].(float64)
		connsFailed, _ := metrics["connections_failed"].(float64)
		sent, _ := metrics["messages_sent"].(float64)
		recv, _ := metrics["messages_received"].(float64)
		dropped, _ := metrics["messages_dropped"].(float64)
		fmt.Fprintf(cmd.OutOrStdout(), "  Connections: %d active, %d total, %d failed\n",
			int(connsActive), int(connsTotal), int(connsFailed))
		fmt.Fprintf(cmd.OutOrStdout(), "  Messages: %d sent, %d received, %d dropped\n",
			int(sent), int(recv), int(dropped))
	}

	if errors, ok := report["errors"].([]any); ok && len(errors) > 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "\nErrors:")
		for _, e := range errors {
			fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", e)
		}
	}
}
