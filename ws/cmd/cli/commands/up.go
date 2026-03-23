package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/klurvio/sukko/cmd/cli/compose"
	"github.com/spf13/cobra"
)

const healthTimeout = 120 * time.Second

// Docker Compose internal service addresses — used by buildComposeConfig
// to wire services together inside the compose network.
const (
	composeDatabaseURL = "postgres://sukko:sukko@postgres:5432/sukko_provisioning?sslmode=disable"
	composeValkeyAddr  = "valkey:6379"
	composeKafkaBroker = "redpanda:9092"
	composeNATSURL     = "nats://nats:4222"
)

func init() {
	rootCmd.AddCommand(upCmd)
}

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Start the local development environment",
	Long: `Start the local development environment using Docker Compose.

Reads .sukko/config.json (created by 'sukko init'), activates the selected
infrastructure profiles, waits for all services to become healthy, and
provisions a default tenant for immediate use.`,
	RunE: runUp,
}

func runUp(cmd *cobra.Command, _ []string) error {
	// Read project config
	configPath := filepath.Join(".", sukkoConfigDir, sukkoConfigFile)
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read config (run 'sukko init' first): %w", err)
	}

	var cfg ProjectConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	// Build profiles and env overrides from selections
	profiles, envOverrides := buildComposeConfig(cfg)

	fmt.Fprintf(cmd.OutOrStdout(), "Starting Sukko (%s + %s + %s)...\n",
		cfg.Database, cfg.Broadcast, cfg.MessageBackend)

	// Start Docker Compose
	mgr, err := compose.NewManager(".")
	if err != nil {
		return fmt.Errorf("create compose manager: %w", err)
	}
	if err := mgr.Up(cmd.Context(), profiles, envOverrides); err != nil {
		return fmt.Errorf("start services: %w", err)
	}

	// Wait for health — resolve URLs from context.
	fmt.Fprintln(cmd.OutOrStdout(), "\nWaiting for services to become healthy...")

	provURL := defaultAPIURL
	gwHTTP := defaultGatewayHTTP
	serverURL := defaultServerURL
	testerURL := defaultTesterURL

	if resolvedCtx != nil {
		if resolvedCtx.ProvisioningURL != "" {
			provURL = resolvedCtx.ProvisioningURL
		}
		if resolvedCtx.GatewayURL != "" {
			gwHTTP = wsToHTTP(resolvedCtx.GatewayURL)
		}
		if resolvedCtx.TesterURL != "" {
			testerURL = resolvedCtx.TesterURL
		}
	}

	targets := []compose.HealthTarget{
		{Name: "provisioning", URL: provURL + "/health"},
		{Name: "ws-gateway", URL: gwHTTP + "/health"},
		{Name: "ws-server", URL: serverURL + "/health"},
		{Name: "sukko-tester", URL: testerURL + "/health"},
	}

	if err := compose.WaitForHealth(cmd.Context(), cmd.OutOrStdout(), targets, healthTimeout); err != nil {
		return fmt.Errorf("health check: %w", err)
	}

	// Provision default tenant
	fmt.Fprintln(cmd.OutOrStdout(), "\nProvisioning default tenant...")
	if err := provisionDefaultTenant(cmd); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: default tenant provisioning failed: %v\n", err)
		fmt.Fprintln(cmd.ErrOrStderr(), "  Services are running. Create a tenant manually with 'sukko tenant create'.")
	}

	fmt.Fprintln(cmd.OutOrStdout(), "\nSukko is ready! Try:")
	fmt.Fprintln(cmd.OutOrStdout(), "  sukko status")
	fmt.Fprintln(cmd.OutOrStdout(), "  sukko tenant list")

	return nil
}

func buildComposeConfig(cfg ProjectConfig) ([]string, map[string]string) {
	var profiles []string
	envOverrides := map[string]string{}

	switch cfg.Database {
	case "postgres":
		profiles = append(profiles, "postgres")
		envOverrides["DATABASE_DRIVER"] = "postgres"
		envOverrides["DATABASE_URL"] = composeDatabaseURL
	}

	switch cfg.Broadcast {
	case "valkey":
		profiles = append(profiles, "valkey")
		envOverrides["BROADCAST_TYPE"] = "valkey"
		envOverrides["VALKEY_ADDRS"] = composeValkeyAddr
	}

	switch cfg.MessageBackend {
	case "kafka":
		profiles = append(profiles, "kafka")
		envOverrides["MESSAGE_BACKEND"] = "kafka"
		envOverrides["KAFKA_BROKERS"] = composeKafkaBroker
	case "nats":
		envOverrides["MESSAGE_BACKEND"] = "nats"
		envOverrides["NATS_JETSTREAM_URLS"] = composeNATSURL
	}

	return profiles, envOverrides
}

func provisionDefaultTenant(cmd *cobra.Command) error {
	c, err := newClient()
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}

	ctx := cmd.Context()

	// Create default tenant (ignore conflict — may already exist)
	_, err = c.CreateTenant(ctx, map[string]any{
		"tenant_id":     "local",
		"name":          "Local Development",
		"consumer_type": "shared",
	})
	if err != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "  Tenant 'local': %v (may already exist)\n", err)
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "  Tenant 'local': created")
	}

	// Set catch-all routing rules
	_, err = c.SetRoutingRules(ctx, "local", map[string]any{
		"rules": []map[string]any{
			{"pattern": "*.*", "topic_suffix": "default"},
		},
	})
	if err != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "  Routing rules: %v\n", err)
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "  Routing rules: set (catch-all)")
	}

	// Set all-public channel rules
	_, err = c.SetChannelRules(ctx, "local", map[string]any{
		"public_patterns": []string{"*"},
	})
	if err != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "  Channel rules: %v\n", err)
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "  Channel rules: set (all public)")
	}

	return nil
}
