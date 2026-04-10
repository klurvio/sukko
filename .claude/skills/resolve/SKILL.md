# Resolve Issues

Tackle issues returned by `/clarify` or `/analyze` one at a time. Each issue is elaborated with full context before the user decides.

## Usage

```
/resolve
```

## Instructions

1. **Find pending issues**:
   - Look for the most recent `/clarify` or `/analyze` output in the conversation
   - Extract all unresolved issues (clarification questions, analysis findings)
   - If no issues found, inform the user and stop

2. **Build the issue queue**:
   - Order by severity: CRITICAL → HIGH → MEDIUM → LOW
   - Within same severity, order by impact (architecture > config > wording)
   - Track which issues have been resolved

3. **Tackle ONE issue at a time**:
   - Present the issue with full context:
     - **Issue ID and severity**
     - **What**: Describe the problem in detail
     - **Where**: Exact file paths, line numbers, or artifact sections involved
     - **Why it matters**: Impact if left unresolved
     - **Options**: 2-5 concrete resolution options with trade-offs
     - **Recommendation**: Which option and why
   - Wait for the user's decision before proceeding
   - Do NOT present the next issue until the current one is resolved

4. **After each decision**:
   - Apply the fix to the relevant artifacts (spec, plan, tasks)
   - Confirm the change was made
   - Report remaining issue count
   - Present the next issue

5. **Completion**:
   - When all issues are resolved, report:
     - Total issues resolved
     - Artifacts modified
     - Suggested next step (`/implement`, `/generate-tasks`, etc.)

## Notes

- NEVER batch multiple issues together — one at a time, always
- NEVER skip elaboration — even if the fix seems obvious, explain the context
- NEVER apply a fix without user approval
- If the user says "skip" or "defer", mark the issue as deferred and move on
- If the user says "fix all", still elaborate each one but apply the recommended option without waiting for per-issue approval
- Respect "done" or "stop" signals — stop iterating immediately
