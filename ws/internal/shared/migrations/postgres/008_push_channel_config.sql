-- Push channel config: per-tenant push routing patterns.
-- Patterns use glob syntax with tenant prefix (e.g., "acme.alerts.*").

CREATE TABLE push_channel_configs (
    id              SERIAL PRIMARY KEY,
    tenant_id       VARCHAR NOT NULL UNIQUE,
    patterns        TEXT[] NOT NULL,
    default_ttl     INTEGER NOT NULL DEFAULT 2419200,
    default_urgency VARCHAR NOT NULL DEFAULT 'normal',
    created_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMP NOT NULL DEFAULT NOW()
);
