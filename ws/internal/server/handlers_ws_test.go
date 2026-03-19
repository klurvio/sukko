package server

import (
	"context"
	"net/http"
	"testing"

	"github.com/klurvio/sukko/internal/shared/httputil"
)

// =============================================================================
// getClientIP Tests
// =============================================================================

func TestGetClientIP_XForwardedFor_SingleIP(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/", http.NoBody)
	req.Header.Set("X-Forwarded-For", "192.168.1.100")
	req.RemoteAddr = "10.0.0.1:12345"

	ip := httputil.GetClientIP(req)

	if ip != "192.168.1.100" {
		t.Errorf("getClientIP: got %q, want %q", ip, "192.168.1.100")
	}
}

func TestGetClientIP_XForwardedFor_MultipleIPs(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/", http.NoBody)
	// Multiple IPs in chain: client -> proxy1 -> proxy2
	req.Header.Set("X-Forwarded-For", "203.0.113.50, 198.51.100.178, 192.0.2.1")
	req.RemoteAddr = "10.0.0.1:12345"

	ip := httputil.GetClientIP(req)

	// Should return first IP (original client)
	if ip != "203.0.113.50" {
		t.Errorf("getClientIP: got %q, want %q", ip, "203.0.113.50")
	}
}

func TestGetClientIP_XForwardedFor_WithSpaces(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/", http.NoBody)
	req.Header.Set("X-Forwarded-For", "  192.168.1.100  ")
	req.RemoteAddr = "10.0.0.1:12345"

	ip := httputil.GetClientIP(req)

	// Should trim spaces
	if ip != "192.168.1.100" {
		t.Errorf("getClientIP: got %q, want %q", ip, "192.168.1.100")
	}
}

func TestGetClientIP_NoForwardedHeader_WithPort(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/", http.NoBody)
	req.RemoteAddr = "192.168.1.100:54321"

	ip := httputil.GetClientIP(req)

	// Should extract IP from RemoteAddr
	if ip != "192.168.1.100" {
		t.Errorf("getClientIP: got %q, want %q", ip, "192.168.1.100")
	}
}

func TestGetClientIP_NoForwardedHeader_IPv6WithPort(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/", http.NoBody)
	req.RemoteAddr = "[::1]:54321"

	ip := httputil.GetClientIP(req)

	// Should extract IPv6 IP from RemoteAddr
	if ip != "::1" {
		t.Errorf("getClientIP: got %q, want %q", ip, "::1")
	}
}

func TestGetClientIP_NoForwardedHeader_JustIP(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/", http.NoBody)
	req.RemoteAddr = "192.168.1.100" // No port (unusual but possible)

	ip := httputil.GetClientIP(req)

	// Should return as-is when no port
	if ip != "192.168.1.100" {
		t.Errorf("getClientIP: got %q, want %q", ip, "192.168.1.100")
	}
}

func TestGetClientIP_Localhost(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/", http.NoBody)
	req.RemoteAddr = "127.0.0.1:12345"

	ip := httputil.GetClientIP(req)

	if ip != "127.0.0.1" {
		t.Errorf("getClientIP: got %q, want %q", ip, "127.0.0.1")
	}
}

func TestGetClientIP_LocalhostIPv6(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/", http.NoBody)
	req.RemoteAddr = "[::1]:12345"

	ip := httputil.GetClientIP(req)

	if ip != "::1" {
		t.Errorf("getClientIP: got %q, want %q", ip, "::1")
	}
}

func TestGetClientIP_XForwardedFor_Empty(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/", http.NoBody)
	req.Header.Set("X-Forwarded-For", "")
	req.RemoteAddr = "10.0.0.5:9999"

	ip := httputil.GetClientIP(req)

	// Empty X-Forwarded-For should fall back to RemoteAddr
	if ip != "10.0.0.5" {
		t.Errorf("getClientIP: got %q, want %q", ip, "10.0.0.5")
	}
}

// =============================================================================
// Table-Driven Tests
// =============================================================================

func TestGetClientIP_TableDriven(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		xForwarded string
		remoteAddr string
		expectedIP string
	}{
		{
			name:       "X-Forwarded-For single IP",
			xForwarded: "192.168.1.100",
			remoteAddr: "10.0.0.1:12345",
			expectedIP: "192.168.1.100",
		},
		{
			name:       "X-Forwarded-For multiple IPs",
			xForwarded: "203.0.113.50, 198.51.100.178",
			remoteAddr: "10.0.0.1:12345",
			expectedIP: "203.0.113.50",
		},
		{
			name:       "No X-Forwarded-For with port",
			xForwarded: "",
			remoteAddr: "192.168.1.100:54321",
			expectedIP: "192.168.1.100",
		},
		{
			name:       "IPv6 with port",
			xForwarded: "",
			remoteAddr: "[2001:db8::1]:54321",
			expectedIP: "2001:db8::1",
		},
		{
			name:       "Localhost IPv4",
			xForwarded: "",
			remoteAddr: "127.0.0.1:12345",
			expectedIP: "127.0.0.1",
		},
		{
			name:       "Localhost IPv6",
			xForwarded: "",
			remoteAddr: "[::1]:12345",
			expectedIP: "::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/", http.NoBody)
			if tt.xForwarded != "" {
				req.Header.Set("X-Forwarded-For", tt.xForwarded)
			}
			req.RemoteAddr = tt.remoteAddr

			ip := httputil.GetClientIP(req)

			if ip != tt.expectedIP {
				t.Errorf("getClientIP: got %q, want %q", ip, tt.expectedIP)
			}
		})
	}
}
