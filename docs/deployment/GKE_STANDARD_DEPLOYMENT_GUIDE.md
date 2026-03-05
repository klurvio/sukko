# GKE Standard Deployment Guide

Complete guide for deploying sukko to GKE Standard with Spot VMs, including all fixes and lessons learned from the initial deployment.

## Table of Contents

1. [Prerequisites](#1-prerequisites)
2. [Infrastructure Setup (Terraform)](#2-infrastructure-setup-terraform)
3. [Artifact Registry Setup](#3-artifact-registry-setup)
4. [Building Images](#4-building-images)
5. [Deploying to GKE](#5-deploying-to-gke)
6. [Verification](#6-verification)
7. [Troubleshooting](#7-troubleshooting)
8. [Fixes Applied](#8-fixes-applied)
9. [Quick Reference](#9-quick-reference)

---

## 1. Prerequisites

### Required Tools

```bash
# Install via Homebrew (macOS)
brew install google-cloud-sdk
brew install terraform
brew install helm
brew install go-task  # Taskfile runner

# Install GKE auth plugin (required for kubectl)
gcloud components install gke-gcloud-auth-plugin
```

### GCP Authentication

```bash
# Login to GCP
gcloud auth login

# Set application default credentials (required for Terraform)
gcloud auth application-default login

# Set default project
gcloud config set project trim-array-480700-j7

# Configure Docker for Artifact Registry
gcloud auth configure-docker us-central1-docker.pkg.dev
```

### Verify Setup

```bash
# Check gcloud is authenticated
gcloud auth list

# Check terraform version
terraform version

# Check helm version
helm version

# Check task is installed
task --version
```

---

## 2. Infrastructure Setup (Terraform)

### Directory Structure

```
deployments/terraform/gke-standard/
├── modules/gke-cluster/     # Shared Terraform module
├── develop/                 # Develop environment (isolated state)
├── staging/                 # Staging environment (isolated state)
└── production/              # Production environment (isolated state)
```

### Deploy Infrastructure

```bash
# Default environment is 'develop'
# For other environments: GKE_STD_ENV=staging task k8s:gke-standard:tf:init

# Initialize Terraform
task k8s:gke-standard:tf:init

# Review planned changes
task k8s:gke-standard:tf:plan

# Apply infrastructure (creates GKE cluster, VPC, etc.)
task k8s:gke-standard:tf:apply

# Connect kubectl to the new cluster
task k8s:gke-standard:connect
```

### Verify Cluster

```bash
# Check nodes are ready
kubectl get nodes

# Expected output:
# NAME                                     STATUS   ROLES    AGE   VERSION
# gke-sukko-develop-...-40k3            Ready    <none>   5m    v1.33.x
# gke-sukko-develop-...-z27l            Ready    <none>   5m    v1.33.x
```

### Fix Applied: Maintenance Policy

GKE requires maintenance windows to have at least 48 hours availability within any 32-day period.

**Error encountered:**
```
maintenance policy would go longer than 32d without 48h maintenance availability
```

**Fix:** Changed `recurrence` from `FREQ=WEEKLY;BYDAY=SU` to `FREQ=DAILY` in:
- `deployments/terraform/gke-standard/modules/gke-cluster/main.tf`

---

## 3. Artifact Registry Setup

### One-Time Setup

These commands only need to be run once per GCP project.

```bash
# Enable required APIs
gcloud services enable cloudbuild.googleapis.com --project=trim-array-480700-j7
gcloud services enable artifactregistry.googleapis.com --project=trim-array-480700-j7

# Create Docker repository
gcloud artifacts repositories create sukko \
  --repository-format=docker \
  --location=us-central1 \
  --project=trim-array-480700-j7 \
  --description="Sukko WebSocket Server images"

# Grant Cloud Build permission to push images
PROJECT_NUMBER=$(gcloud projects describe trim-array-480700-j7 --format='value(projectNumber)')
gcloud projects add-iam-policy-binding trim-array-480700-j7 \
  --member="serviceAccount:${PROJECT_NUMBER}@cloudbuild.gserviceaccount.com" \
  --role="roles/artifactregistry.writer"
```

### Verify Repository

```bash
# List images in repository
gcloud artifacts docker images list us-central1-docker.pkg.dev/trim-array-480700-j7/sukko
```

---

## 4. Building Images

### Option A: Cloud Build (Recommended)

Cloud Build runs on Linux/amd64 - the same architecture as GKE nodes.

```bash
# Build all images using Cloud Build
task k8s:gke-standard:cloud-build

# Check build status
task k8s:gke-standard:cloud-build:status

# Stream logs from latest build
task k8s:gke-standard:cloud-build:logs
```

**Build time:** ~1-2 minutes for all 3 images (parallel builds)

### Option B: Local Docker Build

> **WARNING:** If you're on an Apple Silicon Mac (M1/M2/M3), local Docker builds create `arm64` images. These will **NOT run on GKE** which uses `amd64` nodes. You'll see `exec format error` in pod logs.

```bash
# Only use this if you're on an Intel Mac or Linux
task k8s:gke-standard:build
```

### Images Built

| Image | Description |
|-------|-------------|
| `ws-server` | WebSocket server (Go) |
| `ws-gateway` | WebSocket gateway/proxy (Go) |
| `publisher` | Kafka message publisher (Node.js) |

### Fix Applied: Cloud Build Substitution Variables

Manual `gcloud builds submit` doesn't have `$SHORT_SHA` set (only available with triggers).

**Fix:** Added custom `_TAG` substitution variable with default value in `cloudbuild.yaml`:

```yaml
substitutions:
  _TAG: latest  # Can override with --substitutions=_TAG=v1.0.0
```

---

## 5. Deploying to GKE

### Deploy Application

```bash
# Deploy to develop environment
task k8s:gke-standard:deploy:develop

# Or for other environments:
# task k8s:gke-standard:deploy:staging
# task k8s:gke-standard:deploy:production
```

### What Gets Deployed

| Component | Replicas | Description |
|-----------|----------|-------------|
| ws-server | 2 | WebSocket server shards |
| ws-gateway | 2 | Gateway/proxy layer |
| publisher | 1 | Kafka publisher (for testing) |
| redpanda | 1 | Kafka-compatible message broker |
| nats | 1 | Broadcast bus for cross-shard messaging |
| prometheus | 1 | Metrics collection |
| grafana | 1 | Dashboards |
| loki | 1 | Log aggregation |
| promtail | 2 (DaemonSet) | Log shipping |

### Fix Applied: Helm Template Registry Prefix

Helm templates weren't using `global.imageRegistry`, causing pods to pull from Docker Hub.

**Error encountered:**
```
Failed to pull image "ws-server:latest": docker.io/library/ws-server:latest: not found
```

**Fix:** Updated deployment templates in all subcharts:

```yaml
# Before
image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"

# After
image: "{{- if .Values.global.imageRegistry }}{{ .Values.global.imageRegistry }}/{{- end }}{{ .Values.image.repository }}:{{ .Values.image.tag }}"
```

**Files modified:**
- `deployments/k8s/helm/sukko/charts/ws-server/templates/deployment.yaml`
- `deployments/k8s/helm/sukko/charts/ws-gateway/templates/deployment.yaml`
- `deployments/k8s/helm/sukko/charts/publisher/templates/deployment.yaml`

---

## 6. Verification

### Check Pod Status

```bash
# Quick status
task k8s:gke-standard:status

# Detailed pod info
kubectl get pods -n sukko-std-develop -o wide

# All pods should show: Running, READY 1/1
```

### Check Services

```bash
# List services with external IPs
kubectl get svc -n sukko-std-develop

# Expected LoadBalancer services:
# sukko-gateway      LoadBalancer   x.x.x.x   443:xxxxx/TCP
# sukko-server       LoadBalancer   x.x.x.x   3001:xxxxx/TCP
# sukko-redpanda-external LoadBalancer x.x.x.x   9092:xxxxx/TCP
```

### Health Check

```bash
# Via port-forward (bypasses firewall)
kubectl port-forward -n sukko-std-develop svc/sukko-server 3001:3001 &
curl http://localhost:3001/health

# Expected response:
# {"healthy":true,"status":"healthy","shards":[...]}
```

### View Logs

```bash
# ws-server logs
task k8s:gke-standard:logs

# ws-gateway logs
task k8s:gke-standard:logs:gateway

# All pod logs
task k8s:gke-standard:logs:all
```

---

## 7. Troubleshooting

### ImagePullBackOff

**Symptom:** Pods stuck in `ImagePullBackOff` state

**Check:**
```bash
kubectl describe pod -n sukko-std-develop <pod-name> | grep -A5 "Warning"
```

**Common causes:**
1. **Artifact Registry doesn't exist** → Create with `gcloud artifacts repositories create`
2. **Docker not authenticated** → Run `gcloud auth configure-docker us-central1-docker.pkg.dev`
3. **Wrong image path** → Check `global.imageRegistry` in values file

### exec format error

**Symptom:** Pods crash with `exec format error` in logs

```bash
kubectl logs -n sukko-std-develop <pod-name>
# exec ./sukko: exec format error
```

**Cause:** Image built for wrong architecture (arm64 on Mac, but GKE needs amd64)

**Fix:** Use Cloud Build instead of local Docker:
```bash
task k8s:gke-standard:cloud-build
kubectl rollout restart deployment -n sukko-std-develop
```

### CrashLoopBackOff

**Check logs:**
```bash
kubectl logs -n sukko-std-develop <pod-name> --previous
```

**Common causes:**
1. Missing environment variables
2. Can't connect to Kafka/NATS
3. Wrong command/args in deployment

### External IP Not Accessible

**Symptom:** `curl` to LoadBalancer IP times out

**Check firewall rules:**
```bash
gcloud compute firewall-rules list --project=trim-array-480700-j7
```

**Fix:** Create ingress rule (if missing):
```bash
gcloud compute firewall-rules create allow-sukko-ingress \
  --project=trim-array-480700-j7 \
  --allow=tcp:443,tcp:3001,tcp:9092 \
  --source-ranges=0.0.0.0/0 \
  --target-tags=gke-sukko-develop
```

### Restart Deployments

After updating images or configs:
```bash
kubectl rollout restart deployment -n sukko-std-develop
```

---

## 8. Fixes Applied

Summary of all fixes applied during the initial deployment:

| Issue | Root Cause | Fix | File |
|-------|------------|-----|------|
| Taskfile `REGISTRY` using wrong project | Variable name collision with other taskfiles | Renamed `REGISTRY` → `GKE_STD_REGISTRY` | `taskfiles/k8s/gke-standard.yml` |
| Pods pulling from Docker Hub | Helm templates not using `global.imageRegistry` | Added registry prefix to image template | `charts/*/templates/deployment.yaml` |
| Cloud Build `$SHORT_SHA` empty | Only set for trigger-based builds | Added `_TAG` substitution with default | `cloudbuild.yaml` |
| `exec format error` | arm64 image on amd64 nodes | Use Cloud Build instead of local Docker | N/A |
| Artifact Registry 404 | Repository didn't exist | Created with `gcloud artifacts repositories create` | N/A |
| Docker push denied | Docker not authenticated to GCP | Ran `gcloud auth configure-docker` | N/A |
| Terraform maintenance policy error | Weekly window doesn't meet 48h/32d requirement | Changed to `FREQ=DAILY` | `modules/gke-cluster/main.tf` |

---

## 9. Quick Reference

### Environment Variables

```bash
# Deploy to different environment
GKE_STD_ENV=staging task k8s:gke-standard:deploy

# Override tag when building
gcloud builds submit --config=cloudbuild.yaml --substitutions=_TAG=v1.0.0 .
```

### Common Commands

```bash
# Infrastructure
task k8s:gke-standard:tf:init          # Initialize Terraform
task k8s:gke-standard:tf:plan          # Plan changes
task k8s:gke-standard:tf:apply         # Apply infrastructure
task k8s:gke-standard:connect          # Connect kubectl

# Building
task k8s:gke-standard:cloud-build      # Build with Cloud Build
task k8s:gke-standard:build            # Build locally (Intel only)

# Deployment
task k8s:gke-standard:deploy:develop   # Deploy to develop
task k8s:gke-standard:deploy:staging   # Deploy to staging
task k8s:gke-standard:deploy:production # Deploy to production

# Monitoring
task k8s:gke-standard:status           # Pod/service status
task k8s:gke-standard:logs             # ws-server logs
task k8s:gke-standard:logs:gateway     # ws-gateway logs
task k8s:gke-standard:health           # Health check

# Port Forwarding
task k8s:gke-standard:port-forward:ws      # ws-server on :3005
task k8s:gke-standard:port-forward:gateway # ws-gateway on :3006
task k8s:gke-standard:port-forward:grafana # Grafana on :3000

# Cleanup
task k8s:gke-standard:down             # Uninstall Helm release
task k8s:gke-standard:tf:destroy       # Destroy infrastructure
```

### File Locations

| File | Purpose |
|------|---------|
| `taskfiles/k8s/gke-standard.yml` | All task commands |
| `cloudbuild.yaml` | Cloud Build configuration |
| `deployments/terraform/gke-standard/develop/` | Terraform for develop |
| `deployments/k8s/helm/sukko/values/standard/develop.yaml` | Helm values for develop |
| `deployments/k8s/helm/sukko/charts/*/templates/` | Helm templates |

---

## Appendix: Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     GKE Standard Cluster                     │
│                   (2x e2-standard-4 Spot VMs)                │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  ┌──────────────┐     ┌──────────────┐                      │
│  │ ws-gateway   │     │ ws-gateway   │  ← LoadBalancer :443 │
│  │  (replica 1) │     │  (replica 2) │                      │
│  └──────┬───────┘     └──────┬───────┘                      │
│         │                    │                               │
│         └────────┬───────────┘                               │
│                  ▼                                           │
│  ┌──────────────┐     ┌──────────────┐                      │
│  │ ws-server    │     │ ws-server    │  ← LoadBalancer :3001│
│  │  (shard 0)   │     │  (shard 1)   │                      │
│  └──────┬───────┘     └──────┬───────┘                      │
│         │                    │                               │
│         └────────┬───────────┘                               │
│                  ▼                                           │
│         ┌───────────────┐                                    │
│         │     NATS      │  ← Broadcast bus                  │
│         └───────────────┘                                    │
│                  ▲                                           │
│                  │                                           │
│         ┌───────────────┐                                    │
│         │   Redpanda    │  ← Kafka (LoadBalancer :9092)     │
│         └───────────────┘                                    │
│                  ▲                                           │
│                  │                                           │
│         ┌───────────────┐                                    │
│         │   Publisher   │  ← Test message publisher         │
│         └───────────────┘                                    │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```
