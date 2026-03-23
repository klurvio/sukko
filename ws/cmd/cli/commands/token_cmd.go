package commands

import (
	"fmt"
	"time"

	clitoken "github.com/klurvio/sukko/cmd/cli/token"
	"github.com/spf13/cobra"
)

var (
	tokenSub       string
	tokenTenant    string
	tokenRoles     []string
	tokenGroups    []string
	tokenScopes    []string
	tokenTTL       time.Duration
	tokenSecret    string
	tokenKeyFile   string
	tokenAlgorithm string
)

func init() {
	tokenCmd.AddCommand(tokenGenerateCmd, tokenValidateCmd)

	tokenGenerateCmd.Flags().StringVar(&tokenSub, "sub", "", "Subject (user ID)")
	tokenGenerateCmd.Flags().StringVar(&tokenTenant, "tenant", "", "Tenant ID")
	tokenGenerateCmd.Flags().StringSliceVar(&tokenRoles, "roles", nil, "Roles (repeatable)")
	tokenGenerateCmd.Flags().StringSliceVar(&tokenGroups, "groups", nil, "Groups (repeatable)")
	tokenGenerateCmd.Flags().StringSliceVar(&tokenScopes, "scopes", nil, "Scopes (repeatable)")
	tokenGenerateCmd.Flags().DurationVar(&tokenTTL, "ttl", time.Hour, "Token time-to-live (e.g., 1h, 30m, 24h)")
	tokenGenerateCmd.Flags().StringVar(&tokenSecret, "secret", "", "HMAC secret (overrides context)")
	tokenGenerateCmd.Flags().StringVar(&tokenKeyFile, "key-file", "", "Path to PEM private key (for asymmetric algorithms)")
	tokenGenerateCmd.Flags().StringVar(&tokenAlgorithm, "algorithm", "HS256", "Signing algorithm (HS256, ES256, RS256, EdDSA)")

	rootCmd.AddCommand(tokenCmd)
}

var tokenCmd = &cobra.Command{
	Use:   "token",
	Short: "JWT token helpers",
}

var tokenGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate a signed JWT token",
	RunE: func(cmd *cobra.Command, _ []string) error {
		secret := tokenSecret
		if secret == "" && resolvedCtx != nil && resolvedStore != nil {
			if s, err := resolvedCtx.HMACSecret(resolvedStore.Key()); err == nil && s != "" {
				secret = s
			}
		}

		validAlgorithms := map[string]bool{
			"HS256": true, "ES256": true, "RS256": true, "EdDSA": true,
		}
		if !validAlgorithms[tokenAlgorithm] {
			return fmt.Errorf("invalid algorithm %q: must be one of HS256, ES256, RS256, EdDSA", tokenAlgorithm)
		}

		tenant := resolveTenant(tokenTenant)

		cfg := clitoken.GenerateConfig{
			Subject:   tokenSub,
			TenantID:  tenant,
			Roles:     tokenRoles,
			Groups:    tokenGroups,
			Scopes:    tokenScopes,
			TTL:       tokenTTL,
			Secret:    secret,
			KeyFile:   tokenKeyFile,
			Algorithm: tokenAlgorithm,
		}

		tokenStr, err := clitoken.Generate(cfg)
		if err != nil {
			return fmt.Errorf("generate token: %w", err)
		}

		fmt.Fprintln(cmd.OutOrStdout(), tokenStr)
		return nil
	},
}

var tokenValidateCmd = &cobra.Command{
	Use:   "validate <token>",
	Short: "Decode and validate a JWT token",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		tokenStr := args[0]

		// Try with secret if available
		secret := tokenSecret
		if secret == "" && resolvedCtx != nil && resolvedStore != nil {
			if s, err := resolvedCtx.HMACSecret(resolvedStore.Key()); err == nil && s != "" {
				secret = s
			}
		}

		var result *clitoken.DecodedToken
		var err error
		if secret != "" {
			result, err = clitoken.ValidateWithSecret(tokenStr, secret)
		} else {
			result, err = clitoken.Decode(tokenStr)
		}
		if err != nil {
			return fmt.Errorf("validate token: %w", err)
		}

		if output == "json" {
			return printJSON(result)
		}

		// Human-readable output
		fmt.Fprintln(cmd.OutOrStdout(), "Header:")
		for k, v := range result.Header {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s: %v\n", k, v)
		}

		fmt.Fprintln(cmd.OutOrStdout(), "\nClaims:")
		for k, v := range result.Claims {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s: %v\n", k, v)
		}

		if result.Valid {
			fmt.Fprintf(cmd.OutOrStdout(), "\nStatus: %svalid%s", colorGreen, colorReset)
			if secret != "" {
				fmt.Fprint(cmd.OutOrStdout(), " (signature verified)")
			} else {
				fmt.Fprint(cmd.OutOrStdout(), " (signature not verified — no secret provided)")
			}
			fmt.Fprintln(cmd.OutOrStdout())
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "\nStatus: %sinvalid%s (%s)\n", colorRed, colorReset, result.Error)
		}

		return nil
	},
}
