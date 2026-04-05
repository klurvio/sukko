-- Push credentials: per-tenant provider credentials for push notifications.
-- Credential data is encrypted at the application layer (AES-256-GCM).

CREATE TABLE push_credentials (
    id              SERIAL PRIMARY KEY,
    tenant_id       VARCHAR NOT NULL,
    provider        VARCHAR NOT NULL,
    credential_data TEXT NOT NULL,
    created_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, provider)
);

CREATE INDEX idx_push_credentials_tenant ON push_credentials(tenant_id);
