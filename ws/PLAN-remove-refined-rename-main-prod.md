# Plan: Remove Refined Topics + Rename main→prod Namespace

## Goal

Two related changes to make ws-server topic-agnostic:

1. **Remove `.refined`** — Delete all refined topic assumptions
2. **Rename `main` → `prod`** — Valid namespaces: `local`, `dev`, `staging`, `prod`
3. **Multi-segment topics** — Ensure topics with >3 segments work, add tests

---

## Part A: Remove Refined Topics

### A1. Provisioning: Remove refined auto-creation
**File:** `internal/provisioning/service.go`
- Delete the refined topic creation block in `createTopicsForTenant()` (lines 439-452)

### A2. Delete refined functions from kafka/config.go
**File:** `internal/kafka/config.go`
- Delete `GetRefinedTopic()` (lines 89-93)
- Delete `AllRefinedTopics()` (lines 109-116)
- Delete `AllTopicsWithRefined()` (lines 118-126)
- Update `TopicToEventType()`: Remove `strings.TrimSuffix(topic, ".refined")` line
- Update header comment (line 19): remove `.refined` reference

### A3. Update bundles.go
**File:** `internal/kafka/bundles.go`
- `GetBundleTopics()` line 72: `GetRefinedTopic` → `GetTopic`
- Delete `GetBundleRegularTopics()` (lines 77-89) — now redundant
- `GetTopicsForSubscription()` line 121: `GetRefinedTopic` → `GetTopic`
- Remove "refined" from comments

### A4. Update legacy KafkaConsumerPool
**File:** `internal/orchestration/kafka_pool.go`
- Add `Topics []string` to `KafkaPoolConfig`
- Line 72: Use `config.Topics` if provided, else `kafka.AllTopics(config.Environment)`
- Update log message to remove "(refined topics only)"

---

## Part B: Namespace Rename main → prod

### B1. Update NormalizeEnv
**File:** `internal/kafka/config.go`
- `"production", "prod"` → returns `"prod"` (was `"main"`)
- Remove `"main"` from the case list (clean break)

### B2. Update defaults and validation
**File:** `internal/platform/provisioning_config.go`
- Line 47: `envDefault:"main"` → `envDefault:"prod"`
- Line 177: Update `validNamespaces` to include `"prod"` instead of `"main"`
- Line 179: Update error message

**File:** `internal/platform/server_config.go`
- Line 146: Update valid values comment to `local, dev, staging, prod`
- Lines 149-150: Update use case example from `"main"` to `"prod"`

### B3. Update hardcoded "main" defaults
**File:** `internal/auth/topic.go`
- `DefaultTopicIsolationConfig`: `Environment: "main"` → `"prod"`
- `NewTopicIsolator` fallback: `"main"` → `"prod"`
- Update all doc comments referencing `"main"` → `"prod"`

**File:** `internal/auth/tenant.go`
- Line 103: `Environment: "main"` → `"prod"`

**File:** `internal/kafka/config.go`
- Update doc comment lines from `"main"` to `"prod"`

**File:** `internal/kafka/tenant_registry.go`
- Update comment examples from `"main"` to `"prod"`

**File:** `internal/kafka/producer.go`
- Update comment from `"main"` to `"prod"`

**File:** `internal/orchestration/kafka_pool.go`
- Update comment from `"main"` to `"prod"`

**File:** `internal/orchestration/multitenant_pool.go`
- Update comment from `"main"` to `"prod"`

### B4. Add namespace validation to topic parsing
**File:** `internal/auth/topic.go`
- Add `ValidNamespaces` set
- Validate in `ValidateTopicFormat()` after parsing parts

---

## Part C: Multi-Segment Topic Support

### C1. Verify + add tests
**File:** `internal/auth/topic.go`
- Update `TopicParts.Category` comment: replace `"trade.refined"` with `"trade.v2"`

**File:** `internal/auth/topic_test.go`
- Add test: `ParseTopic("prod.acme.trade.v2")` → Category = `"trade.v2"`
- Add test: `ParseTopic("prod.acme.trade.v2.extra")` → Category = `"trade.v2.extra"`
- Add test: `ValidateTopicFormat("prod.acme.trade.v2")` → no error

---

## Part D: Update All Tests

### D1. kafka/config_test.go
- Delete `TestGetRefinedTopic`
- Delete `TestAllRefinedTopics_*`
- Delete `TestAllTopicsWithRefined_*`
- Update/delete `TestTopicToEventType_RefinedTopics`
- Update `NormalizeEnv` tests: `"main"` no longer valid
- Update all `"main"` → `"prod"` in test expectations

### D2. kafka/bundles_test.go
- Update `GetBundleTopics` tests to expect regular topics (use `GetTopic`)
- Delete `GetBundleRegularTopics` tests
- Remove `GetRefinedTopic`/`AllRefinedTopics` references
- Update `"main"` → `"prod"` in test data

### D3. provisioning/service_test.go
- `TestService_CreateTopics`: expect 2 topics for 2 categories (not 4)

### D4. auth/topic_test.go
- Update all `"main"` → `"prod"` in test data
- Replace `"trade.refined"` test case with multi-segment test
- Add >3 segment test cases
- `DefaultTopicIsolationConfig` test: expect `"prod"` not `"main"`

### D5. orchestration/multitenant_pool_test.go
- Update namespace `"main"` → `"prod"` in test config

### D6. provisioning/topic_registry_test.go
- Update `"main"` → `"prod"` in test data

---

## Verification

```bash
cd /Volumes/Dev/Codev/Toniq/odin-ws/ws
go build ./...
go test ./... -count=1
```
