package commands

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(rulesCmd)
	rulesCmd.AddCommand(rulesGetCmd, rulesSetCmd, rulesDeleteCmd, rulesTestCmd)

	// get
	rulesGetCmd.Flags().String("tenant", "", "Tenant ID (required)")
	_ = rulesGetCmd.MarkFlagRequired("tenant")

	// set
	rulesSetCmd.Flags().String("tenant", "", "Tenant ID (required)")
	rulesSetCmd.Flags().String("rules-file", "", "Path to JSON rules file (required)")
	_ = rulesSetCmd.MarkFlagRequired("tenant")
	_ = rulesSetCmd.MarkFlagRequired("rules-file")

	// delete
	rulesDeleteCmd.Flags().String("tenant", "", "Tenant ID (required)")
	_ = rulesDeleteCmd.MarkFlagRequired("tenant")

	// test
	rulesTestCmd.Flags().String("tenant", "", "Tenant ID (required)")
	rulesTestCmd.Flags().StringSlice("group", nil, "Groups to test access for")
	_ = rulesTestCmd.MarkFlagRequired("tenant")
}

var rulesCmd = &cobra.Command{
	Use:   "rules",
	Short: "Manage channel rules",
}

var rulesGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Get channel rules for a tenant",
	RunE: func(cmd *cobra.Command, _ []string) error {
		tenantID, _ := cmd.Flags().GetString("tenant")

		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := c.GetChannelRules(tenantID)
		if err != nil {
			return fmt.Errorf("get channel rules: %w", err)
		}
		return printOutput(result, output)
	},
}

var rulesSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Set channel rules for a tenant from a JSON file",
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
		result, err := c.SetChannelRules(tenantID, req)
		if err != nil {
			return fmt.Errorf("set channel rules: %w", err)
		}
		return printOutput(result, output)
	},
}

var rulesDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete channel rules for a tenant",
	RunE: func(cmd *cobra.Command, _ []string) error {
		tenantID, _ := cmd.Flags().GetString("tenant")

		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := c.DeleteChannelRules(tenantID)
		if err != nil {
			return fmt.Errorf("delete channel rules: %w", err)
		}
		return printOutput(result, output)
	},
}

var rulesTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Test channel access for given groups",
	RunE: func(cmd *cobra.Command, _ []string) error {
		tenantID, _ := cmd.Flags().GetString("tenant")
		groups, _ := cmd.Flags().GetStringSlice("group")

		req := map[string]any{
			"groups": groups,
		}

		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := c.TestAccess(tenantID, req)
		if err != nil {
			return fmt.Errorf("test access: %w", err)
		}
		return printOutput(result, output)
	},
}
