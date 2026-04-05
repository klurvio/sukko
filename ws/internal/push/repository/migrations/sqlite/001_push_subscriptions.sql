-- Push subscriptions: per-device push notification registrations.
-- Channels stored as JSON TEXT (SQLite has no native array type).

CREATE TABLE IF NOT EXISTS push_subscriptions (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    tenant_id       TEXT NOT NULL,
    principal       TEXT NOT NULL,
    platform        TEXT NOT NULL,
    token           TEXT,
    endpoint        TEXT,
    p256dh_key      TEXT,
    auth_secret     TEXT,
    channels        TEXT NOT NULL,
    created_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    last_success_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_push_subs_tenant ON push_subscriptions(tenant_id);
CREATE INDEX IF NOT EXISTS idx_push_subs_tenant_principal ON push_subscriptions(tenant_id, principal);
