package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog"
)

// provisioningTimeout is the HTTP client timeout for provisioning API calls.
const provisioningTimeout = 10 * time.Second

// maxResponseBody is the maximum response body size read for error messages.
const maxResponseBody = 4096

// RegisterKeyRequest is the request body for registering a public key.
type RegisterKeyRequest struct {
	KeyID     string     `json:"key_id"`
	Algorithm string     `json:"algorithm"`
	PublicKey string     `json:"public_key"`
	ExpiresAt *time.Time `json:"expires_at,omitzero"`
}

// ProvisioningClient is an HTTP client for the provisioning API.
// Admin auth is handled by the Provider (signs each request with an admin JWT).
// Tenant auth (GetTenant) uses a per-call JWT parameter — not the Provider.
type ProvisioningClient struct {
	baseURL      string
	authProvider Provider
	httpClient   *http.Client
	logger       zerolog.Logger
}

// NewProvisioningClient creates a new provisioning API client.
// The authProvider signs every admin request with a JWT. Pass nil to skip auth
// (only valid for unauthenticated endpoints like /edition).
func NewProvisioningClient(baseURL string, authProvider Provider, logger zerolog.Logger) *ProvisioningClient {
	return &ProvisioningClient{
		baseURL:      baseURL,
		authProvider: authProvider,
		httpClient:   &http.Client{Timeout: provisioningTimeout},
		logger:       logger.With().Str("component", "provisioning_client").Logger(),
	}
}

// CreateTenant creates a new tenant via the provisioning API.
func (c *ProvisioningClient) CreateTenant(ctx context.Context, tenantID, name string) error {
	body, err := json.Marshal(map[string]string{
		"id":            tenantID,
		"name":          name,
		"consumer_type": "shared",
	})
	if err != nil {
		return fmt.Errorf("create tenant: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/tenants", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create tenant: build request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("create tenant: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return c.readError("create tenant", resp)
	}

	c.logger.Info().Str("tenant_id", tenantID).Msg("tenant created")
	return nil
}

// DeleteTenant deletes a tenant via the provisioning API with force=true.
func (c *ProvisioningClient) DeleteTenant(ctx context.Context, tenantID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/api/v1/tenants/"+tenantID+"?force=true", http.NoBody)
	if err != nil {
		return fmt.Errorf("delete tenant: build request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete tenant: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return c.readError("delete tenant", resp)
	}

	c.logger.Info().Str("tenant_id", tenantID).Msg("tenant deleted")
	return nil
}

// RegisterKey registers a public key for a tenant.
func (c *ProvisioningClient) RegisterKey(ctx context.Context, tenantID string, keyReq RegisterKeyRequest) error {
	body, err := json.Marshal(keyReq)
	if err != nil {
		return fmt.Errorf("register key: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/tenants/"+tenantID+"/keys", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("register key: build request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("register key: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return c.readError("register key", resp)
	}

	c.logger.Info().Str("tenant_id", tenantID).Str("key_id", keyReq.KeyID).Msg("key registered")
	return nil
}

// RevokeKey revokes a key for a tenant.
func (c *ProvisioningClient) RevokeKey(ctx context.Context, tenantID, keyID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/api/v1/tenants/"+tenantID+"/keys/"+keyID, http.NoBody)
	if err != nil {
		return fmt.Errorf("revoke key: build request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("revoke key: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return c.readError("revoke key", resp)
	}

	c.logger.Info().Str("tenant_id", tenantID).Str("key_id", keyID).Msg("key revoked")
	return nil
}

// GetTenant fetches a tenant by ID using the given token (admin or tenant JWT).
// Returns the HTTP status code and any error.
func (c *ProvisioningClient) GetTenant(ctx context.Context, tenantID, token string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/tenants/"+tenantID, http.NoBody)
	if err != nil {
		return 0, fmt.Errorf("get tenant: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("get tenant: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	// Drain body so connection can be reused.
	_, _ = io.Copy(io.Discard, resp.Body)

	return resp.StatusCode, nil
}

// SetChannelRules sets channel rules for a tenant via PUT /api/v1/tenants/{id}/channel-rules.
func (c *ProvisioningClient) SetChannelRules(ctx context.Context, tenantID string, rules map[string]any) error {
	body, err := json.Marshal(rules)
	if err != nil {
		return fmt.Errorf("set channel rules: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+"/api/v1/tenants/"+tenantID+"/channel-rules", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("set channel rules: build request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("set channel rules: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return c.readError("set channel rules", resp)
	}

	c.logger.Info().Str("tenant_id", tenantID).Msg("channel rules set")
	return nil
}

// SetRoutingRules sets routing rules for a tenant via PUT /api/v1/tenants/{id}/routing-rules.
func (c *ProvisioningClient) SetRoutingRules(ctx context.Context, tenantID string, rules []map[string]any) error {
	body, err := json.Marshal(map[string]any{"rules": rules})
	if err != nil {
		return fmt.Errorf("set routing rules: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+"/api/v1/tenants/"+tenantID+"/routing-rules", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("set routing rules: build request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("set routing rules: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return c.readError("set routing rules", resp)
	}

	c.logger.Info().Str("tenant_id", tenantID).Msg("routing rules set")
	return nil
}

// SetRoutingRulesRaw sets routing rules and returns the HTTP status code.
// Used by edition limit boundary testing which needs to distinguish 200 vs 403.
func (c *ProvisioningClient) SetRoutingRulesRaw(ctx context.Context, tenantID string, rules []map[string]any) (int, error) {
	body, err := json.Marshal(map[string]any{"rules": rules})
	if err != nil {
		return 0, fmt.Errorf("set routing rules: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+"/api/v1/tenants/"+tenantID+"/routing-rules", bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("set routing rules: build request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("set routing rules: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return resp.StatusCode, c.readError("set routing rules", resp)
	}

	return resp.StatusCode, nil
}

// DeleteRoutingRules deletes routing rules for a tenant via DELETE /api/v1/tenants/{id}/routing-rules.
// Returns nil if rules don't exist (404 is acceptable).
func (c *ProvisioningClient) DeleteRoutingRules(ctx context.Context, tenantID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/api/v1/tenants/"+tenantID+"/routing-rules", http.NoBody)
	if err != nil {
		return fmt.Errorf("delete routing rules: build request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete routing rules: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// 200 and 404 are both acceptable — rules may not exist
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return c.readError("delete routing rules", resp)
	}

	c.logger.Info().Str("tenant_id", tenantID).Msg("routing rules deleted")
	return nil
}

// ListTenants fetches the tenant list. Returns raw JSON response body.
func (c *ProvisioningClient) ListTenants(ctx context.Context) ([]byte, error) {
	return c.doGet(ctx, "/api/v1/tenants", "list tenants")
}

// UpdateTenant partially updates a tenant via PATCH.
func (c *ProvisioningClient) UpdateTenant(ctx context.Context, tenantID string, updates map[string]any) error {
	return c.doPatchJSON(ctx, "/api/v1/tenants/"+tenantID, updates, "update tenant")
}

// GetTenantByID fetches a tenant using the admin token. Returns raw JSON response body.
// Unlike GetTenant (which accepts an arbitrary token for JWT testing), this always authenticates
// as the admin — suitable for validate:provisioning CRUD checks.
func (c *ProvisioningClient) GetTenantByID(ctx context.Context, tenantID string) ([]byte, error) {
	return c.doGet(ctx, "/api/v1/tenants/"+tenantID, "get tenant")
}

// SuspendTenant suspends a tenant via POST /api/v1/tenants/{id}/suspend.
func (c *ProvisioningClient) SuspendTenant(ctx context.Context, tenantID string) error {
	return c.doPost(ctx, "/api/v1/tenants/"+tenantID+"/suspend", nil, "suspend tenant")
}

// ReactivateTenant reactivates a suspended tenant via POST /api/v1/tenants/{id}/reactivate.
func (c *ProvisioningClient) ReactivateTenant(ctx context.Context, tenantID string) error {
	return c.doPost(ctx, "/api/v1/tenants/"+tenantID+"/reactivate", nil, "reactivate tenant")
}

// CreateAPIKey creates an API key for a tenant. Returns raw JSON response body (contains the key value).
func (c *ProvisioningClient) CreateAPIKey(ctx context.Context, tenantID, name string) ([]byte, error) {
	body, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return nil, fmt.Errorf("create api key: marshal: %w", err)
	}
	return c.doPostForBody(ctx, "/api/v1/tenants/"+tenantID+"/api-keys", body, "create api key")
}

// ListAPIKeys lists API keys for a tenant. Returns raw JSON response body.
func (c *ProvisioningClient) ListAPIKeys(ctx context.Context, tenantID string) ([]byte, error) {
	return c.doGet(ctx, "/api/v1/tenants/"+tenantID+"/api-keys", "list api keys")
}

// RevokeAPIKey revokes an API key.
func (c *ProvisioningClient) RevokeAPIKey(ctx context.Context, tenantID, keyID string) error {
	return c.doDelete(ctx, "/api/v1/tenants/"+tenantID+"/api-keys/"+keyID, "revoke api key")
}

// ListKeys lists signing keys for a tenant. Returns raw JSON response body.
func (c *ProvisioningClient) ListKeys(ctx context.Context, tenantID string) ([]byte, error) {
	return c.doGet(ctx, "/api/v1/tenants/"+tenantID+"/keys", "list keys")
}

// GetRoutingRules fetches routing rules for a tenant. Returns raw JSON response body.
func (c *ProvisioningClient) GetRoutingRules(ctx context.Context, tenantID string) ([]byte, error) {
	return c.doGet(ctx, "/api/v1/tenants/"+tenantID+"/routing-rules", "get routing rules")
}

// GetChannelRules fetches channel rules for a tenant. Returns raw JSON response body.
func (c *ProvisioningClient) GetChannelRules(ctx context.Context, tenantID string) ([]byte, error) {
	return c.doGet(ctx, "/api/v1/tenants/"+tenantID+"/channel-rules", "get channel rules")
}

// DeleteChannelRules deletes channel rules for a tenant.
func (c *ProvisioningClient) DeleteChannelRules(ctx context.Context, tenantID string) error {
	return c.doDelete(ctx, "/api/v1/tenants/"+tenantID+"/channel-rules", "delete channel rules")
}

// GetQuota fetches quotas for a tenant. Returns raw JSON response body.
func (c *ProvisioningClient) GetQuota(ctx context.Context, tenantID string) ([]byte, error) {
	return c.doGet(ctx, "/api/v1/tenants/"+tenantID+"/quotas", "get quota")
}

// UpdateQuota partially updates quotas for a tenant via PATCH.
func (c *ProvisioningClient) UpdateQuota(ctx context.Context, tenantID string, updates map[string]any) error {
	return c.doPatchJSON(ctx, "/api/v1/tenants/"+tenantID+"/quotas", updates, "update quota")
}

// GetAuditLog fetches the audit log for a tenant. Returns raw JSON response body.
func (c *ProvisioningClient) GetAuditLog(ctx context.Context, tenantID string) ([]byte, error) {
	return c.doGet(ctx, "/api/v1/tenants/"+tenantID+"/audit", "get audit log")
}

// --- HTTP helpers ---

func (c *ProvisioningClient) doGet(ctx context.Context, path, operation string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("%s: build request: %w", operation, err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", operation, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d: %s", operation, resp.StatusCode, string(body))
	}
	if readErr != nil {
		return nil, fmt.Errorf("%s: read response body: %w", operation, readErr)
	}
	return body, nil
}

func (c *ProvisioningClient) doPost(ctx context.Context, path string, payload []byte, operation string) error {
	var bodyReader io.Reader = http.NoBody
	if payload != nil {
		bodyReader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("%s: build request: %w", operation, err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return c.readError(operation, resp)
	}
	return nil
}

func (c *ProvisioningClient) doPostForBody(ctx context.Context, path string, payload []byte, operation string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("%s: build request: %w", operation, err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", operation, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("%s: HTTP %d: %s", operation, resp.StatusCode, string(body))
	}
	if readErr != nil {
		return nil, fmt.Errorf("%s: read response body: %w", operation, readErr)
	}
	return body, nil
}

func (c *ProvisioningClient) doDelete(ctx context.Context, path, operation string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+path, http.NoBody)
	if err != nil {
		return fmt.Errorf("%s: build request: %w", operation, err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return c.readError(operation, resp)
	}
	return nil
}

func (c *ProvisioningClient) doPatchJSON(ctx context.Context, path string, data map[string]any, operation string) error {
	body, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("%s: marshal: %w", operation, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%s: build request: %w", operation, err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return c.readError(operation, resp)
	}
	return nil
}

func (c *ProvisioningClient) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if c.authProvider != nil {
		c.authProvider.SignRequest(req)
	}
}

func (c *ProvisioningClient) readError(operation string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody)) // best-effort: error body for diagnostics only
	return fmt.Errorf("%s: HTTP %d: %s", operation, resp.StatusCode, string(body))
}
