// Command genkeys generates an Ed25519 key pair for Sukko license signing.
// The public key is embedded in the Sukko binary; the private key is used
// only by the License Service.
//
// This is a standalone CLI tool, not a production service. It uses fmt for
// output instead of zerolog (no service context).
//
// Usage: go run ./internal/shared/license/genkeys
//
//nolint:forbidigo // standalone CLI tool — fmt output is intentional, not a logging violation
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"os"
)

func main() {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate key pair: %v\n", err)
		os.Exit(1)
	}

	pubPath := "internal/shared/license/keys/sukko.pub"
	privPath := "internal/shared/license/keys/sukko.dev.key"

	if err := os.WriteFile(pubPath, pub, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "write public key: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(privPath, priv, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "write private key: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Public key:  %s (%d bytes)\n", pubPath, len(pub))
	fmt.Printf("Private key: %s (%d bytes)\n", privPath, len(priv))
	fmt.Println("IMPORTANT: The private key (*.key) is gitignored. Keep it safe.")
}
