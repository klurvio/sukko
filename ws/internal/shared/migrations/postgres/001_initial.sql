-- Sukko Database Schema (consolidated)
-- All tables for provisioning, push, and analytics services.

-- ====================
-- ENUMS
-- ====================

CREATE TYPE tenant_status AS ENUM ('active', 'suspended', 'deprovisioning', 'deleted');
CREATE TYPE consumer_type AS ENUM ('shared', 'dedicated');

-- ====================
-- CORE TABLES
-- ====================

-- Tenant registry
CREATE TABLE tenants (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    status          tenant_status NOT NULL DEFAULT 'active',
    consumer_type   consumer_type NOT NULL DEFAULT 'shared',
    metadata        JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    suspended_at    TIMESTAMPTZ,
    deprovision_at  TIMESTAMPTZ,
    deleted_at      TIMESTAMPTZ,

    CONSTRAINT valid_tenant_id CHECK (id ~ '^[a-z][a-z0-9-]{2,62}$'),
    CONSTRAINT name_not_empty CHECK (char_length(name) > 0),
    CONSTRAINT name_max_length CHECK (char_length(name) <= 256),
    CONSTRAINT chk_tenant_id_no_dots CHECK (id NOT LIKE '%.%'),
    CONSTRAINT chk_tenant_id_no_underscore_prefix CHECK (id NOT LIKE E'\\_%')
);

-- Tenant JWT signing keys
CREATE TABLE tenant_keys (
    key_id          TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    algorithm       TEXT NOT NULL,
    public_key      TEXT NOT NULL,
    is_active       BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ,
    revoked_at      TIMESTAMPTZ,

    CONSTRAINT valid_key_id CHECK (key_id ~ '^[a-z][a-z0-9-]{2,62}$'),
    CONSTRAINT valid_algorithm CHECK (algorithm IN ('ES256', 'RS256', 'EdDSA')),
    CONSTRAINT public_key_not_empty CHECK (char_length(public_key) > 0)
);

-- Tenant channel access rules
CREATE TABLE tenant_channel_rules (
    tenant_id       TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    rules           JSONB NOT NULL DEFAULT '{"public": [], "group_mappings": {}, "default": []}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Tenant topic routing rules
CREATE TABLE tenant_routing_rules (
    tenant_id       TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    rules           JSONB NOT NULL DEFAULT '[]',
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL
);

-- Resource quotas per tenant
CREATE TABLE tenant_quotas (
    tenant_id               TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    max_topics              INT NOT NULL DEFAULT 50,
    max_partitions          INT NOT NULL DEFAULT 200,
    max_storage_bytes       BIGINT NOT NULL DEFAULT 10737418240,
    producer_byte_rate      BIGINT NOT NULL DEFAULT 10485760,
    consumer_byte_rate      BIGINT NOT NULL DEFAULT 52428800,
    max_connections         INTEGER NOT NULL DEFAULT 0,
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT valid_max_topics CHECK (max_topics >= 1),
    CONSTRAINT valid_max_partitions CHECK (max_partitions >= 1),
    CONSTRAINT valid_max_storage CHECK (max_storage_bytes >= 0),
    CONSTRAINT valid_producer_rate CHECK (producer_byte_rate >= 0),
    CONSTRAINT valid_consumer_rate CHECK (consumer_byte_rate >= 0)
);

-- API keys for tenant identification
CREATE TABLE api_keys (
    key_id     TEXT PRIMARY KEY,
    tenant_id  TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name       TEXT NOT NULL DEFAULT '',
    is_active  BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMPTZ
);

-- Admin keys for JWT-based admin authentication
CREATE TABLE admin_keys (
    id            SERIAL       PRIMARY KEY,
    key_id        VARCHAR(64)  NOT NULL UNIQUE,
    name          VARCHAR(255) NOT NULL DEFAULT 'unnamed',
    algorithm     VARCHAR(16)  NOT NULL DEFAULT 'Ed25519',
    public_key    TEXT         NOT NULL,
    registered_by VARCHAR(255) NOT NULL DEFAULT 'system',
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    revoked_at    TIMESTAMPTZ
);

-- Audit log (append-only)
CREATE TABLE provisioning_audit (
    id              BIGSERIAL PRIMARY KEY,
    tenant_id       TEXT,
    action          TEXT NOT NULL,
    actor           TEXT NOT NULL,
    actor_type      TEXT NOT NULL DEFAULT 'user',
    ip_address      INET,
    details         JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT action_not_empty CHECK (char_length(action) > 0),
    CONSTRAINT actor_not_empty CHECK (char_length(actor) > 0),
    CONSTRAINT valid_actor_type CHECK (actor_type IN ('user', 'system', 'api_key'))
);

-- License state (single-row, encrypted)
CREATE TABLE license_state (
    id              INT          PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    encrypted_key   TEXT         NOT NULL,
    edition         VARCHAR(16)  NOT NULL,
    org             VARCHAR(255) NOT NULL DEFAULT '',
    expires_at      TIMESTAMPTZ,
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- ====================
-- PUSH TABLES
-- ====================

-- Push credentials per tenant per provider (encrypted at app layer)
CREATE TABLE push_credentials (
    id              SERIAL PRIMARY KEY,
    tenant_id       VARCHAR NOT NULL,
    provider        VARCHAR NOT NULL,
    credential_data TEXT NOT NULL,
    created_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, provider)
);

-- Push channel config per tenant
CREATE TABLE push_channel_configs (
    id              SERIAL PRIMARY KEY,
    tenant_id       VARCHAR NOT NULL UNIQUE,
    patterns        TEXT[] NOT NULL,
    default_ttl     INTEGER NOT NULL DEFAULT 2419200,
    default_urgency VARCHAR NOT NULL DEFAULT 'normal',
    created_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Push device subscriptions
CREATE TABLE push_subscriptions (
    id              SERIAL PRIMARY KEY,
    tenant_id       VARCHAR NOT NULL,
    principal       VARCHAR NOT NULL,
    platform        VARCHAR NOT NULL,
    token           VARCHAR,
    endpoint        VARCHAR,
    p256dh_key      VARCHAR,
    auth_secret     VARCHAR,
    channels        TEXT[] NOT NULL,
    jti             VARCHAR(255) NOT NULL,
    token_iat       TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    last_success_at TIMESTAMP
);

-- Push credential health tracking
CREATE TABLE push_credential_health (
    id                      SERIAL PRIMARY KEY,
    tenant_id               VARCHAR NOT NULL,
    provider                VARCHAR NOT NULL,
    status                  VARCHAR NOT NULL DEFAULT 'unknown',
    last_success_at         TIMESTAMPTZ,
    last_failure_at         TIMESTAMPTZ,
    consecutive_failures    INT NOT NULL DEFAULT 0,
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Push subscription count snapshots
CREATE TABLE subscription_stats (
    id                  SERIAL PRIMARY KEY,
    tenant_id           VARCHAR NOT NULL,
    platform            VARCHAR NOT NULL,
    total_count         INT NOT NULL DEFAULT 0,
    stale_cleaned_count INT NOT NULL DEFAULT 0,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ====================
-- ANALYTICS TABLES
-- ====================

CREATE TABLE analytics_connections (
    id              BIGSERIAL PRIMARY KEY,
    pod_id          VARCHAR NOT NULL,
    tenant_id       VARCHAR NOT NULL,
    bucket_start    TIMESTAMPTZ NOT NULL,
    bucket_size     VARCHAR NOT NULL,
    transport       VARCHAR NOT NULL,
    active_count    INT NOT NULL DEFAULT 0,
    connect_count   INT NOT NULL DEFAULT 0,
    disconnect_count INT NOT NULL DEFAULT 0,
    error_count     INT NOT NULL DEFAULT 0
);

CREATE TABLE analytics_messages (
    id              BIGSERIAL PRIMARY KEY,
    pod_id          VARCHAR NOT NULL,
    tenant_id       VARCHAR NOT NULL,
    bucket_start    TIMESTAMPTZ NOT NULL,
    bucket_size     VARCHAR NOT NULL,
    channel_prefix  VARCHAR NOT NULL,
    published_count INT NOT NULL DEFAULT 0,
    delivered_count INT NOT NULL DEFAULT 0,
    failed_count    INT NOT NULL DEFAULT 0,
    total_latency_ms BIGINT NOT NULL DEFAULT 0,
    sample_count    INT NOT NULL DEFAULT 0
);

CREATE TABLE analytics_push (
    id                  BIGSERIAL PRIMARY KEY,
    pod_id              VARCHAR NOT NULL,
    tenant_id           VARCHAR NOT NULL,
    bucket_start        TIMESTAMPTZ NOT NULL,
    bucket_size         VARCHAR NOT NULL,
    provider            VARCHAR NOT NULL,
    sent_count          INT NOT NULL DEFAULT 0,
    success_count       INT NOT NULL DEFAULT 0,
    failed_count        INT NOT NULL DEFAULT 0,
    expired_count       INT NOT NULL DEFAULT 0,
    rate_limited_count  INT NOT NULL DEFAULT 0,
    total_latency_ms    BIGINT NOT NULL DEFAULT 0,
    sample_count        INT NOT NULL DEFAULT 0
);

CREATE TABLE analytics_push_patterns (
    id              BIGSERIAL PRIMARY KEY,
    pod_id          VARCHAR NOT NULL,
    tenant_id       VARCHAR NOT NULL,
    bucket_start    TIMESTAMPTZ NOT NULL,
    bucket_size     VARCHAR NOT NULL,
    pattern         VARCHAR NOT NULL,
    match_count     INT NOT NULL DEFAULT 0,
    device_count    INT NOT NULL DEFAULT 0
);

CREATE TABLE analytics_raw_events (
    id              BIGSERIAL PRIMARY KEY,
    pod_id          VARCHAR NOT NULL,
    tenant_id       VARCHAR NOT NULL,
    event_type      VARCHAR NOT NULL,
    event_data      JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ====================
-- INDEXES
-- ====================

-- Tenant keys
CREATE INDEX idx_tenant_keys_tenant_active ON tenant_keys(tenant_id) WHERE is_active = true AND revoked_at IS NULL;
CREATE INDEX idx_tenant_keys_lookup ON tenant_keys(key_id, is_active) WHERE is_active = true AND revoked_at IS NULL;

-- API keys
CREATE INDEX idx_api_keys_tenant_active ON api_keys(tenant_id) WHERE is_active = true AND revoked_at IS NULL;
CREATE INDEX idx_api_keys_lookup ON api_keys(key_id, is_active) WHERE is_active = true AND revoked_at IS NULL;

-- Admin keys
CREATE INDEX idx_admin_keys_active ON admin_keys (key_id) WHERE revoked_at IS NULL;

-- Tenants
CREATE INDEX idx_tenants_status ON tenants(status) WHERE status != 'deleted';
CREATE INDEX idx_tenants_deprovision ON tenants(deprovision_at) WHERE status = 'deprovisioning' AND deprovision_at IS NOT NULL;

-- Audit
CREATE INDEX idx_audit_tenant_time ON provisioning_audit(tenant_id, created_at DESC);

-- Push
CREATE INDEX idx_push_credentials_tenant ON push_credentials(tenant_id);
CREATE INDEX idx_push_subs_tenant ON push_subscriptions(tenant_id);
CREATE INDEX idx_push_subs_tenant_principal ON push_subscriptions(tenant_id, principal);
CREATE INDEX idx_push_subs_channels_gin ON push_subscriptions USING GIN(channels);
CREATE INDEX idx_push_subs_jti ON push_subscriptions (jti);

-- Analytics rollup query indexes
CREATE INDEX idx_analytics_connections_tenant_time ON analytics_connections(tenant_id, bucket_start, bucket_size);
CREATE INDEX idx_analytics_messages_tenant_time ON analytics_messages(tenant_id, bucket_start, bucket_size);
CREATE INDEX idx_analytics_push_tenant_time ON analytics_push(tenant_id, bucket_start, bucket_size);
CREATE INDEX idx_analytics_push_patterns_tenant_time ON analytics_push_patterns(tenant_id, bucket_start, bucket_size);

-- Analytics retention indexes
CREATE INDEX idx_analytics_connections_retention ON analytics_connections(bucket_start, bucket_size);
CREATE INDEX idx_analytics_messages_retention ON analytics_messages(bucket_start, bucket_size);
CREATE INDEX idx_analytics_push_retention ON analytics_push(bucket_start, bucket_size);
CREATE INDEX idx_analytics_push_patterns_retention ON analytics_push_patterns(bucket_start, bucket_size);

-- Analytics debug
CREATE INDEX idx_analytics_raw_events_cleanup ON analytics_raw_events(created_at);
CREATE INDEX idx_analytics_raw_events_tenant ON analytics_raw_events(tenant_id, created_at);

-- ====================
-- UNIQUE CONSTRAINTS (analytics upsert)
-- ====================

ALTER TABLE analytics_connections ADD CONSTRAINT uq_analytics_connections_bucket UNIQUE (pod_id, tenant_id, bucket_start, bucket_size, transport);
ALTER TABLE analytics_messages ADD CONSTRAINT uq_analytics_messages_bucket UNIQUE (pod_id, tenant_id, bucket_start, bucket_size, channel_prefix);
ALTER TABLE analytics_push ADD CONSTRAINT uq_analytics_push_bucket UNIQUE (pod_id, tenant_id, bucket_start, bucket_size, provider);
ALTER TABLE analytics_push_patterns ADD CONSTRAINT uq_analytics_push_patterns_bucket UNIQUE (pod_id, tenant_id, bucket_start, bucket_size, pattern);
ALTER TABLE push_credential_health ADD CONSTRAINT uq_push_credential_health_tenant_provider UNIQUE (tenant_id, provider);
ALTER TABLE subscription_stats ADD CONSTRAINT uq_subscription_stats_tenant_platform UNIQUE (tenant_id, platform);

-- ====================
-- TRIGGERS
-- ====================

CREATE OR REPLACE FUNCTION update_tenant_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_tenant_updated_at
    BEFORE UPDATE ON tenants
    FOR EACH ROW
    EXECUTE FUNCTION update_tenant_updated_at();

CREATE OR REPLACE FUNCTION update_quota_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_quota_updated_at
    BEFORE UPDATE ON tenant_quotas
    FOR EACH ROW
    EXECUTE FUNCTION update_quota_updated_at();

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
