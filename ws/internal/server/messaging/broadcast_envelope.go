package messaging

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// broadcastPrefix is the constant JSON prefix shared by all broadcast messages.
// Field order matches json.Marshal(MessageEnvelope{}): type, seq, ts, channel, data.
// Effectively constant — MUST NOT be modified after package init.
var broadcastPrefix = []byte(`{"type":"message","seq":`)

// maxInt64Digits is the maximum number of digits in a base-10 int64 representation.
// Used for pre-calculating buffer capacity in Build().
const maxInt64Digits = 20

// Suffix fragment sizes (byte lengths of JSON structural tokens).
const (
	suffixTsKey      = 6  // ,"ts":
	suffixChannelKey = 11 // ,"channel":
	suffixDataKey    = 8  // ,"data":
	suffixClose      = 1  // }
)

// BroadcastEnvelope holds pre-computed shared byte fragments for efficient
// per-client message assembly. Immutable after creation — safe for concurrent
// reads by multiple write pump goroutines.
//
// Architecture: The broadcast loop creates one BroadcastEnvelope per event.
// Each write pump goroutine calls Build(seq) to assemble per-client bytes.
// This defers the ~100ns byte assembly to write pumps (parallelized across cores)
// while keeping the broadcast loop at ~25ns/client (atomic + channel send).
type BroadcastEnvelope struct {
	prefix        []byte // {"type":"message","seq":
	sharedSuffix  []byte // ,"ts":...,"channel":"...","data":...}
	totalEstimate int    // pre-calculated capacity for Build()
}

// NewBroadcastEnvelope pre-computes shared byte fragments from broadcast parameters.
// The channel is JSON-escaped via json.Marshal to match json.Marshal(MessageEnvelope{}) output.
// A nil payload is normalized to the JSON literal "null" to produce valid JSON output.
// Called once per broadcast event — O(1), not per-client.
func NewBroadcastEnvelope(channel string, timestamp int64, payload []byte) (*BroadcastEnvelope, error) {
	// Normalize nil payload to JSON null (produces valid "data":null)
	if payload == nil {
		payload = []byte("null")
	}

	// JSON-escape the channel name to match json.Marshal behavior.
	// This handles quotes, backslashes, control characters, and Unicode.
	channelJSON, err := json.Marshal(channel)
	if err != nil {
		return nil, fmt.Errorf("json-escape channel %q: %w", channel, err)
	}

	// Build sharedSuffix: ,"ts":<timestamp>,"channel":<channelJSON>,"data":<payload>}
	suffixCap := suffixTsKey + maxInt64Digits + suffixChannelKey + len(channelJSON) + suffixDataKey + len(payload) + suffixClose
	suffix := make([]byte, 0, suffixCap)
	suffix = append(suffix, `,"ts":`...)
	suffix = strconv.AppendInt(suffix, timestamp, 10)
	suffix = append(suffix, `,"channel":`...)
	suffix = append(suffix, channelJSON...)
	suffix = append(suffix, `,"data":`...)
	suffix = append(suffix, payload...)
	suffix = append(suffix, '}')

	return &BroadcastEnvelope{
		prefix:        broadcastPrefix,
		sharedSuffix:  suffix,
		totalEstimate: len(broadcastPrefix) + maxInt64Digits + len(suffix),
	}, nil
}

// Build assembles the final message bytes for a specific client sequence number.
// Returns a new []byte owned by the caller (safe to send via WebSocket).
// Called per-client in write pump goroutines — O(1), ~100ns.
func (e *BroadcastEnvelope) Build(seq int64) []byte {
	buf := make([]byte, 0, e.totalEstimate)
	buf = append(buf, e.prefix...)
	buf = strconv.AppendInt(buf, seq, 10)
	buf = append(buf, e.sharedSuffix...)
	return buf
}
