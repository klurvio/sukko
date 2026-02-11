# Session Handoff: 2026-02-10 - GCP Foundation Layer & Dev Deployment

**Date:** 2026-02-10
**Branch:** `refactor/taskfile-provisioning-consolidation`
**Status:** Blocked - Terraform ADC permissions issue

## Session Goals

Implement a two-layer Terraform architecture for GCP dev deployment:
1. Foundation layer (VPC, static IPs, firewall rules) - persists across cluster lifecycle
2. Cluster layer (GKE) - uses foundation VPC via remote state

## What Was Accomplished

### Committed (`24303c1`)
- Created foundation Terraform module (`modules/foundation/`)
- Created foundation environment (`environments/standard/foundation/`)
- Made GKE cluster module accept external VPC (conditional `count` on VPC/subnet/firewall/IP resources)
- Updated dev environment to reference foundation via `terraform_remote_state`
- Added `loadBalancerIP` support to ws-gateway Helm chart
- Added `annotations` support to redpanda external service (for Internal LB)
- Updated dev.yaml with Internal LB annotation for Redpanda
- Added foundation tasks to taskfile (`foundation:init`, `foundation:plan`, `foundation:apply`)
- Updated `deploy` task to inject both gateway and redpanda IPs from foundation state
- Rewrote `docs/architecture/current/2026-02-10_PLAN_GCP_DEV_DEPLOYMENT.md`

### Uncommitted (staged, ready to commit)
- Renamed `foundation/` -> `dev-foundation/` (ENV-aware naming)
- Fixed Cloud NAT scoping: `ALL_SUBNETWORKS_ALL_IP_RANGES` -> `LIST_OF_SUBNETWORKS` with explicit WS subnet
- Renamed namespace: `odin-dev` -> `odin-ws-dev`
- Renamed Artifact Registry: `odin` -> `odin-ws`
- Renamed VPC: `odin-dev-vpc` -> `odin-ws-dev-vpc`
- Prefixed static IP names with VPC name: `odin-ws-dev-vpc-gateway-external`, `odin-ws-dev-vpc-redpanda-internal`
- Fixed module source path: `../../modules/foundation` -> `../../../modules/foundation`
- Removed `provision:delete` task (provisioning done via API)

## Key Decisions & Context

1. **One VPC per environment** (best practice) - `odin-ws-dev-vpc` for dev, `odin-ws-stg-vpc` for stg, etc.
2. **No CDC subnet** - removed from plan; can be added later when needed
3. **Foundation dir is ENV-aware** - `dev-foundation/`, `stg-foundation/` pattern prevents accidental cross-env foundation applies
4. **Health check firewall without target_tags** - safe because VPC is dedicated; every instance is a GKE node
5. **Cloud NAT scoped to WS subnet only** - prevents NAT from affecting future subnets in the VPC
6. **Helm release name stays `odin`** - namespace (`odin-ws-dev`) already provides isolation; prefixing would make resource names redundant
7. **No prompt on `tf:apply`** - plan-file workflow (`-out=tfplan` + `apply tfplan`) is Terraform best practice; safety gate is the plan output

## GCP Resource Naming Convention

All resources prefixed with `odin-ws-dev`:

| Resource | GCP Name |
|----------|----------|
| VPC | `odin-ws-dev-vpc` |
| Subnet | `odin-ws-dev-vpc-ws-subnet` |
| Gateway IP | `odin-ws-dev-vpc-gateway-external` |
| Redpanda IP | `odin-ws-dev-vpc-redpanda-internal` |
| Firewall (internal) | `odin-ws-dev-vpc-allow-internal` |
| Firewall (health) | `odin-ws-dev-vpc-allow-health-checks` |
| GKE cluster | `odin-ws-dev` |
| Node pool | `odin-ws-dev-primary-pool` |
| Router | `odin-ws-dev-router` |
| NAT | `odin-ws-dev-nat` |
| K8s namespace | `odin-ws-dev` |
| Artifact Registry | `odin-ws` |

## Issues & Solutions

### RESOLVED: Module source path
- `dev-foundation/main.tf` had `source = "../../modules/foundation"` (2 levels up)
- Needed `../../../modules/foundation` (3 levels: dev-foundation -> standard -> environments -> terraform)

### BLOCKED: Terraform ADC 403 on apply
- `terraform plan` succeeds but `terraform apply` gets 403 for `compute.networks.create` and `compute.addresses.create`
- User has `roles/compute.admin` + `roles/compute.networkAdmin` on `odin-9e902`
- `gcloud auth application-default login` was run, quota project is correct (`odin-9e902`)
- `$GOOGLE_APPLICATION_CREDENTIALS` is unset
- **Not yet tried:** `gcloud auth application-default revoke` then re-login
- **Not yet tried:** Direct gcloud test: `gcloud compute networks create test-vpc --project=odin-9e902 --subnet-mode=custom`
- **Possible cause:** Org policy restriction, or ADC token caching issue

## Current State

- Foundation and cluster Terraform code is complete and reviewed for safety
- Uncommitted changes need to be committed
- `task k8s:setup ENV=dev` fails at `foundation:apply` due to 403 permission error
- The plan output is correct (6 new resources, 0 changes, 0 destroys)

## Next Steps

### Immediate Priority
1. Resolve the ADC 403 issue:
   ```bash
   # Try revoking and re-authenticating
   gcloud auth application-default revoke
   gcloud auth application-default login

   # Test with direct gcloud
   gcloud compute networks create test-vpc-delete-me --project=odin-9e902 --subnet-mode=custom
   # If works, delete immediately:
   gcloud compute networks delete test-vpc-delete-me --project=odin-9e902 --quiet

   # Check org policies
   gcloud resource-manager org-policies list --project=odin-9e902
   ```
2. Commit uncommitted changes
3. Re-run `task k8s:setup ENV=dev`

### Post-Setup
4. Create Artifact Registry: `gcloud artifacts repositories create odin-ws --project=odin-9e902 --location=us-central1 --repository-format=docker`
5. Create Kafka topics: `task k8s:provision:create ENV=dev`
6. Verify: `task k8s:status ENV=dev` and `task k8s:external-ips ENV=dev`

## Files Modified (uncommitted)

| File | Change |
|------|--------|
| `deployments/terraform/modules/foundation/main.tf` | Prefix static IPs with vpc_name, scoped comment |
| `deployments/terraform/modules/gke-standard-cluster/main.tf` | Cloud NAT scoped to WS subnet |
| `deployments/terraform/environments/standard/dev/main.tf` | Remote state path `../dev-foundation/` |
| `deployments/terraform/environments/standard/dev/terraform.tfvars` | Namespace `odin-ws-dev` |
| `deployments/terraform/environments/standard/dev-foundation/terraform.tfvars` | VPC name `odin-ws-dev-vpc` |
| `deployments/helm/odin/values/standard/dev.yaml` | Namespace + registry `odin-ws` |
| `taskfiles/k8s.yml` | ENV-aware foundation dir, namespace `odin-ws-*`, registry `odin-ws`, removed provision:delete |
| `docs/architecture/current/2026-02-10_PLAN_GCP_DEV_DEPLOYMENT.md` | Updated references |

## Commands for Next Session

```bash
# Commit uncommitted changes
git add -u && git add deployments/terraform/environments/standard/dev-foundation/
git commit -m "fix: rename resources and scope NAT for safe GCP deployment"

# Resolve ADC issue, then:
task k8s:setup ENV=dev

# Post-setup
gcloud artifacts repositories create odin-ws --project=odin-9e902 --location=us-central1 --repository-format=docker
task k8s:provision:create ENV=dev
task k8s:status ENV=dev
task k8s:external-ips ENV=dev
```

## Open Questions

1. Why does `terraform plan` succeed but `terraform apply` gets 403 with the same ADC credentials?
2. Is there an org policy on `odin-9e902` restricting VPC creation?
3. Should the Artifact Registry be created before or after `task k8s:setup`? (Currently it's a manual pre-requisite)
