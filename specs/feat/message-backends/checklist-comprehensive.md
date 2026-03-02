# Requirements Checklist: Pluggable Message Backends

**Branch**: `feat/message-backends` | **Generated**: 2026-03-01
**Domains**: Concurrency, Deployment, Performance, Security

---

## Concurrency

### Completeness

- [ ] CHK001 Are goroutine lifecycle requirements defined for each backend's `Start()` and `Shutdown()` methods (context cancellation, WaitGroup tracking, panic recovery)? [Completeness]
- [ ] CHK002 Are concurrency requirements specified for `KafkaBackend.ensureTopicsExist()` which is called from the `SetOnUpdate` callback — is it safe to call concurrently with `Publish()` and `Replay()`? [Completeness]
- [ ] CHK003 Are requirements defined for the `JetStreamBackend.streamsMu` lock scope — specifically which operations require read vs write locks and maximum hold duration? [Completeness]
- [ ] CHK004 Are shutdown ordering requirements specified for each backend (cancel context → wait goroutines → close connections → release resources)? [Completeness]

### Clarity

- [ ] CHK005 Is the concurrency model for `DirectBackend.Publish()` calling `bus.Publish()` specified — is it synchronous, and can multiple goroutines call it concurrently? [Clarity]
- [ ] CHK006 Is it specified whether `KafkaBackend.Replay()` can be called concurrently from multiple client reconnect handlers? [Clarity]
- [ ] CHK007 Are requirements for the `JetStreamBackend.refreshLoop()` goroutine specified — ticker interval, shutdown signaling, and interaction with concurrent `Publish()` calls? [Clarity]

### Coverage

- [ ] CHK008 Are edge cases defined for calling `Shutdown()` while `Start()` is still initializing (partial initialization teardown)? [Coverage]
- [ ] CHK009 Are edge cases defined for calling `Publish()` after `Shutdown()` has been called but before it completes? [Coverage]
- [ ] CHK010 Are requirements defined for `JetStreamBackend` consumer goroutine restart after NATS reconnection? [Coverage]

---

## Deployment

### Completeness

- [ ] CHK011 Are init container requirements fully specified — which containers are unconditional, which are conditional on `messageBackend`, and what each waits for? [Completeness]
- [ ] CHK012 Are Helm value defaults documented for all new configuration fields (`messageBackend`, `jetstream.*`, `nats.tls.*`, `valkey.tls.*`)? [Completeness]
- [ ] CHK013 Are requirements defined for what happens when `messageBackend` is changed on a running deployment (rolling update behavior, zero-downtime expectations)? [Completeness]
- [ ] CHK014 Are health check endpoint changes (`"kafka"` → `"backend"` JSON key) documented as potentially breaking for external monitoring that parses the response? [Completeness]

### Config Coherence

- [ ] CHK015 Are all Go `env:` struct tag names consistent with corresponding Helm configmap template variable names for the 10 new NATS JetStream env vars? [Config Coherence]
- [ ] CHK016 Are all Go `env:` struct tag names consistent with corresponding Helm configmap template variable names for the 6 new broadcast bus TLS env vars? [Config Coherence]
- [ ] CHK017 Are `envDefault:` values in Go struct tags consistent with default values in `values.yaml` for all new fields? [Config Coherence]
- [ ] CHK018 Are the 9 Kafka admin config fields being removed from `provisioning_config.go` confirmed to have no references outside provisioning's Kafka admin creation? [Config Coherence]

### Dependencies

- [ ] CHK019 Are infrastructure prerequisites documented for each backend mode — what external services must be running before the pod starts? [Dependencies]
- [ ] CHK020 Are requirements specified for the `wait-for-nats-jetstream` init container — what host/port it checks, and how it differs from the broadcast bus `wait-for-nats`? [Dependencies]
- [ ] CHK021 Are requirements defined for what happens when provisioning gRPC service is unavailable in each backend mode (direct: no impact, kafka: topic discovery delayed, nats: stream creation delayed)? [Dependencies]

### Coverage

- [ ] CHK022 Are rollback requirements defined — can a deployment be rolled back from `messageBackend: direct` to `messageBackend: kafka` without data loss or service disruption? [Coverage]
- [ ] CHK023 Are requirements specified for Helm lint validation of conditional template blocks (`{{ if eq .Values.messageBackend "kafka" }}`)? [Coverage]

---

## Performance

### Completeness

- [ ] CHK024 Are latency requirements for `DirectBackend.Publish()` quantified beyond "zero latency" (NFR-002) — is a specific P99 target defined? [Completeness]
- [ ] CHK025 Are throughput requirements for `KafkaBackend` specified to ensure the abstraction layer does not degrade existing Kafka performance (NFR-003 says < 1 microsecond overhead — is this P50 or P99)? [Completeness]
- [ ] CHK026 Are throughput requirements for `JetStreamBackend` quantified (NFR-004 says 10,000 msg/s — is this per stream, per backend instance, or aggregate)? [Completeness]

### Measurability

- [ ] CHK027 Can NFR-002 ("zero latency compared to current Kafka path") be objectively measured — are baseline Kafka latency numbers documented for comparison? [Measurability]
- [ ] CHK028 Can NFR-003 ("abstraction layer overhead < 1 microsecond") be objectively measured — is a benchmark methodology specified? [Measurability]
- [ ] CHK029 Can NFR-004 ("10,000 messages/second per stream") be objectively measured — are message size, stream configuration, and test conditions specified? [Measurability]

### Coverage

- [ ] CHK030 Are backpressure requirements defined for `JetStreamBackend.Publish()` when JetStream is slow to ack — does it block, timeout, or drop? [Coverage]
- [ ] CHK031 Are requirements defined for `KafkaBackend.Publish()` latency impact when `ensureTopicsExist()` is running concurrently (cold-path vs hot-path isolation)? [Coverage]
- [ ] CHK032 Are memory requirements specified for `JetStreamBackend.Replay()` when replaying up to `DefaultMaxReplayMessages` (100) messages — is buffering bounded? [Coverage]

---

## Security

### Completeness

- [ ] CHK033 Are secret redaction requirements defined for all credential fields in logging — `NATSJetStreamToken`, `NATSJetStreamPassword`, `KafkaSASLPassword` in `LogConfig()` output? [Completeness]
- [ ] CHK034 Are TLS configuration requirements defined for NATS JetStream connections (separate from broadcast bus TLS) — `NATS_JETSTREAM_TLS_ENABLED`, `NATS_JETSTREAM_TLS_INSECURE`, `NATS_JETSTREAM_TLS_CA_PATH`? [Completeness]
- [ ] CHK035 Are requirements defined for what happens when TLS is enabled but the CA path is invalid or the certificate is expired? [Completeness]

### Clarity

- [ ] CHK036 Is tenant isolation in direct mode clearly specified — are channel-level permission checks sufficient when there are no backend-level topic/stream boundaries? [Clarity]
- [ ] CHK037 Is it specified that `NATS_JETSTREAM_TLS_INSECURE` should only be used in development — are warnings logged when it is enabled? [Clarity]
- [ ] CHK038 Are the security implications of removing `KafkaAdmin` from provisioning documented — specifically that topic creation auth moves to ws-server's `KafkaBackend`? [Clarity]

### Coverage

- [ ] CHK039 Are requirements defined for credential rotation — can NATS JetStream tokens and passwords be rotated without pod restart? [Coverage]
- [ ] CHK040 Are requirements defined for `JetStreamBackend` stream-per-tenant isolation — can one tenant's stream operations (create, delete, publish) affect another tenant's streams? [Coverage]
- [ ] CHK041 Are input validation requirements defined for the `MESSAGE_BACKEND` env var — is it validated at startup against the allowed set with a clear error on invalid values? [Coverage]

### Consistency

- [ ] CHK042 Are TLS configuration patterns consistent across all three TLS-enabled components (NATS broadcast bus, Valkey broadcast bus, NATS JetStream backend) — same field names, same behavior for `Insecure` flag, same CA loading logic? [Consistency]
- [ ] CHK043 Are secret handling requirements consistent between Kafka mode (existing SASL password handling) and NATS JetStream mode (new token/password handling)? [Consistency]

---

## Summary

| Domain | Items | Focus Areas |
|--------|-------|-------------|
| Concurrency | 10 | Backend lifecycle, shutdown ordering, concurrent access to shared state |
| Deployment | 13 | Config coherence, init containers, rolling updates, rollback |
| Performance | 9 | NFR measurability, backpressure, hot-path isolation |
| Security | 11 | TLS consistency, secret redaction, tenant isolation, credential rotation |
| **Total** | **43** | |
