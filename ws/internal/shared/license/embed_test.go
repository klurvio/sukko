package license

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"testing"
)

// mustPKIXPEM marshals a public key to a PKIX ("PUBLIC KEY") PEM block for test fixtures.
func mustPKIXPEM(t *testing.T, pub any) []byte {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("marshal PKIX: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

// TestParsePublicKeyPEM_Rejects pins FR-018: the embedded-key parser must reject a
// malformed PEM, a wrong block type, a non-ECDSA key, and a wrong-curve key — each
// with a clear error — so a bad embedded key fails loudly at load, never a green boot
// that then rejects every license.
func TestParsePublicKeyPEM_Rejects(t *testing.T) {
	t.Parallel()

	// Valid P-256 fixture — must parse.
	p256, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	// P-384 (wrong curve) fixture.
	p384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	// Ed25519 (non-ECDSA) fixture.
	edPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		pem     []byte
		wantErr bool
	}{
		{"valid P-256", mustPKIXPEM(t, &p256.PublicKey), false},
		{"malformed - not PEM", []byte("this is not a pem block"), true},
		{"empty", nil, true},
		{"wrong block type", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte{0x01, 0x02}}), true},
		{"non-ECDSA (Ed25519)", mustPKIXPEM(t, edPub), true},
		{"wrong curve (P-384)", mustPKIXPEM(t, &p384.PublicKey), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := parsePublicKeyPEM(tt.pem)
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected success, got %v", err)
			}
		})
	}
}

// TestEmbeddedKeyFingerprint pins FR-020: the committed production/placeholder public
// key (keys/sukko.pub) MUST have a SHA-256 (over its PKIX/SPKI DER) equal to
// expectedPublicKeyFingerprint. This is the offline drift tripwire — no cloud dependency —
// and the same test validates the KMS-delivered production key once the DK-1 cross-repo PR
// swaps the placeholder (DK-2). It reads keys/sukko.pub directly (not embeddedPublicKeyBytes),
// so it pins the committed production key regardless of build tag: under -tags sukko_e2e the
// embedded bytes are the regenerated dev key, which intentionally does not match the pin.
func TestEmbeddedKeyFingerprint(t *testing.T) {
	t.Parallel()

	pemBytes, err := os.ReadFile("keys/sukko.pub")
	if err != nil {
		t.Fatalf("read committed public key: %v", err)
	}
	pub, err := parsePublicKeyPEM(pemBytes)
	if err != nil {
		t.Fatalf("parse embedded key: %v", err)
	}
	fp, err := publicKeyFingerprint(pub)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	if fp != expectedPublicKeyFingerprint {
		t.Fatalf("embedded key fingerprint = %s, want %s\n(if the embedded key changed intentionally, update expectedPublicKeyFingerprint in license.go)", fp, expectedPublicKeyFingerprint)
	}
}
