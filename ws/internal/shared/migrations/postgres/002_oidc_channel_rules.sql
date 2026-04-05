-- Provisioning Service Database Schema
-- Version: 002_oidc_channel_rules
-- Description: Add OIDC configuration and per-tenant channel rules for multi-issuer support

-- ====================
-- TABLES
-- ====================

-- Tenant OIDC configuration
-- Allows each tenant to register their Identity Provider for external auth
CREATE TABLE tenant_oidc_config (
    tenant_id       TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    issuer_url      TEXT NOT NULL,
    jwks_url        TEXT,                       -- Optional override, defaults to {issuer}/.well-known/jwks.json
    audience        TEXT,                       -- Expected audience claim
    enabled         BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Each issuer can only belong to one tenant
    CONSTRAINT tenant_oidc_issuer_unique UNIQUE (issuer_url),
    -- Issuer URL must be HTTPS
    CONSTRAINT issuer_url_https CHECK (issuer_url LIKE 'https://%'),
    -- Issuer URL max length
    CONSTRAINT issuer_url_max_length CHECK (char_length(issuer_url) <= 512),
    -- JWKS URL must be HTTPS if provided
    CONSTRAINT jwks_url_https CHECK (jwks_url IS NULL OR jwks_url LIKE 'https://%'),
    -- Audience max length
    CONSTRAINT audience_max_length CHECK (audience IS NULL OR char_length(audience) <= 256)
);

COMMENT ON TABLE tenant_oidc_config IS 'OIDC configuration per tenant for external IdP authentication';
COMMENT ON COLUMN tenant_oidc_config.issuer_url IS 'OIDC issuer URL (e.g., "https://acme.auth0.com/")';
COMMENT ON COLUMN tenant_oidc_config.jwks_url IS 'Custom JWKS URL (defaults to {issuer}/.well-known/jwks.json)';
COMMENT ON COLUMN tenant_oidc_config.audience IS 'Expected audience claim in JWTs';
COMMENT ON COLUMN tenant_oidc_config.enabled IS 'Whether this OIDC config is currently active';

-- Tenant channel access rules
-- Maps IdP groups to allowed channel patterns
CREATE TABLE tenant_channel_rules (
    tenant_id       TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    rules           JSONB NOT NULL DEFAULT '{"public": [], "group_mappings": {}, "default": []}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Example rules JSONB structure:
-- {
--   "public": ["*.metadata", "*.analytics"],
--   "group_mappings": {
--     "traders": ["*.trade", "*.liquidity"],
--     "premium": ["*.realtime.*"]
--   },
--   "default": ["*.basic"]
-- }

COMMENT ON TABLE tenant_channel_rules IS 'Per-tenant channel access rules mapping IdP groups to channel patterns';
COMMENT ON COLUMN tenant_channel_rules.rules IS 'JSONB with public patterns, group_mappings, and default patterns';

-- ====================
-- INDEXES
-- ====================

-- Issuer lookup for gateway hot path (enabled issuers only)
-- This is the critical index for token validation - lookup tenant by issuer
CREATE UNIQUE INDEX idx_tenant_oidc_issuer_enabled ON tenant_oidc_config(issuer_url)
    WHERE enabled = true;

-- ====================
-- TRIGGERS
-- ====================

-- Auto-update updated_at on OIDC config changes
CREATE OR REPLACE FUNCTION update_oidc_config_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_oidc_config_updated_at
    BEFORE UPDATE ON tenant_oidc_config
    FOR EACH ROW
    EXECUTE FUNCTION update_oidc_config_updated_at();

-- Auto-update updated_at on channel rules changes
CREATE OR REPLACE FUNCTION update_channel_rules_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_channel_rules_updated_at
    BEFORE UPDATE ON tenant_channel_rules
    FOR EACH ROW
    EXECUTE FUNCTION update_channel_rules_updated_at();
