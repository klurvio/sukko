package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

// MUST NOT use t.Parallel() — tests share package-level publicKey via SetPublicKeyForTesting.

func TestParseAndVerify_Valid(t *testing.T) {
	priv, pub := GenerateTestKeyPair()
	SetPublicKeyForTesting(pub)

	claims := Claims{
		Edition: Pro,
		Org:     "Acme Corp",
		Exp:     time.Now().Add(24 * time.Hour).Unix(),
		Nodes:   3,
	}
	key := SignTestLicense(claims, priv)

	got, err := ParseAndVerify(key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Edition != Pro {
		t.Errorf("Edition = %q, want Pro", got.Edition)
	}
	if got.Org != "Acme Corp" {
		t.Errorf("Org = %q, want %q", got.Org, "Acme Corp")
	}
	if got.Nodes != 3 {
		t.Errorf("Nodes = %d, want 3", got.Nodes)
	}
}

func TestParseAndVerify_Expired(t *testing.T) {
	priv, pub := GenerateTestKeyPair()
	SetPublicKeyForTesting(pub)

	claims := Claims{
		Edition: Pro,
		Org:     "Expired Corp",
		Exp:     time.Now().Add(-1 * time.Hour).Unix(), // expired 1 hour ago
	}
	key := SignTestLicense(claims, priv)

	got, err := ParseAndVerify(key)
	if !errors.Is(err, ErrLicenseExpired) {
		t.Fatalf("expected ErrLicenseExpired, got: %v", err)
	}
	// Expired claims are still returned for inspection
	if got == nil {
		t.Fatal("expected non-nil claims for expired license")
	}
	if got.Org != "Expired Corp" {
		t.Errorf("Org = %q, want %q", got.Org, "Expired Corp")
	}
}

func TestParseAndVerify_InvalidSignature(t *testing.T) {
	_, pub := GenerateTestKeyPair()
	SetPublicKeyForTesting(pub)

	// Sign with a DIFFERENT key
	otherPriv, _ := GenerateTestKeyPair()
	claims := Claims{Edition: Pro, Org: "Tampered", Exp: time.Now().Add(time.Hour).Unix()}
	key := SignTestLicense(claims, otherPriv)

	_, err := ParseAndVerify(key)
	if !errors.Is(err, ErrLicenseInvalidSignature) {
		t.Fatalf("expected ErrLicenseInvalidSignature, got: %v", err)
	}
}

func TestParseAndVerify_InvalidFormat(t *testing.T) {
	_, pub := GenerateTestKeyPair()
	SetPublicKeyForTesting(pub)

	tests := []struct {
		name string
		key  string
	}{
		{"no separator", "nodothere"},
		{"too many parts", "a.b.c"},
		{"empty string", ""},
		{"bad payload base64", "!!!invalid!!!." + base64.RawURLEncoding.EncodeToString([]byte("sig"))},
		{"bad signature base64", base64.RawURLEncoding.EncodeToString([]byte("{}")) + ".!!!invalid!!!"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseAndVerify(tt.key)
			if !errors.Is(err, ErrLicenseInvalidFormat) {
				t.Errorf("expected ErrLicenseInvalidFormat, got: %v", err)
			}
		})
	}
}

func TestParseAndVerify_InvalidJSON(t *testing.T) {
	priv, pub := GenerateTestKeyPair()
	SetPublicKeyForTesting(pub)

	// Sign raw bytes that aren't valid JSON
	payload := []byte("not json at all")
	sig := ed25519.Sign(priv, payload)
	key := base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(sig)

	_, err := ParseAndVerify(key)
	if !errors.Is(err, ErrLicenseInvalidFormat) {
		t.Errorf("expected ErrLicenseInvalidFormat for invalid JSON, got: %v", err)
	}
}

func TestParseAndVerify_WithLimits(t *testing.T) {
	priv, pub := GenerateTestKeyPair()
	SetPublicKeyForTesting(pub)

	claims := Claims{
		Edition: Pro,
		Org:     "Custom Limits Corp",
		Exp:     time.Now().Add(24 * time.Hour).Unix(),
		Limits: Limits{
			MaxTenants: 100,
		},
	}
	key := SignTestLicense(claims, priv)

	got, err := ParseAndVerify(key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Limits.MaxTenants != 100 {
		t.Errorf("Limits.MaxTenants = %d, want 100", got.Limits.MaxTenants)
	}
}
