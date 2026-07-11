package kafka

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sony/gobreaker/v2"

	"github.com/klurvio/sukko/internal/server/backend"
	"github.com/klurvio/sukko/internal/shared/license"
	"github.com/klurvio/sukko/internal/shared/provapi"
	"github.com/klurvio/sukko/internal/shared/types"
)

// =============================================================================
// resolveTargets tests (#179: reject semantics, no community fallback)
// =============================================================================

// stubRulesProvider implements RoutingRulesSource for producer tests and
// RoutingSnapshotProvider for consumer tests (shared with extract_channel_test.go).
type stubRulesProvider struct {
	snap   provapi.TenantRoutingSnapshot
	ok     bool
	synced bool
}

func (s *stubRulesProvider) GetRoutingSnapshot(_ string) (provapi.TenantRoutingSnapshot, bool) {
	return s.snap, s.ok
}

func (s *stubRulesProvider) SnapshotReceived() bool { return s.synced }

// resolveTargetsProducer builds a producer wired with a rules source and a fixed edition
// accessor (edition comes from the server-global license, NOT snap.Edition — #179 §3b).
func resolveTargetsProducer(prov RoutingRulesSource, edition license.Edition) *Producer {
	return &Producer{
		topicNamespace: "prod",
		rulesProvider:  prov,
		edition:        func() license.Edition { return edition },
	}
}

func syncedProvider(edition license.Edition, rules ...types.RoutingRule) *stubRulesProvider {
	return &stubRulesProvider{
		ok:     true,
		synced: true,
		snap:   provapi.TenantRoutingSnapshot{Edition: edition, Rules: rules},
	}
}

func rule(pattern string, topics ...string) types.RoutingRule {
	return types.RoutingRule{Pattern: pattern, Topics: topics, Priority: 1}
}

// TestPublish_MultiTopicRejectDoesNotTripBreaker pins the invariant that a multi-topic rule
// with no fanout pool (the P1a state) is rejected BEFORE the circuit breaker — so one
// tenant's valid multi-topic rule cannot open the shared breaker and 503 every tenant (§IX).
func TestPublish_MultiTopicRejectDoesNotTripBreaker(t *testing.T) {
	t.Parallel()

	logger := zerolog.Nop()
	p, err := NewProducer(ProducerConfig{
		Brokers:                   []string{"localhost:19092"}, // kgo does not dial at construction
		TopicNamespace:            "prod",
		Logger:                    &logger,
		CircuitBreakerMaxFailures: 3, // low, to prove even many rejects don't trip it
		CircuitBreakerTimeout:     time.Minute,
		RulesProvider:             syncedProvider(license.Pro, rule("acme.**.trade", "a", "b")),
		Edition:                   func() license.Edition { return license.Pro },
		// Fanout intentionally nil (P1a).
	})
	if err != nil {
		t.Fatalf("NewProducer: %v", err)
	}
	defer func() { _ = p.Close() }()

	// The reject fires before any Kafka I/O, so no broker connection is needed.
	for i := range 10 {
		perr := p.Publish(context.Background(), 1, "acme.BTC.trade", []byte(`{"x":1}`))
		if !errors.Is(perr, backend.ErrPublishFailed) {
			t.Fatalf("publish %d: err = %v, want ErrPublishFailed", i, perr)
		}
	}
	if state := p.CircuitBreakerState(); state != gobreaker.StateClosed {
		t.Errorf("breaker state = %v, want Closed — multi-topic rejects must not trip the breaker", state)
	}
}

func TestResolveTargets(t *testing.T) {
	t.Parallel()

	const channel = "acme.BTC.trade"

	tests := []struct {
		name       string
		producer   *Producer
		wantTopics []string
		wantErrIs  error // nil = no error expected
	}{
		{
			name:      "nil provider rejects (no community fallback)",
			producer:  resolveTargetsProducer(nil, license.Pro),
			wantErrIs: backend.ErrPublishNotRoutable,
		},
		{
			name:      "not synced yet is unavailable (retryable), not a reject",
			producer:  resolveTargetsProducer(&stubRulesProvider{ok: false, synced: false}, license.Pro),
			wantErrIs: ErrServiceUnavailable,
		},
		{
			name:      "nil edition accessor rejects (feature-missing)",
			producer:  &Producer{topicNamespace: "prod", rulesProvider: syncedProvider(license.Pro, rule("acme.*.trade", "trades")), edition: nil},
			wantErrIs: backend.ErrPublishNotRoutable,
		},
		{
			name:      "edition lacks ChannelTopicRouting rejects",
			producer:  resolveTargetsProducer(syncedProvider(license.Pro, rule("acme.*.trade", "trades")), license.Community),
			wantErrIs: backend.ErrPublishNotRoutable,
		},
		{
			name:      "synced but tenant absent rejects",
			producer:  resolveTargetsProducer(&stubRulesProvider{ok: false, synced: true}, license.Pro),
			wantErrIs: backend.ErrPublishNotRoutable,
		},
		{
			name:      "synced but zero rules rejects",
			producer:  resolveTargetsProducer(syncedProvider(license.Pro), license.Pro),
			wantErrIs: backend.ErrPublishNotRoutable,
		},
		{
			name:       "single-topic rule match produces",
			producer:   resolveTargetsProducer(syncedProvider(license.Pro, rule("acme.*.trade", "trades")), license.Pro),
			wantTopics: []string{"prod.acme.trades"},
		},
		{
			name:       "multi-topic rule returns all topics",
			producer:   resolveTargetsProducer(syncedProvider(license.Pro, rule("acme.**.trade", "trades", "all-data")), license.Pro),
			wantTopics: []string{"prod.acme.trades", "prod.acme.all-data"},
		},
		{
			name:       "duplicate topics in one rule dedupe to a single produce",
			producer:   resolveTargetsProducer(syncedProvider(license.Pro, rule("acme.**.trade", "trades", "trades")), license.Pro),
			wantTopics: []string{"prod.acme.trades"},
		},
		{
			// C1 revised (#179): zero-topic rule → no-match → REJECT (was DLQ).
			name:      "zero-topic rule treated as no-match rejects",
			producer:  resolveTargetsProducer(syncedProvider(license.Pro, rule("acme.*.trade")), license.Pro),
			wantErrIs: backend.ErrNoMatchingRoute,
		},
		{
			// C1 revised (#179): rules present, none match → REJECT (was DLQ), per ratified FR-014.
			name:      "no rule matches rejects (not dead-lettered)",
			producer:  resolveTargetsProducer(syncedProvider(license.Pro, rule("acme.*.orders", "orders")), license.Pro),
			wantErrIs: backend.ErrNoMatchingRoute,
		},
		{
			name:       "first matching rule wins",
			producer:   resolveTargetsProducer(syncedProvider(license.Pro, rule("acme.**.trade", "all-trades"), rule("acme.BTC.trade", "btc-trades")), license.Pro),
			wantTopics: []string{"prod.acme.all-trades"},
		},
		{
			// Edition comes from the accessor, NOT snap.Edition: snapshot says Community but
			// the server is Pro → routable. Guards the #179 §3b staleness regression.
			name:       "edition accessor (Pro) overrides stale snap.Edition (Community)",
			producer:   resolveTargetsProducer(syncedProvider(license.Community, rule("acme.*.trade", "trades")), license.Pro),
			wantTopics: []string{"prod.acme.trades"},
		},
		{
			// Reverse: snapshot says Pro but the server is Community → reject.
			name:      "edition accessor (Community) rejects despite Pro snap.Edition",
			producer:  resolveTargetsProducer(syncedProvider(license.Pro, rule("acme.*.trade", "trades")), license.Community),
			wantErrIs: backend.ErrPublishNotRoutable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			topics, err := tt.producer.resolveTargets(channel, "acme")

			if tt.wantErrIs != nil {
				if !errors.Is(err, tt.wantErrIs) {
					t.Fatalf("err = %v, want errors.Is(%v)", err, tt.wantErrIs)
				}
				// ErrNoMatchingRoute wraps ErrPublishNotRoutable so the shared reject-class
				// handling (gRPC FailedPrecondition, reject metric, Warn log) catches it.
				if errors.Is(err, backend.ErrNoMatchingRoute) && !errors.Is(err, backend.ErrPublishNotRoutable) {
					t.Errorf("ErrNoMatchingRoute must also satisfy errors.Is(ErrPublishNotRoutable); err = %v", err)
				}
				if topics != nil {
					t.Errorf("topics = %v, want nil on reject", topics)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err = %v", err)
			}
			if len(topics) != len(tt.wantTopics) {
				t.Fatalf("topics = %v, want %v", topics, tt.wantTopics)
			}
			for i, want := range tt.wantTopics {
				if topics[i] != want {
					t.Errorf("topics[%d] = %q, want %q", i, topics[i], want)
				}
			}
		})
	}
}
