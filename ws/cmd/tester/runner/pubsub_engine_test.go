package runner

import (
	"slices"
	"testing"
	"time"

	testerws "github.com/klurvio/sukko/cmd/tester/ws"
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

// newCaptureTestUser builds a TestUser with initialized delivery + error state for driving
// onMessage directly (no live connection).
func newCaptureTestUser() *TestUser {
	return &TestUser{received: make(map[string]receivedMsg)}
}

// TestTestUser_ErrorCapture covers FR-001: subscribe_error/publish_error frames are captured with
// their top-level code and matched jointly; delivery envelopes are unaffected; error frames never
// leak into delivery tracking even when carrying a msg_id-shaped payload.
func TestTestUser_ErrorCapture(t *testing.T) {
	t.Parallel()

	t.Run("captures subscribe_error and publish_error with code", func(t *testing.T) {
		t.Parallel()
		u := newCaptureTestUser()
		u.onMessage(testerws.Message{Type: respTypeSubscribeError, Code: "invalid_request"})
		u.onMessage(testerws.Message{Type: respTypePublishError, Code: "forbidden"})
		if !u.HasErrorMatching(respTypeSubscribeError, "invalid_request") {
			t.Error("subscribe_error/invalid_request not captured")
		}
		if !u.HasErrorMatching(respTypePublishError, "forbidden") {
			t.Error("publish_error/forbidden not captured")
		}
		// Error frames must NOT be counted as delivered.
		if got := u.ReceivedCount(); got != 0 {
			t.Errorf("ReceivedCount = %d, want 0 (error frames must not reach delivery tracking)", got)
		}
	})

	t.Run("joint (type,code) discrimination", func(t *testing.T) {
		t.Parallel()
		u := newCaptureTestUser()
		u.onMessage(testerws.Message{Type: respTypeSubscribeError, Code: "invalid_request"})
		// right type, wrong code
		if u.HasErrorMatching(respTypeSubscribeError, "forbidden") {
			t.Error("matched on wrong code")
		}
		// wrong type, right code
		if u.HasErrorMatching(respTypePublishError, "invalid_request") {
			t.Error("matched on wrong type")
		}
	})

	t.Run("publish_error with msg_id-shaped payload does not reach delivery tracking", func(t *testing.T) {
		t.Parallel()
		u := newCaptureTestUser()
		// A publish_error whose Data happens to carry a msg_id-shaped field MUST still be an error,
		// never a delivered message (disjoint-types + first-branch-return guarantee).
		u.onMessage(testerws.Message{Type: respTypePublishError, Code: "forbidden", Data: []byte(`{"msg_id":"m1"}`)})
		if u.ReceivedCount() != 0 {
			t.Error("publish_error with msg_id leaked into delivery tracking")
		}
		if u.HasReceived("m1") {
			t.Error("publish_error msg_id tracked as delivered")
		}
		if !u.HasErrorMatching(respTypePublishError, "forbidden") {
			t.Error("publish_error not captured")
		}
	})

	t.Run("delivery envelopes still tracked; error frames disjoint", func(t *testing.T) {
		t.Parallel()
		u := newCaptureTestUser()
		u.onMessage(testerws.Message{Type: "message", Data: []byte(`{"msg_id":"d1"}`)})
		u.onMessage(testerws.Message{Type: "publish", Data: []byte(`{"msg_id":"d2"}`)})
		if !u.HasReceived("d1") || !u.HasReceived("d2") {
			t.Error("delivery envelopes not tracked after adding error-capture branch")
		}
		if u.ReceivedCount() != 2 {
			t.Errorf("ReceivedCount = %d, want 2", u.ReceivedCount())
		}
	})

	t.Run("ClearErrors resets errors but not delivery, and vice-versa", func(t *testing.T) {
		t.Parallel()
		u := newCaptureTestUser()
		u.onMessage(testerws.Message{Type: "message", Data: []byte(`{"msg_id":"d1"}`)})
		u.onMessage(testerws.Message{Type: respTypeSubscribeError, Code: "invalid_request"})

		u.ClearErrors()
		if u.HasErrorMatching(respTypeSubscribeError, "invalid_request") {
			t.Error("ClearErrors did not reset error frames")
		}
		if !u.HasReceived("d1") {
			t.Error("ClearErrors wrongly cleared delivery state")
		}

		u.onMessage(testerws.Message{Type: respTypePublishError, Code: "forbidden"})
		u.ClearReceived()
		if u.HasReceived("d1") {
			t.Error("ClearReceived did not reset delivery state")
		}
		if !u.HasErrorMatching(respTypePublishError, "forbidden") {
			t.Error("ClearReceived wrongly cleared error frames")
		}
	})
}
