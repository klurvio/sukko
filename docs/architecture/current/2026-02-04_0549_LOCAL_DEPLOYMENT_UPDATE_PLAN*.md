# Plan: KinD Local Deployment Configuration Update

## Summary

Update the Helm chart and KinD configuration to support full multi-tenant testing locally. The current configuration has ws-gateway and ws-server enabled, but is missing PostgreSQL and provisioning service configuration.

## Current State

### Helm Chart Dependencies (Chart.yaml)
| Subchart | Condition | Status |
|----------|-----------|--------|
| ws-gateway | `ws-gateway.enabled` | Enabled in local.yaml |
| ws-server | `ws-server.enabled` | Enabled in local.yaml |
| provisioning | `provisioning.enabled` | **NOT configured in local.yaml** |
| redpanda | `redpanda.enabled` | Enabled in local.yaml |
| nats | `nats.enabled` | Enabled in local.yaml |
| valkey | `valkey.enabled` | Disabled |
| monitoring | `monitoring.enabled` | Enabled in local.yaml |

### KinD Port Mappings (kind-config.yaml)
| Service | Container Port | Host Port | Status |
|---------|----------------|-----------|--------|
| ws-server | 30080 | 3005 | Configured |
| auth (old) | 30082 | 3002 | Configured (obsolete) |
| grafana | 30300 | 3010 | Configured |
| prometheus | 30090 | 9090 | Configured |
| console | 30800 | 8080 | Configured |
| **ws-gateway** | 30000 | 3000 | **MISSING** |
| **provisioning** | 30081 | 8081 | **MISSING** |

### Missing Components
1. **PostgreSQL** - No chart exists; provisioning and gateway (when auth enabled) need it
2. **Provisioning config in local.yaml** - Service disabled, no configuration
3. **Gateway port mapping** - Not exposed to host
4. **Database URL** - `global.provisioning.databaseUrl` not set

## Implementation Options

### Option A: Add PostgreSQL Subchart (Recommended)

Create a minimal PostgreSQL subchart similar to existing valkey/nats charts.

**Pros:**
- Consistent with existing chart structure
- Works offline (no external dependencies)
- Easy to configure for local dev

**Cons:**
- More files to maintain

### Option B: Use Bitnami PostgreSQL as External Dependency

Add Bitnami's postgresql chart as a dependency in Chart.yaml.

**Pros:**
- Battle-tested, feature-rich
- Less code to maintain

**Cons:**
- Large chart, many options we don't need
- External dependency

### Option C: External PostgreSQL (User-Managed)

Document that users should run PostgreSQL externally (Docker, native, etc.)

**Pros:**
- No chart changes needed
- Flexible for users

**Cons:**
- More setup steps for users
- Less reproducible

**Decision:** Option A - Create minimal PostgreSQL subchart for consistency.

## Implementation Steps

### Step 1: Create PostgreSQL Subchart

Create `deployments/k8s/helm/odin/charts/postgresql/`:

```
charts/postgresql/
├── Chart.yaml
├── values.yaml
└── templates/
    ├── deployment.yaml
    ├── service.yaml
    ├── secret.yaml
    └── _helpers.tpl
```

**charts/postgresql/Chart.yaml:**
```yaml
apiVersion: v2
name: postgresql
description: PostgreSQL database for Odin provisioning
type: application
version: 1.0.0
appVersion: "16"
```

**charts/postgresql/values.yaml:**
```yaml
enabled: false

image:
  repository: postgres
  tag: 16-alpine
  pullPolicy: IfNotPresent

auth:
  database: odin_provisioning
  username: odin
  # Password set via secret or values
  password: ""
  existingSecret: ""

resources:
  requests:
    cpu: "100m"
    memory: "128Mi"
  limits:
    cpu: "500m"
    memory: "256Mi"

service:
  type: ClusterIP
  port: 5432

persistence:
  enabled: false  # Use emptyDir for local
  size: 1Gi
  storageClass: ""
```

### Step 2: Add PostgreSQL to Parent Chart

Update `deployments/k8s/helm/odin/Chart.yaml`:

```yaml
dependencies:
  # ... existing dependencies ...
  - name: postgresql
    version: "1.0.0"
    condition: postgresql.enabled
```

### Step 3: Update local.yaml

Add provisioning and postgresql configuration:

```yaml
# PostgreSQL for provisioning database
postgresql:
  enabled: true
  auth:
    database: odin_provisioning
    username: odin
    password: odin_local_dev  # OK for local dev
  resources:
    requests:
      cpu: "100m"
      memory: "128Mi"
    limits:
      cpu: "250m"
      memory: "256Mi"
  persistence:
    enabled: false  # emptyDir for local

# Provisioning API
provisioning:
  enabled: true
  replicaCount: 1
  config:
    logLevel: debug
    logFormat: pretty
    authEnabled: false  # No auth for local dev
    topicNamespace: local
    kafkaBrokers: ""  # Uses dynamic default
  resources:
    requests:
      cpu: "50m"
      memory: "64Mi"
    limits:
      cpu: "250m"
      memory: "128Mi"
  service:
    type: NodePort
    port: 8080
    nodePort: 30081

# Update global with database URL
global:
  namespace: odin-local
  provisioning:
    databaseUrl: "postgres://odin:odin_local_dev@odin-local-postgresql:5432/odin_provisioning?sslmode=disable"
```

### Step 4: Update kind-config.yaml

Add gateway and provisioning port mappings, remove obsolete auth service:

```yaml
nodes:
  - role: control-plane
    extraPortMappings:
      # WebSocket Gateway (public endpoint)
      - containerPort: 30000
        hostPort: 3000
        protocol: TCP
      # WebSocket Server (internal, for testing)
      - containerPort: 30080
        hostPort: 3005
        protocol: TCP
      # Provisioning API
      - containerPort: 30081
        hostPort: 8081
        protocol: TCP
      # Grafana
      - containerPort: 30300
        hostPort: 3010
        protocol: TCP
      # Prometheus
      - containerPort: 30090
        hostPort: 9090
        protocol: TCP
      # Redpanda Console
      - containerPort: 30800
        hostPort: 8080
        protocol: TCP
```

### Step 5: Update ws-gateway local.yaml Config

Ensure gateway has NodePort service for local access:

```yaml
ws-gateway:
  enabled: true
  replicaCount: 1
  config:
    logLevel: debug
    logFormat: pretty
    authEnabled: false  # Disable for simple local testing
  resources:
    requests:
      cpu: "100m"
      memory: "64Mi"
    limits:
      cpu: "250m"
      memory: "128Mi"
  service:
    type: NodePort
    port: 3000
    nodePort: 30000
```

### Step 6: Update Prometheus ConfigMap

Add scrape targets for gateway and provisioning in `charts/monitoring/templates/prometheus-configmap.yaml`:

```yaml
- job_name: 'ws-gateway'
  static_configs:
    - targets: ['{{ .Release.Name }}-ws-gateway:3000']
  metrics_path: /metrics

- job_name: 'provisioning'
  static_configs:
    - targets: ['{{ .Release.Name }}-provisioning:8080']
  metrics_path: /metrics
```

## File Changes Summary

| File | Action | Description |
|------|--------|-------------|
| `charts/postgresql/Chart.yaml` | Create | PostgreSQL subchart metadata |
| `charts/postgresql/values.yaml` | Create | PostgreSQL default values |
| `charts/postgresql/templates/*.yaml` | Create | Deployment, Service, Secret |
| `Chart.yaml` | Edit | Add postgresql dependency |
| `values/local.yaml` | Edit | Add postgresql, provisioning config; update gateway service |
| `environments/local/kind-config.yaml` | Edit | Add gateway/provisioning ports, remove obsolete auth |
| `charts/monitoring/templates/prometheus-configmap.yaml` | Edit | Add gateway/provisioning scrape targets |

## Port Assignments (Final)

| Service | Internal Port | NodePort | Host Port | Description |
|---------|---------------|----------|-----------|-------------|
| ws-gateway | 3000 | 30000 | 3000 | Client WebSocket endpoint |
| ws-server | 3001 | 30080 | 3005 | Backend (for testing) |
| provisioning | 8080 | 30081 | 8081 | Tenant management API |
| postgresql | 5432 | - | - | Database (cluster-only) |
| prometheus | 9090 | 30090 | 9090 | Metrics |
| grafana | 3000 | 30300 | 3010 | Dashboards |
| redpanda | 9092 | - | - | Kafka API (cluster-only) |
| redpanda-console | 8080 | 30800 | 8080 | Redpanda UI |

## Connection Flow

```
Client (Browser/App)
    │
    ▼ ws://localhost:3000/ws
┌─────────────────────┐
│  ws-gateway         │ ◄── JWT validation (when authEnabled: true)
│  NodePort 30000     │     Reads keys from PostgreSQL
└─────────────────────┘
    │
    ▼ ws://odin-local-ws-server:3001/ws
┌─────────────────────┐
│  ws-server          │
│  NodePort 30080     │
└─────────────────────┘
    │
    ▼ Subscribe to Kafka topics
┌─────────────────────┐
│  Redpanda           │
│  ClusterIP 9092     │
└─────────────────────┘
```

## Testing After Implementation

```bash
# Navigate to deployments
cd /Volumes/Dev/Codev/Toniq/odin-ws/deployments/k8s

# Delete existing cluster (if any)
kind delete cluster --name odin-ws-local

# Create cluster with updated config
kind create cluster --config environments/local/kind-config.yaml

# Update Helm dependencies (picks up new postgresql chart)
cd helm/odin
helm dependency update

# Install chart with local values
helm install odin-local . -f values/local.yaml -n odin-local --create-namespace

# Wait for pods
kubectl get pods -n odin-local -w

# Test endpoints
curl http://localhost:3000/version  # Gateway
curl http://localhost:3005/version  # Server
curl http://localhost:8081/version  # Provisioning
curl http://localhost:8081/health   # Provisioning health

# View logs
kubectl logs -n odin-local -l app=ws-gateway -f
kubectl logs -n odin-local -l app=provisioning -f
```

## Auth-Enabled Testing (Optional)

To test multi-tenant auth flow:

1. Update `values/local.yaml`:
   ```yaml
   ws-gateway:
     config:
       authEnabled: true
   ```

2. Reinstall:
   ```bash
   helm upgrade odin-local . -f values/local.yaml -n odin-local
   ```

3. Create tenant via provisioning API:
   ```bash
   curl -X POST http://localhost:8081/api/v1/tenants \
     -H "Content-Type: application/json" \
     -d '{"name": "test-tenant"}'
   ```

4. Create signing key and generate JWT for testing

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| PostgreSQL data loss on pod restart | Expected for local dev (emptyDir), document in README |
| Port conflicts with host services | Use non-standard ports (3000, 3005, 8081) |
| Cluster recreation needed for port changes | Document that kind-config changes require cluster recreation |

## Rollback

```bash
# If issues arise, delete and recreate
kind delete cluster --name odin-ws-local

# Revert config files
git checkout HEAD -- deployments/k8s/helm/odin/values/local.yaml
git checkout HEAD -- deployments/k8s/environments/local/kind-config.yaml

# Recreate with previous config
kind create cluster --config deployments/k8s/environments/local/kind-config.yaml
helm install odin-local deployments/k8s/helm/odin -f deployments/k8s/helm/odin/values/local.yaml -n odin-local --create-namespace
```
