package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/Toniq-Labs/odin-ws/internal/provisioning/configstore"
)

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configInitCmd, configValidateCmd, configExportCmd)

	// init
	configInitCmd.Flags().String("tenant", "my-tenant", "Tenant ID for the template")
	configInitCmd.Flags().String("file", "tenants.yaml", "Output file path")

	// validate
	configValidateCmd.Flags().String("file", "", "Path to YAML config file (required)")
	_ = configValidateCmd.MarkFlagRequired("file")

	// export
	configExportCmd.Flags().String("file", "", "Output YAML file path (required)")
	_ = configExportCmd.MarkFlagRequired("file")
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage YAML config files for config mode",
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Generate a well-commented YAML config template",
	RunE: func(cmd *cobra.Command, _ []string) error {
		tenantID, _ := cmd.Flags().GetString("tenant")
		filePath, _ := cmd.Flags().GetString("file")

		template := fmt.Sprintf(`# Odin WS Provisioning Configuration
# This file defines tenants, keys, topics, and access rules.
# Used when PROVISIONING_MODE=config.
# Reload at runtime by sending SIGHUP to the provisioning process.

tenants:
  - id: %s
    name: "%s"
    # consumer_type: shared (default) or dedicated
    consumer_type: shared

    # Topic categories — at least one required per tenant.
    categories:
      - name: trade
        # partitions: 6        # optional, uses server default
        # retention_ms: 86400000  # optional, 24h in ms

    # Public keys for JWT validation (optional).
    # keys:
    #   - id: key-1
    #     algorithm: ES256     # ES256, RS256, or EdDSA
    #     public_key: |
    #       -----BEGIN PUBLIC KEY-----
    #       ...
    #       -----END PUBLIC KEY-----
    #     # active: true        # optional, defaults to true
    #     # expires_at_unix: 0  # optional, 0 = no expiry

    # Resource quotas (optional).
    # quotas:
    #   max_topics: 100
    #   max_partitions: 600
    #   max_storage_bytes: 10737418240  # 10 GiB
    #   producer_byte_rate: 5242880     # 5 MiB/s
    #   consumer_byte_rate: 10485760    # 10 MiB/s
    #   max_connections: 1000

    # OIDC configuration (optional).
    # oidc:
    #   issuer_url: "https://auth.example.com"
    #   jwks_url: "https://auth.example.com/.well-known/jwks.json"  # optional
    #   audience: "my-api"     # optional
    #   enabled: true          # optional, defaults to true

    # Channel access rules (optional).
    # channel_rules:
    #   public_channels:
    #     - "market.*"
    #   group_mappings:
    #     admin:
    #       - "*"
    #     viewer:
    #       - "market.*"
    #       - "trade.public"
    #   default_channels:
    #     - "market.summary"
`, tenantID, tenantID)

		if err := os.WriteFile(filePath, []byte(template), 0644); err != nil {
			return fmt.Errorf("write config template: %w", err)
		}
		fmt.Printf("Config template written to %s\n", filePath)
		return nil
	},
}

var configValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate a YAML config file",
	RunE: func(cmd *cobra.Command, _ []string) error {
		filePath, _ := cmd.Flags().GetString("file")

		cfg, err := configstore.ParseFile(filePath)
		if err != nil {
			return fmt.Errorf("parse config file: %w", err)
		}

		if err := configstore.Validate(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Validation errors:\n%s\n", err)
			return fmt.Errorf("validation failed")
		}

		fmt.Printf("Config file %s is valid (%d tenants)\n", filePath, len(cfg.Tenants))
		return nil
	},
}

var configExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export current API state to a YAML config file",
	RunE: func(cmd *cobra.Command, _ []string) error {
		filePath, _ := cmd.Flags().GetString("file")
		c := newClient()

		tenantsResp, err := c.ListTenants(map[string]string{"limit": "1000"})
		if err != nil {
			return fmt.Errorf("list tenants: %w", err)
		}

		tenantList, _ := tenantsResp["tenants"].([]any)

		var cfgFile configstore.ConfigFile
		for _, t := range tenantList {
			tm := asMap(t)
			tenantID := asStr(tm, "id")
			if tenantID == "" {
				continue
			}

			tc := configstore.TenantConfig{
				ID:           tenantID,
				Name:         asStr(tm, "name"),
				ConsumerType: asStr(tm, "consumer_type"),
			}

			// Fetch categories
			if catResp, err := c.ListCategories(tenantID); err == nil {
				if topics, ok := catResp["topics"].([]any); ok {
					for _, topic := range topics {
						ts := fmt.Sprintf("%v", topic)
						tc.Categories = append(tc.Categories, configstore.CategoryConfig{Name: ts})
					}
				}
			}

			// Fetch keys
			if keyResp, err := c.ListKeys(tenantID); err == nil {
				if keys, ok := keyResp["keys"].([]any); ok {
					for _, k := range keys {
						km := asMap(k)
						kc := configstore.KeyConfig{
							ID:        asStr(km, "key_id"),
							Algorithm: asStr(km, "algorithm"),
							PublicKey: asStr(km, "public_key_pem"),
						}
						tc.Keys = append(tc.Keys, kc)
					}
				}
			}

			// Fetch quota
			if quotaResp, err := c.GetQuota(tenantID); err == nil {
				qc := configstore.QuotaConfig{
					MaxTopics:        asInt(quotaResp, "max_topics"),
					MaxPartitions:    asInt(quotaResp, "max_partitions"),
					MaxStorageBytes:  asInt64(quotaResp, "max_storage_bytes"),
					ProducerByteRate: asInt64(quotaResp, "producer_byte_rate"),
					ConsumerByteRate: asInt64(quotaResp, "consumer_byte_rate"),
					MaxConnections:   asInt(quotaResp, "max_connections"),
				}
				tc.Quotas = &qc
			}

			// Fetch OIDC
			if oidcResp, err := c.GetOIDCConfig(tenantID); err == nil {
				oc := configstore.OIDCConfig{
					IssuerURL: asStr(oidcResp, "issuer_url"),
					JWKSURL:   asStr(oidcResp, "jwks_url"),
					Audience:  asStr(oidcResp, "audience"),
				}
				tc.OIDC = &oc
			}

			// Fetch channel rules
			if rulesResp, err := c.GetChannelRules(tenantID); err == nil {
				rc := configstore.ChannelRulesConfig{}
				if pubs, ok := rulesResp["public_channels"].([]any); ok {
					for _, p := range pubs {
						rc.PublicChannels = append(rc.PublicChannels, fmt.Sprintf("%v", p))
					}
				}
				if defs, ok := rulesResp["default_channels"].([]any); ok {
					for _, d := range defs {
						rc.DefaultChannels = append(rc.DefaultChannels, fmt.Sprintf("%v", d))
					}
				}
				if gm, ok := rulesResp["group_mappings"].(map[string]any); ok {
					rc.GroupMappings = make(map[string][]string)
					for group, chans := range gm {
						if chanList, ok := chans.([]any); ok {
							for _, ch := range chanList {
								rc.GroupMappings[group] = append(rc.GroupMappings[group], fmt.Sprintf("%v", ch))
							}
						}
					}
				}
				tc.ChannelRules = &rc
			}

			cfgFile.Tenants = append(cfgFile.Tenants, tc)
		}

		data, err := yaml.Marshal(&cfgFile)
		if err != nil {
			return fmt.Errorf("marshal config: %w", err)
		}

		if err := os.WriteFile(filePath, data, 0644); err != nil {
			return fmt.Errorf("write config file: %w", err)
		}

		fmt.Printf("Exported %d tenants to %s\n", len(cfgFile.Tenants), filePath)
		return nil
	},
}

func asInt(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	}
	return 0
}

func asInt64(m map[string]any, key string) int64 {
	switch v := m[key].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	}
	return 0
}
