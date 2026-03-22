-- Add max_connections column to tenant_quotas for per-tenant WebSocket connection limits.
ALTER TABLE tenant_quotas ADD COLUMN max_connections INTEGER NOT NULL DEFAULT 0;
