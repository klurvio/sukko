package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(apiKeyCmd)
	apiKeyCmd.AddCommand(apiKeyCreateCmd, apiKeyListCmd, apiKeyRevokeCmd)

	// create flags
	apiKeyCreateCmd.Flags().String("tenant", "", "Tenant ID (required)")
	apiKeyCreateCmd.Flags().String("name", "", "API key name (optional)")
	_ = apiKeyCreateCmd.MarkFlagRequired("tenant")

	// list flags
	apiKeyListCmd.Flags().String("tenant", "", "Tenant ID (required)")
	_ = apiKeyListCmd.MarkFlagRequired("tenant")

	// revoke flags
	apiKeyRevokeCmd.Flags().String("tenant", "", "Tenant ID (required)")
	apiKeyRevokeCmd.Flags().String("key-id", "", "API key ID (required)")
	_ = apiKeyRevokeCmd.MarkFlagRequired("tenant")
	_ = apiKeyRevokeCmd.MarkFlagRequired("key-id")
}

var apiKeyCmd = &cobra.Command{
	Use:   "api-keys",
	Short: "Manage API keys for tenant authentication",
}

var apiKeyCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new API key",
	RunE: func(cmd *cobra.Command, _ []string) error {
		tenantID, _ := cmd.Flags().GetString("tenant")
		name, _ := cmd.Flags().GetString("name")

		req := map[string]any{}
		if name != "" {
			req["name"] = name
		}

		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := c.CreateAPIKey(tenantID, req)
		if err != nil {
			return fmt.Errorf("create api key: %w", err)
		}
		return printOutput(result, output)
	},
}

var apiKeyListCmd = &cobra.Command{
	Use:   "list",
	Short: "List API keys for a tenant",
	RunE: func(cmd *cobra.Command, _ []string) error {
		tenantID, _ := cmd.Flags().GetString("tenant")

		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := c.ListAPIKeys(tenantID)
		if err != nil {
			return fmt.Errorf("list api keys: %w", err)
		}
		return printOutput(result, output)
	},
}

var apiKeyRevokeCmd = &cobra.Command{
	Use:   "revoke",
	Short: "Revoke an API key",
	RunE: func(cmd *cobra.Command, _ []string) error {
		tenantID, _ := cmd.Flags().GetString("tenant")
		keyID, _ := cmd.Flags().GetString("key-id")

		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := c.RevokeAPIKey(tenantID, keyID)
		if err != nil {
			return fmt.Errorf("revoke api key: %w", err)
		}
		return printOutput(result, output)
	},
}
