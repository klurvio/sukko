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
func (c *AdminClient) CreateTenant(req map[string]any) (map[string]any, error) {
	return c.doJSON("POST", "/api/v1/tenants", req)
}

// GetTenant retrieves a tenant by ID.
func (c *AdminClient) GetTenant(tenantID string) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON("GET", tenantPath(tenantID), nil)
}

// ListTenants lists tenants with optional filter parameters.
func (c *AdminClient) ListTenants(params map[string]string) (map[string]any, error) {
	path := "/api/v1/tenants" + encodeParams(params)
	return c.doJSON("GET", path, nil)
}

// UpdateTenant updates a tenant by ID.
func (c *AdminClient) UpdateTenant(tenantID string, req map[string]any) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON("PATCH", tenantPath(tenantID), req)
}

// SuspendTenant suspends a tenant by ID.
func (c *AdminClient) SuspendTenant(tenantID string) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON("POST", tenantPath(tenantID, "suspend"), nil)
}

// ReactivateTenant reactivates a suspended tenant.
func (c *AdminClient) ReactivateTenant(tenantID string) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON("POST", tenantPath(tenantID, "reactivate"), nil)
}

// DeprovisionTenant deprovisions a tenant by ID.
func (c *AdminClient) DeprovisionTenant(tenantID string) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON("DELETE", tenantPath(tenantID), nil)
}

// --- Keys ---

// CreateKey registers a new public key for a tenant.
func (c *AdminClient) CreateKey(tenantID string, req map[string]any) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON("POST", tenantPath(tenantID, "keys"), req)
}

// ListKeys lists all keys for a tenant.
func (c *AdminClient) ListKeys(tenantID string) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON("GET", tenantPath(tenantID, "keys"), nil)
}

// RevokeKey revokes a key by tenant and key ID.
func (c *AdminClient) RevokeKey(tenantID, keyID string) (map[string]any, error) {
	if tenantID == "" || keyID == "" {
		return nil, errors.New("tenantID and keyID are required")
	}
	return c.doJSON("DELETE", tenantPath(tenantID, "keys", url.PathEscape(keyID)), nil)
}

// --- API Keys ---

// CreateAPIKey creates a new API key for a tenant.
func (c *AdminClient) CreateAPIKey(tenantID string, req map[string]any) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON("POST", tenantPath(tenantID, "api-keys"), req)
}

// ListAPIKeys lists all API keys for a tenant.
func (c *AdminClient) ListAPIKeys(tenantID string) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON("GET", tenantPath(tenantID, "api-keys"), nil)
}

// RevokeAPIKey revokes an API key by tenant and key ID.
func (c *AdminClient) RevokeAPIKey(tenantID, keyID string) (map[string]any, error) {
	if tenantID == "" || keyID == "" {
		return nil, errors.New("tenantID and keyID are required")
	}
	return c.doJSON("DELETE", tenantPath(tenantID, "api-keys", url.PathEscape(keyID)), nil)
}

// --- Routing Rules ---

// GetRoutingRules retrieves routing rules for a tenant.
func (c *AdminClient) GetRoutingRules(tenantID string) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON("GET", tenantPath(tenantID, "routing-rules"), nil)
}

// SetRoutingRules sets routing rules for a tenant.
func (c *AdminClient) SetRoutingRules(tenantID string, body any) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON("PUT", tenantPath(tenantID, "routing-rules"), body)
}

// DeleteRoutingRules deletes routing rules for a tenant.
func (c *AdminClient) DeleteRoutingRules(tenantID string) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON("DELETE", tenantPath(tenantID, "routing-rules"), nil)
}

// --- Quotas ---

// GetQuota retrieves the quota for a tenant.
func (c *AdminClient) GetQuota(tenantID string) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON("GET", tenantPath(tenantID, "quotas"), nil)
}

// UpdateQuota updates the quota for a tenant.
func (c *AdminClient) UpdateQuota(tenantID string, req map[string]any) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON("PATCH", tenantPath(tenantID, "quotas"), req)
}

// --- Channel Rules ---

// GetChannelRules retrieves channel rules for a tenant.
func (c *AdminClient) GetChannelRules(tenantID string) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON("GET", tenantPath(tenantID, "channel-rules"), nil)
}

// SetChannelRules sets channel rules for a tenant.
func (c *AdminClient) SetChannelRules(tenantID string, req map[string]any) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON("PUT", tenantPath(tenantID, "channel-rules"), req)
}

// DeleteChannelRules deletes channel rules for a tenant.
func (c *AdminClient) DeleteChannelRules(tenantID string) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON("DELETE", tenantPath(tenantID, "channel-rules"), nil)
}

// --- Test Access ---

// TestAccess tests channel access for a tenant with the given parameters.
func (c *AdminClient) TestAccess(tenantID string, req map[string]any) (map[string]any, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	return c.doJSON("POST", tenantPath(tenantID, "test-access"), req)
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

	// CLI commands are synchronous and short-lived; the HTTP client timeout provides
	// cancellation. context.TODO marks this as a candidate for context propagation
	// if the CLI ever needs cancellation (e.g., signal handling).
	req, err := http.NewRequestWithContext(context.TODO(), method, c.baseURL+path, bodyReader)
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
