package commands

import (
	"fmt"
	"text/tabwriter"

	clicontext "github.com/klurvio/sukko/cmd/cli/context"
	"github.com/spf13/cobra"
)

var contextCmd = &cobra.Command{
	Use:   "context",
	Short: "Manage CLI contexts (environments)",
	Long:  "Add, remove, switch, and list named contexts for different Sukko environments.",
}

var contextAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Add a new context",
	Args:  cobra.ExactArgs(1),
	RunE:  runContextAdd,
}

var contextUseCmd = &cobra.Command{
	Use:   "use <name>",
	Short: "Set the active context",
	Args:  cobra.ExactArgs(1),
	RunE:  runContextUse,
}

var contextListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all contexts",
	RunE:    runContextList,
}

var contextRemoveCmd = &cobra.Command{
	Use:     "remove <name>",
	Aliases: []string{"rm"},
	Short:   "Remove a context",
	Args:    cobra.ExactArgs(1),
	RunE:    runContextRemove,
}

var contextCurrentCmd = &cobra.Command{
	Use:   "current",
	Short: "Show the active context",
	RunE:  runContextCurrent,
}

// Flags for context add
var (
	ctxGatewayURL      string
	ctxProvisioningURL string
	ctxTesterURL       string
	ctxAdminToken      string
	ctxHMACSecret      string
	ctxAPIKey          string
	ctxTenant          string
)

func init() {
	contextAddCmd.Flags().StringVar(&ctxGatewayURL, "gateway-url", "", "WebSocket gateway URL (e.g., ws://localhost:3000)")
	contextAddCmd.Flags().StringVar(&ctxProvisioningURL, "provisioning-url", "", "Provisioning API URL (e.g., http://localhost:8080)")
	contextAddCmd.Flags().StringVar(&ctxTesterURL, "tester-url", "", "Tester service URL (e.g., http://localhost:8090)")
	contextAddCmd.Flags().StringVar(&ctxAdminToken, "admin-token", "", "Admin authentication token")
	contextAddCmd.Flags().StringVar(&ctxHMACSecret, "hmac-secret", "", "HMAC secret for JWT signing")
	contextAddCmd.Flags().StringVar(&ctxAPIKey, "api-key", "", "API key for gateway access")
	contextAddCmd.Flags().StringVar(&ctxTenant, "tenant", "", "Default active tenant ID")

	contextCmd.AddCommand(contextAddCmd)
	contextCmd.AddCommand(contextUseCmd)
	contextCmd.AddCommand(contextListCmd)
	contextCmd.AddCommand(contextRemoveCmd)
	contextCmd.AddCommand(contextCurrentCmd)
	rootCmd.AddCommand(contextCmd)
}

func runContextAdd(cmd *cobra.Command, args []string) error {
	name := args[0]

	store, err := clicontext.NewStore()
	if err != nil {
		return fmt.Errorf("init context store: %w", err)
	}

	ctx := &clicontext.Context{
		Name:            name,
		GatewayURL:      ctxGatewayURL,
		ProvisioningURL: ctxProvisioningURL,
		TesterURL:       ctxTesterURL,
		ActiveTenant:    ctxTenant,
	}

	if ctxAdminToken != "" {
		enc, err := store.EncryptSecret(ctxAdminToken)
		if err != nil {
			return fmt.Errorf("encrypt admin token: %w", err)
		}
		ctx.AdminTokenEnc = enc
	}

	if ctxHMACSecret != "" {
		enc, err := store.EncryptSecret(ctxHMACSecret)
		if err != nil {
			return fmt.Errorf("encrypt hmac secret: %w", err)
		}
		ctx.HMACSecretEnc = enc
	}

	if ctxAPIKey != "" {
		enc, err := store.EncryptSecret(ctxAPIKey)
		if err != nil {
			return fmt.Errorf("encrypt api key: %w", err)
		}
		ctx.APIKeyEnc = enc
	}

	if err := store.Add(ctx); err != nil {
		return fmt.Errorf("add context: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Context %q added.\n", name)
	return nil
}

func runContextUse(cmd *cobra.Command, args []string) error {
	name := args[0]

	store, err := clicontext.NewStore()
	if err != nil {
		return fmt.Errorf("init context store: %w", err)
	}

	if err := store.SetActive(name); err != nil {
		return fmt.Errorf("set active context: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Switched to context %q.\n", name)
	return nil
}

func runContextList(cmd *cobra.Command, _ []string) error {
	store, err := clicontext.NewStore()
	if err != nil {
		return fmt.Errorf("init context store: %w", err)
	}

	contexts, err := store.List()
	if err != nil {
		return fmt.Errorf("list contexts: %w", err)
	}

	activeName, _ := store.ActiveName() // no active context simply means no marker displayed

	if output == "json" {
		return printJSON(contexts)
	}

	if len(contexts) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No contexts configured. Use 'sukko context add' to create one.")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ACTIVE\tNAME\tGATEWAY\tPROVISIONING\tTENANT")
	for _, ctx := range contexts {
		marker := ""
		if ctx.Name == activeName {
			marker = "*"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", marker, ctx.Name, ctx.GatewayURL, ctx.ProvisioningURL, ctx.ActiveTenant)
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("flush context list: %w", err)
	}
	return nil
}

func runContextRemove(cmd *cobra.Command, args []string) error {
	name := args[0]

	store, err := clicontext.NewStore()
	if err != nil {
		return fmt.Errorf("init context store: %w", err)
	}

	if err := store.Remove(name); err != nil {
		return fmt.Errorf("remove context: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Context %q removed.\n", name)
	return nil
}

func runContextCurrent(cmd *cobra.Command, _ []string) error {
	store, err := clicontext.NewStore()
	if err != nil {
		return fmt.Errorf("init context store: %w", err)
	}

	ctx, err := store.Active()
	if err != nil {
		return fmt.Errorf("get active context: %w", err)
	}

	if output == "json" {
		return printJSON(ctx)
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "Name:\t%s\n", ctx.Name)
	fmt.Fprintf(w, "Gateway:\t%s\n", ctx.GatewayURL)
	fmt.Fprintf(w, "Provisioning:\t%s\n", ctx.ProvisioningURL)
	if ctx.TesterURL != "" {
		fmt.Fprintf(w, "Tester:\t%s\n", ctx.TesterURL)
	}
	fmt.Fprintf(w, "Tenant:\t%s\n", ctx.ActiveTenant)
	fmt.Fprintf(w, "Admin Token:\t%s\n", secretIndicator(ctx.AdminTokenEnc))
	fmt.Fprintf(w, "HMAC Secret:\t%s\n", secretIndicator(ctx.HMACSecretEnc))
	fmt.Fprintf(w, "API Key:\t%s\n", secretIndicator(ctx.APIKeyEnc))
	if err := w.Flush(); err != nil {
		return fmt.Errorf("flush context details: %w", err)
	}
	return nil
}

func secretIndicator(enc string) string {
	if enc != "" {
		return "(set)"
	}
	return "(not set)"
}
