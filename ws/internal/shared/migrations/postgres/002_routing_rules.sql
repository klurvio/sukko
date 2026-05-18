-- Migration 002: Replace JSONB-blob routing rules with per-row table.
-- The old table stored rules as a JSONB blob per tenant; this replaces it
-- with a normalized row-per-rule schema supporting topics[], priority, and
-- per-rule ordering required by the channel-topic routing feature.

DROP TABLE IF EXISTS tenant_routing_rules;

CREATE TABLE tenant_routing_rules (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   TEXT        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    pattern     TEXT        NOT NULL,
    topics      TEXT[]      NOT NULL,
    priority    INT         NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT uq_routing_rule_tenant_priority UNIQUE (tenant_id, priority)
);

CREATE INDEX idx_routing_rules_tenant ON tenant_routing_rules (tenant_id, priority ASC);
