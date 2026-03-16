// Package client provides a REST admin client for the provisioning API.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// AdminClient communicates with the provisioning REST API.
type AdminClient struct {
	baseURL    string
	httpClient *http.Client
	token      string
}

// Config holds AdminClient configuration.
type Config struct {
	BaseURL string
	Timeout time.Duration
	Token   string
}

// New creates a new AdminClient.
func New(cfg Config) *AdminClient {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &AdminClient{
		baseURL: cfg.BaseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		token: cfg.Token,
	}
}

// --- Tenants ---

// CreateTenant creates a new tenant via the provisioning API.
func (c *AdminClient) CreateTenant(req map[string]any) (map[string]any, error) {
	return c.doJSON("POST", "/api/v1/tenants", req)
}

// GetTenant retrieves a tenant by ID.
func (c *AdminClient) GetTenant(tenantID string) (map[string]any, error) {
	return c.doJSON("GET", "/api/v1/tenants/"+tenantID, nil)
}

// ListTenants lists tenants with optional filter parameters.
func (c *AdminClient) ListTenants(params map[string]string) (map[string]any, error) {
	path := "/api/v1/tenants" + encodeParams(params)
	return c.doJSON("GET", path, nil)
}

// UpdateTenant updates a tenant by ID.
func (c *AdminClient) UpdateTenant(tenantID string, req map[string]any) (map[string]any, error) {
	return c.doJSON("PATCH", "/api/v1/tenants/"+tenantID, req)
}

// SuspendTenant suspends a tenant by ID.
func (c *AdminClient) SuspendTenant(tenantID string) (map[string]any, error) {
	return c.doJSON("POST", "/api/v1/tenants/"+tenantID+"/suspend", nil)
}

// ReactivateTenant reactivates a suspended tenant.
func (c *AdminClient) ReactivateTenant(tenantID string) (map[string]any, error) {
	return c.doJSON("POST", "/api/v1/tenants/"+tenantID+"/reactivate", nil)
}

// DeprovisionTenant deprovisions a tenant by ID.
func (c *AdminClient) DeprovisionTenant(tenantID string) (map[string]any, error) {
	return c.doJSON("DELETE", "/api/v1/tenants/"+tenantID, nil)
}

// --- Keys ---

// CreateKey registers a new public key for a tenant.
func (c *AdminClient) CreateKey(tenantID string, req map[string]any) (map[string]any, error) {
	return c.doJSON("POST", "/api/v1/tenants/"+tenantID+"/keys", req)
}

// ListKeys lists all keys for a tenant.
func (c *AdminClient) ListKeys(tenantID string) (map[string]any, error) {
	return c.doJSON("GET", "/api/v1/tenants/"+tenantID+"/keys", nil)
}

// RevokeKey revokes a key by tenant and key ID.
func (c *AdminClient) RevokeKey(tenantID, keyID string) (map[string]any, error) {
	return c.doJSON("POST", "/api/v1/tenants/"+tenantID+"/keys/"+keyID+"/revoke", nil)
}

// --- Routing Rules ---

// GetRoutingRules retrieves routing rules for a tenant.
func (c *AdminClient) GetRoutingRules(tenantID string) (map[string]any, error) {
	return c.doJSON("GET", "/api/v1/tenants/"+tenantID+"/routing-rules", nil)
}

// SetRoutingRules sets routing rules for a tenant.
func (c *AdminClient) SetRoutingRules(tenantID string, body any) (map[string]any, error) {
	return c.doJSON("PUT", "/api/v1/tenants/"+tenantID+"/routing-rules", body)
}

// DeleteRoutingRules deletes routing rules for a tenant.
func (c *AdminClient) DeleteRoutingRules(tenantID string) (map[string]any, error) {
	return c.doJSON("DELETE", "/api/v1/tenants/"+tenantID+"/routing-rules", nil)
}

// --- Quotas ---

// GetQuota retrieves the quota for a tenant.
func (c *AdminClient) GetQuota(tenantID string) (map[string]any, error) {
	return c.doJSON("GET", "/api/v1/tenants/"+tenantID+"/quota", nil)
}

// UpdateQuota updates the quota for a tenant.
func (c *AdminClient) UpdateQuota(tenantID string, req map[string]any) (map[string]any, error) {
	return c.doJSON("PUT", "/api/v1/tenants/"+tenantID+"/quota", req)
}

// --- OIDC ---

// GetOIDCConfig retrieves the OIDC configuration for a tenant.
func (c *AdminClient) GetOIDCConfig(tenantID string) (map[string]any, error) {
	return c.doJSON("GET", "/api/v1/tenants/"+tenantID+"/oidc", nil)
}

// CreateOIDCConfig creates an OIDC configuration for a tenant.
func (c *AdminClient) CreateOIDCConfig(tenantID string, req map[string]any) (map[string]any, error) {
	return c.doJSON("POST", "/api/v1/tenants/"+tenantID+"/oidc", req)
}

// UpdateOIDCConfig updates the OIDC configuration for a tenant.
func (c *AdminClient) UpdateOIDCConfig(tenantID string, req map[string]any) (map[string]any, error) {
	return c.doJSON("PUT", "/api/v1/tenants/"+tenantID+"/oidc", req)
}

// DeleteOIDCConfig deletes the OIDC configuration for a tenant.
func (c *AdminClient) DeleteOIDCConfig(tenantID string) (map[string]any, error) {
	return c.doJSON("DELETE", "/api/v1/tenants/"+tenantID+"/oidc", nil)
}

// --- Channel Rules ---

// GetChannelRules retrieves channel rules for a tenant.
func (c *AdminClient) GetChannelRules(tenantID string) (map[string]any, error) {
	return c.doJSON("GET", "/api/v1/tenants/"+tenantID+"/channel-rules", nil)
}

// SetChannelRules sets channel rules for a tenant.
func (c *AdminClient) SetChannelRules(tenantID string, req map[string]any) (map[string]any, error) {
	return c.doJSON("PUT", "/api/v1/tenants/"+tenantID+"/channel-rules", req)
}

// DeleteChannelRules deletes channel rules for a tenant.
func (c *AdminClient) DeleteChannelRules(tenantID string) (map[string]any, error) {
	return c.doJSON("DELETE", "/api/v1/tenants/"+tenantID+"/channel-rules", nil)
}

// --- Test Access ---

// TestAccess tests channel access for a tenant with the given parameters.
func (c *AdminClient) TestAccess(tenantID string, req map[string]any) (map[string]any, error) {
	return c.doJSON("POST", "/api/v1/tenants/"+tenantID+"/test-access", req)
}

// --- Internal ---

func (c *AdminClient) doJSON(method, path string, body any) (map[string]any, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(context.Background(), method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	var result map[string]any
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &result); err != nil {
			return nil, fmt.Errorf("unmarshal response: %w", err)
		}
	}

	return result, nil
}

func encodeParams(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	v := url.Values{}
	for key, val := range params {
		v.Set(key, val)
	}
	return "?" + v.Encode()
}
