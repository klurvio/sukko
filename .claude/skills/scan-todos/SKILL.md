---
name: scan-todos
description: Scan the Go codebase for TODO and "for now" comments that indicate band-aid solutions. Spec out proper fixes.
user-invocable: true
---

# Scan TODOs

Find all TODO and "for now" comments in the Go codebase. Each one indicates incomplete or band-aid code that needs a proper, robust solution. For each finding, create a spec in the backlog.

## Usage

```
/scan-todos [path]
```

Examples:
- `/scan-todos` - Scan the entire Go codebase
- `/scan-todos ws/internal/gateway/` - Scan a specific package

## Instructions

1. **Determine scope**:
   - If a path is provided as argument (`{{args}}`), scan that path
   - If no argument, scan the entire `ws/` directory
   - Only scan `.go` files, exclude `*_test.go`, `*.pb.go`, and `vendor/`

2. **Search for band-aid indicators**:
   - `TODO` (case-insensitive)
   - `FIXME` (case-insensitive)
   - `HACK` (case-insensitive)
   - `for now` (case-insensitive)
   - `temporary` (case-insensitive, in comments only)
   - `placeholder` (case-insensitive, in comments only)

3. **For each finding**:
   - Read the surrounding code (10 lines before and after) to understand the context
   - Determine what the proper solution should be
   - Classify severity:
     - **Critical** — security gap, data integrity risk, or silent failure
     - **Warning** — missing functionality that affects robustness
     - **Info** — cosmetic or minor improvement

4. **Present findings** as a table:

   ```markdown
   ## TODO Scan Report

   **Scope**: [path scanned]
   **Files scanned**: N
   **Findings**: X critical, Y warnings, Z info

   | # | File:Line | Severity | Comment | What it means | Proper solution |
   |---|-----------|----------|---------|---------------|-----------------|
   ```

5. **Create specs** for critical and warning findings:
   - Do NOT create/switch branches
   - Create spec in `specs/backlog/fix/{short-name}/spec.md`
   - Include the TODO context, why it's a band-aid, and what the proper fix should be
   - If multiple related TODOs should be fixed together, group them into one spec
   - If a spec already exists for the finding (check backlog and completed), skip it and note "already spec'd"

6. **Skip known exceptions**:
   - `//nolint` directives are NOT TODOs
   - Comments explaining WHY something is intentionally simple are NOT TODOs
   - Test files — TODOs in tests are less critical
   - Generated code (`*.pb.go`)

7. **Report completion**:
   - Total findings by severity
   - Specs created (with paths)
   - Specs skipped (already exist)
   - Remaining info-level items (no spec needed)

## Notes

- This skill is primarily **read-only** — it only writes spec files, never modifies source code
- Do NOT create branches or switch branches
- Do NOT modify the source code to remove TODOs
- Group related TODOs into single specs when they're part of the same gap
- Check `specs/backlog/` and `specs/completed/` before creating duplicate specs
