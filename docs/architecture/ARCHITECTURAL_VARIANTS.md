# Architectural Variants Strategy: Single-Core vs Multi-Core

## The Problem

**Current Design**: ws/ is architected for **single-core** operation
- `GOMAXPROCS=1` (enforced by automaxprocs)
- 12K connections @ 1 CPU core
- No lock contention (single-threaded simplicity)
- ~30% CPU at capacity

**Future Need**: **Multi-core** architecture from scratch
- `GOMAXPROCS=N` (use all available cores)
- Higher connection capacity (50K+ connections?)
- Concurrent worker pools with synchronization
- Completely different resource management

**Key Insight**: This is NOT version evolution (v1→v2), this is **architectural variants** (Design A vs Design B) that may coexist long-term.

---

## Version Evolution vs Architectural Variants

### Version Evolution (My Previous Assessment)
```
v1.0.0 → v1.1.0 → v2.0.0
   ↓        ↓         ↓
 Fixes  Features  Breaking Changes
```
**Characteristics:**
- Linear progression
- v2 replaces v1 eventually
- Incremental improvements
- **Solution**: Git branches/tags

### Architectural Variants (Your Scenario)
```
Single-Core Design    ←→    Multi-Core Design
     (Design A)                 (Design B)
        ↓                           ↓
   Optimized for             Optimized for
   simplicity                parallelism
        ↓                           ↓
   May coexist long-term
   (different use cases)
```
**Characteristics:**
- Parallel development
- Both remain valid indefinitely
- Different architectural philosophies
- Different trade-offs
- **Solution**: Directory structure or cmd/ pattern

---

## Why This Changes Everything

### Use Cases for Parallel Architectures

**1. Experimentation & Research**
```bash
# Test both approaches side-by-side
./ws-single --config single.env
./ws-multi --config multi.env
# Compare metrics, CPU usage, connection handling
```

**2. Benchmarking & Validation**
```bash
# Prove multi-core is actually better
task bench:single  # 12K connections, 1 core
task bench:multi   # 50K connections, 4 cores
# Document results before full migration
```

**3. Gradual Migration**
```bash
# Run both in production during transition
GCP Instance 1: ws-single (proven stable)
GCP Instance 2: ws-multi (testing new approach)
# Shift traffic gradually
```

**4. Specialization by Deployment**
```bash
# Small deployments
aws-small: ws-single (1 vCPU, simpler, cheaper)

# Large deployments  
aws-large: ws-multi (8 vCPU, maximum capacity)
```

**5. Long-Term Coexistence**
```bash
# Both architectures remain valid
Edge servers:     ws-single (low latency, simple)
Regional hubs:    ws-multi (high throughput)
```

---

## Solution Options

### Option 1: Go Standard `cmd/` Pattern ⭐ RECOMMENDED

**Structure:**
```
odin-ws/
├── cmd/
│   ├── ws-single/              # Single-core binary
│   │   ├── main.go
│   │   └── Dockerfile
│   └── ws-multi/               # Multi-core binary
│       ├── main.go
│       └── Dockerfile
│
├── internal/                   # Private packages
│   ├── single/                 # Single-core specific
│   │   ├── server.go
│   │   ├── worker_pool.go
│   │   └── resource_guard.go
│   ├── multi/                  # Multi-core specific
│   │   ├── server.go
│   │   ├── worker_pool_parallel.go
│   │   └── resource_guard_concurrent.go
│   └── shared/                 # Common code
│       ├── config.go
│       ├── connection.go
│       └── message.go
│
├── pkg/                        # Public packages
│   ├── metrics/                # Shared Prometheus metrics
│   ├── logger/                 # Shared logging
│   └── kafka/                  # Shared Kafka consumer
│
├── go.mod                      # Single Go module
├── go.sum
└── VERSION
```

**Build Commands:**
```bash
# Build single-core
go build -o bin/ws-single ./cmd/ws-single

# Build multi-core
go build -o bin/ws-multi ./cmd/ws-multi

# Or both
go build -o bin/ ./cmd/...
```

**Docker:**
```dockerfile
# cmd/ws-single/Dockerfile
FROM golang:1.25.1-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /ws-single ./cmd/ws-single

FROM alpine:latest
COPY --from=builder /ws-single .
CMD ["./ws-single"]
```

**Pros:**
- ✅ Standard Go project layout
- ✅ Single `go.mod` (easy dependency management)
- ✅ Shared code in `internal/shared` and `pkg/`
- ✅ Clear separation of concerns
- ✅ Both binaries built from same repo
- ✅ Easy to share bug fixes
- ✅ Standard Go tooling works

**Cons:**
- ⚠️ Larger codebase (both architectures in one repo)
- ⚠️ Need discipline to avoid coupling

**When to Use:**
- Architectures share significant code (>30%)
- Active development on both
- Want to share bug fixes easily
- Team works on both variants

---

### Option 2: Separate Directories (Independent Projects)

**Structure:**
```
odin-ws/
├── ws-single/                  # Completely independent
│   ├── go.mod
│   ├── *.go
│   ├── Dockerfile
│   └── VERSION
│
├── ws-multi/                   # Completely independent
│   ├── go.mod
│   ├── *.go
│   ├── Dockerfile
│   └── VERSION
│
└── ws -> ws-single             # Symlink to current
```

**Go Modules:**
```go
// ws-single/go.mod
module github.com/Toniq-Labs/odin-ws/ws-single

// ws-multi/go.mod
module github.com/Toniq-Labs/odin-ws/ws-multi
```

**Build Commands:**
```bash
# Build single-core
cd ws-single && go build -o ../bin/ws-single .

# Build multi-core
cd ws-multi && go build -o ../bin/ws-multi .
```

**Pros:**
- ✅ Complete independence
- ✅ No coupling between architectures
- ✅ Can use different dependencies/versions
- ✅ Clear ownership boundaries
- ✅ Easy to delete one later

**Cons:**
- ❌ Code duplication (metrics, config, etc.)
- ❌ Bug fixes need manual porting
- ❌ Two go.mod files to maintain
- ❌ More disk space

**When to Use:**
- Architectures share minimal code (<20%)
- Different teams own each
- Want complete independence
- One will be deprecated eventually

---

### Option 3: Separate Repositories

**Structure:**
```
ws-single/                      # Separate repo
├── go.mod
├── *.go
└── Dockerfile

ws-multi/                       # Separate repo
├── go.mod
├── *.go
└── Dockerfile
```

**Pros:**
- ✅ Complete isolation
- ✅ Independent release cycles
- ✅ Different git histories
- ✅ Can be owned by different teams

**Cons:**
- ❌ Duplicate deployment configs
- ❌ Duplicate publisher, scripts, etc.
- ❌ Hard to share infrastructure
- ❌ More repositories to manage

**When to Use:**
- Completely different projects
- Different teams/organizations
- Different lifecycles (one OSS, one proprietary?)
- Never need to compare/benchmark side-by-side

---

### Option 4: Long-Lived Git Branch (Feature Branch)

**Structure:**
```bash
main branch:              ws/ (single-core)
multicore-rewrite branch: ws/ (multi-core)
```

**Workflow:**
```bash
# Work on single-core
git checkout main
cd ws && go run main.go

# Work on multi-core
git checkout multicore-rewrite
cd ws && go run main.go
```

**Pros:**
- ✅ Simple (no structure changes)
- ✅ Git-native approach
- ✅ Easy to switch between

**Cons:**
- ❌ Can't run both simultaneously on same machine
- ❌ Can't easily compare/benchmark
- ❌ Hard to share bug fixes (cherry-pick hell)
- ❌ Confusion about which branch is "production"

**When to Use:**
- Short-term experiment only
- Multi-core will replace single-core quickly
- Not concerned about benchmarking side-by-side

---

## Recommended Approach: Option 1 (cmd/ Pattern)

### Why cmd/ is Best for Your Use Case

1. **Benchmarking**: Run both binaries side-by-side
   ```bash
   ./bin/ws-single &
   ./bin/ws-multi &
   # Compare Grafana dashboards
   ```

2. **Shared Infrastructure**: Same deployment configs, Kafka setup, monitoring
   ```
   deployments/v1/local/docker-compose.yml:
     ws-single:
       build: ./cmd/ws-single
     ws-multi:
       build: ./cmd/ws-multi
   ```

3. **Code Reuse**: Metrics, logging, Kafka consumers shared
   ```go
   // internal/shared/metrics.go
   package shared
   // Used by both single and multi
   ```

4. **Gradual Development**: Start by moving current code to `cmd/ws-single`, then build `cmd/ws-multi` from scratch
   ```bash
   # Phase 1: Reorganize current
   mkdir -p cmd/ws-single internal/single
   mv ws/*.go cmd/ws-single/  # or internal/single/
   
   # Phase 2: Build multi-core
   mkdir -p cmd/ws-multi internal/multi
   # Start fresh implementation
   ```

5. **Standard Practice**: Many Go projects follow this pattern
   - Kubernetes: `cmd/kube-apiserver`, `cmd/kube-controller-manager`
   - Prometheus: `cmd/prometheus`, `cmd/promtool`
   - Docker: `cmd/dockerd`, `cmd/docker-cli`

---

## Implementation Plan: cmd/ Pattern

### Phase 1: Analyze Current Codebase

**Identify Shared vs Specific:**
```bash
# Likely SHARED (use in both):
- config.go           # Configuration loading
- connection.go       # WebSocket connection handling
- message.go          # Message types
- logger.go           # Logging setup
- metrics.go          # Prometheus metrics (mostly)
- kafka/              # Kafka consumer setup

# Likely SINGLE-CORE SPECIFIC:
- worker_pool.go      # Single-threaded pool
- resource_guard.go   # Single-core guards (no mutex)
- server.go           # Server with single-core assumptions

# Likely MULTI-CORE WILL NEED:
- worker_pool_parallel.go    # Concurrent pool with channels
- resource_guard_concurrent.go  # Mutex-protected guards
- server_parallel.go          # Multi-threaded server
```

### Phase 2: Create Directory Structure

```bash
cd /Volumes/Dev/Codev/Toniq/odin-ws

# Create new structure
mkdir -p cmd/ws-single cmd/ws-multi
mkdir -p internal/single internal/multi internal/shared
mkdir -p pkg/metrics pkg/logger pkg/kafka

# Move current implementation
# (Keep original ws/ for now as backup)
```

### Phase 3: Refactor Single-Core into cmd/

**Step 1: Create main.go**
```go
// cmd/ws-single/main.go
package main

import (
    "github.com/Toniq-Labs/odin-ws/internal/single"
    "github.com/Toniq-Labs/odin-ws/pkg/logger"
)

func main() {
    logger.Init()
    server := single.NewServer()
    server.Run()
}
```

**Step 2: Move architecture-specific code**
```bash
# Single-core specific
mv ws/server.go internal/single/
mv ws/worker_pool.go internal/single/
mv ws/resource_guard.go internal/single/

# Shared code
mv ws/config.go internal/shared/
mv ws/connection.go internal/shared/
mv ws/message.go internal/shared/
mv ws/logger.go pkg/logger/
mv ws/metrics.go pkg/metrics/
mv ws/kafka/ pkg/kafka/
```

**Step 3: Update imports**
```go
// internal/single/server.go
package single

import (
    "github.com/Toniq-Labs/odin-ws/internal/shared"
    "github.com/Toniq-Labs/odin-ws/pkg/metrics"
    "github.com/Toniq-Labs/odin-ws/pkg/kafka"
)
```

**Step 4: Create Dockerfile**
```dockerfile
# cmd/ws-single/Dockerfile
FROM golang:1.25.1-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /ws-single ./cmd/ws-single

FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=builder /ws-single .
EXPOSE 3002
CMD ["./ws-single"]
```

### Phase 4: Test Single-Core Refactor

```bash
# Build
go build -o bin/ws-single ./cmd/ws-single

# Test
./bin/ws-single

# Docker build
docker build -f cmd/ws-single/Dockerfile -t odin-ws-single:test .
```

### Phase 5: Create Multi-Core (From Scratch)

```bash
# Create structure
mkdir -p internal/multi
```

```go
// cmd/ws-multi/main.go
package main

import (
    "runtime"
    "github.com/Toniq-Labs/odin-ws/internal/multi"
    "github.com/Toniq-Labs/odin-ws/pkg/logger"
)

func main() {
    // Use all cores
    runtime.GOMAXPROCS(runtime.NumCPU())
    
    logger.Init()
    server := multi.NewServer()
    server.Run()
}
```

```go
// internal/multi/server.go
package multi

import (
    "sync"
    "github.com/Toniq-Labs/odin-ws/internal/shared"
    "github.com/Toniq-Labs/odin-ws/pkg/metrics"
)

type Server struct {
    mu sync.RWMutex  // Multi-core needs synchronization
    connections map[string]*shared.Connection
    workerPools []*WorkerPool  // Multiple pools
    // ... multi-core specific fields
}

func NewServer() *Server {
    numWorkerPools := runtime.NumCPU()
    // ... create multiple worker pools
}
```

```go
// internal/multi/worker_pool_parallel.go
package multi

import (
    "sync"
    "context"
)

type WorkerPool struct {
    workers   int
    jobs      chan Job
    wg        sync.WaitGroup
    mu        sync.Mutex  // Protect shared state
    // ... parallel worker pool implementation
}
```

### Phase 6: Update Deployments

**docker-compose.yml:**
```yaml
services:
  # Single-core variant
  ws-single:
    build:
      context: ../../
      dockerfile: cmd/ws-single/Dockerfile
    container_name: odin-ws-single
    ports:
      - "3005:3002"
    env_file:
      - ../shared/base.env
      - ./overrides.env
      - .env.local
    deploy:
      resources:
        limits:
          cpus: "1.0"
          memory: 4G

  # Multi-core variant
  ws-multi:
    build:
      context: ../../
      dockerfile: cmd/ws-multi/Dockerfile
    container_name: odin-ws-multi
    ports:
      - "3006:3002"
    env_file:
      - ../shared/base.env
      - ./overrides-multi.env
      - .env.local
    deploy:
      resources:
        limits:
          cpus: "4.0"
          memory: 8G
```

### Phase 7: Update VERSION Files

```yaml
# VERSION (root)
components:
  ws-single: v1.0.0
  ws-multi: v0.1.0  # Experimental
  publisher: v2.0.0

architectures:
  single-core:
    status: production
    max_connections: 12000
    cpu_cores: 1
  multi-core:
    status: experimental
    max_connections: 50000
    cpu_cores: 4
```

```yaml
# cmd/ws-single/VERSION
architecture: single-core
version: v1.0.0
optimized_for: simplicity
max_connections: 12000
cpu_cores: 1

# cmd/ws-multi/VERSION
architecture: multi-core
version: v0.1.0
optimized_for: parallelism
max_connections: 50000
cpu_cores: 4
```

---

## Deployment Strategies

### Strategy 1: A/B Testing

```yaml
# GCP Instance 1
services:
  ws-single:
    image: odin-ws-single:v1.0.0

# GCP Instance 2  
services:
  ws-multi:
    image: odin-ws-multi:v0.1.0

# Load balancer splits traffic 50/50
# Compare metrics in Grafana
```

### Strategy 2: Progressive Rollout

```
Week 1: 100% single, 0% multi (baseline)
Week 2:  90% single, 10% multi (canary)
Week 3:  70% single, 30% multi (expanded)
Week 4:  50% single, 50% multi (equal)
Week 5:  20% single, 80% multi (majority)
Week 6:   0% single, 100% multi (full migration)
```

### Strategy 3: Specialization

```
Small instances (t3.medium):  ws-single
Large instances (c5.4xlarge): ws-multi

Edge locations: ws-single (lower latency)
Data centers:   ws-multi (higher throughput)
```

---

## Taskfile Commands

```yaml
# taskfiles/version.yml additions

build:single:
  desc: Build single-core binary
  cmds:
    - go build -o bin/ws-single ./cmd/ws-single

build:multi:
  desc: Build multi-core binary
  cmds:
    - go build -o bin/ws-multi ./cmd/ws-multi

build:both:
  desc: Build both architectures
  cmds:
    - task: build:single
    - task: build:multi

test:single:
  desc: Test single-core architecture
  cmds:
    - cd cmd/ws-single && go test ./...

test:multi:
  desc: Test multi-core architecture
  cmds:
    - cd cmd/ws-multi && go test ./...

bench:compare:
  desc: Benchmark both architectures
  cmds:
    - echo "Starting benchmarks..."
    - task: bench:single
    - task: bench:multi
    - task: bench:report

images:build:single:
  desc: Build single-core Docker image
  cmds:
    - docker build -f cmd/ws-single/Dockerfile -t odin-ws-single:{{.VERSION}} .

images:build:multi:
  desc: Build multi-core Docker image
  cmds:
    - docker build -f cmd/ws-multi/Dockerfile -t odin-ws-multi:{{.VERSION}} .
```

---

## Summary & Recommendation

### Your Scenario Requires Directory Structure

**You're right** - for completely different architectural designs that will coexist:
- ✅ **DO** use directory structure (cmd/ pattern)
- ❌ **DON'T** rely solely on git branches

### Recommended Structure

```
odin-ws/
├── cmd/
│   ├── ws-single/     # Current: Single-core (production)
│   └── ws-multi/      # Future: Multi-core (experimental)
├── internal/
│   ├── single/        # Single-core specific code
│   ├── multi/         # Multi-core specific code
│   └── shared/        # Shared between both
├── pkg/               # Public shared libraries
└── go.mod             # Single module
```

### Benefits for Your Use Case

1. **Develop multi-core from scratch** without touching single-core
2. **Run both simultaneously** for benchmarking
3. **Share common code** (metrics, config, Kafka)
4. **Independent evolution** of each architecture
5. **Easy comparison** and A/B testing
6. **Standard Go practice** (cmd/ pattern)

### Next Steps

1. **Decide**: Confirm cmd/ pattern is right for your needs
2. **Refactor**: Move current ws/ to cmd/ws-single
3. **Build**: Create cmd/ws-multi from scratch
4. **Test**: Benchmark both side-by-side
5. **Deploy**: Progressive rollout or specialization strategy

This is **architectural variants**, not version evolution - and yes, directory structure absolutely makes sense here.
