package gateway

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/shared/types"
)

// oidcMockRegistry is a test double for TenantRegistry used by OIDC tests.
// Separate from mockTenantRegistry in tenant_registry_test.go because this
// adds a getErr field for simulating registry failures.
type oidcMockRegistry struct {
	tenantByIssuer map[string]string
	oidcConfigs    map[string]*types.TenantOIDCConfig
	channelRules   map[string]*types.ChannelRules
	getErr         error // if set, all calls return this error
}

func newOIDCMockRegistry() *oidcMockRegistry {
	return &oidcMockRegistry{
		tenantByIssuer: make(map[string]string),
		oidcConfigs:    make(map[string]*types.TenantOIDCConfig),
		channelRules:   make(map[string]*types.ChannelRules),
	}
}

func (m *oidcMockRegistry) GetTenantByIssuer(_ context.Context, issuerURL string) (string, error) {
	if m.getErr != nil {
		return "", m.getErr
	}
	tid, ok := m.tenantByIssuer[issuerURL]
	if !ok {
		return "", types.ErrIssuerNotFound
	}
	return tid, nil
}

func (m *oidcMockRegistry) GetOIDCConfig(_ context.Context, tenantID string) (*types.TenantOIDCConfig, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	cfg, ok := m.oidcConfigs[tenantID]
	if !ok {
		return nil, types.ErrOIDCNotConfigured
	}
	return cfg, nil
}

func (m *oidcMockRegistry) GetChannelRules(_ context.Context, tenantID string) (*types.ChannelRules, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	rules, ok := m.channelRules[tenantID]
	if !ok {
		return nil, types.ErrChannelRulesNotFound
	}
	return rules, nil
}

func (m *oidcMockRegistry) Close() error { return nil }

// serveJWKS creates an httptest.Server serving a valid JWKS endpoint with an EC P-256 key.
func serveJWKS(t *testing.T) *httptest.Server {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate EC key: %v", err)
	}

	// Extract raw X/Y coordinates from the uncompressed SEC1 public key bytes.
	// Format: 0x04 || X (32 bytes) || Y (32 bytes) for P-256.
	ecdhPub, err := key.PublicKey.ECDH()
	if err != nil {
		t.Fatalf("convert to ECDH public key: %v", err)
	}
	rawPub := ecdhPub.Bytes() // uncompressed: 04 || X || Y
	xBytes := rawPub[1:33]
	yBytes := rawPub[33:65]

	// Build minimal JWKS JSON with the public key
	jwks := map[string]any{
		"keys": []map[string]any{
			{
				"kty": "EC",
				"crv": "P-256",
				"x":   base64.RawURLEncoding.EncodeToString(xBytes),
				"y":   base64.RawURLEncoding.EncodeToString(yBytes),
				"kid": "test-key-1",
				"use": "sig",
				"alg": "ES256",
			},
		},
	}

	jwksBytes, err := json.Marshal(jwks)
	if err != nil {
		t.Fatalf("marshal JWKS: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/jwks.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwksBytes)
	})

	return httptest.NewServer(mux)
}

func TestNewMultiIssuerOIDC_NilRegistry(t *testing.T) {
	t.Parallel()

	_, err := NewMultiIssuerOIDC(MultiIssuerOIDCConfig{
		Registry: nil,
		Logger:   zerolog.Nop(),
	})
	if err == nil {
		t.Fatal("expected error for nil registry")
	}
}

func TestNewMultiIssuerOIDC_ZeroValues(t *testing.T) {
	t.Parallel()

	registry := newOIDCMockRegistry()

	// Zero KeyfuncCacheTTL should be rejected
	_, err := NewMultiIssuerOIDC(MultiIssuerOIDCConfig{
		Registry:         registry,
		KeyfuncCacheTTL:  0,
		JWKSFetchTimeout: 10 * time.Second,
		Logger:           zerolog.Nop(),
	})
	if err == nil {
		t.Fatal("expected error for zero KeyfuncCacheTTL")
	}

	// Zero JWKSFetchTimeout should be rejected
	_, err = NewMultiIssuerOIDC(MultiIssuerOIDCConfig{
		Registry:         registry,
		KeyfuncCacheTTL:  1 * time.Hour,
		JWKSFetchTimeout: 0,
		Logger:           zerolog.Nop(),
	})
	if err == nil {
		t.Fatal("expected error for zero JWKSFetchTimeout")
	}
}

func TestMultiIssuerOIDC_GetKeyfunc_Success(t *testing.T) {
	t.Parallel()

	jwksServer := serveJWKS(t)
	defer jwksServer.Close()

	registry := newOIDCMockRegistry()
	registry.tenantByIssuer[jwksServer.URL] = "tenant-1"
	registry.oidcConfigs["tenant-1"] = &types.TenantOIDCConfig{
		TenantID:  "tenant-1",
		IssuerURL: jwksServer.URL,
		JWKSURL:   jwksServer.URL + "/.well-known/jwks.json",
		Enabled:   true,
	}

	m, err := NewMultiIssuerOIDC(MultiIssuerOIDCConfig{
		Registry:         registry,
		KeyfuncCacheTTL:  1 * time.Hour,
		JWKSFetchTimeout: 10 * time.Second,
		Logger:           zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = m.Close() }()

	kf, err := m.GetKeyfunc(context.Background(), jwksServer.URL)
	if err != nil {
		t.Fatalf("GetKeyfunc() error: %v", err)
	}
	if kf == nil {
		t.Fatal("GetKeyfunc() returned nil keyfunc")
	}
}

func TestMultiIssuerOIDC_GetKeyfunc_CacheHit(t *testing.T) {
	t.Parallel()

	jwksServer := serveJWKS(t)
	defer jwksServer.Close()

	registry := newOIDCMockRegistry()
	registry.tenantByIssuer[jwksServer.URL] = "tenant-1"
	registry.oidcConfigs["tenant-1"] = &types.TenantOIDCConfig{
		TenantID:  "tenant-1",
		IssuerURL: jwksServer.URL,
		JWKSURL:   jwksServer.URL + "/.well-known/jwks.json",
		Enabled:   true,
	}

	m, err := NewMultiIssuerOIDC(MultiIssuerOIDCConfig{
		Registry:         registry,
		KeyfuncCacheTTL:  1 * time.Hour,
		JWKSFetchTimeout: 10 * time.Second,
		Logger:           zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = m.Close() }()

	// First call populates cache
	kf1, err := m.GetKeyfunc(context.Background(), jwksServer.URL)
	if err != nil {
		t.Fatalf("first GetKeyfunc() error: %v", err)
	}

	// Second call should hit cache (registry calls not needed)
	registry.getErr = errors.New("should not be called")
	kf2, err := m.GetKeyfunc(context.Background(), jwksServer.URL)
	if err != nil {
		t.Fatalf("second GetKeyfunc() error: %v", err)
	}

	// Both calls should return non-nil keyfuncs
	if kf1 == nil || kf2 == nil {
		t.Fatal("keyfuncs should not be nil")
	}
}

func TestMultiIssuerOIDC_GetKeyfunc_CacheExpired(t *testing.T) {
	t.Parallel()

	jwksServer := serveJWKS(t)
	defer jwksServer.Close()

	registry := newOIDCMockRegistry()
	registry.tenantByIssuer[jwksServer.URL] = "tenant-1"
	registry.oidcConfigs["tenant-1"] = &types.TenantOIDCConfig{
		TenantID:  "tenant-1",
		IssuerURL: jwksServer.URL,
		JWKSURL:   jwksServer.URL + "/.well-known/jwks.json",
		Enabled:   true,
	}

	m, err := NewMultiIssuerOIDC(MultiIssuerOIDCConfig{
		Registry:         registry,
		KeyfuncCacheTTL:  1 * time.Millisecond, // Very short TTL
		JWKSFetchTimeout: 10 * time.Second,
		Logger:           zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = m.Close() }()

	// First call
	_, err = m.GetKeyfunc(context.Background(), jwksServer.URL)
	if err != nil {
		t.Fatalf("first GetKeyfunc() error: %v", err)
	}

	// Wait for expiration
	time.Sleep(5 * time.Millisecond)

	// Second call should re-create (cache expired)
	kf, err := m.GetKeyfunc(context.Background(), jwksServer.URL)
	if err != nil {
		t.Fatalf("second GetKeyfunc() error: %v", err)
	}
	if kf == nil {
		t.Fatal("keyfunc should not be nil after refresh")
	}
}

func TestMultiIssuerOIDC_GetKeyfunc_UnknownIssuer(t *testing.T) {
	t.Parallel()

	registry := newOIDCMockRegistry()

	m, err := NewMultiIssuerOIDC(MultiIssuerOIDCConfig{
		Registry:         registry,
		KeyfuncCacheTTL:  1 * time.Hour,
		JWKSFetchTimeout: 10 * time.Second,
		Logger:           zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = m.Close() }()

	_, err = m.GetKeyfunc(context.Background(), "https://unknown.example.com")
	if err == nil {
		t.Fatal("expected error for unknown issuer")
	}
	if !errors.Is(err, types.ErrIssuerNotFound) {
		t.Errorf("expected ErrIssuerNotFound, got: %v", err)
	}
}

func TestMultiIssuerOIDC_GetKeyfunc_OIDCDisabled(t *testing.T) {
	t.Parallel()

	registry := newOIDCMockRegistry()
	registry.tenantByIssuer["https://auth.example.com"] = "tenant-1"
	registry.oidcConfigs["tenant-1"] = &types.TenantOIDCConfig{
		TenantID:  "tenant-1",
		IssuerURL: "https://auth.example.com",
		Enabled:   false, // Disabled
	}

	m, err := NewMultiIssuerOIDC(MultiIssuerOIDCConfig{
		Registry:         registry,
		KeyfuncCacheTTL:  1 * time.Hour,
		JWKSFetchTimeout: 10 * time.Second,
		Logger:           zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = m.Close() }()

	_, err = m.GetKeyfunc(context.Background(), "https://auth.example.com")
	if err == nil {
		t.Fatal("expected error for disabled OIDC")
	}
	if !errors.Is(err, types.ErrOIDCNotConfigured) {
		t.Errorf("expected ErrOIDCNotConfigured, got: %v", err)
	}
}

func TestMultiIssuerOIDC_GetTenantByIssuer(t *testing.T) {
	t.Parallel()

	registry := newOIDCMockRegistry()
	registry.tenantByIssuer["https://auth.acme.com"] = "acme"

	m, err := NewMultiIssuerOIDC(MultiIssuerOIDCConfig{
		Registry:         registry,
		KeyfuncCacheTTL:  1 * time.Hour,
		JWKSFetchTimeout: 10 * time.Second,
		Logger:           zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = m.Close() }()

	tenantID, err := m.GetTenantByIssuer(context.Background(), "https://auth.acme.com")
	if err != nil {
		t.Fatalf("GetTenantByIssuer() error: %v", err)
	}
	if tenantID != "acme" {
		t.Errorf("tenantID = %q, want %q", tenantID, "acme")
	}

	// Unknown issuer
	_, err = m.GetTenantByIssuer(context.Background(), "https://unknown.example.com")
	if err == nil {
		t.Fatal("expected error for unknown issuer")
	}
}

func TestMultiIssuerOIDC_InvalidateIssuer(t *testing.T) {
	t.Parallel()

	jwksServer := serveJWKS(t)
	defer jwksServer.Close()

	registry := newOIDCMockRegistry()
	registry.tenantByIssuer[jwksServer.URL] = "tenant-1"
	registry.oidcConfigs["tenant-1"] = &types.TenantOIDCConfig{
		TenantID:  "tenant-1",
		IssuerURL: jwksServer.URL,
		JWKSURL:   jwksServer.URL + "/.well-known/jwks.json",
		Enabled:   true,
	}

	m, err := NewMultiIssuerOIDC(MultiIssuerOIDCConfig{
		Registry:         registry,
		KeyfuncCacheTTL:  1 * time.Hour,
		JWKSFetchTimeout: 10 * time.Second,
		Logger:           zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = m.Close() }()

	// Populate cache
	_, err = m.GetKeyfunc(context.Background(), jwksServer.URL)
	if err != nil {
		t.Fatalf("GetKeyfunc() error: %v", err)
	}

	// Verify cache entry exists
	m.keyfuncCacheMu.RLock()
	_, cached := m.keyfuncCache[jwksServer.URL]
	m.keyfuncCacheMu.RUnlock()
	if !cached {
		t.Fatal("expected cache entry after GetKeyfunc")
	}

	// Invalidate
	m.InvalidateIssuer(jwksServer.URL)

	// Verify cache entry removed
	m.keyfuncCacheMu.RLock()
	_, cached = m.keyfuncCache[jwksServer.URL]
	m.keyfuncCacheMu.RUnlock()
	if cached {
		t.Fatal("cache entry should be removed after InvalidateIssuer")
	}
}

func TestMultiIssuerOIDC_InvalidateIssuer_NotCached(t *testing.T) {
	t.Parallel()

	registry := newOIDCMockRegistry()
	m, err := NewMultiIssuerOIDC(MultiIssuerOIDCConfig{
		Registry:         registry,
		KeyfuncCacheTTL:  1 * time.Hour,
		JWKSFetchTimeout: 10 * time.Second,
		Logger:           zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = m.Close() }()

	// Should not panic on non-existent issuer
	m.InvalidateIssuer("https://not-cached.example.com")
}

func TestMultiIssuerOIDC_Close(t *testing.T) {
	t.Parallel()

	jwksServer := serveJWKS(t)
	defer jwksServer.Close()

	registry := newOIDCMockRegistry()
	registry.tenantByIssuer[jwksServer.URL] = "tenant-1"
	registry.oidcConfigs["tenant-1"] = &types.TenantOIDCConfig{
		TenantID:  "tenant-1",
		IssuerURL: jwksServer.URL,
		JWKSURL:   jwksServer.URL + "/.well-known/jwks.json",
		Enabled:   true,
	}

	m, err := NewMultiIssuerOIDC(MultiIssuerOIDCConfig{
		Registry:         registry,
		KeyfuncCacheTTL:  1 * time.Hour,
		JWKSFetchTimeout: 10 * time.Second,
		Logger:           zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Populate cache
	_, err = m.GetKeyfunc(context.Background(), jwksServer.URL)
	if err != nil {
		t.Fatalf("GetKeyfunc() error: %v", err)
	}

	// Close should clear cache
	if err := m.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	m.keyfuncCacheMu.RLock()
	cacheLen := len(m.keyfuncCache)
	m.keyfuncCacheMu.RUnlock()
	if cacheLen != 0 {
		t.Errorf("cache should be empty after Close, got %d entries", cacheLen)
	}
}

func TestMultiIssuerOIDC_Concurrent_GetKeyfunc(t *testing.T) {
	t.Parallel()

	jwksServer := serveJWKS(t)
	defer jwksServer.Close()

	registry := newOIDCMockRegistry()
	registry.tenantByIssuer[jwksServer.URL] = "tenant-1"
	registry.oidcConfigs["tenant-1"] = &types.TenantOIDCConfig{
		TenantID:  "tenant-1",
		IssuerURL: jwksServer.URL,
		JWKSURL:   jwksServer.URL + "/.well-known/jwks.json",
		Enabled:   true,
	}

	m, err := NewMultiIssuerOIDC(MultiIssuerOIDCConfig{
		Registry:         registry,
		KeyfuncCacheTTL:  1 * time.Hour,
		JWKSFetchTimeout: 10 * time.Second,
		Logger:           zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = m.Close() }()

	var wg sync.WaitGroup
	errCh := make(chan error, 10)

	for range 10 {
		wg.Go(func() {
			kf, err := m.GetKeyfunc(context.Background(), jwksServer.URL)
			if err != nil {
				errCh <- err
				return
			}
			if kf == nil {
				errCh <- errors.New("keyfunc is nil")
			}
		})
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent GetKeyfunc error: %v", err)
	}
}
