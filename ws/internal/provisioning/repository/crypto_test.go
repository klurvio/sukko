package repository

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestEncryptDecryptRoundtrip(t *testing.T) {
	t.Parallel()

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate key: %v", err)
	}

	tests := []struct {
		name      string
		plaintext string
	}{
		{name: "short string", plaintext: "hello"},
		{name: "json payload", plaintext: `{"project_id":"my-project","private_key":"-----BEGIN RSA..."}`},
		{name: "empty string", plaintext: ""},
		{name: "unicode", plaintext: "credentials with unicode: \u00e9\u00e0\u00fc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			encrypted, err := encryptCredential(tt.plaintext, key)
			if err != nil {
				t.Fatalf("encryptCredential() error = %v", err)
			}
			if encrypted == tt.plaintext && tt.plaintext != "" {
				t.Error("encrypted should differ from plaintext")
			}

			decrypted, err := decryptCredential(encrypted, key)
			if err != nil {
				t.Fatalf("decryptCredential() error = %v", err)
			}
			if decrypted != tt.plaintext {
				t.Errorf("decryptCredential() = %q, want %q", decrypted, tt.plaintext)
			}
		})
	}
}

func TestEncryptDifferentNonce(t *testing.T) {
	t.Parallel()

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate key: %v", err)
	}

	plaintext := "same plaintext for both encryptions"

	enc1, err := encryptCredential(plaintext, key)
	if err != nil {
		t.Fatalf("first encrypt: %v", err)
	}
	enc2, err := encryptCredential(plaintext, key)
	if err != nil {
		t.Fatalf("second encrypt: %v", err)
	}

	if enc1 == enc2 {
		t.Error("encrypting the same plaintext twice should produce different ciphertexts (random nonce)")
	}

	// Both should decrypt to the same value
	dec1, err := decryptCredential(enc1, key)
	if err != nil {
		t.Fatalf("decrypt first: %v", err)
	}
	dec2, err := decryptCredential(enc2, key)
	if err != nil {
		t.Fatalf("decrypt second: %v", err)
	}
	if dec1 != dec2 {
		t.Errorf("decrypted values differ: %q vs %q", dec1, dec2)
	}
}

func TestDecryptWrongKey(t *testing.T) {
	t.Parallel()

	keyA := make([]byte, 32)
	keyB := make([]byte, 32)
	if _, err := rand.Read(keyA); err != nil {
		t.Fatalf("generate key A: %v", err)
	}
	if _, err := rand.Read(keyB); err != nil {
		t.Fatalf("generate key B: %v", err)
	}

	encrypted, err := encryptCredential("secret data", keyA)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	_, err = decryptCredential(encrypted, keyB)
	if err == nil {
		t.Fatal("decryptCredential() with wrong key should return an error")
	}
}

func TestParseEncryptionKey(t *testing.T) {
	t.Parallel()

	rawKey := make([]byte, 32)
	if _, err := rand.Read(rawKey); err != nil {
		t.Fatalf("generate key: %v", err)
	}

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "hex encoded 64 chars",
			input:   hex.EncodeToString(rawKey),
			wantErr: false,
		},
		{
			name:    "base64 standard encoded",
			input:   base64.StdEncoding.EncodeToString(rawKey),
			wantErr: false,
		},
		{
			name:    "base64 URL encoded",
			input:   base64.URLEncoding.EncodeToString(rawKey),
			wantErr: false,
		},
		{
			name:    "too short",
			input:   "abcd1234",
			wantErr: true,
		},
		{
			name:    "wrong length hex",
			input:   hex.EncodeToString(rawKey[:16]), // 16 bytes = 32 hex chars, not 32 bytes
			wantErr: true,
		},
		{
			name:    "invalid encoding",
			input:   "not-a-valid-encoding-string-that-is-long-enough-to-test!!!!!!!!!!!",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			key, err := ParseEncryptionKey(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseEncryptionKey() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseEncryptionKey() error = %v", err)
			}
			if len(key) != 32 {
				t.Errorf("key length = %d, want 32", len(key))
			}
		})
	}
}

func TestEncryptInvalidKeyLength(t *testing.T) {
	t.Parallel()

	shortKey := make([]byte, 16)
	if _, err := rand.Read(shortKey); err != nil {
		t.Fatalf("generate key: %v", err)
	}

	_, err := encryptCredential("test", shortKey)
	if err == nil {
		t.Fatal("encryptCredential() with 16-byte key should return an error")
	}

	_, err = decryptCredential("dGVzdA==", shortKey)
	if err == nil {
		t.Fatal("decryptCredential() with 16-byte key should return an error")
	}
}
