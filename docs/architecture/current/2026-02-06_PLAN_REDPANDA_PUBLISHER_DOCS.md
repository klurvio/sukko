# Plan: Document Direct Redpanda Publishing for CDC

**Status:** Implemented
**Date:** 2026-02-06
**Scope:** Add documentation for CDC pipelines publishing directly to Redpanda

---

## Summary

CDC (Change Data Capture) pipelines publish events directly to Redpanda via internal Kubernetes networking. These events are then consumed by the ws-server and broadcast to subscribed WebSocket clients.

---

## Changes Made

### 1. `ws/docs/asyncapi/redpanda.asyncapi.yaml`

#### A. Internal Connection Details (in info.description)

Added section documenting:
- Kubernetes broker address (placeholder, confirm with DevOps)
- Publishing requirements (topic, key, value format)
- Aggregate channel convention: `odin.all.trade` for all trades
- TypeScript/KafkaJS code examples
- Optional headers for observability

#### B. Internal Server Definition

```yaml
servers:
  redpanda-internal:
    host: "redpanda.odin-infra.svc.cluster.local:9092"
    protocol: kafka
    description: |
      Internal Redpanda access for CDC pipelines and backend services.
      Accessible only within the Kubernetes cluster.
      NOTE: Confirm actual broker address with DevOps.
```

#### C. CDC Publish Schema

Added `CDCPublishRequest` schema documenting the TypeScript/KafkaJS publish structure with examples for both specific (`odin.BTC.trade`) and aggregate (`odin.all.trade`) channels.

#### D. Updated Examples

Added `odin.all.trade` examples to:
- `CDCPublishRequest` schema
- `KafkaEvent` schema
- `event` message examples

---

## Key Conventions

### Channel/Key Format

| Type | Key Format | Example |
|------|------------|---------|
| Specific token | `{tenant}.{symbol}.{category}` | `odin.BTC.trade` |
| Aggregate (all) | `{tenant}.all.{category}` | `odin.all.trade` |

### Topic Format

`{namespace}.{tenant}.{category}` — e.g., `prod.odin.trade`

---

## What Was NOT Changed

- Existing topic/key/value format documentation (already correct)
- WebSocket client publishing docs (separate concern in client-ws.asyncapi.yaml)
- Consumer documentation (ws-server side)

---

## Verification

```bash
go build ./...   # ✓ Passes
go test ./...    # ✓ Passes
```

