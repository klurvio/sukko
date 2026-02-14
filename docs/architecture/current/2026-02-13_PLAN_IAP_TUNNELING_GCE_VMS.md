# Plan: Add IAP Tunneling for GCE VMs

**Date:** 2026-02-13
**Status:** Implemented

---

## Context

Publisher and loadtest GCE VMs are created with `--no-address` (no public IP) inside the VPC so they can reach Redpanda at `10.0.0.2:9092`. The `gcloud compute ssh` commands in `gce.yml` fail because there's no public IP and IAP tunneling isn't configured. The fix is to enable IAP tunneling: add a firewall rule and `--tunnel-through-iap` flag to SSH commands.

---

## Changes

### 1. Add IAP firewall rule to foundation module

**File:** `deployments/terraform/modules/foundation/main.tf`

Add after the `google_compute_firewall.internal` resource:

```hcl
resource "google_compute_firewall" "iap_ssh" {
  name    = "${var.vpc_name}-allow-iap-ssh"
  network = google_compute_network.vpc.name
  project = var.project_id

  allow {
    protocol = "tcp"
    ports    = ["22"]
  }

  source_ranges = ["35.235.240.0/20"]
}
```

This allows SSH (port 22 only) from Google's IAP proxy range. Non-destructive — adds one firewall rule, touches nothing else.

### 2. Add `--tunnel-through-iap` to all SSH commands

**File:** `taskfiles/gce.yml`

Add `--tunnel-through-iap` to all 6 `gcloud compute ssh` commands:
- `loadtest:run` (line 96)
- `loadtest:stop` (line 117)
- `loadtest:ssh` (line 133)
- `publisher:run` (line 210)
- `publisher:stop` (line 231)
- `publisher:ssh` (line 247)

---

## Safety

- **Firewall rule**: Adds one new rule. Does not modify or delete existing rules. Only opens port 22 from Google's IAP range — not from the internet.
- **IAM role**: Only grants tunnel access. No infrastructure modification permissions.
- **Terraform plan**: Should show exactly 1 resource to add, 0 to change, 0 to destroy.

---

## Verification

```bash
# 1. Apply firewall rule
task k8s:foundation:init ENV=dev    # If not already initialized
task k8s:foundation:plan ENV=dev    # Should show 1 to add, 0 to change, 0 to destroy
task k8s:foundation:apply ENV=dev

# 2. Test publisher
task gce:publisher:run ENV=dev RATE=1 DURATION=30m
task gce:publisher:logs ENV=dev
task gce:publisher:stop ENV=dev
```
