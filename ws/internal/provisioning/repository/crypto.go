package repository

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

// aes256KeySize is the required key length for AES-256.
const aes256KeySize = 32

// errInvalidKeyLength is returned when the parsed key is not exactly 32 bytes.
var errInvalidKeyLength = errors.New("encryption key must be exactly 32 bytes for AES-256")

// encryptCredential encrypts plaintext using AES-256-GCM and returns a base64-encoded
// ciphertext with the nonce prepended.
func encryptCredential(plaintext string, key []byte) (string, error) {
	if len(key) != aes256KeySize {
		return "", errInvalidKeyLength
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	// Seal appends the ciphertext to the nonce slice, so the result is nonce + ciphertext + tag.
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)

	return base64.StdEncoding.EncodeToString(sealed), nil
}

// decryptCredential base64-decodes the ciphertext, extracts the prepended nonce,
// and decrypts using AES-256-GCM.
func decryptCredential(ciphertext string, key []byte) (string, error) {
	if len(key) != aes256KeySize {
		return "", errInvalidKeyLength
	}

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce := data[:nonceSize]
	encrypted := data[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}

	return string(plaintext), nil
}

// parseEncryptionKey accepts a hex-encoded (64 chars) or base64-encoded string
// and returns the 32-byte key. Returns an error if neither encoding works or
// the result is not exactly 32 bytes.
func parseEncryptionKey(hexOrBase64 string) ([]byte, error) {
	// Try hex first (64 hex chars = 32 bytes).
	if len(hexOrBase64) == hex.EncodedLen(aes256KeySize) {
		key, err := hex.DecodeString(hexOrBase64)
		if err == nil && len(key) == aes256KeySize {
			return key, nil
		}
	}

	// Try base64.
	key, err := base64.StdEncoding.DecodeString(hexOrBase64)
	if err == nil && len(key) == aes256KeySize {
		return key, nil
	}

	// Try base64 URL encoding as fallback.
	key, err = base64.URLEncoding.DecodeString(hexOrBase64)
	if err == nil && len(key) == aes256KeySize {
		return key, nil
	}

	return nil, fmt.Errorf("encryption key must decode to exactly %d bytes (provide 64-char hex or base64)", aes256KeySize)
}
