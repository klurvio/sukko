// Package commands implements the sukko CLI command tree.
package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/klurvio/sukko/cmd/cli/client"
)

// CLI default values. Env vars take precedence via envOrDefault; CLI flags take precedence over env.
const (
	defaultAPIURL = "http://localhost:8080"
	defaultOutput = "table"
)

var (
	apiURL string
	token  string
	output string
)

var rootCmd = &cobra.Command{
	Use:   "sukko",
	Short: "Sukko WS provisioning CLI",
	Long:  "CLI tool for managing tenants, keys, topics, and channel rules in the Sukko WS platform.",
}

func init() {
	rootCmd.PersistentFlags().StringVar(&apiURL, "api-url", envOrDefault("SUKKO_API_URL", defaultAPIURL), "Provisioning API base URL")
	rootCmd.PersistentFlags().StringVar(&token, "token", os.Getenv("SUKKO_TOKEN"), "Admin authentication token")
	rootCmd.PersistentFlags().StringVarP(&output, "output", "o", defaultOutput, "Output format (json|table)")
}

// Execute runs the root command.
func Execute() error {
	if err := rootCmd.Execute(); err != nil {
		return fmt.Errorf("execute CLI: %w", err)
	}
	return nil
}

// newClient creates an AdminClient from the global flags.
// Returns an error if client creation fails (e.g., empty BaseURL).
func newClient() (*client.AdminClient, error) {
	c, err := client.New(client.Config{
		BaseURL: apiURL,
		Token:   token,
		Timeout: client.DefaultClientTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("create admin client: %w", err)
	}
	return c, nil
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
