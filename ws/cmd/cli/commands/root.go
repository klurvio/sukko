// Package commands implements the odin CLI command tree.
package commands

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/Toniq-Labs/odin-ws/cmd/cli/client"
)

var (
	apiURL string
	token  string
	output string
)

var rootCmd = &cobra.Command{
	Use:   "odin",
	Short: "Odin WS provisioning CLI",
	Long:  "CLI tool for managing tenants, keys, topics, and channel rules in the Odin WS platform.",
}

func init() {
	rootCmd.PersistentFlags().StringVar(&apiURL, "api-url", envOrDefault("ODIN_API_URL", "http://localhost:8080"), "Provisioning API base URL")
	rootCmd.PersistentFlags().StringVar(&token, "token", os.Getenv("ODIN_TOKEN"), "Admin authentication token")
	rootCmd.PersistentFlags().StringVarP(&output, "output", "o", "table", "Output format (json|table)")
}

// Execute runs the root command.
func Execute() error {
	if err := rootCmd.Execute(); err != nil {
		return fmt.Errorf("execute CLI: %w", err)
	}
	return nil
}

// newClient creates an AdminClient from the global flags.
func newClient() *client.AdminClient {
	return client.New(client.Config{
		BaseURL: apiURL,
		Token:   token,
		Timeout: 30 * time.Second,
	})
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
