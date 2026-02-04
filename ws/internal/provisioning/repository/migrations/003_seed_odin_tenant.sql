-- Seed Odin Tenant with Default Categories
-- Version: 003_seed_odin_tenant
-- Description: Provisions the Odin trading platform tenant with its 8 default categories.
--              Categories are stored WITHOUT namespace - topic names built at runtime
--              using KAFKA_TOPIC_NAMESPACE environment variable.

-- ====================
-- ODIN TENANT
-- ====================

-- Insert odin tenant (the default tenant for Odin trading platform)
INSERT INTO tenants (id, name, status, consumer_type, metadata, created_at)
VALUES (
    'odin',
    'Odin Trading Platform',
    'active',
    'shared',
    '{"description": "Default tenant for Odin WebSocket trading platform", "version": "1.0"}',
    NOW()
)
ON CONFLICT (id) DO NOTHING;

-- ====================
-- DEFAULT QUOTAS
-- ====================

-- Insert default quotas for odin tenant
INSERT INTO tenant_quotas (tenant_id, max_topics, max_partitions, max_storage_bytes, producer_byte_rate, consumer_byte_rate)
VALUES (
    'odin',
    50,          -- max topics
    200,         -- max partitions
    10737418240, -- 10GB storage
    10485760,    -- 10MB/s producer rate
    52428800     -- 50MB/s consumer rate
)
ON CONFLICT (tenant_id) DO NOTHING;

-- ====================
-- TENANT CATEGORIES TABLE
-- ====================

-- Create tenant_categories table (stores categories WITHOUT namespace prefix)
-- Topic names are built at runtime: {KAFKA_TOPIC_NAMESPACE}.{tenant_id}.{category}
-- Example: "prod" + "odin" + "trade" -> "prod.odin.trade"
CREATE TABLE IF NOT EXISTS tenant_categories (
    id SERIAL PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    category TEXT NOT NULL,
    partitions INT NOT NULL DEFAULT 3,
    retention_ms BIGINT NOT NULL DEFAULT 604800000,  -- 7 days
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP,
    UNIQUE(tenant_id, category)
);

CREATE INDEX IF NOT EXISTS idx_tenant_categories_tenant
    ON tenant_categories(tenant_id) WHERE deleted_at IS NULL;

-- ====================
-- ODIN TENANT CATEGORIES
-- ====================

-- Insert the 8 default categories for Odin trading platform
-- NOTE: No namespace stored - topic names built at runtime from KAFKA_TOPIC_NAMESPACE

-- Trade events (price updates, order executions)
INSERT INTO tenant_categories (tenant_id, category, partitions, retention_ms, created_at)
VALUES ('odin', 'trade', 3, 604800000, NOW())
ON CONFLICT (tenant_id, category) DO NOTHING;

-- Liquidity events (pool updates, rebalancing)
INSERT INTO tenant_categories (tenant_id, category, partitions, retention_ms, created_at)
VALUES ('odin', 'liquidity', 3, 604800000, NOW())
ON CONFLICT (tenant_id, category) DO NOTHING;

-- Metadata events (token info updates)
INSERT INTO tenant_categories (tenant_id, category, partitions, retention_ms, created_at)
VALUES ('odin', 'metadata', 3, 604800000, NOW())
ON CONFLICT (tenant_id, category) DO NOTHING;

-- Social events (twitter verification, social links)
INSERT INTO tenant_categories (tenant_id, category, partitions, retention_ms, created_at)
VALUES ('odin', 'social', 3, 604800000, NOW())
ON CONFLICT (tenant_id, category) DO NOTHING;

-- Community events (comments, favorites)
INSERT INTO tenant_categories (tenant_id, category, partitions, retention_ms, created_at)
VALUES ('odin', 'community', 3, 604800000, NOW())
ON CONFLICT (tenant_id, category) DO NOTHING;

-- Creation events (new token listings)
INSERT INTO tenant_categories (tenant_id, category, partitions, retention_ms, created_at)
VALUES ('odin', 'creation', 3, 604800000, NOW())
ON CONFLICT (tenant_id, category) DO NOTHING;

-- Analytics events (price deltas, trending)
INSERT INTO tenant_categories (tenant_id, category, partitions, retention_ms, created_at)
VALUES ('odin', 'analytics', 3, 604800000, NOW())
ON CONFLICT (tenant_id, category) DO NOTHING;

-- Balance events (user balance updates)
INSERT INTO tenant_categories (tenant_id, category, partitions, retention_ms, created_at)
VALUES ('odin', 'balances', 3, 604800000, NOW())
ON CONFLICT (tenant_id, category) DO NOTHING;

-- ====================
-- VERIFICATION
-- ====================

-- Verify tenant and categories were created
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM tenants WHERE id = 'odin') THEN
        RAISE EXCEPTION 'Failed to create odin tenant';
    END IF;

    IF (SELECT COUNT(*) FROM tenant_categories WHERE tenant_id = 'odin' AND deleted_at IS NULL) < 8 THEN
        RAISE WARNING 'Expected 8 categories for odin tenant, got %',
            (SELECT COUNT(*) FROM tenant_categories WHERE tenant_id = 'odin' AND deleted_at IS NULL);
    END IF;
END $$;
