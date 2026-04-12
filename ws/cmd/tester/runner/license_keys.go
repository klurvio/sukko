package runner

import (
	"crypto/ed25519"
	"fmt"
	"os"
	"time"

	"github.com/klurvio/sukko/internal/shared/license"
)

// licenseKeyGenerator manages monotonic iat and signs test license keys.
// The private key is read from a file path at runtime (never embedded).
type licenseKeyGenerator struct {
	privateKey ed25519.PrivateKey
	nextIat    int64
}

// newLicenseKeyGenerator reads the Ed25519 private key from the given file path
// and returns a generator with monotonic iat starting from now.
func newLicenseKeyGenerator(keyFilePath string) (*licenseKeyGenerator, error) {
	keyBytes, err := os.ReadFile(keyFilePath) //nolint:gosec // G304: file path is from trusted config (TESTER_SIGNING_KEY_FILE env var), not user input
	if err != nil {
		return nil, fmt.Errorf("read license signing key %s: %w", keyFilePath, err)
	}
	return newLicenseKeyGeneratorFromBytes(keyBytes)
}

// newLicenseKeyGeneratorFromBytes creates a generator from raw Ed25519 private key bytes.
// Used when the key is passed via the API request body (no file read needed).
func newLicenseKeyGeneratorFromBytes(keyBytes []byte) (*licenseKeyGenerator, error) {
	if len(keyBytes) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid Ed25519 private key: expected %d bytes, got %d", ed25519.PrivateKeySize, len(keyBytes))
	}
	return &licenseKeyGenerator{
		privateKey: ed25519.PrivateKey(keyBytes),
		nextIat:    time.Now().Unix(),
	}, nil
}

// sign creates a valid license key for the given edition with a monotonically increasing iat.
func (g *licenseKeyGenerator) sign(edition license.Edition, org string, expireIn time.Duration) string {
	iat := g.nextIat
	g.nextIat++
	return license.SignTestLicense(license.Claims{
		Edition: edition,
		Org:     org,
		Exp:     time.Now().Add(expireIn).Unix(),
		Iat:     iat,
	}, g.privateKey)
}

// signExpired creates a key with an expiration in the past (for testing 400 rejection).
func (g *licenseKeyGenerator) signExpired(edition license.Edition) string {
	iat := g.nextIat
	g.nextIat++
	return license.SignTestLicense(license.Claims{
		Edition: edition,
		Org:     "Test Expired",
		Exp:     time.Now().Add(-1 * time.Hour).Unix(),
		Iat:     iat,
	}, g.privateKey)
}

// signWithIat creates a key with a specific iat (for replay testing).
// Does NOT increment nextIat — caller controls the iat value.
func (g *licenseKeyGenerator) signWithIat(edition license.Edition, iat int64) string {
	return license.SignTestLicense(license.Claims{
		Edition: edition,
		Org:     "Test Replay",
		Exp:     time.Now().Add(24 * time.Hour).Unix(),
		Iat:     iat,
	}, g.privateKey)
}
