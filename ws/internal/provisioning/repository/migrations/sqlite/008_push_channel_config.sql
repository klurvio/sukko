-- Push channel config: per-tenant push routing patterns.
-- Patterns stored as JSON TEXT (SQLite has no native array type).

CREATE TABLE IF NOT EXISTS push_channel_configs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    tenant_id       TEXT NOT NULL UNIQUE,
    patterns        TEXT NOT NULL,
    default_ttl     INTEGER NOT NULL DEFAULT 2419200,
    default_urgency TEXT NOT NULL DEFAULT 'normal',
    created_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at      DATETIME NOT NULL DEFAULT (datetime('now'))
);
