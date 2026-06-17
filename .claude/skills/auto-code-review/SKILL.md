---
name: auto-code-review
description: Runs a full parallel code review and automatically applies all recommended fixes without user approval. Iterates until no issues remain or the diff is clean.
user-invocable: true
---

# Auto Code Review

Identical to `/code-review` but invokes `/auto-resolve` instead of `/resolve` — all findings are fixed automatically using the recommended option. Iterates until no findings remain on modified files.

## Usage

```
/auto-code-review [file or directory]
```

Examples:
- `/auto-code-review` - Review and auto-fix all uncommitted changes on the current branch
- `/auto-code-review ws/internal/shared/kafka/consumer.go` - Review a specific file
- `/auto-code-review ws/internal/server/` - Review all files in a directory

---

## Instructions

Follow all steps from `/code-review` exactly, with two changes:

**Step 7 — Auto-resolve instead of interactive resolve**

If findings exist, invoke `/auto-resolve` instead of `/resolve`:
- `/auto-resolve` applies the recommended fix for each finding immediately, no user approval
- After `/auto-resolve` completes, proceed directly to Step 8

If no findings exist: report "All changes pass review (0 issues scored ≥70)" and skip to Step 9.

**Step 8 — Iterate automatically**

After `/auto-resolve` completes a round, re-run agents (Steps 4–5) on files modified during resolution only:
- If new findings survive scoring: invoke `/auto-resolve` again without asking
- Repeat until agents find no new issues on modified files
- Run `go vet ./...` on all changed packages when the iteration loop is clean
- Do NOT ask "Continue?" between rounds — keep going until the diff is clean or 5 rounds have elapsed (safety cap)

---

## All Other Steps

Follow `/code-review` Steps 1–6, 9–10 exactly:

### Step 1 — Eligibility Check

- Run `git status` and `git diff --name-only` — if no changes exist, report "Nothing to review" and stop
- Count changed files: if >50 Go files are changed, warn ("Large diff — review may be slow") and continue automatically
- Identify file types in scope: `.go`, `.yaml`, `.tf` only — ignore generated files (`*.pb.go`, `*_gen.go`), vendored code, and test fixtures
- Report: "Reviewing N files across M packages"

### Step 2 — Find CLAUDE.md Files

```bash
find . -name "CLAUDE.md" | sort
```

- Load each file found; rules in subdirectory `CLAUDE.md` files apply only to code under that path
- Compile all guidance into a unified rule set for the agents
- Root `CLAUDE.md` `## Constitution` section takes precedence in conflicts

### Step 3 — PR Summary

Run two operations **in parallel**:

**A — Change summary**: Collect commit messages (`git log main..HEAD --oneline`), PR description (if available via `gh pr view`), and the list of changed files with their change type.

**B — Diff context**: For each changed file, read the full file plus the `git diff`. Also read 1-2 neighboring files in the same package to understand local patterns.

### Step 4 — Five Parallel Review Agents

Spawn all five agents **concurrently**:

| Agent | Focus |
|-------|-------|
| **Agent 1** | Constitution compliance — all principles from every CLAUDE.md found in Step 2 |
| **Agent 2** | Logic bugs — type mismatches, off-by-one errors, nil dereferences, incorrect error handling |
| **Agent 3** | Concurrency safety — goroutine lifecycle, channel rules, mutex scope, atomics, panic recovery |
| **Agent 4** | Infrastructure coherence — Helm values match Go `env:` tags, Terraform variables have descriptions, no hardcoded secrets or IPs |
| **Agent 5** | Go best practices — modern Go (1.22+), interface design, allocations in hot paths, dead code, stale comments |

Each agent formats findings as:
```
[AGENT-N] file/path.go:LINE — <issue title>
Confidence: XX/100
What: <description>
Why: <impact>
Fix: <code snippet or clear instruction>
```

### Step 5 — Score and Filter Issues

1. **Spawn a verification subagent** per finding — its job is to **challenge** (disprove) the finding, not restate it
2. **Adjust confidence** based on verification: confirmed → unchanged/boosted; partial → −10–20; disproven → 0
3. **Filter**: discard findings with final score below **70**
4. **Deduplicate**: merge same-issue findings, keep the higher score
5. **Rank**: 🔴 Critical (≥90) · 🟡 Warning (80–89) · 🔵 Suggestion (70–79)

### Step 6 — Present Findings

Present surviving findings grouped by severity:
```
🔴 Critical — [file:line]
Constitution: <principle violated>
Issue: <what is wrong>
Impact: <why it matters>
Fix:
  <code snippet>
Confidence: XX/100
```

If no findings: report "All changes pass review (0 issues scored ≥70)."

### Step 9 — Update Spec Pass Counter

- Look for a `**Passes**:` line near the top of the spec (after the metadata block)
- Increment the `code-review` counter. Do NOT modify other skill counters.
- Set `✓` if PASS (no issues), remove `✓` if issues were found

### Step 10 — Mark Spec Complete

When review passes with no remaining issues:

- Get current branch: `git branch --show-current`
- Resolve spec directory (first match wins): `specs/in-progress/[branch]/` → `specs/backlog/[branch]/` → `specs/completed/[branch]/` → `specs/[branch]/`
- If found in `in-progress/` or `backlog/`, move to `specs/completed/[branch-name]/`
- Create a `COMPLETED_MM-DD-YYYY_HH-MM` timestamp marker

---

## Notes

- NEVER wait for user input between issues or between iterations — apply and continue
- The 5-round safety cap prevents infinite loops if fixes keep introducing new issues
- Parallel agent spawning (Step 4) and verification (Step 5) MUST run concurrently
- The 70-point threshold is the primary noise filter — do not lower it
- Do not flag issues in code that was not changed unless it directly impacts the reviewed code
