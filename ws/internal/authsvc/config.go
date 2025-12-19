package authsvc

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config represents the tenant configuration loaded from YAML.
type Config struct {
	Tenants []Tenant `yaml:"tenants"`
}

// LoadConfig loads tenant configuration from a YAML file.
// App secrets are loaded from environment variables specified in secret_env.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Load app secrets from env vars and set tenant IDs
	for i := range cfg.Tenants {
		for j := range cfg.Tenants[i].Apps {
			app := &cfg.Tenants[i].Apps[j]
			app.TenantID = cfg.Tenants[i].ID
			if app.SecretEnv != "" {
				app.SetSecret(os.Getenv(app.SecretEnv))
			}
		}
	}

	return &cfg, nil
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if len(c.Tenants) == 0 {
		return fmt.Errorf("no tenants configured")
	}

	seenAppIDs := make(map[string]bool)
	for _, tenant := range c.Tenants {
		if tenant.ID == "" {
			return fmt.Errorf("tenant has empty ID")
		}
		for _, app := range tenant.Apps {
			if app.ID == "" {
				return fmt.Errorf("app in tenant %q has empty ID", tenant.ID)
			}
			if seenAppIDs[app.ID] {
				return fmt.Errorf("duplicate app ID: %q", app.ID)
			}
			seenAppIDs[app.ID] = true
			if app.Secret() == "" {
				return fmt.Errorf("app %q has no secret configured (env var: %s)", app.ID, app.SecretEnv)
			}
		}
	}

	return nil
}

// FindApp finds an app by ID across all tenants.
func (c *Config) FindApp(appID string) *App {
	for i := range c.Tenants {
		for j := range c.Tenants[i].Apps {
			if c.Tenants[i].Apps[j].ID == appID {
				return &c.Tenants[i].Apps[j]
			}
		}
	}
	return nil
}
