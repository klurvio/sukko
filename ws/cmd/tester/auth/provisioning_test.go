package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestProvisioningClient_CreateTenant(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/tenants" {
			t.Errorf("path = %q, want /api/v1/tenants", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-admin-token" {
			t.Errorf("auth = %q, want Bearer test-admin-token", auth)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q, want application/json", ct)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	client := NewProvisioningClient(srv.URL, "test-admin-token", zerolog.Nop())
	err := client.CreateTenant(context.Background(), "test-t1", "Test Tenant")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
}

func TestProvisioningClient_DeleteTenant(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %q, want DELETE", r.Method)
		}
		if !strings.HasPrefix(r.URL.Path, "/api/v1/tenants/test-t1") {
			t.Errorf("path = %q, want prefix /api/v1/tenants/test-t1", r.URL.Path)
		}
		if r.URL.Query().Get("force") != "true" {
			t.Error("expected force=true query parameter")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewProvisioningClient(srv.URL, "test-admin-token", zerolog.Nop())
	err := client.DeleteTenant(context.Background(), "test-t1")
	if err != nil {
		t.Fatalf("DeleteTenant: %v", err)
	}
}

func TestProvisioningClient_RegisterKey(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/tenants/test-t1/keys" {
			t.Errorf("path = %q, want /api/v1/tenants/test-t1/keys", r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	client := NewProvisioningClient(srv.URL, "test-admin-token", zerolog.Nop())
	expires := time.Now().Add(24 * time.Hour)
	err := client.RegisterKey(context.Background(), "test-t1", RegisterKeyRequest{
		KeyID:     "tester-abcd1234",
		Algorithm: "ES256",
		PublicKey: "-----BEGIN PUBLIC KEY-----\ntest\n-----END PUBLIC KEY-----\n",
		ExpiresAt: &expires,
	})
	if err != nil {
		t.Fatalf("RegisterKey: %v", err)
	}
}

func TestProvisioningClient_RevokeKey(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %q, want DELETE", r.Method)
		}
		if r.URL.Path != "/api/v1/tenants/test-t1/keys/tester-abcd1234" {
			t.Errorf("path = %q, want /api/v1/tenants/test-t1/keys/tester-abcd1234", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewProvisioningClient(srv.URL, "test-admin-token", zerolog.Nop())
	err := client.RevokeKey(context.Background(), "test-t1", "tester-abcd1234")
	if err != nil {
		t.Fatalf("RevokeKey: %v", err)
	}
}

func TestProvisioningClient_HTTPError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":"INTERNAL","message":"boom"}`))
	}))
	defer srv.Close()

	client := NewProvisioningClient(srv.URL, "token", zerolog.Nop())

	err := client.CreateTenant(context.Background(), "t1", "T1")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("error = %q, want HTTP 500", err.Error())
	}
}

func TestProvisioningClient_GetTenant(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer my-jwt" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := NewProvisioningClient(srv.URL, "admin-token", zerolog.Nop())

	// With correct JWT
	status, err := client.GetTenant(context.Background(), "t1", "my-jwt")
	if err != nil {
		t.Fatalf("GetTenant: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}

	// With wrong JWT
	status, err = client.GetTenant(context.Background(), "t1", "wrong-jwt")
	if err != nil {
		t.Fatalf("GetTenant: %v", err)
	}
	if status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", status)
	}
}

func TestProvisioningClient_SetChannelRules(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %q, want PUT", r.Method)
		}
		if r.URL.Path != "/api/v1/tenants/test-t1/channel-rules" {
			t.Errorf("path = %q, want /api/v1/tenants/test-t1/channel-rules", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q, want application/json", ct)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-admin-token" {
			t.Errorf("auth = %q, want Bearer test-admin-token", auth)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewProvisioningClient(srv.URL, "test-admin-token", zerolog.Nop())
	err := client.SetChannelRules(context.Background(), "test-t1", map[string]any{
		"public": []string{"general.*"},
	})
	if err != nil {
		t.Fatalf("SetChannelRules: %v", err)
	}
}

func TestProvisioningClient_SetRoutingRules(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %q, want PUT", r.Method)
		}
		if r.URL.Path != "/api/v1/tenants/test-t1/routing-rules" {
			t.Errorf("path = %q, want /api/v1/tenants/test-t1/routing-rules", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewProvisioningClient(srv.URL, "test-admin-token", zerolog.Nop())
	err := client.SetRoutingRules(context.Background(), "test-t1", []map[string]any{
		{"pattern": "*.*", "topic_suffix": "test"},
	})
	if err != nil {
		t.Fatalf("SetRoutingRules: %v", err)
	}
}

func TestProvisioningClient_DeleteRoutingRules(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %q, want DELETE", r.Method)
		}
		if r.URL.Path != "/api/v1/tenants/test-t1/routing-rules" {
			t.Errorf("path = %q, want /api/v1/tenants/test-t1/routing-rules", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewProvisioningClient(srv.URL, "test-admin-token", zerolog.Nop())
	err := client.DeleteRoutingRules(context.Background(), "test-t1")
	if err != nil {
		t.Fatalf("DeleteRoutingRules: %v", err)
	}
}

func TestProvisioningClient_DeleteRoutingRules_NotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewProvisioningClient(srv.URL, "test-admin-token", zerolog.Nop())
	err := client.DeleteRoutingRules(context.Background(), "test-t1")
	if err != nil {
		t.Fatalf("DeleteRoutingRules with 404 should not error, got: %v", err)
	}
}
