# Sukko WebSocket Server - Development Environment Setup

**Date:** 2026-01-17

---

## What Is This?

Sukko WebSocket Server provides real-time data streaming for the trading platform. It pushes live updates (trades, price changes, liquidity, etc.) to connected clients instantly, rather than clients polling the API repeatedly.

---

## How It Fits

```
┌─────────────┐      ┌─────────────┐      ┌─────────────┐
│sukko-api-cdc │ ───▶ │  Redpanda   │ ───▶ │   sukko   │ ───▶ Clients
│    (CDC)    │      │  (messages) │      │ (websocket) │
└─────────────┘      └─────────────┘      └─────────────┘
```

- **sukko-api-cdc** captures database changes (new trade, price update, etc.) and publishes them as events
- **Redpanda** queues and delivers messages
- **sukko** pushes updates to connected browser/mobile clients in real-time

---

## What the POC Proved

- Sub-100ms message delivery from backend to clients
- Self-hosted infrastructure works reliably (Redpanda, NATS, monitoring stack)
- Integrated with sukko-api-cdc and sukko-explorer (as client) - validated and working

---

## Why Move Now?

The POC is running on a Migs' personal free-tier GCP account. Risks of staying:

- **Quotas & limits** - Free tier has strict resource limits
- **No SLA** - Can be suspended without notice
- **Data on personal account** - Company data shouldn't live on personal infrastructure
- **Blocks progress** - Can't onboard real users or integrate with production sukko-api

---

## What's Next

| Phase | Environment | Purpose |
|-------|-------------|---------|
| ✅ Done | POC (free-tier) | Validate architecture, integrate with sukko-api-cdc and sukko-explorer |
| 👉 Now | Development | Move to company infrastructure, continue iteration |
| Next | Staging | Pre-production testing |
| Future | Production | Live users |

---

## What We Need From You

### 1. GCP Project Access

- [ ] Create a GCP project for Sukko development (or provide an existing one)
- [ ] Add Red and Migs as project members
- [ ] Link the project to the company billing account

---

### 2. Budget Approval

Estimated monthly costs for development environment:

| Resource | Estimated Cost |
|----------|---------------|
| GKE Cluster (1-2 nodes) | ~$50-100/month |
| Persistent Storage | ~$10-20/month |
| **Total** | **~$60-120/month** |

- [ ] Approve estimated development infrastructure budget

---

## What We Handle

Once access is granted, Red and Migs will:

- Deploy development Kubernetes cluster
- Set up Redpanda (message streaming) - self-hosted
- Set up NATS (event bus) - self-hosted
- Deploy monitoring stack (Grafana, Prometheus, Loki) - self-hosted
- Migrate from free-tier POC account

---

## Timeline

Once access is granted, migration takes approximately 1 day.
