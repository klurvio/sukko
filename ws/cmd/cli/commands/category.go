package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(categoryCmd)
	categoryCmd.AddCommand(categoryCreateCmd, categoryListCmd)

	categoryCreateCmd.Flags().String("tenant", "", "Tenant ID (required)")
	categoryCreateCmd.Flags().StringSlice("name", nil, "Category names (required, repeatable)")
	categoryCreateCmd.Flags().Int("partitions", 0, "Number of partitions (optional)")
	categoryCreateCmd.Flags().Int64("retention-ms", 0, "Retention in milliseconds (optional)")
	_ = categoryCreateCmd.MarkFlagRequired("tenant")
	_ = categoryCreateCmd.MarkFlagRequired("name")

	categoryListCmd.Flags().String("tenant", "", "Tenant ID (required)")
	_ = categoryListCmd.MarkFlagRequired("tenant")
}

var categoryCmd = &cobra.Command{
	Use:   "category",
	Short: "Manage topic categories",
}

var categoryCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create topic categories",
	RunE: func(cmd *cobra.Command, _ []string) error {
		tenantID, _ := cmd.Flags().GetString("tenant")
		names, _ := cmd.Flags().GetStringSlice("name")

		req := map[string]any{
			"categories": names,
		}

		result, err := newClient().CreateCategory(tenantID, req)
		if err != nil {
			return fmt.Errorf("create categories: %w", err)
		}
		return printOutput(result, output)
	},
}

var categoryListCmd = &cobra.Command{
	Use:   "list",
	Short: "List categories for a tenant",
	RunE: func(cmd *cobra.Command, _ []string) error {
		tenantID, _ := cmd.Flags().GetString("tenant")

		result, err := newClient().ListCategories(tenantID)
		if err != nil {
			return fmt.Errorf("list categories: %w", err)
		}
		return printOutput(result, output)
	},
}
