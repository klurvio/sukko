package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// AuthProvider signs HTTP requests with admin credentials.
// Every code path — CLI, tester, unit tests — uses KeypairAuthProvider.
// No noop implementation: every request exercises real JWT signing.
type AuthProvider interface {
	// SignRequest adds an Authorization header with a signed admin JWT.
	SignRequest(req *http.Request)
}

// KeypairAuthProvider signs requests with an Ed25519 admin JWT.
// Each call to SignRequest creates a fresh short-lived JWT (5 min expiry)
// with a unique jti.
type KeypairAuthProvider struct {
	privateKey ed25519.PrivateKey
	keyID      string // kid in JWT header
	keyName    string // sub in JWT claims
}

// NewKeypairAuthProvider creates a KeypairAuthProvider with the given keypair identity.
func NewKeypairAuthProvider(privateKey ed25519.PrivateKey, keyID, keyName string) *KeypairAuthProvider {
	return &KeypairAuthProvider{
		privateKey: privateKey,
		keyID:      keyID,
		keyName:    keyName,
	}
}

// NewEphemeralAuthProvider generates a random Ed25519 keypair and returns
// a KeypairAuthProvider. Used by the tester and unit tests — the keypair
// is registered with provisioning via ADMIN_BOOTSTRAP_KEY.
func NewEphemeralAuthProvider() (*KeypairAuthProvider, ed25519.PublicKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate ephemeral admin keypair: %w", err)
	}
	provider := &KeypairAuthProvider{
		privateKey: priv,
		keyID:      "ephemeral-" + uuid.NewString()[:8],
		keyName:    "tester",
	}
	return provider, pub, nil
}

// SignRequest creates a short-lived admin JWT and sets it as the Authorization header.
func (p *KeypairAuthProvider) SignRequest(req *http.Request) {
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": "sukko-admin",
		"sub": p.keyName,
		"exp": jwt.NewNumericDate(now.Add(5 * time.Minute)),
		"iat": jwt.NewNumericDate(now),
		"jti": uuid.NewString(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["kid"] = p.keyID

	signed, err := token.SignedString(p.privateKey)
	if err != nil {
		return // Ed25519 signing cannot fail with a valid key; unauthenticated request will be rejected by server
	}

	req.Header.Set("Authorization", "Bearer "+signed)
}

// KeyID returns the admin key ID used in JWT headers.
func (p *KeypairAuthProvider) KeyID() string {
	return p.keyID
}

// PublicKey returns the Ed25519 public key (derived from the private key).
func (p *KeypairAuthProvider) PublicKey() ed25519.PublicKey {
	return p.privateKey.Public().(ed25519.PublicKey)
}
