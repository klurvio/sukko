# Plan: GCP Dev Deployment

**Date:** 2026-02-10
**Status:** Implemented
**Project ID:** odin-9e902

---

## Architecture

Two-layer Terraform design:

1. **Foundation layer** - VPC, subnets, static IPs, firewall rules. Persists across cluster destroy/recreate.
2. **Cluster layer** - GKE cluster and node pool. Uses foundation VPC via `terraform_remote_state`.

### Service Exposure

| Service | Type | Access |
|---------|------|--------|
| ws-gateway | LoadBalancer (external static IP) | Public internet |
| Redpanda | LoadBalancer (internal static IP) | VPC-internal only |
| Provisioning | ClusterIP | Internal only |
| Grafana | ClusterIP | Internal (port-forward for access) |

### Static IP Preservation

Static IPs are owned by the foundation layer. Destroying and recreating the cluster does not affect them. The deploy task reads IPs from foundation Terraform state and injects them via `--set` at Helm deploy time.

### CIDR Planning

| Subnet | CIDR | Purpose |
|--------|------|---------|
| WS cluster nodes | 10.0.0.0/20 | ws-gateway, ws-server, redpanda, etc. |
| WS pods | 10.1.0.0/16 | Pod IPs |
| WS services | 10.2.0.0/20 | Service IPs |

---

## Deployment Procedure

### One-time prerequisites

```bash
# Authenticate
gcloud auth login
gcloud config set project odin-9e902
gcloud auth configure-docker us-central1-docker.pkg.dev

# Create Artifact Registry (if not exists)
gcloud artifacts repositories create odin-ws \
  --project=odin-9e902 --location=us-central1 --repository-format=docker
```

### Full setup (foundation + cluster + build + deploy + migrate)

```bash
task k8s:setup ENV=dev
```

This runs in order:
1. `foundation:init` / `foundation:plan` / `foundation:apply` - VPC, static IPs
2. `tf:init` / `tf:plan` / `tf:apply` - GKE cluster
3. `connect` - kubectl credentials
4. `build` - Docker build + push
5. `deploy` - Helm install with static IPs injected
6. `status` - verify pods

### Post-setup

```bash
# Create Kafka topics
task k8s:provision:create ENV=dev

# Verify
task k8s:status ENV=dev
task k8s:external-ips ENV=dev
```

### Day-to-day operations

```bash
# Redeploy (code changes)
task k8s:build ENV=dev && task k8s:deploy ENV=dev

# Check status
task k8s:status ENV=dev

# View IPs
task k8s:external-ips ENV=dev

# Access Grafana
task k8s:port-forward:grafana ENV=dev
```

---

## Verification Checklist

- [ ] `task k8s:status ENV=dev` - all pods Running
- [ ] `task k8s:external-ips ENV=dev` - shows gateway public IP + redpanda internal IP
- [ ] `curl http://<GATEWAY_IP>:443/health` - gateway responds
- [ ] Provisioning and Grafana have NO external IP (ClusterIP only)
- [ ] Cluster destroy + recreate preserves static IPs (foundation intact)

---

## Files

| Action | File |
|--------|------|
| CREATE | `deployments/terraform/modules/foundation/{main,variables,outputs}.tf` |
| CREATE | `deployments/terraform/environments/standard/dev-foundation/{main,variables,outputs,versions}.tf` |
| CREATE | `deployments/terraform/environments/standard/dev-foundation/terraform.tfvars` |
| MODIFY | `deployments/terraform/modules/gke-standard-cluster/{main,variables,outputs}.tf` |
| MODIFY | `deployments/terraform/environments/standard/dev/{main,outputs}.tf` |
| MODIFY | `deployments/helm/odin/charts/ws-gateway/{templates/service.yaml,values.yaml}` |
| MODIFY | `deployments/helm/odin/charts/redpanda/{templates/service-external.yaml,values.yaml}` |
| MODIFY | `deployments/helm/odin/values/standard/dev.yaml` |
| MODIFY | `taskfiles/k8s.yml` |
