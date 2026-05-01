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

### Step 1 — Load Artifacts

- Get current branch: `git branch --show-current`
- Resolve spec directory by searching in order (first match wins):
  1. `specs/in-progress/[branch-name]/`
  2. `specs/backlog/[branch-name]/`
  3. `specs/completed/[branch-name]/`
  4. `specs/[branch-name]/` (legacy fallback)
- Load `{resolved-spec-dir}/spec.md`, `plan.md`, `tasks.md`
- Load constitution from `CLAUDE.md`
- If required files are missing, abort and instruct user to run the missing prerequisite

---

### Step 2 — Load Code Context

Extract every file path explicitly named in `tasks.md` and read each one that exists on disk. Do **not** read neighborhood files or run `git diff` — scope is strictly files the tasks reference.

This grounds the "Config Coherence" and "Inconsistency" passes in actual code state rather than spec text alone. Skip files that do not exist yet (they are new files to be created).

---

### Step 3 — Build Semantic Models (internal, not output)

- **Change inventory**: Each planned change with file path and description
- **Task coverage mapping**: Map each task to planned changes
- **Constitution rules**: Extract MUST/SHOULD normative statements
- **Code snapshot**: For each file read in Step 2, note actual struct fields, env var tags, function signatures, and Helm keys present — used to validate plan claims against reality

---

### Step 4 — Two Parallel Agents

Spawn both agents **concurrently**. Each receives the full artifact text (spec, plan, tasks) plus the code snapshots from Step 3.

**Agent 1 — Artifact Consistency**
Finds issues within and across the three spec artifacts:

| Pass | What to detect |
|------|----------------|
| Ambiguity | Vague descriptions, unresolved placeholders (TODO, ???) |
| Underspecification | Changes missing file paths, env var names, or types |
| Coverage Gaps | Planned changes with zero tasks; tasks with no mapped change |
| Cross-artifact Inconsistency | Line numbers, insertion points, or field names that conflict between plan and tasks |
| Plan-vs-Reality | Plan claims a field/function/key exists in code but Step 2 snapshot shows it absent or renamed |

**Agent 2 — Constitution & Config Coherence**
Finds violations of project rules and config name mismatches:

| Pass | What to detect |
|------|----------------|
| Constitution Alignment | Conflicts with MUST principles from CLAUDE.md |
| Env Var Coherence | Env var names differ between Go `env:` tags (from code snapshot), Helm values/templates, and plan/tasks text |
| Helm/Go Coherence | Helm value keys do not match Go struct fields or envDefault values |
| Config Duplication | Defaults duplicated in Helm that should come from Go envDefault |
| Feature Gate Compliance | Edition-gated features missing `EditionHasFeature()` checks per Constitution XIII |

Each agent returns findings in this format:
```
[AGENT-N] file/path:LINE_OR_SECTION — <issue title>
Category: <category name>
What: <description>
Why: <impact if unresolved>
Recommendation: <fix>
```

---

### Step 5 — Merge and Assign Severity

Deduplicate findings reported by both agents. Assign severity:

- **CRITICAL**: Constitution MUST violations, env var name mismatches, plan-vs-reality contradictions
- **HIGH**: Missing file paths in tasks, ambiguous config changes, coverage gaps on critical paths
- **MEDIUM**: Missing verification steps, incomplete resource impact, cross-artifact wording conflicts
- **LOW**: Wording improvements, minor redundancy

Limit to 50 findings total; summarize overflow.

---

### Step 6 — Output Analysis Report

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

---

### Step 7 — Update Pass Counter in `spec.md`

- Look for a `**Passes**:` line near the top of the spec (after the metadata block)
- If it exists, increment the `analyze` counter and update its status. Do NOT modify other skill counters.
- If it doesn't exist, add `**Passes**: analyze: 1 ✓` (or without `✓` if issues were found) after the last metadata line (before `## Context`)
- Set `✓` if no findings (PASS), remove `✓` if issues were found
- Example: `**Passes**: clarify: 2 ✓ | analyze: 1 ✓` means analyze ran once, clean

---

### Step 8 — Resolve via `/resolve`

If findings exist, hand them to `/resolve` for interactive resolution. The resolve loop:

- Presents issues **one at a time**, ordered CRITICAL → HIGH → MEDIUM → LOW
- For each issue: full context, options, recommendation — waits for user decision
- Applies the accepted fix to the relevant artifact (spec.md, plan.md, or tasks.md)
- Confirms the change, reports remaining count, moves to the next issue
- Repeats until all issues are resolved or user defers/stops

If no findings exist: report "Analysis clean — all artifacts consistent" and stop.

---

## Notes

- **Analysis is read-only** — agents (Steps 4–5) and the report (Step 6) never modify files. Step 7 (pass counter) and Step 8 (`/resolve` fixes) are the sole exceptions.
- **NEVER hallucinate missing sections** — report accurately what's absent
- **Step 2 scope is strict**: only files named in tasks.md, only if they exist on disk
- **Prioritize constitution violations** — always CRITICAL severity
- Parallel agent spawning (Step 4) MUST run concurrently
- Limit to 50 findings; summarize overflow
