-- Push credentials: per-tenant provider credentials for push notifications.
-- Credential data is encrypted at the application layer (AES-256-GCM).

CREATE TABLE IF NOT EXISTS push_credentials (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    tenant_id       TEXT NOT NULL,
    provider        TEXT NOT NULL,
    credential_data TEXT NOT NULL,
    created_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE(tenant_id, provider)
);

CREATE INDEX IF NOT EXISTS idx_push_credentials_tenant ON push_credentials(tenant_id);
