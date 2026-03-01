package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(oidcCmd)
	oidcCmd.AddCommand(oidcGetCmd, oidcCreateCmd, oidcUpdateCmd, oidcDeleteCmd)

	// get
	oidcGetCmd.Flags().String("tenant", "", "Tenant ID (required)")
	_ = oidcGetCmd.MarkFlagRequired("tenant")

	// create
	oidcCreateCmd.Flags().String("tenant", "", "Tenant ID (required)")
	oidcCreateCmd.Flags().String("issuer-url", "", "OIDC issuer URL (required)")
	oidcCreateCmd.Flags().String("jwks-url", "", "JWKS endpoint URL")
	oidcCreateCmd.Flags().String("audience", "", "Expected audience claim")
	_ = oidcCreateCmd.MarkFlagRequired("tenant")
	_ = oidcCreateCmd.MarkFlagRequired("issuer-url")

	// update
	oidcUpdateCmd.Flags().String("tenant", "", "Tenant ID (required)")
	oidcUpdateCmd.Flags().String("issuer-url", "", "New issuer URL")
	oidcUpdateCmd.Flags().String("jwks-url", "", "New JWKS URL")
	oidcUpdateCmd.Flags().String("audience", "", "New audience")
	oidcUpdateCmd.Flags().Bool("enabled", true, "Enable/disable OIDC")
	_ = oidcUpdateCmd.MarkFlagRequired("tenant")

	// delete
	oidcDeleteCmd.Flags().String("tenant", "", "Tenant ID (required)")
	_ = oidcDeleteCmd.MarkFlagRequired("tenant")
}

var oidcCmd = &cobra.Command{
	Use:   "oidc",
	Short: "Manage OIDC configuration",
}

var oidcGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Get OIDC config for a tenant",
	RunE: func(cmd *cobra.Command, _ []string) error {
		tenantID, _ := cmd.Flags().GetString("tenant")

		result, err := newClient().GetOIDCConfig(tenantID)
		if err != nil {
			return fmt.Errorf("get OIDC config: %w", err)
		}
		return printOutput(result, output)
	},
}

var oidcCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create OIDC config for a tenant",
	RunE: func(cmd *cobra.Command, _ []string) error {
		tenantID, _ := cmd.Flags().GetString("tenant")
		issuerURL, _ := cmd.Flags().GetString("issuer-url")
		jwksURL, _ := cmd.Flags().GetString("jwks-url")
		audience, _ := cmd.Flags().GetString("audience")

		req := map[string]any{
			"issuer_url": issuerURL,
		}
		if jwksURL != "" {
			req["jwks_url"] = jwksURL
		}
		if audience != "" {
			req["audience"] = audience
		}

		result, err := newClient().CreateOIDCConfig(tenantID, req)
		if err != nil {
			return fmt.Errorf("create OIDC config: %w", err)
		}
		return printOutput(result, output)
	},
}

var oidcUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update OIDC config for a tenant",
	RunE: func(cmd *cobra.Command, _ []string) error {
		tenantID, _ := cmd.Flags().GetString("tenant")

		req := map[string]any{}
		if cmd.Flags().Changed("issuer-url") {
			v, _ := cmd.Flags().GetString("issuer-url")
			req["issuer_url"] = v
		}
		if cmd.Flags().Changed("jwks-url") {
			v, _ := cmd.Flags().GetString("jwks-url")
			req["jwks_url"] = v
		}
		if cmd.Flags().Changed("audience") {
			v, _ := cmd.Flags().GetString("audience")
			req["audience"] = v
		}
		if cmd.Flags().Changed("enabled") {
			v, _ := cmd.Flags().GetBool("enabled")
			req["enabled"] = v
		}

		result, err := newClient().UpdateOIDCConfig(tenantID, req)
		if err != nil {
			return fmt.Errorf("update OIDC config: %w", err)
		}
		return printOutput(result, output)
	},
}

var oidcDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete OIDC config for a tenant",
	RunE: func(cmd *cobra.Command, _ []string) error {
		tenantID, _ := cmd.Flags().GetString("tenant")

		result, err := newClient().DeleteOIDCConfig(tenantID)
		if err != nil {
			return fmt.Errorf("delete OIDC config: %w", err)
		}
		return printOutput(result, output)
	},
}
