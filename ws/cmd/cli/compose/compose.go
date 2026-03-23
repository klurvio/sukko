// Package compose provides Docker Compose integration for local development.
package compose

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const defaultLogTailLines = "100"

// Manager wraps Docker Compose CLI operations.
type Manager struct {
	projectDir string
}

// NewManager creates a Manager for the given project directory (where docker-compose.yml lives).
// Returns an error if projectDir is empty.
func NewManager(projectDir string) (*Manager, error) {
	if projectDir == "" {
		return nil, errors.New("project directory must not be empty")
	}
	return &Manager{projectDir: projectDir}, nil
}

// ServiceStatus represents the status of a Docker Compose service.
type ServiceStatus struct {
	Name    string `json:"Name"`
	State   string `json:"State"`
	Health  string `json:"Health"`
	Service string `json:"Service"`
	Status  string `json:"Status"`
}

// Up starts services with the given profiles and environment overrides.
func (m *Manager) Up(ctx context.Context, profiles []string, envOverrides map[string]string) error {
	args := make([]string, 0, 1+2*len(profiles)+3)
	args = append(args, "compose")
	for _, p := range profiles {
		args = append(args, "--profile", p)
	}
	args = append(args, "up", "-d", "--build")

	cmd := exec.CommandContext(ctx, "docker", args...) //nolint:gosec // G204: args built from fixed strings and validated profile names
	cmd.Dir = m.projectDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	env := os.Environ()
	for k, v := range envOverrides {
		env = append(env, k+"="+v)
	}
	cmd.Env = env

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose up: %w", err)
	}
	return nil
}

// Down stops and removes services.
func (m *Manager) Down(ctx context.Context, removeVolumes bool) error {
	args := []string{"compose", "down"}
	if removeVolumes {
		args = append(args, "-v")
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = m.projectDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose down: %w", err)
	}
	return nil
}

// Status returns the status of all services.
func (m *Manager) Status(ctx context.Context) ([]ServiceStatus, error) {
	cmd := exec.CommandContext(ctx, "docker", "compose", "ps", "--format", "json")
	cmd.Dir = m.projectDir

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker compose ps: %w", err)
	}

	if strings.TrimSpace(string(out)) == "" {
		return nil, nil
	}

	// docker compose ps --format json outputs one JSON object per line
	var services []ServiceStatus
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var svc ServiceStatus
		if err := json.Unmarshal([]byte(line), &svc); err != nil {
			// docker compose may emit non-JSON preamble or progress lines; silently
			// skipping them is safe because valid service entries are always JSON objects.
			continue
		}
		services = append(services, svc)
	}

	return services, nil
}

// Logs streams logs from the specified services. If services is empty, tails all.
func (m *Manager) Logs(ctx context.Context, services []string, follow bool) error {
	args := []string{"compose", "logs"}
	if follow {
		args = append(args, "-f")
	}
	args = append(args, "--tail", defaultLogTailLines)
	args = append(args, services...)

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = m.projectDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose logs: %w", err)
	}
	return nil
}

// IsRunning returns true if the Docker Compose project has running services.
func (m *Manager) IsRunning(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "docker", "compose", "ps", "-q")
	cmd.Dir = m.projectDir

	// err ignored: docker compose ps failure (daemon unreachable, not installed) is treated as "not running"
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}
