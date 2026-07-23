---
name: auto-analyze
model: opus
description: Runs full artifact analysis (spec, plan, tasks) and automatically applies all recommended fixes via /auto-resolve without user approval. Repeats until clean.
user-invocable: true
---

# Auto-Analyze Artifacts

Identical to `/analyze` but invokes `/auto-resolve` instead of `/resolve` — all findings are fixed automatically using the recommended option. Repeats until no findings remain.

## Usage

```
/auto-analyze
```

---

## Instructions

Follow all steps from `/analyze` exactly, with one change:

**Step 8 — Auto-resolve instead of interactive resolve**

If findings exist, invoke `/auto-resolve` instead of `/resolve`:
- `/auto-resolve` applies the recommended fix for each finding immediately, no user approval
- After `/auto-resolve` completes, re-run agents (Steps 4–5) on modified files only
- If new findings survive scoring, invoke `/auto-resolve` again
- Repeat until agents find no new issues on modified files

If no findings exist: report "Analysis clean — all artifacts consistent" and stop.

---

## All Other Steps

Follow `/analyze` Steps 1–7 exactly:

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

### Step 2 — Load Code Context

Extract every file path explicitly named in `tasks.md` and read each one that exists on disk. Skip files that do not exist yet.

### Step 3 — Build Semantic Models (internal, not output)

- **Change inventory**: Each planned change with file path and description
- **Task coverage mapping**: Map each task to planned changes
- **Constitution rules**: Extract MUST/SHOULD normative statements
- **Code snapshot**: Actual struct fields, env var tags, function signatures, Helm keys

### Step 4 — Two Parallel Agents

Spawn both agents **concurrently**:

**Agent 1 — Artifact Consistency**: Ambiguity, underspecification, coverage gaps, cross-artifact inconsistency, plan-vs-reality.

**Agent 2 — Constitution & Config Coherence**: Constitution alignment, env var coherence, Helm/Go coherence, config duplication, feature gate compliance.

Each agent returns findings as:
```
[AGENT-N] file/path:LINE_OR_SECTION — <issue title>
Category: <category name>
What: <description>
Why: <impact if unresolved>
Recommendation: <fix>
```

### Step 5 — Merge and Assign Severity

Deduplicate. Assign: CRITICAL / HIGH / MEDIUM / LOW. Limit to 50 findings.

### Step 6 — Output Analysis Report

```markdown
## Analysis Report

| ID | Category | Severity | Location(s) | Summary | Recommendation |

**Coverage Summary:**
| Planned Change | Has Task? | Task IDs | Notes |

**Metrics:**
- Total Changes / Total Tasks / Coverage %
- Critical Issues Count
```

### Step 7 — Update Pass Counter in `spec.md`

- Increment `analyze` counter in `**Passes**:` line
- Set ✓ if no findings, remove ✓ if issues were found

## Notes

- Parallel agent spawning (Step 4) MUST run concurrently
- Limit to 50 findings; summarize overflow
- Analysis agents (Steps 4–5) are read-only; only `/auto-resolve` and pass counter modify files
