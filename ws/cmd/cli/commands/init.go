package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	clicontext "github.com/klurvio/sukko/cmd/cli/context"
	"github.com/spf13/cobra"
)

const (
	sukkoConfigDir  = ".sukko"
	sukkoConfigFile = "config.json"
)

// Default dev credentials read from environment, falling back to docker-compose defaults.
// These are NOT compiled secrets — they are well-known local dev values
// that match docker-compose.yml and are overridable via env vars.
var (
	devAdminToken = envOrDefault("SUKKO_DEV_ADMIN_TOKEN", "sukko-dev-token")
	devHMACSecret = envOrDefault("SUKKO_DEV_HMAC_SECRET", "sukko-dev-secret-minimum-32-bytes!!")
)

var useDefaults bool

func init() {
	initCmd.Flags().BoolVar(&useDefaults, "defaults", false, "Skip prompts and use defaults (SQLite + NATS + direct)")
	rootCmd.AddCommand(initCmd)
}

// ProjectConfig stores infrastructure selections from sukko init.
// Secrets (admin token, HMAC secret) are NOT stored here — they live
// exclusively in the encrypted context store (~/.config/sukko/contexts/).
type ProjectConfig struct {
	Database       string `json:"database"`
	Broadcast      string `json:"broadcast"`
	MessageBackend string `json:"message_backend"`
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a local Sukko development environment",
	Long: `Initialize a local Sukko development environment.

Creates .sukko/config.json with infrastructure selections and sets up
a local context with dev credentials. Run 'sukko up' to start services.`,
	RunE: runInit,
}

func runInit(cmd *cobra.Command, _ []string) error {
	cfg := ProjectConfig{
		Database:       "sqlite",
		Broadcast:      "nats",
		MessageBackend: "direct",
	}

	if !useDefaults {
		var err error
		cfg.Database, err = promptChoice(cmd, "Database", []string{"sqlite", "postgres"}, "sqlite")
		if err != nil {
			return err
		}
		cfg.Broadcast, err = promptChoice(cmd, "Broadcast bus", []string{"nats", "valkey"}, "nats")
		if err != nil {
			return err
		}
		cfg.MessageBackend, err = promptChoice(cmd, "Message backend", []string{"direct", "kafka", "nats"}, "direct")
		if err != nil {
			return err
		}
	}

	// Write .sukko/config.json
	configDir := filepath.Join(".", sukkoConfigDir)
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	configPath := filepath.Join(configDir, sukkoConfigFile)
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Config written to %s\n", configPath)

	// Create local context
	store, err := clicontext.NewStore()
	if err != nil {
		return fmt.Errorf("init context store: %w", err)
	}

	tokenEnc, err := store.EncryptSecret(devAdminToken)
	if err != nil {
		return fmt.Errorf("encrypt admin token: %w", err)
	}

	hmacEnc, err := store.EncryptSecret(devHMACSecret)
	if err != nil {
		return fmt.Errorf("encrypt hmac secret: %w", err)
	}

	ctx := &clicontext.Context{
		Name:            "local",
		GatewayURL:      defaultGatewayURL,
		ProvisioningURL: defaultAPIURL,
		TesterURL:       defaultTesterURL,
		AdminTokenEnc:   tokenEnc,
		HMACSecretEnc:   hmacEnc,
		ActiveTenant:    "local",
	}

	if err := store.Add(ctx); err != nil {
		return fmt.Errorf("add local context: %w", err)
	}

	if err := store.SetActive("local"); err != nil {
		return fmt.Errorf("set active context: %w", err)
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Local context created and set as active.")
	fmt.Fprintf(cmd.OutOrStdout(), "\nStack: %s + %s + %s\n", cfg.Database, cfg.Broadcast, cfg.MessageBackend)
	if os.Getenv("SUKKO_DEV_ADMIN_TOKEN") == "" {
		fmt.Fprintln(cmd.ErrOrStderr(), "Note: using default dev credentials. Set SUKKO_DEV_ADMIN_TOKEN and SUKKO_DEV_HMAC_SECRET for non-local environments.")
	}
	fmt.Fprintln(cmd.OutOrStdout(), "\nRun 'sukko up' to start the local environment.")

	return nil
}

func promptChoice(cmd *cobra.Command, label string, options []string, defaultVal string) (string, error) { //nolint:unparam // error return reserved for future validation; callers already handle it
	fmt.Fprintf(cmd.OutOrStdout(), "%s (%s) [%s]: ", label, joinOptions(options), defaultVal)

	var input string
	if _, err := fmt.Scanln(&input); err != nil || strings.TrimSpace(input) == "" {
		// Empty input or EOF (non-interactive/piped): use default. This is intentional
		// graceful degradation for non-TTY environments (Principle IV).
		return defaultVal, nil //nolint:nilerr // EOF/empty input is expected in non-TTY; returning default is intentional
	}

	if slices.Contains(options, input) {
		return input, nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Invalid choice %q, using default %q\n", input, defaultVal)
	return defaultVal, nil
}

func joinOptions(options []string) string {
	return strings.Join(options, "/")
}
