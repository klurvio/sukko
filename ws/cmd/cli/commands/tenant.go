package commands

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(tenantCmd)
	tenantCmd.AddCommand(tenantCreateCmd, tenantGetCmd, tenantListCmd, tenantUpdateCmd,
		tenantSuspendCmd, tenantReactivateCmd, tenantDeprovisionCmd)

	// create flags
	tenantCreateCmd.Flags().String("id", "", "Tenant ID (required)")
	tenantCreateCmd.Flags().String("name", "", "Display name")
	tenantCreateCmd.Flags().StringSlice("category", nil, "Topic categories (repeatable)")
	tenantCreateCmd.Flags().String("consumer-type", "shared", "Consumer type (shared|dedicated)")
	_ = tenantCreateCmd.MarkFlagRequired("id")

	// list flags
	tenantListCmd.Flags().Int("limit", 50, "Maximum results")
	tenantListCmd.Flags().Int("offset", 0, "Results offset")
	tenantListCmd.Flags().String("status", "", "Filter by status")

	// update flags
	tenantUpdateCmd.Flags().String("name", "", "New display name")
}

var tenantCmd = &cobra.Command{
	Use:   "tenant",
	Short: "Manage tenants",
}

var tenantCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new tenant",
	RunE: func(cmd *cobra.Command, _ []string) error {
		id, _ := cmd.Flags().GetString("id")
		name, _ := cmd.Flags().GetString("name")
		categories, _ := cmd.Flags().GetStringSlice("category")
		consumerType, _ := cmd.Flags().GetString("consumer-type")

		if name == "" {
			name = id
		}

		req := map[string]any{
			"tenant_id":     id,
			"name":          name,
			"consumer_type": consumerType,
		}
		if len(categories) > 0 {
			req["categories"] = categories
		}

		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := c.CreateTenant(req)
		if err != nil {
			return fmt.Errorf("create tenant: %w", err)
		}
		return printOutput(result, output)
	},
}

var tenantGetCmd = &cobra.Command{
	Use:   "get [tenant-id]",
	Short: "Get tenant details",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := c.GetTenant(args[0])
		if err != nil {
			return fmt.Errorf("get tenant: %w", err)
		}
		return printOutput(result, output)
	},
}

var tenantListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tenants",
	RunE: func(cmd *cobra.Command, _ []string) error {
		limit, _ := cmd.Flags().GetInt("limit")
		offset, _ := cmd.Flags().GetInt("offset")
		status, _ := cmd.Flags().GetString("status")

		params := map[string]string{
			"limit":  strconv.Itoa(limit),
			"offset": strconv.Itoa(offset),
		}
		if status != "" {
			params["status"] = status
		}

		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := c.ListTenants(params)
		if err != nil {
			return fmt.Errorf("list tenants: %w", err)
		}
		return printOutput(result, output)
	},
}

var tenantUpdateCmd = &cobra.Command{
	Use:   "update [tenant-id]",
	Short: "Update a tenant",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		req := map[string]any{}
		if name, _ := cmd.Flags().GetString("name"); name != "" {
			req["name"] = name
		}

		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := c.UpdateTenant(args[0], req)
		if err != nil {
			return fmt.Errorf("update tenant: %w", err)
		}
		return printOutput(result, output)
	},
}

var tenantSuspendCmd = &cobra.Command{
	Use:   "suspend [tenant-id]",
	Short: "Suspend a tenant",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := c.SuspendTenant(args[0])
		if err != nil {
			return fmt.Errorf("suspend tenant: %w", err)
		}
		return printOutput(result, output)
	},
}

var tenantReactivateCmd = &cobra.Command{
	Use:   "reactivate [tenant-id]",
	Short: "Reactivate a suspended tenant",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := c.ReactivateTenant(args[0])
		if err != nil {
			return fmt.Errorf("reactivate tenant: %w", err)
		}
		return printOutput(result, output)
	},
}

var tenantDeprovisionCmd = &cobra.Command{
	Use:   "deprovision [tenant-id]",
	Short: "Initiate tenant deletion",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := c.DeprovisionTenant(args[0])
		if err != nil {
			return fmt.Errorf("deprovision tenant: %w", err)
		}
		return printOutput(result, output)
	},
}
