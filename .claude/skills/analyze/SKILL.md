---
name: analyze
description: Cross-artifact consistency and quality analysis across spec, plan, and tasks. Read-only — does not modify files.
user-invocable: true
---

# Analyze Artifacts

Identify inconsistencies, duplications, ambiguities, and underspecified items across spec, plan, and tasks BEFORE implementation. This is a **read-only** analysis.

## Usage

```
/analyze
```

## Instructions

1. **Load artifacts**:
   - Get current branch: `git branch --show-current`
   - Resolve spec directory by searching in order (first match wins):
     1. `specs/in-progress/[branch-name]/`
     2. `specs/backlog/[branch-name]/`
     3. `specs/completed/[branch-name]/`
     4. `specs/[branch-name]/` (legacy fallback)
   - Load `{resolved-spec-dir}/spec.md`, `plan.md`, `tasks.md`
   - Load constitution from `CLAUDE.md`
   - If required files are missing, abort and instruct user to run the missing prerequisite

2. **Build semantic models** (internal, not output):
   - **Change inventory**: Each planned change with file path and description
   - **Task coverage mapping**: Map each task to planned changes
   - **Constitution rules**: Extract MUST/SHOULD normative statements

3. **Run detection passes** (limit to 50 findings total):

   | Pass | What to detect |
   |------|---------------|
   | Ambiguity | Vague descriptions, unresolved placeholders (TODO, ???) |
   | Underspecification | Changes missing file paths, env var names, or types |
   | Constitution Alignment | Conflicts with MUST principles |
   | Coverage Gaps | Planned changes with zero tasks, tasks with no mapped change |
   | Inconsistency | Env var name mismatches between Go/Helm/docs, type mismatches |
   | Config Coherence | Helm values match Go struct tags, Terraform variables match |

4. **Assign severity**:
   - **CRITICAL**: Constitution MUST violations, env var name mismatches
   - **HIGH**: Missing file paths in tasks, ambiguous config changes
   - **MEDIUM**: Missing verification steps, incomplete resource impact
   - **LOW**: Wording improvements, minor redundancy

5. **Output analysis report** (Markdown, no file writes):

   ```markdown
   ## Analysis Report

   | ID | Category | Severity | Location(s) | Summary | Recommendation |
   |----|----------|----------|-------------|---------|----------------|

   **Coverage Summary:**
   | Planned Change | Has Task? | Task IDs | Notes |

   **Metrics:**
   - Total Changes / Total Tasks / Coverage %
   - Critical Issues Count

   **Next Actions:**
   - [Prioritized recommendations]
   ```

6. **Offer remediation**: Ask if the user wants concrete edit suggestions for top issues (do NOT apply automatically)

## Notes

- **NEVER modify files** — this is read-only analysis
- **NEVER hallucinate missing sections** — report accurately what's absent
- **Prioritize constitution violations** — always CRITICAL severity
- Limit to 50 findings; summarize overflow
