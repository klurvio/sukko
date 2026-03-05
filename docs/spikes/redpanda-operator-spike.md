# Spike: Redpanda Operator for Staging & Production

**Date:** 2025-12-30
**Author:** Engineering Team
**Status:** Research Complete
**Time-box:** 4 hours

---

## Objective

Evaluate whether migrating from the embedded Redpanda Helm chart to Redpanda Operator is beneficial for staging and production environments.

---

## Background

### Current State

| Environment | Redpanda Deployment | Replicas | Notes |
|-------------|---------------------|----------|-------|
| Local (kind) | Embedded Helm chart | 1 | Simple, works well |
| Develop (GKE) | Embedded Helm chart | 1 | Simple, works well |
| Staging | Embedded Helm chart | 3 | Manual scaling, no self-healing |
| Production | Embedded Helm chart | 3 | Manual upgrades, limited HA |

### Problem Statement

The embedded Helm chart works well for local/develop but lacks production-grade features:
- No automated rolling upgrades
- No self-healing on pod failures
- Manual topic management via Jobs
- No built-in rack awareness configuration
- Complex day-2 operations (scaling, config changes)

---

## Research Findings

### 1. What is Redpanda Operator?

Redpanda Operator is a Kubernetes operator that manages Redpanda clusters using Custom Resource Definitions (CRDs). It provides:

- **Declarative management** - Define desired state, operator reconciles
- **Automated lifecycle** - Rolling upgrades, scaling, self-healing
- **GitOps compatible** - CRDs can be version controlled
- **Topic CRDs** - Declarative topic management (replaces our Job-based approach)

### 2. Deployment Topology (Same Cluster)

**Important:** The Operator does NOT require a separate Kubernetes cluster. Redpanda and ws-server remain on the same GKE cluster. The only difference is *who manages* the Redpanda StatefulSet.

**Current (Helm chart):**
```
GKE Cluster
└── sukko-staging namespace
    ├── ws-gateway (Deployment)
    ├── ws-server (Deployment)
    ├── redpanda (StatefulSet)  ← managed by Helm subchart
    └── nats (Deployment)
```

**With Operator:**
```
GKE Cluster
├── cert-manager namespace
│   └── cert-manager pods
├── redpanda-system namespace
│   └── redpanda-operator pod  ← watches for Redpanda CRDs
└── sukko-staging namespace
    ├── ws-gateway (Deployment)
    ├── ws-server (Deployment)
    ├── redpanda (StatefulSet)  ← managed by Operator via CRD
    └── nats (Deployment)
```

**Key points:**
- Same Kubernetes cluster
- Same namespace for Redpanda and ws-server
- Same network connectivity (no firewall changes)
- Operator just changes *how* Redpanda is managed, not *where* it runs

### 3. Prerequisites

| Dependency | Version | Purpose |
|------------|---------|---------|
| cert-manager | v1.12+ | TLS certificate management |
| Kubernetes | 1.25+ | CRD support |
| Helm | 3.x | Operator installation |

**Note:** cert-manager adds ~50MB memory overhead (3 pods).

### 4. CRD Structure

The operator uses two main CRDs:

**Redpanda CRD** (cluster definition):
```yaml
apiVersion: cluster.redpanda.com/v1alpha2
kind: Redpanda
metadata:
  name: sukko-redpanda
spec:
  chartRef:
    chartVersion: 5.9.x
  clusterSpec:
    statefulset:
      replicas: 3
    resources:
      cpu:
        cores: 1
      memory:
        container:
          max: 2Gi
    storage:
      persistentVolume:
        enabled: true
        size: 20Gi
    listeners:
      kafka:
        port: 9093
```

**Topic CRD** (topic definition):
```yaml
apiVersion: cluster.redpanda.com/v1alpha2
kind: Topic
metadata:
  name: sukko-trades
spec:
  partitions: 12
  replicationFactor: 3
  additionalConfig:
    retention.ms: "604800000"
```

### 5. Migration Path

Redpanda provides an official migration guide from Helm to Operator:

1. **Data preservation** - PVCs are retained; Operator adopts existing StatefulSet
2. **Service names** - Service DNS names change slightly
3. **Downtime** - Migration requires brief downtime (~5 minutes)
4. **Rollback** - Possible by re-deploying Helm chart (PVCs preserved)

**Risk assessment:** Medium - requires careful coordination but well-documented.

### 6. Service Discovery Changes

| Component | Current (Helm) | With Operator |
|-----------|----------------|---------------|
| Internal Kafka | `sukko-redpanda:9093` | `sukko-redpanda.{namespace}.svc.cluster.local:9093` |
| Admin API | `sukko-redpanda:9644` | Same pattern |
| Schema Registry | `sukko-redpanda:8081` | Same pattern |

**Impact:** ws-server `KAFKA_BROKERS` config needs update.

### 7. Feature Comparison

| Feature | Embedded Helm | Redpanda Operator |
|---------|---------------|-------------------|
| Rolling upgrades | Manual | Automated |
| Self-healing | K8s StatefulSet only | Operator-managed |
| Rack awareness | Manual config | Built-in via CRD |
| Topic management | Job-based | Topic CRD |
| Config changes | Helm upgrade + restart | Reconciled automatically |
| Scaling | Manual | Declarative |
| TLS management | Manual secrets | cert-manager integration |
| Console UI | Separate deployment | Bundled option |
| Resource overhead | None | ~100MB (operator pod) |

### 8. GKE-Specific Considerations

- **Storage class:** Use `pd-ssd` for production, `standard-rwo` for staging
- **Zone spreading:** `topologySpreadConstraints` works with GKE zones
- **Node pools:** Operator supports `nodeSelector` and `tolerations`
- **Autopilot:** Supported but resource requests must be explicit

### 9. Monitoring Integration

The Operator exposes Prometheus metrics at the same endpoints:
- Metrics endpoint: `:9644/metrics`
- No changes needed to existing Prometheus scrape configs
- Grafana dashboards remain compatible

---

## Key Questions Answered

### Q1: Can we preserve existing data when migrating?

**Yes.** The migration process preserves PVCs. Steps:
1. Scale down Helm-deployed StatefulSet
2. Deploy Operator with same PVC claim names
3. Operator adopts existing volumes

### Q2: How does external access work?

**Same as Helm.** The Operator creates a LoadBalancer service for external access:
```yaml
external:
  enabled: true
  type: LoadBalancer
```
Publisher can connect via external IP.

### Q3: What's the upgrade path for Redpanda versions?

**Declarative.** Update `image.tag` in the Redpanda CRD:
```yaml
image:
  tag: v24.3.x  # Operator handles rolling update
```
Operator performs rolling restart with health checks.

### Q4: How does rack awareness work with GKE?

**Automatic.** Configure in CRD:
```yaml
rackAwareness:
  enabled: true
  nodeAnnotation: topology.kubernetes.io/zone
```
Operator reads zone labels from nodes.

### Q5: What's the resource overhead?

| Component | CPU | Memory |
|-----------|-----|--------|
| Redpanda Operator | 100m | 100Mi |
| cert-manager (3 pods) | 30m | 150Mi |
| **Total overhead** | ~130m | ~250Mi |

### Q6: Does Redpanda need its own separate cluster?

**No.** The Operator runs on the same GKE cluster as ws-server. It's just a different management approach, not a different infrastructure topology. See [Section 2: Deployment Topology](#2-deployment-topology-same-cluster) for details.

---

## Recommendation

### Environments to Migrate

| Environment | Recommendation | Rationale |
|-------------|----------------|-----------|
| Local | **Keep Helm** | Simplicity, no operator overhead |
| Develop | **Keep Helm** | Simplicity, single replica |
| Staging | **Migrate to Operator** | Test operator workflows before prod |
| Production | **Migrate to Operator** | Automated ops, self-healing, HA |

### Migration Order

1. **Phase 1:** Deploy Operator to staging, validate
2. **Phase 2:** Migrate staging Redpanda cluster
3. **Phase 3:** Run load tests, validate ws-server connectivity
4. **Phase 4:** Deploy Operator to production
5. **Phase 5:** Migrate production cluster (maintenance window)

### Estimated Effort

| Task | Effort |
|------|--------|
| cert-manager installation | 30 min |
| Operator installation | 30 min |
| Create Redpanda CRD manifests | 2 hours |
| Create Topic CRDs | 1 hour |
| Update Helm values | 1 hour |
| Staging migration + testing | 4 hours |
| Production migration | 2 hours |
| Documentation | 2 hours |
| **Total** | ~13 hours |

---

## Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Migration data loss | Low | High | Test in staging; backup PVCs |
| Service discovery breaks | Medium | Medium | Update configs before migration |
| Operator bugs | Low | Medium | Use stable release; test in staging |
| cert-manager conflicts | Low | Low | Use dedicated namespace |

---

## Decision

**Proceed with migration** for staging and production environments.

**Rationale:**
- Automated rolling upgrades reduce operational burden
- Self-healing improves reliability
- Topic CRDs eliminate Job-based topic management
- GitOps-friendly declarative management
- Minimal resource overhead (~250Mi total)

---

## Next Steps

1. [ ] Create implementation plan with detailed steps
2. [ ] Set up cert-manager in staging cluster
3. [ ] Install Redpanda Operator in staging
4. [ ] Create Redpanda CRD for staging environment
5. [ ] Create Topic CRDs for all 8 sukko.* topics
6. [ ] Update ws-server Helm values for Operator
7. [ ] Test migration in staging
8. [ ] Document runbooks for day-2 operations

---

## References

- [Redpanda Operator Documentation](https://docs.redpanda.com/current/deploy/deployment-option/self-hosted/kubernetes/k-deployment-overview/)
- [Migrate from Helm to Operator](https://docs.redpanda.com/current/migrate/kubernetes/helm-to-operator/)
- [Redpanda CRD Reference](https://docs.redpanda.com/current/reference/k-crd/)
- [Topic CRD Reference](https://docs.redpanda.com/current/manage/kubernetes/k-manage-topics/)
- [GKE Deployment Guide](https://docs.redpanda.com/current/deploy/deployment-option/self-hosted/kubernetes/gke-guide/)
