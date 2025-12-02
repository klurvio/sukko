# Terraform Integration Plan for GCP Environments

## Overview

Integrate Terraform to manage GCP infrastructure for develop, staging, and production environments. Terraform will manage GKE clusters, VPC, networking, and Artifact Registry. Helm will continue to deploy applications to the clusters.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| **Scope** | Infrastructure only | Terraform manages GCP resources; Helm deploys apps |
| **Cluster Strategy** | Prod separate only | Dev+Staging share cluster (cost savings), Production isolated |
| **State Backend** | Terraform Cloud | Team collaboration, locking, audit trail |
| **Extra Services** | Basic GKE (flexible) | Start minimal, add managed services as needed |

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        Terraform Cloud                          │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐          │
│  │ odin-dev-stg │  │  odin-prod   │  │   shared     │          │
│  │  workspace   │  │  workspace   │  │  workspace   │          │
│  └──────────────┘  └──────────────┘  └──────────────┘          │
└─────────────────────────────────────────────────────────────────┘
         │                    │                   │
         ▼                    ▼                   ▼
┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐
│  GKE Autopilot  │  │  GKE Autopilot  │  │ Artifact Registry│
│   (dev + stg)   │  │  (production)   │  │    (shared)      │
│                 │  │                 │  │                 │
│ ┌─────────────┐ │  │ ┌─────────────┐ │  │  odin/ws-server │
│ │ ns: odin-dev│ │  │ │ns: odin-prod│ │  │  odin/publisher │
│ └─────────────┘ │  │ └─────────────┘ │  │                 │
│ ┌─────────────┐ │  │                 │  └─────────────────┘
│ │ns: odin-stg │ │  │                 │
│ └─────────────┘ │  │                 │
└─────────────────┘  └─────────────────┘
```

## Directory Structure

```
deployments/
├── k8s/
│   └── helm/odin/              # Existing Helm charts (unchanged)
│       ├── values-local.yaml
│       ├── values-develop.yaml
│       ├── values-staging.yaml
│       └── values-production.yaml
└── terraform/
    ├── modules/                 # Reusable Terraform modules
    │   ├── gke-cluster/        # GKE Autopilot cluster
    │   ├── vpc/                # VPC and networking
    │   ├── artifact-registry/  # Container registry
    │   └── iam/                # Service accounts and IAM
    ├── environments/
    │   ├── shared/             # Shared resources (Artifact Registry)
    │   │   ├── main.tf
    │   │   ├── variables.tf
    │   │   ├── outputs.tf
    │   │   └── terraform.tf    # Backend config for TF Cloud
    │   ├── dev-staging/        # Dev + Staging cluster
    │   │   ├── main.tf
    │   │   ├── variables.tf
    │   │   ├── outputs.tf
    │   │   └── terraform.tf
    │   └── production/         # Production cluster
    │       ├── main.tf
    │       ├── variables.tf
    │       ├── outputs.tf
    │       └── terraform.tf
    └── .terraform-version      # tfenv version pinning
```

---

## Phase 1: Foundation Setup

### 1.1 Terraform Cloud Configuration

Create workspaces in Terraform Cloud:
- `odin-shared` - Artifact Registry, shared IAM
- `odin-dev-staging` - Dev/Staging GKE cluster
- `odin-production` - Production GKE cluster

Configure workspace variables:
```hcl
# Environment variables (sensitive)
GOOGLE_CREDENTIALS = <service-account-key-json>

# Terraform variables
gcp_project        = "odin-ws-server"
gcp_region         = "us-central1"
```

### 1.2 GCP Service Account

Create Terraform service account with roles:
- `roles/container.admin` - GKE management
- `roles/compute.networkAdmin` - VPC/firewall
- `roles/artifactregistry.admin` - Container registry
- `roles/iam.serviceAccountAdmin` - Service account management
- `roles/storage.admin` - GCS (if needed for state)

---

## Phase 2: Terraform Modules

### 2.1 VPC Module (`modules/vpc/`)

```hcl
# modules/vpc/main.tf
resource "google_compute_network" "vpc" {
  name                    = var.network_name
  auto_create_subnetworks = false
  project                 = var.project_id
}

resource "google_compute_subnetwork" "subnet" {
  name          = "${var.network_name}-subnet"
  ip_cidr_range = var.subnet_cidr
  region        = var.region
  network       = google_compute_network.vpc.id

  secondary_ip_range {
    range_name    = "pods"
    ip_cidr_range = var.pods_cidr
  }

  secondary_ip_range {
    range_name    = "services"
    ip_cidr_range = var.services_cidr
  }
}

# Firewall rules
resource "google_compute_firewall" "ws_ingress" {
  name    = "${var.network_name}-ws-ingress"
  network = google_compute_network.vpc.name

  allow {
    protocol = "tcp"
    ports    = ["3001", "443"]  # WebSocket ports
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["ws-server"]
}
```

### 2.2 GKE Cluster Module (`modules/gke-cluster/`)

```hcl
# modules/gke-cluster/main.tf
resource "google_container_cluster" "autopilot" {
  name     = var.cluster_name
  location = var.region
  project  = var.project_id

  # Enable Autopilot
  enable_autopilot = true

  # Network configuration
  network    = var.network_id
  subnetwork = var.subnet_id

  ip_allocation_policy {
    cluster_secondary_range_name  = "pods"
    services_secondary_range_name = "services"
  }

  # Private cluster (optional, recommended for prod)
  private_cluster_config {
    enable_private_nodes    = var.enable_private_nodes
    enable_private_endpoint = false
    master_ipv4_cidr_block  = var.master_cidr
  }

  # Maintenance window
  maintenance_policy {
    recurring_window {
      start_time = "2024-01-01T04:00:00Z"
      end_time   = "2024-01-01T08:00:00Z"
      recurrence = "FREQ=WEEKLY;BYDAY=SU"
    }
  }

  # Release channel
  release_channel {
    channel = var.release_channel  # STABLE for prod, REGULAR for dev
  }
}

# Output kubeconfig for Helm
output "kubeconfig" {
  value = {
    host                   = "https://${google_container_cluster.autopilot.endpoint}"
    cluster_ca_certificate = google_container_cluster.autopilot.master_auth[0].cluster_ca_certificate
  }
  sensitive = true
}
```

### 2.3 Artifact Registry Module (`modules/artifact-registry/`)

```hcl
# modules/artifact-registry/main.tf
resource "google_artifact_registry_repository" "odin" {
  location      = var.region
  repository_id = "odin"
  description   = "Container images for Odin WebSocket infrastructure"
  format        = "DOCKER"
  project       = var.project_id
}

# IAM for GKE to pull images
resource "google_artifact_registry_repository_iam_member" "gke_reader" {
  for_each   = toset(var.gke_service_accounts)

  project    = var.project_id
  location   = var.region
  repository = google_artifact_registry_repository.odin.name
  role       = "roles/artifactregistry.reader"
  member     = "serviceAccount:${each.value}"
}
```

---

## Phase 3: Environment Configurations

### 3.1 Shared Environment (`environments/shared/`)

```hcl
# environments/shared/main.tf
terraform {
  cloud {
    organization = "toniq"
    workspaces {
      name = "odin-shared"
    }
  }
}

module "artifact_registry" {
  source     = "../../modules/artifact-registry"
  project_id = var.gcp_project
  region     = var.gcp_region

  gke_service_accounts = [
    # Will be populated after clusters are created
  ]
}

output "registry_url" {
  value = "${var.gcp_region}-docker.pkg.dev/${var.gcp_project}/odin"
}
```

### 3.2 Dev-Staging Environment (`environments/dev-staging/`)

```hcl
# environments/dev-staging/main.tf
terraform {
  cloud {
    organization = "toniq"
    workspaces {
      name = "odin-dev-staging"
    }
  }
}

module "vpc" {
  source       = "../../modules/vpc"
  project_id   = var.gcp_project
  region       = var.gcp_region
  network_name = "odin-dev-staging"
  subnet_cidr  = "10.0.0.0/20"
  pods_cidr    = "10.4.0.0/14"
  services_cidr = "10.8.0.0/20"
}

module "gke" {
  source       = "../../modules/gke-cluster"
  project_id   = var.gcp_project
  region       = var.gcp_region
  cluster_name = "odin-dev-staging"

  network_id   = module.vpc.network_id
  subnet_id    = module.vpc.subnet_id

  release_channel      = "REGULAR"
  enable_private_nodes = false  # Public for dev/staging
}

# Create namespaces for dev and staging
resource "kubernetes_namespace" "dev" {
  metadata {
    name = "odin-dev"
    labels = {
      environment = "develop"
    }
  }
}

resource "kubernetes_namespace" "staging" {
  metadata {
    name = "odin-staging"
    labels = {
      environment = "staging"
    }
  }
}

# Outputs for Helm deployment
output "cluster_endpoint" {
  value = module.gke.kubeconfig.host
}

output "cluster_ca_certificate" {
  value     = module.gke.kubeconfig.cluster_ca_certificate
  sensitive = true
}
```

### 3.3 Production Environment (`environments/production/`)

```hcl
# environments/production/main.tf
terraform {
  cloud {
    organization = "toniq"
    workspaces {
      name = "odin-production"
    }
  }
}

module "vpc" {
  source       = "../../modules/vpc"
  project_id   = var.gcp_project
  region       = var.gcp_region
  network_name = "odin-production"
  subnet_cidr  = "10.16.0.0/20"
  pods_cidr    = "10.20.0.0/14"
  services_cidr = "10.24.0.0/20"
}

module "gke" {
  source       = "../../modules/gke-cluster"
  project_id   = var.gcp_project
  region       = var.gcp_region
  cluster_name = "odin-production"

  network_id   = module.vpc.network_id
  subnet_id    = module.vpc.subnet_id

  release_channel      = "STABLE"       # More stable for prod
  enable_private_nodes = true           # Private cluster for security
  master_cidr          = "172.16.0.0/28"
}

resource "kubernetes_namespace" "prod" {
  metadata {
    name = "odin-prod"
    labels = {
      environment = "production"
    }
  }
}
```

---

## Phase 4: CI/CD Integration

### 4.1 Taskfile Updates

Add Terraform tasks to existing Taskfile structure:

```yaml
# taskfiles/terraform/Taskfile.yml
version: '3'

vars:
  TF_DIR: '{{.ROOT_DIR}}/deployments/terraform'

tasks:
  init:
    desc: Initialize Terraform for an environment
    cmds:
      - terraform -chdir={{.TF_DIR}}/environments/{{.ENV}} init
    requires:
      vars: [ENV]

  plan:
    desc: Plan Terraform changes
    cmds:
      - terraform -chdir={{.TF_DIR}}/environments/{{.ENV}} plan -out=tfplan
    requires:
      vars: [ENV]

  apply:
    desc: Apply Terraform changes
    cmds:
      - terraform -chdir={{.TF_DIR}}/environments/{{.ENV}} apply tfplan
    requires:
      vars: [ENV]

  destroy:
    desc: Destroy Terraform resources (DANGEROUS)
    cmds:
      - terraform -chdir={{.TF_DIR}}/environments/{{.ENV}} destroy
    requires:
      vars: [ENV]
    prompt: "Are you sure you want to destroy {{.ENV}}?"

  # Convenience tasks
  dev:plan:
    desc: Plan dev-staging infrastructure
    cmds:
      - task: plan
        vars: { ENV: dev-staging }

  dev:apply:
    desc: Apply dev-staging infrastructure
    cmds:
      - task: apply
        vars: { ENV: dev-staging }

  prod:plan:
    desc: Plan production infrastructure
    cmds:
      - task: plan
        vars: { ENV: production }

  prod:apply:
    desc: Apply production infrastructure
    cmds:
      - task: apply
        vars: { ENV: production }
```

### 4.2 Helm Integration

Update Helm deployment to use Terraform outputs:

```yaml
# taskfiles/k8s/Taskfile.yml
tasks:
  deploy:dev:
    desc: Deploy to develop environment
    cmds:
      - |
        # Get cluster credentials from Terraform output
        CLUSTER=$(cd {{.TF_DIR}}/environments/dev-staging && terraform output -raw cluster_name)
        gcloud container clusters get-credentials $CLUSTER --region {{.GCP_REGION}}
      - helm upgrade --install odin {{.HELM_DIR}}
          -f {{.HELM_DIR}}/values-develop.yaml
          -n odin-dev
```

---

## Phase 5: Implementation Order

### Step 1: Setup (Day 1)
1. Create Terraform Cloud organization and workspaces
2. Create GCP service account for Terraform
3. Configure workspace variables and credentials
4. Create directory structure

### Step 2: Modules (Day 1-2)
1. Implement VPC module
2. Implement GKE cluster module
3. Implement Artifact Registry module
4. Test modules locally

### Step 3: Shared Environment (Day 2)
1. Deploy Artifact Registry
2. Configure IAM policies
3. Test image push/pull

### Step 4: Dev-Staging Cluster (Day 2-3)
1. Deploy VPC and GKE cluster
2. Create namespaces
3. Test Helm deployment to dev namespace
4. Test Helm deployment to staging namespace

### Step 5: Production Cluster (Day 3)
1. Deploy production VPC and GKE
2. Configure private cluster settings
3. Test Helm deployment to production

### Step 6: Integration (Day 4)
1. Update Taskfiles with Terraform commands
2. Document deployment workflow
3. Test full CI/CD pipeline

---

## Files to Create

| File | Purpose |
|------|---------|
| `deployments/terraform/.terraform-version` | Pin Terraform version (1.6+) |
| `deployments/terraform/modules/vpc/main.tf` | VPC module |
| `deployments/terraform/modules/vpc/variables.tf` | VPC inputs |
| `deployments/terraform/modules/vpc/outputs.tf` | VPC outputs |
| `deployments/terraform/modules/gke-cluster/main.tf` | GKE module |
| `deployments/terraform/modules/gke-cluster/variables.tf` | GKE inputs |
| `deployments/terraform/modules/gke-cluster/outputs.tf` | GKE outputs |
| `deployments/terraform/modules/artifact-registry/main.tf` | Registry module |
| `deployments/terraform/environments/shared/main.tf` | Shared resources |
| `deployments/terraform/environments/dev-staging/main.tf` | Dev/Staging cluster |
| `deployments/terraform/environments/production/main.tf` | Production cluster |
| `taskfiles/terraform/Taskfile.yml` | Terraform task automation |

---

## Security Considerations

1. **State Security**: Terraform Cloud encrypts state at rest
2. **Credentials**: Use Terraform Cloud variable sets for GCP credentials
3. **Private Clusters**: Production uses private GKE nodes
4. **IAM Least Privilege**: Terraform SA has minimal required permissions
5. **Network Isolation**: Separate VPCs for prod vs dev/staging

---

## Cost Estimates

| Resource | Dev-Staging | Production | Notes |
|----------|-------------|------------|-------|
| GKE Autopilot | ~$70/mo base | ~$70/mo base | Pay per pod usage |
| VPC | Free | Free | Egress charges apply |
| Artifact Registry | ~$5/mo | ~$5/mo | Storage + egress |
| Load Balancer | ~$20/mo | ~$20/mo | Per environment |
| **Total** | ~$95/mo | ~$95/mo | Plus compute usage |
