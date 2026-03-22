package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAdminClient_Tenants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		serverStatus   int
		serverResponse map[string]any
		call           func(c *AdminClient) (map[string]any, error)
		wantMethod     string
		wantPath       string
		wantBody       map[string]any
		wantErr        bool
		wantErrContain string
		wantSentinel   error
	}{
		{
			name:         "CreateTenant sends POST /api/v1/tenants with body",
			serverStatus: http.StatusCreated,
			serverResponse: map[string]any{
				"id":   "tenant-1",
				"name": "Test Tenant",
			},
			call: func(c *AdminClient) (map[string]any, error) {
				return c.CreateTenant(map[string]any{
					"name": "Test Tenant",
					"tier": "standard",
				})
			},
			wantMethod: "POST",
			wantPath:   "/api/v1/tenants",
			wantBody: map[string]any{
				"name": "Test Tenant",
				"tier": "standard",
			},
		},
		{
			name:         "GetTenant sends GET /api/v1/tenants/{id}",
			serverStatus: http.StatusOK,
			serverResponse: map[string]any{
				"id":     "tenant-42",
				"name":   "My Tenant",
				"status": "active",
			},
			call: func(c *AdminClient) (map[string]any, error) {
				return c.GetTenant("tenant-42")
			},
			wantMethod: "GET",
			wantPath:   "/api/v1/tenants/tenant-42",
		},
		{
			name:         "ListTenants sends GET /api/v1/tenants with query params",
			serverStatus: http.StatusOK,
			serverResponse: map[string]any{
				"tenants": []any{},
				"total":   float64(0),
			},
			call: func(c *AdminClient) (map[string]any, error) {
				return c.ListTenants(map[string]string{
					"status": "active",
				})
			},
			wantMethod: "GET",
			wantPath:   "/api/v1/tenants",
		},
		{
			name:         "SuspendTenant sends POST /api/v1/tenants/{id}/suspend",
			serverStatus: http.StatusOK,
			serverResponse: map[string]any{
				"id":     "tenant-99",
				"status": "suspended",
			},
			call: func(c *AdminClient) (map[string]any, error) {
				return c.SuspendTenant("tenant-99")
			},
			wantMethod: "POST",
			wantPath:   "/api/v1/tenants/tenant-99/suspend",
		},
		{
			name:         "API error HTTP 400 returns error",
			serverStatus: http.StatusBadRequest,
			call: func(c *AdminClient) (map[string]any, error) {
				return c.CreateTenant(map[string]any{"name": ""})
			},
			wantMethod:   "POST",
			wantPath:     "/api/v1/tenants",
			wantErr:      true,
			wantSentinel: ErrAPIBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotMethod, gotPath string
			var gotBody map[string]any

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				gotPath = r.URL.Path

				if r.Body != nil && r.ContentLength > 0 {
					if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
						t.Errorf("failed to decode request body: %v", err)
					}
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.serverStatus)

				if tt.serverResponse != nil {
					if err := json.NewEncoder(w).Encode(tt.serverResponse); err != nil {
						t.Errorf("failed to encode response: %v", err)
					}
				} else if tt.wantErr {
					_, _ = w.Write([]byte(`{"error":"bad request"}`))
				}
			}))
			defer srv.Close()

			client, _ := New(Config{
				BaseURL: srv.URL,
				Token:   "test-token",
			})

			result, err := tt.call(client)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error but got nil")
				}
				if tt.wantSentinel != nil && !errors.Is(err, tt.wantSentinel) {
					t.Errorf("error %q is not %q", err.Error(), tt.wantSentinel)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if gotMethod != tt.wantMethod {
				t.Errorf("method = %q, want %q", gotMethod, tt.wantMethod)
			}

			if gotPath != tt.wantPath {
				t.Errorf("path = %q, want %q", gotPath, tt.wantPath)
			}

			if tt.wantBody != nil {
				for k, wantVal := range tt.wantBody {
					gotVal, ok := gotBody[k]
					if !ok {
						t.Errorf("request body missing key %q", k)
						continue
					}
					if fmt.Sprintf("%v", gotVal) != fmt.Sprintf("%v", wantVal) {
						t.Errorf("request body[%q] = %v, want %v", k, gotVal, wantVal)
					}
				}
			}

			if tt.serverResponse != nil {
				for k, wantVal := range tt.serverResponse {
					gotVal, ok := result[k]
					if !ok {
						t.Errorf("response missing key %q", k)
						continue
					}
					if fmt.Sprintf("%v", gotVal) != fmt.Sprintf("%v", wantVal) {
						t.Errorf("response[%q] = %v, want %v", k, gotVal, wantVal)
					}
				}
			}
		})
	}
}

func TestAdminClient_Keys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		serverStatus   int
		serverResponse map[string]any
		call           func(c *AdminClient) (map[string]any, error)
		wantMethod     string
		wantPath       string
	}{
		{
			name:         "CreateKey sends POST /api/v1/tenants/{id}/keys",
			serverStatus: http.StatusCreated,
			serverResponse: map[string]any{
				"key_id": "key-abc",
			},
			call: func(c *AdminClient) (map[string]any, error) {
				return c.CreateKey("tenant-1", map[string]any{
					"label": "my-key",
				})
			},
			wantMethod: "POST",
			wantPath:   "/api/v1/tenants/tenant-1/keys",
		},
		{
			name:         "ListKeys sends GET /api/v1/tenants/{id}/keys",
			serverStatus: http.StatusOK,
			serverResponse: map[string]any{
				"keys": []any{},
			},
			call: func(c *AdminClient) (map[string]any, error) {
				return c.ListKeys("tenant-5")
			},
			wantMethod: "GET",
			wantPath:   "/api/v1/tenants/tenant-5/keys",
		},
		{
			name:         "RevokeKey sends DELETE /api/v1/tenants/{id}/keys/{keyId}",
			serverStatus: http.StatusOK,
			serverResponse: map[string]any{
				"revoked": true,
			},
			call: func(c *AdminClient) (map[string]any, error) {
				return c.RevokeKey("tenant-5", "key-xyz")
			},
			wantMethod: "DELETE",
			wantPath:   "/api/v1/tenants/tenant-5/keys/key-xyz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotMethod, gotPath string

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				gotPath = r.URL.Path

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.serverStatus)
				if tt.serverResponse != nil {
					if err := json.NewEncoder(w).Encode(tt.serverResponse); err != nil {
						t.Errorf("failed to encode response: %v", err)
					}
				}
			}))
			defer srv.Close()

			client, _ := New(Config{
				BaseURL: srv.URL,
				Token:   "test-token",
			})

			result, err := tt.call(client)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if gotMethod != tt.wantMethod {
				t.Errorf("method = %q, want %q", gotMethod, tt.wantMethod)
			}

			if gotPath != tt.wantPath {
				t.Errorf("path = %q, want %q", gotPath, tt.wantPath)
			}

			if result == nil {
				t.Fatal("expected non-nil result")
			}
		})
	}
}

func TestAdminClient_BearerToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		token     string
		wantAuth  bool
		wantValue string
	}{
		{
			name:      "token set includes Authorization Bearer header",
			token:     "my-secret-token",
			wantAuth:  true,
			wantValue: "Bearer my-secret-token",
		},
		{
			name:     "no token omits Authorization header",
			token:    "",
			wantAuth: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotAuth string
			var hasAuth bool

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAuth = r.Header.Get("Authorization")
				hasAuth = r.Header.Get("Authorization") != ""

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"ok":true}`))
			}))
			defer srv.Close()

			client, _ := New(Config{
				BaseURL: srv.URL,
				Token:   tt.token,
			})

			_, err := client.GetTenant("test-id")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if hasAuth != tt.wantAuth {
				t.Errorf("Authorization header present = %v, want %v", hasAuth, tt.wantAuth)
			}

			if tt.wantAuth && gotAuth != tt.wantValue {
				t.Errorf("Authorization = %q, want %q", gotAuth, tt.wantValue)
			}
		})
	}
}

func TestAdminClient_ErrorResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		serverStatus int
		serverBody   string
		wantSentinel error
	}{
		{
			name:         "HTTP 400 error",
			serverStatus: http.StatusBadRequest,
			serverBody:   `{"error":"invalid input"}`,
			wantSentinel: ErrAPIBadRequest,
		},
		{
			name:         "HTTP 500 error",
			serverStatus: http.StatusInternalServerError,
			serverBody:   `{"error":"internal failure"}`,
			wantSentinel: ErrAPIInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.serverStatus)
				_, _ = w.Write([]byte(tt.serverBody))
			}))
			defer srv.Close()

			client, _ := New(Config{
				BaseURL: srv.URL,
				Token:   "test-token",
			})

			_, err := client.GetTenant("any-id")
			if err == nil {
				t.Fatal("expected error but got nil")
			}

			if !errors.Is(err, tt.wantSentinel) {
				t.Errorf("error %q is not %q", err.Error(), tt.wantSentinel)
			}
		})
	}
}

func TestAdminClient_EmptyBaseURL(t *testing.T) {
	t.Parallel()

	_, err := New(Config{BaseURL: ""})
	if err == nil {
		t.Fatal("expected error for empty BaseURL, got nil")
	}
}

func TestAdminClient_EmptyTenantID(t *testing.T) {
	t.Parallel()

	c, _ := New(Config{BaseURL: "http://localhost:9999"})

	_, err := c.GetTenant("")
	if err == nil {
		t.Fatal("expected error for empty tenantID, got nil")
	}
}

func TestAdminClient_EmptyKeyID(t *testing.T) {
	t.Parallel()

	c, _ := New(Config{BaseURL: "http://localhost:9999"})

	_, err := c.RevokeKey("tenant-1", "")
	if err == nil {
		t.Fatal("expected error for empty keyID, got nil")
	}
}

func TestAdminClient_DefaultTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		timeout     time.Duration
		wantTimeout time.Duration
	}{
		{
			name:        "zero timeout defaults to 30s",
			timeout:     0,
			wantTimeout: 30 * time.Second,
		},
		{
			name:        "explicit timeout is respected",
			timeout:     10 * time.Second,
			wantTimeout: 10 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, _ := New(Config{
				BaseURL: "http://localhost:9999",
				Timeout: tt.timeout,
			})

			if client.httpClient.Timeout != tt.wantTimeout {
				t.Errorf("timeout = %v, want %v", client.httpClient.Timeout, tt.wantTimeout)
			}
		})
	}
}
