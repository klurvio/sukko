package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
)

// GenerateTestKeyPair creates an Ed25519 key pair for testing.
// Panics on failure — test-only, never used in production.
func GenerateTestKeyPair() (ed25519.PrivateKey, ed25519.PublicKey) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic("license: generate test key pair: " + err.Error())
	}
	return priv, pub
}

// SignTestLicense creates a signed license key string for testing.
// Format: base64url(json_claims).base64url(signature)
func SignTestLicense(claims Claims, privateKey ed25519.PrivateKey) string {
	payload, err := json.Marshal(claims)
	if err != nil {
		panic("license: marshal test claims: " + err.Error())
	}
	signature := ed25519.Sign(privateKey, payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(signature)
}

// SetPublicKeyForTesting replaces the package-level publicKey for test isolation.
// Must be called before ParseAndVerify in each test.
// NOT exported outside the package — same-package tests only.
//
// Tests that call this MUST NOT use t.Parallel() since they share the
// package-level publicKey variable (Constitution VIII).
func SetPublicKeyForTesting(key ed25519.PublicKey) {
	publicKey = key
}
