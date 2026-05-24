package main

import (
	"context"
	"maps"
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"

	"github.com/klurvio/sukko/internal/provisioning"
	"github.com/klurvio/sukko/internal/shared/kafka"
	"github.com/klurvio/sukko/internal/shared/routing"
)

// fakeTopicAdmin implements topicAdmin and records all calls.
type fakeTopicAdmin struct {
	mu sync.Mutex

	// Per-call responses (popped in order; last entry reused when exhausted).
	createResponses []fakeCreateResp
	deleteErr       error

	// Recorded calls.
	createCalls []createCall
	deleteCalls []string

	// Captured replication factor from most recent CreateTopics call.
	lastRF int16
}

type fakeCreateResp struct {
	topicErr error // if non-nil, CreateTopicResponse.Err is set for every topic
	callErr  error // returned as the second (error) return of CreateTopics
}

type createCall struct {
	Topics     []string
	RF         int16
	Partitions int32
	Configs    map[string]*string // copy of configs passed to CreateTopics
}

func (f *fakeTopicAdmin) CreateTopics(
	ctx context.Context,
	partitions int32,
	replicationFactor int16,
	configs map[string]*string,
	topics ...string,
) (kadm.CreateTopicResponses, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lastRF = replicationFactor
	cfgCopy := make(map[string]*string, len(configs))
	maps.Copy(cfgCopy, configs)
	f.createCalls = append(f.createCalls, createCall{Topics: topics, RF: replicationFactor, Partitions: partitions, Configs: cfgCopy})

	var resp fakeCreateResp
	if len(f.createResponses) > 0 {
		resp = f.createResponses[0]
		if len(f.createResponses) > 1 {
			f.createResponses = f.createResponses[1:]
		}
	}

	if resp.callErr != nil {
		return nil, resp.callErr
	}

	result := make(kadm.CreateTopicResponses, len(topics))
	for _, t := range topics {
		result[t] = kadm.CreateTopicResponse{Topic: t, Err: resp.topicErr}
	}
	return result, nil
}

func (f *fakeTopicAdmin) DeleteTopics(
	ctx context.Context,
	topics ...string,
) (kadm.DeleteTopicResponses, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.deleteCalls = append(f.deleteCalls, topics...)

	if f.deleteErr != nil {
		return nil, f.deleteErr
	}

	result := make(kadm.DeleteTopicResponses, len(topics))
	for _, t := range topics {
		result[t] = kadm.DeleteTopicResponse{Topic: t}
	}
	return result, nil
}

func (f *fakeTopicAdmin) createCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.createCalls)
}

func (f *fakeTopicAdmin) deleteCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.deleteCalls)
}

func testTenant(slug string) *provisioning.Tenant {
	return &provisioning.Tenant{Slug: slug, Name: slug}
}

// testSpecs returns a two-element spec slice (DLQ + default) for tests that only care about
// topic names and counts, not retention or partition configs.
func testSpecs(partitions int32) []topicMigSpec {
	return []topicMigSpec{
		{suffix: routing.DeadLetterTopicSuffix, partitions: partitions},
		{suffix: routing.DefaultTopicSuffix, partitions: partitions},
	}
}

func dlqTopicName(ns, tenantID string) string {
	return kafka.BuildTopicName(ns, tenantID, routing.DeadLetterTopicSuffix)
}

func defaultTopicName(ns, tenantID string) string {
	return kafka.BuildTopicName(ns, tenantID, routing.DefaultTopicSuffix)
}

func TestProvisionTenantTopics_DryRun(t *testing.T) {
	t.Parallel()
	admin := &fakeTopicAdmin{}
	ok := provisionTenantTopics(
		context.Background(), admin, "ns", testTenant("t1"),
		testSpecs(3), 1,
		false, // recreate
		true,  // dryRun
		zerolog.Nop(),
	)
	if !ok {
		t.Error("expected ok=true for dry-run")
	}
	if admin.createCallCount() != 0 {
		t.Errorf("CreateTopics called %d times, want 0 in dry-run", admin.createCallCount())
	}
	if admin.deleteCallCount() != 0 {
		t.Errorf("DeleteTopics called %d times, want 0 in dry-run", admin.deleteCallCount())
	}
}

func TestProvisionTenantTopics_DryRunRecreate(t *testing.T) {
	t.Parallel()
	admin := &fakeTopicAdmin{}
	ok := provisionTenantTopics(
		context.Background(), admin, "ns", testTenant("t1"),
		testSpecs(3), 1,
		true, // recreate
		true, // dryRun
		zerolog.Nop(),
	)
	if !ok {
		t.Error("expected ok=true for dry-run recreate")
	}
	if admin.createCallCount() != 0 {
		t.Errorf("CreateTopics called %d times, want 0 in dry-run", admin.createCallCount())
	}
	if admin.deleteCallCount() != 0 {
		t.Errorf("DeleteTopics called %d times, want 0 in dry-run", admin.deleteCallCount())
	}
}

func TestProvisionTenantTopics_HappyPath(t *testing.T) {
	t.Parallel()
	admin := &fakeTopicAdmin{}
	ok := provisionTenantTopics(
		context.Background(), admin, "ns", testTenant("t1"),
		testSpecs(3), 1,
		false, false, zerolog.Nop(),
	)
	if !ok {
		t.Error("expected ok=true")
	}
	// Two suffixes (DLQ + default) → two CreateTopics calls, zero DeleteTopics.
	if admin.createCallCount() != 2 {
		t.Errorf("CreateTopics called %d times, want 2", admin.createCallCount())
	}
	if admin.deleteCallCount() != 0 {
		t.Errorf("DeleteTopics called %d times, want 0", admin.deleteCallCount())
	}
}

func TestProvisionTenantTopics_TopicAlreadyExists(t *testing.T) {
	t.Parallel()
	// All create calls return TopicAlreadyExists — should be treated as idempotent (ok).
	admin := &fakeTopicAdmin{
		createResponses: []fakeCreateResp{
			{topicErr: kerr.TopicAlreadyExists},
		},
	}
	ok := provisionTenantTopics(
		context.Background(), admin, "ns", testTenant("t1"),
		testSpecs(3), 1,
		false, false, zerolog.Nop(),
	)
	if !ok {
		t.Error("expected ok=true when topic already exists")
	}
	if admin.deleteCallCount() != 0 {
		t.Errorf("DeleteTopics called %d times, want 0", admin.deleteCallCount())
	}
}

func TestProvisionTenantTopics_Recreate(t *testing.T) {
	t.Parallel()
	admin := &fakeTopicAdmin{}
	ok := provisionTenantTopics(
		context.Background(), admin, "ns", testTenant("t1"),
		testSpecs(3), 1,
		true,  // recreate
		false, // dryRun
		zerolog.Nop(),
	)
	if !ok {
		t.Error("expected ok=true")
	}
	// DeleteTopics must be called once per suffix before CreateTopics.
	if admin.deleteCallCount() != 2 {
		t.Errorf("DeleteTopics called %d times, want 2", admin.deleteCallCount())
	}
	if admin.createCallCount() != 2 {
		t.Errorf("CreateTopics called %d times, want 2", admin.createCallCount())
	}

	// Verify delete precedes create by checking recorded delete topics contain both suffixes.
	dlq := dlqTopicName("ns", "t1")
	def := defaultTopicName("ns", "t1")

	admin.mu.Lock()
	deleted := admin.deleteCalls
	admin.mu.Unlock()

	wantDeleted := map[string]bool{dlq: false, def: false}
	for _, d := range deleted {
		wantDeleted[d] = true
	}
	for topic, found := range wantDeleted {
		if !found {
			t.Errorf("expected %q in deleteCalls, got %v", topic, deleted)
		}
	}
}

func TestProvisionTenantTopics_RecreateDeleteSuccessCreateFails(t *testing.T) {
	t.Parallel()
	// Delete succeeds, but create fails with a non-idempotent error.
	admin := &fakeTopicAdmin{
		createResponses: []fakeCreateResp{
			{topicErr: kerr.BrokerNotAvailable},
		},
	}

	ok := provisionTenantTopics(
		context.Background(), admin, "ns", testTenant("t1"),
		testSpecs(3), 1,
		true, false, zerolog.Nop(),
	)
	if ok {
		t.Error("expected ok=false when recreation fails")
	}
}

func TestProvisionTenantTopics_ReplicationFactor(t *testing.T) {
	t.Parallel()
	wantRF := int16(3)
	admin := &fakeTopicAdmin{}
	provisionTenantTopics(
		context.Background(), admin, "ns", testTenant("t1"),
		testSpecs(6), wantRF,
		false, false, zerolog.Nop(),
	)
	admin.mu.Lock()
	gotRF := admin.lastRF
	admin.mu.Unlock()
	if gotRF != wantRF {
		t.Errorf("replication factor = %d, want %d", gotRF, wantRF)
	}
}

func TestProvisionTenantTopics_PartialFailureContinues(t *testing.T) {
	t.Parallel()
	// First suffix (DLQ) fails, second (default) succeeds.
	admin := &fakeTopicAdmin{
		createResponses: []fakeCreateResp{
			{topicErr: kerr.BrokerNotAvailable}, // DLQ fails
			{},                                  // default succeeds
		},
	}
	ok := provisionTenantTopics(
		context.Background(), admin, "ns", testTenant("t1"),
		testSpecs(3), 1,
		false, false, zerolog.Nop(),
	)
	if ok {
		t.Error("expected ok=false when first suffix fails")
	}
	// Both suffixes attempted (no early return).
	if admin.createCallCount() != 2 {
		t.Errorf("CreateTopics called %d times, want 2", admin.createCallCount())
	}
}

func TestProvisionTenantTopics_PerSuffixParams(t *testing.T) {
	t.Parallel()
	// Verify each suffix is created with its own partition count (not the DLQ count for both).
	dlqRetention := "86400000"
	defaultRetention := "604800000"
	specs := []topicMigSpec{
		{
			suffix:     routing.DeadLetterTopicSuffix,
			partitions: 1,
			cfg:        map[string]*string{kafka.RetentionMsConfigKey: &dlqRetention},
		},
		{
			suffix:     routing.DefaultTopicSuffix,
			partitions: 3,
			cfg:        map[string]*string{kafka.RetentionMsConfigKey: &defaultRetention},
		},
	}

	admin := &fakeTopicAdmin{}
	ok := provisionTenantTopics(
		context.Background(), admin, "ns", testTenant("t1"),
		specs, 1,
		false, false, zerolog.Nop(),
	)
	if !ok {
		t.Error("expected ok=true")
	}

	admin.mu.Lock()
	calls := admin.createCalls
	admin.mu.Unlock()

	if len(calls) != 2 {
		t.Fatalf("expected 2 CreateTopics calls, got %d", len(calls))
	}
	// First call = DLQ: 1 partition, 1-day retention.
	if calls[0].Partitions != 1 {
		t.Errorf("DLQ partitions = %d, want 1", calls[0].Partitions)
	}
	if v := calls[0].Configs[kafka.RetentionMsConfigKey]; v == nil || *v != dlqRetention {
		var got string
		if v != nil {
			got = *v
		}
		t.Errorf("DLQ retention.ms = %q, want %q", got, dlqRetention)
	}
	// Second call = default: 3 partitions, 7-day retention.
	if calls[1].Partitions != 3 {
		t.Errorf("default partitions = %d, want 3", calls[1].Partitions)
	}
	if v := calls[1].Configs[kafka.RetentionMsConfigKey]; v == nil || *v != defaultRetention {
		var got string
		if v != nil {
			got = *v
		}
		t.Errorf("default retention.ms = %q, want %q", got, defaultRetention)
	}
}

func TestSplitBrokers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  []string
	}{
		{"", nil},
		{"localhost:9092", []string{"localhost:9092"}},
		{"a:9092,b:9092", []string{"a:9092", "b:9092"}},
		{"a:9092, b:9092", []string{"a:9092", "b:9092"}}, // whitespace trimmed
		{" , , ", nil}, // whitespace-only entries dropped
		{",localhost:9092,", []string{"localhost:9092"}}, // leading/trailing comma
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := splitBrokers(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("splitBrokers(%q) = %v, want %v", tt.input, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitBrokers(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}
