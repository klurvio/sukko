---
name: code-review
description: Review local code changes against the project constitution and Go best practices. Iterates until all issues are resolved or user opts to stop.
user-invocable: true
---

# Code Review

Review local code changes for constitution compliance, codebase consistency, Go best practices, performance, and robustness. Iterates until clean.

## Usage

```
/code-review [file or directory]
```

Examples:
- `/code-review` - Review all uncommitted changes on the current branch
- `/code-review ws/internal/shared/kafka/consumer.go` - Review a specific file
- `/code-review ws/internal/server/` - Review all files in a directory

## Instructions

1. **Determine review scope**:
   - If a file or directory is provided as argument (`{{args}}`), review that target
   - If no argument is provided, run these commands in parallel:
     - `git diff --name-only` - Uncommitted changed files
     - `git diff --cached --name-only` - Staged files
     - `git log main..HEAD --name-only --pretty=format:""` - All files changed on this branch
   - Deduplicate and filter to `.go`, `.yaml`, `.tf` files
   - If no changes are found, inform the user

2. **Load the constitution** from `CLAUDE.md` (the `## Constitution` section)

3. **Read all files under review** and gather codebase context:
   - Read each changed file in full
   - Read neighboring files in the same package to understand local patterns
   - Check for related code that interacts with the changed code

4. **Review against each constitution principle**:

   | Principle | What to check |
   |-----------|---------------|
   | I. No Hardcoded Values | All configurable params use env vars or config structs |
   | II. Defense in Depth | Input validation at every boundary, no assumptions about upstream |
   | III. Error Handling | Errors wrapped with context (`%w`), sentinel errors defined, no ignored errors |
   | IV. Graceful Degradation | Optional deps use noop implementations, multi-step cleanup continues on failure |
   | V. Structured Logging | zerolog with structured fields, correct log levels, panic recovery on goroutines |
   | VI. Observability | Prometheus metrics for key operations, consistent naming (`ws_` prefix), labels documented |
   | VII. Concurrency Safety | Goroutine lifecycle managed (ctx+wg), minimal mutex sections, atomics for simple counters |
   | VIII. Configuration | Env vars with `env:` struct tags, validation at startup, sensible defaults |
   | IX. Testing | Table-driven tests, interface mocks, no t.Parallel() on shared resources |
   | X. Security | Input validation, rate limiting, no secrets in logs, JWT validation |
   | XI. Shared Code | No duplicates across packages, check `internal/shared/` before writing new utilities |

5. **Review for Go best practices** (beyond constitution):
   - Modern Go (1.22+): `any`, `slices`, `maps`, `for range N`, `errors.Join`
   - Interface design: small interfaces at point of use, accept interfaces return concrete
   - No unnecessary allocations in hot paths
   - Channel operations non-blocking where appropriate
   - Proper `defer` ordering (panic recovery first)

6. **Review Helm/Terraform changes** (if applicable):
   - Values have sensible defaults
   - Env var names match Go `env:` struct tags
   - No hardcoded IPs, passwords, or secrets
   - Terraform variables have descriptions and validation

7. **Present findings** to the user:
   - Group issues by severity: **Critical**, **Warning**, **Suggestion**
   - For each issue include:
     - File path and line number
     - Constitution principle violated (if applicable)
     - What the issue is
     - Why it matters
     - The fix (code snippet or clear instruction)
   - If no issues are found, confirm the code passes review

8. **Apply fixes** after user approval:
   - Ask the user which issues to fix (all, specific ones, or none)
   - Apply the approved fixes
   - Re-read the modified files to verify fixes don't introduce new issues

9. **Iterate** — repeat steps 4-8 on the modified files:
   - Continue reviewing until no issues remain
   - On each iteration, only review files that were modified in the previous pass
   - Ask the user before each iteration if they want to continue or stop

10. **Move spec to completed** when review passes with no remaining issues:
    - Get current branch: `git branch --show-current`
    - Resolve spec directory by searching in order (first match wins):
      1. `specs/in-progress/[branch-name]/`
      2. `specs/backlog/[branch-name]/`
      3. `specs/completed/[branch-name]/`
      4. `specs/[branch-name]/` (legacy fallback)
    - If the spec was found in `specs/in-progress/[branch-name]/` or `specs/backlog/[branch-name]/`, move it to `specs/completed/[branch-name]/`
    - Create a `COMPLETED_MM-DD-YYYY_HH-MM` timestamp marker in the spec directory
    - If already in `specs/completed/`, just create the timestamp marker if not present

## Notes

- Always read files before reviewing — never assume code patterns from memory
- Prioritize issues that affect correctness and robustness over style preferences
- Do not flag issues in code that was not changed unless it directly impacts the reviewed code
- Respect existing patterns even if they differ from textbook best practices — consistency matters
- Do not add comments, docstrings, or type annotations to unchanged code
- If a pattern seems intentionally unconventional, ask before flagging it
