package revocation

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestRevoke_ByJTI(t *testing.T) {
	t.Parallel()
	s := New(zerolog.Nop())
	defer s.Close()

	err := s.Revoke(Entry{
		TenantID:  "acme",
		Type:      "token",
		JTI:       "tok-123",
		RevokedAt: time.Now().Unix(),
		ExpiresAt: time.Now().Add(1 * time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if s.Len() != 1 {
		t.Errorf("Len() = %d, want 1", s.Len())
	}
}

func TestRevoke_BySub(t *testing.T) {
	t.Parallel()
	s := New(zerolog.Nop())
	defer s.Close()

	err := s.Revoke(Entry{
		TenantID:  "acme",
		Type:      "user",
		Sub:       "alice",
		RevokedAt: time.Now().Unix(),
		ExpiresAt: time.Now().Add(1 * time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if s.Len() != 1 {
		t.Errorf("Len() = %d, want 1", s.Len())
	}
}

func TestRevoke_Validation(t *testing.T) {
	t.Parallel()
	s := New(zerolog.Nop())
	defer s.Close()

	tests := []struct {
		name  string
		entry Entry
	}{
		{"missing tenant_id", Entry{Type: "token", JTI: "x"}},
		{"invalid type", Entry{TenantID: "a", Type: "bad"}},
		{"user missing sub", Entry{TenantID: "a", Type: "user"}},
		{"token missing jti", Entry{TenantID: "a", Type: "token"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := s.Revoke(tt.entry); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestRevoke_Idempotent(t *testing.T) {
	t.Parallel()
	s := New(zerolog.Nop())
	defer s.Close()

	entry := Entry{
		TenantID:  "acme",
		Type:      "token",
		JTI:       "tok-dup",
		RevokedAt: time.Now().Unix(),
		ExpiresAt: time.Now().Add(1 * time.Hour).Unix(),
	}

	_ = s.Revoke(entry)
	_ = s.Revoke(entry)

	if s.Len() != 1 {
		t.Errorf("duplicate revocation created %d entries, want 1", s.Len())
	}
}

func TestPrune_RemovesExpired(t *testing.T) {
	t.Parallel()
	s := New(zerolog.Nop())
	defer s.Close()

	// Add an already-expired entry
	_ = s.Revoke(Entry{
		TenantID:  "acme",
		Type:      "token",
		JTI:       "expired",
		RevokedAt: time.Now().Add(-2 * time.Hour).Unix(),
		ExpiresAt: time.Now().Add(-1 * time.Hour).Unix(),
	})

	// Add a valid entry
	_ = s.Revoke(Entry{
		TenantID:  "acme",
		Type:      "token",
		JTI:       "valid",
		RevokedAt: time.Now().Unix(),
		ExpiresAt: time.Now().Add(1 * time.Hour).Unix(),
	})

	pruned := s.Prune()
	if pruned != 1 {
		t.Errorf("Prune() = %d, want 1", pruned)
	}
	if s.Len() != 1 {
		t.Errorf("Len() after prune = %d, want 1", s.Len())
	}
}

func TestSnapshot_ExcludesExpired(t *testing.T) {
	t.Parallel()
	s := New(zerolog.Nop())
	defer s.Close()

	_ = s.Revoke(Entry{
		TenantID: "acme", Type: "token", JTI: "expired",
		RevokedAt: time.Now().Add(-2 * time.Hour).Unix(),
		ExpiresAt: time.Now().Add(-1 * time.Hour).Unix(),
	})
	_ = s.Revoke(Entry{
		TenantID: "acme", Type: "token", JTI: "valid",
		RevokedAt: time.Now().Unix(),
		ExpiresAt: time.Now().Add(1 * time.Hour).Unix(),
	})

	snap := s.Snapshot()
	if len(snap) != 1 {
		t.Errorf("Snapshot() returned %d entries, want 1", len(snap))
	}
	if len(snap) > 0 && snap[0].JTI != "valid" {
		t.Errorf("Snapshot()[0].JTI = %q, want %q", snap[0].JTI, "valid")
	}
}

func TestRevoke_TenantIsolation(t *testing.T) {
	t.Parallel()
	s := New(zerolog.Nop())
	defer s.Close()

	_ = s.Revoke(Entry{
		TenantID: "acme", Type: "user", Sub: "alice",
		RevokedAt: time.Now().Unix(),
		ExpiresAt: time.Now().Add(1 * time.Hour).Unix(),
	})
	_ = s.Revoke(Entry{
		TenantID: "globex", Type: "user", Sub: "alice",
		RevokedAt: time.Now().Unix(),
		ExpiresAt: time.Now().Add(1 * time.Hour).Unix(),
	})

	// Same sub, different tenants — two separate entries
	if s.Len() != 2 {
		t.Errorf("Len() = %d, want 2 (different tenants)", s.Len())
	}
}
