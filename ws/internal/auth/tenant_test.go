package auth

import (
	"context"
	"testing"
)

func TestNewTenantIsolator(t *testing.T) {
	t.Run("with default config", func(t *testing.T) {
		iso := NewTenantIsolator(DefaultTenantIsolationConfig())
		if iso == nil {
			t.Fatal("expected non-nil isolator")
		}
		if iso.channelMapper == nil {
			t.Error("expected channel mapper to be initialized")
		}
		if iso.topicIsolator == nil {
			t.Error("expected topic isolator to be initialized")
		}
	})

	t.Run("with custom channel mapper", func(t *testing.T) {
		customMapper := NewChannelMapper(ChannelConfig{Separator: "/"})
		iso := NewTenantIsolator(
			DefaultTenantIsolationConfig(),
			WithChannelMapper(customMapper),
		)
		if iso.channelMapper != customMapper {
			t.Error("expected custom channel mapper to be used")
		}
	})

	t.Run("with custom audit logger", func(t *testing.T) {
		logger := &testAuditLogger{}
		iso := NewTenantIsolator(
			DefaultTenantIsolationConfig(),
			WithAuditLogger(logger),
		)
		if iso.auditLogger != logger {
			t.Error("expected custom audit logger to be used")
		}
	})
}

func TestTenantIsolator_CheckChannelAccess(t *testing.T) {
	iso := NewTenantIsolator(TenantIsolationConfig{
		CrossTenantRoles:      []string{"admin", "system"},
		SharedChannelPatterns: []string{"system.*", "broadcast.*"},
		AuditDenials:          true,
	})

	ctx := context.Background()

	tests := []struct {
		name          string
		claims        *Claims
		channel       string
		action        AccessAction
		expectAllowed bool
	}{
		{
			name:          "same tenant allowed",
			claims:        &Claims{TenantID: "acme"},
			channel:       "acme.BTC.trade",
			action:        ActionSubscribe,
			expectAllowed: true,
		},
		{
			name:          "different tenant denied",
			claims:        &Claims{TenantID: "acme"},
			channel:       "globex.BTC.trade",
			action:        ActionSubscribe,
			expectAllowed: false,
		},
		{
			name:          "nil claims allowed",
			claims:        nil,
			channel:       "acme.BTC.trade",
			action:        ActionSubscribe,
			expectAllowed: true,
		},
		{
			name:          "empty tenant allowed",
			claims:        &Claims{TenantID: ""},
			channel:       "acme.BTC.trade",
			action:        ActionSubscribe,
			expectAllowed: true,
		},
		{
			name:          "shared channel allowed",
			claims:        &Claims{TenantID: "acme"},
			channel:       "system.notifications",
			action:        ActionSubscribe,
			expectAllowed: true,
		},
		{
			name:          "broadcast shared channel allowed",
			claims:        &Claims{TenantID: "acme"},
			channel:       "broadcast.all",
			action:        ActionSubscribe,
			expectAllowed: true,
		},
		{
			name:          "admin role cross-tenant allowed",
			claims:        &Claims{TenantID: "acme", Roles: []string{"admin"}},
			channel:       "globex.BTC.trade",
			action:        ActionSubscribe,
			expectAllowed: true,
		},
		{
			name:          "publish same tenant allowed",
			claims:        &Claims{TenantID: "acme"},
			channel:       "acme.events",
			action:        ActionPublish,
			expectAllowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := iso.CheckChannelAccess(ctx, tt.claims, tt.channel, tt.action)

			if result.Allowed != tt.expectAllowed {
				t.Errorf("CheckChannelAccess() Allowed = %v, want %v (reason: %s)",
					result.Allowed, tt.expectAllowed, result.Reason)
			}

			if result.ResourceType != "channel" {
				t.Errorf("expected ResourceType 'channel', got %q", result.ResourceType)
			}

			if result.Resource != tt.channel {
				t.Errorf("expected Resource %q, got %q", tt.channel, result.Resource)
			}
		})
	}
}

func TestTenantIsolator_CheckTopicAccess(t *testing.T) {
	iso := NewTenantIsolator(TenantIsolationConfig{
		CrossTenantRoles:    []string{"admin"},
		SharedTopicPatterns: []string{"main.shared.*"},
	})

	ctx := context.Background()

	tests := []struct {
		name          string
		claims        *Claims
		topic         string
		action        AccessAction
		expectAllowed bool
	}{
		{
			name:          "same tenant consume allowed",
			claims:        &Claims{TenantID: "acme"},
			topic:         "main.acme.trade",
			action:        ActionConsume,
			expectAllowed: true,
		},
		{
			name:          "same tenant publish allowed",
			claims:        &Claims{TenantID: "acme"},
			topic:         "main.acme.trade",
			action:        ActionPublish,
			expectAllowed: true,
		},
		{
			name:          "different tenant denied",
			claims:        &Claims{TenantID: "acme"},
			topic:         "main.globex.trade",
			action:        ActionConsume,
			expectAllowed: false,
		},
		{
			name:          "shared topic allowed",
			claims:        &Claims{TenantID: "acme"},
			topic:         "main.shared.broadcast",
			action:        ActionConsume,
			expectAllowed: true,
		},
		{
			name:          "admin cross-tenant allowed",
			claims:        &Claims{TenantID: "acme", Roles: []string{"admin"}},
			topic:         "main.globex.trade",
			action:        ActionConsume,
			expectAllowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := iso.CheckTopicAccess(ctx, tt.claims, tt.topic, tt.action)

			if result.Allowed != tt.expectAllowed {
				t.Errorf("CheckTopicAccess() Allowed = %v, want %v (reason: %s)",
					result.Allowed, tt.expectAllowed, result.Reason)
			}

			if result.ResourceType != "topic" {
				t.Errorf("expected ResourceType 'topic', got %q", result.ResourceType)
			}
		})
	}
}

func TestTenantIsolator_MapClientToInternal(t *testing.T) {
	iso := NewTenantIsolator(DefaultTenantIsolationConfig())

	claims := &Claims{TenantID: "acme"}
	result := iso.MapClientToInternal(claims, "BTC.trade")

	expected := "acme.BTC.trade"
	if result != expected {
		t.Errorf("MapClientToInternal() = %q, want %q", result, expected)
	}
}

func TestTenantIsolator_MapInternalToClient(t *testing.T) {
	iso := NewTenantIsolator(DefaultTenantIsolationConfig())

	result := iso.MapInternalToClient("acme.BTC.trade")

	expected := "BTC.trade"
	if result != expected {
		t.Errorf("MapInternalToClient() = %q, want %q", result, expected)
	}
}

func TestTenantIsolator_BuildTopicName(t *testing.T) {
	iso := NewTenantIsolator(DefaultTenantIsolationConfig())

	result := iso.BuildTopicName("acme", "trade")

	expected := "main.acme.trade"
	if result != expected {
		t.Errorf("BuildTopicName() = %q, want %q", result, expected)
	}
}

func TestTenantIsolator_GetTenantFromChannel(t *testing.T) {
	iso := NewTenantIsolator(DefaultTenantIsolationConfig())

	result := iso.GetTenantFromChannel("acme.BTC.trade")

	expected := "acme"
	if result != expected {
		t.Errorf("GetTenantFromChannel() = %q, want %q", result, expected)
	}
}

func TestTenantIsolator_GetTenantFromTopic(t *testing.T) {
	iso := NewTenantIsolator(DefaultTenantIsolationConfig())

	result := iso.GetTenantFromTopic("main.acme.trade")

	expected := "acme"
	if result != expected {
		t.Errorf("GetTenantFromTopic() = %q, want %q", result, expected)
	}
}

func TestTenantIsolator_CanAccessResource(t *testing.T) {
	iso := NewTenantIsolator(DefaultTenantIsolationConfig())

	claims := &Claims{TenantID: "acme"}

	// Channel access
	if !iso.CanAccessResource(claims, "acme.BTC.trade", "channel", ActionSubscribe) {
		t.Error("expected channel access to be allowed for same tenant")
	}

	if iso.CanAccessResource(claims, "globex.BTC.trade", "channel", ActionSubscribe) {
		t.Error("expected channel access to be denied for different tenant")
	}

	// Topic access
	if !iso.CanAccessResource(claims, "main.acme.trade", "topic", ActionConsume) {
		t.Error("expected topic access to be allowed for same tenant")
	}

	// Unknown resource type
	if iso.CanAccessResource(claims, "something", "unknown", ActionSubscribe) {
		t.Error("expected unknown resource type to be denied")
	}
}

func TestTenantIsolator_AuditLogging(t *testing.T) {
	logger := &testAuditLogger{}

	iso := NewTenantIsolator(
		TenantIsolationConfig{
			AuditDenials:   true,
			AuditAllAccess: true,
		},
		WithAuditLogger(logger),
	)

	ctx := context.Background()
	claims := &Claims{TenantID: "acme"}

	// Access same tenant (allowed) - should be logged with AuditAllAccess
	iso.CheckChannelAccess(ctx, claims, "acme.trade", ActionSubscribe)
	if logger.allowedCount != 1 {
		t.Errorf("expected 1 allowed log, got %d", logger.allowedCount)
	}

	// Access different tenant (denied) - should be logged
	iso.CheckChannelAccess(ctx, claims, "globex.trade", ActionSubscribe)
	if logger.deniedCount != 1 {
		t.Errorf("expected 1 denied log, got %d", logger.deniedCount)
	}
}

func TestTenantIsolator_ResultFlags(t *testing.T) {
	iso := NewTenantIsolator(TenantIsolationConfig{
		CrossTenantRoles:      []string{"admin"},
		SharedChannelPatterns: []string{"system.*"},
	})

	ctx := context.Background()

	t.Run("cross-tenant flag set", func(t *testing.T) {
		claims := &Claims{TenantID: "acme", Roles: []string{"admin"}}
		result := iso.CheckChannelAccess(ctx, claims, "globex.trade", ActionSubscribe)

		if !result.IsCrossTenant {
			t.Error("expected IsCrossTenant to be true")
		}
	})

	t.Run("shared flag set", func(t *testing.T) {
		claims := &Claims{TenantID: "acme"}
		result := iso.CheckChannelAccess(ctx, claims, "system.broadcast", ActionSubscribe)

		if !result.IsShared {
			t.Error("expected IsShared to be true")
		}
	})
}

func TestExtractTenantContext(t *testing.T) {
	t.Run("with claims", func(t *testing.T) {
		claims := &Claims{
			TenantID: "acme",
			Roles:    []string{"admin", "user"},
			Groups:   []string{"vip"},
		}
		claims.Subject = "user123"

		ctx := ExtractTenantContext(claims)

		if ctx.TenantID != "acme" {
			t.Errorf("expected TenantID 'acme', got %q", ctx.TenantID)
		}
		if ctx.UserID != "user123" {
			t.Errorf("expected UserID 'user123', got %q", ctx.UserID)
		}
		if len(ctx.Roles) != 2 {
			t.Errorf("expected 2 roles, got %d", len(ctx.Roles))
		}
		if len(ctx.Groups) != 1 {
			t.Errorf("expected 1 group, got %d", len(ctx.Groups))
		}
	})

	t.Run("with nil claims", func(t *testing.T) {
		ctx := ExtractTenantContext(nil)

		if ctx.TenantID != "" {
			t.Errorf("expected empty TenantID, got %q", ctx.TenantID)
		}
	})
}

func TestDefaultTenantIsolationConfig(t *testing.T) {
	config := DefaultTenantIsolationConfig()

	if len(config.CrossTenantRoles) != 2 {
		t.Errorf("expected 2 cross-tenant roles, got %d", len(config.CrossTenantRoles))
	}

	if !config.AuditDenials {
		t.Error("expected AuditDenials to be true")
	}

	if config.AuditAllAccess {
		t.Error("expected AuditAllAccess to be false by default")
	}
}

// testAuditLogger is a test implementation of AuditLogger.
type testAuditLogger struct {
	deniedCount  int
	allowedCount int
	entries      []*AuditEntry
}

func (l *testAuditLogger) LogDenied(ctx context.Context, entry *AuditEntry) {
	l.deniedCount++
	l.entries = append(l.entries, entry)
}

func (l *testAuditLogger) LogAllowed(ctx context.Context, entry *AuditEntry) {
	l.allowedCount++
	l.entries = append(l.entries, entry)
}
