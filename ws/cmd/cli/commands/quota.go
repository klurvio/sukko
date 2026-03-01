package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(quotaCmd)
	quotaCmd.AddCommand(quotaGetCmd, quotaUpdateCmd)

	quotaGetCmd.Flags().String("tenant", "", "Tenant ID (required)")
	_ = quotaGetCmd.MarkFlagRequired("tenant")

	quotaUpdateCmd.Flags().String("tenant", "", "Tenant ID (required)")
	quotaUpdateCmd.Flags().Int("max-topics", 0, "Maximum topics")
	quotaUpdateCmd.Flags().Int("max-partitions", 0, "Maximum partitions")
	quotaUpdateCmd.Flags().Int64("max-storage-bytes", 0, "Maximum storage in bytes")
	quotaUpdateCmd.Flags().Int64("producer-byte-rate", 0, "Producer throughput limit (bytes/sec)")
	quotaUpdateCmd.Flags().Int64("consumer-byte-rate", 0, "Consumer throughput limit (bytes/sec)")
	quotaUpdateCmd.Flags().Int("max-connections", 0, "Maximum WebSocket connections")
	_ = quotaUpdateCmd.MarkFlagRequired("tenant")
}

var quotaCmd = &cobra.Command{
	Use:   "quota",
	Short: "Manage tenant quotas",
}

var quotaGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Get quotas for a tenant",
	RunE: func(cmd *cobra.Command, _ []string) error {
		tenantID, _ := cmd.Flags().GetString("tenant")

		result, err := newClient().GetQuota(tenantID)
		if err != nil {
			return fmt.Errorf("get quota: %w", err)
		}
		return printOutput(result, output)
	},
}

var quotaUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update quotas for a tenant",
	RunE: func(cmd *cobra.Command, _ []string) error {
		tenantID, _ := cmd.Flags().GetString("tenant")

		req := map[string]any{}
		if v, _ := cmd.Flags().GetInt("max-topics"); v > 0 {
			req["max_topics"] = v
		}
		if v, _ := cmd.Flags().GetInt("max-partitions"); v > 0 {
			req["max_partitions"] = v
		}
		if v, _ := cmd.Flags().GetInt64("max-storage-bytes"); v > 0 {
			req["max_storage_bytes"] = v
		}
		if v, _ := cmd.Flags().GetInt64("producer-byte-rate"); v > 0 {
			req["producer_byte_rate"] = v
		}
		if v, _ := cmd.Flags().GetInt64("consumer-byte-rate"); v > 0 {
			req["consumer_byte_rate"] = v
		}
		if v, _ := cmd.Flags().GetInt("max-connections"); v > 0 {
			req["max_connections"] = v
		}

		result, err := newClient().UpdateQuota(tenantID, req)
		if err != nil {
			return fmt.Errorf("update quota: %w", err)
		}
		return printOutput(result, output)
	},
}
