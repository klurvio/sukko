-- Provisioning Service Database Schema
-- Version: 001_initial
-- Description: Initial schema for tenant management, keys, topics, quotas, and audit

-- ====================
-- ENUMS
-- ====================

-- Tenant lifecycle states
CREATE TYPE tenant_status AS ENUM ('active', 'suspended', 'deprovisioning', 'deleted');

-- Consumer group assignment type
CREATE TYPE consumer_type AS ENUM ('shared', 'dedicated');

-- ====================
-- TABLES
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
    deprovision_at  TIMESTAMPTZ,          -- When grace period ends, deletion will occur
    deleted_at      TIMESTAMPTZ,

    -- Tenant ID validation: lowercase alphanumeric + hyphens, 3-63 chars
    CONSTRAINT valid_tenant_id CHECK (id ~ '^[a-z][a-z0-9-]{2,62}$'),
    CONSTRAINT name_not_empty CHECK (char_length(name) > 0),
    CONSTRAINT name_max_length CHECK (char_length(name) <= 256)
);

COMMENT ON TABLE tenants IS 'Registry of all tenants in the system';
COMMENT ON COLUMN tenants.id IS 'Unique tenant identifier (e.g., "acme")';
COMMENT ON COLUMN tenants.status IS 'Lifecycle state: active, suspended, deprovisioning, deleted';
COMMENT ON COLUMN tenants.consumer_type IS 'Kafka consumer group assignment: shared or dedicated';
COMMENT ON COLUMN tenants.deprovision_at IS 'Timestamp when grace period ends and final deletion occurs';

-- Tenant public keys for JWT validation
CREATE TABLE tenant_keys (
    key_id          TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    algorithm       TEXT NOT NULL,
    public_key      TEXT NOT NULL,
    is_active       BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ,
    revoked_at      TIMESTAMPTZ,

    -- Key ID validation: same pattern as tenant ID
    CONSTRAINT valid_key_id CHECK (key_id ~ '^[a-z][a-z0-9-]{2,62}$'),
    -- Algorithm must be one of supported values
    CONSTRAINT valid_algorithm CHECK (algorithm IN ('ES256', 'RS256', 'EdDSA')),
    -- Public key must not be empty
    CONSTRAINT public_key_not_empty CHECK (char_length(public_key) > 0)
);

COMMENT ON TABLE tenant_keys IS 'Public keys for JWT validation, read by WS Gateway';
COMMENT ON COLUMN tenant_keys.key_id IS 'Key identifier, used as "kid" in JWT header';
COMMENT ON COLUMN tenant_keys.algorithm IS 'JWT signing algorithm: ES256, RS256, or EdDSA';
COMMENT ON COLUMN tenant_keys.is_active IS 'Whether key is currently valid for verification';
COMMENT ON COLUMN tenant_keys.revoked_at IS 'Timestamp when key was revoked (NULL if active)';

-- Tenant topic categories (category only, no namespace prefix)
-- Full topic names are built at runtime: kafka.BuildTopicName(namespace, tenantID, category)
-- Example: namespace="prod", tenant="sukko", category="trade" -> "prod.sukko.trade"
CREATE TABLE tenant_categories (
    id              SERIAL PRIMARY KEY,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    category        TEXT NOT NULL,
    partitions      INT NOT NULL DEFAULT 3,
    retention_ms    BIGINT NOT NULL DEFAULT 604800000,  -- 7 days
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ,

    UNIQUE(tenant_id, category)
);

COMMENT ON TABLE tenant_categories IS 'Kafka topic categories per tenant (category only, topic names built at runtime)';
COMMENT ON COLUMN tenant_categories.category IS 'Category name (e.g., "trade", "balances") - NOT full topic name';
COMMENT ON COLUMN tenant_categories.partitions IS 'Number of Kafka partitions for this category';
COMMENT ON COLUMN tenant_categories.retention_ms IS 'Kafka message retention in milliseconds';

-- Resource quotas per tenant
CREATE TABLE tenant_quotas (
    tenant_id               TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    max_topics              INT NOT NULL DEFAULT 50,
    max_partitions          INT NOT NULL DEFAULT 200,
    max_storage_bytes       BIGINT NOT NULL DEFAULT 10737418240,  -- 10GB
    producer_byte_rate      BIGINT NOT NULL DEFAULT 10485760,     -- 10MB/s
    consumer_byte_rate      BIGINT NOT NULL DEFAULT 52428800,     -- 50MB/s
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT valid_max_topics CHECK (max_topics >= 1),
    CONSTRAINT valid_max_partitions CHECK (max_partitions >= 1),
    CONSTRAINT valid_max_storage CHECK (max_storage_bytes >= 0),
    CONSTRAINT valid_producer_rate CHECK (producer_byte_rate >= 0),
    CONSTRAINT valid_consumer_rate CHECK (consumer_byte_rate >= 0)
);

COMMENT ON TABLE tenant_quotas IS 'Resource quotas per tenant';
COMMENT ON COLUMN tenant_quotas.max_topics IS 'Maximum number of Kafka topics';
COMMENT ON COLUMN tenant_quotas.max_partitions IS 'Maximum total partitions across all topics';
COMMENT ON COLUMN tenant_quotas.max_storage_bytes IS 'Maximum storage usage in bytes';
COMMENT ON COLUMN tenant_quotas.producer_byte_rate IS 'Max producer throughput in bytes/sec';
COMMENT ON COLUMN tenant_quotas.consumer_byte_rate IS 'Max consumer throughput in bytes/sec';

-- Audit log (append-only)
CREATE TABLE provisioning_audit (
    id              BIGSERIAL PRIMARY KEY,
    tenant_id       TEXT,  -- May be NULL for system-wide actions
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

COMMENT ON TABLE provisioning_audit IS 'Immutable audit log for all provisioning actions';
COMMENT ON COLUMN provisioning_audit.action IS 'Action performed (e.g., "create_tenant", "revoke_key")';
COMMENT ON COLUMN provisioning_audit.actor IS 'Who performed the action (user principal or system)';
COMMENT ON COLUMN provisioning_audit.actor_type IS 'Type of actor: user, system, or api_key';

-- ====================
-- INDEXES
-- ====================

-- Tenant keys: lookup by tenant (active keys only)
CREATE INDEX idx_tenant_keys_tenant_active ON tenant_keys(tenant_id)
    WHERE is_active = true AND revoked_at IS NULL;

-- Tenant keys: lookup by key_id for JWT validation (fast path)
CREATE INDEX idx_tenant_keys_lookup ON tenant_keys(key_id, is_active)
    WHERE is_active = true AND revoked_at IS NULL;

-- Tenant categories: lookup by tenant (active only)
CREATE INDEX idx_tenant_categories_tenant ON tenant_categories(tenant_id)
    WHERE deleted_at IS NULL;

-- Audit log: lookup by tenant and time (for audit queries)
CREATE INDEX idx_audit_tenant_time ON provisioning_audit(tenant_id, created_at DESC);

-- Tenants: filter by status (for admin queries)
CREATE INDEX idx_tenants_status ON tenants(status)
    WHERE status != 'deleted';

-- Tenants: find tenants ready for deletion (scheduled deletion job)
CREATE INDEX idx_tenants_deprovision ON tenants(deprovision_at)
    WHERE status = 'deprovisioning' AND deprovision_at IS NOT NULL;

-- ====================
-- TRIGGERS
-- ====================

-- Auto-update updated_at on tenant changes
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

-- Auto-update updated_at on quota changes
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

-- ====================
-- INITIAL DATA
-- ====================

-- No initial data - tenants are provisioned via API
