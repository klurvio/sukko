-- Add JWT metadata to push subscriptions for token revocation support.
-- jti: JWT ID for per-token revocation lookup.
-- token_iat: JWT issued-at for per-user revocation comparison (token_iat < revoked_at).
-- No existing registrations — columns are NOT NULL with no migration concerns.

ALTER TABLE push_subscriptions
    ADD COLUMN jti VARCHAR(255) NOT NULL,
    ADD COLUMN token_iat TIMESTAMPTZ NOT NULL;

CREATE INDEX idx_push_subs_jti ON push_subscriptions (jti);
