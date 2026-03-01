// CLI tool for Odin WS provisioning management.
package main

import (
	"os"

	"github.com/Toniq-Labs/odin-ws/cmd/cli/commands"
)

func main() {
	if err := commands.Execute(); err != nil {
		os.Exit(1)
	}
}
