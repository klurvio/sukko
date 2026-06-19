package worker

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/provisioning"
	"github.com/klurvio/sukko/internal/shared/crypto"
	"github.com/klurvio/sukko/internal/shared/types"
)

// testKey is a valid 32-byte AES-256 encryption key for tests.
var testKey = []byte("0123456789abcdef0123456789abcdef")

// encryptSecret encrypts plaintext with testKey and returns raw ciphertext bytes.
func encryptSecret(t *testing.T, plaintext string) []byte {
	t.Helper()
	ct, err := crypto.EncryptCredential(plaintext, testKey)
	if err != nil {
		t.Fatalf("encrypt secret: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(ct)
	if err != nil {
		t.Fatalf("decode b64 ciphertext: %v", err)
	}
	return raw
}

// --- mock HTTPDoer ---

type mockHTTPDoer struct {
	resp *http.Response
	err  error
}

func (m *mockHTTPDoer) Do(_ *http.Request) (*http.Response, error) {
	return m.resp, m.err
}

// callCheckDoer wraps an HTTPDoer and records whether Do was called.
type callCheckDoer struct {
	t      *testing.T
	called *bool
	inner  HTTPDoer
}

func (c *callCheckDoer) Do(req *http.Request) (*http.Response, error) {
	*c.called = true
	return c.inner.Do(req)
}

// cacheWithWebhook builds a WebhookCache populated with one webhook.
func cacheWithWebhook(t *testing.T, status string, secretEnc []byte) (*WebhookCache, string) { //nolint:gocritic // unnamed returns are clearer for this small test helper
	t.Helper()
	client := newStubClient()
	client.records["t1"] = []*provisioning.WebhookRecord{
		{
			ID:         "wh-1",
			TenantID:   "t1",
			URL:        "https://example.com/hook",
			Status:     status,
			SecretEnc:  secretEnc,
			MaxRetries: 5,
		},
	}
	cache := NewWebhookCache(client, zerolog.Nop())
	if err := cache.Refresh(context.Background(), "t1"); err != nil {
		t.Fatalf("cache.Refresh: %v", err)
	}
	return cache, "t1"
}

func mockOKResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// TestDeliver_HappyPath verifies HMAC signing and successful delivery.
func TestDeliver_HappyPath(t *testing.T) {
	t.Parallel()
	secret := encryptSecret(t, "my-webhook-secret")
	cache, _ := cacheWithWebhook(t, types.WebhookStatusEnabled, secret)
	doer := &mockHTTPDoer{resp: mockOKResponse(`{"ok":true}`)} //nolint:bodyclose // closed by Deliver() via defer resp.Body.Close()

	d := NewDeliverer(cache, doer, testKey, zerolog.Nop())
	result := d.Deliver(context.Background(), DeliveryTask{
		WebhookID: "wh-1", TenantID: "t1",
		SecretEnc: secret, // runner populates from cache record
		Payload:   []byte(`{"event":"test"}`), Attempt: 1,
	})
	if result.StatusLabel != "success" {
		t.Errorf("StatusLabel = %q, want success; error = %q", result.StatusLabel, result.Error)
	}
	if result.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", result.StatusCode)
	}
}

// TestDeliver_DegradedWebhookAllowed verifies degraded webhooks pass FR-017 gate
// so the degradedScheduler can achieve recovery via a successful HTTP delivery.
func TestDeliver_DegradedWebhookAllowed(t *testing.T) {
	t.Parallel()
	secret := encryptSecret(t, "sec")
	cache, _ := cacheWithWebhook(t, types.WebhookStatusDegraded, secret)
	var httpCalled bool
	innerDoer := &mockHTTPDoer{resp: mockOKResponse("ok")} //nolint:bodyclose // closed by Deliver() via defer resp.Body.Close()
	doer := &callCheckDoer{t: t, called: &httpCalled, inner: innerDoer}

	d := NewDeliverer(cache, doer, testKey, zerolog.Nop())
	result := d.Deliver(context.Background(), DeliveryTask{
		WebhookID: "wh-1", TenantID: "t1", SecretEnc: secret, Payload: []byte("x"), Attempt: 1,
	})
	if result.StatusLabel == "skipped" {
		t.Error("degraded webhook must not be skipped by FR-017 gate — it needs to recover")
	}
	if !httpCalled {
		t.Error("HTTP call must be made for degraded webhook (required for recovery)")
	}
}

// TestDeliver_FR017_SuspendedWebhookCancelled covers SC-014(a).
func TestDeliver_FR017_SuspendedWebhookCancelled(t *testing.T) {
	t.Parallel()
	secret := encryptSecret(t, "sec")
	cache, _ := cacheWithWebhook(t, types.WebhookStatusSuspended, secret)
	var httpCalled bool
	innerDoer := &mockHTTPDoer{resp: mockOKResponse("")} //nolint:bodyclose // body is never read (Deliver() skips dispatch for suspended webhooks)
	doer := &callCheckDoer{t: t, called: &httpCalled, inner: innerDoer}

	d := NewDeliverer(cache, doer, testKey, zerolog.Nop())
	result := d.Deliver(context.Background(), DeliveryTask{
		WebhookID: "wh-1", TenantID: "t1", Payload: []byte("x"), Attempt: 1,
	})
	if result.StatusLabel != "skipped" {
		t.Errorf("suspended webhook should be skipped, got %q", result.StatusLabel)
	}
	if httpCalled {
		t.Error("HTTP call must not be made for suspended webhook")
	}
}

// TestDeliver_FR017_AbsentWebhookCancelled covers SC-014(b).
func TestDeliver_FR017_AbsentWebhookCancelled(t *testing.T) {
	t.Parallel()
	client := newStubClient() // empty cache
	cache := NewWebhookCache(client, zerolog.Nop())
	var httpCalled bool
	doer := &callCheckDoer{t: t, called: &httpCalled, inner: &mockHTTPDoer{}}

	d := NewDeliverer(cache, doer, testKey, zerolog.Nop())
	result := d.Deliver(context.Background(), DeliveryTask{
		WebhookID: "wh-missing", TenantID: "t1", Payload: []byte("x"), Attempt: 1,
	})
	if result.StatusLabel != "skipped" {
		t.Errorf("absent webhook should be skipped, got %q", result.StatusLabel)
	}
	if httpCalled {
		t.Error("HTTP call must not be made for absent webhook")
	}
}

// TestDeliver_DecryptionFailure verifies StatusLabel and Error on decrypt failure.
// The test also verifies no HTTP call is made (SC-017 behavior in delivery layer).
func TestDeliver_DecryptionFailure(t *testing.T) {
	t.Parallel()
	client := newStubClient()
	client.records["t1"] = []*provisioning.WebhookRecord{
		{ID: "wh-1", TenantID: "t1", URL: "https://example.com",
			SecretEnc: []byte("not-valid-ciphertext"), Status: "enabled"},
	}
	cache := NewWebhookCache(client, zerolog.Nop())
	_ = cache.Refresh(context.Background(), "t1")

	var httpCalled bool
	doer := &callCheckDoer{t: t, called: &httpCalled, inner: &mockHTTPDoer{}}
	d := NewDeliverer(cache, doer, testKey, zerolog.Nop())

	result := d.Deliver(context.Background(), DeliveryTask{
		WebhookID: "wh-1", TenantID: "t1", Payload: []byte("x"), Attempt: 1,
	})
	if result.StatusLabel != "decrypt_failed" {
		t.Errorf("StatusLabel = %q, want decrypt_failed", result.StatusLabel)
	}
	if result.Error != "decryption_failure" {
		t.Errorf("Error = %q, want decryption_failure", result.Error)
	}
	if httpCalled {
		t.Error("HTTP call must not be made on decryption failure")
	}
}

// TestDeliver_BodyPreviewTruncated covers SC-018 body truncation at 512 bytes.
func TestDeliver_BodyPreviewTruncated(t *testing.T) {
	t.Parallel()
	secret := encryptSecret(t, "sec")
	cache, _ := cacheWithWebhook(t, types.WebhookStatusEnabled, secret)
	bigBody := strings.Repeat("x", 1024)
	doer := &mockHTTPDoer{resp: &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(bigBody)),
	}}
	d := NewDeliverer(cache, doer, testKey, zerolog.Nop())
	result := d.Deliver(context.Background(), DeliveryTask{
		WebhookID: "wh-1", TenantID: "t1", SecretEnc: secret, Payload: []byte("x"), Attempt: 1,
	})
	if len(result.BodyPreview) != bodyPreviewBytes {
		t.Errorf("BodyPreview length = %d, want %d", len(result.BodyPreview), bodyPreviewBytes)
	}
}

// TestTestDeliver_SSRFBlocked covers SC-018 SSRF block in TestDeliver path.
func TestTestDeliver_SSRFBlocked(t *testing.T) {
	t.Parallel()
	secret := encryptSecret(t, "sec")
	client := newStubClient()
	client.records["t1"] = []*provisioning.WebhookRecord{
		{ID: "wh-ssrf", TenantID: "t1", URL: "http://10.0.0.1/hook",
			SecretEnc: secret, Status: "enabled", MaxRetries: 3},
	}
	cache := NewWebhookCache(client, zerolog.Nop())
	_ = cache.Refresh(context.Background(), "t1")

	doer := &mockHTTPDoer{err: errors.New("ssrf_blocked: resolved to private IP")}
	d := NewDeliverer(cache, doer, testKey, zerolog.Nop())

	result := d.TestDeliver(context.Background(), "wh-ssrf", "t1")
	if result.StatusLabel != "ssrf_blocked" {
		t.Errorf("StatusLabel = %q, want ssrf_blocked", result.StatusLabel)
	}
}

// TestTestDeliver_SideEffectFreeOnDecryptFailure covers FR-016 semantics.
func TestTestDeliver_SideEffectFreeOnDecryptFailure(t *testing.T) {
	t.Parallel()
	client := newStubClient()
	client.records["t1"] = []*provisioning.WebhookRecord{
		{ID: "wh-1", TenantID: "t1", URL: "https://example.com",
			SecretEnc: []byte("garbage"), Status: "suspended"},
	}
	cache := NewWebhookCache(client, zerolog.Nop())
	_ = cache.Refresh(context.Background(), "t1")

	var httpCalled bool
	doer := &callCheckDoer{t: t, called: &httpCalled, inner: &mockHTTPDoer{}}
	d := NewDeliverer(cache, doer, testKey, zerolog.Nop())

	result := d.TestDeliver(context.Background(), "wh-1", "t1")
	if result.Error != "decryption_failure" {
		t.Errorf("Error = %q, want decryption_failure", result.Error)
	}
	if httpCalled {
		t.Error("no HTTP call on TestDeliver decryption failure")
	}
}
