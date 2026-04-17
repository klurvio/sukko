package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog"
	"golang.org/x/time/rate"

	"github.com/klurvio/sukko/internal/shared/auth"
	"github.com/klurvio/sukko/internal/shared/protocol"
)

// drainConn reads and discards all data from a connection until it closes.
func drainConn(conn net.Conn) {
	buf := make([]byte, 4096)
	for {
		if _, err := conn.Read(buf); err != nil {
			return
		}
	}
}

// mockTokenValidator is a test double for TokenValidator.
type mockTokenValidator struct {
	claims *auth.Claims
	err    error
}

func (m *mockTokenValidator) ValidateToken(_ context.Context, _ string) (*auth.Claims, error) {
	return m.claims, m.err
}

// newAuthTestProxy creates a Proxy configured for auth refresh tests.
// Uses net.Pipe for client/backend connections so sendToClient works.
func newAuthTestProxy(validator TokenValidator, claims *auth.Claims) (proxy *Proxy, client, backend net.Conn) {
	clientConn, clientRemote := net.Pipe()
	backendConn, backendRemote := net.Pipe()

	tenantID := ""
	if claims != nil {
		tenantID = claims.TenantID
	}

	pc := NewPermissionChecker(
		[]string{"*.trade", "*.liquidity"},
		[]string{},
		[]string{},
	)

	proxy = &Proxy{
		clientConn:  clientConn,
		backendConn: backendConn,

		claims:                claims,
		tenantID:              tenantID,
		permissions:           pc,
		validator:             validator,
		logger:                zerolog.Nop(),
		messageTimeout:        60 * time.Second,
		maxFrameSize:          protocol.DefaultMaxFrameSize,
		publishLimiter:        rate.NewLimiter(10, 100),
		maxPublishSize:        64 * 1024,
		authLimiter:           rate.NewLimiter(rate.Every(100*time.Millisecond), 1), // Fast for tests
		authValidationTimeout: 5 * time.Second,
		subscribedChannels:    make(map[string]struct{}),
	}

	return proxy, clientRemote, backendRemote
}

func TestInterceptAuthRefresh_ValidToken(t *testing.T) {
	t.Parallel()
	oldClaims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user1"},
		TenantID:         "test-tenant",
	}
	newClaims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user1",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		TenantID: "test-tenant",
	}
	validator := &mockTokenValidator{claims: newClaims}
	proxy, clientRemote, backendRemote := newAuthTestProxy(validator, oldClaims)
	defer func() { _ = clientRemote.Close() }()
	defer func() { _ = backendRemote.Close() }()
	go drainConn(clientRemote)

	msg := protocol.ClientMessage{
		Type: MsgTypeAuth,
		Data: json.RawMessage(`{"token":"valid-jwt-token"}`),
	}
	result, err := proxy.interceptAuthRefresh(context.Background(), msg)

	if err != nil {
		t.Fatalf("interceptAuthRefresh error: %v", err)
	}
	if result != nil {
		t.Error("Expected nil result (message handled, not forwarded)")
	}

	// Verify claims were swapped
	proxy.claimsMu.RLock()
	if proxy.claims != newClaims {
		t.Error("Claims should have been swapped to new claims")
	}
	proxy.claimsMu.RUnlock()
}

func TestInterceptAuthRefresh_ExpiredToken(t *testing.T) {
	t.Parallel()
	oldClaims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user1"},
		TenantID:         "test-tenant",
	}
	validator := &mockTokenValidator{err: auth.ErrTokenExpired}
	proxy, clientRemote, backendRemote := newAuthTestProxy(validator, oldClaims)
	defer func() { _ = clientRemote.Close() }()
	defer func() { _ = backendRemote.Close() }()
	go drainConn(clientRemote)

	msg := protocol.ClientMessage{
		Type: MsgTypeAuth,
		Data: json.RawMessage(`{"token":"expired-token"}`),
	}
	result, err := proxy.interceptAuthRefresh(context.Background(), msg)

	if err != nil {
		t.Fatalf("interceptAuthRefresh error: %v", err)
	}
	if result != nil {
		t.Error("Expected nil result")
	}

	// Claims should NOT have changed
	proxy.claimsMu.RLock()
	if proxy.claims != oldClaims {
		t.Error("Claims should not have changed on expired token")
	}
	proxy.claimsMu.RUnlock()
}

func TestInterceptAuthRefresh_InvalidToken(t *testing.T) {
	t.Parallel()
	oldClaims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user1"},
		TenantID:         "test-tenant",
	}
	validator := &mockTokenValidator{err: auth.ErrInvalidToken}
	proxy, clientRemote, backendRemote := newAuthTestProxy(validator, oldClaims)
	defer func() { _ = clientRemote.Close() }()
	defer func() { _ = backendRemote.Close() }()
	go drainConn(clientRemote)

	msg := protocol.ClientMessage{
		Type: MsgTypeAuth,
		Data: json.RawMessage(`{"token":"bad-signature"}`),
	}
	result, _ := proxy.interceptAuthRefresh(context.Background(), msg)

	if result != nil {
		t.Error("Expected nil result")
	}
}

func TestInterceptAuthRefresh_TenantMismatch(t *testing.T) {
	t.Parallel()
	oldClaims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user1"},
		TenantID:         "tenant-a",
	}
	newClaims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user1"},
		TenantID:         "tenant-b",
	}
	validator := &mockTokenValidator{claims: newClaims}
	proxy, clientRemote, backendRemote := newAuthTestProxy(validator, oldClaims)
	defer func() { _ = clientRemote.Close() }()
	defer func() { _ = backendRemote.Close() }()
	go drainConn(clientRemote)

	msg := protocol.ClientMessage{
		Type: MsgTypeAuth,
		Data: json.RawMessage(`{"token":"different-tenant-token"}`),
	}
	result, _ := proxy.interceptAuthRefresh(context.Background(), msg)

	if result != nil {
		t.Error("Expected nil result")
	}

	// Claims should NOT have changed
	proxy.claimsMu.RLock()
	if proxy.claims != oldClaims {
		t.Error("Claims should not have changed on tenant mismatch")
	}
	proxy.claimsMu.RUnlock()
}

func TestInterceptAuthRefresh_RateLimited(t *testing.T) {
	t.Parallel()
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user1"},
		TenantID:         "test-tenant",
	}
	newClaims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user1",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		TenantID: "test-tenant",
	}
	validator := &mockTokenValidator{claims: newClaims}
	proxy, clientRemote, backendRemote := newAuthTestProxy(validator, claims)
	defer func() { _ = clientRemote.Close() }()
	defer func() { _ = backendRemote.Close() }()
	go drainConn(clientRemote)

	// Use a very slow rate limiter
	proxy.authLimiter = rate.NewLimiter(rate.Every(time.Hour), 1)

	msg := protocol.ClientMessage{
		Type: MsgTypeAuth,
		Data: json.RawMessage(`{"token":"valid-token"}`),
	}

	// First call succeeds (consumes the burst token)
	_, _ = proxy.interceptAuthRefresh(context.Background(), msg)

	// Second call should be rate limited
	result, err := proxy.interceptAuthRefresh(context.Background(), msg)
	if err != nil {
		t.Fatalf("Expected nil error, got: %v", err)
	}
	if result != nil {
		t.Error("Expected nil result for rate limited request")
	}
}

func TestInterceptAuthRefresh_EmptyToken(t *testing.T) {
	t.Parallel()
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user1"},
		TenantID:         "test-tenant",
	}
	validator := &mockTokenValidator{claims: claims}
	proxy, clientRemote, backendRemote := newAuthTestProxy(validator, claims)
	defer func() { _ = clientRemote.Close() }()
	defer func() { _ = backendRemote.Close() }()
	go drainConn(clientRemote)

	msg := protocol.ClientMessage{
		Type: MsgTypeAuth,
		Data: json.RawMessage(`{"token":""}`),
	}
	result, _ := proxy.interceptAuthRefresh(context.Background(), msg)

	if result != nil {
		t.Error("Expected nil result for empty token")
	}
}

func TestInterceptAuthRefresh_MissingData(t *testing.T) {
	t.Parallel()
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user1"},
		TenantID:         "test-tenant",
	}
	validator := &mockTokenValidator{claims: claims}
	proxy, clientRemote, backendRemote := newAuthTestProxy(validator, claims)
	defer func() { _ = clientRemote.Close() }()
	defer func() { _ = backendRemote.Close() }()
	go drainConn(clientRemote)

	msg := protocol.ClientMessage{
		Type: MsgTypeAuth,
		Data: json.RawMessage(`{}`),
	}
	result, _ := proxy.interceptAuthRefresh(context.Background(), msg)

	if result != nil {
		t.Error("Expected nil result for missing data")
	}
}

func TestInterceptAuthRefresh_NilValidator(t *testing.T) {
	t.Parallel()
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user1"},
		TenantID:         "test-tenant",
	}
	proxy, clientRemote, backendRemote := newAuthTestProxy(nil, claims)
	proxy.validator = nil
	defer func() { _ = clientRemote.Close() }()
	defer func() { _ = backendRemote.Close() }()
	go drainConn(clientRemote)

	msg := protocol.ClientMessage{
		Type: MsgTypeAuth,
		Data: json.RawMessage(`{"token":"valid-token"}`),
	}
	result, err := proxy.interceptAuthRefresh(context.Background(), msg)

	if err != nil {
		t.Fatalf("Expected nil error, got: %v", err)
	}
	if result != nil {
		t.Error("Expected nil result for nil validator")
	}
}

func TestInterceptAuthRefresh_ValidatorError(t *testing.T) {
	t.Parallel()
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user1"},
		TenantID:         "test-tenant",
	}
	validator := &mockTokenValidator{err: errors.New("unexpected error")}
	proxy, clientRemote, backendRemote := newAuthTestProxy(validator, claims)
	defer func() { _ = clientRemote.Close() }()
	defer func() { _ = backendRemote.Close() }()
	go drainConn(clientRemote)

	msg := protocol.ClientMessage{
		Type: MsgTypeAuth,
		Data: json.RawMessage(`{"token":"some-token"}`),
	}
	result, _ := proxy.interceptAuthRefresh(context.Background(), msg)

	if result != nil {
		t.Error("Expected nil result for validator error")
	}
}

// =============================================================================
// T022: Forced Unsubscription Tests
// =============================================================================

func TestForceUnsubscribeRevokedChannels_NoRevocation(t *testing.T) {
	t.Parallel()
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user1"},
		TenantID:         "test-tenant",
	}
	pc := NewPermissionChecker([]string{"*.trade", "*.liquidity"}, nil, nil)

	proxy := &Proxy{

		claims:             claims,
		tenantID:           "test-tenant",
		permissions:        pc,
		logger:             zerolog.Nop(),
		subscribedChannels: map[string]struct{}{"test-tenant.BTC.trade": {}, "test-tenant.ETH.liquidity": {}},
		authLimiter:        rate.NewLimiter(rate.Every(30*time.Second), 1),
		publishLimiter:     rate.NewLimiter(10, 100),
		maxPublishSize:     64 * 1024,
	}

	// New claims have same permissions — no revocation
	newClaims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user1"},
		TenantID:         "test-tenant",
	}

	revoked := proxy.forceUnsubscribeRevokedChannels(newClaims)

	if len(revoked) != 0 {
		t.Errorf("Expected 0 revoked channels, got %d: %v", len(revoked), revoked)
	}
	if len(proxy.subscribedChannels) != 2 {
		t.Errorf("Expected 2 subscribed channels, got %d", len(proxy.subscribedChannels))
	}
}

func TestForceUnsubscribeRevokedChannels_PartialRevocation(t *testing.T) {
	t.Parallel()
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user1"},
		TenantID:         "test-tenant",
	}
	// Initial permissions: trade + liquidity
	pc := NewPermissionChecker([]string{"*.trade", "*.liquidity"}, nil, nil)

	clientConn, clientRemote := net.Pipe()
	backendConn, backendRemote := net.Pipe()
	defer func() { _ = clientRemote.Close() }()
	defer func() { _ = backendRemote.Close() }()

	proxy := &Proxy{
		clientConn:  clientConn,
		backendConn: backendConn,

		claims:      claims,
		tenantID:    "test-tenant",
		permissions: pc,
		logger:      zerolog.Nop(),
		subscribedChannels: map[string]struct{}{
			"test-tenant.BTC.trade":     {},
			"test-tenant.ETH.liquidity": {},
		},
		authLimiter:    rate.NewLimiter(rate.Every(30*time.Second), 1),
		publishLimiter: rate.NewLimiter(10, 100),
		maxPublishSize: 64 * 1024,
	}

	// New permissions: only trade (liquidity revoked)
	// The permission checker uses the SAME patterns, so we need new claims
	// that cause CanSubscribe to return false for liquidity.
	// Since PermissionChecker uses pattern matching (not claims-based filtering for public),
	// we need to use a checker with reduced patterns.
	proxy.permissions = NewPermissionChecker([]string{"*.trade"}, nil, nil)

	newClaims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user1"},
		TenantID:         "test-tenant",
	}

	// Drain client and backend reads in background to prevent blocking
	go drainConn(clientRemote)
	go drainConn(backendRemote)

	revoked := proxy.forceUnsubscribeRevokedChannels(newClaims)

	if len(revoked) != 1 {
		t.Fatalf("Expected 1 revoked channel, got %d: %v", len(revoked), revoked)
	}
	if revoked[0] != "test-tenant.ETH.liquidity" {
		t.Errorf("Expected revoked channel test-tenant.ETH.liquidity, got %q", revoked[0])
	}
	// BTC.trade should still be tracked
	if _, ok := proxy.subscribedChannels["test-tenant.BTC.trade"]; !ok {
		t.Error("test-tenant.BTC.trade should still be in subscribedChannels")
	}
}

func TestForceUnsubscribeRevokedChannels_AllRevoked(t *testing.T) {
	t.Parallel()

	clientConn, clientRemote := net.Pipe()
	backendConn, backendRemote := net.Pipe()
	defer func() { _ = clientRemote.Close() }()
	defer func() { _ = backendRemote.Close() }()

	// No patterns allowed
	pc := NewPermissionChecker(nil, nil, nil)

	proxy := &Proxy{
		clientConn:  clientConn,
		backendConn: backendConn,

		claims: &auth.Claims{
			RegisteredClaims: jwt.RegisteredClaims{Subject: "user1"},
			TenantID:         "test-tenant",
		},
		tenantID:    "test-tenant",
		permissions: pc,
		logger:      zerolog.Nop(),
		subscribedChannels: map[string]struct{}{
			"test-tenant.BTC.trade":     {},
			"test-tenant.ETH.liquidity": {},
		},
		authLimiter:    rate.NewLimiter(rate.Every(30*time.Second), 1),
		publishLimiter: rate.NewLimiter(10, 100),
		maxPublishSize: 64 * 1024,
	}

	// Drain connections
	go drainConn(clientRemote)
	go drainConn(backendRemote)

	newClaims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user1"},
		TenantID:         "test-tenant",
	}

	revoked := proxy.forceUnsubscribeRevokedChannels(newClaims)

	if len(revoked) != 2 {
		t.Fatalf("Expected 2 revoked channels, got %d: %v", len(revoked), revoked)
	}
	if len(proxy.subscribedChannels) != 0 {
		t.Errorf("Expected 0 subscribed channels, got %d", len(proxy.subscribedChannels))
	}
}

func TestForceUnsubscribeRevokedChannels_NoSubscriptions(t *testing.T) {
	t.Parallel()

	proxy := &Proxy{

		claims: &auth.Claims{
			RegisteredClaims: jwt.RegisteredClaims{Subject: "user1"},
			TenantID:         "test-tenant",
		},
		tenantID:           "test-tenant",
		permissions:        NewPermissionChecker([]string{"*.trade"}, nil, nil),
		logger:             zerolog.Nop(),
		subscribedChannels: make(map[string]struct{}),
		authLimiter:        rate.NewLimiter(rate.Every(30*time.Second), 1),
		publishLimiter:     rate.NewLimiter(10, 100),
		maxPublishSize:     64 * 1024,
	}

	newClaims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user1"},
		TenantID:         "test-tenant",
	}

	revoked := proxy.forceUnsubscribeRevokedChannels(newClaims)

	if revoked != nil {
		t.Errorf("Expected nil revoked, got %v", revoked)
	}
}

// =============================================================================
// Benchmark
// =============================================================================

func BenchmarkInterceptAuthRefresh(b *testing.B) {
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user1",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		TenantID: "test-tenant",
	}
	newClaims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user1",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(2 * time.Hour)),
		},
		TenantID: "test-tenant",
	}
	validator := &mockTokenValidator{claims: newClaims}

	clientConn, clientRemote := net.Pipe()
	backendConn, backendRemote := net.Pipe()
	defer func() { _ = clientRemote.Close() }()
	defer func() { _ = backendRemote.Close() }()

	proxy := &Proxy{
		clientConn:  clientConn,
		backendConn: backendConn,

		claims:                claims,
		tenantID:              "test-tenant",
		permissions:           NewPermissionChecker([]string{"*.trade"}, nil, nil),
		validator:             validator,
		logger:                zerolog.Nop(),
		publishLimiter:        rate.NewLimiter(10, 100),
		maxPublishSize:        64 * 1024,
		authLimiter:           rate.NewLimiter(rate.Inf, 1), // No rate limit for benchmarks
		authValidationTimeout: 5 * time.Second,
		subscribedChannels:    make(map[string]struct{}),
	}

	msg := protocol.ClientMessage{
		Type: MsgTypeAuth,
		Data: json.RawMessage(`{"token":"valid-jwt-token"}`),
	}

	// Drain client reads
	go drainConn(clientRemote)

	for b.Loop() {
		_, _ = proxy.interceptAuthRefresh(context.Background(), msg)
	}
}
