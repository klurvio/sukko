package messaging

import (
	"encoding/json"
	"sync"
	"testing"
)

// =============================================================================
// SequenceGenerator Tests
// =============================================================================

func TestSequenceGenerator_Next(t *testing.T) {
	sg := NewSequenceGenerator()

	// First sequence should be 1
	seq := sg.Next()
	if seq != 1 {
		t.Errorf("First sequence should be 1, got %d", seq)
	}

	// Second sequence should be 2
	seq = sg.Next()
	if seq != 2 {
		t.Errorf("Second sequence should be 2, got %d", seq)
	}

	// Third sequence should be 3
	seq = sg.Next()
	if seq != 3 {
		t.Errorf("Third sequence should be 3, got %d", seq)
	}
}

func TestSequenceGenerator_Monotonic(t *testing.T) {
	sg := NewSequenceGenerator()
	const count = 1000

	var prev int64 = 0
	for i := 0; i < count; i++ {
		seq := sg.Next()
		if seq <= prev {
			t.Errorf("Sequence %d not greater than previous %d at iteration %d", seq, prev, i)
		}
		prev = seq
	}
}

func TestSequenceGenerator_Current(t *testing.T) {
	sg := NewSequenceGenerator()

	// Initial current should be 0
	if sg.Current() != 0 {
		t.Errorf("Initial current should be 0, got %d", sg.Current())
	}

	// After Next(), current should match
	sg.Next()
	if sg.Current() != 1 {
		t.Errorf("Current after one Next() should be 1, got %d", sg.Current())
	}

	// Current doesn't increment
	curr1 := sg.Current()
	curr2 := sg.Current()
	if curr1 != curr2 {
		t.Error("Current should not change between calls")
	}
}

func TestSequenceGenerator_Reset(t *testing.T) {
	sg := NewSequenceGenerator()

	// Generate some sequences
	sg.Next()
	sg.Next()
	sg.Next()

	if sg.Current() != 3 {
		t.Errorf("Current should be 3 before reset, got %d", sg.Current())
	}

	// Reset
	sg.Reset()

	if sg.Current() != 0 {
		t.Errorf("Current should be 0 after reset, got %d", sg.Current())
	}

	// Next after reset should be 1
	seq := sg.Next()
	if seq != 1 {
		t.Errorf("First sequence after reset should be 1, got %d", seq)
	}
}

func TestSequenceGenerator_Concurrent(t *testing.T) {
	sg := NewSequenceGenerator()
	const numGoroutines = 100
	const numOps = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	results := make(chan int64, numGoroutines*numOps)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				results <- sg.Next()
			}
		}()
	}

	wg.Wait()
	close(results)

	// Verify all sequences are unique
	seen := make(map[int64]bool)
	for seq := range results {
		if seen[seq] {
			t.Errorf("Duplicate sequence number: %d", seq)
		}
		seen[seq] = true
	}

	// Verify correct total count
	expected := numGoroutines * numOps
	if len(seen) != expected {
		t.Errorf("Expected %d unique sequences, got %d", expected, len(seen))
	}

	// Verify final counter matches
	if sg.Current() != int64(expected) {
		t.Errorf("Final counter should be %d, got %d", expected, sg.Current())
	}
}

// =============================================================================
// MessageEnvelope Tests
// =============================================================================

func TestMessageEnvelope_Serialize(t *testing.T) {
	data := json.RawMessage(`{"tokenId":"BTC","price":"100.50"}`)
	envelope := &MessageEnvelope{
		Seq:       1,
		Timestamp: 1234567890,
		Type:      "price:update",
		Priority:  PRIORITY_HIGH,
		Data:      data,
	}

	result, err := envelope.Serialize()
	if err != nil {
		t.Fatalf("Serialize failed: %v", err)
	}

	// Deserialize and verify
	var decoded map[string]any
	if err := json.Unmarshal(result, &decoded); err != nil {
		t.Fatalf("Failed to decode serialized envelope: %v", err)
	}

	if decoded["seq"].(float64) != 1 {
		t.Errorf("seq: got %v, want 1", decoded["seq"])
	}
	if decoded["ts"].(float64) != 1234567890 {
		t.Errorf("ts: got %v, want 1234567890", decoded["ts"])
	}
	if decoded["type"].(string) != "price:update" {
		t.Errorf("type: got %v, want price:update", decoded["type"])
	}
	if decoded["data"] == nil {
		t.Error("data should not be nil")
	}
}

func TestMessageEnvelope_Priority(t *testing.T) {
	tests := []struct {
		name     string
		priority MessagePriority
	}{
		{"CRITICAL", PRIORITY_CRITICAL},
		{"HIGH", PRIORITY_HIGH},
		{"NORMAL", PRIORITY_NORMAL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envelope := &MessageEnvelope{
				Seq:      1,
				Type:     "test",
				Priority: tt.priority,
				Data:     json.RawMessage(`{}`),
			}

			result, err := envelope.Serialize()
			if err != nil {
				t.Fatalf("Serialize failed: %v", err)
			}

			var decoded map[string]any
			if err := json.Unmarshal(result, &decoded); err != nil {
				t.Fatalf("Failed to decode: %v", err)
			}

			// Priority should be included in JSON
			if tt.priority != PRIORITY_CRITICAL { // 0 is omitted by omitempty
				if decoded["priority"] == nil {
					t.Errorf("priority should be present for %s", tt.name)
				}
			}
		})
	}
}

func TestMessageEnvelope_DataPreserved(t *testing.T) {
	originalData := `{"nested":{"key":"value"},"array":[1,2,3]}`
	envelope := &MessageEnvelope{
		Seq:  1,
		Type: "test",
		Data: json.RawMessage(originalData),
	}

	result, err := envelope.Serialize()
	if err != nil {
		t.Fatalf("Serialize failed: %v", err)
	}

	// Verify the data is preserved exactly (no re-encoding artifacts)
	var decoded MessageEnvelope
	if err := json.Unmarshal(result, &decoded); err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}

	var decodedData map[string]any
	if err := json.Unmarshal(decoded.Data, &decodedData); err != nil {
		t.Fatalf("Failed to decode data: %v", err)
	}

	// Verify nested structure preserved
	nested, ok := decodedData["nested"].(map[string]any)
	if !ok {
		t.Fatal("nested should be a map")
	}
	if nested["key"].(string) != "value" {
		t.Error("nested key should be 'value'")
	}

	// Verify array preserved
	arr, ok := decodedData["array"].([]any)
	if !ok {
		t.Fatal("array should be an array")
	}
	if len(arr) != 3 {
		t.Error("array should have 3 elements")
	}
}

func TestWrapMessage(t *testing.T) {
	sg := NewSequenceGenerator()
	data := []byte(`{"tokenId":"BTC","price":"100.50"}`)

	envelope, err := WrapMessage(data, "price:update", PRIORITY_HIGH, sg)
	if err != nil {
		t.Fatalf("WrapMessage failed: %v", err)
	}

	if envelope.Seq != 1 {
		t.Errorf("Seq should be 1, got %d", envelope.Seq)
	}
	if envelope.Type != "price:update" {
		t.Errorf("Type: got %s, want price:update", envelope.Type)
	}
	if envelope.Priority != PRIORITY_HIGH {
		t.Errorf("Priority: got %d, want %d", envelope.Priority, PRIORITY_HIGH)
	}
	if envelope.Timestamp == 0 {
		t.Error("Timestamp should be set")
	}
	if string(envelope.Data) != string(data) {
		t.Error("Data should match input")
	}
}

func TestWrapMessage_SequenceIncrement(t *testing.T) {
	sg := NewSequenceGenerator()

	env1, _ := WrapMessage([]byte(`{}`), "test", PRIORITY_NORMAL, sg)
	env2, _ := WrapMessage([]byte(`{}`), "test", PRIORITY_NORMAL, sg)
	env3, _ := WrapMessage([]byte(`{}`), "test", PRIORITY_NORMAL, sg)

	if env1.Seq != 1 || env2.Seq != 2 || env3.Seq != 3 {
		t.Errorf("Sequences should be 1, 2, 3; got %d, %d, %d",
			env1.Seq, env2.Seq, env3.Seq)
	}
}

func TestMessagePriority_Values(t *testing.T) {
	// Verify priority constants have expected values
	if PRIORITY_CRITICAL != 0 {
		t.Errorf("PRIORITY_CRITICAL should be 0, got %d", PRIORITY_CRITICAL)
	}
	if PRIORITY_HIGH != 1 {
		t.Errorf("PRIORITY_HIGH should be 1, got %d", PRIORITY_HIGH)
	}
	if PRIORITY_NORMAL != 2 {
		t.Errorf("PRIORITY_NORMAL should be 2, got %d", PRIORITY_NORMAL)
	}
}
