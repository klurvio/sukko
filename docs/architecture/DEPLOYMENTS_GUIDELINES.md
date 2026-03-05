# Deployments Guidelines

Reference for adding and maintaining deployment configurations in the Sukko WebSocket project.

## Directory Structure

```
deployments/
├── helm/sukko/                    # Helm chart (K8s deployments)
│   ├── Chart.yaml                # Chart metadata + dependencies
│   ├── values.yaml               # Base/default values
│   ├── values/
│   │   ├── local.yaml            # Kind cluster (local dev)
│   │   └── standard/
│   │       ├── dev.yaml          # GKE dev environment
│   │       ├── stg.yaml          # GKE staging environment
│   │       └── prod.yaml         # GKE production environment
│   ├── charts/                   # Subcharts (one per service)
│   │   ├── ws-gateway/
│   │   ├── ws-server/
│   │   ├── provisioning/
│   │   ├── postgresql/
│   │   ├── redpanda/
│   │   ├── nats/
│   │   ├── valkey/
│   │   └── monitoring/
│   └── templates/                # Parent chart templates
│       ├── kernel-tuning-daemonset.yaml
│       └── provisioning-db-secret.yaml
├── terraform/                    # Infrastructure as Code
│   ├── environments/standard/
│   │   ├── dev/
│   │   ├── stg/
│   │   └── prod/
│   └── modules/                  # Reusable TF modules
│       ├── gke-standard-cluster/
│       ├── vpc/
│       └── artifact-registry/
├── k8s/local/                    # Kind cluster config
│   └── kind-config.yaml
├── wsloadtest/                   # GCE VM scripts for load testing
└── wspublisher/                  # GCE VM scripts for publisher
```

## Environment Naming

| Environment | Short | Namespace | Cluster Name | Helm Values |
|-------------|-------|-----------|--------------|-------------|
| Local (Kind) | local | `sukko-local` | `sukko-local` | `values/local.yaml` |
| Development | dev | `sukko-dev` | `sukko-dev` | `values/standard/dev.yaml` |
| Staging | stg | `sukko-stg` | `sukko-stg` | `values/standard/stg.yaml` |
| Production | prod | `sukko-prod` | `sukko-prod` | `values/standard/prod.yaml` |

## Helm Values Hierarchy

Values are merged in order (later overrides earlier):

```
1. charts/{subchart}/values.yaml   # Subchart defaults
2. values.yaml                      # Parent chart base values
3. values/{env}.yaml                # Environment overrides
4. --set flags                      # Command-line overrides
```

### Example Flow

For `task k8s:deploy ENV=dev`:
```
ws-server/values.yaml     →  Base defaults (replicaCount: 1)
values.yaml               →  Override (replicaCount: 2)
values/standard/dev.yaml  →  Final (replicaCount: 2, logLevel: info)
```

---

## Adding a New Helm Value

### Step 1: Add to Subchart values.yaml

```yaml
# charts/ws-server/values.yaml
config:
  myNewSetting: "default"  # Add with sensible default
```

### Step 2: Use in Template

```yaml
# charts/ws-server/templates/configmap.yaml
data:
  MY_NEW_SETTING: "{{ .Values.config.myNewSetting }}"
```

### Step 3: Override Per Environment (if needed)

```yaml
# values/local.yaml
ws-server:
  config:
    myNewSetting: "local-value"

# values/standard/dev.yaml
ws-server:
  config:
    myNewSetting: "dev-value"
```

---

## Adding a New Subchart

### Step 1: Create Chart Structure

```bash
mkdir -p deployments/helm/sukko/charts/my-service/templates
```

```yaml
# charts/my-service/Chart.yaml
apiVersion: v2
name: my-service
description: My new service
type: application
version: "1.0.0"
appVersion: "1.0.0"
```

### Step 2: Create values.yaml

```yaml
# charts/my-service/values.yaml
enabled: true
replicaCount: 1

image:
  repository: my-service
  tag: latest
  pullPolicy: IfNotPresent

config:
  logLevel: info
  logFormat: json

resources:
  requests:
    cpu: "100m"
    memory: "128Mi"
  limits:
    cpu: "500m"
    memory: "256Mi"

service:
  type: ClusterIP
  port: 8080
```

### Step 3: Create Templates

```yaml
# charts/my-service/templates/deployment.yaml
{{- if .Values.enabled }}
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "my-service.fullname" . }}
  labels:
    {{- include "my-service.labels" . | nindent 4 }}
spec:
  replicas: {{ .Values.replicaCount }}
  selector:
    matchLabels:
      {{- include "my-service.selectorLabels" . | nindent 6 }}
  template:
    metadata:
      labels:
        {{- include "my-service.selectorLabels" . | nindent 8 }}
    spec:
      containers:
        - name: {{ .Chart.Name }}
          image: "{{- if .Values.global.imageRegistry }}{{ .Values.global.imageRegistry }}/{{- end }}{{ .Values.image.repository }}:{{ .Values.image.tag }}"
          imagePullPolicy: {{ .Values.image.pullPolicy }}
          ports:
            - containerPort: {{ .Values.service.port }}
          resources:
            {{- toYaml .Values.resources | nindent 12 }}
{{- end }}
```

### Step 4: Add _helpers.tpl

```yaml
# charts/my-service/templates/_helpers.tpl
{{- define "my-service.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "my-service.fullname" -}}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "my-service.labels" -}}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
app.kubernetes.io/name: {{ include "my-service.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "my-service.selectorLabels" -}}
app.kubernetes.io/name: {{ include "my-service.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
```

### Step 5: Register in Parent Chart

```yaml
# Chart.yaml
dependencies:
  # ... existing dependencies ...
  - name: my-service
    version: "1.0.0"
    condition: my-service.enabled
```

### Step 6: Add to Environment Values

```yaml
# values/local.yaml
my-service:
  enabled: true
  replicaCount: 1
  config:
    logLevel: debug

# values/standard/dev.yaml
my-service:
  enabled: true
  replicaCount: 2
```

### Step 7: Update Dependencies

```bash
cd deployments/helm/sukko
helm dependency update
```

---

## Modifying Environment Values

### Local Development (Kind)

File: `values/local.yaml`

```yaml
# Key principles:
# - 1 replica per service (M1 MacBook constraint)
# - Minimal resources
# - Debug logging
# - No auth for simplicity
# - NodePort services for port-forwarding

ws-server:
  replicaCount: 1
  config:
    logLevel: debug
    logFormat: pretty
  resources:
    requests:
      cpu: "100m"      # Low for M1
      memory: "128Mi"
```

### Remote GKE (dev/stg/prod)

Files: `values/standard/{dev,stg,prod}.yaml`

```yaml
# Key differences by environment:

# dev.yaml - Development
ws-server:
  replicaCount: 2
  config:
    logLevel: info
    environment: dev
  autoscaling:
    enabled: false  # Fixed replicas for cost

# stg.yaml - Staging (production-like)
ws-server:
  replicaCount: 2
  config:
    logLevel: info
    environment: stg
  autoscaling:
    enabled: false

# prod.yaml - Production
ws-server:
  replicaCount: 3
  config:
    logLevel: warn   # Less noise
    environment: prod
  autoscaling:
    enabled: true
    minReplicas: 2
    maxReplicas: 8
```

---

## Terraform Configuration

### Adding a New Environment Variable

1. Add to `variables.tf`:
```hcl
# modules/gke-standard-cluster/variables.tf
variable "my_new_setting" {
  description = "Description of the setting"
  type        = string
  default     = "default-value"
}
```

2. Use in `main.tf`:
```hcl
# modules/gke-standard-cluster/main.tf
resource "google_container_cluster" "primary" {
  # ...
  my_setting = var.my_new_setting
}
```

3. Set per environment in `terraform.tfvars`:
```hcl
# environments/standard/dev/terraform.tfvars
my_new_setting = "dev-value"

# environments/standard/prod/terraform.tfvars
my_new_setting = "prod-value"
```

### Environment tfvars Template

```hcl
# =============================================================================
# GKE Standard - {Env} Environment
# =============================================================================

# Project Configuration
project_id = "your-project-id"

# Region & Zone
region = "us-central1"
zone   = "us-central1-a"

# Cluster Configuration
cluster_name = "sukko-{env}"
environment  = "{env}"
namespace    = "sukko-{env}"
network_name = "sukko-{env}-vpc"

# Node Pool Configuration
node_machine_type = "e2-standard-4"
node_disk_size_gb = 50

# Spot VMs
use_spot_vms     = true
taint_spot_nodes = false

# Scaling
node_count         = 1          # dev: 1, stg: 2, prod: 2
enable_autoscaling = false      # prod: true

# Features
enable_network_policy           = true
enable_vertical_pod_autoscaling = true
release_channel                 = "REGULAR"  # prod: "STABLE"
deletion_protection             = false      # prod: true
```

---

## Topic Configuration

Topics are defined in environment values files:

```yaml
# values/standard/dev.yaml
redpanda:
  topics:
    # Regular topics (publisher → external service)
    - name: sukko.main.trade
      partitions: 1
      replicationFactor: 1
    # Refined topics (external service → ws-server)
    - name: sukko.main.trade.refined
      partitions: 1
      replicationFactor: 1
```

### Topic Naming Convention

```
sukko.{namespace}.{category}[.refined]
```

| Component | Description | Examples |
|-----------|-------------|----------|
| `sukko` | Fixed prefix | - |
| `namespace` | Topic namespace | `local`, `main`, `stg` |
| `category` | Data category | `trade`, `liquidity`, `balances` |
| `.refined` | Suffix for processed topics | ws-server consumes these |

### Namespace Mapping

| Environment | Topic Namespace | Example Topic |
|-------------|-----------------|---------------|
| local | `local` | `sukko.local.trade.refined` |
| dev | `main` | `sukko.main.trade.refined` |
| stg | `stg` | `sukko.stg.trade.refined` |
| prod | `main` | `sukko.main.trade.refined` |

Note: `dev` uses `main` namespace to test against production topics.

---

## Best Practices

### DO

```yaml
# Use consistent naming
namespace: sukko-{env}
cluster_name: sukko-{env}

# Set sensible defaults in subchart values.yaml
replicaCount: 1  # Override in environment files

# Use global values for shared config
global:
  imageRegistry: "us-central1-docker.pkg.dev/project/sukko"

# Document non-obvious settings
config:
  # Slow client threshold: disconnect after N consecutive failed sends
  # Range: 1-10, Industry standard: 2-5
  slowClientMaxAttempts: 3
```

### DON'T

```yaml
# Don't hardcode environment-specific values in base
namespace: sukko-dev  # BAD - should be in dev.yaml only

# Don't duplicate across environments
# Instead, set in values.yaml and override only what differs

# Don't use inconsistent naming
cluster: sukko-development  # BAD - use sukko-dev
namespace: sukko-std-dev       # BAD - use sukko-dev
```

---

## Resource Guidelines

### Local (Kind on M1)

```yaml
resources:
  requests:
    cpu: "100m"
    memory: "64Mi"
  limits:
    cpu: "250m"
    memory: "128Mi"
```

### Remote (GKE)

```yaml
# Development
resources:
  requests:
    cpu: "250m"
    memory: "256Mi"
  limits:
    cpu: "500m"
    memory: "512Mi"

# Production
resources:
  requests:
    cpu: "500m"
    memory: "512Mi"
  limits:
    cpu: "1"
    memory: "1Gi"
```

---

## Checklist for Changes

### Adding New Values

- [ ] Added to subchart `values.yaml` with default
- [ ] Used in template with `{{ .Values.path.to.value }}`
- [ ] Overridden in environment files as needed
- [ ] Documented if non-obvious

### Adding New Subchart

- [ ] Created `Chart.yaml`, `values.yaml`, `templates/`
- [ ] Added `_helpers.tpl` with standard labels
- [ ] Registered in parent `Chart.yaml` dependencies
- [ ] Added to all environment values files
- [ ] Ran `helm dependency update`
- [ ] Tested with `helm template` and `helm lint`

### Modifying Terraform

- [ ] Added variable to `variables.tf`
- [ ] Used in `main.tf` or `outputs.tf`
- [ ] Set in all `terraform.tfvars` files
- [ ] Tested with `terraform plan`

### Environment Changes

- [ ] Used short env names (`dev`, `stg`, `prod`)
- [ ] Consistent namespace pattern (`sukko-{env}`)
- [ ] Consistent cluster pattern (`sukko-{env}`)
- [ ] Updated all affected environments

---

## Quick Reference

### Helm Commands

```bash
# Validate chart
helm lint deployments/helm/sukko

# Preview rendered templates
helm template sukko deployments/helm/sukko -f deployments/helm/sukko/values/local.yaml

# Update dependencies
helm dependency update deployments/helm/sukko

# Deploy
helm upgrade --install sukko deployments/helm/sukko \
  -f deployments/helm/sukko/values/standard/dev.yaml \
  -n sukko-dev --create-namespace
```

### Terraform Commands

```bash
# Initialize
terraform -chdir=deployments/terraform/environments/standard/dev init

# Plan
terraform -chdir=deployments/terraform/environments/standard/dev plan

# Apply
terraform -chdir=deployments/terraform/environments/standard/dev apply
```

### Task Commands

```bash
# Local
task local:deploy

# Remote
task k8s:deploy ENV=dev
task k8s:tf:plan ENV=dev
task k8s:tf:apply ENV=dev
```
