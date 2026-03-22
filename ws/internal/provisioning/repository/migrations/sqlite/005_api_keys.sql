-- API keys for tenant identification (public identifiers, stored plaintext).
CREATE TABLE api_keys (
    key_id     TEXT PRIMARY KEY,
    tenant_id  TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name       TEXT NOT NULL DEFAULT '',
    is_active  INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    revoked_at DATETIME
);

-- Fast lookup of active keys per tenant.
CREATE INDEX idx_api_keys_tenant_active ON api_keys(tenant_id)
    WHERE is_active = 1 AND revoked_at IS NULL;

-- Fast lookup by key_id for active keys (used by gateway streaming).
CREATE INDEX idx_api_keys_lookup ON api_keys(key_id, is_active)
    WHERE is_active = 1 AND revoked_at IS NULL;
