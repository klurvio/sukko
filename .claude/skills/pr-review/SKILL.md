---
name: pr-review
description: Review an existing pull request against the project constitution. Reports findings without modifying code.
user-invocable: true
---

# Review Pull Request

Review an existing PR against the project constitution and Go best practices. One-pass review with structured findings.

## Usage

```
/pr-review [pr-number]
```

Examples:
- `/pr-review 42` - Review PR #42
- `/pr-review` - Will ask for a PR number

## Instructions

1. **Determine the PR**:
   - If a PR number is provided as argument (`{{args}}`), use that
   - If no number is provided, run `gh pr list --state open` and ask the user which PR to review

2. **Gather PR context** by running these commands in parallel:
   - `gh pr view [number] --json title,body,baseRefName,headRefName,files,commits` - PR metadata
   - `gh pr diff [number]` - Full diff
   - `gh pr checks [number]` - CI status (if available)

3. **Load the constitution** from `CLAUDE.md` (the `## Constitution` section)

4. **Read changed files in full** (not just the diff):
   - For each file in the PR, read the complete file to understand full context
   - Read neighboring files if needed to understand patterns

5. **Review against constitution**:

   | Principle | What to check |
   |-----------|---------------|
   | I. No Hardcoded Values | Config externalized via env vars / Helm values |
   | II. Defense in Depth | Input validation at every boundary |
   | III. Error Handling | Errors wrapped with context, no ignored errors |
   | IV. Graceful Degradation | Optional deps use noop, cleanup continues on failure |
   | V. Structured Logging | zerolog, correct levels, panic recovery |
   | VI. Observability | Prometheus metrics, consistent naming |
   | VII. Concurrency Safety | ctx+wg lifecycle, minimal mutex sections, atomics |
   | VIII. Configuration | env struct tags, validation, defaults |
   | IX. Testing | Table-driven, mocks, no t.Parallel() on shared resources |
   | X. Security | Input validation, rate limiting, no secrets in logs |
   | XI. Shared Code | No duplicates, uses internal/shared/ |

6. **Review Helm/Terraform/K8s** (if applicable):
   - Helm values match Go env var names
   - Resource requests/limits sensible
   - No hardcoded secrets

7. **Review PR quality**:
   - PR title follows conventional format
   - PR description adequately explains changes
   - No unrelated changes included
   - No secrets or credentials committed
   - Commit messages follow convention

8. **Present findings** as a structured report:

   ```markdown
   ## PR Review: #[number] — [title]

   **Branch**: [head] → [base]
   **Files Changed**: [count]
   **CI Status**: [pass/fail/pending]

   ### Constitution Compliance
   | Principle | Status | Notes |
   |-----------|--------|-------|
   | I. No Hardcoded Values | PASS/FAIL | [details] |
   | ... | ... | ... |

   ### Code Issues
   | Severity | File | Line | Issue | Suggestion |
   |----------|------|------|-------|------------|

   ### Summary
   - [Overall assessment]
   - [Key concerns if any]
   - [Recommendation: approve / request changes / needs discussion]
   ```

9. **Report** the review summary to the user

## Notes

- This is a **read-only** review — do NOT modify any files or leave PR comments automatically
- If the user wants to post comments on the PR, they can ask after reviewing the findings
- Review the full file context, not just the diff
- Be fair: acknowledge good patterns and improvements, not just problems
- Flag potential breaking changes prominently
