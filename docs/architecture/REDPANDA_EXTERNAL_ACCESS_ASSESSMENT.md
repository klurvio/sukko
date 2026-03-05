# Redpanda External Access: Architecture Assessment

**Date:** 2026-01-09
**Status:** Assessment Complete
**Authors:** Engineering Team

---

## Executive Summary

This document assesses two approaches for external publishers to send messages to Redpanda:

1. **Current:** External Redpanda LoadBalancer with static IP
2. **Proposed:** Internal-only Redpanda with NATS router for external ingestion

**Recommendation:** The NATS router approach provides better security and simpler configuration, but requires additional components. Choose based on whether security or simplicity is the primary concern.

---

## Table of Contents

- [Background](#background)
- [Current Architecture](#current-architecture)
- [Static IP Management](#static-ip-management)
- [Proposed Architecture: NATS Router](#proposed-architecture-nats-router)
- [Comparison](#comparison)
- [Redpanda Operator Considerations](#redpanda-operator-considerations)
- [Recommendations](#recommendations)
- [Implementation Guide](#implementation-guide)

---

## Background

### Problem Statement

External services (publisher VM, other clusters) need to send messages to Redpanda running in the sukko-* Kubernetes cluster. The current approach exposes Redpanda externally via LoadBalancer, which raises concerns about:

1. **Security:** Kafka protocol exposed to the internet
2. **Complexity:** Static IP and `advertisedHost` configuration
3. **Operational overhead:** Dual listener setup

### Key Questions Addressed

1. Does updating Redpanda require a new static IP?
2. What is the upgrade flow for internal and external consumers?
3. How does the Redpanda Operator affect this?
4. Should external communication be internal-only?

---

## Current Architecture

### External Redpanda with LoadBalancer

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              INTERNET                                        │
│                                                                              │
│   ┌─────────────────┐                                                       │
│   │ Publisher VM    │                                                       │
│   │ (sukko-tools)    │                                                       │
│   │                 │                                                       │
│   │ KAFKA_BROKERS=  │                                                       │
│   │ 34.72.x.x:9092  │───────────────────┐                                   │
│   └─────────────────┘                   │                                   │
│                                         │                                   │
│   ┌─────────────────┐                   │                                   │
│   │ Other Service   │                   │                                   │
│   │ (external)      │───────────────────┤                                   │
│   └─────────────────┘                   │                                   │
│                                         │                                   │
└─────────────────────────────────────────┼───────────────────────────────────┘
                                          │
                                          │ Kafka protocol (TCP 9092)
                                          │ EXPOSED TO INTERNET
                                          ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│  sukko-develop cluster                                                     │
│                                                                              │
│   ┌─────────────────────────────┐                                           │
│   │ LoadBalancer Service        │                                           │
│   │ sukko-redpanda-external      │                                           │
│   │ IP: 34.72.x.x (static)      │◄── Kafka exposed to internet             │
│   │ Port: 9092                  │                                           │
│   └──────────────┬──────────────┘                                           │
│                  │                                                           │
│                  ▼                                                           │
│   ┌─────────────────────────────┐     ┌─────────────────┐                   │
│   │ Redpanda                    │     │ ws-server       │                   │
│   │ ├── external: 19092         │◄────│ (consumer)      │                   │
│   │ └── internal: 9092          │     │                 │                   │
│   │                             │     │ sukko-redpanda   │                   │
│   │ advertisedHost: 34.72.x.x   │     │ :9092 (internal)│                   │
│   └─────────────────────────────┘     └─────────────────┘                   │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Current Configuration

**Terraform (static IP):**
```hcl
# deployments/terraform/gke-standard/modules/gke-cluster/main.tf
resource "google_compute_address" "redpanda_external" {
  name         = "${var.cluster_name}-redpanda-external"
  region       = var.region
  address_type = "EXTERNAL"
  project      = var.project_id
}
```

**Helm deployment:**
```bash
# taskfiles/k8s/standard.yml
helm upgrade --install sukko ./chart \
  --set redpanda.externalAccess.loadBalancerIP=${REDPANDA_IP} \
  --set redpanda.externalAccess.advertisedHost=${REDPANDA_IP}
```

**Redpanda dual listeners:**
```yaml
# StatefulSet command args
--kafka-addr=internal://0.0.0.0:9092,external://0.0.0.0:19092
--advertise-kafka-addr=internal://sukko-redpanda:9092,external://34.72.x.x:9092
```

### Concerns with Current Approach

| Concern | Description |
|---------|-------------|
| **Security** | Kafka protocol exposed to internet without TLS/SASL by default |
| **Complexity** | Dual listener configuration, advertisedHost must match static IP |
| **Attack surface** | Port 9092 open to potential attacks |
| **Configuration drift** | Static IP must be synchronized across Terraform, Helm, and clients |

---

## Static IP Management

### How Static IP Works

```
┌─────────────────────────────────────────────────────────────────┐
│                    GCP Resources (Terraform)                     │
│                                                                  │
│  google_compute_address.redpanda_external                       │
│  ├── name: "sukko-develop-redpanda-external"                  │
│  ├── address: "34.72.x.x"                                       │
│  └── lifecycle: INDEPENDENT of K8s resources                    │
│                           │                                      │
│                           │ referenced by                        │
│                           ▼                                      │
├─────────────────────────────────────────────────────────────────┤
│                    K8s Resources (Helm)                          │
│                                                                  │
│  Service/sukko-redpanda-external                                 │
│  ├── spec.loadBalancerIP: "34.72.x.x" ◄── requests this IP     │
│  └── status.loadBalancer.ingress[0].ip: "34.72.x.x"            │
│                           │                                      │
│                           │ routes to                            │
│                           ▼                                      │
│  StatefulSet/sukko-redpanda                                      │
│  └── Pod/sukko-redpanda-0  ◄── this restarts during upgrade     │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Does Updating Redpanda Require New Static IP?

**No.** The static IP persists across Redpanda updates because:

1. The static IP is a `google_compute_address` resource in Terraform
2. It exists independently of K8s resources
3. The LoadBalancer Service references this IP via `loadBalancerIP`
4. Helm upgrades don't recreate the Service (they update it in-place)

### When Would IP Change?

| Action | IP Changes? | Reason |
|--------|-------------|--------|
| `helm upgrade` (normal) | **No** | Service unchanged |
| `kubectl rollout restart` | **No** | Service unchanged |
| `terraform apply` (normal) | **No** | IP resource unchanged |
| `terraform destroy` + `apply` | **Yes** | IP resource recreated |
| `helm uninstall` + `install` without `loadBalancerIP` | **Maybe** | Ephemeral IP assigned |

### Upgrade Flow

```
REDPANDA UPGRADE FLOW
─────────────────────────────────────────────────────────────────

Step 1: Developer triggers upgrade
        $ task k8s:gke-standard:deploy GKE_STD_ENV=develop

Step 2: Helm updates K8s resources
        - Service: NO CHANGE (same loadBalancerIP)
        - StatefulSet: CHANGED (new image tag)

Step 3: StatefulSet rolling update
        T+0s    Pod running (old version)
        T+1s    Pod terminating (graceful shutdown)
        T+30s   Pod terminated
        T+31s   Pod creating (new version)
        T+60s   Pod running (new version), readiness passed

Step 4: Clients reconnect
        - Internal (ws-server): reconnects to sukko-redpanda:9092
        - External (publisher): reconnects to 34.72.x.x:9092 (SAME IP)

RESULT: No IP change, no client reconfiguration needed
DOWNTIME: ~30-60 seconds (single replica)
```

---

## Proposed Architecture: NATS Router

### Internal-Only Redpanda with NATS Ingestion

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              INTERNET                                        │
│                                                                              │
│   ┌─────────────────┐                                                       │
│   │ Publisher VM    │                                                       │
│   │ (sukko-tools)    │                                                       │
│   │                 │                                                       │
│   │ NATS client     │◄── Simple TCP, no broker discovery                   │
│   │ nats://x.x.x.x  │───────────────────┐                                   │
│   └─────────────────┘                   │                                   │
│                                         │                                   │
│   ┌─────────────────┐                   │                                   │
│   │ Other Service   │                   │                                   │
│   │ (external)      │───────────────────┤                                   │
│   └─────────────────┘                   │                                   │
│                                         │                                   │
└─────────────────────────────────────────┼───────────────────────────────────┘
                                          │
                                          │ NATS protocol (TCP 4222)
                                          │ Simpler, stateless
                                          ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│  sukko-develop cluster                                                     │
│                                                                              │
│   ┌─────────────────────────────┐                                           │
│   │ LoadBalancer Service        │                                           │
│   │ sukko-nats-external          │◄── Only NATS exposed (simpler)           │
│   │ IP: 34.72.x.x               │                                           │
│   │ Port: 4222                  │                                           │
│   └──────────────┬──────────────┘                                           │
│                  │                                                           │
│                  ▼                                                           │
│   ┌─────────────────────────────┐                                           │
│   │ NATS Server                 │                                           │
│   │ (receives external msgs)    │                                           │
│   └──────────────┬──────────────┘                                           │
│                  │                                                           │
│                  │ Internal pub/sub                                          │
│                  ▼                                                           │
│   ┌─────────────────────────────┐                                           │
│   │ NATS-to-Kafka Bridge        │◄── NEW COMPONENT                          │
│   │ (Deployment, 2+ replicas)   │                                           │
│   │                             │                                           │
│   │ Subscribes: sukko.>          │                                           │
│   │ Produces to: Redpanda       │                                           │
│   └──────────────┬──────────────┘                                           │
│                  │                                                           │
│                  │ Kafka protocol (internal only)                            │
│                  ▼                                                           │
│   ┌─────────────────────────────┐     ┌─────────────────┐                   │
│   │ Redpanda                    │     │ ws-server       │                   │
│   │ ClusterIP ONLY              │◄────│ (consumer)      │                   │
│   │ NO external listener        │     │                 │                   │
│   │ NO LoadBalancer             │     │ sukko-redpanda   │                   │
│   │                             │     │ :9092           │                   │
│   └─────────────────────────────┘     └─────────────────┘                   │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Benefits

| Benefit | Description |
|---------|-------------|
| **Security** | Redpanda never exposed externally |
| **Simplicity** | No `advertisedHost` configuration |
| **Protocol** | NATS is simpler than Kafka for external access |
| **Authentication** | NATS token auth is simpler than Kafka SASL |
| **Existing infra** | NATS already deployed in cluster |

### Trade-offs

| Trade-off | Description |
|-----------|-------------|
| **Extra component** | Bridge service needs development/deployment |
| **Extra hop** | +0.5ms latency through NATS |
| **Publisher changes** | Must change from Kafka to NATS client |
| **Bridge HA** | Bridge becomes potential SPOF, needs replicas |

---

## Comparison

### Security Analysis

| Aspect | External Redpanda | Internal + NATS Router |
|--------|-------------------|------------------------|
| Attack surface | Kafka protocol exposed | Only NATS exposed |
| Authentication | Kafka SASL (complex) | NATS tokens (simple) |
| Encryption | Kafka TLS (complex setup) | NATS TLS (simpler) |
| Protocol complexity | High (broker discovery) | Low (direct connect) |
| Firewall rules | Port 9092 open | Port 4222 open |

### Operational Analysis

| Aspect | External Redpanda | Internal + NATS Router |
|--------|-------------------|------------------------|
| Static IP | Required for Redpanda | Required for NATS |
| IP on upgrade | Need advertisedHost sync | Just LoadBalancer IP |
| Configuration | Dual listeners | Single NATS listener |
| Components | Redpanda only | NATS + Bridge |
| Failure modes | Direct path | Extra hop (bridge) |

### Performance Analysis

| Aspect | External Redpanda | Internal + NATS Router |
|--------|-------------------|------------------------|
| Latency | Direct to Kafka | +0.5ms (NATS hop) |
| Throughput | Kafka native | Bridge is bottleneck |
| Reliability | Single path | Bridge needs HA |
| Message ordering | Kafka guarantees | Depends on bridge |

### Decision Matrix

| Criteria | External Redpanda | Internal + NATS | Winner |
|----------|-------------------|-----------------|--------|
| Security | Kafka exposed | Only NATS exposed | **NATS** |
| Simplicity | Dual listeners | Single listener | **NATS** |
| Static IP config | Complex (advertisedHost) | Simple (just LB) | **NATS** |
| Components | Single (Redpanda) | Two (NATS + Bridge) | **Redpanda** |
| Latency | Direct path | Extra hop | **Redpanda** |
| Failure modes | Fewer | Bridge is SPOF | **Redpanda** |
| Publisher changes | None | Kafka → NATS client | **Redpanda** |

---

## Redpanda Operator Considerations

### How Operator Changes the Flow

With Redpanda Operator, the management changes but architecture remains similar:

```
CURRENT (Helm):
  Helm → StatefulSet → Redpanda Pods
  Helm → Service (LoadBalancer)

WITH OPERATOR:
  Redpanda CRD → Operator → StatefulSet → Redpanda Pods
  Redpanda CRD → Operator → Service (LoadBalancer)
```

### Static IP with Operator

The static IP configuration moves from Helm `--set` to the Redpanda CRD:

```yaml
# Redpanda CRD with static IP
apiVersion: cluster.redpanda.com/v1alpha2
kind: Redpanda
metadata:
  name: sukko-redpanda
spec:
  clusterSpec:
    external:
      enabled: true
      type: LoadBalancer
      annotations:
        networking.gke.io/load-balancer-ip-addresses: "34.72.x.x"
    listeners:
      kafka:
        external:
          default:
            advertisedHost: "34.72.x.x"
```

### Operator Does NOT Solve

| Concern | Operator Helps? |
|---------|-----------------|
| Static IP management | No - still needed |
| advertisedHost complexity | No - still required |
| External exposure security | No - still exposed |
| Dual listener setup | No - still required |

### Operator DOES Help With

| Benefit | Description |
|---------|-------------|
| Rolling upgrades | Health-aware, automatic |
| Self-healing | Cluster-aware recovery |
| Topic management | Declarative Topic CRDs |
| Day-2 operations | Simplified scaling, config |

---

## Recommendations

### Option 1: Keep External Redpanda (Simpler)

**Choose if:** Operational simplicity is priority, security risks are acceptable.

```
Publisher ──► Redpanda LB ──► Redpanda ──► ws-server
            (exposed)

Actions needed:
├── Add TLS encryption
├── Add SASL authentication
├── Add network policies
└── Document security posture
```

**Pros:** No new components, no publisher changes
**Cons:** Kafka protocol exposed, complex auth setup

### Option 2: NATS Router (More Secure)

**Choose if:** Security is priority, willing to add components.

```
Publisher ──► NATS LB ──► NATS ──► Bridge ──► Redpanda ──► ws-server
            (exposed)    (internal only)

Actions needed:
├── Build or deploy NATS-Kafka bridge
├── Expose NATS externally (LoadBalancer)
├── Update publisher to use NATS client
├── Configure bridge HA (2+ replicas)
└── Add monitoring for bridge
```

**Pros:** Better security, simpler protocol exposed
**Cons:** Extra component, publisher code changes

### Option 3: Hybrid (Security + Keep Kafka Client)

**Choose if:** Want security but can't change publisher code.

```
Publisher ──► Kafka Proxy LB ──► Kafka Proxy ──► Redpanda ──► ws-server
             (exposed, with TLS)  (terminates TLS, internal only)

Use: Strimzi Kafka Bridge or similar proxy
```

**Pros:** Keep Kafka client, add security layer
**Cons:** Extra component, proxy overhead

---

## Implementation Guide

### If Choosing NATS Router

#### 1. Create NATS External Service

```yaml
# deployments/k8s/helm/sukko/charts/nats/templates/service-external.yaml
{{- if .Values.externalAccess.enabled }}
apiVersion: v1
kind: Service
metadata:
  name: {{ include "nats.fullname" . }}-external
spec:
  type: LoadBalancer
  {{- if .Values.externalAccess.loadBalancerIP }}
  loadBalancerIP: {{ .Values.externalAccess.loadBalancerIP }}
  {{- end }}
  ports:
    - port: 4222
      targetPort: client
      name: client
  selector:
    {{- include "nats.selectorLabels" . | nindent 4 }}
{{- end }}
```

#### 2. Create NATS-Kafka Bridge

```go
// nats-kafka-bridge/main.go
package main

import (
    "github.com/nats-io/nats.go"
    "github.com/confluentinc/confluent-kafka-go/kafka"
)

func main() {
    nc, _ := nats.Connect("nats://sukko-nats:4222")

    producer, _ := kafka.NewProducer(&kafka.ConfigMap{
        "bootstrap.servers": "sukko-redpanda:9092",
    })

    // Subscribe to all sukko.* subjects
    nc.Subscribe("sukko.>", func(msg *nats.Msg) {
        topic := msg.Subject
        key := msg.Header.Get("key")

        producer.Produce(&kafka.Message{
            TopicPartition: kafka.TopicPartition{Topic: &topic},
            Key:            []byte(key),
            Value:          msg.Data,
        }, nil)
    })

    select {}
}
```

#### 3. Deploy Bridge

```yaml
# deployments/k8s/helm/sukko/charts/nats-kafka-bridge/templates/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nats-kafka-bridge
spec:
  replicas: 2  # HA
  selector:
    matchLabels:
      app: nats-kafka-bridge
  template:
    spec:
      containers:
        - name: bridge
          image: sukko/nats-kafka-bridge:latest
          env:
            - name: NATS_URL
              value: "nats://sukko-nats:4222"
            - name: KAFKA_BROKERS
              value: "sukko-redpanda:9092"
```

#### 4. Remove Redpanda External Access

```yaml
# values/standard/develop.yaml
redpanda:
  externalAccess:
    enabled: false  # No longer needed
```

#### 5. Update Publisher

```go
// Before (Kafka client)
producer.Produce(&kafka.Message{
    TopicPartition: kafka.TopicPartition{Topic: &topic},
    Key:            []byte(tokenID),
    Value:          data,
})

// After (NATS client)
nc.PublishMsg(&nats.Msg{
    Subject: topic,
    Data:    data,
    Header:  nats.Header{"key": []string{tokenID}},
})
```

---

## References

- [Redpanda Operator Spike](../spikes/redpanda-operator-spike.md)
- [NATS External Broadcast Bus Spike](../spikes/SPIKE_EXTERNAL_BROADCAST_BUS.md)
- [GKE Standard Deployment Guide](../deployment/GKE_STANDARD_DEPLOYMENT_GUIDE.md)
- [Redpanda Documentation](https://docs.redpanda.com/)
- [NATS Documentation](https://docs.nats.io/)

---

## Revision History

| Date | Author | Changes |
|------|--------|---------|
| 2026-01-09 | Engineering | Initial assessment |
