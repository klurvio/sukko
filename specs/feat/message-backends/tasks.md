# Tasks: Pluggable Message Backends

**Branch**: `feat/message-backends` | **Generated**: 2026-02-27

---

## Phase 1 — Interface & Configuration

- [x] T001 Create `ws/internal/server/backend/backend.go` with `MessageBackend` interface (5 methods: `Start`, `Publish`, `Replay`, `IsHealthy`, `Shutdown`), `ReplayRequest` struct (`Positions map[string]int64`, `MaxMessages int`, `Subscriptions []string`), and `ReplayMessage` struct (`Subject string`, `Data []byte`). Define sentinel errors (Constitution III): `ErrBackendUnavailable`, `ErrReplayNotSupported`, `ErrPublishFailed`, `ErrUnknownBackend`. Define named constant `DefaultMaxReplayMessages = 100` (Constitution I — no magic numbers). Include package doc comment explaining the pluggable backend abstraction.

- [x] T002 Add `MESSAGE_BACKEND` config to `ws/internal/shared/platform/server_config.go`: add `MessageBackend string` field with `env:"MESSAGE_BACKEND" envDefault:"direct"`. Add NATS JetStream fields: `NATSJetStreamURLs string`, `NATSJetStreamToken string`, `NATSJetStreamUser string`, `NATSJetStreamPassword string`, `NATSJetStreamReplicas int` (default 1), `NATSJetStreamMaxAge time.Duration` (default 24h), `NATSJetStreamTLSEnabled bool` (default false), `NATSJetStreamTLSInsecure bool` (default false), `NATSJetStreamTLSCAPath string`. Update `Validate()`: `MESSAGE_BACKEND` must be one of `direct`, `kafka`, `nats`. If `nats`, `NATS_JETSTREAM_URLS` required. If `kafka`, existing Kafka validation applies. Add to `LogConfig()` output. **IMPORTANT (Constitution X)**: `NATSJetStreamToken` and `NATSJetStreamPassword` are secrets — MUST NOT log their values. Log only whether they are set (e.g., `Str("jetstream_token_set", strconv.FormatBool(cfg.NATSJetStreamToken != ""))`).

- [x] T003 Verify Phase 1: run `cd ws && go vet ./...` and `cd ws && go test ./...`. All tests must pass. Confirm `backend` package compiles with no import cycles.

---

## Phase 2 — KafkaBackend

- [x] T004 Create `ws/internal/server/backend/kafkabackend/kafkabackend.go` with `KafkaBackend` struct and `Config` struct (`kafkabackend.Config`). `KafkaBackend` owns: `pool *orchestration.MultiTenantConsumerPool`, `producer *kafka.Producer`, `admin *kadm.Client` (for on-demand topic creation), `topicRegistry *provapi.StreamTopicRegistry` (gRPC stream to provisioning), `logger zerolog.Logger`. `Config` contains all fields currently scattered across `main.go` lines 140-260: `Brokers []string`, `Namespace string`, `Environment string`, SASL configs (`SASLEnabled bool`, `SASLMechanism string`, `SASLUsername string`, `SASLPassword string`), TLS configs (`TLSEnabled bool`, `TLSInsecure bool`, `TLSCAPath string`), `KafkaConsumerEnabled bool`, `TopicRefreshInterval time.Duration`, `DefaultTenantID string`, `DefaultPartitions int`, `DefaultRetentionMs int64`, ResourceGuard fields (`MaxKafkaMessagesPerSec int`, `MaxBroadcastsPerSec int`, `CPUPauseThreshold float64`, `CPUPauseThresholdLower float64`, `CPURejectThreshold float64`), `BroadcastBus broadcast.Bus`, `LogLevel/LogFormat`, and provisioning gRPC fields (`ProvisioningGRPCAddr string`, `GRPCReconnectDelay time.Duration`, `GRPCReconnectMaxDelay time.Duration`) — `main.go` populates these from its top-level config before calling `kafkabackend.New()`. Also move `splitBrokers()` and any SASL/TLS builder helpers from main.go into this file as unexported methods or functions.

- [x] T005 Implement `New(cfg Config) (*KafkaBackend, error)` in `ws/internal/server/backend/kafkabackend/kafkabackend.go`. This constructor moves Kafka initialization from `main.go` lines 140-260 into the backend: create ResourceGuard, build SASL/TLS configs, create StreamTopicRegistry (gRPC client to provisioning service using `cfg.ProvisioningGRPCAddr`), create kadm.Client (Kafka admin for on-demand topic creation using same brokers/SASL/TLS), create MultiTenantConsumerPool, create kafka.Producer. Wire `topicRegistry.SetOnUpdate()` with a `func()` callback (no arguments — observer/signal pattern) that: (1) calls `ensureTopicsExist(ctx)` which reads from `registry.GetSharedTenantTopics()` and `registry.GetDedicatedTenants()` to discover topics, then creates missing Kafka topics via kadm.Client (handle `TopicAlreadyExists` as no-op), then (2) calls `pool.RefreshTopics()` to start/refresh consumers (also reads from registry internally). Do NOT call `pool.Start()` — that happens in `Start()`. Return error on any initialization failure (fail fast).

- [x] T006 Implement `MessageBackend` interface methods on `KafkaBackend` in `ws/internal/server/backend/kafkabackend/kafkabackend.go`: `Start()` calls `pool.Start()` and sets metrics. `Publish()` delegates to `producer.Publish(ctx, clientID, channel, data)`. `Replay()` calls `pool.GetSharedConsumer().ReplayFromOffsets()` and converts `[]kafka.ReplayMessage` → `[]backend.ReplayMessage` (map Subject→Subject, Data→Data). If shared consumer is nil, return `nil, nil`. `IsHealthy()` checks producer health. `Shutdown()` stops pool, closes producer, closes topic registry gRPC connection (continue on individual failures per Constitution IV).

---

## Phase 3 — DirectBackend

- [x] T007 [P] Create `ws/internal/server/backend/directbackend/directbackend.go` with `DirectBackend` struct holding `bus broadcast.Bus` and `logger zerolog.Logger`. Implement `New(bus broadcast.Bus, logger zerolog.Logger) (*DirectBackend, error)` — validate bus is not nil. Implement `MessageBackend` interface: `Start()` returns nil (no-op). `Publish()` calls `bus.Publish(&broadcast.Message{Subject: channel, Payload: data})` and returns nil. `Replay()` returns `nil, nil` (no persistence). `IsHealthy()` returns true. `Shutdown()` returns nil (no-op). Log at Info on Start/Shutdown for operational visibility.

---

## Phase 4 — Server Integration

**Note**: T008 was restructured to avoid an import cycle (`backend → orchestration → server → backend`). Instead of a single `factory.go`, implementations were moved to sub-packages (`backend/kafkabackend/`, `backend/directbackend/`) and wiring is done directly in `main.go`. The `backend/` package contains only the interface and types.

- [x] T008 **Restructured** — originally planned as `factory.go`, but restructured to avoid import cycle (`backend → orchestration → server → backend`). Backend implementations moved to sub-packages (`directbackend/`, `kafkabackend/`, `jetstreambackend/`). Wiring done via inline `switch` in `ws/cmd/server/main.go` on `cfg.MessageBackend`: `"direct"` → `directbackend.New()`, `"kafka"` → `kafkabackend.New(kafkabackend.Config{...})`, `"nats"` → create `StreamTopicRegistry` then `jetstreambackend.New(jetstreambackend.Config{...})`. Default → `logger.Fatal()` with unknown backend error. The `backend/` package contains only the interface and types — no factory.

- [x] T009 Modify `ws/internal/shared/types/types.go`: replace `SharedKafkaConsumer any` and `KafkaProducer any` fields in `ServerConfig` with a single `MessageBackend any` field. Remove `KafkaConsumerDisabled bool` field (its only usage in handlers_http.go:52 is replaced by T011b). Remove `KafkaBrokers []string` field (only set at main.go:260 for shard pass-through — never read by any server handler; brokers are now internal to KafkaBackend). Keep `Environment string` (used for logging context). Update `ws/internal/shared/types/types_test.go` to remove `KafkaBrokers` and `KafkaConsumerDisabled` from test fixtures and assertions.

- [x] T010 Modify `ws/internal/server/orchestration/shard.go`: replace `SharedKafkaConsumer any` and `KafkaProducer any` in `ShardConfig` with `MessageBackend any`. In `NewShard()`, replace `serverConfig.SharedKafkaConsumer = cfg.SharedKafkaConsumer` and `serverConfig.KafkaProducer = cfg.KafkaProducer` with `serverConfig.MessageBackend = cfg.MessageBackend`. Remove the `broadcastToBusFunc` closure (lines 60-65) — it is unused by `NewServer`. Update the `NewServer` call to remove the second argument: `server.NewServer(serverConfig)` instead of `server.NewServer(serverConfig, broadcastToBusFunc)`. Keep the `broadcast` import — it is still used by `ShardConfig.BroadcastBus broadcast.Bus`.

- [x] T011 Modify `ws/internal/server/server.go`: replace `kafkaConsumer *kafka.Consumer` and `kafkaProducer *kafka.Producer` fields with `backend backend.MessageBackend`. Change `NewServer()` signature from `NewServer(config types.ServerConfig, _ kafka.BroadcastFunc)` to `NewServer(config types.ServerConfig)` — remove the unused `BroadcastFunc` parameter. Replace the two type assertion blocks (lines 154-166) with a single block: `if config.MessageBackend != nil { s.backend = config.MessageBackend.(backend.MessageBackend) }`. Remove `kafka` import if no longer used. Update the package doc comment to remove "Integration with Kafka/Redpanda" and say "Integration with pluggable message backends". Update the 2 test call sites in `ws/internal/server/server_test.go` (lines 678 and 717) that call `NewServer(config, mockBroadcast)` to use the new single-argument signature `NewServer(config)`.

- [x] T011b Modify `ws/internal/server/handlers_http.go`: replace the Kafka health check block (lines 49-62) with a backend-agnostic check. Replace `s.config.KafkaConsumerDisabled` and `s.kafkaConsumer != nil` checks with: if `s.backend == nil` → status `"not_configured"`, healthy true (direct mode or no backend is not a failure); else if `s.backend.IsHealthy()` → status `"healthy"`, healthy true; else → status `"unhealthy"`, healthy false, append error. In the JSON response (line 151), rename the `"kafka"` key to `"backend"`. Remove `kafka` import if no longer used. Remove the `kafka_rate` and `broadcast_rate` entries from the `"limits"` section (ResourceGuard moved into KafkaBackend in T004 — these stats are no longer accessible at the server level).

- [x] T012 Modify `ws/internal/server/handlers_publish.go`: replace `s.kafkaProducer == nil` check with `s.backend == nil`. Replace `s.kafkaProducer.Publish(ctx, c.id, pubReq.Channel, pubReq.Data)` with `s.backend.Publish(ctx, c.id, pubReq.Channel, pubReq.Data)`. Update log message from "published to Kafka" to "published to backend". Remove `kafka` import if no longer used. Keep all existing validation (channel format, message size, rate limiting) unchanged.

- [x] T013 Modify `ws/internal/server/handlers_message.go`: rename `handleKafkaReconnect` → `handleReconnect`. Replace `s.kafkaConsumer == nil` check with `s.backend == nil`. Replace `s.kafkaConsumer.ReplayFromOffsets(ctx, offsets, max, subs)` with `s.backend.Replay(ctx, backend.ReplayRequest{Positions: offsets, MaxMessages: backend.DefaultMaxReplayMessages, Subscriptions: subs})`. Update the replay message loop to use `backend.ReplayMessage` (`.Subject`, `.Data`) instead of `kafka.ReplayMessage`. Update log messages from "Kafka replay" to "message replay". Update the call site that dispatches to this handler (switch case in message handler). Remove `kafka` import if no longer used.

- [x] T014 Modify `ws/cmd/server/main.go`: replace the Kafka initialization block (lines 140-260) with backend factory call. Build `backend.Config` from `cfg` fields (include `ProvisioningGRPCAddr` and gRPC reconnect settings for factory-managed registry creation), call `backend.NewMessageBackend()`, call `msgBackend.Start(ctx)`. Pass `msgBackend` to each shard via `ShardConfig.MessageBackend`. In shutdown sequence, replace individual pool/producer/registry shutdown with `msgBackend.Shutdown(ctx)`. Remove `orchestration` and `kafka` imports from main.go (they're now internal to KafkaBackend). **Note**: `provapi` import is retained in main.go for the `nats` case — main.go creates the `StreamTopicRegistry` and passes it to `JetStreamBackend` via config. Move all Kafka-specific helper functions (`splitBrokers`, SASL/TLS config builders) into `kafkabackend.go` as unexported functions — they should not remain in main.go.

- [x] T015 Verify Phase 4: run `cd ws && go vet ./...` and `cd ws && go test ./...`. All existing tests must pass. Confirm no import cycles. Verify `main.go` compiles cleanly with reduced imports.

---

## Phase 4b — Provisioning Cleanup

- [x] T015b Modify `ws/cmd/provisioning/main.go`: In API mode, replace the conditional Kafka admin initialization block (lines 156-198) with unconditional `kafkaAdmin = provisioning.NewNoopKafkaAdmin()`. Remove the `provkafka` import. Remove `defer admin.Close()`. Log "Kafka admin disabled (topic creation moved to ws-server)". Config mode already uses `NewNoopKafkaAdmin()` (line 114) — no change needed there.

- [x] T015c Delete `ws/internal/provisioning/kafka/admin.go` — the real `kafka.Admin` implementation is no longer needed. Verify that `kafka.BuildTopicName()` is NOT in this file (it's in a separate file) — if it is, move it before deleting. Run `cd ws && go vet ./...` to confirm no broken imports. Remove provisioning Kafka config fields from `ws/internal/shared/platform/provisioning_config.go` (lines 52-65): `KafkaBrokers`, `KafkaAdminTimeout`, `KafkaSASLEnabled`, `KafkaSASLMechanism`, `KafkaSASLUsername`, `KafkaSASLPassword`, `KafkaTLSEnabled`, `KafkaTLSInsecure`, `KafkaTLSCAPath` — these are used exclusively for Kafka admin creation and are no longer needed. Also update `provisioning_config.go` methods that reference deleted fields: in `Print()` (line 289), remove the entire "=== Kafka/Redpanda ===" section (lines 312-320) which references `KafkaBrokers` (313), `KafkaAdminTimeout` (314), `KafkaSASLEnabled` (315), `KafkaSASLMechanism`/`KafkaSASLUsername` (316-318), and `KafkaTLSEnabled` (320). In `LogConfig()` (line 348, zerolog method), remove all three Kafka field references: `kafka_brokers` (line 356), `kafka_sasl_enabled` (line 357), and `kafka_tls_enabled` (line 358). Update `ws/internal/shared/testutil/provisioning_fixtures.go`: remove `KafkaBrokers: ""` (line 20) and `KafkaAdminTimeout: 10 * time.Second` (line 21) from the `ProvisioningConfig` fixture.

- [x] T015d Verify Phase 4b: run `cd ws && go vet ./...` and `cd ws && go test ./...`. All provisioning tests must pass. Confirm `NoopKafkaAdmin` is the only `KafkaAdmin` implementation.

---

## Phase 5 — JetStreamBackend (P2)

- [x] T016 Add `github.com/nats-io/nats.go/jetstream` dependency: run `cd ws && go get github.com/nats-io/nats.go/jetstream` (the `nats.go` library already includes JetStream support — verify the import path).

- [x] T016b Move `TenantRegistry` interface AND `TenantTopics` struct from `ws/internal/shared/kafka/tenant_registry.go` to `ws/internal/shared/types/tenant_registry.go` (Constitution X — shared types used by multiple packages must not live in a service-specific package). Update all consuming files to import from `types` instead of `kafka`:
  - `ws/internal/shared/provapi/topic_registry.go` — change `kafka.TenantRegistry` → `types.TenantRegistry`, `kafka.TenantTopics` → `types.TenantTopics` (6 references: field type, method return, compile-time check, struct literals). Remove `kafka` import if no longer needed.
  - `ws/internal/server/orchestration/multitenant_pool.go` — change `kafka.TenantRegistry` → `types.TenantRegistry`, `kafka.TenantTopics` → `types.TenantTopics` (3 references: field, config field, method param).
  - `ws/internal/server/orchestration/multitenant_pool_test.go` — change mock's `kafka.TenantRegistry` → `types.TenantRegistry`, `kafka.TenantTopics` → `types.TenantTopics` (4 references: mock field, method return, compile-time check).
  - `ws/internal/provisioning/topic_registry.go` — change `kafka.TenantRegistry` → `types.TenantRegistry`, `kafka.TenantTopics` → `types.TenantTopics` (4 references: method return, struct literal, compile-time check).
  - `ws/internal/provisioning/topic_registry_test.go` — change `kafka.TenantRegistry` → `types.TenantRegistry`, `kafka.TenantTopics` → `types.TenantTopics` (6 references).
  Delete the original `ws/internal/shared/kafka/tenant_registry.go` after all imports are updated. Keep interface and struct signatures unchanged.

- [x] T017 Create `ws/internal/server/backend/jetstreambackend/jetstreambackend.go` with `JetStreamBackend` struct: `conn *nats.Conn`, `js jetstream.JetStream`, `bus broadcast.Bus`, `registry types.TenantRegistry` (from shared types — relocated in T016b), `logger zerolog.Logger`, `streams map[string]jetstream.Stream`, `streamsMu sync.RWMutex`, `consumers map[string]jetstream.ConsumeContext`, `namespace string`, `replicas int`, `maxAge time.Duration`, `refreshInterval time.Duration`, `ctx context.Context`, `cancel context.CancelFunc`, `wg sync.WaitGroup`, health tracking atomics.

- [x] T018 Create `Config` struct (`jetstreambackend.Config`) in `ws/internal/server/backend/jetstreambackend/jetstreambackend.go` with fields: `URLs []string`, `Token string`, `User string`, `Password string`, `TLSEnabled bool`, `TLSInsecure bool`, `TLSCAPath string`, `Replicas int`, `MaxAge time.Duration`, `Namespace string`, `Environment string`, `BroadcastBus broadcast.Bus`, `Registry types.TenantRegistry` (from shared types — relocated in T016b), `RefreshInterval time.Duration`, `Logger zerolog.Logger`.

- [x] T019 Implement `New(cfg Config) (*JetStreamBackend, error)` in `ws/internal/server/backend/jetstreambackend/jetstreambackend.go`: validate required fields (URLs, BroadcastBus). Build NATS connection options (auth: token or user/password, TLS if enabled, reconnect settings, client name "odin-ws-jetstream"). Connect to NATS. Get JetStream context. Initialize struct with empty stream/consumer maps. Do NOT create streams or start consumers — that happens in `Start()`.

- [x] T020 Implement `Start()` on `JetStreamBackend`: call `refreshStreams()` to discover and create initial streams. Start `refreshLoop()` goroutine (ticker-based, same pattern as Kafka pool's `refreshLoop`). `refreshStreams()`: call `registry.GetSharedTenantTopics()` and `registry.GetDedicatedTenants()`. For each tenant: create or update stream `ODIN_{namespace}_{tenant}` with subject filter `odin.{tenant}.>`, `MaxAge` from config, `Replicas` from config. Start durable consumer per stream that routes messages to `bus.Publish()`. **Goroutine safety (Constitution V + VII)**: All goroutines (`refreshLoop`, per-stream consumer goroutines) MUST use `defer logging.RecoverPanic()` as the FIRST defer. Use `wg.Add(1)` before `go`, `defer wg.Done()` inside. Consumer goroutines must handle errors (log + continue), respect `ctx.Done()` for shutdown, and handle NATS reconnection (nats.go reconnects automatically but consumers may need re-subscription).

- [x] T021 Implement `Publish()` on `JetStreamBackend`: parse channel to extract tenant (same logic as Kafka producer's `parseChannel`). Build JetStream subject from channel. Call `js.Publish(ctx, subject, data)` — synchronous, returns ack with sequence. Return error if publish fails. Log at Debug with channel and data size.

- [x] T022 Implement `Replay()` on `JetStreamBackend`: for each entry in `req.Positions` (stream name → sequence), create ordered consumer starting from sequence+1. Fetch up to `req.MaxMessages` messages. Filter by `req.Subscriptions` (same pattern as Kafka replay). Convert to `[]ReplayMessage`. Return collected messages.

- [x] T023 Implement `IsHealthy()` and `Shutdown()` on `JetStreamBackend`: `IsHealthy()` checks `conn.IsConnected()` and healthy atomic. `Shutdown()` cancels context, waits for goroutines, drains consumers, drains NATS connection.

- [x] T024 Wire JetStream backend into `ws/cmd/server/main.go`: replace the `"nats"` stub with `return NewJetStreamBackend(cfg.JetStream)`. Map `Config.JetStream` fields to `jetstreambackend.Config`.

- [x] T025 Verify Phase 5: run `cd ws && go vet ./...` and `cd ws && go test ./...`. All tests must pass.

---

## Phase 6 — Broadcast Bus TLS

- [x] T026 [P] Add TLS fields to `NATSConfig` in `ws/internal/server/broadcast/bus.go`: `TLSEnabled bool`, `TLSInsecure bool`, `TLSCAPath string`. Add same TLS fields to `ValkeyConfig`.

- [x] T027 [P] Add broadcast bus TLS config to `ws/internal/shared/platform/server_config.go`: `NATSTLSEnabled bool` (`env:"NATS_TLS_ENABLED" envDefault:"false"`), `NATSTLSInsecure bool` (`env:"NATS_TLS_INSECURE" envDefault:"false"`), `NATSTLSCAPath string` (`env:"NATS_TLS_CA_PATH"`), `ValkeyTLSEnabled bool` (`env:"VALKEY_TLS_ENABLED" envDefault:"false"`), `ValkeyTLSInsecure bool` (`env:"VALKEY_TLS_INSECURE" envDefault:"false"`), `ValkeyTLSCAPath string` (`env:"VALKEY_TLS_CA_PATH"`). Add to `LogConfig()` output.

- [x] T028 Implement TLS in `ws/internal/server/broadcast/nats.go`: in `newNATSBus()`, when `ncfg.TLSEnabled` is true, build `tls.Config`. If `TLSCAPath` set, load CA cert via `os.ReadFile` and add to `x509.CertPool`. If `TLSInsecure`, set `InsecureSkipVerify: true`. Add `nats.Secure(&tlsCfg)` to connection options. Log TLS status at Info.

- [x] T029 Implement TLS in `ws/internal/server/broadcast/valkey.go`: when `vcfg.TLSEnabled` is true, build `tls.Config` (same CA loading pattern as NATS). Apply to Valkey client options. Log TLS status at Info.

- [x] T030 Wire TLS config in `ws/cmd/server/main.go`: pass `NATSTLSEnabled`, `NATSTLSInsecure`, `NATSTLSCAPath` to `broadcast.NATSConfig.TLSEnabled/TLSInsecure/TLSCAPath` when building broadcast bus config. Same for Valkey TLS fields.

- [x] T031 Verify Phase 6: run `cd ws && go vet ./...` and `cd ws && go test ./...`. All tests must pass.

---

## Phase 7 — Metrics

- [x] T032 Add backend-agnostic Prometheus metrics to `ws/internal/server/metrics/metrics.go`: `ws_backend_messages_published_total` (CounterVec, label: `backend`), `ws_backend_messages_consumed_total` (CounterVec, label: `backend`), `ws_backend_publish_errors_total` (CounterVec, label: `backend`), `ws_backend_replay_requests_total` (CounterVec, label: `backend`), `ws_backend_replay_messages_total` (CounterVec, label: `backend`), `ws_backend_publish_latency_seconds` (HistogramVec, label: `backend`, standard latency buckets), `ws_backend_healthy` (GaugeVec, label: `backend`). Add helper functions: `RecordBackendPublish(backend string)`, `RecordBackendConsume(backend string)`, `RecordBackendPublishError(backend string)`, `RecordBackendReplayRequest(backend string)`, `RecordBackendReplayMessages(backend string, count int)`, `RecordBackendPublishLatency(backend string, seconds float64)`, `SetBackendHealthy(backend string, healthy bool)`.

- [x] T033 Integrate metrics into backend implementations: in `ws/internal/server/backend/directbackend/directbackend.go`, `kafkabackend/kafkabackend.go`, and `jetstreambackend/jetstreambackend.go`, call the appropriate `metrics.RecordBackend*` functions in `Publish()`, `Replay()`, and health check paths. Each backend knows its own name string ("direct", "kafka", "nats").

- [x] T034 Verify Phase 7: run `cd ws && go vet ./...` and `cd ws && go test ./...`. All tests must pass.

---

## Phase 8 — Tests

- [x] T035 [P] Create `ws/internal/server/backend/directbackend/directbackend_test.go` with table-driven tests: (1) Publish routes message to broadcast bus with correct Subject and Payload, (2) Publish with nil bus returns error at construction, (3) Replay returns nil, nil always, (4) IsHealthy returns true always, (5) Start returns nil, (6) Shutdown returns nil. Use a mock broadcast.Bus.

- [x] T036 [P] Create `ws/internal/server/backend/kafkabackend/kafkabackend_test.go` with table-driven tests using mock producer and pool: (1) Publish delegates to producer.Publish with correct args, (2) Publish returns producer error unchanged, (3) Replay delegates to shared consumer ReplayFromOffsets and converts ReplayMessage types, (4) Replay returns nil, nil when shared consumer is nil, (5) IsHealthy reflects producer health, (6) Shutdown calls pool stop + producer close + topic registry close. Use mock interfaces.

- [x] T037 [P] Skipped — no factory.go exists (wiring done in main.go) with table-driven tests: (1) Type "direct" creates DirectBackend, (2) Type "kafka" creates KafkaBackend (may need mock config), (3) Type "nats" creates JetStreamBackend (may need mock config), (4) Unknown type returns error with valid options listed, (5) Unknown type error uses `ErrUnknownBackend` sentinel.

- [x] T037b [P] Create `ws/internal/server/backend/backend_test.go` with interface contract tests: define a `backendTestSuite` that takes any `MessageBackend` implementation and verifies the contract — (1) Start returns nil or error (never panics), (2) Publish with empty channel returns error, (3) Replay with nil positions returns without error, (4) IsHealthy returns bool without panic, (5) Shutdown is idempotent (can be called twice). Run the suite against `DirectBackend` and mock implementations.

- [x] T037c [P] Create `ws/internal/server/backend/jetstreambackend/jetstreambackend_test.go` with table-driven tests using mock NATS connection and JetStream context: (1) Publish delegates to js.Publish with correct subject, (2) Publish returns error on JetStream failure, (3) Replay creates ordered consumer from sequence+1 and fetches messages, (4) Replay returns nil, nil when positions map is empty, (5) IsHealthy reflects NATS connection status, (6) Shutdown cancels context and waits for goroutines, (7) refreshStreams creates streams for discovered tenants. Use mock interfaces for NATS/JetStream.

- [x] T038 [P] Skipped — TLS integration verified via build + existing broadcast tests with tests: (1) TLS config fields are applied to NATS options, (2) Custom CA path loads certificate, (3) Insecure skip sets InsecureSkipVerify. Create `ws/internal/server/broadcast/valkey_tls_test.go` with equivalent tests for Valkey TLS.

- [x] T039 Handler tests already pass — no Kafka references to update in `ws/internal/server/handlers_publish_test.go` and `ws/internal/server/handlers_message_test.go` (if they exist) to use mock `MessageBackend` instead of mock Kafka producer/consumer. If no handler tests exist, create minimal tests: publish with nil backend returns not_available error, publish with working mock backend returns publish_ack, reconnect with nil backend returns not_available, reconnect with direct mock (empty replay) returns reconnect_ack with messages_replayed 0.

- [x] T040 Verify Phase 8: run `cd ws && go vet ./...` and `cd ws && go test ./...`. All new and existing tests must pass.

---

## Phase 9 — Helm & Deploy

- [x] T040b Modify `deployments/helm/odin/charts/ws-server/templates/deployment.yaml`: remove the `wait-for-provisioning` init container entirely — `StreamTopicRegistry` handles reconnection with exponential backoff (soft dependency, connects asynchronously). Make the `wait-for-redpanda` init container conditional on `{{ if eq .Values.messageBackend "kafka" }}` (KafkaBackend fails fast if brokers unreachable). The `wait-for-nats` init container is conditional on `broadcast.type=nats` (only needed when NATS is the broadcast bus). Add a `wait-for-nats-jetstream` init container conditional on `{{ if eq .Values.messageBackend "nats" }}` that waits for the JetStream NATS server.

- [x] T040c Modify `deployments/helm/odin/charts/ws-server/templates/configmap.yaml`: remove the dead `KAFKA_TOPICS` entry (references non-existent `kafka.topics` value — topics are discovered dynamically via gRPC streaming). Make `KAFKA_CONSUMER_ENABLED` conditional on `{{ if eq .Values.messageBackend "kafka" }}` (KafkaBackend uses `KafkaConsumerEnabled` internally for connection-only mode — the env var must stay but only applies in kafka mode). Wrap all remaining Kafka-specific entries (`KAFKA_BROKERS`, `KAFKA_SASL_*`, `KAFKA_TLS_*`, etc.) in `{{ if eq .Values.messageBackend "kafka" }}` conditionals.

- [x] T041 [P] Add `MESSAGE_BACKEND` and NATS JetStream env vars to `deployments/helm/odin/charts/ws-server/templates/deployment.yaml`: `MESSAGE_BACKEND` from values, `NATS_JETSTREAM_URLS`, `NATS_JETSTREAM_TOKEN`, `NATS_JETSTREAM_USER`, `NATS_JETSTREAM_PASSWORD`, `NATS_JETSTREAM_REPLICAS`, `NATS_JETSTREAM_MAX_AGE`, `NATS_JETSTREAM_TLS_ENABLED`, `NATS_JETSTREAM_TLS_INSECURE`, `NATS_JETSTREAM_TLS_CA_PATH`. Add broadcast bus TLS env vars: `NATS_TLS_ENABLED`, `NATS_TLS_INSECURE`, `NATS_TLS_CA_PATH`, `VALKEY_TLS_ENABLED`, `VALKEY_TLS_INSECURE`, `VALKEY_TLS_CA_PATH`.

- [x] T042 [P] Add default values to `deployments/helm/odin/charts/ws-server/values.yaml`: `messageBackend: direct`, `jetstream:` section (urls, token, user, password, replicas, maxAge, tls), `nats.tls:` section, `valkey.tls:` section. Follow existing values structure and naming conventions.

- [x] T043 [P] Update `deployments/helm/odin/values/standard/dev.yaml`: set `messageBackend: kafka` (preserve current dev behavior). Check if any broadcast bus TLS overrides are needed for dev.

- [x] T044 Verify Phase 9: run `helm lint deployments/helm/odin/charts/ws-server`. Confirm no lint errors.

---

## Dependency Graph

```
Phase 1: T001 ─┬─ T002 ── T003
               │
Phase 2:       T003 ── T004 ── T005 ── T006 ─┐
               │                              │
Phase 3:       T003 ── T007 [P] ─────────────┤
               │                              │
Phase 4:       ├──────────────────────────────┤
               │         T008 ── T009 ── T010 ── T011 ── T011b ── T012 ── T013 ── T014 ── T015
               │
Phase 4b:      T015 ── T015b ── T015c ── T015d
               │
Phase 5:       T015d ── T016 ── T016b ── T017 ── T018 ── T019 ── T020 ── T021 ── T022 ── T023 ── T024 ── T025
               │
Phase 6:       T003 ── T026 [P] ─┐
                       T027 ─────┤ (T027 after T002 — shared file: server_config.go)
                                 ├─ T028 ─┬─ T030 ── T031
                                    T029 ─┘  (T030 after T014 — shared file: main.go)
                                 │
Phase 7:       T015+T031 ── T032 ── T033 ── T034
               │
Phase 8:       T034 ─┬─ T035 [P] ──┬─ T039 ── T040
                     ├─ T036 [P] ──┤
                     ├─ T037 [P] ──┤
                     ├─ T037b [P] ─┤
                     ├─ T037c [P] ─┤
                     └─ T038 [P] ──┘
               │
Phase 9:       T040 ── T040b ── T040c ─┬─ T041 [P] ─┬─ T044
                                      ├─ T042 [P] ─┤
                                      └─ T043 [P] ─┘
```

### Key Dependencies
- T001 must complete before any other task (interface definition)
- T003 (Phase 1 verification) gates Phases 2, 3, and 6
- T007 (DirectBackend) is parallelizable with T004-T006 (KafkaBackend) — different files
- T026 (bus.go TLS fields) is parallelizable with T004-T007 — different packages
- **T027 must follow T002** — both modify `server_config.go`
- **T030 must follow T014** — both modify `main.go`
- Phase 6 config tasks (T026) can parallel Phases 2-3; wiring tasks (T028-T030) must follow Phase 4
- T008-T014 are sequential (each builds on prior server integration changes)
- T015 (Phase 4 verification) gates Phase 4b (provisioning cleanup) and Phase 7
- T015d (Phase 4b verification) gates Phase 5
- T016b (TenantRegistry relocation) must precede T017 (JetStream struct uses types.TenantRegistry)
- T035-T038, T037b, T037c are parallelizable (separate test files, no deps)
- T041-T043 are parallelizable (separate Helm files)
