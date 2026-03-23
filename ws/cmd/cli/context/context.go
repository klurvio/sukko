package context

import "fmt"

// Context represents a named environment configuration for the CLI.
type Context struct {
	Name             string `json:"name"`
	GatewayURL       string `json:"gateway_url"`
	ProvisioningURL  string `json:"provisioning_url"`
	TesterURL        string `json:"tester_url,omitempty"`
	AdminTokenEnc    string `json:"admin_token_encrypted,omitempty"`
	HMACSecretEnc    string `json:"hmac_secret_encrypted,omitempty"`
	APIKeyEnc        string `json:"api_key_encrypted,omitempty"`
	ActiveTenant     string `json:"active_tenant,omitempty"`
}

// AdminToken decrypts and returns the admin token.
func (c *Context) AdminToken(key []byte) (string, error) {
	if c.AdminTokenEnc == "" {
		return "", nil
	}
	plaintext, err := Decrypt(key, c.AdminTokenEnc)
	if err != nil {
		return "", fmt.Errorf("decrypt admin token: %w", err)
	}
	return plaintext, nil
}

// HMACSecret decrypts and returns the HMAC secret.
func (c *Context) HMACSecret(key []byte) (string, error) {
	if c.HMACSecretEnc == "" {
		return "", nil
	}
	plaintext, err := Decrypt(key, c.HMACSecretEnc)
	if err != nil {
		return "", fmt.Errorf("decrypt hmac secret: %w", err)
	}
	return plaintext, nil
}

// APIKey decrypts and returns the API key.
func (c *Context) APIKey(key []byte) (string, error) {
	if c.APIKeyEnc == "" {
		return "", nil
	}
	plaintext, err := Decrypt(key, c.APIKeyEnc)
	if err != nil {
		return "", fmt.Errorf("decrypt api key: %w", err)
	}
	return plaintext, nil
}
