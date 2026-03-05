# TCP Tuning Implementation Plan

**Branch**: `fix/tcp-backlog-tuning`
**Purpose**: Fix 18% handshake failure rate during connection bursts
**Reversibility**: All changes configurable via environment variables

---

## Problem Statement

During capacity testing (100 conn/sec ramp):
- 12,000 connections attempted
- 2,220 failed (18.5% failure rate)
- Error: `websocketproxy: couldn't dial to remote backend url websocket: bad handshake`
- Root cause: **TCP listen backlog overflow** (not CPU threshold)

---

## Solution Overview

### 1. Configuration Added ✅
```go
// ws/internal/shared/platform/config.go
TCPListenBacklog  int           // Default: 2048 (was ~128)
HTTPReadTimeout   time.Duration // Default: 15s (was 5s)
HTTPWriteTimeout  time.Duration // Default: 15s (was 5s)
HTTPIdleTimeout   time.Duration // Default: 60s (was 10s)
```

### 2. Files to Modify

#### A. Update Config Print (ws/internal/shared/platform/config.go)
```go
// Around line 174, add after CPU Pause:
fmt.Printf("\n=== TCP/Network Tuning ===\n")
fmt.Printf("Listen Backlog:  %d\n", c.TCPListenBacklog)
fmt.Printf("Read Timeout:    %s\n", c.HTTPReadTimeout)
fmt.Printf("Write Timeout:   %s\n", c.HTTPWriteTimeout)
fmt.Printf("Idle Timeout:    %s\n", c.HTTPIdleTimeout)
```

#### B. Implement TCP Backlog in Shared Server (ws/internal/shared/server.go)
Replace `ListenAndServe()` with custom listener:

```go
// Add import: "syscall"

// Around line 350, replace:
// return s.server.ListenAndServe()

// With:
func (s *Server) listenWithCustomBacklog() error {
	ln, err := net.Listen("tcp", s.config.Addr)
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}

	// Set custom TCP backlog if configured
	if s.config.TCPListenBacklog > 0 {
		if tcpLn, ok := ln.(*net.TCPListener); ok {
			file, err := tcpLn.File()
			if err == nil {
				// syscall.Listen sets the backlog size
				syscall.Listen(int(file.Fd()), s.config.TCPListenBacklog)
				file.Close()

				s.logger.Info().
					Int("backlog", s.config.TCPListenBacklog).
					Msg("Set custom TCP listen backlog")
			}
		}
	}

	s.logger.Info().
		Str("address", s.config.Addr).
		Msg("Server listening")

	return s.server.Serve(ln)
}
```

#### C. Apply Timeouts to Shared Server (ws/internal/shared/server.go)
```go
// Around line 230, update server creation:
s.server = &http.Server{
	Addr:    s.config.Addr,
	Handler: mux,
	ReadTimeout:    s.config.HTTPReadTimeout,   // Use config
	WriteTimeout:   s.config.HTTPWriteTimeout,  // Use config
	IdleTimeout:    s.config.HTTPIdleTimeout,   // Use config
	MaxHeaderBytes: 1 << 20,
}
```

#### D. Apply Timeouts to Load Balancer (ws/internal/multi/loadbalancer.go)
```go
// Around line 87-95, update:
server := &http.Server{
	Addr:    lb.addr,
	Handler: mux,
	ReadTimeout:    cfg.HTTPReadTimeout,   // From config
	WriteTimeout:   cfg.HTTPWriteTimeout,  // From config
	IdleTimeout:    cfg.HTTPIdleTimeout,   // From config
	MaxHeaderBytes: 1 << 20,
}
```

#### E. Pass Config to LoadBalancer (ws/cmd/multi/main.go)
```go
// Around line 185, update LoadBalancerConfig:
lbConfig := multi.LoadBalancerConfig{
	Addr:            lbAddr,
	Shards:          shards,
	Logger:          logger,
	HTTPReadTimeout:  cfg.HTTPReadTimeout,   // Add
	HTTPWriteTimeout: cfg.HTTPWriteTimeout,  // Add
	HTTPIdleTimeout:  cfg.HTTPIdleTimeout,   // Add
}
```

#### F. Update LoadBalancerConfig struct (ws/internal/multi/loadbalancer.go)
```go
// Around line 32-37:
type LoadBalancerConfig struct {
	Addr   string
	Shards []*Shard
	Logger zerolog.Logger

	// TCP/Network tuning
	HTTPReadTimeout  time.Duration
	HTTPWriteTimeout time.Duration
	HTTPIdleTimeout  time.Duration
}
```

### 3. OS-Level Tuning (Deployment)

Add to deployment script (`taskfiles/v1/gcp/deployment.yml`):

```bash
# After WS server setup, add tuning step:
echo "🔧 Tuning TCP stack for burst tolerance..."
gcloud compute ssh sukko-go \\
  --zone=us-central1-a \\
  --project=sukko-server \\
  --command="sudo bash -c '
    # Increase TCP connection backlog
    sysctl -w net.core.somaxconn=4096

    # Increase TCP buffers
    sysctl -w net.core.rmem_max=16777216
    sysctl -w net.core.wmem_max=16777216

    # Make persistent
    echo \"net.core.somaxconn=4096\" >> /etc/sysctl.conf
    sysctl -p

    echo \"✅ TCP stack tuned\"
  '"
```

---

## Environment Variables for Deployment

Add to `deployments/v1/shared/base.env`:

```bash
# =============================================================================
# TCP/NETWORK TUNING (Burst Tolerance)
# =============================================================================
# Improves tolerance to connection bursts by increasing buffers and timeouts
# Set TCP_LISTEN_BACKLOG=0 to revert to Go defaults (~128)
TCP_LISTEN_BACKLOG=2048
HTTP_READ_TIMEOUT=15s
HTTP_WRITE_TIMEOUT=15s
HTTP_IDLE_TIMEOUT=60s
```

---

## Rollback Plan

### Quick Revert (Without Redeployment)
Set in base.env and redeploy:
```bash
TCP_LISTEN_BACKLOG=0      # Disable custom backlog
HTTP_READ_TIMEOUT=5s      # Back to original
HTTP_WRITE_TIMEOUT=5s     # Back to original
HTTP_IDLE_TIMEOUT=10s     # Back to original
```

### Full Revert (Git)
```bash
git checkout new-arch        # Switch back to main branch
git branch -D fix/tcp-backlog-tuning  # Delete feature branch
```

### OS Revert
```bash
sudo sysctl -w net.core.somaxconn=128  # Back to default
```

---

## Testing Plan

### Phase 1: OS Tuning Only
1. Apply OS-level tuning to existing deployment
2. Run capacity test
3. Check if failure rate improves

### Phase 2: Code Changes
1. Deploy code with new defaults
2. Run capacity test
3. Compare failure rates

### Phase 3: Gradual Tuning
If performance degrades:
1. Reduce backlog from 2048 → 1024 → 512
2. Reduce timeouts from 15s → 10s → 5s
3. Find optimal balance

---

## Success Metrics

- **Before**: 81.5% success rate (2,220 failures)
- **Target**: >95% success rate (<600 failures)
- **Ideal**: >99% success rate (<120 failures)

Monitor:
- Handshake failure count
- CPU usage (should not increase significantly)
- Memory usage (small increase from larger buffers expected)
- Connection latency

---

## Implementation Status

- [x] Add environment variables to config
- [ ] Update config print function
- [ ] Implement TCP backlog in server
- [ ] Apply timeouts to shared server
- [ ] Apply timeouts to load balancer
- [ ] Update deployment script
- [ ] Add env vars to base.env
- [ ] Test on GCP
- [ ] Document results

---

## Next Steps

1. Complete code implementation (5 files to modify)
2. Commit changes with clear message
3. Deploy to GCP
4. Run capacity test
5. Compare results
6. Document findings

