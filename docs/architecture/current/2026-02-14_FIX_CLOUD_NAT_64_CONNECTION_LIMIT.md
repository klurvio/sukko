# Fix: Cloud NAT 64-Connection Limit for Loadtest VM

**Date**: 2026-02-14
**Status**: Pending
**Severity**: Blocks loadtest at scale (>64 connections)

## Problem

Running `task gce:loadtest:run ENV=dev CONNECTIONS=10000` only establishes **64 connections** and then every subsequent connection attempt times out.

Dashboard shows 64 active connections split across 2 ws-server pods (25 + 39), with `failed_connections` incrementing by 1 every ~10 seconds.

## Root Cause

**GCP Cloud NAT default `minPortsPerVm` = 64.**

The loadtest VM is created with `--no-address` (no external IP). Its traffic to the gateway's external LoadBalancer IP (`34.44.7.48:443`) routes through Cloud NAT. GCP allocates a default of 64 source ports per VM, limiting the VM to exactly 64 simultaneous connections to the same destination IP:port.

### Traffic paths

```
Browser client:  Internet -> LB (34.44.7.48:443) -> Gateway pod     (no NAT, not affected)
Loadtest VM:     VM (no ext IP) -> Cloud NAT -> LB (34.44.7.48:443) (NAT bottleneck at 64)
Publisher VM:    VM -> Redpanda internal IP (10.x.x.x:9092)         (no NAT, not affected)
```

### Current Cloud NAT config

```
Resource: google_compute_router_nat.nat
File:     deployments/terraform/modules/gke-standard-cluster/main.tf:67-84
Name:     odin-ws-dev-nat
Router:   odin-ws-dev-router
Region:   us-central1

Missing settings (using GCP defaults):
  minPortsPerVm:                 64    (default)
  enableDynamicPortAllocation:   false (default)
```

## Fix

### Recommended: gcloud (dev-only, zero blast radius)

Apply directly to the dev NAT. Does not affect stg/prod.

```bash
gcloud compute routers nats update odin-ws-dev-nat \
  --router=odin-ws-dev-router \
  --region=us-central1 \
  --project=odin-9e902 \
  --min-ports-per-vm=16384 \
  --enable-dynamic-port-allocation \
  --max-ports-per-vm=30000
```

### Alternative: Terraform (requires variable extraction)

The Cloud NAT resource lives in the **shared module** `deployments/terraform/modules/gke-standard-cluster/main.tf`,
which is used by **all environments** (dev, stg, prod). Hardcoding port values there would apply
to all clusters.

If using Terraform, expose these as variables with safe defaults so each environment can opt in:

```terraform
# In modules/gke-standard-cluster/variables.tf
variable "nat_min_ports_per_vm" {
  description = "Minimum NAT ports per VM (default 64). Increase for high-connection workloads like loadtest."
  type        = number
  default     = 64
}

variable "nat_enable_dynamic_port_allocation" {
  description = "Enable dynamic NAT port allocation. Required when min_ports_per_vm > 64."
  type        = bool
  default     = false
}

variable "nat_max_ports_per_vm" {
  description = "Maximum NAT ports per VM when dynamic allocation is enabled."
  type        = number
  default     = 30000
}
```

```terraform
# In modules/gke-standard-cluster/main.tf
resource "google_compute_router_nat" "nat" {
  name                               = "${var.cluster_name}-nat"
  router                             = google_compute_router.router.name
  region                             = var.region
  project                            = var.project_id
  nat_ip_allocate_option             = "AUTO_ONLY"
  source_subnetwork_ip_ranges_to_nat = "LIST_OF_SUBNETWORKS"

  min_ports_per_vm                 = var.nat_min_ports_per_vm
  enable_dynamic_port_allocation   = var.nat_enable_dynamic_port_allocation
  max_ports_per_vm                 = var.nat_enable_dynamic_port_allocation ? var.nat_max_ports_per_vm : null

  subnetwork {
    name                    = local.subnet_id
    source_ip_ranges_to_nat = ["ALL_IP_RANGES"]
  }

  log_config {
    enable = true
    filter = "ERRORS_ONLY"
  }
}
```

Then override only in dev:

```terraform
# In environments/standard/dev/main.tf
module "gke" {
  source = "../../../modules/gke-standard-cluster"
  # ...existing config...

  nat_min_ports_per_vm                 = 16384
  nat_enable_dynamic_port_allocation   = true
}
```

Stg/prod remain on defaults (64 ports, no dynamic allocation) unless explicitly changed.

## Safety Review

### Safe (no impact)

- **Browser clients**: Inbound traffic to the LoadBalancer does not traverse Cloud NAT.
- **Publisher VM**: Connects to Redpanda via internal VPC IP — never touches NAT.
- **GKE cluster**: Subnet has `private_ip_google_access = true` for image pulls. Cloud NAT is redundant for GKE nodes.
- **Other VPCs/clusters**: Change scoped to `odin-ws-dev-vpc` only.
- **Existing connections**: Cloud NAT port updates are non-disruptive — applied to new port mappings only.
- **Cost**: Negligible. Port reservation doesn't add cost; only data processed through NAT is billed.

### Risks to watch

| Risk | Severity | Detail |
|------|----------|--------|
| Shared module blast radius | **MEDIUM** | Hardcoding in the shared Terraform module applies to dev/stg/prod. Use variables (above) or gcloud to avoid. |
| NAT IP exhaustion | **LOW** | 16,384 ports/VM means each NAT IP serves ~4 VMs (64,512 usable ports per IP). Fine for dev (1-2 VMs), but could exhaust NAT IPs if many VMs are added. `AUTO_ONLY` auto-allocates more IPs as needed. |
| Port starvation | **LOW** | `max_ports_per_vm=30000` lets one VM consume an entire NAT IP. Acceptable for dev loadtest; avoid in prod without review. |

## Verification

After applying, re-run:
```bash
task gce:loadtest:run ENV=dev CONNECTIONS=10000 DURATION=30m
```

Expected: connections should ramp to 10,000 (at 100/sec = ~100 seconds to full ramp).
