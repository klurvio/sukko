---
name: auto-resolve
description: Automatically resolves issues from /clarify or /analyze by applying the recommended option for each issue without waiting for user approval. Repeats until all issues are resolved.
user-invocable: true
---

# Auto-Resolve Issues

Automatically apply the recommended resolution for every pending issue from `/clarify` or `/analyze`. No per-issue approval — recommended option is applied immediately. Repeats until the queue is empty.

## Usage

```
/auto-resolve
```

---

## Instructions

1. **Find pending issues**:
   - Look for the most recent `/clarify` or `/analyze` output in the conversation
   - Extract all unresolved issues ordered by severity: CRITICAL → HIGH → MEDIUM → LOW
   - If no issues found, report "Nothing to resolve" and stop

2. **For each issue, in order**:
   - State the issue ID, severity, and title (one line)
   - State the recommended option (one line)
   - Apply the fix immediately to the relevant artifact (spec.md, plan.md, or tasks.md)
   - Confirm the change was applied
   - Move to the next issue without pausing

3. **Repeat** until all issues are resolved

4. **Completion**:
   - Report total issues resolved and artifacts modified
   - Suggest next step (`/plan-feature`, `/implement`, `/generate-tasks`, etc.)

## Notes

- NEVER wait for user input between issues — apply and move on
- NEVER skip an issue — if the recommendation is clear, apply it; if genuinely ambiguous, apply the safest/most conservative option and note it
- Apply fixes atomically — read the file, make the change, confirm
- If the user interrupts with "stop" or "done", stop immediately
