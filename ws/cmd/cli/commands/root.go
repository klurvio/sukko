// Package commands implements the sukko CLI command tree.
package commands

import (
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/klurvio/sukko/cmd/cli/client"
)

var (
	apiURL string
	token  string
	output string
)

var rootCmd = &cobra.Command{
	Use:   "sukko",
	Short: "Sukko provisioning CLI",
	Long:  "CLI tool for managing tenants, keys, topics, and channel rules in the Sukko platform.",
}

func init() {
	rootCmd.PersistentFlags().StringVar(&apiURL, "api-url", envOrDefault("SUKKO_API_URL", "http://localhost:8080"), "Provisioning API base URL")
	rootCmd.PersistentFlags().StringVar(&token, "token", os.Getenv("SUKKO_TOKEN"), "Admin authentication token")
	rootCmd.PersistentFlags().StringVarP(&output, "output", "o", "table", "Output format (json|table)")
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
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
