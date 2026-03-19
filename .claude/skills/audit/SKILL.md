---
name: audit
description: Audit code changes or specific files/directories against the project constitution. Read-only — reports violations without modifying code.
user-invocable: true
---

# Constitution Audit

Audit code against the project constitution. Reports violations grouped by principle and severity. Read-only — does not modify files.

## Usage

```
/audit [path or file]
```

Examples:
- `/audit` - Audit all uncommitted changes on the current branch
- `/audit ws/internal/server/` - Audit all Go files in a directory
- `/audit ws/internal/shared/kafka/consumer.go` - Audit a specific file

## Instructions

1. **Determine audit scope**:
   - If a file or directory is provided as argument (`{{args}}`), audit that target:
     - For a directory, find all `.go`, `.yaml`, `.proto`, `.sql` files within it
     - For a file, audit just that file
   - If no argument is provided, audit current code changes:
     - Run these commands in parallel:
       - `git diff --name-only` - Uncommitted changed files
       - `git diff --cached --name-only` - Staged files
       - `git log main..HEAD --name-only --pretty=format:""` - All files changed on this branch vs main
     - Deduplicate the file list
     - Filter to `.go`, `.yaml`, `.proto`, `.sql`, `.tf` files only
   - If no files are found, inform the user and stop

2. **Load the constitution** from `CLAUDE.md` (the `## Constitution` section). Read it in full.

3. **Read all files under audit**:
   - Read each file in full
   - For Go files, also read neighboring files in the same package when needed to understand context (e.g., interfaces, types, constants referenced by the audited code)

4. **Audit against each constitution principle**:

   For each file, check against every applicable principle:

   | Principle | What to check |
   |-----------|---------------|
   | I. Configuration | All configurable params externalized via `env:` struct tags. Go `envDefault:` is source of truth. Helm doesn't duplicate Go defaults. No magic numbers or strings. Validation at startup. |
   | II. Defense in Depth | Input validation at every boundary. No assumptions about upstream validation. |
   | III. Error Handling | Errors wrapped with `fmt.Errorf("context: %w", err)`. Sentinel errors for expected conditions. No silently ignored errors. |
   | IV. Graceful Degradation | Optional deps use noop implementations. Multi-step cleanup continues on failure. Retry uses exponential backoff. |
   | V. Structured Logging | zerolog with structured fields. Correct log levels. No `log.Printf` or `fmt.Println`. Panic recovery mandated. |
   | VI. Observability | Prometheus metrics for significant ops. Correct prefix (`ws_`, `gateway_`, `provisioning_`). Units in names. Histograms for latency. |
   | VII. Concurrency Safety | Goroutine lifecycle (wg.Add before go, defer recover first, defer wg.Done second, ctx.Done check). Channel patterns correct. Mutex minimal sections with defer unlock. Atomics for hot-path counters. |
   | VIII. Testing | Table-driven tests. Interface mocks. No t.Parallel() on shared resources. Edge cases covered. Test coverage for all changes. |
   | IX. Security | Rate limiting at multiple levels. No secrets in logs. JWT validation. `//nolint` has justification. |
   | X. Shared Code | No duplicates across packages. Check `internal/shared/` before new utilities. Service-specific code stays in service package. |
   | XI. Prior Art | New features informed by established services (Pusher, Ably, Centrifugo, etc.). |
   | XII. API Design | REST for external APIs, gRPC for internal. Versioned endpoints. Error response format. Pagination. |

5. **Classify each finding by severity**:
   - **Critical** — Constitution violation that affects correctness, security, or data integrity (e.g., missing error handling, concurrency bug, secrets in logs)
   - **Warning** — Constitution violation that affects maintainability or robustness (e.g., missing metrics, hardcoded value, missing test)
   - **Info** — Minor deviation or suggestion for improvement (e.g., could use atomics instead of mutex for a counter)

6. **Present the audit report**:

   ```
   ## Audit Report

   **Scope**: [description of what was audited]
   **Files audited**: N
   **Findings**: X critical, Y warnings, Z info

   ### Critical

   #### [File:Line] — Principle N violation: [short description]
   **What**: [description of the issue]
   **Why it matters**: [impact]
   **Fix**: [concrete suggestion or code snippet]

   ### Warnings
   ...

   ### Info
   ...

   ### Clean
   [List files that passed all checks]
   ```

   - If no findings, confirm: "All files pass constitution audit."
   - Sort findings by severity (critical first), then by file path

7. **Summary line** at the end:
   - `PASS` — No critical or warning findings
   - `WARN` — No critical findings but warnings exist
   - `FAIL` — Critical findings exist

## Notes

- This skill is **read-only** — it NEVER modifies files. Use `/code-review` if you want iterative fixes.
- Always read files before auditing — never assume code patterns from memory
- Only flag issues that are genuine constitution violations, not style preferences
- When auditing a directory (not git changes), audit ALL files — not just changed ones
- For Go files, understand the full context before flagging (read types, interfaces, related code)
- Do not flag issues in third-party or generated code (e.g., `*.pb.go`)
- If a `//nolint` or `#nosec` has a justification comment, respect it unless the justification is clearly wrong
