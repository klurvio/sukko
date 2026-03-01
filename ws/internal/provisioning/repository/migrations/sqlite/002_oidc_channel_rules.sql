-- Provisioning Service Database Schema (SQLite)
-- Version: 002_oidc_channel_rules
-- Description: Add OIDC configuration and per-tenant channel rules for multi-issuer support

-- ====================
-- TABLES
-- ====================

-- Tenant OIDC configuration
CREATE TABLE IF NOT EXISTS tenant_oidc_config (
    tenant_id       TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    issuer_url      TEXT NOT NULL UNIQUE CHECK(issuer_url LIKE 'https://%' AND length(issuer_url) <= 512),
    jwks_url        TEXT CHECK(jwks_url IS NULL OR jwks_url LIKE 'https://%'),
    audience        TEXT CHECK(audience IS NULL OR length(audience) <= 256),
    enabled         INTEGER NOT NULL DEFAULT 1,
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Tenant channel access rules
CREATE TABLE IF NOT EXISTS tenant_channel_rules (
    tenant_id       TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    rules           TEXT NOT NULL DEFAULT '{"public": [], "group_mappings": {}, "default": []}',
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

-- ====================
-- INDEXES
-- ====================

CREATE UNIQUE INDEX IF NOT EXISTS idx_tenant_oidc_issuer_enabled ON tenant_oidc_config(issuer_url)
    WHERE enabled = 1;

-- ====================
-- TRIGGERS
-- ====================

CREATE TRIGGER IF NOT EXISTS trigger_oidc_config_updated_at
    AFTER UPDATE ON tenant_oidc_config
    FOR EACH ROW
BEGIN
    UPDATE tenant_oidc_config SET updated_at = datetime('now') WHERE tenant_id = NEW.tenant_id;
END;

CREATE TRIGGER IF NOT EXISTS trigger_channel_rules_updated_at
    AFTER UPDATE ON tenant_channel_rules
    FOR EACH ROW
BEGIN
    UPDATE tenant_channel_rules SET updated_at = datetime('now') WHERE tenant_id = NEW.tenant_id;
END;
