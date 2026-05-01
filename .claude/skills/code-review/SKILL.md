---
name: code-review
description: Review local code changes against the project constitution and Go best practices. Uses parallel specialized agents, adversarial scoring, and iterates until all issues are resolved or user opts to stop.
user-invocable: true
---

# Code Review

Review local code changes for constitution compliance, codebase consistency, Go best practices, performance, and robustness. Runs parallel specialized agents with adversarial verification scoring — only findings scored ≥70 are surfaced.

## Usage

```
/code-review [file or directory]
```

Examples:
- `/code-review` - Review all uncommitted changes on the current branch
- `/code-review ws/internal/shared/kafka/consumer.go` - Review a specific file
- `/code-review ws/internal/server/` - Review all files in a directory

---

## Step 1 — Eligibility Check

Before investing in a full review, verify the scope is valid:

- Run `git status` and `git diff --name-only` — if no changes exist, report "Nothing to review" and stop
- Count changed files: if >50 Go files are changed, warn the user ("Large diff — review may be slow") and ask if they want to proceed
- Identify file types in scope: `.go`, `.yaml`, `.tf` only — ignore generated files (`*.pb.go`, `*_gen.go`), vendored code, and test fixtures
- Report: "Reviewing N files across M packages"

---

## Step 2 — Find CLAUDE.md Files

Scan the **entire directory hierarchy** for `CLAUDE.md` files — not just the root:

```bash
find . -name "CLAUDE.md" | sort
```

- Load each file found
- Rules in subdirectory `CLAUDE.md` files apply only to code under that path
- Compile all guidance into a unified rule set for the agents
- The root `CLAUDE.md` `## Constitution` section takes precedence in conflicts

---

## Step 3 — PR Summary

Run two operations **in parallel** to build context for the review agents:

**A — Change summary**: Collect commit messages (`git log main..HEAD --oneline`), PR description (if available via `gh pr view`), and the list of changed files with their change type (added/modified/deleted).

**B — Diff context**: For each changed file, read the full file plus the `git diff` for that file. Also read 1-2 neighboring files in the same package to understand local patterns and what the changed code interacts with.

Produce a brief internal summary (not shown to user unless asked):
- What the change set does in one sentence
- Which packages are affected
- Whether infrastructure (Helm/Terraform) is also changed

---

## Step 4 — Five Parallel Review Agents

Spawn all five agents **concurrently**. Each agent reviews the full diff + file context from Step 3, applies its specific lens, and returns findings with a **confidence score (0–100)** per finding.

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

---

## Step 5 — Score and Filter Issues

For each finding returned by the five agents:

1. **Spawn a verification subagent** whose job is to **challenge** the finding — not restate it. The verifier re-reads the relevant code and tries to disprove the issue (e.g., "is the nil check actually present two lines up?", "does this mutex scope actually extend across I/O?").

2. **Adjust the confidence score** based on verification:
   - Finding confirmed as real: score unchanged or boosted
   - Finding partially confirmed: score reduced by 10-20
   - Finding disproven: score dropped to 0

3. **Filter**: discard any finding with a final score below **70**. Findings below 70 are noise — either false positives or too speculative to act on.

4. **Deduplicate**: if two agents reported the same issue, merge into one finding, keeping the higher confidence score.

5. **Rank** surviving findings by severity:
   - 🔴 **Critical** (score ≥ 90): Constitution MUST violations, data races, panic paths, silent failures
   - 🟡 **Warning** (score 80–89): Constitution SHOULD violations, non-obvious bugs, missing tests for changed code
   - 🔵 **Suggestion** (score 70–79): Style, naming, minor improvements worth addressing but not blocking

---

## Step 6 — Present Findings

Present surviving findings (score ≥ 70) grouped by severity. For each issue:

```
🔴 Critical — [file:line]
Constitution: <principle violated>
Issue: <what is wrong>
Impact: <why it matters>
Fix:
  <code snippet>
Confidence: XX/100
```

If no findings survive scoring: confirm "All changes pass review (0 issues scored ≥70)."

---

## Step 7 — Resolve via `/resolve`

Hand surviving findings to `/resolve` for interactive resolution. The resolve loop:

- Presents issues **one at a time**, ordered Critical → Warning → Suggestion
- For each issue: full context, options, recommendation — waits for user decision
- Applies the accepted fix directly to the relevant file(s)
- Confirms the change, reports remaining count, moves to the next issue
- Repeats until all issues are resolved or user defers/stops

Do NOT apply fixes directly — `/resolve` owns the fix-apply loop.

---

## Step 8 — Iterate

After `/resolve` completes a round, re-run agents (Steps 4–5) on files modified during resolution only:

- If new findings survive scoring: hand them back to `/resolve` for another round
- Ask before each re-run: "Modified files re-checked — N new issues found. Continue? (yes / stop)"
- Continue until agents find no new issues on modified files, or user stops
- Run `go vet ./...` on all changed packages when the iteration loop is clean

---

## Step 9 — Update Spec Pass Counter

- Look for a `**Passes**:` line near the top of the spec (after the metadata block)
- If it exists, increment the `code-review` counter and update its status. Do NOT modify other skill counters.
- If it doesn't exist, add `**Passes**: code-review: 1 ✓` (or without `✓` if issues were found) after the last metadata line (before `## Context`)
- Set `✓` if PASS (no issues), remove `✓` if issues were found
- Example: `**Passes**: clarify: 2 ✓ | code-review: 3 ✓` means 3 code-review passes, last one clean

---

## Step 10 — Mark Spec Complete

When review passes with no remaining issues:

- Get current branch: `git branch --show-current`
- Resolve spec directory by searching in order (first match wins):
  1. `specs/in-progress/[branch-name]/`
  2. `specs/backlog/[branch-name]/`
  3. `specs/completed/[branch-name]/`
  4. `specs/[branch-name]/` (legacy fallback)
- If found in `specs/in-progress/` or `specs/backlog/`, move to `specs/completed/[branch-name]/`
- Create a `COMPLETED_MM-DD-YYYY_HH-MM` timestamp marker in the spec directory
- If already in `specs/completed/`, just add the timestamp marker if not present

---

## Notes

- Always read files before reviewing — never assume code patterns from memory
- Verification agents (Step 5) must be adversarial — their job is to disprove, not confirm
- The 70-point threshold is the primary noise filter — do not lower it
- Do not flag issues in code that was not changed unless it directly impacts the reviewed code
- Respect existing patterns even if they differ from textbook best practices — consistency matters
- Parallel agent spawning (Step 4) and verification (Step 5) MUST run concurrently for performance
