package compose

import (
	"testing"
)

func TestNewManager(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		projectDir string
		wantErr    bool
	}{
		{"valid", "/tmp/project", false},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m, err := NewManager(tt.projectDir)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error for empty project dir")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if m == nil {
				t.Fatal("expected non-nil manager")
			}
			if m.projectDir != tt.projectDir {
				t.Errorf("projectDir = %q, want %q", m.projectDir, tt.projectDir)
			}
		})
	}
}
