package license

import "testing"

func TestRequiredEdition(t *testing.T) {
	t.Parallel()
	proFeatures := []Feature{
		KafkaBackend, NATSJetStreamBackend, SSETransport,
		PerTenantChannelRules, PerTenantConnectionLimits, PerTenantConfigurableQuotas,
		TenantLifecycleManager, Alerting, Analytics, ConnectionTracing, AdminUI,
		TokenRevocation, Webhooks, MessageHistory, ChannelPatternsCEL, DeltaCompression,
	}
	for _, f := range proFeatures {
		if got := RequiredEdition(f); got != Pro {
			t.Errorf("RequiredEdition(%q) = %q, want Pro", f, got)
		}
	}

	enterpriseFeatures := []Feature{
		PushNotifications, IPAllowlisting, AuditLogging,
		E2EEncryption, PriorityRouting, CustomQuotaPolicies,
	}
	for _, f := range enterpriseFeatures {
		if got := RequiredEdition(f); got != Enterprise {
			t.Errorf("RequiredEdition(%q) = %q, want Enterprise", f, got)
		}
	}

	// Unknown feature → Community (not gated)
	if got := RequiredEdition(Feature("nonexistent")); got != Community {
		t.Errorf("RequiredEdition(unknown) = %q, want Community", got)
	}
}

func TestEditionHasFeature(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		edition Edition
		feature Feature
		want    bool
	}{
		// Community cannot use Pro features
		{"community+kafka", Community, KafkaBackend, false},
		{"community+alerting", Community, Alerting, false},
		{"community+push", Community, PushNotifications, false},

		// Pro can use Pro features, not Enterprise
		{"pro+kafka", Pro, KafkaBackend, true},
		{"pro+alerting", Pro, Alerting, true},
		{"pro+push", Pro, PushNotifications, false},
		{"pro+audit", Pro, AuditLogging, false},

		// Enterprise can use everything
		{"enterprise+kafka", Enterprise, KafkaBackend, true},
		{"enterprise+push", Enterprise, PushNotifications, true},
		{"enterprise+audit", Enterprise, AuditLogging, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := EditionHasFeature(tt.edition, tt.feature); got != tt.want {
				t.Errorf("EditionHasFeature(%q, %q) = %v, want %v", tt.edition, tt.feature, got, tt.want)
			}
		})
	}
}
