# GCP Commands Hierarchy

> Complete reference for all GCP task commands in `taskfiles/v1/gcp/`

## Table of Contents
- [Overview](#overview)
- [File Structure](#file-structure)
- [High-Level Commands](#high-level-commands)
- [Deployment Commands](#deployment-commands)
- [Service Management](#service-management)
- [Health Checks](#health-checks)
- [Load Testing](#load-testing)
- [Stats & Info](#stats--info)
- [Utilities (Internal)](#utilities-internal)
- [Command Relationships](#command-relationships)

---

## Overview

The GCP task system is organized into **6 main files** plus a central orchestrator:

| File | Purpose | Namespace |
|------|---------|-----------|
| `Taskfile.yml` | Main orchestrator, high-level shortcuts | `gcp:` |
| `deployment.yml` | Infrastructure & deployment workflows | `gcp:deployment:` |
| `services.yml` | Service control (start/stop/restart) | `gcp:services:` |
| `health.yml` | Health checks for all services | `gcp:health:` |
| `load-test.yml` | Load testing commands | `gcp:load-test:` |
| `stats.yml` | Stats, IPs, URLs | `gcp:stats:` |
| `utilities.yml` | Internal helper functions | `gcp:utilities:` |

---

## File Structure

```
taskfiles/v1/gcp/
├── Taskfile.yml          # Main orchestrator (includes all others)
├── deployment.yml        # Infra setup, instance management
├── services.yml          # Service lifecycle (start/stop/restart)
├── health.yml            # Health checks
├── load-test.yml         # Load testing
├── stats.yml             # Info & URLs
└── utilities.yml         # Internal helpers
```

---

## High-Level Commands

> **Namespace:** `gcp:`  
> **File:** `Taskfile.yml`  
> **Purpose:** Quick shortcuts for common workflows

### Daily Use Commands

```bash
# Quick start/stop
task gcp:up              # Start all services (assumes infra exists)
task gcp:down            # Stop all services (keeps infra)
task gcp:restart         # Restart all services + health check

# First-time setup
task gcp:deploy          # Complete deployment from scratch
                         # → Creates infra + deploys code + health check

# Status & info
task gcp:status          # Show deployment status + URLs + containers
task gcp:verify          # Run health checks
task gcp:urls            # Show all service URLs

# Destructive
task gcp:delete          # Delete ALL instances (saves maximum costs)
```

### Command Flow

```
gcp:deploy
  └─> deployment:setup
        ├─> deployment:infrastructure
        │     ├─> create:backend
        │     ├─> create:ws
        │     ├─> create:loadtest
        │     ├─> firewall:backend
        │     ├─> firewall:ws
        │     └─> reserve-ip:ws
        ├─> setup:backend
        ├─> setup:ws
        └─> setup:loadtest

gcp:restart
  └─> services:restart:all
        ├─> restart:backend
        └─> restart:ws
  └─> health:all
```

---

## Deployment Commands

> **Namespace:** `gcp:deployment:`  
> **File:** `deployment.yml`  
> **Purpose:** Infrastructure provisioning and application deployment

### Workflows

```bash
# Complete workflows
task gcp:deployment:setup              # Complete first-time setup
task gcp:deployment:quick-start        # Start services (daily use)
task gcp:deployment:stop               # Stop all services
task gcp:deployment:reset              # Stop + rebuild + start

# Code updates
task gcp:deployment:rebuild-code       # Rebuild all after code changes
task gcp:deployment:update:all         # Pull latest code on all instances
```

### Infrastructure Management

```bash
# Instance creation
task gcp:deployment:create:backend     # Create backend instance (e2-medium)
task gcp:deployment:create:ws          # Create ws-server (e2-standard-4)
task gcp:deployment:create:loadtest    # Create loadtest (e2-standard-8)

# Instance deletion
task gcp:deployment:delete:backend     # Delete backend instance
task gcp:deployment:delete:ws          # Delete ws-server instance
task gcp:deployment:delete:loadtest    # Delete loadtest instance
task gcp:deployment:delete:all         # Delete ALL instances

# Networking
task gcp:deployment:firewall:backend   # Configure backend firewall
task gcp:deployment:firewall:ws        # Configure ws-server firewall
task gcp:deployment:reserve-ip:ws      # Reserve static IP for ws-server
```

### Application Setup

```bash
# Initial setup (runs after instance creation)
task gcp:deployment:setup:backend      # Clone repo + start containers
task gcp:deployment:setup:ws           # Clone repo + start ws-server
task gcp:deployment:setup:loadtest     # Clone repo + build test image
```

### Code Rebuilds

```bash
# Rebuild with latest code
task gcp:deployment:rebuild:backend    # Pull code + rebuild backend
task gcp:deployment:rebuild:ws         # Pull code + rebuild ws-server
task gcp:deployment:rebuild:all        # Rebuild all services

# Update code only (no rebuild)
task gcp:deployment:update:backend     # Git pull on backend
task gcp:deployment:update:ws          # Git pull on ws-server
task gcp:deployment:update:all         # Git pull on all instances
```

### SSH Access

```bash
task gcp:deployment:ssh:backend        # SSH to backend instance
task gcp:deployment:ssh:ws             # SSH to ws-server instance
```

### Instance Details

| Instance | Type | vCPUs | RAM | Disk | Purpose |
|----------|------|-------|-----|------|---------|
| odin-backend | e2-medium | 2 | 4GB | 20GB | Redpanda, Publisher, Grafana, Prometheus |
| odin-ws-go | e2-standard-4 | 4 | 16GB | 20GB | WebSocket server (12K connections) |
| odin-loadtest | e2-standard-8 | 8 | 32GB | 30GB | Load testing client |

---

## Service Management

> **Namespace:** `gcp:services:`  
> **File:** `services.yml`  
> **Purpose:** Control running services (start/stop/restart)

### Start Services

```bash
task gcp:services:start:backend        # Start backend (redpanda, grafana)
task gcp:services:start:ws             # Start ws-server
task gcp:services:start:all            # Start all services
```

### Stop Services

```bash
task gcp:services:stop:backend         # Stop backend services
task gcp:services:stop:ws              # Stop ws-server
task gcp:services:stop:all             # Stop all services
```

### Restart Services

```bash
task gcp:services:restart:backend      # Restart backend
task gcp:services:restart:ws           # Restart ws-server
task gcp:services:restart:all          # Restart all + health check
```

### Container Inspection

```bash
task gcp:services:ps                   # Show all running containers
task gcp:services:ps:backend           # Show backend containers
task gcp:services:ps:ws                # Show ws-server containers
task gcp:services:ps:all               # Show containers on all instances
```

### Logs

```bash
task gcp:services:logs:backend         # Tail backend logs (all containers)
task gcp:services:logs:ws              # Tail ws-server logs
task gcp:services:logs:backend:tail    # Follow backend logs (live)
task gcp:services:logs:ws:tail         # Follow ws-server logs (live)
```

---

## Health Checks

> **Namespace:** `gcp:health:`  
> **File:** `health.yml`  
> **Purpose:** Verify service health and connectivity

```bash
task gcp:health:backend                # Check backend services
                                       # → Publisher, Redpanda, Grafana

task gcp:health:ws                     # Check ws-server health
                                       # → WebSocket endpoint + detailed stats

task gcp:health:all                    # Check all services
```

### Health Check Output

**Backend Health:**
- ✅ Publisher status (running/stopped, current rate)
- ✅ Redpanda cluster health (nodes, partitions)
- ✅ Grafana availability (HTTP 200)

**WS-Server Health:**
- ✅ Connections (current/max)
- ✅ CPU usage (% and threshold)
- ✅ Memory usage (MB and %)
- ✅ Goroutines (current/limit)
- ✅ Kafka connectivity
- ✅ Rate limiting status
- ✅ Dropped broadcasts
- ✅ Buffer saturation
- ✅ Disconnect reasons

---

## Load Testing

> **Namespace:** `gcp:load-test:`  
> **File:** `load-test.yml`  
> **Purpose:** Performance testing and capacity validation

### Predefined Tests

```bash
task gcp:load-test:capacity            # 12K sustained connections (full capacity)
task gcp:load-test:capacity:short      # 12K for 5 minutes (quick test)
task gcp:load-test:stress              # 15K connections (110% load)
task gcp:load-test:stress:heavy        # 20K connections (167% load)
```

### Custom Tests

```bash
task gcp:load-test:custom -- \
  --connections 8000 \
  --duration 600 \
  --rate 50 \
  --workers 32
```

### Test Management

```bash
task gcp:load-test:stop                # Stop all running tests
task gcp:load-test:logs                # View test logs (follow)
```

### Test Parameters

| Test | Connections | Duration | Rate | Workers | Purpose |
|------|-------------|----------|------|---------|---------|
| capacity | 12,000 | 30 min | 25/s | 64 | Full capacity validation |
| capacity:short | 12,000 | 5 min | 25/s | 64 | Quick verification |
| stress | 15,000 | 15 min | 30/s | 80 | Overload test (110%) |
| stress:heavy | 20,000 | 10 min | 40/s | 100 | Heavy stress (167%) |

---

## Stats & Info

> **Namespace:** `gcp:stats:`  
> **File:** `stats.yml`  
> **Purpose:** Display service information and URLs

```bash
task gcp:stats:urls                    # Show all service URLs
                                       # → Grafana, Redpanda Console, Publisher,
                                       #   WebSocket, Health endpoint

task gcp:stats:ip:backend              # Show backend IPs (external + internal)
task gcp:stats:ip:ws                   # Show ws-server IPs (external + internal)

task gcp:stats:info                    # Show deployment info
                                       # → Project, region, zone, instances
                                       # → Service URLs
```

### Service URLs

**Backend (odin-backend):**
- Grafana: `http://BACKEND_IP:3010`
- Redpanda Console: `http://BACKEND_IP:8080`
- Publisher Status: `http://BACKEND_IP:3003/status`

**WebSocket Server (odin-ws-go):**
- WebSocket: `ws://WS_IP:3004/ws`
- Health: `http://WS_IP:3004/health`

---

## Utilities (Internal)

> **Namespace:** `gcp:utilities:`  
> **File:** `utilities.yml`  
> **Purpose:** Internal helper functions (not for direct use)

### Instance Checks

```bash
check-instance-exists                  # Verify instance exists
check-instance-running                 # Verify instance is running
wait-for-instance-ssh                  # Wait for SSH to be ready
wait-for-startup-script                # Wait for startup script completion
```

### Application Checks

```bash
check-repo-cloned                      # Verify git repo is cloned
check-docker-running                   # Verify Docker daemon is running
check-containers-running               # Verify containers are running
check-topics-exist                     # Verify Kafka topics exist
```

### Networking Checks

```bash
check-firewall-exists                  # Verify firewall rule exists
check-static-ip-exists                 # Verify static IP exists
get-static-ip                          # Get static IP address
wait-for-http-endpoint                 # Wait for HTTP endpoint to respond
```

### SSH Helpers

```bash
ssh-exec                               # Execute command via SSH (as root)
ssh-exec-as-deploy                     # Execute command as deploy user
show-deployment-state                  # Show complete deployment state
```

---

## Command Relationships

### Common Workflows

#### 1. First-Time Deployment

```
task gcp:deploy
  │
  ├─ deployment:infrastructure        # Create all instances + networking
  │   ├─ create:backend               # e2-medium instance
  │   ├─ create:ws                    # e2-standard-4 instance  
  │   ├─ create:loadtest              # e2-standard-8 instance
  │   ├─ firewall:backend             # Ports: 3003, 3010, 8080, 9092, 9644
  │   ├─ firewall:ws                  # Port: 3004
  │   └─ reserve-ip:ws                # Static IP for ws-server
  │
  ├─ setup:backend                    # Clone repo + start containers
  │   └─ docker-compose up -d         # redpanda, publisher, grafana, etc.
  │
  ├─ setup:ws                         # Clone repo + start ws-server
  │   └─ docker-compose up -d         # ws-go, promtail
  │
  ├─ setup:loadtest                   # Clone repo + build test image
  │   └─ docker build loadtest
  │
  └─ health:all                       # Verify everything is healthy
```

#### 2. Daily Start/Stop

```
task gcp:up
  └─ deployment:quick-start
      ├─ services:start:all
      │   ├─ start:backend
      │   └─ start:ws
      └─ health:all

task gcp:down
  └─ deployment:stop
      └─ services:stop:all
          ├─ stop:backend
          └─ stop:ws
```

#### 3. Code Updates

```
task gcp:deployment:rebuild-code
  │
  ├─ rebuild:backend
  │   ├─ update:backend              # git pull
  │   └─ services:restart:backend    # docker-compose restart
  │
  ├─ rebuild:ws
  │   ├─ update:ws                   # git pull
  │   └─ services:restart:ws         # docker-compose restart
  │
  └─ health:all
```

#### 4. Load Testing Flow

```
# Run capacity test
task gcp:load-test:capacity
  └─ SSH to loadtest instance
      └─ docker run loadtest \
         --connections 12000 \
         --duration 1800 \
         --rate 25 \
         --workers 64

# Monitor (in another terminal)
task gcp:health:ws                    # Check ws-server stats
task gcp:services:logs:ws:tail        # Watch logs
```

### Dependency Graph

```
gcp:deploy
  └─ deployment:setup
      ├─ deployment:infrastructure
      │   ├─ create:backend
      │   ├─ create:ws
      │   ├─ create:loadtest
      │   ├─ firewall:backend
      │   ├─ firewall:ws
      │   └─ reserve-ip:ws
      ├─ setup:backend
      │   └─ utilities:wait-for-startup-script
      ├─ setup:ws
      │   └─ utilities:wait-for-startup-script
      └─ setup:loadtest
          └─ utilities:wait-for-startup-script

gcp:restart
  └─ services:restart:all
      ├─ restart:backend
      │   └─ utilities:ssh-exec-as-deploy
      └─ restart:ws
          └─ utilities:ssh-exec-as-deploy
```

---

## Quick Reference

### Most Common Commands

```bash
# Daily operations
task gcp:up                           # Start everything
task gcp:down                         # Stop everything
task gcp:restart                      # Restart + health check
task gcp:status                       # Full status overview

# Deployment
task gcp:deploy                       # First-time setup
task gcp:deployment:rebuild-code      # Update code

# Monitoring
task gcp:health:all                   # Health checks
task gcp:urls                         # Service URLs
task gcp:services:logs:ws:tail        # Watch logs

# Testing
task gcp:load-test:capacity           # Full capacity test

# Troubleshooting
task gcp:services:ps:all              # See all containers
task gcp:deployment:ssh:ws            # SSH to ws-server
task gcp:deployment:ssh:backend       # SSH to backend
```

### Command Naming Convention

```
gcp:{category}:{action}:{target}

Examples:
  gcp:services:start:backend          # Start backend services
  gcp:deployment:create:ws            # Create ws-server instance
  gcp:health:all                      # Check all health
  gcp:load-test:capacity              # Run capacity test
```

### Namespace Hierarchy

```
gcp:                                  # High-level shortcuts
  ├─ deployment:                      # Infrastructure & deployment
  ├─ services:                        # Service lifecycle
  ├─ health:                          # Health checks
  ├─ load-test:                       # Load testing
  ├─ stats:                           # Info & URLs
  └─ utilities:                       # Internal helpers (don't use directly)
```

---

## Notes

- **Idempotent**: Most commands can be run multiple times safely
- **Sequential**: Infrastructure must exist before services can start
- **Stateful**: Instances persist until explicitly deleted
- **Costs**: Running instances cost money - use `gcp:down` when not in use
- **Static IP**: ws-server has a static IP that persists across instance deletions

## See Also

- Main Taskfile: `/Volumes/Dev/Codev/Toniq/odin-ws/Taskfile.yml`
- GCP Deployment Guide: `GCP_CONSOLIDATION_PLAN.md`
- Health Check Reference: `health.yml`
- Load Test Configs: `load-test.yml`
