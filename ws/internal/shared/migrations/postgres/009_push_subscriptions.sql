-- Push subscriptions: per-device push notification registrations.
-- Owned exclusively by the push service. Supports 1M+ rows per tenant.

CREATE TABLE push_subscriptions (
    id              SERIAL PRIMARY KEY,
    tenant_id       VARCHAR NOT NULL,
    principal       VARCHAR NOT NULL,
    platform        VARCHAR NOT NULL,
    token           VARCHAR,
    endpoint        VARCHAR,
    p256dh_key      VARCHAR,
    auth_secret     VARCHAR,
    channels        TEXT[] NOT NULL,
    created_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    last_success_at TIMESTAMP
);

CREATE INDEX idx_push_subs_tenant ON push_subscriptions(tenant_id);
CREATE INDEX idx_push_subs_tenant_principal ON push_subscriptions(tenant_id, principal);
CREATE INDEX idx_push_subs_channels_gin ON push_subscriptions USING GIN(channels);
