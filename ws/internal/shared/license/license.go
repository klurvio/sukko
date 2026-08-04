package license

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

const (
	// rawSigLen is the length of a raw r||s ECDSA P-256 signature: two 32-byte
	// big-endian scalars. The License Service's signer emits DER and converts to
	// this fixed-width form; the validator splits it back into (r, s).
	rawSigLen = 64

	// p256ScalarLen is the byte width of a single P-256 scalar (r or s) — the split
	// point of a raw r||s signature.
	p256ScalarLen = rawSigLen / 2

	// pemTypePublicKey is the PEM block type for a PKIX (SubjectPublicKeyInfo)
	// public key — the form genkeys emits and the validator embeds.
	pemTypePublicKey = "PUBLIC KEY"

	// expectedPublicKeyFingerprint is the SHA-256 (lowercase hex) over the embedded
	// public key's PKIX/SPKI DER — the same fingerprint the License Service's KMS
	// adapter computes. The FR-020 tripwire test asserts the embedded key matches
	// this value; it is updated in lockstep whenever the embedded key changes (in
	// the KMS-provisioning wave, by the sukko-license DK-1 workflow that opens the
	// cross-repo key-bump PR). Today it pins the placeholder dev key (keys/sukko.pub).
	expectedPublicKeyFingerprint = "0c4171ee7ed131eadcbd1a872ddedbc5b7dc0fad7a01b179063b34bc610f054a"
)

// embeddedPublicKeyBytes holds the PKIX-PEM license public key compiled into the
// binary. It is declared by exactly one build-tag-selected source: embed_default.go
// (the committed production/placeholder key, keys/sukko.pub) for normal builds, or
// embed_e2e.go (the regenerated dev key, keys/sukko.dev.pub) under the `sukko_e2e`
// build tag so the e2e harness can validate keys it mints at runtime (FR-017).

// publicKey is the ECDSA P-256 public key used for license verification.
// Initialized from the embedded PKIX PEM at package init.
// Tests override via SetPublicKeyForTesting().
var publicKey *ecdsa.PublicKey

func init() {
	pub, err := parsePublicKeyPEM(embeddedPublicKeyBytes)
	if err != nil {
		// The embedded key is a build-time constant — a malformed, non-ECDSA, or
		// wrong-curve embed is a build/deploy error, never a green boot that then
		// rejects every license at runtime (FR-018, §II/§IV fail-fast).
		panic("license: parse embedded public key: " + err.Error())
	}
	publicKey = pub
}

// parsePublicKeyPEM decodes a PKIX (SubjectPublicKeyInfo) PEM block into an
// ECDSA P-256 public key, returning distinct wrapped errors for each failure mode
// so a bad embedded key (FR-018) or a bad operator-supplied key fails clearly.
func parsePublicKeyPEM(pemBytes []byte) (*ecdsa.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("no PEM block found")
	}
	if block.Type != pemTypePublicKey {
		return nil, fmt.Errorf("PEM block type is %q, want %q", block.Type, pemTypePublicKey)
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse PKIX public key: %w", err)
	}
	pub, ok := parsed.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("embedded key is %T, want *ecdsa.PublicKey", parsed)
	}
	if pub.Curve != elliptic.P256() {
		return nil, fmt.Errorf("embedded key curve is %s, want P-256", pub.Curve.Params().Name)
	}
	return pub, nil
}

// publicKeyFingerprint returns the SHA-256 (lowercase hex) of a public key's PKIX
// (SubjectPublicKeyInfo) DER — the FR-020 identity token, matching the fingerprint
// the License Service's KMS adapter computes.
func publicKeyFingerprint(pub *ecdsa.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", fmt.Errorf("marshal PKIX public key: %w", err)
	}
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:]), nil
}

// Claims holds the decoded fields from a license key.
type Claims struct {
	// Edition is the licensed tier (pro, enterprise).
	Edition Edition `json:"edition"`

	// Org is the licensee organization name.
	Org string `json:"org"`

	// Exp is the expiration time as a Unix timestamp (seconds).
	Exp int64 `json:"exp"`

	// Iat is the issued-at time as a Unix timestamp (seconds).
	// Used for replay protection: reject keys with iat <= current key's iat.
	// Omitempty for backward compatibility with pre-existing keys.
	Iat int64 `json:"iat,omitempty"`

	// Nodes is the advisory maximum node count (contractual, not enforced).
	Nodes int `json:"nodes,omitempty"`

	// Limits contains per-customer limit overrides. Non-zero values override
	// the edition's DefaultLimits.
	Limits Limits `json:"limits,omitzero"`
}

// IsExpired returns true if the license has passed its expiration date.
func (c *Claims) IsExpired() bool {
	return time.Now().Unix() > c.Exp
}

// ParseAndVerify verifies and parses a license key using the embedded public key.
func ParseAndVerify(key string) (*Claims, error) {
	return parseAndVerify(key, publicKey)
}

// parseAndVerify is the testable inner function that accepts an explicit key — the
// per-call injectable seam the golden-vector test uses (FR-007), sharing no mutable
// package state with the embedded-key path.
func parseAndVerify(key string, pubKey *ecdsa.PublicKey) (*Claims, error) {
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

	// Decode signature (64-byte raw r||s)
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: signature base64 decode: %w", ErrLicenseInvalidFormat, err)
	}

	// A signature that is not exactly 64 bytes is a malformed key (format error).
	// §XVIII: the sukko-license mirror (SelfVerify) classifies a wrong-length sig
	// under ErrInvalidSignature; the validator treats length as a format check
	// (spec edge case) — both still reject, only the error class differs.
	if len(sig) != rawSigLen {
		return nil, fmt.Errorf("%w: signature must be %d bytes, got %d", ErrLicenseInvalidFormat, rawSigLen, len(sig))
	}

	// Verify ECDSA P-256 signature over SHA-256 of the raw payload bytes.
	if !verifyRawSignature(pubKey, payload, sig) {
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

// verifyRawSignature checks a 64-byte raw r||s ECDSA P-256 signature against the
// SHA-256 digest of the payload. r and s are the two 32-byte big-endian halves;
// short (leading-zero) halves verify correctly because SetBytes is width-agnostic.
func verifyRawSignature(pubKey *ecdsa.PublicKey, payload, sig []byte) bool {
	if pubKey == nil || len(sig) != rawSigLen {
		return false
	}
	digest := sha256.Sum256(payload)
	r := new(big.Int).SetBytes(sig[:p256ScalarLen])
	s := new(big.Int).SetBytes(sig[p256ScalarLen:])
	return ecdsa.Verify(pubKey, digest[:], r, s)
}
