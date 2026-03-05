// CLI tool for Sukko provisioning management.
package main

import (
	"os"

	"github.com/klurvio/sukko/cmd/cli/commands"
)

func main() {
	if err := commands.Execute(); err != nil {
		os.Exit(1)
	}
}
