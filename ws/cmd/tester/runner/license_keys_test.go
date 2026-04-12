package runner

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

func TestNewLicenseKeyGeneratorFromBytes_Valid(t *testing.T) {
	t.Parallel()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	gen, err := newLicenseKeyGeneratorFromBytes(priv)
	if err != nil {
		t.Fatalf("newLicenseKeyGeneratorFromBytes() error = %v", err)
	}
	if gen == nil {
		t.Fatal("expected non-nil generator")
	}
}

func TestNewLicenseKeyGeneratorFromBytes_InvalidSize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		key  []byte
	}{
		{"too short", make([]byte, 32)},
		{"too long", make([]byte, 128)},
		{"one byte", make([]byte, 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := newLicenseKeyGeneratorFromBytes(tt.key)
			if err == nil {
				t.Errorf("expected error for %d-byte key, got nil", len(tt.key))
			}
		})
	}
}

func TestNewLicenseKeyGeneratorFromBytes_Empty(t *testing.T) {
	t.Parallel()
	_, err := newLicenseKeyGeneratorFromBytes(nil)
	if err == nil {
		t.Error("expected error for nil key, got nil")
	}
	_, err = newLicenseKeyGeneratorFromBytes([]byte{})
	if err == nil {
		t.Error("expected error for empty key, got nil")
	}
}
