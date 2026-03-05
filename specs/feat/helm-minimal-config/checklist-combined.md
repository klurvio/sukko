# Requirements Checklist: Deployment + Config Coherence + Security

**Branch**: feat/helm-minimal-config | **Generated**: 2026-03-03
**Spec**: specs/feat/helm-minimal-config/spec.md | **Plan**: specs/feat/helm-minimal-config/plan.md

---

## Deployment

### Completeness

- [ ] CHK001 Are rollback procedures specified for each Go default change (BROADCAST_TYPE, AUTH_ENABLED, ENVIRONMENT, rate limits)? [Completeness]
- [ ] CHK002 Are health check endpoints, intervals, and failure thresholds defined for all Docker Compose services? [Completeness]
- [ ] CHK003 Are resource requests and limits specified for all Helm pre-install validation hook Jobs? [Completeness]
- [ ] CHK004 Are startup ordering and dependency health conditions defined for all Docker Compose services (not just `depends_on`)? [Completeness]
- [ ] CHK005 Are image pull policies and registry authentication requirements specified for validation hook Jobs? [Completeness]

### Clarity

- [ ] CHK006 Is "equivalent runtime behavior" (SC-004, NFR-004) quantified with specific criteria beyond "application behavior is identical"? [Clarity]
- [ ] CHK007 Is "within 60 seconds" (NFR-001, SC-002) measured from a defined starting point (container start, first log line, health check pass)? [Clarity]
- [ ] CHK008 Is the exact Helm merge precedence behavior documented for subchart vs parent vs override file conflicts (edge case in spec)? [Clarity]
- [ ] CHK009 Is "zero-config" precisely defined — does it mean zero values file, empty values file, or default values only? [Clarity]

### Consistency

- [ ] CHK010 Are Docker Compose service names consistent with Helm auto-wired address templates (`nats`, `provisioning`, `ws-server`)? [Consistency]
- [ ] CHK011 Are port numbers consistent across Docker Compose (3000, 3001, 8080, 9090) and Helm service definitions? [Consistency]
- [ ] CHK012 Is `provisioning.enabled` default consistent between parent values.yaml (plan says `true`) and current chart (currently `false`)? [Consistency]
- [ ] CHK013 Are validation hook Job container commands consistent with actual binary names in Dockerfiles (`./sukko`, `./sukko-gateway`, `./sukko-provisioning`)? [Consistency]

### Measurability

- [ ] CHK014 Is the "at least 50% reduction" target (NFR-003) measurable against a defined baseline (current line counts documented in research.md)? [Measurability]
- [ ] CHK015 Are specific line count targets (<100, <60, <80, <200, <80) for each values.yaml file verifiable via automated check? [Measurability]
- [ ] CHK016 Is "no more than 3 fields" (NFR-002) for mode switching verifiable against each mode combination? [Measurability]

### Coverage

- [ ] CHK017 Are failure scenarios specified for validation hook Jobs (timeout, OOM, image pull failure)? [Coverage]
- [ ] CHK018 Are upgrade path scenarios covered (existing deployment with old values.yaml structure → new structure)? [Coverage]
- [ ] CHK019 Are multi-replica scenarios addressed for Docker Compose (e.g., scaling ws-server with `--scale`)? [Coverage]
- [ ] CHK020 Is the behavior specified when `extraEnv` overrides conflict with template-injected env vars? [Coverage]

---

## Config Coherence

### Completeness

- [ ] CHK021 Are all five Go envDefault changes (BROADCAST_TYPE, WS_MAX_BROADCAST_RATE, CONN_RATE_LIMIT_IP_BURST, CONN_RATE_LIMIT_IP_RATE, ENVIRONMENT) specified with exact before/after values? [Completeness]
- [ ] CHK022 Are all env vars removed from Helm templates enumerated (spec lists "~50 fields" for ws-server — are they individually named)? [Completeness]
- [ ] CHK023 Is the complete list of `redact:"true"` tagged fields specified for each config struct? [Completeness]
- [ ] CHK024 Are auto-wired address templates specified with exact Helm template expressions (e.g., `{{ .Release.Name }}-nats:4222`)? [Completeness]

### Clarity

- [ ] CHK025 Is the hysteresis sentinel value (0) clearly distinguished from a legitimate lower threshold of 0.0? [Clarity]
- [ ] CHK026 Is the `extraEnv` precedence specified — does it override template-injected vars, or vice versa? [Clarity]
- [ ] CHK027 Is the `MESSAGE_BACKEND` env var injection behavior specified — injected always, or only when explicitly set in values? [Clarity]
- [ ] CHK028 Is the difference between `nats.urls` (broadcast) and `natsJetStream.urls` (message backend) clearly specified in both spec and plan? [Clarity]

### Consistency

- [ ] CHK029 Are env var names in plan Phase 3 templates consistent with Go struct `env:` tags (e.g., `BROADCAST_TYPE` matches `env:"BROADCAST_TYPE"`)? [Consistency]
- [ ] CHK030 Is `ENVIRONMENT` envDefault change (`development` → `local`) applied consistently across all three config structs (server, gateway, provisioning)? [Consistency]
- [ ] CHK031 Are conditional template blocks consistent — does `messageBackend: kafka` trigger KAFKA_BROKERS injection in both deployment and validation hook templates? [Consistency]
- [ ] CHK032 Is the `DATABASE_DRIVER` auto-derivation logic (from externalDatabase presence) consistent between plan description and task T027? [Consistency]

### Measurability

- [ ] CHK033 Can the "every env var set in a Helm template MUST have a corresponding `env:` struct tag in Go" (FR-008) be verified by automated tooling? [Measurability]
- [ ] CHK034 Is there a defined method to verify that removed Helm fields produce identical runtime behavior via Go defaults? [Measurability]

### Coverage

- [ ] CHK035 Are all three config structs covered for the ENVIRONMENT default change (server_config.go, gateway_config.go, provisioning_config.go)? [Coverage]
- [ ] CHK036 Are all mode combinations covered in conditional template logic (direct+nats, kafka+nats, nats+nats, direct+valkey, kafka+valkey)? [Coverage]
- [ ] CHK037 Is the behavior specified when both `broadcast.type` and `messageBackend` are set to `nats` (shared NATS instance, separate config)? [Coverage]
- [ ] CHK038 Are edge cases specified for `--validate-config` (missing env vars, invalid types, partial config)? [Coverage]

### Dependencies

- [ ] CHK039 Are NATS version requirements documented (2.10+ for JetStream support)? [Dependencies]
- [ ] CHK040 Are Docker version requirements documented for Docker Compose `depends_on.condition` syntax? [Dependencies]
- [ ] CHK041 Are Helm version requirements documented (Helm 3.x for hook-delete-policy support)? [Dependencies]

---

## Security

### Completeness

- [ ] CHK042 Are all sensitive fields across all three config structs identified and tagged for redaction? [Completeness]
- [ ] CHK043 Is the `/config` endpoint access control specified — is it exposed only on internal admin mux, or also on public-facing ports? [Completeness]
- [ ] CHK044 Are Kubernetes Secret references specified for sensitive values in Helm templates (DATABASE_URL, KAFKA_SASL_PASSWORD, NATS tokens)? [Completeness]
- [ ] CHK045 Is the `AUTH_ENABLED=false` temporary default documented with a migration timeline or trigger condition for reverting to `true`? [Completeness]

### Clarity

- [ ] CHK046 Is "sensitive fields" precisely defined — passwords and tokens only, or also connection strings, CA paths, usernames? [Clarity]
- [ ] CHK047 Is the redaction behavior for empty vs non-empty secrets clearly specified (`""` stays `""`, non-empty becomes `"[REDACTED]"`)? [Clarity]
- [ ] CHK048 Is the security implication of `AUTH_ENABLED=false` default clearly documented for production deployments? [Clarity]

### Consistency

- [ ] CHK049 Is the redaction tag (`redact:"true"`) consistent with Go struct tag conventions used elsewhere in the codebase? [Consistency]
- [ ] CHK050 Are Secret references in Helm templates consistent between deployment templates and validation hook Job templates? [Consistency]
- [ ] CHK051 Is the `AUTH_ENABLED` default consistent between spec (FR-018a says `false`), plan (Phase 1.3 says `false`), and tasks (T007 says `false`)? [Consistency]

### Coverage

- [ ] CHK052 Is the behavior specified when `/config` is called before config is fully loaded (race condition during startup)? [Coverage]
- [ ] CHK053 Are TLS configuration requirements specified for inter-service communication in Docker Compose (or explicitly stated as not required for local dev)? [Coverage]
- [ ] CHK054 Is the behavior specified when `existingSecret` references a non-existent Kubernetes Secret? [Coverage]
- [ ] CHK055 Are rate limiting requirements maintained after the config cleanup — are rate limit defaults preserved via Go envDefaults? [Coverage]

---

## Summary

| Domain | Items | Focus Areas |
|--------|-------|-------------|
| Deployment | 20 | Rollback procedures, upgrade paths, Docker Compose health/ordering, validation hook failure modes |
| Config Coherence | 21 | Env var name alignment, mode combination coverage, conditional template consistency, hysteresis sentinel |
| Security | 14 | Redaction completeness, /config access control, Secret references, AUTH_ENABLED implications |
| **Total** | **55** | |

## Suggested Next Steps

1. Review each checklist item against spec.md and plan.md
2. Items marked as gaps should be addressed in spec.md before `/implement`
3. Run `/analyze` after any spec updates to verify cross-artifact consistency
4. Proceed with `/implement` once all CRITICAL/HIGH items are resolved
