package commands

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(routingRulesCmd)
	routingRulesCmd.AddCommand(routingRulesGetCmd, routingRulesSetCmd, routingRulesDeleteCmd)

	routingRulesGetCmd.Flags().String("tenant", "", "Tenant ID (required)")
	_ = routingRulesGetCmd.MarkFlagRequired("tenant")

	routingRulesSetCmd.Flags().String("tenant", "", "Tenant ID (required)")
	routingRulesSetCmd.Flags().String("rules-file", "", "Path to JSON routing rules file (required)")
	_ = routingRulesSetCmd.MarkFlagRequired("tenant")
	_ = routingRulesSetCmd.MarkFlagRequired("rules-file")

	routingRulesDeleteCmd.Flags().String("tenant", "", "Tenant ID (required)")
	_ = routingRulesDeleteCmd.MarkFlagRequired("tenant")
}

var routingRulesCmd = &cobra.Command{
	Use:   "routing-rules",
	Short: "Manage topic routing rules",
}

var routingRulesGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Get routing rules for a tenant",
	RunE: func(cmd *cobra.Command, _ []string) error {
		tenantID, _ := cmd.Flags().GetString("tenant")

		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := c.GetRoutingRules(tenantID)
		if err != nil {
			return fmt.Errorf("get routing rules: %w", err)
		}
		return printOutput(result, output)
	},
}

var routingRulesSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Set routing rules for a tenant from a JSON file",
	RunE: func(cmd *cobra.Command, _ []string) error {
		tenantID, _ := cmd.Flags().GetString("tenant")
		rulesFile, _ := cmd.Flags().GetString("rules-file")

		data, err := os.ReadFile(rulesFile) //nolint:gosec // G304: CLI reads user-specified file path from --rules-file flag
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
		result, err := c.SetRoutingRules(tenantID, req)
		if err != nil {
			return fmt.Errorf("set routing rules: %w", err)
		}
		return printOutput(result, output)
	},
}

var routingRulesDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete routing rules for a tenant",
	RunE: func(cmd *cobra.Command, _ []string) error {
		tenantID, _ := cmd.Flags().GetString("tenant")

		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := c.DeleteRoutingRules(tenantID)
		if err != nil {
			return fmt.Errorf("delete routing rules: %w", err)
		}
		return printOutput(result, output)
	},
}
