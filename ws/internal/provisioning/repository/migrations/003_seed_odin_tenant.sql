-- Seed Odin Tenant with Default Categories
-- Version: 003_seed_odin_tenant
-- Description: Provisions the Odin trading platform tenant with its 8 default categories.
--              Categories are now data (not hardcoded in Go code).

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
-- TOPIC CATEGORIES
-- ====================

-- Insert the 8 default topic categories for Odin trading platform
-- Topic name format: {namespace}.{tenant}.{category}
-- Note: Using 'prod' namespace - adjust via environment variable KAFKA_TOPIC_NAMESPACE

-- Trade events (price updates, order executions)
INSERT INTO tenant_topics (tenant_id, topic_name, category, partitions, retention_ms, created_at)
VALUES ('odin', 'prod.odin.trade', 'trade', 3, 604800000, NOW())
ON CONFLICT (topic_name) DO NOTHING;

-- Liquidity events (pool updates, rebalancing)
INSERT INTO tenant_topics (tenant_id, topic_name, category, partitions, retention_ms, created_at)
VALUES ('odin', 'prod.odin.liquidity', 'liquidity', 3, 604800000, NOW())
ON CONFLICT (topic_name) DO NOTHING;

-- Metadata events (token info updates)
INSERT INTO tenant_topics (tenant_id, topic_name, category, partitions, retention_ms, created_at)
VALUES ('odin', 'prod.odin.metadata', 'metadata', 3, 604800000, NOW())
ON CONFLICT (topic_name) DO NOTHING;

-- Social events (twitter verification, social links)
INSERT INTO tenant_topics (tenant_id, topic_name, category, partitions, retention_ms, created_at)
VALUES ('odin', 'prod.odin.social', 'social', 3, 604800000, NOW())
ON CONFLICT (topic_name) DO NOTHING;

-- Community events (comments, favorites)
INSERT INTO tenant_topics (tenant_id, topic_name, category, partitions, retention_ms, created_at)
VALUES ('odin', 'prod.odin.community', 'community', 3, 604800000, NOW())
ON CONFLICT (topic_name) DO NOTHING;

-- Creation events (new token listings)
INSERT INTO tenant_topics (tenant_id, topic_name, category, partitions, retention_ms, created_at)
VALUES ('odin', 'prod.odin.creation', 'creation', 3, 604800000, NOW())
ON CONFLICT (topic_name) DO NOTHING;

-- Analytics events (price deltas, trending)
INSERT INTO tenant_topics (tenant_id, topic_name, category, partitions, retention_ms, created_at)
VALUES ('odin', 'prod.odin.analytics', 'analytics', 3, 604800000, NOW())
ON CONFLICT (topic_name) DO NOTHING;

-- Balance events (user balance updates)
INSERT INTO tenant_topics (tenant_id, topic_name, category, partitions, retention_ms, created_at)
VALUES ('odin', 'prod.odin.balances', 'balances', 3, 604800000, NOW())
ON CONFLICT (topic_name) DO NOTHING;

-- ====================
-- VERIFICATION
-- ====================

-- Verify tenant was created
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM tenants WHERE id = 'odin') THEN
        RAISE EXCEPTION 'Failed to create odin tenant';
    END IF;

    IF (SELECT COUNT(*) FROM tenant_topics WHERE tenant_id = 'odin' AND deleted_at IS NULL) < 8 THEN
        RAISE WARNING 'Expected 8 topics for odin tenant, got %',
            (SELECT COUNT(*) FROM tenant_topics WHERE tenant_id = 'odin' AND deleted_at IS NULL);
    END IF;
END $$;
