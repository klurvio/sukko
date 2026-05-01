---
name: clarify
description: Identify underspecified areas in the current feature spec by asking targeted clarification questions. Uses parallel specialized agents with confidence scoring, then resolves each issue via /resolve until the spec is clean.
user-invocable: true
---

# Clarify Spec

Detect and reduce ambiguity in the active feature spec using parallel specialized agents. Runs BEFORE planning. Surfaces all issues upfront, then resolves them one by one via `/resolve` until the spec is clean.

## Usage

```
/clarify
```

---

## Step 1 — Find the Active Spec

- Get current branch: `git branch --show-current`
- Resolve spec directory by searching in order (first match wins):
  1. `specs/in-progress/[branch-name]/`
  2. `specs/backlog/[branch-name]/`
  3. `specs/completed/[branch-name]/`
  4. `specs/[branch-name]/` (legacy fallback)
- Load `spec.md` from the resolved directory
- Load constitution from `CLAUDE.md`
- If spec not found: instruct user to run `/specify` first and stop

---

## Step 2 — Five Parallel Analysis Agents

Spawn all five agents **concurrently**. Each receives the full spec text plus the constitution. Each returns findings with a **confidence score (0–100)** per finding.

| Agent | Lens |
|-------|------|
| **Agent 1** | Functional completeness — missing requirements, ambiguous acceptance criteria, undefined edge cases, out-of-scope declarations that contradict in-scope requirements |
| **Agent 2** | Config & types — env var names, types, defaults, units, validation rules; Helm/Go coherence; magic numbers that should be named constants |
| **Agent 3** | Integration & failure modes — service interactions, backpressure, crash/reconnect recovery, partial failure modes, ordering guarantees |
| **Agent 4** | Testing coverage — missing test cases for stated requirements, untestable requirements, race/timing gaps, edge cases in the Testing section that lack a corresponding requirement |
| **Agent 5** | Constitution alignment — spec's planned approach vs MUST rules from CLAUDE.md (config, concurrency, error handling, feature gates, shared code, security) |

Each agent formats findings as:

```
[AGENT-N] Section/Requirement — <issue title>
Confidence: XX/100
What: <description of the ambiguity or gap>
Why: <impact if left unresolved — what breaks or stays undefined>
Options: <2–4 concrete resolution options>
Recommendation: <preferred option with rationale>
```

---

## Step 3 — Score and Filter

For each finding:

1. **Verify** — re-read the relevant spec section and try to disprove the finding. Does the spec already address this elsewhere? Is the concern hypothetical?
2. **Adjust score** based on verification:
   - Confirmed real gap: score unchanged or boosted
   - Partially addressed elsewhere: reduce by 10–20
   - Already resolved in spec: drop to 0
3. **Filter**: discard findings with final score below **70**
4. **Deduplicate**: merge findings from multiple agents on the same issue, keeping the higher score
5. **Rank** by severity:
   - 🔴 **Critical** (score ≥ 90): requirement is contradictory, missing, or violates a constitution MUST
   - 🟡 **Warning** (score 80–89): significant gap that will create ambiguity during planning or implementation
   - 🔵 **Suggestion** (score 70–79): worth clarifying but not blocking

If no findings survive: report "No critical ambiguities detected — spec is ready for planning" and jump to Step 5.

---

## Step 4 — Resolve Issues via `/resolve`

Hand surviving findings (score ≥ 70) to the `/resolve` skill. The resolve loop:

- Presents issues **one at a time**, ordered Critical → Warning → Suggestion
- For each issue: full context, options, recommendation — waits for user decision
- Applies the accepted resolution directly to `spec.md`
- Repeats until all issues are resolved or user defers/stops

Do NOT batch issues or apply fixes without user approval. Follow `/resolve`'s contract exactly.

---

## Step 5 — Update Pass Counter

- Look for a `**Passes**:` line near the top of `spec.md` (after the metadata block)
- If it exists, increment the `clarify` counter and update its status. Do NOT modify other skill counters.
- If it doesn't exist, add `**Passes**: clarify: 1 ✓` after the last metadata line (before `## Context`)
- Set `✓` if the pass was clean (no findings ≥ 70), remove `✓` if issues were found
- Example: `**Passes**: clarify: 2 ✓` means 2 passes, last one was clean

---

## Step 6 — Report Completion

- Total findings surfaced / resolved / deferred
- Sections of `spec.md` modified
- Suggested next step (`/plan-feature` if clean, or `/clarify` again if issues were deferred)

---

## Notes

- Parallel agent spawning (Step 2) MUST run concurrently — never sequentially
- The 70-point threshold is the noise filter — do not lower it
- Never ask about tech stack — that's already defined by the constitution and codebase
- Never reveal the full issue list before Step 4 begins — let `/resolve` control pacing
- Respect "done" / "stop" signals immediately — stop iterating
- If the user says "fix all", still elaborate each issue but apply the recommended option without per-issue approval
