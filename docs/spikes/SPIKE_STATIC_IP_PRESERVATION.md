# Spike: Static IP Preservation for Redpanda Across Cluster Recreations

**Date:** 2026-01-12
**Status:** Draft
**Authors:** Engineering Team

---

## Executive Summary

This spike evaluates approaches to preserve static IPs for Redpanda when GKE clusters are destroyed and recreated. This is relevant for:
- **Cost optimization:** Destroying dev clusters overnight/weekends
- **Disaster recovery testing:** Validating rebuild procedures
- **Clean slate rebuilds:** When cluster state becomes problematic

---

## Problem Statement

Currently, `google_compute_address.redpanda_external` is defined inside the GKE cluster module (`deployments/terraform/gke-standard/modules/gke-cluster/main.tf:274-279`). When `task k8s:standard:destroy` runs `terraform destroy`, the static IP is also destroyed.

```
Current Architecture:
┌─────────────────────────────────────────────────────────────┐
│  Cluster Module (single Terraform state)                    │
│  ├─ GKE Cluster                                             │
│  ├─ Node Pools                                              │
│  ├─ VPC/Subnets                                             │
│  └─ google_compute_address.redpanda_external  ◄── PROBLEM   │
│       (destroyed when cluster is destroyed)                 │
└─────────────────────────────────────────────────────────────┘
```

---

## Section 1: Foundation Layer with External Static IP

### Overview

Move static IP resources to a separate Terraform configuration ("foundation layer") with its own state file. The foundation layer is never destroyed during normal operations.

### Architecture

```
┌─────────────────────────────────────────────────────────────┐
│  Foundation Layer (separate Terraform state)                │
│  └─ google_compute_address.redpanda_external                │
│       address_type = "EXTERNAL"                             │
│       (NEVER destroyed during cluster lifecycle)            │
└─────────────────────────────────────────────────────────────┘
                              │
                              │ terraform_remote_state
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  Cluster Layer (can be destroyed/recreated)                 │
│  ├─ GKE Cluster                                             │
│  ├─ Node Pools                                              │
│  └─ VPC/Subnets                                             │
│       (references IP from foundation)                       │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  Kubernetes (Helm)                                          │
│  └─ LoadBalancer Service                                    │
│       loadBalancerIP: <from foundation>                     │
│       (binds to pre-allocated external IP)                  │
└─────────────────────────────────────────────────────────────┘
```

### Network Flow

```
┌──────────────────┐         ┌─────────────────────────────────────┐
│  INTERNET        │         │  GKE Cluster                        │
│                  │         │                                     │
│  Publisher VM    │         │   External LoadBalancer             │
│  (any location)  │────────►│   34.72.x.x:9092 (public)           │
│                  │  TCP    │         │                           │
│                  │  9092   │         ▼                           │
└──────────────────┘         │   Redpanda Pod                      │
                             │   (Kafka protocol)                  │
                             └─────────────────────────────────────┘
```

### Directory Structure

```
deployments/terraform/gke-standard/
├── foundation/                    # NEW - persistent resources
│   ├── main.tf                   # Static IPs for all environments
│   ├── variables.tf
│   ├── outputs.tf
│   └── terraform.tfvars
├── modules/gke-cluster/          # MODIFIED - remove IP creation
├── develop/                      # MODIFIED - reference foundation
├── staging/
└── production/
```

### Terraform Configuration

**foundation/main.tf:**
```hcl
resource "google_compute_address" "redpanda_external" {
  for_each = toset(var.environments)  # ["develop", "staging", "production"]

  name         = "odin-ws-${each.key}-redpanda-external"
  region       = var.region
  address_type = "EXTERNAL"
  project      = var.project_id

  lifecycle {
    prevent_destroy = true  # Extra safety
  }
}

resource "google_compute_address" "gateway_external" {
  for_each = toset(var.environments)

  name         = "odin-ws-${each.key}-gateway-external"
  region       = var.region
  address_type = "EXTERNAL"
  project      = var.project_id

  lifecycle {
    prevent_destroy = true
  }
}

output "redpanda_external_ips" {
  value = { for env, addr in google_compute_address.redpanda_external : env => addr.address }
}

output "gateway_external_ips" {
  value = { for env, addr in google_compute_address.gateway_external : env => addr.address }
}
```

**develop/main.tf (modified):**
```hcl
data "terraform_remote_state" "foundation" {
  backend = "gcs"  # or "local"
  config = {
    bucket = "odin-terraform-state"
    prefix = "foundation"
  }
}

module "gke_cluster" {
  source = "../modules/gke-cluster"
  # ... existing config ...

  # Pass IPs from foundation
  redpanda_external_ip = data.terraform_remote_state.foundation.outputs.redpanda_external_ips["develop"]
  gateway_external_ip  = data.terraform_remote_state.foundation.outputs.gateway_external_ips["develop"]
}
```

### Pros

| Benefit | Description |
|---------|-------------|
| **Industry standard** | Common pattern used by Google, AWS, HashiCorp |
| **Clean separation** | Persistent vs ephemeral resources clearly separated |
| **No manual intervention** | Cluster destroy/recreate works seamlessly |
| **Extensible** | Can add DNS zones, certificates, service accounts |
| **Multi-environment** | Single foundation serves all environments |

### Cons

| Drawback | Description |
|----------|-------------|
| **Initial migration effort** | Need to import existing IPs, update state |
| **Two terraform states** | Must manage foundation separately from cluster |
| **Remote state dependency** | Cluster layer depends on foundation outputs |
| **Public IP exposure** | Kafka protocol exposed to internet |

### When to Use

- Publisher is **outside your VPC** (different GCP project, on-prem, etc.)
- You need **publicly accessible** Kafka endpoint
- You're already using external IPs and want to preserve them

---

## Section 2: Foundation Layer with Internal Static IP

### Overview

Same foundation layer approach, but using **internal static IPs** instead of external. Requires publisher to be in the same VPC as the GKE cluster.

### Architecture

```
┌─────────────────────────────────────────────────────────────┐
│  Foundation Layer (separate Terraform state)                │
│  ├─ VPC Network                                             │
│  ├─ Subnets                                                 │
│  └─ google_compute_address.redpanda_internal                │
│       address_type = "INTERNAL"                             │
│       subnetwork = subnet reference                         │
└─────────────────────────────────────────────────────────────┘
                              │
                              │ terraform_remote_state
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  Cluster Layer                                              │
│  ├─ GKE Cluster (uses VPC from foundation)                  │
│  └─ Node Pools                                              │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  Kubernetes (Helm)                                          │
│  └─ Internal LoadBalancer Service                           │
│       annotations:                                          │
│         cloud.google.com/load-balancer-type: "Internal"     │
│       loadBalancerIP: <internal IP from foundation>         │
└─────────────────────────────────────────────────────────────┘
```

### Network Flow

```
┌─────────────────────────────────────────────────────────────────────┐
│  Same VPC (e.g., 10.0.0.0/16)                                       │
│                                                                     │
│  ┌──────────────────┐         ┌─────────────────────────────────┐   │
│  │  Publisher VM    │         │  GKE Cluster                    │   │
│  │  10.0.1.50       │         │                                 │   │
│  │                  │         │   Internal LoadBalancer         │   │
│  │  KAFKA_BROKERS=  │────────►│   10.0.100.50:9092 (private)    │   │
│  │  10.0.100.50:9092│  VPC    │         │                       │   │
│  │                  │  route  │         ▼                       │   │
│  └──────────────────┘         │   Redpanda Pod                  │   │
│                               └─────────────────────────────────┘   │
│                                                                     │
│  NO INTERNET EXPOSURE                                               │
└─────────────────────────────────────────────────────────────────────┘
```

### Key Difference: VPC Must Be in Foundation

Internal IPs require a subnet reference. Since the IP must outlive the cluster, the VPC/subnet must also be in the foundation layer:

**foundation/main.tf:**
```hcl
# VPC must be in foundation for internal IP to reference it
resource "google_compute_network" "vpc" {
  name                    = "odin-ws-vpc"
  auto_create_subnetworks = false
  project                 = var.project_id
}

resource "google_compute_subnetwork" "subnet" {
  name          = "odin-ws-subnet"
  ip_cidr_range = "10.0.0.0/20"
  region        = var.region
  network       = google_compute_network.vpc.id
  project       = var.project_id

  secondary_ip_range {
    range_name    = "pods"
    ip_cidr_range = "10.1.0.0/16"
  }

  secondary_ip_range {
    range_name    = "services"
    ip_cidr_range = "10.2.0.0/20"
  }
}

# Internal static IP - requires subnet
resource "google_compute_address" "redpanda_internal" {
  for_each = toset(var.environments)

  name         = "odin-ws-${each.key}-redpanda-internal"
  region       = var.region
  address_type = "INTERNAL"
  subnetwork   = google_compute_subnetwork.subnet.id
  address      = cidrhost("10.0.0.0/20", 100 + index(var.environments, each.key))  # e.g., 10.0.0.100, 10.0.0.101
  project      = var.project_id

  lifecycle {
    prevent_destroy = true
  }
}

output "vpc_id" {
  value = google_compute_network.vpc.id
}

output "subnet_id" {
  value = google_compute_subnetwork.subnet.id
}

output "redpanda_internal_ips" {
  value = { for env, addr in google_compute_address.redpanda_internal : env => addr.address }
}
```

### Helm Chart Changes

**charts/redpanda/templates/service-external.yaml:**
```yaml
{{- if .Values.externalAccess.enabled }}
apiVersion: v1
kind: Service
metadata:
  name: {{ include "redpanda.fullname" . }}-external
  annotations:
    {{- if eq .Values.externalAccess.type "Internal" }}
    cloud.google.com/load-balancer-type: "Internal"  # NEW
    {{- end }}
spec:
  type: LoadBalancer
  {{- if .Values.externalAccess.loadBalancerIP }}
  loadBalancerIP: {{ .Values.externalAccess.loadBalancerIP }}
  {{- end }}
  ports:
    - port: 9092
      targetPort: {{ .Values.externalAccess.containerPort | default 19092 }}
      name: kafka
{{- end }}
```

**values/standard/develop.yaml:**
```yaml
redpanda:
  externalAccess:
    enabled: true
    type: Internal              # NEW - internal LoadBalancer
    loadBalancerIP: ""          # Injected from Terraform
    advertisedHost: ""          # Injected from Terraform
```

### Pros

| Benefit | Description |
|---------|-------------|
| **Better security** | No internet exposure, VPC-only access |
| **Lower latency** | No NAT/firewall traversal |
| **Lower cost** | No external IP charges (~$3/month saved) |
| **Same preservation** | Static IP survives cluster recreation |
| **Simpler firewall** | No need to open port 9092 to internet |

### Cons

| Drawback | Description |
|----------|-------------|
| **VPC coupling** | Publisher must be in same VPC |
| **More in foundation** | VPC/subnet must also be in foundation layer |
| **Initial migration** | Larger migration if VPC currently in cluster module |
| **Cross-project complexity** | Requires Shared VPC for multi-project setups |

### When to Use

- Publisher is **in the same VPC** as GKE cluster
- Security is a concern (don't want Kafka exposed to internet)
- You have control over publisher's network location

---

## Section 3: VPC Peering with Internal Static IP

### Overview

If the publisher is in a **different GKE cluster** (separate VPC) but you still want private connectivity without internet exposure, use **VPC Peering**. This connects two VPCs privately over Google's backbone network.

This is essentially Section 2 (Internal IP) extended to work across VPCs/clusters.

**Note:** The publisher is assumed to be a workload (pod) running in a separate GKE cluster, not a standalone VM.

### Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  Foundation Layer (separate Terraform state)                                │
│  ├─ Redpanda Cluster VPC + Subnets                                          │
│  ├─ VPC Peering (redpanda-cluster-vpc ↔ publisher-cluster-vpc)              │
│  └─ google_compute_address.redpanda_internal                                │
│       address_type = "INTERNAL"                                             │
└─────────────────────────────────────────────────────────────────────────────┘
                              │
                              │ terraform_remote_state
                              ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│  Redpanda Cluster Layer                                                     │
│  ├─ GKE Cluster (odin-ws-*) using VPC from foundation                       │
│  └─ Internal LoadBalancer (binds to internal static IP)                     │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Network Flow

```
┌───────────────────────────────────┐         ┌───────────────────────────────────┐
│  Publisher Cluster VPC            │         │  Redpanda Cluster VPC             │
│  CIDR: 10.1.0.0/16                │         │  CIDR: 10.0.0.0/16                │
│  Pods: 10.4.0.0/16                │         │  Pods: 10.2.0.0/16                │
│                                   │         │                                   │
│  ┌─────────────────────────┐      │ PEERING │      ┌─────────────────────────┐  │
│  │ Publisher GKE Cluster   │      │ (FREE)  │      │ Redpanda GKE Cluster    │  │
│  │                         │      │         │      │ (odin-ws-develop)       │  │
│  │  ┌───────────────────┐  │      │         │      │  ┌───────────────────┐  │  │
│  │  │ Publisher Pod     │  │      │         │      │  │ Internal LB       │  │  │
│  │  │ (Kafka producer)  │◄─┼──────┼─────────┼──────┼─►│ 10.0.100.50:9092  │  │  │
│  │  │                   │  │      │         │      │  │      │            │  │  │
│  │  │ KAFKA_BROKERS=    │  │Private         │      │  │      ▼            │  │  │
│  │  │ 10.0.100.50:9092  │  │Google│         │      │  │  Redpanda Pod     │  │  │
│  │  └───────────────────┘  │backbone        │      │  └───────────────────┘  │  │
│  └─────────────────────────┘      │         │      └─────────────────────────┘  │
│                                   │         │                                   │
└───────────────────────────────────┘         └───────────────────────────────────┘

• No internet exposure
• No cost (VPC peering is free)
• Pod-to-Service communication across clusters
• Traffic stays on Google's private network
• Static IP preserved via foundation layer
• VPC CIDRs and Pod CIDRs must NOT overlap
```

### Important: CIDR Planning for Multi-Cluster

When running multiple GKE clusters that need to communicate, plan CIDRs carefully:

| Network | Publisher Cluster | Redpanda Cluster | Notes |
|---------|------------------|------------------|-------|
| **VPC Subnet** | 10.1.0.0/16 | 10.0.0.0/16 | Must not overlap |
| **Pod CIDR** | 10.4.0.0/16 | 10.2.0.0/16 | Must not overlap |
| **Service CIDR** | 10.5.0.0/20 | 10.3.0.0/20 | Must not overlap |

### Terraform Configuration

**foundation/main.tf:**
```hcl
# Cluster VPC (in foundation so it persists)
resource "google_compute_network" "cluster_vpc" {
  name                    = "odin-ws-cluster-vpc"
  auto_create_subnetworks = false
  project                 = var.project_id
}

resource "google_compute_subnetwork" "cluster_subnet" {
  name          = "odin-ws-cluster-subnet"
  ip_cidr_range = "10.0.0.0/20"  # Must NOT overlap with publisher VPC
  region        = var.region
  network       = google_compute_network.cluster_vpc.id
  project       = var.project_id

  secondary_ip_range {
    range_name    = "pods"
    ip_cidr_range = "10.2.0.0/16"
  }

  secondary_ip_range {
    range_name    = "services"
    ip_cidr_range = "10.3.0.0/20"
  }
}

# VPC Peering - bidirectional
resource "google_compute_network_peering" "cluster_to_publisher" {
  name         = "cluster-to-publisher"
  network      = google_compute_network.cluster_vpc.self_link
  peer_network = var.publisher_vpc_self_link  # e.g., "projects/proj/global/networks/publisher-vpc"
}

resource "google_compute_network_peering" "publisher_to_cluster" {
  name         = "publisher-to-cluster"
  network      = var.publisher_vpc_self_link
  peer_network = google_compute_network.cluster_vpc.self_link
}

# Internal static IP for Redpanda
resource "google_compute_address" "redpanda_internal" {
  for_each = toset(var.environments)

  name         = "odin-ws-${each.key}-redpanda-internal"
  region       = var.region
  address_type = "INTERNAL"
  subnetwork   = google_compute_subnetwork.cluster_subnet.id
  address      = cidrhost("10.0.0.0/20", 100 + index(var.environments, each.key))
  project      = var.project_id

  lifecycle {
    prevent_destroy = true
  }
}

output "cluster_vpc_id" {
  value = google_compute_network.cluster_vpc.id
}

output "cluster_subnet_id" {
  value = google_compute_subnetwork.cluster_subnet.id
}

output "redpanda_internal_ips" {
  value = { for env, addr in google_compute_address.redpanda_internal : env => addr.address }
}
```

**foundation/variables.tf:**
```hcl
variable "publisher_vpc_self_link" {
  description = "Self-link of the publisher VPC to peer with"
  type        = string
  # e.g., "projects/my-project/global/networks/publisher-vpc"
}

variable "environments" {
  description = "List of environments"
  type        = list(string)
  default     = ["develop", "staging", "production"]
}

variable "region" {
  description = "GCP region"
  type        = string
  default     = "us-central1"
}

variable "project_id" {
  description = "GCP project ID"
  type        = string
}
```

### Firewall Rules

VPC peering doesn't automatically allow traffic - you need firewall rules. Since the publisher is a pod in another GKE cluster, allow traffic from both the VPC subnet and pod CIDR:

```hcl
# Allow publisher cluster (nodes + pods) to reach Redpanda on port 9092
resource "google_compute_firewall" "allow_kafka_from_publisher_cluster" {
  name    = "allow-kafka-from-publisher-cluster"
  network = google_compute_network.cluster_vpc.name
  project = var.project_id

  allow {
    protocol = "tcp"
    ports    = ["9092"]
  }

  # Allow from publisher cluster's VPC subnet AND pod CIDR
  source_ranges = [
    "10.1.0.0/16",  # Publisher VPC subnet CIDR
    "10.4.0.0/16",  # Publisher pod CIDR (VPC-native)
  ]

  # Target the internal load balancer / Redpanda nodes
  target_tags = ["redpanda", "gke-odin-ws-develop"]
}
```

### Alternative: Shared VPC (Single VPC for Both Clusters)

If both clusters can share the same VPC, you can avoid peering entirely:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  Shared VPC (10.0.0.0/8)                                                    │
│                                                                             │
│  ┌─────────────────────────┐         ┌─────────────────────────┐            │
│  │ Publisher GKE Cluster   │         │ Redpanda GKE Cluster    │            │
│  │ Subnet: 10.1.0.0/20     │         │ Subnet: 10.0.0.0/20     │            │
│  │ Pods: 10.4.0.0/16       │────────►│ Internal LB: 10.0.100.50│            │
│  │                         │ Direct  │                         │            │
│  └─────────────────────────┘  VPC    └─────────────────────────┘            │
│                               route                                         │
│  No peering needed - same VPC                                               │
└─────────────────────────────────────────────────────────────────────────────┘
```

This is simpler but requires both clusters to be planned together from the start.

### Pros

| Benefit | Description |
|---------|-------------|
| **No internet exposure** | All traffic stays on Google's private backbone |
| **Free** | VPC peering has no additional cost |
| **Low latency** | Direct private connectivity |
| **Separate VPCs** | Publisher and cluster can be in different VPCs/projects |
| **Static IP preserved** | Same foundation layer approach |
| **No publisher code changes** | Still uses Kafka client with internal IP |

### Cons

| Drawback | Description |
|----------|-------------|
| **CIDR planning required** | VPC CIDRs must not overlap |
| **No transitive peering** | A↔B and B↔C doesn't mean A↔C |
| **Firewall rules needed** | Must explicitly allow cross-VPC traffic |
| **Same org (typically)** | Cross-org peering has limitations |

### When to Use

- Publisher is a pod in a **different GKE cluster** (separate VPC)
- Both clusters are in the **same GCP organization**
- You want **private connectivity** without internet exposure
- You can ensure **non-overlapping CIDRs** (VPC, pod, and service ranges)
- You want **zero additional cost** for connectivity

### When NOT to Use

- VPC or pod CIDRs overlap and can't be changed
- Need cross-organization connectivity (consider Private Service Connect)
- Complex multi-VPC topology requiring transitive routing
- Both clusters can share the same VPC (use Section 2 instead - simpler)

---

## Section 4: NATS Router Approach

### Overview

Instead of exposing Redpanda externally, use NATS as a message router. NATS is exposed externally, and a bridge forwards messages to Redpanda internally.

### Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│  INTERNET                                                           │
│                                                                     │
│  ┌──────────────────┐                                               │
│  │  Publisher VM    │                                               │
│  │                  │                                               │
│  │  NATS Client     │───────────────────┐                           │
│  │  nats://x.x.x.x  │                   │                           │
│  └──────────────────┘                   │                           │
│                                         │ NATS protocol             │
│                                         │ (simpler than Kafka)      │
└─────────────────────────────────────────┼───────────────────────────┘
                                          │
                                          ▼
┌─────────────────────────────────────────────────────────────────────┐
│  GKE Cluster                                                        │
│                                                                     │
│   ┌─────────────────────┐                                           │
│   │ External LoadBalancer│                                          │
│   │ NATS: x.x.x.x:4222  │◄── Only NATS exposed (not Kafka)          │
│   └──────────┬──────────┘                                           │
│              │                                                      │
│              ▼                                                      │
│   ┌─────────────────────┐                                           │
│   │ NATS Server         │                                           │
│   └──────────┬──────────┘                                           │
│              │                                                      │
│              ▼                                                      │
│   ┌─────────────────────┐     ┌─────────────────────┐               │
│   │ NATS-Kafka Bridge   │────►│ Redpanda            │               │
│   │ (new component)     │     │ ClusterIP only      │               │
│   └─────────────────────┘     │ NO external access  │               │
│                               └──────────┬──────────┘               │
│                                          │                          │
│                                          ▼                          │
│                               ┌─────────────────────┐               │
│                               │ ws-server           │               │
│                               │ (consumer)          │               │
│                               └─────────────────────┘               │
└─────────────────────────────────────────────────────────────────────┘
```

### Components Required

1. **NATS External Service** - LoadBalancer exposing NATS to internet
2. **NATS-Kafka Bridge** - New service that subscribes to NATS and produces to Redpanda
3. **Publisher Changes** - Must change from Kafka client to NATS client

### Does NATS Solve Static IP Preservation?

**NO.** The static IP problem still exists - it just moves from Redpanda to NATS:

| Approach | What Needs Static IP |
|----------|---------------------|
| External Redpanda | Redpanda LoadBalancer |
| NATS Router | NATS LoadBalancer |

You still need the foundation layer to preserve the NATS static IP.

### Pros

| Benefit | Description |
|---------|-------------|
| **Better security** | Kafka never exposed to internet |
| **Simpler protocol** | NATS is simpler than Kafka for external access |
| **Simpler auth** | NATS tokens easier than Kafka SASL |
| **Existing infra** | NATS may already be in cluster |
| **Smaller attack surface** | NATS protocol less complex |

### Cons

| Drawback | Description |
|----------|-------------|
| **Extra component** | Bridge service needs development and maintenance |
| **Extra hop** | +0.5ms latency through NATS |
| **Publisher changes** | Must rewrite publisher to use NATS client |
| **Bridge is SPOF** | Needs HA (2+ replicas), monitoring |
| **Message ordering** | Must ensure bridge preserves Kafka ordering semantics |
| **Still needs static IP** | For NATS LoadBalancer (same problem, different place) |
| **Operational overhead** | More components to monitor and troubleshoot |

### When to Use

- Security is **highest priority** and Kafka exposure is unacceptable
- You're willing to invest in bridge development and maintenance
- Publisher code can be modified to use NATS client
- You need simpler authentication than Kafka SASL

### When NOT to Use

- If internal LoadBalancer (Section 2) can solve security concerns
- If you can't modify publisher code
- If you want to minimize operational complexity

---

## Comparison Matrix

| Criteria | External IP (S1) | Internal IP (S2) | VPC Peering (S3) | NATS Router (S4) |
|----------|------------------|------------------|------------------|------------------|
| **Static IP preserved** | Yes | Yes | Yes | Yes (for NATS) |
| **Security** | Low (internet) | High (VPC-only) | High (private) | High (NATS only) |
| **Complexity** | Low | Medium | Medium | High |
| **New components** | None | None | Peering + FW rules | Bridge service |
| **Publisher changes** | None | Network location | None | Code (NATS client) |
| **Latency** | Direct | Direct | Direct | +0.5ms |
| **Cost** | ~$3/mo (ext IP) | Free | Free | Higher (pods) |
| **Same VPC required** | No | Yes | No | No |
| **Internet exposure** | Yes | No | No | No |

---

## Recommendation

### If Both Clusters Can Share Same VPC

**Use Section 2: Foundation Layer with Internal Static IP**

- Best security (no internet exposure)
- No VPC peering needed
- No publisher code changes
- Simplest setup
- Plan non-overlapping subnets and pod CIDRs upfront

### If Clusters Must Be in Different VPCs (Same Org)

**Use Section 3: VPC Peering with Internal Static IP** (Recommended for your case)

- Private connectivity without internet exposure
- Free (no additional cost for peering)
- No publisher code changes
- Requires non-overlapping CIDRs (VPC, pod, service ranges)
- Pod-to-Service communication across clusters

### If Publisher Cluster is External (Different Org / On-Prem)

**Use Section 1: Foundation Layer with External Static IP**

- Simpler than NATS router
- No publisher code changes
- Add TLS/SASL for security if needed

### If Security is Paramount and Willing to Invest

**Use Section 4: NATS Router (with foundation layer for NATS IP)**

- Only if other options don't meet security requirements
- Requires bridge development
- Requires publisher code changes

---

## Implementation Guide

### Migration Steps (One-Time)

To migrate without losing the current IP:

1. **Create foundation module:**
   ```bash
   mkdir -p deployments/terraform/gke-standard/foundation
   # Create main.tf, variables.tf, outputs.tf, terraform.tfvars
   ```

2. **Import existing IP into foundation state:**
   ```bash
   cd deployments/terraform/gke-standard/foundation
   terraform init
   terraform import 'google_compute_address.redpanda_external["develop"]' \
     projects/{project}/regions/us-central1/addresses/odin-ws-develop-redpanda-external
   ```

3. **Remove IP from cluster state:**
   ```bash
   cd deployments/terraform/gke-standard/develop
   terraform state rm 'module.gke_cluster.google_compute_address.redpanda_external'
   ```

4. **Apply foundation:**
   ```bash
   cd deployments/terraform/gke-standard/foundation
   terraform apply
   ```

5. **Update cluster module and apply:**
   ```bash
   cd deployments/terraform/gke-standard/develop
   terraform apply
   ```

### Verification

1. `terraform plan` in foundation shows no changes
2. `terraform plan` in cluster shows no changes to IP resources
3. `kubectl get svc odin-redpanda-external` shows correct IP
4. Destroy and recreate cluster - IP is preserved
5. Publisher can still connect after recreation

---

## References

- [Redpanda External Access Assessment](../architecture/REDPANDA_EXTERNAL_ACCESS_ASSESSMENT.md)
- [Redpanda Operator Spike](./redpanda-operator-spike.md)
- [GKE Standard Deployment Guide](../deployment/GKE_STANDARD_DEPLOYMENT_GUIDE.md)

---

## Revision History

| Date | Author | Changes |
|------|--------|---------|
| 2026-01-12 | Engineering | Initial spike document |
| 2026-01-12 | Engineering | Added Section 3: VPC Peering option |
| 2026-01-12 | Engineering | Updated Section 3 for GKE-to-GKE cluster scenario |
