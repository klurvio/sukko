package runner

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/cmd/tester/auth"
)

func TestIsRoutingRulesEditionGated(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "feature gate 403 (Community stack)",
			err:  errors.New(`set routing rules: HTTP 403: {"code":"EDITION_LIMIT","message":"This feature requires pro edition or higher"}`),
			want: true,
		},
		{
			name: "rule count limit — same code, different message, must NOT be tolerated",
			err:  errors.New(`set routing rules: HTTP 403: {"code":"EDITION_LIMIT","message":"edition limit exceeded: routing_rules_per_tenant 11 > 10 (pro)"}`),
			want: false,
		},
		{
			name: "plain 403 without edition gate",
			err:  errors.New(`set routing rules: HTTP 403: {"code":"FORBIDDEN","message":"admin role required"}`),
			want: false,
		},
		{
			name: "network error",
			err:  errors.New("set routing rules: dial tcp: connection refused"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isRoutingRulesEditionGated(tt.err); got != tt.want {
				t.Errorf("isRoutingRulesEditionGated(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// setupSuiteRoutingRules must tolerate ONLY the Pro feature gate: on Community the
// Kafka backend (the sole consumer of routing rules) is itself edition-gated, so the
// 403 feature-gate response means the rules are unnecessary — not that setup broke.
func TestSetupSuiteRoutingRules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr bool
	}{
		{
			name:    "rules accepted (Pro/kafka stack)",
			status:  http.StatusOK,
			body:    `{}`,
			wantErr: false,
		},
		{
			name:    "edition feature gate tolerated (Community stack)",
			status:  http.StatusForbidden,
			body:    `{"code":"EDITION_LIMIT","message":"This feature requires pro edition or higher"}`,
			wantErr: false,
		},
		{
			name:    "rule count limit surfaces as error",
			status:  http.StatusForbidden,
			body:    `{"code":"EDITION_LIMIT","message":"edition limit exceeded: routing_rules_per_tenant 11 > 10 (pro)"}`,
			wantErr: true,
		},
		{
			name:    "server error surfaces as error",
			status:  http.StatusInternalServerError,
			body:    `{"code":"INTERNAL_ERROR","message":"boom"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			client := auth.NewProvisioningClient(srv.URL, nil, zerolog.Nop())
			err := setupSuiteRoutingRules(context.Background(), client, "tenant-x", zerolog.Nop())
			if (err != nil) != tt.wantErr {
				t.Errorf("setupSuiteRoutingRules() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
