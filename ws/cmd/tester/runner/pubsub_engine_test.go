package runner

import (
	"slices"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// testUserWith builds an in-package TestUser whose received-tracker is pre-seeded with the given
// message IDs. buildResult only inspects presence (via HasReceived) and Subject, so a zero-value
// receivedMsg is sufficient.
func testUserWith(subject string, receivedIDs ...string) *TestUser {
	u := &TestUser{Subject: subject, received: make(map[string]receivedMsg)}
	for _, id := range receivedIDs {
		u.received[id] = receivedMsg{}
	}
	return u
}

// TestBuildResult pins the pure leak-detection logic buildResult computes from the users' received
// state — the committed, race-detector-runnable guard for the tenant-isolation NEGATIVE assertion
// (FR-009 / SC-004a). The load-bearing row is "leak": when a must-NOT-receive user receives the
// measured message, MisroutedTo MUST be populated (→ fail) while Delivered STAYS true (the expected
// receiver still got it — Delivered only flips false on a genuine miss).
func TestBuildResult(t *testing.T) {
	t.Parallel()
	const msgID = "msg-1"
	e := NewPubSubEngine(PubSubEngineConfig{Logger: zerolog.Nop()})

	tests := []struct {
		name        string
		expected    []string // subjects that SHOULD receive
		allUsers    []*TestUser
		wantDeliver bool
		wantMisrout []string
		wantMissing []string
	}{
		{
			name:        "clean delivery — expected got it, other did not",
			expected:    []string{"A"},
			allUsers:    []*TestUser{testUserWith("A", msgID), testUserWith("B")},
			wantDeliver: true,
			wantMisrout: nil,
			wantMissing: nil,
		},
		{
			name:        "leak — expected got it AND other also got it (Delivered stays true)",
			expected:    []string{"A"},
			allUsers:    []*TestUser{testUserWith("A", msgID), testUserWith("B", msgID)},
			wantDeliver: true, // expected receiver still got it; the FAIL is driven by MisroutedTo, not Delivered
			wantMisrout: []string{"B"},
			wantMissing: nil,
		},
		{
			name:        "miss — expected did NOT receive (Delivered false)",
			expected:    []string{"A"},
			allUsers:    []*TestUser{testUserWith("A"), testUserWith("B")},
			wantDeliver: false,
			wantMisrout: nil,
			wantMissing: []string{"A"},
		},
		{
			name:        "miss + leak — expected missed AND other leaked",
			expected:    []string{"A"},
			allUsers:    []*TestUser{testUserWith("A"), testUserWith("B", msgID)},
			wantDeliver: false,
			wantMisrout: []string{"B"},
			wantMissing: []string{"A"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			expectedSet := make(map[string]bool, len(tt.expected))
			for _, s := range tt.expected {
				expectedSet[s] = true
			}
			got := e.buildResult("tenant.general.test", msgID, time.Now(), expectedSet, tt.allUsers)

			if got.Delivered != tt.wantDeliver {
				t.Errorf("Delivered = %v, want %v", got.Delivered, tt.wantDeliver)
			}
			if !slices.Equal(got.MisroutedTo, tt.wantMisrout) {
				t.Errorf("MisroutedTo = %v, want %v", got.MisroutedTo, tt.wantMisrout)
			}
			if !slices.Equal(got.Missing, tt.wantMissing) {
				t.Errorf("Missing = %v, want %v", got.Missing, tt.wantMissing)
			}
		})
	}
}
