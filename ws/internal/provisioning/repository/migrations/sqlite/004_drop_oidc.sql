-- Provisioning Service Database Schema (SQLite)
-- Version: 004_drop_oidc
-- Description: Drop OIDC configuration table and related objects (OIDC removed from Sukko)

-- ====================
-- TRIGGERS
-- ====================

DROP TRIGGER IF EXISTS trigger_oidc_config_updated_at;

-- ====================
-- INDEXES
-- ====================

DROP INDEX IF EXISTS idx_tenant_oidc_issuer_enabled;

-- ====================
-- TABLES
-- ====================

DROP TABLE IF EXISTS tenant_oidc_config;
