-- Provisioning Service Database Schema (SQLite)
-- Version: 001_initial
-- Description: Initial schema for tenant management, keys, topics, quotas, and audit

-- ====================
-- TABLES
-- ====================

-- Tenant registry
CREATE TABLE IF NOT EXISTS tenants (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL CHECK(length(name) > 0 AND length(name) <= 256),
    status          TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active', 'suspended', 'deprovisioning', 'deleted')),
    consumer_type   TEXT NOT NULL DEFAULT 'shared' CHECK(consumer_type IN ('shared', 'dedicated')),
    metadata        TEXT NOT NULL DEFAULT '{}',
    created_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    suspended_at    DATETIME,
    deprovision_at  DATETIME,
    deleted_at      DATETIME
);

-- Tenant public keys for JWT validation
CREATE TABLE IF NOT EXISTS tenant_keys (
    key_id          TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    algorithm       TEXT NOT NULL CHECK(algorithm IN ('ES256', 'RS256', 'EdDSA')),
    public_key      TEXT NOT NULL CHECK(length(public_key) > 0),
    is_active       INTEGER NOT NULL DEFAULT 1,
    created_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    expires_at      DATETIME,
    revoked_at      DATETIME
);

-- Tenant topic categories
CREATE TABLE IF NOT EXISTS tenant_categories (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    category        TEXT NOT NULL,
    partitions      INTEGER NOT NULL DEFAULT 3,
    retention_ms    INTEGER NOT NULL DEFAULT 604800000,
    created_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    deleted_at      DATETIME,

    UNIQUE(tenant_id, category)
);

-- Resource quotas per tenant
CREATE TABLE IF NOT EXISTS tenant_quotas (
    tenant_id               TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    max_topics              INTEGER NOT NULL DEFAULT 50 CHECK(max_topics >= 1),
    max_partitions          INTEGER NOT NULL DEFAULT 200 CHECK(max_partitions >= 1),
    max_storage_bytes       INTEGER NOT NULL DEFAULT 10737418240 CHECK(max_storage_bytes >= 0),
    producer_byte_rate      INTEGER NOT NULL DEFAULT 10485760 CHECK(producer_byte_rate >= 0),
    consumer_byte_rate      INTEGER NOT NULL DEFAULT 52428800 CHECK(consumer_byte_rate >= 0),
    updated_at              DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- Audit log (append-only)
CREATE TABLE IF NOT EXISTS provisioning_audit (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    tenant_id       TEXT,
    action          TEXT NOT NULL CHECK(length(action) > 0),
    actor           TEXT NOT NULL CHECK(length(actor) > 0),
    actor_type      TEXT NOT NULL DEFAULT 'user' CHECK(actor_type IN ('user', 'system', 'api_key')),
    ip_address      TEXT,
    details         TEXT NOT NULL DEFAULT '{}',
    created_at      DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- ====================
-- INDEXES
-- ====================

CREATE INDEX IF NOT EXISTS idx_tenant_keys_tenant_active ON tenant_keys(tenant_id)
    WHERE is_active = 1 AND revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_tenant_keys_lookup ON tenant_keys(key_id, is_active)
    WHERE is_active = 1 AND revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_tenant_categories_tenant ON tenant_categories(tenant_id)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_audit_tenant_time ON provisioning_audit(tenant_id, created_at);

CREATE INDEX IF NOT EXISTS idx_tenants_status ON tenants(status)
    WHERE status != 'deleted';

CREATE INDEX IF NOT EXISTS idx_tenants_deprovision ON tenants(deprovision_at)
    WHERE status = 'deprovisioning' AND deprovision_at IS NOT NULL;

-- ====================
-- TRIGGERS
-- ====================

CREATE TRIGGER IF NOT EXISTS trigger_tenant_updated_at
    AFTER UPDATE ON tenants
    FOR EACH ROW
BEGIN
    UPDATE tenants SET updated_at = datetime('now') WHERE id = NEW.id;
END;

CREATE TRIGGER IF NOT EXISTS trigger_quota_updated_at
    AFTER UPDATE ON tenant_quotas
    FOR EACH ROW
BEGIN
    UPDATE tenant_quotas SET updated_at = datetime('now') WHERE tenant_id = NEW.tenant_id;
END;
