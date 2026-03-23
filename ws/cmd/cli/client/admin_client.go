// Package client provides a REST admin client for the provisioning API.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultClientTimeout is the default HTTP client timeout.
const DefaultClientTimeout = 30 * time.Second

// Sentinel errors for API responses.
var (
	ErrAPIBadRequest   = errors.New("API bad request")
	ErrAPIUnauthorized = errors.New("API unauthorized")
	ErrAPIForbidden    = errors.New("API forbidden")
	ErrAPINotFound     = errors.New("API not found")
	ErrAPIInternal     = errors.New("API internal error")
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
func New(cfg Config) (*AdminClient, error) {
	if cfg.BaseURL == "" {
		return nil, errors.New("admin client: BaseURL is required")
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = DefaultClientTimeout
	}
	return &AdminClient{
		baseURL: cfg.BaseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		token: cfg.Token,
	}, nil
}

// requireTenantID validates that a tenantID is not empty.
func requireTenantID(tenantID string) error {
	if tenantID == "" {
		return errors.New("tenantID is required")
	}
	return nil
}

// tenantPath builds a URL path for a tenant resource, escaping path components.
func tenantPath(tenantID string, subpath ...string) string {
	parts := make([]string, 0, 2+len(subpath))
	parts = append(parts, "/api/v1/tenants", url.PathEscape(tenantID))
	parts = append(parts, subpath...)
	return strings.Join(parts, "/")
}

// --- Tenants ---

// CreateTenant creates a new tenant via the provisioning API.
func (c *AdminClient) CreateTenant(ctx context.Context, req map[string]any) (map[string]any, error) {
	return c.doJSON(ctx, "POST", "/api/v1/tenants", req)
}

// GetTenant retrieves a tenant by ID.
func (c *AdminClient) GetTenant(ctx context.Context, tenantID string) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON(ctx, "GET", tenantPath(tenantID), nil)
}

// ListTenants lists tenants with optional filter parameters.
func (c *AdminClient) ListTenants(ctx context.Context, params map[string]string) (map[string]any, error) {
	path := "/api/v1/tenants" + encodeParams(params)
	return c.doJSON(ctx, "GET", path, nil)
}

// UpdateTenant updates a tenant by ID.
func (c *AdminClient) UpdateTenant(ctx context.Context, tenantID string, req map[string]any) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON(ctx, "PATCH", tenantPath(tenantID), req)
}

// SuspendTenant suspends a tenant by ID.
func (c *AdminClient) SuspendTenant(ctx context.Context, tenantID string) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON(ctx, "POST", tenantPath(tenantID, "suspend"), nil)
}

// ReactivateTenant reactivates a suspended tenant.
func (c *AdminClient) ReactivateTenant(ctx context.Context, tenantID string) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON(ctx, "POST", tenantPath(tenantID, "reactivate"), nil)
}

// DeprovisionTenant deprovisions a tenant by ID.
func (c *AdminClient) DeprovisionTenant(ctx context.Context, tenantID string) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON(ctx, "DELETE", tenantPath(tenantID), nil)
}

// --- Keys ---

// CreateKey registers a new public key for a tenant.
func (c *AdminClient) CreateKey(ctx context.Context, tenantID string, req map[string]any) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON(ctx, "POST", tenantPath(tenantID, "keys"), req)
}

// ListKeys lists all keys for a tenant.
func (c *AdminClient) ListKeys(ctx context.Context, tenantID string) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON(ctx, "GET", tenantPath(tenantID, "keys"), nil)
}

// RevokeKey revokes a key by tenant and key ID.
func (c *AdminClient) RevokeKey(ctx context.Context, tenantID, keyID string) (map[string]any, error) {
	if tenantID == "" || keyID == "" {
		return nil, errors.New("tenantID and keyID are required")
	}
	return c.doJSON(ctx, "DELETE", tenantPath(tenantID, "keys", url.PathEscape(keyID)), nil)
}

// --- API Keys ---

// CreateAPIKey creates a new API key for a tenant.
func (c *AdminClient) CreateAPIKey(ctx context.Context, tenantID string, req map[string]any) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON(ctx, "POST", tenantPath(tenantID, "api-keys"), req)
}

// ListAPIKeys lists all API keys for a tenant.
func (c *AdminClient) ListAPIKeys(ctx context.Context, tenantID string) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON(ctx, "GET", tenantPath(tenantID, "api-keys"), nil)
}

// RevokeAPIKey revokes an API key by tenant and key ID.
func (c *AdminClient) RevokeAPIKey(ctx context.Context, tenantID, keyID string) (map[string]any, error) {
	if tenantID == "" || keyID == "" {
		return nil, errors.New("tenantID and keyID are required")
	}
	return c.doJSON(ctx, "DELETE", tenantPath(tenantID, "api-keys", url.PathEscape(keyID)), nil)
}

// --- Routing Rules ---

// GetRoutingRules retrieves routing rules for a tenant.
func (c *AdminClient) GetRoutingRules(ctx context.Context, tenantID string) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON(ctx, "GET", tenantPath(tenantID, "routing-rules"), nil)
}

// SetRoutingRules sets routing rules for a tenant.
func (c *AdminClient) SetRoutingRules(ctx context.Context, tenantID string, body any) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON(ctx, "PUT", tenantPath(tenantID, "routing-rules"), body)
}

// DeleteRoutingRules deletes routing rules for a tenant.
func (c *AdminClient) DeleteRoutingRules(ctx context.Context, tenantID string) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON(ctx, "DELETE", tenantPath(tenantID, "routing-rules"), nil)
}

// --- Quotas ---

// GetQuota retrieves the quota for a tenant.
func (c *AdminClient) GetQuota(ctx context.Context, tenantID string) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON(ctx, "GET", tenantPath(tenantID, "quotas"), nil)
}

// UpdateQuota updates the quota for a tenant.
func (c *AdminClient) UpdateQuota(ctx context.Context, tenantID string, req map[string]any) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON(ctx, "PATCH", tenantPath(tenantID, "quotas"), req)
}

// --- Channel Rules ---

// GetChannelRules retrieves channel rules for a tenant.
func (c *AdminClient) GetChannelRules(ctx context.Context, tenantID string) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON(ctx, "GET", tenantPath(tenantID, "channel-rules"), nil)
}

// SetChannelRules sets channel rules for a tenant.
func (c *AdminClient) SetChannelRules(ctx context.Context, tenantID string, req map[string]any) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON(ctx, "PUT", tenantPath(tenantID, "channel-rules"), req)
}

// DeleteChannelRules deletes channel rules for a tenant.
func (c *AdminClient) DeleteChannelRules(ctx context.Context, tenantID string) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON(ctx, "DELETE", tenantPath(tenantID, "channel-rules"), nil)
}

// --- Test Access ---

// TestAccess tests channel access for a tenant with the given parameters.
func (c *AdminClient) TestAccess(ctx context.Context, tenantID string, req map[string]any) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON(ctx, "POST", tenantPath(tenantID, "test-access"), req)
}

// --- Internal ---

func (c *AdminClient) doJSON(ctx context.Context, method, path string, body any) (map[string]any, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
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
	defer func() { _ = resp.Body.Close() }() // close error inconsequential for completed HTTP response

	// Limit response body to 10MB to prevent OOM from malicious/broken servers
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		body := string(respBody)
		switch {
		case resp.StatusCode == http.StatusUnauthorized:
			return nil, fmt.Errorf("%w: %s", ErrAPIUnauthorized, body)
		case resp.StatusCode == http.StatusForbidden:
			return nil, fmt.Errorf("%w: %s", ErrAPIForbidden, body)
		case resp.StatusCode == http.StatusNotFound:
			return nil, fmt.Errorf("%w: %s", ErrAPINotFound, body)
		case resp.StatusCode >= 400 && resp.StatusCode < 500:
			return nil, fmt.Errorf("%w (HTTP %d): %s", ErrAPIBadRequest, resp.StatusCode, body)
		default:
			return nil, fmt.Errorf("%w (HTTP %d): %s", ErrAPIInternal, resp.StatusCode, body)
		}
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
