# Plan: Integrate odin-cdc into odin-ws K8s Cluster

**Date:** 2026-02-12
**Status:** Planning
**Branch:** `feat/cdc-pipeline-k8s-integration`

---

## Context

The odin-cdc pipeline currently runs in its own separate GKE cluster (`odin-cdc-cluster` in project `trim-array-480700-j7`). It captures MySQL binlog changes from a CloudSQL read replica via Debezium, transforms them through a Node.js consumer, and publishes refined events to Redpanda. This is the production data source that replaces `wspublisher` (synthetic test data).

**Goal:** Consolidate odin-cdc into the odin-ws cluster (`odin-ws-dev` in project `odin-9e902`) so all services live in one cluster, reducing operational overhead and cost.

**Data flow after integration:**
```
CloudSQL Read Replica -> Debezium -> Redpanda-CDC -> odin-cdc consumer -> Redpanda-WS (existing) -> ws-server -> WebSocket clients
```

Two Redpanda instances are required:
- **Redpanda-CDC**: Debezium internal topics + raw CDC events
- **Redpanda-WS** (existing): Refined events for ws-server consumption

---

## Phase 0: Move odin-cdc Source Code into odin-ws

The odin-cdc consumer code currently lives in a separate repository. As part of this consolidation, we will move it into the odin-ws monorepo rather than using a git submodule.

### Why monorepo over git submodule

- **Atomic cross-service changes** -- The open question about adding `TARGET_KAFKA_BROKER` support to the consumer is a perfect example. With a submodule, this requires coordinated PRs across two repos. In-repo it's one commit that touches both the consumer code and its Helm chart.
- **Shared tooling** -- Taskfile, Helm charts, Terraform, CI (`cloudbuild.yaml`) all live in odin-ws. A submodule would need its own build plumbing or awkward cross-repo path references.
- **Consistent deployment lifecycle** -- `task k8s:deploy` deploys everything. A submodule adds a `git submodule update` step and version drift risk (detached HEAD, forgotten updates).
- **No independent release cadence** -- odin-cdc doesn't have its own team or consumers outside odin-ws. It exists solely to feed data into ws-server. There's no benefit to repo-level isolation.
- **Small footprint** -- The consumer is a lightweight Node.js app, not a large independent product.

### Placement: `cdc/` at repo root

The odin-ws repo structure separates services by language/runtime:
```
ws/              # Go module -- gateway, server, provisioning (shared go.mod)
wsloadtest/      # Go standalone service
wspublisher/     # Go standalone service
cdc/             # Node.js -- CDC consumer (new)
```

`cdc/` lives at the top level because:
- It's **Node.js** (JavaScript), not Go -- it cannot share `ws/go.mod` and doesn't belong inside the `ws/` directory
- Follows the same pattern as `wsloadtest/` and `wspublisher/` -- standalone services get their own top-level directory
- Short, clean name matching the existing convention

The directory will contain the consumer source, `package.json`, `Dockerfile`, and any CDC-specific config. The old odin-cdc repository can be archived once migration is verified.

### Migration steps

1. Copy source files from odin-cdc repo into `cdc/`
2. Verify `Dockerfile` builds correctly in the new location
3. Update `cloudbuild.yaml` if needed to build the cdc image from the new path
4. Update Helm chart image reference to point to the new build artifact
5. Archive the old odin-cdc repository

---

## Phase 1: Terraform -- Cloud NAT Static IP for CloudSQL Egress

Debezium needs to connect to an external CloudSQL read replica. CloudSQL uses IP whitelisting for access control. The cluster's Cloud NAT currently uses `AUTO_ONLY` (ephemeral IPs), so we need a static egress IP.

### 1.1 Add static NAT IP to foundation module

**`deployments/terraform/modules/foundation/main.tf`** -- Add after existing static IPs:
```hcl
resource "google_compute_address" "nat_egress" {
  name         = "${var.vpc_name}-nat-egress"
  region       = var.region
  address_type = "EXTERNAL"
  project      = var.project_id
}
```

**`deployments/terraform/modules/foundation/outputs.tf`** -- Add:
```hcl
output "nat_egress_ip" {
  description = "Static external IP for Cloud NAT egress (whitelist on CloudSQL)"
  value       = google_compute_address.nat_egress.address
}

output "nat_egress_ip_self_link" {
  description = "Self link of NAT egress IP (for Cloud NAT resource binding)"
  value       = google_compute_address.nat_egress.self_link
}
```

**`deployments/terraform/environments/standard/dev-foundation/outputs.tf`** -- Pass through:
```hcl
output "nat_egress_ip" {
  value = module.foundation.nat_egress_ip
}

output "nat_egress_ip_self_link" {
  value = module.foundation.nat_egress_ip_self_link
}
```

### 1.2 Update Cloud NAT to use static IP

**`deployments/terraform/modules/gke-standard-cluster/variables.tf`** -- Add:
```hcl
variable "nat_static_ip_self_link" {
  description = "Static IP self_link for Cloud NAT (empty = AUTO_ONLY)"
  type        = string
  default     = ""
}
```

**`deployments/terraform/modules/gke-standard-cluster/main.tf`** -- Modify `google_compute_router_nat`:
```hcl
resource "google_compute_router_nat" "nat" {
  name                               = "${var.cluster_name}-nat"
  router                             = google_compute_router.router.name
  region                             = var.region
  project                            = var.project_id
  nat_ip_allocate_option             = var.nat_static_ip_self_link != "" ? "MANUAL_ONLY" : "AUTO_ONLY"
  nat_ips                            = var.nat_static_ip_self_link != "" ? [var.nat_static_ip_self_link] : []
  source_subnetwork_ip_ranges_to_nat = "LIST_OF_SUBNETWORKS"
  # ... rest unchanged
}
```

**`deployments/terraform/environments/standard/dev/main.tf`** -- Pass foundation output:
```hcl
module "gke" {
  # ... existing args ...
  nat_static_ip_self_link = data.terraform_remote_state.foundation.outputs.nat_egress_ip_self_link
}
```

---

## Phase 1.5: Terraform -- VPC Peering for odin-api Cross-VPC Access

odin-api lives in a separate VPC within the same GCP project. It needs to publish events to Redpanda-WS in the odin-ws cluster VPC. VPC Peering is the optimal approach: free (no egress charges for same-region peered traffic), traffic stays on Google's private backbone, and a single firewall rule scopes access down to the Redpanda port.

### 1.5.1 VPC Peering resource

**`deployments/terraform/modules/foundation/variables.tf`** -- Add:
```hcl
variable "peer_vpc_self_link" {
  description = "Self link of the VPC to peer with (e.g., odin-api VPC). Empty = no peering."
  type        = string
  default     = ""
}
```

**`deployments/terraform/modules/foundation/main.tf`** -- Add:
```hcl
resource "google_compute_network_peering" "ws_to_api" {
  count        = var.peer_vpc_self_link != "" ? 1 : 0
  name         = "${var.vpc_name}-to-api"
  network      = google_compute_network.vpc.self_link
  peer_network = var.peer_vpc_self_link
}
```

Note: The reverse peering (api VPC -> ws VPC) must also be created in the odin-api Terraform config. VPC peering is bidirectional but requires a resource on each side.

### 1.5.2 Firewall rule -- allow odin-api to Redpanda only

**`deployments/terraform/modules/foundation/main.tf`** -- Add:
```hcl
resource "google_compute_firewall" "allow_api_to_redpanda" {
  count   = var.peer_vpc_self_link != "" ? 1 : 0
  name    = "${var.vpc_name}-allow-api-to-redpanda"
  network = google_compute_network.vpc.self_link
  project = var.project_id

  allow {
    protocol = "tcp"
    ports    = ["30092"]  # Redpanda NodePort
  }

  source_ranges = [var.peer_vpc_subnet_cidr]  # odin-api subnet CIDR
  target_tags   = ["gke-node"]                # Only GKE nodes

  description = "Allow odin-api VPC to reach Redpanda-WS via NodePort"
}
```

**`deployments/terraform/modules/foundation/variables.tf`** -- Add:
```hcl
variable "peer_vpc_subnet_cidr" {
  description = "CIDR of the peer VPC subnet (for firewall source_ranges)"
  type        = string
  default     = ""
}
```

### 1.5.3 Expose Redpanda-WS via NodePort

The existing Redpanda-WS Helm chart may need a NodePort service (or the existing service updated) so odin-api can reach it via `<GKE-node-internal-IP>:<NodePort>`. The internal node IPs are routable across peered VPCs.

Alternatively, an **Internal Load Balancer** (ILB) can front the Redpanda service for a stable internal IP, avoiding node IP dependency:
```hcl
resource "google_compute_address" "redpanda_ilb" {
  count        = var.peer_vpc_self_link != "" ? 1 : 0
  name         = "${var.vpc_name}-redpanda-ilb"
  region       = var.region
  address_type = "INTERNAL"
  subnetwork   = google_compute_subnetwork.subnet.self_link
  project      = var.project_id
}
```

Then in the Redpanda Helm chart, add a `LoadBalancer` service with `cloud.google.com/load-balancer-type: "Internal"` annotation pointing to this static internal IP.

### 1.5.4 Environment config

**`deployments/terraform/environments/standard/dev-foundation/main.tf`** -- Pass peer VPC details:
```hcl
module "foundation" {
  # ... existing args ...
  peer_vpc_self_link  = "projects/odin-9e902/global/networks/<odin-api-vpc-name>"
  peer_vpc_subnet_cidr = "<odin-api-subnet-cidr>"  # e.g., "10.x.x.x/xx"
}
```

### Open questions for this phase

- What is the odin-api VPC name and subnet CIDR?
- Does odin-api's Terraform config live in a separate state? The reverse peering resource needs to be added there.
- NodePort vs Internal Load Balancer: NodePort is simpler but ties odin-api to node IPs. ILB gives a stable IP but adds ~$0.025/hr (~$18/mo). For dev, NodePort is likely fine.

---

## Phase 2: Helm Subcharts

Add 3 new subcharts following the existing pattern (e.g., `redpanda/`, `provisioning/`).

### 2.1 `redpanda-cdc` subchart

**Directory:** `deployments/helm/odin/charts/redpanda-cdc/`

Clone from existing `redpanda/` subchart with these differences:
- Chart name: `redpanda-cdc`
- All template helpers use `redpanda-cdc.` prefix
- Default `kafkaPort: 9093` (avoids collision with WS Redpanda's 9092)
- `enabled: false` by default
- No external access needed (internal-only)
- No topics init job (Debezium creates its own topics)
- Smaller default resources (less traffic than WS Redpanda)

**Files to create** (based on existing `charts/redpanda/` templates):
- `Chart.yaml`
- `values.yaml`
- `templates/_helpers.tpl` (s/redpanda/redpanda-cdc/g)
- `templates/statefulset.yaml` (same pattern, port 9093)
- `templates/service.yaml` (ClusterIP only)

**Result:** Service DNS `odin-redpanda-cdc:9093`

### 2.2 `debezium` subchart

**Directory:** `deployments/helm/odin/charts/debezium/`

**Files to create:**
- `Chart.yaml` (appVersion: "2.7.3")
- `values.yaml`
- `templates/_helpers.tpl`
- `templates/deployment.yaml`
- `templates/service.yaml` (ClusterIP, port 8083)

**Key deployment spec** (derived from `odin-cdc/k8s/odin-cdc.yaml` lines 13-64):
- Image: `debezium/connect:2.7.3.Final`
- Port: 8083
- Init container: wait for `odin-redpanda-cdc:9093`
- Env vars:
  - `BOOTSTRAP_SERVERS`: `{{ .Release.Name }}-redpanda-cdc:9093`
  - `GROUP_ID`: `odin-cdc-connect`
  - `CONFIG_STORAGE_TOPIC`: `connect_configs`
  - `OFFSET_STORAGE_TOPIC`: `connect_offsets`
  - `STATUS_STORAGE_TOPIC`: `connect_status`
  - `*_REPLICATION_FACTOR`: `1`
- Resources: 25m/300m CPU, 512Mi/1Gi RAM (from existing manifest)
- Readiness probe: `GET / :8083` (initialDelaySeconds: 30)
- Liveness probe: `GET / :8083` (initialDelaySeconds: 60)

### 2.3 `cdc-consumer` subchart

**Directory:** `deployments/helm/odin/charts/cdc-consumer/`

**Files to create:**
- `Chart.yaml`
- `values.yaml`
- `templates/_helpers.tpl`
- `templates/deployment.yaml`

**Key deployment spec** (derived from `odin-cdc/k8s/odin-cdc.yaml` lines 80-117):
- Image: `gcr.io/trim-array-480700-j7/odin-cdc:latest` (external registry initially)
- `imagePullPolicy: Always`
- Init containers: wait for both `odin-redpanda-cdc:9093` AND `odin-redpanda:9092`
- Env vars:
  - `KAFKA_BROKER`: `{{ .Release.Name }}-redpanda-cdc:9093` (source: raw CDC)
  - `KAFKA_CONSUMER_GROUP`: `odin-cdc-consumer`
- `envFrom`: secretRef to `odin-cdc-env` secret (DATABASE_URL, API keys, etc.)
- Resources: 50m/300m CPU, 64Mi/128Mi RAM (from existing manifest)
- ServiceAccount: `odin-cdc-sa` (optional, for Workload Identity if needed later)

**Open question:** The existing odin-cdc consumer reads from and writes to the SAME Redpanda. In the new architecture, it reads from Redpanda-CDC and writes to Redpanda-WS. This may require a `TARGET_KAFKA_BROKER` env var pointing to `odin-redpanda:9092`. Need to verify if the odin-cdc consumer code supports separate source/target brokers, or if this needs a code change in odin-cdc.

### 2.4 Update parent Chart.yaml

**`deployments/helm/odin/Chart.yaml`** -- Add 3 new dependencies:
```yaml
dependencies:
  # ... existing ...
  - name: redpanda-cdc
    version: "1.0.0"
    condition: redpanda-cdc.enabled
  - name: debezium
    version: "1.0.0"
    condition: debezium.enabled
  - name: cdc-consumer
    version: "1.0.0"
    condition: cdc-consumer.enabled
```

### 2.5 CDC secret template

**`deployments/helm/odin/templates/cdc-secret.yaml`** -- For the odin-cdc consumer secrets (DATABASE_URL, API keys). Following the pattern of `provisioning-db-secret.yaml`.

---

## Phase 3: Environment Values

### 3.1 Base values.yaml

**`deployments/helm/odin/values.yaml`** -- Add disabled defaults:
```yaml
# CDC Pipeline (disabled by default)
redpanda-cdc:
  enabled: false
debezium:
  enabled: false
cdc-consumer:
  enabled: false
```

### 3.2 GKE Dev values

**`deployments/helm/odin/values/standard/dev.yaml`** -- Add enabled CDC config:
```yaml
redpanda-cdc:
  enabled: true
  replicaCount: 1
  memory: "384M"
  resources:
    requests: { cpu: "100m", memory: "384Mi" }
    limits:   { cpu: "250m", memory: "512Mi" }
  storage: { enabled: true, size: 5Gi }
  tolerations: [Spot VM toleration]

debezium:
  enabled: true
  replicaCount: 1
  resources:
    requests: { cpu: "25m", memory: "512Mi" }
    limits:   { cpu: "300m", memory: "1Gi" }
  tolerations: [Spot VM toleration]

cdc-consumer:
  enabled: true
  replicaCount: 1
  image:
    repository: gcr.io/trim-array-480700-j7/odin-cdc
    tag: latest
  secrets:
    existingSecret: "odin-cdc-env"
  resources:
    requests: { cpu: "50m", memory: "64Mi" }
    limits:   { cpu: "300m", memory: "128Mi" }
  tolerations: [Spot VM toleration]
```

### 3.3 Local values (disabled)

**`deployments/helm/odin/values/local.yaml`** -- CDC disabled (no CloudSQL locally):
```yaml
redpanda-cdc:
  enabled: false
debezium:
  enabled: false
cdc-consumer:
  enabled: false
```

### 3.4 Resource budget check

Current dev cluster resource requests + CDC additions:
| Component | CPU Request | Memory Request |
|-----------|------------|----------------|
| Existing services | ~1550m | ~2048Mi |
| + redpanda-cdc | 100m | 384Mi |
| + debezium | 25m | 512Mi |
| + cdc-consumer | 50m | 64Mi |
| **Total** | **~1725m** | **~3008Mi** |

Fits within 2x `e2-standard-4` nodes (8 vCPU, 32GB RAM total). No scaling needed.

---

## Phase 4: Taskfile Automation

### 4.1 Update `taskfiles/k8s.yml`

Add CDC tasks:
```yaml
logs:cdc:
  desc: Tail cdc-consumer logs

logs:debezium:
  desc: Tail Debezium logs

cdc:connector:register:
  desc: Register Debezium MySQL connector via REST API
  # Port-forwards debezium:8083, POSTs connector config JSON

cdc:connector:status:
  desc: Check Debezium connector status

cdc:secrets:create:
  desc: Create/update odin-cdc-env secret
  # Interactive prompt for DB credentials + API keys
```

Update `external-ips` task to also show NAT egress IP.

Update `deploy` task vars to read `NAT_IP` from foundation outputs.

### 4.2 Debezium connector config

**`deployments/cdc/mysql-connector.json`** -- Connector registration payload:
```json
{
  "name": "odin-mysql-cdc",
  "config": {
    "connector.class": "io.debezium.connector.mysql.MySqlConnector",
    "tasks.max": "1",
    "database.hostname": "<CLOUDSQL_READ_REPLICA_IP>",
    "database.port": "3306",
    "database.user": "debezium",
    "database.password": "<from-secret>",
    "topic.prefix": "odin",
    "database.include.list": "main,dev",
    "table.include.list": "main.trade,dev.trade",
    "schema.history.internal.kafka.bootstrap.servers": "odin-redpanda-cdc:9093",
    "schema.history.internal.kafka.topic": "odin.schema.history",
    "snapshot.mode": "schema_only",
    "snapshot.locking.mode": "none",
    "database.ssl.mode": "preferred",
    "include.schema.changes": "false",
    "max.batch.size": "2048",
    "max.queue.size": "8192",
    "decimal.handling.mode": "string"
  }
}
```

---

## Phase 5: Deployment Sequence

1. **Terraform foundation** -- Apply to create NAT static IP
2. **Whitelist NAT IP** on CloudSQL read replica firewall (manual step in `trim-array-480700-j7` project)
3. **Create k8s secret** -- `kubectl create secret generic odin-cdc-env -n odin-ws-dev --from-env-file=.env.cdc`
4. **Helm deploy** -- `task k8s:deploy ENV=dev` (deploys all 3 new subcharts)
5. **Register connector** -- `task k8s:cdc:connector:register ENV=dev`
6. **Verify pipeline** -- Check logs, verify events flow through

---

## Verification

1. `task k8s:status ENV=dev` -- All CDC pods Running
2. `task k8s:logs:debezium ENV=dev` -- Shows "Kafka Connect started"
3. `task k8s:cdc:connector:status ENV=dev` -- Connector RUNNING
4. `task k8s:logs:cdc ENV=dev` -- Consumer processing events
5. `task k8s:logs ENV=dev` -- ws-server receiving refined events
6. Connect WebSocket client to gateway -- Receive live trade events

---

## Open Questions (to address during implementation)

1. **Source/target broker separation in odin-cdc consumer**: Does the consumer code support writing to a different Kafka broker than it reads from? If `KAFKA_BROKER` is the only env var, the consumer may need a code change to add `TARGET_KAFKA_BROKER` support. Otherwise, both Redpandas would need to be the same instance (defeating the two-Redpanda design).

2. **Image registry migration** (partially resolved): With the consumer code moved into odin-ws, we can build the image in the `odin-9e902` project's Artifact Registry (`us-central1-docker.pkg.dev/odin-9e902/odin-ws/cdc-consumer`). Initially the Helm chart can still reference the old image at `gcr.io/trim-array-480700-j7/odin-cdc` while we set up the new build pipeline via `cloudbuild.yaml`.

---

## Files to Create
- `cdc/` -- odin-cdc consumer source code (moved from separate repo)
- `deployments/helm/odin/charts/redpanda-cdc/` (5 files)
- `deployments/helm/odin/charts/debezium/` (5 files)
- `deployments/helm/odin/charts/cdc-consumer/` (4 files)
- `deployments/helm/odin/templates/cdc-secret.yaml`
- `deployments/cdc/mysql-connector.json`

## Files to Modify
- `deployments/helm/odin/Chart.yaml` -- Add 3 subchart dependencies
- `deployments/helm/odin/values.yaml` -- Add disabled CDC defaults
- `deployments/helm/odin/values/standard/dev.yaml` -- Add enabled CDC config
- `deployments/helm/odin/values/local.yaml` -- Add disabled CDC config
- `deployments/terraform/modules/foundation/main.tf` -- Add NAT static IP + VPC peering + firewall rule
- `deployments/terraform/modules/foundation/variables.tf` -- Add peer VPC variables
- `deployments/terraform/modules/foundation/outputs.tf` -- Add NAT outputs
- `deployments/terraform/environments/standard/dev-foundation/main.tf` -- Pass peer VPC config
- `deployments/terraform/environments/standard/dev-foundation/outputs.tf` -- Pass through NAT outputs
- `deployments/terraform/modules/gke-standard-cluster/variables.tf` -- Add nat_static_ip_self_link var
- `deployments/terraform/modules/gke-standard-cluster/main.tf` -- Update Cloud NAT to use static IP
- `deployments/terraform/environments/standard/dev/main.tf` -- Pass NAT IP to GKE module
- `taskfiles/k8s.yml` -- Add CDC tasks (logs, connector, secrets)
