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
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// ProvisioningClient is an HTTP client for the provisioning API.
// It handles tenant and key management operations using the admin token.
type ProvisioningClient struct {
	baseURL    string
	adminToken string
	httpClient *http.Client
	logger     zerolog.Logger
}

// NewProvisioningClient creates a new provisioning API client.
func NewProvisioningClient(baseURL, adminToken string, logger zerolog.Logger) *ProvisioningClient {
	return &ProvisioningClient{
		baseURL:    baseURL,
		adminToken: adminToken,
		httpClient: &http.Client{Timeout: provisioningTimeout},
		logger:     logger.With().Str("component", "provisioning_client").Logger(),
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

func (c *ProvisioningClient) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if c.adminToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.adminToken)
	}
}

func (c *ProvisioningClient) readError(operation string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	return fmt.Errorf("%s: HTTP %d: %s", operation, resp.StatusCode, string(body))
}
