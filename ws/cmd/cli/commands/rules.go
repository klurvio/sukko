package commands

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(rulesCmd)
	rulesCmd.AddCommand(routingCmd, channelsCmd, rulesTestAccessCmd)

	// routing subcommands
	routingCmd.AddCommand(routingGetCmd, routingSetCmd, routingDeleteCmd)
	routingGetCmd.Flags().String("tenant", "", "Tenant ID (uses active tenant from context if not set)")
	routingSetCmd.Flags().String("tenant", "", "Tenant ID (uses active tenant from context if not set)")
	routingSetCmd.Flags().String("file", "", "Path to JSON routing rules file (required)")
	_ = routingSetCmd.MarkFlagRequired("file")
	routingDeleteCmd.Flags().String("tenant", "", "Tenant ID (uses active tenant from context if not set)")

	// channels subcommands
	channelsCmd.AddCommand(channelsGetCmd, channelsSetCmd, channelsDeleteCmd)
	channelsGetCmd.Flags().String("tenant", "", "Tenant ID (uses active tenant from context if not set)")
	channelsSetCmd.Flags().String("tenant", "", "Tenant ID (uses active tenant from context if not set)")
	channelsSetCmd.Flags().StringSlice("public", nil, "Public channel patterns (repeatable)")
	channelsSetCmd.Flags().String("file", "", "Path to JSON channel rules file (alternative to --public)")
	channelsDeleteCmd.Flags().String("tenant", "", "Tenant ID (uses active tenant from context if not set)")

	// test-access
	rulesTestAccessCmd.Flags().String("tenant", "", "Tenant ID (uses active tenant from context if not set)")
	rulesTestAccessCmd.Flags().String("sub", "", "Subject (user ID) to test access for")
	rulesTestAccessCmd.Flags().String("channel", "", "Channel to test access for")
	rulesTestAccessCmd.Flags().StringSlice("group", nil, "Groups to test access for")
}

var rulesCmd = &cobra.Command{
	Use:   "rules",
	Short: "Manage routing and channel rules",
}

// ─── Routing Rules ─────────────────────────────────────────────

var routingCmd = &cobra.Command{
	Use:   "routing",
	Short: "Manage topic routing rules",
}

var routingGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Get routing rules for a tenant",
	RunE: func(cmd *cobra.Command, _ []string) error {
		tenantID := resolveTenantFromCmd(cmd)
		if tenantID == "" {
			return fmt.Errorf("tenant ID required (use --tenant or set active tenant in context)")
		}

		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := c.GetRoutingRules(cmd.Context(), tenantID)
		if err != nil {
			return fmt.Errorf("get routing rules: %w", err)
		}
		return printOutput(result, output)
	},
}

var routingSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Set routing rules for a tenant from a JSON file",
	RunE: func(cmd *cobra.Command, _ []string) error {
		tenantID := resolveTenantFromCmd(cmd)
		if tenantID == "" {
			return fmt.Errorf("tenant ID required (use --tenant or set active tenant in context)")
		}

		rulesFile, _ := cmd.Flags().GetString("file")
		data, err := os.ReadFile(rulesFile) //nolint:gosec // G304: CLI reads user-specified file path from --file flag
		if err != nil {
			return fmt.Errorf("read rules file: %w", err)
		}

		var req map[string]any
		if err := json.Unmarshal(data, &req); err != nil {
			return fmt.Errorf("parse rules file: %w", err)
		}

		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := c.SetRoutingRules(cmd.Context(), tenantID, req)
		if err != nil {
			return fmt.Errorf("set routing rules: %w", err)
		}
		return printOutput(result, output)
	},
}

var routingDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete routing rules for a tenant",
	RunE: func(cmd *cobra.Command, _ []string) error {
		tenantID := resolveTenantFromCmd(cmd)
		if tenantID == "" {
			return fmt.Errorf("tenant ID required (use --tenant or set active tenant in context)")
		}

		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := c.DeleteRoutingRules(cmd.Context(), tenantID)
		if err != nil {
			return fmt.Errorf("delete routing rules: %w", err)
		}
		return printOutput(result, output)
	},
}

// ─── Channel Rules ─────────────────────────────────────────────

var channelsCmd = &cobra.Command{
	Use:   "channels",
	Short: "Manage channel permission rules",
}

var channelsGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Get channel rules for a tenant",
	RunE: func(cmd *cobra.Command, _ []string) error {
		tenantID := resolveTenantFromCmd(cmd)
		if tenantID == "" {
			return fmt.Errorf("tenant ID required (use --tenant or set active tenant in context)")
		}

		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := c.GetChannelRules(cmd.Context(), tenantID)
		if err != nil {
			return fmt.Errorf("get channel rules: %w", err)
		}
		return printOutput(result, output)
	},
}

var channelsSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Set channel rules for a tenant",
	RunE: func(cmd *cobra.Command, _ []string) error {
		tenantID := resolveTenantFromCmd(cmd)
		if tenantID == "" {
			return fmt.Errorf("tenant ID required (use --tenant or set active tenant in context)")
		}

		var req map[string]any
		rulesFile, _ := cmd.Flags().GetString("file")
		publicPatterns, _ := cmd.Flags().GetStringSlice("public")

		if rulesFile != "" && len(publicPatterns) > 0 {
			return fmt.Errorf("--file and --public are mutually exclusive")
		}

		if rulesFile != "" {
			data, err := os.ReadFile(rulesFile) //nolint:gosec // G304: CLI reads user-specified file path from --file flag
			if err != nil {
				return fmt.Errorf("read rules file: %w", err)
			}
			if err := json.Unmarshal(data, &req); err != nil {
				return fmt.Errorf("parse rules file: %w", err)
			}
		} else if len(publicPatterns) > 0 {
			req = map[string]any{
				"public_patterns": publicPatterns,
			}
		} else {
			return fmt.Errorf("either --file or --public is required")
		}

		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := c.SetChannelRules(cmd.Context(), tenantID, req)
		if err != nil {
			return fmt.Errorf("set channel rules: %w", err)
		}
		return printOutput(result, output)
	},
}

var channelsDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete channel rules for a tenant",
	RunE: func(cmd *cobra.Command, _ []string) error {
		tenantID := resolveTenantFromCmd(cmd)
		if tenantID == "" {
			return fmt.Errorf("tenant ID required (use --tenant or set active tenant in context)")
		}

		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := c.DeleteChannelRules(cmd.Context(), tenantID)
		if err != nil {
			return fmt.Errorf("delete channel rules: %w", err)
		}
		return printOutput(result, output)
	},
}

// ─── Test Access ───────────────────────────────────────────────

var rulesTestAccessCmd = &cobra.Command{
	Use:   "test-access",
	Short: "Test channel access for a subject",
	RunE: func(cmd *cobra.Command, _ []string) error {
		tenantID := resolveTenantFromCmd(cmd)
		if tenantID == "" {
			return fmt.Errorf("tenant ID required (use --tenant or set active tenant in context)")
		}

		sub, _ := cmd.Flags().GetString("sub")
		channel, _ := cmd.Flags().GetString("channel")
		groups, _ := cmd.Flags().GetStringSlice("group")

		req := map[string]any{}
		if sub != "" {
			req["sub"] = sub
		}
		if channel != "" {
			req["channel"] = channel
		}
		if len(groups) > 0 {
			req["groups"] = groups
		}

		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := c.TestAccess(cmd.Context(), tenantID, req)
		if err != nil {
			return fmt.Errorf("test access: %w", err)
		}
		return printOutput(result, output)
	},
}

// resolveTenantFromCmd extracts tenant from --tenant flag, falling back to context.
func resolveTenantFromCmd(cmd *cobra.Command) string {
	tenantFlag, _ := cmd.Flags().GetString("tenant")
	return resolveTenant(tenantFlag)
}
