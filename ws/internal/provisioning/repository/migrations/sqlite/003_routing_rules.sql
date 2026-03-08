-- Provisioning Service Database Schema (SQLite)
-- Version: 003_routing_rules
-- Description: Add tenant routing rules table and tenant ID constraints

-- ====================
-- TABLES
-- ====================

-- Tenant topic routing rules
CREATE TABLE IF NOT EXISTS tenant_routing_rules (
    tenant_id       TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    rules           TEXT NOT NULL DEFAULT '[]',
    created_at      DATETIME NOT NULL,
    updated_at      DATETIME NOT NULL
);

-- ====================
-- CONSTRAINTS
-- ====================

-- SQLite CHECK constraints for tenant IDs (matching Postgres version).
-- These are added as new columns are not possible; use a trigger or
-- rely on application-level validation. SQLite does not support
-- ALTER TABLE ADD CONSTRAINT, so we validate in the Go layer.
-- The Go service.CreateTenant already rejects dots and underscore prefixes.

-- ====================
-- CLEANUP
-- ====================

-- Drop tenant_categories table (replaced by routing rules)
DROP TABLE IF EXISTS tenant_categories;
