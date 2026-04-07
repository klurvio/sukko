-- ====================
-- Admin Keys
-- ====================
-- Stores admin public keys for JWT-based authentication.
-- The bootstrap key is auto-registered on first startup via ADMIN_BOOTSTRAP_KEY env var.
-- Each admin generates an Ed25519 keypair locally and registers the public key here.

CREATE TABLE admin_keys (
    id            SERIAL       PRIMARY KEY,
    key_id        VARCHAR(64)  NOT NULL UNIQUE,
    name          VARCHAR(255) NOT NULL DEFAULT 'unnamed',
    algorithm     VARCHAR(16)  NOT NULL DEFAULT 'Ed25519',
    public_key    TEXT         NOT NULL,
    registered_by VARCHAR(255) NOT NULL DEFAULT 'system',
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    revoked_at    TIMESTAMPTZ
);

COMMENT ON TABLE admin_keys IS 'Admin public keys for JWT authentication. Bootstrap key auto-registered on first startup when no keys exist.';
COMMENT ON COLUMN admin_keys.key_id IS 'Unique key identifier (e.g., bootstrap-0, ak_7xPm3kR9vQ2n). Used as kid in JWT header.';
COMMENT ON COLUMN admin_keys.name IS 'Human-readable admin name (informational, not unique). Used as sub in JWT.';
COMMENT ON COLUMN admin_keys.algorithm IS 'Signing algorithm (Ed25519 or RS256).';
COMMENT ON COLUMN admin_keys.public_key IS 'PEM-encoded public key material.';
COMMENT ON COLUMN admin_keys.registered_by IS 'Identity of the admin who registered this key (key_id or "system" for bootstrap).';
COMMENT ON COLUMN admin_keys.revoked_at IS 'When the key was revoked. NULL = active.';

-- Partial index for active key lookups (used on every admin JWT validation)
CREATE INDEX idx_admin_keys_active ON admin_keys (key_id) WHERE revoked_at IS NULL;
