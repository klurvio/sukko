# Plan: KinD Local Deployment Configuration Update

**Status:** ✅ Implemented

## Summary

Update the Helm chart and KinD configuration to support full multi-tenant testing locally **without authentication**. This enables local provisioning of tenants, topics, and channel rules for development and testing.

## Current State (Updated 2026-02-04)

### Helm Chart Dependencies (Chart.yaml)
| Subchart | Condition | Status in local.yaml |
|----------|-----------|----------------------|
| ws-gateway | `ws-gateway.enabled` | ✅ Configured with NodePort 30000 |
| ws-server | `ws-server.enabled` | ✅ Configured with NodePort 30080 |
| provisioning | `provisioning.enabled` | ❌ NOT configured |
| postgresql | `postgresql.enabled` | ❌ Subchart does not exist |
| redpanda | `redpanda.enabled` | ✅ Configured |
| nats | `nats.enabled` | ✅ Configured |
| valkey | `valkey.enabled` | ✅ Disabled |
| monitoring | `monitoring.enabled` | ✅ Configured |

### KinD Port Mappings (kind-config.yaml)
| Service | Container Port | Host Port | Status |
|---------|----------------|-----------|--------|
| ws-server | 30080 | 3005 | ✅ Configured |
| auth (old) | 30082 | 3002 | ⚠️ Obsolete - remove |
| grafana | 30300 | 3010 | ✅ Configured |
| prometheus | 30090 | 9090 | ✅ Configured |
| console | 30800 | 8080 | ✅ Configured |
| **ws-gateway** | 30000 | 3000 | ❌ MISSING |
| **provisioning** | 30081 | 8081 | ❌ MISSING |

### Prometheus Scrape Targets
| Service | Status |
|---------|--------|
| ws-server | ✅ Configured |
| redpanda | ✅ Configured |
| ws-gateway | ❌ MISSING |
| provisioning | ❌ MISSING |

### What's Missing
1. **PostgreSQL subchart** - Provisioning service requires a database
2. **Provisioning config in local.yaml** - Not enabled, no configuration
3. **KinD port mappings** - Gateway and provisioning ports not exposed
4. **Prometheus scrape targets** - Gateway and provisioning not monitored

---

## Decision

**Option A: Create minimal PostgreSQL subchart** (Selected)

Consistent with existing chart structure (valkey, nats). Works offline, easy to configure.

---

## Implementation Steps

### Step 1: Create PostgreSQL Subchart

Create `deployments/helm/odin/charts/postgresql/`:

```
charts/postgresql/
├── Chart.yaml
├── values.yaml
└── templates/
    ├── _helpers.tpl
    ├── deployment.yaml
    ├── service.yaml
    └── secret.yaml
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
  enabled: false  # Use emptyDir for local dev
  size: 1Gi
  storageClass: ""
```

**charts/postgresql/templates/deployment.yaml:**
```yaml
{{- if .Values.enabled }}
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "postgresql.fullname" . }}
  labels:
    {{- include "postgresql.labels" . | nindent 4 }}
spec:
  replicas: 1
  selector:
    matchLabels:
      {{- include "postgresql.selectorLabels" . | nindent 6 }}
  template:
    metadata:
      labels:
        {{- include "postgresql.selectorLabels" . | nindent 8 }}
    spec:
      containers:
        - name: postgresql
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
          imagePullPolicy: {{ .Values.image.pullPolicy }}
          ports:
            - name: postgresql
              containerPort: 5432
              protocol: TCP
          env:
            - name: POSTGRES_DB
              value: {{ .Values.auth.database | quote }}
            - name: POSTGRES_USER
              value: {{ .Values.auth.username | quote }}
            - name: POSTGRES_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: {{ include "postgresql.secretName" . }}
                  key: {{ include "postgresql.secretKey" . }}
            - name: PGDATA
              value: /var/lib/postgresql/data/pgdata
          volumeMounts:
            - name: data
              mountPath: /var/lib/postgresql/data
          {{- if .Values.livenessProbe.enabled }}
          livenessProbe:
            exec:
              command:
                - pg_isready
                - -U
                - {{ .Values.auth.username | quote }}
                - -d
                - {{ .Values.auth.database | quote }}
            initialDelaySeconds: {{ .Values.livenessProbe.initialDelaySeconds }}
            periodSeconds: {{ .Values.livenessProbe.periodSeconds }}
            timeoutSeconds: {{ .Values.livenessProbe.timeoutSeconds }}
            failureThreshold: {{ .Values.livenessProbe.failureThreshold }}
          {{- end }}
          {{- if .Values.readinessProbe.enabled }}
          readinessProbe:
            exec:
              command:
                - pg_isready
                - -U
                - {{ .Values.auth.username | quote }}
                - -d
                - {{ .Values.auth.database | quote }}
            initialDelaySeconds: {{ .Values.readinessProbe.initialDelaySeconds }}
            periodSeconds: {{ .Values.readinessProbe.periodSeconds }}
            timeoutSeconds: {{ .Values.readinessProbe.timeoutSeconds }}
            failureThreshold: {{ .Values.readinessProbe.failureThreshold }}
          {{- end }}
          resources:
            {{- toYaml .Values.resources | nindent 12 }}
      volumes:
        - name: data
          {{- if .Values.persistence.enabled }}
          persistentVolumeClaim:
            claimName: {{ include "postgresql.fullname" . }}
          {{- else }}
          emptyDir: {}
          {{- end }}
      {{- with .Values.nodeSelector }}
      nodeSelector:
        {{- toYaml . | nindent 8 }}
      {{- end }}
{{- end }}
```

**charts/postgresql/templates/service.yaml:**
```yaml
{{- if .Values.enabled }}
apiVersion: v1
kind: Service
metadata:
  name: {{ include "postgresql.fullname" . }}
  labels:
    {{- include "postgresql.labels" . | nindent 4 }}
spec:
  type: {{ .Values.service.type }}
  ports:
    - port: {{ .Values.service.port }}
      targetPort: postgresql
      protocol: TCP
      name: postgresql
  selector:
    {{- include "postgresql.selectorLabels" . | nindent 4 }}
{{- end }}
```

---

### Step 2: Add PostgreSQL to Parent Chart

Update `deployments/helm/odin/Chart.yaml`:

```yaml
dependencies:
  # ... existing dependencies ...
  - name: postgresql
    version: "1.0.0"
    condition: postgresql.enabled
```

---

### Step 3: Update local.yaml

Add provisioning and postgresql configuration to `deployments/helm/odin/values/local.yaml`.

**Add to end of file:**
```yaml
# PostgreSQL for provisioning database
postgresql:
  enabled: true
  auth:
    database: odin_provisioning
    username: odin
    password: odin_local_dev  # OK for local dev only
  resources:
    requests:
      cpu: "100m"
      memory: "128Mi"
    limits:
      cpu: "250m"
      memory: "256Mi"
  persistence:
    enabled: false  # emptyDir - data lost on restart

# Provisioning API (no auth for local dev)
provisioning:
  enabled: true
  replicaCount: 1
  config:
    logLevel: debug
    logFormat: pretty
    authEnabled: false  # No auth for local provisioning
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
```

**Update existing `global:` section** (add `provisioning` subsection):
```yaml
global:
  namespace: odin-local
  provisioning:
    databaseUrl: "postgres://odin:odin_local_dev@odin-local-postgresql:5432/odin_provisioning?sslmode=disable"
```

**Notes:**
- `ws-gateway` is already configured in local.yaml with NodePort 30000 - no changes needed
- The `global.provisioning.databaseUrl` triggers automatic secret creation by parent chart

---

### Step 4: Update kind-config.yaml

Update `deployments/k8s/local/kind-config.yaml`:

**Changes:**
- Add ws-gateway port mapping (30000 → 3000)
- Add provisioning port mapping (30081 → 8081)
- Remove obsolete auth service port (30082 → 3002)

```yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: odin-ws-local

nodes:
  - role: control-plane
    extraPortMappings:
      # WebSocket Gateway (client entry point)
      - containerPort: 30000
        hostPort: 3000
        protocol: TCP
      # WebSocket Server (internal, for testing)
      - containerPort: 30080
        hostPort: 3005
        protocol: TCP
      # Provisioning API (tenant management)
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

kubeadmConfigPatches:
  - |
    kind: ClusterConfiguration
    apiServer:
      extraArgs:
        "enable-admission-plugins": "NodeRestriction,PodSecurity"
```

---

### Step 5: Update Prometheus ConfigMap

Add scrape targets for gateway and provisioning in `deployments/helm/odin/charts/monitoring/templates/prometheus-configmap.yaml`:

```yaml
      # Scrape ws-gateway metrics
      - job_name: 'ws-gateway'
        static_configs:
          - targets: ['{{ .Release.Name }}-ws-gateway:3000']
        metrics_path: /metrics

      # Scrape provisioning metrics
      - job_name: 'provisioning'
        static_configs:
          - targets: ['{{ .Release.Name }}-provisioning:8080']
        metrics_path: /metrics
```

---

## File Changes Summary

| File | Action | Description |
|------|--------|-------------|
| `charts/postgresql/Chart.yaml` | **Create** | PostgreSQL subchart metadata |
| `charts/postgresql/values.yaml` | **Create** | PostgreSQL default values |
| `charts/postgresql/templates/_helpers.tpl` | **Create** | Template helpers |
| `charts/postgresql/templates/deployment.yaml` | **Create** | PostgreSQL Deployment |
| `charts/postgresql/templates/service.yaml` | **Create** | PostgreSQL Service |
| `charts/postgresql/templates/secret.yaml` | **Create** | PostgreSQL Secret |
| `Chart.yaml` | **Edit** | Add postgresql dependency |
| `values/local.yaml` | **Edit** | Add postgresql, provisioning config |
| `k8s/local/kind-config.yaml` | **Edit** | Add gateway/provisioning ports, remove auth |
| `charts/monitoring/templates/prometheus-configmap.yaml` | **Edit** | Add gateway/provisioning scrape targets |

**Note:** Step 5 from original plan (ws-gateway config) is already done - no changes needed.

---

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

---

## Local Provisioning Flow (No Auth)

```
Developer
    │
    ▼ curl http://localhost:8081/api/v1/tenants
┌─────────────────────┐
│  provisioning       │ ◄── authEnabled: false (no JWT required)
│  NodePort 30081     │
└─────────────────────┘
    │
    ├──▶ PostgreSQL (tenant/topic metadata)
    │
    └──▶ Redpanda (create Kafka topics)
```

**Example: Create a tenant locally**
```bash
# Create tenant
curl -X POST http://localhost:8081/api/v1/tenants \
  -H "Content-Type: application/json" \
  -d '{"id": "acme", "name": "Acme Corp"}'

# Add categories (creates Kafka topics)
curl -X POST http://localhost:8081/api/v1/tenants/acme/categories \
  -H "Content-Type: application/json" \
  -d '{"categories": ["trade", "liquidity"]}'

# Set channel rules
curl -X PUT http://localhost:8081/api/v1/tenants/acme/channel-rules \
  -H "Content-Type: application/json" \
  -d '{"public": ["*.metadata"], "group_mappings": {"traders": ["*.trade"]}}'
```

---

## Testing After Implementation

```bash
# Navigate to deployments
cd /Volumes/Dev/Codev/Toniq/odin-ws/deployments

# Delete existing cluster (required for port mapping changes)
kind delete cluster --name odin-ws-local

# Create cluster with updated config
kind create cluster --config k8s/local/kind-config.yaml

# Update Helm dependencies (picks up new postgresql chart)
cd helm/odin
helm dependency update

# Install chart with local values
helm install odin-local . -f values/local.yaml -n odin-local --create-namespace

# Wait for pods
kubectl get pods -n odin-local -w

# Test endpoints
curl http://localhost:3000/health   # Gateway
curl http://localhost:3005/health   # Server
curl http://localhost:8081/health   # Provisioning

# Test provisioning (no auth required)
curl http://localhost:8081/api/v1/tenants

# View logs
kubectl logs -n odin-local -l app.kubernetes.io/name=provisioning -f
```

---

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| PostgreSQL data loss on pod restart | Expected for local dev (emptyDir), document behavior |
| Port conflicts with host services | Use non-standard ports (3000, 3005, 8081) |
| Cluster recreation needed for port changes | Document that kind-config changes require cluster recreation |
| No auth in local dev | Only for local; production requires `authEnabled: true` |
| Provisioning starts before PostgreSQL ready | PostgreSQL has readiness probe; Kubernetes restarts provisioning until DB ready |

---

## Rollback

```bash
# Delete cluster
kind delete cluster --name odin-ws-local

# Revert config files
git checkout HEAD -- deployments/helm/odin/values/local.yaml
git checkout HEAD -- deployments/k8s/local/kind-config.yaml

# Recreate with previous config
kind create cluster --config deployments/k8s/local/kind-config.yaml
helm install odin-local deployments/helm/odin -f deployments/helm/odin/values/local.yaml -n odin-local --create-namespace
```
