package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

// TestRun pins FR-008/FR-017: genkeys emits a P-256 keypair — public key as PKIX PEM,
// private key as PKCS#8 PEM — to sukko.dev.pub/sukko.dev.key, and NEVER writes sukko.pub
// (which would clobber the committed embedded key during an e2e run).
func TestRun(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pubPath, privPath, err := run(dir)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if got := filepath.Base(pubPath); got != "sukko.dev.pub" {
		t.Errorf("public key path = %s, want sukko.dev.pub", got)
	}
	if got := filepath.Base(privPath); got != "sukko.dev.key" {
		t.Errorf("private key path = %s, want sukko.dev.key", got)
	}

	// It must NOT have written the committed embedded key path.
	if _, err := os.Stat(filepath.Join(dir, "sukko.pub")); !os.IsNotExist(err) {
		t.Fatalf("genkeys wrote sukko.pub (must never touch the committed embedded key); stat err=%v", err)
	}

	// Public key: PKIX PEM, ECDSA P-256.
	pub := decodePEM(t, pubPath, "PUBLIC KEY")
	parsedPub, err := x509.ParsePKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("parse public key: %v", err)
	}
	ecPub, ok := parsedPub.(*ecdsa.PublicKey)
	if !ok || ecPub.Curve != elliptic.P256() {
		t.Fatalf("public key is not ECDSA P-256: %T", parsedPub)
	}

	// Private key: PKCS#8 PEM, ECDSA P-256.
	priv := decodePEM(t, privPath, "PRIVATE KEY")
	parsedPriv, err := x509.ParsePKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("parse private key: %v", err)
	}
	ecPriv, ok := parsedPriv.(*ecdsa.PrivateKey)
	if !ok || ecPriv.Curve != elliptic.P256() {
		t.Fatalf("private key is not ECDSA P-256: %T", parsedPriv)
	}
}

func decodePEM(t *testing.T, path, wantType string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		t.Fatalf("%s: no PEM block", path)
	}
	if block.Type != wantType {
		t.Fatalf("%s: PEM type = %q, want %q", path, block.Type, wantType)
	}
	return block.Bytes
}
