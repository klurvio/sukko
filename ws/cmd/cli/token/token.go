// Package token provides JWT token generation and validation for the CLI.
package token

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	defaultAlgorithm = "HS256"
	defaultTTL       = time.Hour
)

// GenerateConfig configures token generation.
type GenerateConfig struct {
	Subject   string
	TenantID  string
	Roles     []string
	Groups    []string
	Scopes    []string
	TTL       time.Duration
	Secret    string // HMAC secret
	KeyFile   string // path to PEM private key (asymmetric)
	Algorithm string // HS256, ES256, RS256, EdDSA
}

// DecodedToken represents a decoded (but not necessarily verified) JWT.
type DecodedToken struct {
	Header map[string]any `json:"header"`
	Claims map[string]any `json:"claims"`
	Valid  bool           `json:"valid"`
	Error  string         `json:"error,omitempty"`
}

// Generate creates a signed JWT token.
func Generate(cfg GenerateConfig) (string, error) {
	if cfg.Algorithm == "" {
		cfg.Algorithm = defaultAlgorithm
	}
	if cfg.TTL == 0 {
		cfg.TTL = defaultTTL
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"iat": now.Unix(),
		"exp": now.Add(cfg.TTL).Unix(),
	}

	if cfg.Subject != "" {
		claims["sub"] = cfg.Subject
	}
	if cfg.TenantID != "" {
		claims["tenant_id"] = cfg.TenantID
	}
	if len(cfg.Roles) > 0 {
		claims["roles"] = cfg.Roles
	}
	if len(cfg.Groups) > 0 {
		claims["groups"] = cfg.Groups
	}
	if len(cfg.Scopes) > 0 {
		claims["scopes"] = cfg.Scopes
	}

	method, err := signingMethod(cfg.Algorithm)
	if err != nil {
		return "", err
	}

	token := jwt.NewWithClaims(method, claims)

	key, err := signingKey(cfg)
	if err != nil {
		return "", err
	}

	signed, err := token.SignedString(key)
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}

	return signed, nil
}

// Decode decodes a JWT without verifying the signature. Use ValidateWithSecret
// for cryptographic verification.
func Decode(tokenString string) (*DecodedToken, error) {
	if tokenString == "" {
		return nil, errors.New("token string is required")
	}

	parser := jwt.NewParser()
	token, _, err := parser.ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		return &DecodedToken{ //nolint:nilerr // parse error is returned as a field in DecodedToken, not as err
			Valid: false,
			Error: err.Error(),
		}, nil
	}

	claims, _ := token.Claims.(jwt.MapClaims)

	result := &DecodedToken{
		Header: token.Header,
		Claims: mapFromClaims(claims),
		Valid:  true,
	}

	// Check expiry
	if exp, ok := claims["exp"]; ok {
		if expFloat, ok := exp.(float64); ok {
			if time.Unix(int64(expFloat), 0).Before(time.Now()) {
				result.Valid = false
				result.Error = "token expired"
			}
		}
	}

	return result, nil
}

// ValidateWithSecret verifies a JWT using an HMAC secret.
func ValidateWithSecret(tokenString, secret string) (*DecodedToken, error) {
	if tokenString == "" {
		return nil, errors.New("token string is required")
	}
	if secret == "" {
		return nil, errors.New("secret is required for HMAC validation")
	}

	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})

	result := &DecodedToken{
		Valid: err == nil && token.Valid,
	}

	if err != nil {
		result.Error = err.Error()
	}

	if token != nil {
		result.Header = token.Header
		if claims, ok := token.Claims.(jwt.MapClaims); ok {
			result.Claims = mapFromClaims(claims)
		}
	}

	return result, nil
}

func signingMethod(algorithm string) (jwt.SigningMethod, error) {
	switch algorithm {
	case "HS256":
		return jwt.SigningMethodHS256, nil
	case "HS384":
		return jwt.SigningMethodHS384, nil
	case "HS512":
		return jwt.SigningMethodHS512, nil
	case "ES256":
		return jwt.SigningMethodES256, nil
	case "ES384":
		return jwt.SigningMethodES384, nil
	case "RS256":
		return jwt.SigningMethodRS256, nil
	case "RS384":
		return jwt.SigningMethodRS384, nil
	case "RS512":
		return jwt.SigningMethodRS512, nil
	case "EdDSA":
		return jwt.SigningMethodEdDSA, nil
	default:
		return nil, fmt.Errorf("unsupported algorithm: %s", algorithm)
	}
}

func signingKey(cfg GenerateConfig) (any, error) {
	if cfg.Secret != "" && cfg.KeyFile != "" {
		return nil, errors.New("cannot specify both --secret and --key-file")
	}

	switch {
	case cfg.Secret != "":
		switch cfg.Algorithm {
		case "HS256", "HS384", "HS512", "":
			return []byte(cfg.Secret), nil
		default:
			return nil, fmt.Errorf("--secret can only be used with HMAC algorithms (HS256/HS384/HS512), not %s", cfg.Algorithm)
		}
	case cfg.KeyFile != "":
		return loadPrivateKey(cfg.KeyFile)
	default:
		return nil, errors.New("either --secret or --key-file is required")
	}
}

func loadPrivateKey(path string) (crypto.PrivateKey, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: CLI reads user-specified key file path
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in %s", path)
	}

	// Try PKCS8 first, then EC, then RSA
	key, pkcs8Err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if pkcs8Err == nil {
		return key, nil
	}
	ecKey, ecErr := x509.ParseECPrivateKey(block.Bytes)
	if ecErr == nil {
		return ecKey, nil
	}
	rsaKey, rsaErr := x509.ParsePKCS1PrivateKey(block.Bytes)
	if rsaErr == nil {
		return rsaKey, nil
	}

	return nil, fmt.Errorf("unsupported key format in %s (pkcs8: %w, ec: %w, pkcs1: %w)", path, pkcs8Err, ecErr, rsaErr)
}

func mapFromClaims(claims jwt.MapClaims) map[string]any {
	result := make(map[string]any, len(claims))
	for k, v := range claims {
		// Convert numeric types for clean JSON output
		if f, ok := v.(json.Number); ok {
			if i, err := f.Int64(); err == nil {
				result[k] = i
				continue
			}
		}
		result[k] = v
	}
	return result
}

// Exported key type checkers for referencing in validation code.
var (
	_ crypto.PrivateKey = (*ecdsa.PrivateKey)(nil)
	_ crypto.PrivateKey = (*rsa.PrivateKey)(nil)
	_ crypto.PrivateKey = ed25519.PrivateKey(nil)
)
