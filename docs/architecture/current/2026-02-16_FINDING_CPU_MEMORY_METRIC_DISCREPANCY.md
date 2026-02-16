# Finding: CPU/Memory Metric Discrepancy Between ResourceGuard and Grafana

**Date:** 2026-02-16
**Status:** Identified (not yet fixed)

## Symptom

ResourceGuard logs show high CPU triggering connection rejections, but the "WS Server CPU Usage" Grafana panel doesn't reflect the spike. The same may apply to memory.

## Root Cause: Two Issues

### Issue 1: Collector Overwrites SystemMonitor's CPU Gauge

There are **two writers** to the same `ws_cpu_usage_percent` Prometheus gauge:

```
SystemMonitor (singleton, created in main.go)
  → updateCPUOnly() every 1s → CPUUsagePercent.Set(fresh_cgroup_cpu)  ✓ ACCURATE
  → updateMetrics() every 15s → CPUUsagePercent.Set(fresh_cgroup_cpu)  ✓ ACCURATE

Collector (one per shard, 3 shards = 3 Collectors)
  → estimateCPU() every 15s → reads serverStats.CPUPercent (copied by ResourceGuard)
    → CPUUsagePercent.Set(stale_value)  ✗ OVERWRITES with up to 15s-old data
```

SystemMonitor updates the gauge with fresh cgroup data every 1 second. But every 15 seconds, each shard's Collector calls `estimateCPU()` (metrics.go:618-627) which reads a **copy** of the CPU value from `serverStats.CPUPercent` — a value that ResourceGuard copied from SystemMonitor up to 15s ago — and overwrites the gauge with stale data.

Same for memory: Collector sets `memoryUsageBytes` from `runtime.ReadMemStats()` every 15s (metrics.go:600-602), while SystemMonitor also sets it every 15s (system_monitor.go:146-153).

**Note:** The connections gauge had the same multi-shard overwrite bug but was already fixed — LoadBalancer now aggregates all shards (see metrics.go:594-597 comment). CPU and memory were NOT fixed.

### Issue 2: Prometheus Scrape Misses CPU Spikes

- Prometheus scrapes every **15 seconds** (`scrapeInterval: "15s"` in monitoring values.yaml:17)
- ResourceGuard checks CPU on **every connection request** using `systemMonitor.GetCPUPercent()` (updated every 1s)
- A CPU spike can trigger ResourceGuard rejections (logged) but resolve before the next Prometheus scrape

```
t=0s:  CPU=30%  ← Prometheus scrapes this
t=2s:  CPU=65%  ← ResourceGuard rejects! Logs "CPU exceeds reject threshold"
t=5s:  CPU=40%  ← spike resolved
t=15s: CPU=35%  ← Prometheus scrapes this (missed the 65% spike entirely)
```

The gauge is a point-in-time value, not a max or average over the scrape interval.

## Code Path Trace

### ResourceGuard Decision Path (real-time, per-request)
```
ShouldAcceptConnection() / ShouldPauseKafka()
  → rg.systemMonitor.GetCPUPercent()          [resource_guard.go:225,322]
    → SystemMonitor.metrics.CPUPercent         [system_monitor.go:195-198]
      → Updated every 1s by updateCPUOnly()    [system_monitor.go:112-135]
        → platform.CPUMonitor.GetPercent()     [cgroup-aware measurement]
```

### Prometheus Gauge Update Path (stale overwrite)
```
SystemMonitor.updateCPUOnly() [every 1s]
  → CPUUsagePercent.Set(cpuPercent)            [system_monitor.go:126]  ✓ Fresh

ResourceGuard.StartMonitoring() [every 15s]
  → serverStats.CPUPercent = systemMonitor.metrics.CPUPercent  [resource_guard.go:425]

Collector.collect() [every 15s, per shard]
  → estimateCPU()
    → reads serverStats.CPUPercent             [metrics.go:623]
    → CPUUsagePercent.Set(cpuPercent)           [metrics.go:625]  ✗ Overwrites with stale copy
```

### Grafana Dashboard Query
```
Panel: "WS Server CPU Usage" (overview.json, id: 30)
  expr: ws_cpu_usage_percent
  legendFormat: {{pod}}

Panel: "WS Server Memory Usage" (overview.json, id: 31)
  expr: ws_memory_bytes / 1024 / 1024
  legendFormat: {{pod}} Used
```

## Timing Summary

| Component | Frequency | What It Does |
|---|---|---|
| SystemMonitor CPU poll | Every 1s | Fresh cgroup CPU → sets `CPUUsagePercent` gauge |
| SystemMonitor full metrics | Every 15s | CPU + memory + goroutines → sets gauges |
| ResourceGuard sync | Every 15s | Copies SystemMonitor → serverStats (for health endpoint) |
| Collector (per shard) | Every 15s | Reads serverStats → **overwrites** `CPUUsagePercent` gauge |
| Prometheus scrape | Every 15s | Reads current gauge value |
| ResourceGuard decision | Per request | Reads SystemMonitor directly (1s-fresh data) |

## Existing Metrics That Could Help

These already exist in Prometheus but are NOT shown on the CPU/Memory panels:

| Metric | Description | File |
|---|---|---|
| `ws_capacity_rejections_total{reason="cpu"}` | Counter of CPU-triggered connection rejections | resource_guard.go |
| `ws_capacity_headroom_percent{resource="cpu"}` | CPU headroom (100% - current CPU) | resource_guard.go:466 |
| `ws_capacity_headroom_percent{resource="memory"}` | Memory headroom | resource_guard.go:466 |
| `ws_cpu_throttled_seconds_total` | Container CPU throttling time | system_monitor.go:133 |
| `ws_cpu_throttle_events_total` | Number of CPU throttle events | system_monitor.go:130 |
| `ws_cpu_container_percent` | CPU as % of container allocation | system_monitor.go:127 |

## Files Involved

| File | Role |
|---|---|
| `ws/internal/server/metrics/system_monitor.go` | Singleton, cgroup CPU measurement, Prometheus gauge updates |
| `ws/internal/server/metrics/metrics.go` | Collector.estimateCPU() overwrites gauge; metric definitions |
| `ws/internal/server/limits/resource_guard.go` | Per-request CPU check; copies to serverStats every 15s |
| `deployments/helm/odin/charts/monitoring/dashboards/overview.json` | Grafana panels for CPU (id:30) and Memory (id:31) |
| `deployments/helm/odin/charts/monitoring/values.yaml` | `scrapeInterval: "15s"` |

## Potential Fixes

### Dashboard-only (no code change, recommended first)
1. Add CPU rejection rate (`rate(ws_capacity_rejections_total{reason="cpu"}[1m])`) as overlay on CPU panel
2. Add reject/pause threshold lines (60%/70%) to CPU panel
3. Add CPU throttle events to CPU panel
4. Add memory headroom to memory panel

### Code fix (remove redundant Collector overwrite)
Remove `estimateCPU()` and `memoryUsageBytes.Set()` from `Collector.collect()` — SystemMonitor already handles these. This was already done for connections (see comment at metrics.go:594-597).

### Infrastructure (if finer granularity needed)
Decrease Prometheus `scrapeInterval` from 15s to 5s (increases storage/CPU on Prometheus side).
