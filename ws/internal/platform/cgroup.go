package platform

import (
	"os"
	"strconv"
	"strings"
)

// GetMemoryLimit returns the container memory limit in bytes from cgroup filesystem.
//
// Purpose:
//
//	Automatically detect memory constraints in containerized environments
//	(Docker, Kubernetes, Cloud Run, ECS, etc.) to calculate safe connection limits.
//
// Supports:
//   - cgroup v2 (modern systems, Cloud Run, newer Kubernetes)
//   - cgroup v1 (legacy systems, older Docker versions)
//
// Return values:
//   - success: Returns memory limit in bytes
//   - no limit: Returns 0 (unlimited or non-containerized environment)
//   - error: Returns 0 with error (file not found, parse error)
//
// Implementation:
//
//	Tries cgroup v2 first (/sys/fs/cgroup/memory.max)
//	Falls back to cgroup v1 (/sys/fs/cgroup/memory/memory.limit_in_bytes)
//
// Example output:
//   - 512MB container: Returns 536870912 (512 * 1024 * 1024)
//   - Unlimited: Returns 0
//   - Non-containerized: Returns 0 with error
func GetMemoryLimit() (int64, error) {
	// Try cgroup v2 first (newer systems, Cloud Run)
	// Path: /sys/fs/cgroup/memory.max
	// Format: "536870912" or "max" (unlimited)
	if data, err := os.ReadFile("/sys/fs/cgroup/memory.max"); err == nil {
		limitStr := strings.TrimSpace(string(data))
		if limitStr != "max" {
			return strconv.ParseInt(limitStr, 10, 64)
		}
	}

	// Fallback to cgroup v1 (legacy systems)
	// Path: /sys/fs/cgroup/memory/memory.limit_in_bytes
	// Format: "536870912" (always a number, never "max")
	if data, err := os.ReadFile("/sys/fs/cgroup/memory/memory.limit_in_bytes"); err == nil {
		limitStr := strings.TrimSpace(string(data))
		return strconv.ParseInt(limitStr, 10, 64)
	}

	// If no cgroup limits found, return 0 (no limit detected)
	// This happens on:
	//   - Non-containerized systems (bare metal, VMs)
	//   - macOS/Windows development environments
	//   - Containers without memory limits
	return 0, nil
}
