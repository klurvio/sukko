package kafka

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/klurvio/sukko/internal/shared/license"
	"github.com/klurvio/sukko/internal/shared/provapi"
	"github.com/klurvio/sukko/internal/shared/types"
)

// =============================================================================
// extractChannel tests
// =============================================================================

func TestExtractChannel_MalformedTopic_TwoSegments(t *testing.T) {
	t.Parallel()

	rec := &kgo.Record{Topic: "prod.acme"} // only 2 parts, need 3
	_, _, malformed := extractChannel(rec, nil)
	if !malformed {
		t.Error("expected malformed=true for topic with <3 segments")
	}
}

func TestExtractChannel_MalformedTopic_OneSegment(t *testing.T) {
	t.Parallel()

	rec := &kgo.Record{Topic: "prod"}
	_, _, malformed := extractChannel(rec, nil)
	if !malformed {
		t.Error("expected malformed=true for topic with 1 segment")
	}
}

func TestExtractChannel_ValidHeader_TenantMatch(t *testing.T) {
	t.Parallel()

	rec := &kgo.Record{
		Topic: "prod.acme.orders",
		Headers: []kgo.RecordHeader{
			{Key: HeaderChannel, Value: []byte("acme.BTC.orders")},
		},
	}

	channel, reason, malformed := extractChannel(rec, nil)
	if malformed {
		t.Error("expected malformed=false")
	}
	if reason != "" {
		t.Errorf("reason = %q, want empty", reason)
	}
	if channel != "acme.BTC.orders" {
		t.Errorf("channel = %q, want %q", channel, "acme.BTC.orders")
	}
}

func TestExtractChannel_ValidHeader_TenantMismatch(t *testing.T) {
	t.Parallel()

	rec := &kgo.Record{
		Topic: "prod.acme.orders",
		Headers: []kgo.RecordHeader{
			{Key: HeaderChannel, Value: []byte("globex.BTC.orders")}, // wrong tenant
		},
	}

	_, reason, malformed := extractChannel(rec, nil)
	if malformed {
		t.Error("expected malformed=false")
	}
	if reason != ReasonTenantPrefixMismatch {
		t.Errorf("reason = %q, want %q", reason, ReasonTenantPrefixMismatch)
	}
}

func TestExtractChannel_NoHeader_ProEdition_ReturnsReason(t *testing.T) {
	t.Parallel()

	provider := &stubRulesProvider{
		ok: true,
		snap: provapi.TenantRoutingSnapshot{
			Edition: license.Pro,
			Rules:   []types.RoutingRule{},
		},
	}

	rec := &kgo.Record{Topic: "prod.acme.orders"}
	_, reason, malformed := extractChannel(rec, provider)
	if malformed {
		t.Error("expected malformed=false")
	}
	if reason != ReasonMissingChannelHeader {
		t.Errorf("reason = %q, want %q", reason, ReasonMissingChannelHeader)
	}
}

func TestExtractChannel_NoHeader_CommunityEdition_ValidKey(t *testing.T) {
	t.Parallel()

	// Community edition (no provider): should use record.Key as channel.
	rec := &kgo.Record{
		Topic: "prod.acme.orders",
		Key:   []byte("acme.BTC.orders"),
	}

	channel, reason, malformed := extractChannel(rec, nil)
	if malformed {
		t.Error("expected malformed=false")
	}
	if reason != "" {
		t.Errorf("reason = %q, want empty", reason)
	}
	if channel != "acme.BTC.orders" {
		t.Errorf("channel = %q, want %q", channel, "acme.BTC.orders")
	}
}

func TestExtractChannel_NoHeader_CommunityEdition_InvalidKey_NoDot(t *testing.T) {
	t.Parallel()

	rec := &kgo.Record{
		Topic: "prod.acme.orders",
		Key:   []byte("nodot"),
	}

	_, reason, malformed := extractChannel(rec, nil)
	if malformed {
		t.Error("expected malformed=false")
	}
	if reason != ReasonInvalidChannelKey {
		t.Errorf("reason = %q, want %q", reason, ReasonInvalidChannelKey)
	}
}

func TestExtractChannel_NoHeader_CommunityEdition_EmptyKey(t *testing.T) {
	t.Parallel()

	rec := &kgo.Record{
		Topic: "prod.acme.orders",
		Key:   nil,
	}

	_, reason, malformed := extractChannel(rec, nil)
	if malformed {
		t.Error("expected malformed=false")
	}
	if reason != ReasonInvalidChannelKey {
		t.Errorf("reason = %q, want %q", reason, ReasonInvalidChannelKey)
	}
}

func TestExtractChannel_NoHeader_CommunityEditionProvider_ValidKey(t *testing.T) {
	t.Parallel()

	// Provider present but edition is Community — should fall back to key-based routing.
	provider := &stubRulesProvider{
		ok: true,
		snap: provapi.TenantRoutingSnapshot{
			Edition: license.Community,
			Rules:   []types.RoutingRule{},
		},
	}

	rec := &kgo.Record{
		Topic: "prod.acme.trades",
		Key:   []byte("acme.BTC.trade"),
	}

	channel, reason, malformed := extractChannel(rec, provider)
	if malformed {
		t.Error("expected malformed=false")
	}
	if reason != "" {
		t.Errorf("reason = %q, want empty", reason)
	}
	if channel != "acme.BTC.trade" {
		t.Errorf("channel = %q, want %q", channel, "acme.BTC.trade")
	}
}

func TestFindHeader_ReturnsNilWhenAbsent(t *testing.T) {
	t.Parallel()

	rec := &kgo.Record{
		Headers: []kgo.RecordHeader{
			{Key: "other-key", Value: []byte("v")},
		},
	}

	val := findHeader(rec, HeaderChannel)
	if val != nil {
		t.Errorf("findHeader = %q, want nil", val)
	}
}

func TestFindHeader_ReturnsFirstMatch(t *testing.T) {
	t.Parallel()

	rec := &kgo.Record{
		Headers: []kgo.RecordHeader{
			{Key: HeaderChannel, Value: []byte("first")},
			{Key: HeaderChannel, Value: []byte("second")},
		},
	}

	val := findHeader(rec, HeaderChannel)
	if string(val) != "first" {
		t.Errorf("findHeader = %q, want %q", val, "first")
	}
}

// =============================================================================
// routeToDLQ tests
// =============================================================================

func TestRouteToDLQ_NilDLQ_DoesNotPanic(t *testing.T) {
	t.Parallel()

	c := &Consumer{namespace: "prod", dlq: nil}
	rec := &kgo.Record{
		Topic: "prod.acme.orders",
		Value: []byte("payload"),
	}
	c.routeToDLQ(rec, ReasonMissingChannelHeader)
}

func TestRouteToDLQ_MalformedTopic_EmptyTenant(t *testing.T) {
	t.Parallel()

	pool := &DLQPool{
		jobs:   make(chan dlqJob, 4),
		cfg:    DLQConfig{Workers: 0},
		logger: zerolog.Nop(),
	}
	c := &Consumer{namespace: "prod", dlq: pool}
	// Topic has only 1 segment — tenant extraction must yield "".
	rec := &kgo.Record{
		Topic: "prod",
		Value: []byte("payload"),
	}
	c.routeToDLQ(rec, ReasonMissingChannelHeader)

	select {
	case job := <-pool.jobs:
		if job.tenant != "" {
			t.Errorf("tenant = %q, want empty for single-segment topic", job.tenant)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected DLQ job to be submitted")
	}
}

func TestRouteToDLQ_ValidTopic_SubmitsJobWithReasonHeader(t *testing.T) {
	t.Parallel()

	pool := &DLQPool{
		jobs:   make(chan dlqJob, 4),
		cfg:    DLQConfig{Workers: 0},
		logger: zerolog.Nop(),
	}
	c := &Consumer{namespace: "prod", dlq: pool}
	rec := &kgo.Record{
		Topic:   "prod.acme.orders",
		Key:     []byte("acme.BTC.orders"),
		Value:   []byte("payload"),
		Headers: []kgo.RecordHeader{{Key: "existing", Value: []byte("val")}},
	}
	c.routeToDLQ(rec, ReasonMissingChannelHeader)

	select {
	case job := <-pool.jobs:
		if job.tenant != "acme" {
			t.Errorf("tenant = %q, want %q", job.tenant, "acme")
		}
		if job.reason != ReasonMissingChannelHeader {
			t.Errorf("reason = %q, want %q", job.reason, ReasonMissingChannelHeader)
		}
		found := false
		for _, h := range job.record.Headers {
			if h.Key == HeaderReason && string(h.Value) == ReasonMissingChannelHeader {
				found = true
			}
		}
		if !found {
			t.Error("DLQ record missing HeaderReason header with expected value")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected DLQ job to be submitted")
	}
}

func TestRouteToDLQ_QueueFull_DropsJobSilently(t *testing.T) {
	t.Parallel()

	// Capacity-0 channel: TrySubmit always returns false.
	pool := &DLQPool{
		jobs:   make(chan dlqJob, 0),
		cfg:    DLQConfig{Workers: 0},
		logger: zerolog.Nop(),
	}
	c := &Consumer{namespace: "prod", dlq: pool}
	rec := &kgo.Record{
		Topic: "prod.acme.orders",
		Value: []byte("payload"),
	}
	// Must not block or panic.
	c.routeToDLQ(rec, ReasonNoRoutingRuleMatched)
}
