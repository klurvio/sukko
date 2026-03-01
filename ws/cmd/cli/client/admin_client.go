// Package client provides a REST admin client for the provisioning API.
package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

func (c *AdminClient) CreateTenant(req map[string]any) (map[string]any, error) {
	return c.doJSON("POST", "/api/v1/tenants", req)
}

func (c *AdminClient) GetTenant(tenantID string) (map[string]any, error) {
	return c.doJSON("GET", "/api/v1/tenants/"+tenantID, nil)
}

func (c *AdminClient) ListTenants(params map[string]string) (map[string]any, error) {
	path := "/api/v1/tenants"
	if len(params) > 0 {
		path += "?" + encodeParams(params)
	}
	return c.doJSON("GET", path, nil)
}

func (c *AdminClient) UpdateTenant(tenantID string, req map[string]any) (map[string]any, error) {
	return c.doJSON("PUT", "/api/v1/tenants/"+tenantID, req)
}

func (c *AdminClient) SuspendTenant(tenantID string) (map[string]any, error) {
	return c.doJSON("POST", "/api/v1/tenants/"+tenantID+"/suspend", nil)
}

func (c *AdminClient) ReactivateTenant(tenantID string) (map[string]any, error) {
	return c.doJSON("POST", "/api/v1/tenants/"+tenantID+"/reactivate", nil)
}

func (c *AdminClient) DeprovisionTenant(tenantID string) (map[string]any, error) {
	return c.doJSON("POST", "/api/v1/tenants/"+tenantID+"/deprovision", nil)
}

// --- Keys ---

func (c *AdminClient) CreateKey(tenantID string, req map[string]any) (map[string]any, error) {
	return c.doJSON("POST", "/api/v1/tenants/"+tenantID+"/keys", req)
}

func (c *AdminClient) ListKeys(tenantID string) (map[string]any, error) {
	return c.doJSON("GET", "/api/v1/tenants/"+tenantID+"/keys", nil)
}

func (c *AdminClient) RevokeKey(tenantID, keyID string) (map[string]any, error) {
	return c.doJSON("POST", "/api/v1/tenants/"+tenantID+"/keys/"+keyID+"/revoke", nil)
}

// --- Categories/Topics ---

func (c *AdminClient) CreateCategory(tenantID string, req map[string]any) (map[string]any, error) {
	return c.doJSON("POST", "/api/v1/tenants/"+tenantID+"/topics", req)
}

func (c *AdminClient) ListCategories(tenantID string) (map[string]any, error) {
	return c.doJSON("GET", "/api/v1/tenants/"+tenantID+"/topics", nil)
}

// --- Quotas ---

func (c *AdminClient) GetQuota(tenantID string) (map[string]any, error) {
	return c.doJSON("GET", "/api/v1/tenants/"+tenantID+"/quota", nil)
}

func (c *AdminClient) UpdateQuota(tenantID string, req map[string]any) (map[string]any, error) {
	return c.doJSON("PUT", "/api/v1/tenants/"+tenantID+"/quota", req)
}

// --- OIDC ---

func (c *AdminClient) GetOIDCConfig(tenantID string) (map[string]any, error) {
	return c.doJSON("GET", "/api/v1/tenants/"+tenantID+"/oidc", nil)
}

func (c *AdminClient) CreateOIDCConfig(tenantID string, req map[string]any) (map[string]any, error) {
	return c.doJSON("POST", "/api/v1/tenants/"+tenantID+"/oidc", req)
}

func (c *AdminClient) UpdateOIDCConfig(tenantID string, req map[string]any) (map[string]any, error) {
	return c.doJSON("PUT", "/api/v1/tenants/"+tenantID+"/oidc", req)
}

func (c *AdminClient) DeleteOIDCConfig(tenantID string) (map[string]any, error) {
	return c.doJSON("DELETE", "/api/v1/tenants/"+tenantID+"/oidc", nil)
}

// --- Channel Rules ---

func (c *AdminClient) GetChannelRules(tenantID string) (map[string]any, error) {
	return c.doJSON("GET", "/api/v1/tenants/"+tenantID+"/channel-rules", nil)
}

func (c *AdminClient) SetChannelRules(tenantID string, req map[string]any) (map[string]any, error) {
	return c.doJSON("PUT", "/api/v1/tenants/"+tenantID+"/channel-rules", req)
}

func (c *AdminClient) DeleteChannelRules(tenantID string) (map[string]any, error) {
	return c.doJSON("DELETE", "/api/v1/tenants/"+tenantID+"/channel-rules", nil)
}

// --- Test Access ---

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

	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
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
	defer resp.Body.Close()

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
	var parts []string
	for k, v := range params {
		parts = append(parts, k+"="+v)
	}
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += "&"
		}
		result += p
	}
	return result
}
