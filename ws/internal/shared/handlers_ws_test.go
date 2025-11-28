package shared

import (
	"net/http"
	"testing"
)

// =============================================================================
// getClientIP Tests
// =============================================================================

func TestGetClientIP_XForwardedFor_SingleIP(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "192.168.1.100")
	req.RemoteAddr = "10.0.0.1:12345"

	ip := getClientIP(req)

	if ip != "192.168.1.100" {
		t.Errorf("getClientIP: got %q, want %q", ip, "192.168.1.100")
	}
}

func TestGetClientIP_XForwardedFor_MultipleIPs(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	// Multiple IPs in chain: client -> proxy1 -> proxy2
	req.Header.Set("X-Forwarded-For", "203.0.113.50, 198.51.100.178, 192.0.2.1")
	req.RemoteAddr = "10.0.0.1:12345"

	ip := getClientIP(req)

	// Should return first IP (original client)
	if ip != "203.0.113.50" {
		t.Errorf("getClientIP: got %q, want %q", ip, "203.0.113.50")
	}
}

func TestGetClientIP_XForwardedFor_WithSpaces(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "  192.168.1.100  ")
	req.RemoteAddr = "10.0.0.1:12345"

	ip := getClientIP(req)

	// Should trim spaces
	if ip != "192.168.1.100" {
		t.Errorf("getClientIP: got %q, want %q", ip, "192.168.1.100")
	}
}

func TestGetClientIP_NoForwardedHeader_WithPort(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.100:54321"

	ip := getClientIP(req)

	// Should extract IP from RemoteAddr
	if ip != "192.168.1.100" {
		t.Errorf("getClientIP: got %q, want %q", ip, "192.168.1.100")
	}
}

func TestGetClientIP_NoForwardedHeader_IPv6WithPort(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	req.RemoteAddr = "[::1]:54321"

	ip := getClientIP(req)

	// Should extract IPv6 IP from RemoteAddr
	if ip != "::1" {
		t.Errorf("getClientIP: got %q, want %q", ip, "::1")
	}
}

func TestGetClientIP_NoForwardedHeader_JustIP(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.100" // No port (unusual but possible)

	ip := getClientIP(req)

	// Should return as-is when no port
	if ip != "192.168.1.100" {
		t.Errorf("getClientIP: got %q, want %q", ip, "192.168.1.100")
	}
}

func TestGetClientIP_Localhost(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"

	ip := getClientIP(req)

	if ip != "127.0.0.1" {
		t.Errorf("getClientIP: got %q, want %q", ip, "127.0.0.1")
	}
}

func TestGetClientIP_LocalhostIPv6(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	req.RemoteAddr = "[::1]:12345"

	ip := getClientIP(req)

	if ip != "::1" {
		t.Errorf("getClientIP: got %q, want %q", ip, "::1")
	}
}

func TestGetClientIP_XForwardedFor_Empty(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "")
	req.RemoteAddr = "10.0.0.5:9999"

	ip := getClientIP(req)

	// Empty X-Forwarded-For should fall back to RemoteAddr
	if ip != "10.0.0.5" {
		t.Errorf("getClientIP: got %q, want %q", ip, "10.0.0.5")
	}
}

// =============================================================================
// Table-Driven Tests
// =============================================================================

func TestGetClientIP_TableDriven(t *testing.T) {
	tests := []struct {
		name          string
		xForwardedFor string
		remoteAddr    string
		expectedIP    string
	}{
		{
			name:          "AWS ALB style",
			xForwardedFor: "203.0.113.50",
			remoteAddr:    "10.0.0.1:12345",
			expectedIP:    "203.0.113.50",
		},
		{
			name:          "CloudFlare style (multiple proxies)",
			xForwardedFor: "203.0.113.50, 198.41.215.162",
			remoteAddr:    "10.0.0.1:12345",
			expectedIP:    "203.0.113.50",
		},
		{
			name:          "Nginx proxy",
			xForwardedFor: "172.16.0.100",
			remoteAddr:    "127.0.0.1:54321",
			expectedIP:    "172.16.0.100",
		},
		{
			name:          "Direct connection",
			xForwardedFor: "",
			remoteAddr:    "203.0.113.50:54321",
			expectedIP:    "203.0.113.50",
		},
		{
			name:          "Internal load balancer",
			xForwardedFor: "",
			remoteAddr:    "127.0.0.1:12345",
			expectedIP:    "127.0.0.1",
		},
		{
			name:          "IPv6 direct",
			xForwardedFor: "",
			remoteAddr:    "[2001:db8::1]:54321",
			expectedIP:    "2001:db8::1",
		},
		{
			name:          "IPv6 forwarded",
			xForwardedFor: "2001:db8::50",
			remoteAddr:    "[::1]:12345",
			expectedIP:    "2001:db8::50",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/", nil)
			if tt.xForwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.xForwardedFor)
			}
			req.RemoteAddr = tt.remoteAddr

			ip := getClientIP(req)

			if ip != tt.expectedIP {
				t.Errorf("getClientIP: got %q, want %q", ip, tt.expectedIP)
			}
		})
	}
}

// =============================================================================
// Benchmark Tests
// =============================================================================

func BenchmarkGetClientIP_WithForwarded(b *testing.B) {
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.50, 198.51.100.178")
	req.RemoteAddr = "10.0.0.1:12345"

	for b.Loop() {
		_ = getClientIP(req)
	}
}

func BenchmarkGetClientIP_Direct(b *testing.B) {
	req, _ := http.NewRequest("GET", "/", nil)
	req.RemoteAddr = "203.0.113.50:54321"

	for b.Loop() {
		_ = getClientIP(req)
	}
}
