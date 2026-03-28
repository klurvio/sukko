---
name: plan-feature
description: Create a technical implementation plan from a feature spec. Generates research, data model, and project structure.
user-invocable: true
---

# Plan Feature Implementation

Transform a WHAT (spec) into a HOW (plan). Creates the technical implementation plan with research, architecture decisions, and file changes.

## Usage

```
/plan-feature [feature description or context]
```

Examples:
- `/plan-feature add per-topic kafka metrics` - Plan with specific context
- `/plan-feature` - Will use existing spec from current branch

## Instructions

1. **Find the active spec**:
   - Get current branch: `git branch --show-current`
   - Resolve spec directory by searching in order (first match wins):
     1. `specs/in-progress/[branch-name]/`
     2. `specs/backlog/[branch-name]/`
     3. `specs/completed/[branch-name]/`
     4. `specs/[branch-name]/` (legacy fallback)
   - Load `spec.md` from the resolved directory
   - Load constitution from `CLAUDE.md` (look for `## Constitution` section)
   - If spec not found in any location, instruct user to run `/specify` first

2. **Gather project context**:
   - Review relevant existing code to understand current patterns

3. **Fill Technical Context** in the plan:

   ```markdown
   # Implementation Plan: [FEATURE]

   **Branch**: [branch] | **Date**: [date] | **Spec**: specs/[branch]/spec.md

   ## Summary
   [Primary requirement + technical approach]

   ## Technical Context
   **Language**: Go 1.22+
   **Services**: ws-server, ws-gateway, provisioning
   **Infrastructure**: Kubernetes (DOKS), Helm, Terraform
   **Messaging**: Redpanda/Kafka (franz-go), NATS (broadcast bus)
   **Storage**: PostgreSQL (provisioning), Valkey (optional)
   **Monitoring**: Prometheus, Grafana, Loki
   **Build/Deploy**: Docker, Taskfile, GitHub Container Registry (ghcr.io)
   ```

4. **Constitution Check** (from `CLAUDE.md`):
   - Evaluate each principle as a gate
   - ERROR if violations exist without justification
   - Document any justified violations

5. **Phase 0 — Research** (`{resolved-spec-dir}/research.md`):
   - For each NEEDS CLARIFICATION → research and resolve
   - For each technology choice → find best practices in codebase
   - Document decisions with rationale and alternatives considered

6. **Phase 1 — Design**:
   - Define data flow and component interactions
   - Identify affected packages (`ws/internal/`, `ws/cmd/`, `deployments/`)
   - Define configuration (env vars, Helm values, Terraform variables)
   - Map changes to existing file structure

7. **Write the plan** to `{resolved-spec-dir}/plan.md`

8. **Re-check constitution** after design phase

9. **Report completion**:
   - Branch and plan path
   - Files to modify/create
   - Constitution compliance status
   - Resource impact assessment
   - Suggested next command (`/generate-tasks`)

## Notes

- Resolve ALL "NEEDS CLARIFICATION" items during Phase 0
- Constitution violations without justification are ERRORs
- Stop after planning — do NOT implement
- Include verification steps (tests, deploy commands, log checks)
