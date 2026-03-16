package platform

import (
	"maps"
	"reflect"
	"testing"
)

// TestSharedDefaultsConsistency uses reflection to verify that envDefault tags
// for shared environment variables are consistent across GatewayConfig,
// ServerConfig, and ProvisioningConfig.
//
// This catches drift when someone updates a default in one config but forgets
// the other(s). The expected values come from the constants in defaults.go.
func TestSharedDefaultsConsistency(t *testing.T) {
	t.Parallel()

	// Build env var → envDefault maps for each config struct.
	gatewayDefaults := extractEnvDefaults(reflect.TypeFor[GatewayConfig]())
	serverDefaults := extractEnvDefaults(reflect.TypeFor[ServerConfig]())
	provisioningDefaults := extractEnvDefaults(reflect.TypeFor[ProvisioningConfig]())

	// Shared env vars and their expected default values (from defaults.go constants).
	// LOG_LEVEL, LOG_FORMAT, and ENVIRONMENT are structurally guaranteed by BaseConfig
	// embedding — no need to verify them here. The reflection recurses into embedded
	// structs, so all three configs share the same BaseConfig defaults by construction.

	sharedExpectations := []struct {
		envVar   string
		expected string
		configs  []struct {
			name     string
			defaults map[string]string
		}
	}{
		// Default tenant ID (gateway and server)
		{
			envVar:   "DEFAULT_TENANT_ID",
			expected: DefaultTenantID,
			configs: []struct {
				name     string
				defaults map[string]string
			}{
				{"GatewayConfig", gatewayDefaults},
				{"ServerConfig", serverDefaults},
			},
		},
		// Provisioning gRPC (gateway and server)
		{
			envVar:   "PROVISIONING_GRPC_ADDR",
			expected: DefaultProvisioningGRPCAddr,
			configs: []struct {
				name     string
				defaults map[string]string
			}{
				{"GatewayConfig", gatewayDefaults},
				{"ServerConfig", serverDefaults},
			},
		},
		{
			envVar:   "PROVISIONING_GRPC_RECONNECT_DELAY",
			expected: DefaultGRPCReconnectDelay,
			configs: []struct {
				name     string
				defaults map[string]string
			}{
				{"GatewayConfig", gatewayDefaults},
				{"ServerConfig", serverDefaults},
			},
		},
		{
			envVar:   "PROVISIONING_GRPC_RECONNECT_MAX_DELAY",
			expected: DefaultGRPCReconnectMaxDelay,
			configs: []struct {
				name     string
				defaults map[string]string
			}{
				{"GatewayConfig", gatewayDefaults},
				{"ServerConfig", serverDefaults},
			},
		},
		// Auth enabled (gateway and provisioning)
		{
			envVar:   "AUTH_ENABLED",
			expected: DefaultAuthEnabled,
			configs: []struct {
				name     string
				defaults map[string]string
			}{
				{"GatewayConfig", gatewayDefaults},
				{"ProvisioningConfig", provisioningDefaults},
			},
		},
		// Kafka/Namespace (server and provisioning)
		{
			envVar:   "KAFKA_TOPIC_NAMESPACE_OVERRIDE",
			expected: DefaultKafkaTopicNamespaceOverride,
			configs: []struct {
				name     string
				defaults map[string]string
			}{
				{"ServerConfig", serverDefaults},
				{"ProvisioningConfig", provisioningDefaults},
			},
		},
		{
			envVar:   "VALID_NAMESPACES",
			expected: DefaultValidNamespaces,
			configs: []struct {
				name     string
				defaults map[string]string
			}{
				{"ServerConfig", serverDefaults},
				{"ProvisioningConfig", provisioningDefaults},
			},
		},
	}

	for _, tc := range sharedExpectations {
		for _, cfg := range tc.configs {
			got, ok := cfg.defaults[tc.envVar]
			if !ok {
				t.Errorf("%s: env var %s not found in struct", cfg.name, tc.envVar)
				continue
			}
			if got != tc.expected {
				t.Errorf("%s: envDefault for %s = %q, want %q (from defaults.go)", cfg.name, tc.envVar, got, tc.expected)
			}
		}
	}

	// Semantic consistency: HTTP timeout defaults should match across all services.
	// Gateway uses GATEWAY_*_TIMEOUT env vars; server and provisioning use HTTP_*_TIMEOUT.
	// The default values should be identical since they serve the same purpose.
	httpTimeoutPairs := []struct {
		name            string
		gatewayEnvVar   string
		serverEnvVar    string
		expectedDefault string
	}{
		{"read timeout", "GATEWAY_READ_TIMEOUT", "HTTP_READ_TIMEOUT", DefaultHTTPReadTimeout},
		{"write timeout", "GATEWAY_WRITE_TIMEOUT", "HTTP_WRITE_TIMEOUT", DefaultHTTPWriteTimeout},
		{"idle timeout", "GATEWAY_IDLE_TIMEOUT", "HTTP_IDLE_TIMEOUT", DefaultHTTPIdleTimeout},
	}

	for _, tc := range httpTimeoutPairs {
		gwVal, gwOk := gatewayDefaults[tc.gatewayEnvVar]
		srvVal, srvOk := serverDefaults[tc.serverEnvVar]
		provVal, provOk := provisioningDefaults[tc.serverEnvVar]

		if !gwOk {
			t.Errorf("GatewayConfig missing %s", tc.gatewayEnvVar)
		}
		if !srvOk {
			t.Errorf("ServerConfig missing %s", tc.serverEnvVar)
		}
		if !provOk {
			t.Errorf("ProvisioningConfig missing %s", tc.serverEnvVar)
		}
		if gwOk && gwVal != tc.expectedDefault {
			t.Errorf("GatewayConfig %s = %q, want %q", tc.gatewayEnvVar, gwVal, tc.expectedDefault)
		}
		if srvOk && srvVal != tc.expectedDefault {
			t.Errorf("ServerConfig %s = %q, want %q", tc.serverEnvVar, srvVal, tc.expectedDefault)
		}
		if provOk && provVal != tc.expectedDefault {
			t.Errorf("ProvisioningConfig %s = %q, want %q", tc.serverEnvVar, provVal, tc.expectedDefault)
		}
	}
}

// extractEnvDefaults uses reflection to build a map of env var name → envDefault value
// from a struct's field tags.
func extractEnvDefaults(t reflect.Type) map[string]string {
	defaults := make(map[string]string)
	for field := range t.Fields() {
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			maps.Copy(defaults, extractEnvDefaults(field.Type))
			continue
		}
		envVar := field.Tag.Get("env")
		envDefault := field.Tag.Get("envDefault")
		if envVar != "" {
			defaults[envVar] = envDefault
		}
	}
	return defaults
}
