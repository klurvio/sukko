package messaging

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

// TestBroadcastEnvelope_MatchesJsonMarshal verifies that Build(seq) output is byte-identical
// to json.Marshal(MessageEnvelope{...}) for the same inputs. This is the core regression
// protection for FR-004 (wire format correctness).
func TestBroadcastEnvelope_MatchesJsonMarshal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		channel string
		ts      int64
		payload []byte
		seq     int64
	}{
		{
			name:    "typical broadcast",
			channel: "BTC.trade",
			ts:      1708903200000,
			payload: []byte(`{"token":"BTC","price":45000}`),
			seq:     1,
		},
		{
			name:    "large seq",
			channel: "ETH.liquidity",
			ts:      1708903200001,
			payload: []byte(`{"amount":1000}`),
			seq:     9999999999,
		},
		{
			name:    "seq 1",
			channel: "SOL.trade",
			ts:      0,
			payload: []byte(`{}`),
			seq:     1,
		},
		{
			name:    "max int64 seq",
			channel: "BTC.trade",
			ts:      1708903200000,
			payload: []byte(`{"price":1}`),
			seq:     9223372036854775807, // math.MaxInt64
		},
		{
			name:    "multi-part channel",
			channel: "BTC.trade.user123",
			ts:      1708903200000,
			payload: []byte(`{"data":"test"}`),
			seq:     42,
		},
		{
			name:    "zero timestamp",
			channel: "BTC.trade",
			ts:      0,
			payload: []byte(`null`),
			seq:     1,
		},
		{
			name:    "boolean payload",
			channel: "BTC.trade",
			ts:      1000,
			payload: []byte(`true`),
			seq:     5,
		},
		{
			name:    "array payload",
			channel: "BTC.trade",
			ts:      1000,
			payload: []byte(`[1,2,3]`),
			seq:     10,
		},
		{
			name:    "string payload",
			channel: "BTC.trade",
			ts:      1000,
			payload: []byte(`"hello"`),
			seq:     7,
		},
		{
			name:    "number payload",
			channel: "BTC.trade",
			ts:      1000,
			payload: []byte(`42`),
			seq:     3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			env, err := NewBroadcastEnvelope(tt.channel, tt.ts, tt.payload)
			if err != nil {
				t.Fatalf("NewBroadcastEnvelope() error: %v", err)
			}

			got := env.Build(tt.seq)

			// Build expected via json.Marshal(MessageEnvelope{...})
			expected, err := json.Marshal(MessageEnvelope{
				Type:      "message",
				Seq:       tt.seq,
				Timestamp: tt.ts,
				Channel:   tt.channel,
				Data:      json.RawMessage(tt.payload),
			})
			if err != nil {
				t.Fatalf("json.Marshal() error: %v", err)
			}

			if string(got) != string(expected) {
				t.Errorf("Build() output mismatch:\n  got:  %s\n  want: %s", string(got), string(expected))
			}
		})
	}
}

// TestBroadcastEnvelope_FieldOrder verifies JSON field order matches json.Marshal output:
// type, seq, ts, channel, data.
func TestBroadcastEnvelope_FieldOrder(t *testing.T) {
	t.Parallel()

	env, err := NewBroadcastEnvelope("BTC.trade", 1708903200000, []byte(`{"price":45000}`))
	if err != nil {
		t.Fatalf("NewBroadcastEnvelope() error: %v", err)
	}

	result := string(env.Build(1234))

	// Verify field order by checking byte positions
	typeIdx := strings.Index(result, `"type"`)
	seqIdx := strings.Index(result, `"seq"`)
	tsIdx := strings.Index(result, `"ts"`)
	channelIdx := strings.Index(result, `"channel"`)
	dataIdx := strings.Index(result, `"data"`)

	if typeIdx < 0 || seqIdx < 0 || tsIdx < 0 || channelIdx < 0 || dataIdx < 0 {
		t.Fatalf("Missing field in output: %s", result)
	}

	if typeIdx >= seqIdx || seqIdx >= tsIdx || tsIdx >= channelIdx || channelIdx >= dataIdx {
		t.Errorf("Field order incorrect. Expected type < seq < ts < channel < data, got positions: type=%d, seq=%d, ts=%d, channel=%d, data=%d",
			typeIdx, seqIdx, tsIdx, channelIdx, dataIdx)
	}
}

// TestBroadcastEnvelope_SequenceValues verifies different seq values produce different outputs.
func TestBroadcastEnvelope_SequenceValues(t *testing.T) {
	t.Parallel()

	env, err := NewBroadcastEnvelope("BTC.trade", 1708903200000, []byte(`{"price":45000}`))
	if err != nil {
		t.Fatalf("NewBroadcastEnvelope() error: %v", err)
	}

	results := make(map[string]int64)
	for _, seq := range []int64{1, 2, 3, 100, 999999} {
		result := string(env.Build(seq))
		if prevSeq, exists := results[result]; exists {
			t.Errorf("Build(%d) produced same output as Build(%d)", seq, prevSeq)
		}
		results[result] = seq
	}
}

// TestBroadcastEnvelope_ChannelEscaping verifies special characters in channel names
// are properly JSON-escaped, matching json.Marshal behavior.
func TestBroadcastEnvelope_ChannelEscaping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		channel string
	}{
		{"quotes", `test"channel`},
		{"backslash", `back\slash`},
		{"newline", "test\nchannel"},
		{"tab", "test\tchannel"},
		{"unicode", "test\u0000channel"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			env, err := NewBroadcastEnvelope(tt.channel, 1000, []byte(`{}`))
			if err != nil {
				t.Fatalf("NewBroadcastEnvelope() error: %v", err)
			}

			got := env.Build(1)

			expected, err := json.Marshal(MessageEnvelope{
				Type:      "message",
				Seq:       1,
				Timestamp: 1000,
				Channel:   tt.channel,
				Data:      json.RawMessage(`{}`),
			})
			if err != nil {
				t.Fatalf("json.Marshal() error: %v", err)
			}

			if string(got) != string(expected) {
				t.Errorf("Channel escaping mismatch for %q:\n  got:  %s\n  want: %s", tt.channel, string(got), string(expected))
			}
		})
	}
}

// TestBroadcastEnvelope_NilPayload verifies nil payload is normalized to JSON null,
// producing valid "data":null output that is byte-identical to json.Marshal.
func TestBroadcastEnvelope_NilPayload(t *testing.T) {
	t.Parallel()

	env, err := NewBroadcastEnvelope("BTC.trade", 1000, nil)
	if err != nil {
		t.Fatalf("NewBroadcastEnvelope() error: %v", err)
	}

	got := env.Build(1)

	// Verify "data":null is present
	if !strings.Contains(string(got), `"data":null`) {
		t.Errorf("Expected \"data\":null in output, got: %s", string(got))
	}

	// Verify byte-identical to json.Marshal with nil Data
	expected, err := json.Marshal(MessageEnvelope{
		Type:      "message",
		Seq:       1,
		Timestamp: 1000,
		Channel:   "BTC.trade",
		Data:      nil,
	})
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}

	if string(got) != string(expected) {
		t.Errorf("Nil payload mismatch:\n  got:  %s\n  want: %s", string(got), string(expected))
	}
}

// TestBroadcastEnvelope_EmptyPayload verifies []byte("null") payload produces valid JSON.
func TestBroadcastEnvelope_EmptyPayload(t *testing.T) {
	t.Parallel()

	env, err := NewBroadcastEnvelope("BTC.trade", 1000, []byte("null"))
	if err != nil {
		t.Fatalf("NewBroadcastEnvelope() error: %v", err)
	}

	got := env.Build(1)

	// Verify valid JSON
	var parsed map[string]any
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("Output is not valid JSON: %v\nOutput: %s", err, string(got))
	}

	if parsed["data"] != nil {
		t.Errorf("Expected data to be null, got %v", parsed["data"])
	}
}

// TestBroadcastEnvelope_LargePayload verifies integrity with large payloads.
func TestBroadcastEnvelope_LargePayload(t *testing.T) {
	t.Parallel()

	// Create 100KB payload
	payload := []byte(`{"data":"` + strings.Repeat("x", 100*1024) + `"}`)

	env, err := NewBroadcastEnvelope("BTC.trade", 1000, payload)
	if err != nil {
		t.Fatalf("NewBroadcastEnvelope() error: %v", err)
	}

	got := env.Build(1)

	// Verify byte-identical
	expected, err := json.Marshal(MessageEnvelope{
		Type:      "message",
		Seq:       1,
		Timestamp: 1000,
		Channel:   "BTC.trade",
		Data:      json.RawMessage(payload),
	})
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}

	if string(got) != string(expected) {
		t.Errorf("Large payload mismatch: got len=%d, want len=%d", len(got), len(expected))
	}
}

// TestBroadcastEnvelope_ValidJSON verifies output is always valid JSON with all fields.
func TestBroadcastEnvelope_ValidJSON(t *testing.T) {
	t.Parallel()

	env, err := NewBroadcastEnvelope("BTC.trade", 1708903200000, []byte(`{"price":45000}`))
	if err != nil {
		t.Fatalf("NewBroadcastEnvelope() error: %v", err)
	}

	got := env.Build(42)

	var parsed map[string]any
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("Output is not valid JSON: %v\nOutput: %s", err, string(got))
	}

	// Verify all fields present
	if parsed["type"] != "message" {
		t.Errorf("type = %v, want message", parsed["type"])
	}
	if parsed["seq"] != float64(42) {
		t.Errorf("seq = %v, want 42", parsed["seq"])
	}
	if parsed["ts"] != float64(1708903200000) {
		t.Errorf("ts = %v, want 1708903200000", parsed["ts"])
	}
	if parsed["channel"] != "BTC.trade" {
		t.Errorf("channel = %v, want BTC.trade", parsed["channel"])
	}
	if parsed["data"] == nil {
		t.Error("data should not be nil")
	}
}

// TestBroadcastEnvelope_Concurrent verifies concurrent Build() calls on the same
// envelope produce correct results without data races (run with -race).
func TestBroadcastEnvelope_Concurrent(t *testing.T) {
	t.Parallel()

	env, err := NewBroadcastEnvelope("BTC.trade", 1708903200000, []byte(`{"price":45000}`))
	if err != nil {
		t.Fatalf("NewBroadcastEnvelope() error: %v", err)
	}

	var wg sync.WaitGroup
	const numGoroutines = 100

	for i := range numGoroutines {
		seq := int64(i + 1)
		wg.Go(func() {
			got := env.Build(seq)

			// Verify each result matches json.Marshal
			expected, err := json.Marshal(MessageEnvelope{
				Type:      "message",
				Seq:       seq,
				Timestamp: 1708903200000,
				Channel:   "BTC.trade",
				Data:      json.RawMessage(`{"price":45000}`),
			})
			if err != nil {
				t.Errorf("json.Marshal() error for seq %d: %v", seq, err)
				return
			}

			if string(got) != string(expected) {
				t.Errorf("Concurrent Build(%d) mismatch:\n  got:  %s\n  want: %s", seq, string(got), string(expected))
			}
		})
	}

	wg.Wait()
}

// BenchmarkBroadcastEnvelope_Build measures per-call cost of Build().
func BenchmarkBroadcastEnvelope_Build(b *testing.B) {
	env, err := NewBroadcastEnvelope("BTC.trade", 1708903200000, []byte(`{"token":"BTC","price":45000,"amount_btc":1500000000}`))
	if err != nil {
		b.Fatalf("NewBroadcastEnvelope() error: %v", err)
	}

	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		_ = env.Build(int64(i))
	}
}

// BenchmarkBroadcastEnvelope_VsJsonMarshal compares Build() vs json.Marshal.
func BenchmarkBroadcastEnvelope_VsJsonMarshal(b *testing.B) {
	payload := json.RawMessage(`{"token":"BTC","price":45000,"amount_btc":1500000000}`)

	b.Run("Build", func(b *testing.B) {
		env, err := NewBroadcastEnvelope("BTC.trade", 1708903200000, payload)
		if err != nil {
			b.Fatalf("NewBroadcastEnvelope() error: %v", err)
		}
		b.ResetTimer()
		for i := 0; b.Loop(); i++ {
			_ = env.Build(int64(i))
		}
	})

	b.Run("JsonMarshal", func(b *testing.B) {
		for i := 0; b.Loop(); i++ {
			_, _ = json.Marshal(MessageEnvelope{
				Type:      "message",
				Seq:       int64(i),
				Timestamp: 1708903200000,
				Channel:   "BTC.trade",
				Data:      payload,
			})
		}
	})
}
