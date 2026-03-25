package license

import (
	"crypto/ed25519"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

//go:embed keys/sukko.pub
var embeddedPublicKeyBytes []byte

// publicKey is the Ed25519 public key used for license verification.
// Initialized from the embedded file at package init.
// Tests override via SetPublicKeyForTesting().
var publicKey ed25519.PublicKey

func init() {
	publicKey = ed25519.PublicKey(embeddedPublicKeyBytes)
}

// Claims holds the decoded fields from a license key.
type Claims struct {
	// Edition is the licensed tier (pro, enterprise).
	Edition Edition `json:"edition"`

	// Org is the licensee organization name.
	Org string `json:"org"`

	// Exp is the expiration time as a Unix timestamp (seconds).
	Exp int64 `json:"exp"`

	// Nodes is the advisory maximum node count (contractual, not enforced).
	Nodes int `json:"nodes,omitempty"`

	// Limits contains per-customer limit overrides. Non-zero values override
	// the edition's DefaultLimits.
	Limits Limits `json:"limits,omitempty"`
}

// IsExpired returns true if the license has passed its expiration date.
func (c *Claims) IsExpired() bool {
	return time.Now().Unix() > c.Exp
}

// ParseAndVerify verifies and parses a license key using the embedded public key.
func ParseAndVerify(key string) (*Claims, error) {
	return parseAndVerify(key, publicKey)
}

// parseAndVerify is the testable inner function that accepts an explicit key.
func parseAndVerify(key string, pubKey ed25519.PublicKey) (*Claims, error) {
	// Split into payload.signature (exactly 2 parts)
	parts := strings.SplitN(key, ".", 3)
	if len(parts) != 2 {
		return nil, fmt.Errorf("%w: expected 2 parts (payload.signature), got %d", ErrLicenseInvalidFormat, len(parts))
	}

	// Decode payload
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: payload base64 decode: %w", ErrLicenseInvalidFormat, err)
	}

	// Decode signature
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: signature base64 decode: %w", ErrLicenseInvalidFormat, err)
	}

	// Verify Ed25519 signature
	if !ed25519.Verify(pubKey, payload, sig) {
		return nil, ErrLicenseInvalidSignature
	}

	// Unmarshal claims
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("%w: claims JSON: %w", ErrLicenseInvalidFormat, err)
	}

	// Check expiry
	if claims.IsExpired() {
		return &claims, ErrLicenseExpired
	}

	return &claims, nil
}
