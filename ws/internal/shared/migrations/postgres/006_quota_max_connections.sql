-- Add max_connections column to tenant_quotas for per-tenant WebSocket connection limits.
ALTER TABLE tenant_quotas ADD COLUMN max_connections INTEGER NOT NULL DEFAULT 0;

COMMENT ON COLUMN tenant_quotas.max_connections IS 'Maximum concurrent WebSocket connections (0 = system default)';
