-- Provisioning Service Database Schema
-- Version: 010_analytics
-- Description: Analytics rollup tables for per-tenant historical data.
--   Rollup-first design: no raw events stored by default.
--   Per-pod writes with pod_id column — queries SUM() at read time.
--   TimescaleDB-compatible: all TIMESTAMPTZ, no PARTITION BY.
--   Latency as weighted average: total_latency_ms + sample_count.

-- ====================
-- ROLLUP TABLES
-- ====================

-- Per-tenant connection analytics by transport type and time bucket.
-- Written by: gateway (future feat/analytics pipeline).
CREATE TABLE analytics_connections (
    id              BIGSERIAL PRIMARY KEY,
    pod_id          VARCHAR NOT NULL,
    tenant_id       VARCHAR NOT NULL,
    bucket_start    TIMESTAMPTZ NOT NULL,
    bucket_size     VARCHAR NOT NULL,
    transport       VARCHAR NOT NULL,
    active_count    INT NOT NULL DEFAULT 0,
    connect_count   INT NOT NULL DEFAULT 0,
    disconnect_count INT NOT NULL DEFAULT 0,
    error_count     INT NOT NULL DEFAULT 0
);

COMMENT ON TABLE analytics_connections IS 'Per-tenant connection rollups by transport type (ws/sse/grpc) and time bucket (minute/hour/day)';
COMMENT ON COLUMN analytics_connections.pod_id IS 'Kubernetes pod identifier — enables per-pod writes with SUM() at query time';
COMMENT ON COLUMN analytics_connections.bucket_start IS 'Start of the time bucket (TimescaleDB-compatible TIMESTAMPTZ)';
COMMENT ON COLUMN analytics_connections.bucket_size IS 'Bucket granularity: minute, hour, or day';
COMMENT ON COLUMN analytics_connections.active_count IS 'Number of active connections at bucket end (gauge, not cumulative)';

-- Per-tenant message throughput by channel prefix and time bucket.
-- Written by: ws-server (future feat/analytics pipeline).
CREATE TABLE analytics_messages (
    id              BIGSERIAL PRIMARY KEY,
    pod_id          VARCHAR NOT NULL,
    tenant_id       VARCHAR NOT NULL,
    bucket_start    TIMESTAMPTZ NOT NULL,
    bucket_size     VARCHAR NOT NULL,
    channel_prefix  VARCHAR NOT NULL,
    published_count INT NOT NULL DEFAULT 0,
    delivered_count INT NOT NULL DEFAULT 0,
    failed_count    INT NOT NULL DEFAULT 0,
    total_latency_ms BIGINT NOT NULL DEFAULT 0,
    sample_count    INT NOT NULL DEFAULT 0
);

COMMENT ON TABLE analytics_messages IS 'Per-tenant message throughput rollups by channel prefix and time bucket';
COMMENT ON COLUMN analytics_messages.total_latency_ms IS 'Sum of all message latencies — divide by sample_count for weighted average across pods';
COMMENT ON COLUMN analytics_messages.sample_count IS 'Number of latency samples — used with total_latency_ms for accurate cross-pod averaging';

-- Per-tenant push delivery stats by provider and time bucket (Enterprise).
-- Written by: push-service (future feat/analytics pipeline).
CREATE TABLE analytics_push (
    id                  BIGSERIAL PRIMARY KEY,
    pod_id              VARCHAR NOT NULL,
    tenant_id           VARCHAR NOT NULL,
    bucket_start        TIMESTAMPTZ NOT NULL,
    bucket_size         VARCHAR NOT NULL,
    provider            VARCHAR NOT NULL,
    sent_count          INT NOT NULL DEFAULT 0,
    success_count       INT NOT NULL DEFAULT 0,
    failed_count        INT NOT NULL DEFAULT 0,
    expired_count       INT NOT NULL DEFAULT 0,
    rate_limited_count  INT NOT NULL DEFAULT 0,
    total_latency_ms    BIGINT NOT NULL DEFAULT 0,
    sample_count        INT NOT NULL DEFAULT 0
);

COMMENT ON TABLE analytics_push IS 'Per-tenant push delivery rollups by provider (fcm/apns/webpush) and time bucket (Enterprise)';

-- Per-tenant push pattern match stats by pattern and time bucket (Enterprise).
-- Written by: push-service (future feat/analytics pipeline).
CREATE TABLE analytics_push_patterns (
    id              BIGSERIAL PRIMARY KEY,
    pod_id          VARCHAR NOT NULL,
    tenant_id       VARCHAR NOT NULL,
    bucket_start    TIMESTAMPTZ NOT NULL,
    bucket_size     VARCHAR NOT NULL,
    pattern         VARCHAR NOT NULL,
    match_count     INT NOT NULL DEFAULT 0,
    device_count    INT NOT NULL DEFAULT 0
);

COMMENT ON TABLE analytics_push_patterns IS 'Per-tenant push pattern match rollups — which patterns trigger the most notifications (Enterprise)';

-- ====================
-- STATE TABLES
-- ====================

-- Per-tenant per-provider push credential health (Enterprise).
-- Updated in place on every delivery attempt.
CREATE TABLE push_credential_health (
    id                      SERIAL PRIMARY KEY,
    tenant_id               VARCHAR NOT NULL,
    provider                VARCHAR NOT NULL,
    status                  VARCHAR NOT NULL DEFAULT 'unknown',
    last_success_at         TIMESTAMPTZ,
    last_failure_at         TIMESTAMPTZ,
    consecutive_failures    INT NOT NULL DEFAULT 0,
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE push_credential_health IS 'Push credential health tracking per tenant per provider — updated on every delivery attempt (Enterprise)';
COMMENT ON COLUMN push_credential_health.status IS 'Credential status: unknown, valid, expired, rate_limited, failed';

-- Per-tenant per-platform subscription count snapshot (Enterprise).
-- Refreshed periodically by push-service.
CREATE TABLE subscription_stats (
    id                  SERIAL PRIMARY KEY,
    tenant_id           VARCHAR NOT NULL,
    platform            VARCHAR NOT NULL,
    total_count         INT NOT NULL DEFAULT 0,
    stale_cleaned_count INT NOT NULL DEFAULT 0,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE subscription_stats IS 'Push subscription count snapshots per tenant per platform (web/android/ios) — refreshed periodically (Enterprise)';

-- ====================
-- OPTIONAL DEBUG TABLE
-- ====================

-- Raw per-event log for temporary debugging. Disabled by default (ANALYTICS_RAW_EVENTS=true).
-- High volume — only enable temporarily. Auto-cleaned after 1 hour by retention job.
CREATE TABLE analytics_raw_events (
    id              BIGSERIAL PRIMARY KEY,
    pod_id          VARCHAR NOT NULL,
    tenant_id       VARCHAR NOT NULL,
    event_type      VARCHAR NOT NULL,
    event_data      JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE analytics_raw_events IS 'Raw per-event debug log — disabled by default, high volume, auto-cleaned after 1 hour';

-- ====================
-- UNIQUE CONSTRAINTS
-- ====================

-- Rollup table upsert constraints — enables ON CONFLICT DO UPDATE for counter accumulation.
ALTER TABLE analytics_connections
    ADD CONSTRAINT uq_analytics_connections_bucket
    UNIQUE (pod_id, tenant_id, bucket_start, bucket_size, transport);

ALTER TABLE analytics_messages
    ADD CONSTRAINT uq_analytics_messages_bucket
    UNIQUE (pod_id, tenant_id, bucket_start, bucket_size, channel_prefix);

ALTER TABLE analytics_push
    ADD CONSTRAINT uq_analytics_push_bucket
    UNIQUE (pod_id, tenant_id, bucket_start, bucket_size, provider);

ALTER TABLE analytics_push_patterns
    ADD CONSTRAINT uq_analytics_push_patterns_bucket
    UNIQUE (pod_id, tenant_id, bucket_start, bucket_size, pattern);

-- State table uniqueness — one row per tenant per dimension.
ALTER TABLE push_credential_health
    ADD CONSTRAINT uq_push_credential_health_tenant_provider
    UNIQUE (tenant_id, provider);

ALTER TABLE subscription_stats
    ADD CONSTRAINT uq_subscription_stats_tenant_platform
    UNIQUE (tenant_id, platform);

-- ====================
-- INDEXES
-- ====================

-- Rollup table query indexes: per-tenant time range lookups.
CREATE INDEX idx_analytics_connections_tenant_time ON analytics_connections(tenant_id, bucket_start, bucket_size);
CREATE INDEX idx_analytics_messages_tenant_time ON analytics_messages(tenant_id, bucket_start, bucket_size);
CREATE INDEX idx_analytics_push_tenant_time ON analytics_push(tenant_id, bucket_start, bucket_size);
CREATE INDEX idx_analytics_push_patterns_tenant_time ON analytics_push_patterns(tenant_id, bucket_start, bucket_size);

-- Rollup table retention indexes: cleanup by time bucket.
CREATE INDEX idx_analytics_connections_retention ON analytics_connections(bucket_start, bucket_size);
CREATE INDEX idx_analytics_messages_retention ON analytics_messages(bucket_start, bucket_size);
CREATE INDEX idx_analytics_push_retention ON analytics_push(bucket_start, bucket_size);
CREATE INDEX idx_analytics_push_patterns_retention ON analytics_push_patterns(bucket_start, bucket_size);

-- Debug table indexes.
CREATE INDEX idx_analytics_raw_events_cleanup ON analytics_raw_events(created_at);
CREATE INDEX idx_analytics_raw_events_tenant ON analytics_raw_events(tenant_id, created_at);
