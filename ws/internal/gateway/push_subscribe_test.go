package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pushv1 "github.com/klurvio/sukko/gen/proto/sukko/push/v1"
	"github.com/klurvio/sukko/internal/shared/license"
	"github.com/klurvio/sukko/internal/shared/platform"
)

// mockPushForwarder implements PushForwarder for testing.
type mockPushForwarder struct {
	registerResp   *pushv1.RegisterDeviceResponse
	registerErr    error
	unregisterResp *pushv1.UnregisterDeviceResponse
	unregisterErr  error
	vapidResp      *pushv1.GetVAPIDKeyResponse
	vapidErr       error

	// Captured requests for assertion
	lastRegisterReq   *pushv1.RegisterDeviceRequest
	lastUnregisterReq *pushv1.UnregisterDeviceRequest
	lastVAPIDReq      *pushv1.GetVAPIDKeyRequest
}

func (m *mockPushForwarder) RegisterDevice(_ context.Context, req *pushv1.RegisterDeviceRequest) (*pushv1.RegisterDeviceResponse, error) {
	m.lastRegisterReq = req
	return m.registerResp, m.registerErr
}

func (m *mockPushForwarder) UnregisterDevice(_ context.Context, req *pushv1.UnregisterDeviceRequest) (*pushv1.UnregisterDeviceResponse, error) {
	m.lastUnregisterReq = req
	return m.unregisterResp, m.unregisterErr
}

func (m *mockPushForwarder) GetVAPIDKey(_ context.Context, req *pushv1.GetVAPIDKeyRequest) (*pushv1.GetVAPIDKeyResponse, error) {
	m.lastVAPIDReq = req
	return m.vapidResp, m.vapidErr
}

// pushTestGateway creates a minimal Gateway for push handler testing with auth disabled.
func pushTestGateway(t *testing.T, mock PushForwarder) *Gateway {
	t.Helper()
	return &Gateway{
		config: &platform.GatewayConfig{
			AuthConfig:      platform.AuthConfig{AuthEnabled: false},
			DefaultTenantID: "test-tenant",
			MaxPublishSize:  65536,
		},
		logger:     testLogger(),
		pushClient: mock,
	}
}

func TestHandlePushSubscribe_WebSuccess(t *testing.T) {
	t.Parallel()

	mock := &mockPushForwarder{
		registerResp: &pushv1.RegisterDeviceResponse{DeviceId: 42},
	}
	gw := pushTestGateway(t, mock)

	body := `{
		"platform": "web",
		"endpoint": "https://fcm.googleapis.com/fcm/send/abc123",
		"p256dh_key": "BNcRdreALRFXTkOOUHK1EtK2w...",
		"auth_secret": "tBHItJI5svbpC7htfGg...",
		"channels": ["test-tenant.alerts"]
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/push/subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	gw.HandlePushSubscribe(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if deviceID, ok := resp["device_id"].(float64); !ok || int64(deviceID) != 42 {
		t.Errorf("device_id = %v, want 42", resp["device_id"])
	}

	// Verify forwarded request
	if mock.lastRegisterReq == nil {
		t.Fatal("RegisterDevice was not called")
	}
	if mock.lastRegisterReq.GetPlatform() != "web" {
		t.Errorf("platform = %q, want %q", mock.lastRegisterReq.GetPlatform(), "web")
	}
	if mock.lastRegisterReq.GetTenantId() != "test-tenant" {
		t.Errorf("tenant_id = %q, want %q", mock.lastRegisterReq.GetTenantId(), "test-tenant")
	}
}

func TestHandlePushSubscribe_AndroidSuccess(t *testing.T) {
	t.Parallel()

	mock := &mockPushForwarder{
		registerResp: &pushv1.RegisterDeviceResponse{DeviceId: 99},
	}
	gw := pushTestGateway(t, mock)

	body := `{
		"platform": "android",
		"token": "fcm-token-abc123",
		"channels": ["test-tenant.notifications"]
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/push/subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	gw.HandlePushSubscribe(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if deviceID, ok := resp["device_id"].(float64); !ok || int64(deviceID) != 99 {
		t.Errorf("device_id = %v, want 99", resp["device_id"])
	}
}

func TestHandlePushSubscribe_IOSSuccess(t *testing.T) {
	t.Parallel()

	mock := &mockPushForwarder{
		registerResp: &pushv1.RegisterDeviceResponse{DeviceId: 77},
	}
	gw := pushTestGateway(t, mock)

	body := `{
		"platform": "ios",
		"token": "apns-device-token-xyz",
		"channels": ["test-tenant.updates"]
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/push/subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	gw.HandlePushSubscribe(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

func TestHandlePushSubscribe_InvalidPlatform(t *testing.T) {
	t.Parallel()

	gw := pushTestGateway(t, nil)

	body := `{"platform": "blackberry", "channels": ["test-tenant.ch"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/push/subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	gw.HandlePushSubscribe(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	assertErrorCode(t, rec, "INVALID_REQUEST")
}

func TestHandlePushSubscribe_MissingChannels(t *testing.T) {
	t.Parallel()

	gw := pushTestGateway(t, nil)

	body := `{"platform": "web", "endpoint": "https://example.com", "p256dh_key": "key", "auth_secret": "sec", "channels": []}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/push/subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	gw.HandlePushSubscribe(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	assertErrorCode(t, rec, "INVALID_REQUEST")
}

func TestHandlePushSubscribe_InvalidTenantPrefix(t *testing.T) {
	t.Parallel()

	gw := pushTestGateway(t, nil)

	body := `{
		"platform": "web",
		"endpoint": "https://example.com",
		"p256dh_key": "key",
		"auth_secret": "sec",
		"channels": ["wrong-tenant.alerts"]
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/push/subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	gw.HandlePushSubscribe(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	assertErrorCode(t, rec, "INVALID_REQUEST")
}

func TestHandlePushSubscribe_MissingWebFields(t *testing.T) {
	t.Parallel()

	gw := pushTestGateway(t, nil)

	// Web platform without endpoint
	body := `{"platform": "web", "p256dh_key": "key", "auth_secret": "sec", "channels": ["test-tenant.ch"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/push/subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	gw.HandlePushSubscribe(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	assertErrorCode(t, rec, "INVALID_REQUEST")
}

func TestHandlePushSubscribe_MissingAndroidToken(t *testing.T) {
	t.Parallel()

	gw := pushTestGateway(t, nil)

	body := `{"platform": "android", "channels": ["test-tenant.ch"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/push/subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	gw.HandlePushSubscribe(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	assertErrorCode(t, rec, "INVALID_REQUEST")
}

func TestHandlePushSubscribe_NoPushClient(t *testing.T) {
	t.Parallel()

	gw := &Gateway{
		config: &platform.GatewayConfig{
			AuthConfig:      platform.AuthConfig{AuthEnabled: false},
			DefaultTenantID: "test-tenant",
			MaxPublishSize:  65536,
		},
		logger:     testLogger(),
		pushClient: nil,
	}

	body := `{
		"platform": "web",
		"endpoint": "https://example.com",
		"p256dh_key": "key",
		"auth_secret": "sec",
		"channels": ["test-tenant.ch"]
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/push/subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	gw.HandlePushSubscribe(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestHandlePushSubscribe_InvalidJSON(t *testing.T) {
	t.Parallel()

	gw := pushTestGateway(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/push/subscribe", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	gw.HandlePushSubscribe(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	assertErrorCode(t, rec, "INVALID_REQUEST")
}

func TestHandlePushUnsubscribe_Success(t *testing.T) {
	t.Parallel()

	mock := &mockPushForwarder{
		unregisterResp: &pushv1.UnregisterDeviceResponse{Success: true},
	}
	gw := pushTestGateway(t, mock)

	body := `{"device_id": 42}`
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/push/subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	gw.HandlePushUnsubscribe(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if success, ok := resp["success"].(bool); !ok || !success {
		t.Errorf("success = %v, want true", resp["success"])
	}

	// Verify forwarded request
	if mock.lastUnregisterReq == nil {
		t.Fatal("UnregisterDevice was not called")
	}
	if mock.lastUnregisterReq.GetTenantId() != "test-tenant" {
		t.Errorf("tenant_id = %q, want %q", mock.lastUnregisterReq.GetTenantId(), "test-tenant")
	}
	if mock.lastUnregisterReq.GetDeviceId() != 42 {
		t.Errorf("device_id = %d, want 42", mock.lastUnregisterReq.GetDeviceId())
	}
}

func TestHandlePushUnsubscribe_MissingDeviceID(t *testing.T) {
	t.Parallel()

	gw := pushTestGateway(t, nil)

	body := `{}`
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/push/subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	gw.HandlePushUnsubscribe(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	assertErrorCode(t, rec, "INVALID_REQUEST")
}

func TestHandlePushUnsubscribe_NoPushClient(t *testing.T) {
	t.Parallel()

	gw := &Gateway{
		config: &platform.GatewayConfig{
			AuthConfig:      platform.AuthConfig{AuthEnabled: false},
			DefaultTenantID: "test-tenant",
			MaxPublishSize:  65536,
		},
		logger:     testLogger(),
		pushClient: nil,
	}

	body := `{"device_id": 1}`
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/push/subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	gw.HandlePushUnsubscribe(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestHandlePushSubscribe_EditionGate_Community(t *testing.T) {
	t.Parallel()

	// Community edition should block Web Push
	mgr := license.NewTestManager(license.Community)
	pushGate := RequireFeature(mgr, license.PushNotifications)

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := pushGate(inner)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/push/subscribe", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	assertErrorCode(t, rec, "EDITION_LIMIT")
}

func TestHandlePushSubscribe_EditionGate_Pro(t *testing.T) {
	t.Parallel()

	// Pro edition should also block Web Push (Enterprise only)
	mgr := license.NewTestManager(license.Pro)
	pushGate := RequireFeature(mgr, license.PushNotifications)

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := pushGate(inner)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/push/subscribe", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d (Pro should not have PushNotifications)", rec.Code, http.StatusForbidden)
	}
}

func TestHandlePushSubscribe_EditionGate_Enterprise(t *testing.T) {
	t.Parallel()

	// Enterprise edition should allow Web Push
	mgr := license.NewTestManager(license.Enterprise)
	pushGate := RequireFeature(mgr, license.PushNotifications)

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := pushGate(inner)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/push/subscribe", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
