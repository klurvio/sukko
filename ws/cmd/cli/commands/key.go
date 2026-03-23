package commands

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(keysCmd)
	keysCmd.AddCommand(keysCreateCmd, keysListCmd, keysRevokeCmd)

	// create flags
	keysCreateCmd.Flags().String("tenant", "", "Tenant ID (uses active tenant from context if not set)")
	keysCreateCmd.Flags().String("algorithm", "", "Signing algorithm: ES256, RS256, EdDSA (required)")
	keysCreateCmd.Flags().String("public-key-file", "", "Path to PEM public key file (required)")
	keysCreateCmd.Flags().String("key-id", "", "Key ID (optional, auto-generated if not set)")
	keysCreateCmd.Flags().String("expires-at", "", "Expiration time (RFC3339)")
	_ = keysCreateCmd.MarkFlagRequired("algorithm")
	_ = keysCreateCmd.MarkFlagRequired("public-key-file")

	// list flags
	keysListCmd.Flags().String("tenant", "", "Tenant ID (uses active tenant from context if not set)")

	// revoke flags
	keysRevokeCmd.Flags().String("tenant", "", "Tenant ID (uses active tenant from context if not set)")
	keysRevokeCmd.Flags().String("key-id", "", "Key ID (required)")
	_ = keysRevokeCmd.MarkFlagRequired("key-id")
}

var keysCmd = &cobra.Command{
	Use:   "keys",
	Short: "Manage JWT signing keys",
}

var keysCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Register a new public key",
	RunE: func(cmd *cobra.Command, _ []string) error {
		tenantFlag, _ := cmd.Flags().GetString("tenant")
		tenantID := resolveTenant(tenantFlag)
		if tenantID == "" {
			return errors.New("tenant ID required (use --tenant or set active tenant in context)")
		}

		algorithm, _ := cmd.Flags().GetString("algorithm")
		pubKeyFile, _ := cmd.Flags().GetString("public-key-file")
		keyID, _ := cmd.Flags().GetString("key-id")
		expiresAt, _ := cmd.Flags().GetString("expires-at")

		pubKey, err := os.ReadFile(pubKeyFile) //nolint:gosec // G304: CLI reads user-specified file path from --public-key-file flag
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

		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := c.CreateKey(cmd.Context(), tenantID, req)
		if err != nil {
			return fmt.Errorf("create key: %w", err)
		}
		return printOutput(result, output)
	},
}

var keysListCmd = &cobra.Command{
	Use:   "list",
	Short: "List keys for a tenant",
	RunE: func(cmd *cobra.Command, _ []string) error {
		tenantFlag, _ := cmd.Flags().GetString("tenant")
		tenantID := resolveTenant(tenantFlag)
		if tenantID == "" {
			return errors.New("tenant ID required (use --tenant or set active tenant in context)")
		}

		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := c.ListKeys(cmd.Context(), tenantID)
		if err != nil {
			return fmt.Errorf("list keys: %w", err)
		}
		return printOutput(result, output)
	},
}

var keysRevokeCmd = &cobra.Command{
	Use:   "revoke",
	Short: "Revoke a key",
	RunE: func(cmd *cobra.Command, _ []string) error {
		tenantFlag, _ := cmd.Flags().GetString("tenant")
		tenantID := resolveTenant(tenantFlag)
		if tenantID == "" {
			return errors.New("tenant ID required (use --tenant or set active tenant in context)")
		}
		keyID, _ := cmd.Flags().GetString("key-id")

		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := c.RevokeKey(cmd.Context(), tenantID, keyID)
		if err != nil {
			return fmt.Errorf("revoke key: %w", err)
		}
		return printOutput(result, output)
	},
}
