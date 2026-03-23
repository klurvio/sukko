// Package commands implements the sukko CLI command tree.
package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/klurvio/sukko/cmd/cli/client"
	clicontext "github.com/klurvio/sukko/cmd/cli/context"
)

// CLI default values. Env vars take precedence via envOrDefault; CLI flags take precedence over env.
var (
	defaultAPIURL      = envOrDefault("SUKKO_API_URL", "http://localhost:8080")
	defaultGatewayURL  = envOrDefault("SUKKO_GATEWAY_URL", "ws://localhost:3000")
	defaultGatewayHTTP = envOrDefault("SUKKO_GATEWAY_HTTP_URL", "http://localhost:3000")
	defaultServerURL   = envOrDefault("SUKKO_SERVER_URL", "http://localhost:3005")
	defaultTesterURL   = envOrDefault("SUKKO_TESTER_URL", "http://localhost:8090")
)

const defaultOutput = "table"

var (
	apiURL      string
	token       string
	output      string
	contextName string
	verbose     bool

	// resolvedCtx is the active context loaded by PersistentPreRunE.
	resolvedCtx   *clicontext.Context
	resolvedStore *clicontext.Store
)

var rootCmd = &cobra.Command{
	Use:   "sukko",
	Short: "Sukko WebSocket platform CLI",
	Long:  "CLI tool for managing tenants, keys, rules, and operating the Sukko WebSocket platform.",
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		// Skip context loading for context management commands and completion
		if isContextSubcommand(cmd) {
			return nil
		}
		if err := loadContext(cmd); err != nil {
			return err
		}
		if output != "json" && output != "table" {
			return fmt.Errorf("invalid output format %q: must be json or table", output)
		}
		return nil
	},
	SilenceUsage: true,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&apiURL, "api-url", "", "Provisioning API base URL (overrides context)")
	rootCmd.PersistentFlags().StringVar(&token, "token", os.Getenv("SUKKO_TOKEN"), "Admin authentication token (overrides context)")
	rootCmd.PersistentFlags().StringVarP(&output, "output", "o", defaultOutput, "Output format (json|table)")
	rootCmd.PersistentFlags().StringVar(&contextName, "context", os.Getenv("SUKKO_CONTEXT"), "Context name (overrides active context)")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "Enable verbose output")
}

// Execute runs the root command.
func Execute() error {
	if err := rootCmd.Execute(); err != nil {
		return fmt.Errorf("execute CLI: %w", err)
	}
	return nil
}

// newClient creates an AdminClient resolving config from: flags > context > env > defaults.
func newClient() (*client.AdminClient, error) {
	url, tok := resolveClientConfig()

	c, err := client.New(client.Config{
		BaseURL: url,
		Token:   tok,
		Timeout: client.DefaultClientTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("create admin client: %w", err)
	}
	return c, nil
}

// resolveClientConfig returns the API URL and token, preferring flags over context.
func resolveClientConfig() (string, string) {
	url := apiURL
	tok := token

	// If no flags given, try context
	if resolvedCtx != nil && resolvedStore != nil {
		if url == "" {
			url = resolvedCtx.ProvisioningURL
		}
		if tok == "" {
			// Ignore decrypt error: server will return 401, giving the user a clear unauthorized error.
			if t, err := resolvedCtx.AdminToken(resolvedStore.Key()); err == nil && t != "" {
				tok = t
			}
		}
	}

	// Final fallback
	if url == "" {
		url = defaultAPIURL
	}

	return url, tok
}

// resolveTenant returns the tenant ID from: --tenant flag > context > empty.
func resolveTenant(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if resolvedCtx != nil {
		return resolvedCtx.ActiveTenant
	}
	return ""
}

// resolveGatewayURL returns the gateway URL from: flag > context > default.
func resolveGatewayURL(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if resolvedCtx != nil && resolvedCtx.GatewayURL != "" {
		return resolvedCtx.GatewayURL
	}
	return defaultGatewayURL
}

// wsToHTTP converts a WebSocket URL scheme to HTTP.
func wsToHTTP(wsURL string) string {
	switch {
	case strings.HasPrefix(wsURL, "wss://"):
		return "https://" + strings.TrimPrefix(wsURL, "wss://")
	case strings.HasPrefix(wsURL, "ws://"):
		return "http://" + strings.TrimPrefix(wsURL, "ws://")
	default:
		return wsURL
	}
}

// resolveTesterURL returns the tester URL from: flag > context > default.
func resolveTesterURL(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if resolvedCtx != nil && resolvedCtx.TesterURL != "" {
		return resolvedCtx.TesterURL
	}
	return defaultTesterURL
}

// loadContext attempts to load the active context. Non-fatal if no context is configured.
func loadContext(cmd *cobra.Command) error {
	store, err := clicontext.NewStore()
	if err != nil {
		if verbose {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not init context store: %v\n", err)
		}
		return nil // non-fatal
	}
	resolvedStore = store

	name := contextName
	if name == "" {
		name, _ = store.ActiveName() // ignore error — no active context is fine
	}
	if name == "" {
		return nil
	}

	ctx, err := store.Get(name)
	if err != nil {
		if verbose {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not load context %q: %v\n", name, err)
		}
		return nil // non-fatal
	}
	resolvedCtx = ctx

	return nil
}

// isContextSubcommand returns true for commands that should skip context loading.
func isContextSubcommand(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Name() == "context" || c.Name() == "completion" || c.Name() == "init" {
			return true
		}
	}
	return false
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
