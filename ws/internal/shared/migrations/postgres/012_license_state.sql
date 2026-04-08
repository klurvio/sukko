-- ====================
-- License State
-- ====================
-- Single-row table storing the most recent license key (encrypted with AES-256-GCM).
-- Persists webhook-pushed license keys across provisioning restarts.
-- Startup precedence: DB → SUKKO_LICENSE_KEY env var.

CREATE TABLE license_state (
    id              INT          PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    encrypted_key   TEXT         NOT NULL,
    edition         VARCHAR(16)  NOT NULL,
    org             VARCHAR(255) NOT NULL DEFAULT '',
    expires_at      TIMESTAMPTZ,
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE license_state IS 'Single-row table storing the most recent license key (encrypted). Survives restarts.';
COMMENT ON COLUMN license_state.id IS 'Fixed to 1 — only one active license per deployment.';
COMMENT ON COLUMN license_state.encrypted_key IS 'AES-256-GCM encrypted license key string (nonce + ciphertext + tag, base64).';
COMMENT ON COLUMN license_state.edition IS 'Edition from the license claims (pro, enterprise). Informational — the encrypted key is the source of truth.';
COMMENT ON COLUMN license_state.org IS 'Organization name from the license claims. Informational.';
COMMENT ON COLUMN license_state.expires_at IS 'Expiration from the license claims. Informational.';
COMMENT ON COLUMN license_state.updated_at IS 'When the license was last pushed/reloaded.';
