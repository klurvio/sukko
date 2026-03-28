---
name: generate-tasks
description: Generate an actionable, dependency-ordered task list from the implementation plan.
user-invocable: true
---

# Generate Tasks

Break the implementation plan into ordered, executable tasks.

## Usage

```
/generate-tasks
```

## Instructions

1. **Load design documents** from the active feature:
   - Get current branch: `git branch --show-current`
   - Resolve spec directory by searching in order (first match wins):
     1. `specs/in-progress/[branch-name]/`
     2. `specs/backlog/[branch-name]/`
     3. `specs/completed/[branch-name]/`
     4. `specs/[branch-name]/` (legacy fallback)
   - **Required**: `{resolved-spec-dir}/plan.md`, `{resolved-spec-dir}/spec.md`
   - **Optional**: `{resolved-spec-dir}/research.md`
   - Load constitution from `CLAUDE.md`

2. **Extract from loaded docs**:
   - Components affected (ws-server, gateway, provisioning, Helm, Terraform)
   - Configuration changes (env vars, Helm values, Terraform variables)
   - Code changes (Go packages, functions, tests)
   - Infrastructure changes (K8s resources, Redpanda topics, monitoring)

3. **Generate tasks** organized by phase:

   **Task Format** (REQUIRED for every task):
   ```
   - [ ] [TaskID] [P?] Description with file path
   ```
   - `TaskID` = T001, T002, T003... (sequential)
   - `[P]` = parallelizable (different files, no deps) — only if applicable
   - Description must include exact file path

4. **Phase structure**:
   - **Phase 1: Config** — Helm values, env vars, Terraform variables
   - **Phase 2: Code** — Go implementation (models → logic → handlers)
   - **Phase 3: Infrastructure** — K8s manifests, monitoring dashboards
   - **Phase 4: Testing** — Unit tests, integration verification
   - **Phase 5: Deploy & Verify** — Build, deploy, log checks, load test

5. **Include dependency information**:
   - Phase dependencies (Config → Code → Infrastructure → Testing → Deploy)
   - Within-phase dependencies
   - Parallel opportunities (tasks on different files with no deps)

6. **Write tasks** to `{resolved-spec-dir}/tasks.md`

7. **Report**:
   - Total task count and per-phase breakdown
   - Parallel opportunities identified
   - Suggested first task to start with
   - Suggested next command (`/implement`)

## Notes

- Tests are OPTIONAL — only include if explicitly requested or plan mentions them
- Each phase should be independently verifiable
- Tasks must be specific enough for an LLM to execute without additional context
- Include exact file paths in every task description
- Mark parallel tasks with [P] only when they truly have no dependencies
