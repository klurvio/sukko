---
name: auto-clarify
model: opus
description: Runs full spec ambiguity analysis and automatically applies all recommended fixes via /auto-resolve without user approval. Repeats until the spec is clean.
user-invocable: true
---

# Auto-Clarify Spec

Identical to `/clarify` but invokes `/auto-resolve` instead of `/resolve` — all findings are fixed automatically using the recommended option. Repeats until no findings remain.

## Usage

```
/auto-clarify
```

---

## Instructions

Follow all steps from `/clarify` exactly, with one change:

**Step 4 — Auto-resolve instead of interactive resolve**

Hand surviving findings to `/auto-resolve` instead of `/resolve`:
- `/auto-resolve` applies the recommended resolution for each finding immediately, no user approval
- After `/auto-resolve` completes, re-run agents (Steps 2–3) on the updated spec
- If new findings survive scoring, invoke `/auto-resolve` again
- Repeat until agents find no findings ≥ 70

---

## All Other Steps

Follow `/clarify` Steps 1–3 and 5–6 exactly:

### Step 1 — Find the Active Spec

- Get current branch: `git branch --show-current`
- Resolve spec directory by searching in order (first match wins):
  1. `specs/in-progress/[branch-name]/`
  2. `specs/backlog/[branch-name]/`
  3. `specs/completed/[branch-name]/`
  4. `specs/[branch-name]/` (legacy fallback)
- Load `spec.md` from the resolved directory
- Load constitution from `CLAUDE.md`
- If spec not found: instruct user to run `/specify` first and stop

### Step 2 — Five Parallel Analysis Agents

Spawn all five agents **concurrently**. Each receives the full spec text plus the constitution. Each returns findings with a **confidence score (0–100)**.

| Agent | Lens |
|-------|------|
| **Agent 1** | Functional completeness — missing requirements, ambiguous acceptance criteria, undefined edge cases |
| **Agent 2** | Config & types — env var names, types, defaults, units, validation rules; magic numbers |
| **Agent 3** | Integration & failure modes — service interactions, backpressure, crash/reconnect recovery, partial failures |
| **Agent 4** | Testing coverage — missing test cases, untestable requirements, race/timing gaps |
| **Agent 5** | Constitution alignment — spec's planned approach vs MUST rules from CLAUDE.md |

Each agent formats findings as:
```
[AGENT-N] Section/Requirement — <issue title>
Confidence: XX/100
What: <description>
Why: <impact if left unresolved>
Options: <2–4 concrete resolution options>
Recommendation: <preferred option with rationale>
```

### Step 3 — Score and Filter

1. **Verify** each finding — re-read spec section, try to disprove
2. **Adjust score**: confirmed → unchanged; partially addressed → -10 to -20; resolved → 0
3. **Filter**: discard below 70
4. **Deduplicate**: merge same-issue findings, keep higher score
5. **Rank**: 🔴 Critical (≥90) | 🟡 Warning (80–89) | 🔵 Suggestion (70–79)

If no findings survive: report "No critical ambiguities detected — spec is ready for planning" and jump to Step 5.

### Step 5 — Update Pass Counter

- Increment `clarify` counter in `**Passes**:` line of `spec.md`
- Set ✓ if pass was clean, remove ✓ if issues were found

### Step 6 — Report Completion

- Total findings surfaced / resolved
- Sections of `spec.md` modified
- Suggested next step (`/plan-feature` if clean)

## Notes

- Parallel agent spawning (Step 2) MUST run concurrently
- The 70-point threshold is the noise filter — do not lower it
- Never ask about tech stack — defined by constitution and codebase
- If user interrupts with "stop", stop immediately
