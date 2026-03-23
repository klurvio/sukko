package commands

import (
	"fmt"

	"github.com/klurvio/sukko/cmd/cli/compose"
	"github.com/spf13/cobra"
)

var logsFollow bool

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "Follow log output")
	rootCmd.AddCommand(logsCmd)
}

var logsCmd = &cobra.Command{
	Use:   "logs [service...]",
	Short: "View logs from local Docker Compose services",
	Long: `View logs from local Docker Compose services.

Only available for local Docker Compose environments. Specify service names
to filter (e.g., 'sukko logs ws-gateway ws-server').`,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := compose.NewManager(".")
		if err != nil {
			return fmt.Errorf("create compose manager: %w", err)
		}

		if !mgr.IsRunning(cmd.Context()) {
			return fmt.Errorf("logs only available for local Docker Compose environments (no compose project running)")
		}

		return mgr.Logs(cmd.Context(), args, logsFollow)
	},
}
