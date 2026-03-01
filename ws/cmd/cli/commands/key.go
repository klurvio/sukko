package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(keyCmd)
	keyCmd.AddCommand(keyCreateCmd, keyListCmd, keyRevokeCmd)

	// create flags
	keyCreateCmd.Flags().String("tenant", "", "Tenant ID (required)")
	keyCreateCmd.Flags().String("algorithm", "", "Signing algorithm: ES256, RS256, EdDSA (required)")
	keyCreateCmd.Flags().String("public-key-file", "", "Path to PEM public key file (required)")
	keyCreateCmd.Flags().String("key-id", "", "Key ID (optional, auto-generated if not set)")
	keyCreateCmd.Flags().String("expires-at", "", "Expiration time (RFC3339)")
	_ = keyCreateCmd.MarkFlagRequired("tenant")
	_ = keyCreateCmd.MarkFlagRequired("algorithm")
	_ = keyCreateCmd.MarkFlagRequired("public-key-file")

	// list flags
	keyListCmd.Flags().String("tenant", "", "Tenant ID (required)")
	_ = keyListCmd.MarkFlagRequired("tenant")

	// revoke flags
	keyRevokeCmd.Flags().String("tenant", "", "Tenant ID (required)")
	keyRevokeCmd.Flags().String("key-id", "", "Key ID (required)")
	_ = keyRevokeCmd.MarkFlagRequired("tenant")
	_ = keyRevokeCmd.MarkFlagRequired("key-id")
}

var keyCmd = &cobra.Command{
	Use:   "key",
	Short: "Manage API keys",
}

var keyCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Register a new public key",
	RunE: func(cmd *cobra.Command, _ []string) error {
		tenantID, _ := cmd.Flags().GetString("tenant")
		algorithm, _ := cmd.Flags().GetString("algorithm")
		pubKeyFile, _ := cmd.Flags().GetString("public-key-file")
		keyID, _ := cmd.Flags().GetString("key-id")
		expiresAt, _ := cmd.Flags().GetString("expires-at")

		pubKey, err := os.ReadFile(pubKeyFile)
		if err != nil {
			return fmt.Errorf("read public key file: %w", err)
		}

		req := map[string]any{
			"algorithm":  algorithm,
			"public_key": string(pubKey),
		}
		if keyID != "" {
			req["key_id"] = keyID
		}
		if expiresAt != "" {
			req["expires_at"] = expiresAt
		}

		result, err := newClient().CreateKey(tenantID, req)
		if err != nil {
			return fmt.Errorf("create key: %w", err)
		}
		return printOutput(result, output)
	},
}

var keyListCmd = &cobra.Command{
	Use:   "list",
	Short: "List keys for a tenant",
	RunE: func(cmd *cobra.Command, _ []string) error {
		tenantID, _ := cmd.Flags().GetString("tenant")

		result, err := newClient().ListKeys(tenantID)
		if err != nil {
			return fmt.Errorf("list keys: %w", err)
		}
		return printOutput(result, output)
	},
}

var keyRevokeCmd = &cobra.Command{
	Use:   "revoke",
	Short: "Revoke a key",
	RunE: func(cmd *cobra.Command, _ []string) error {
		tenantID, _ := cmd.Flags().GetString("tenant")
		keyID, _ := cmd.Flags().GetString("key-id")

		result, err := newClient().RevokeKey(tenantID, keyID)
		if err != nil {
			return fmt.Errorf("revoke key: %w", err)
		}
		return printOutput(result, output)
	},
}
