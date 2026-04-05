-- Provisioning Service Database Schema
-- Version: 003_routing_rules
-- Description: Add tenant routing rules table and tenant ID constraints

-- ====================
-- TABLES
-- ====================

-- Tenant topic routing rules
-- Maps channel suffix patterns to Kafka topic suffixes per tenant.
-- Rules are evaluated in order (first match wins).
CREATE TABLE tenant_routing_rules (
    tenant_id       TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    rules           JSONB NOT NULL DEFAULT '[]',
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL
);

-- Example rules JSONB structure:
-- [
--   {"pattern": "*.trade", "topic_suffix": "trade"},
--   {"pattern": "*.liquidity", "topic_suffix": "liquidity"},
--   {"pattern": "*", "topic_suffix": "default"}
-- ]

COMMENT ON TABLE tenant_routing_rules IS 'Per-tenant topic routing rules mapping channel suffixes to Kafka topic suffixes';
COMMENT ON COLUMN tenant_routing_rules.rules IS 'JSONB array of {pattern, topic_suffix} objects evaluated in order (first match wins)';

-- ====================
-- CONSTRAINTS
-- ====================

-- Tenant IDs must not contain dots (used as separator in channel names)
DO $$ BEGIN
    ALTER TABLE tenants ADD CONSTRAINT chk_tenant_id_no_dots CHECK (id NOT LIKE '%.%');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- Tenant IDs must not start with underscore (reserved for system tenants)
DO $$ BEGIN
    ALTER TABLE tenants ADD CONSTRAINT chk_tenant_id_no_underscore_prefix CHECK (id NOT LIKE E'\\_%');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- ====================
-- CLEANUP
-- ====================

-- Drop tenant_categories table (replaced by routing rules)
DROP TABLE IF EXISTS tenant_categories;
