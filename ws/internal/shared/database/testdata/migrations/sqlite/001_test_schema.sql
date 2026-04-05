-- Test migration for shared database package tests.
CREATE TABLE IF NOT EXISTS test_items (
    id   TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
