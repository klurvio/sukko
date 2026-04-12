---
name: scan-silent-errors
description: Scan Go codebase for error paths that don't log or return the error — silent failures that make debugging impossible.
user-invocable: true
---

# Scan Silent Errors

Find error handling blocks that swallow errors without logging or returning them. Silent failures violate Constitution III (Error Handling) and V (Structured Logging) — they make production debugging impossible.

## Usage

```
/scan-silent-errors [path]
```

Examples:
- `/scan-silent-errors` - Scan the entire Go codebase
- `/scan-silent-errors ws/internal/gateway/` - Scan a specific package

## Instructions

1. **Determine scope**:
   - If a path is provided as argument (`{{args}}`), scan that path
   - If no argument, scan the entire `ws/` directory
   - Only scan `.go` files, exclude `*.pb.go` and `vendor/`

2. **Search for silent error patterns**:

   Pattern 1: **Ignored error return** — `_ = someFunc()` without a comment explaining why
   ```go
   _ = conn.Close()  // ← silent if no comment
   ```

   Pattern 2: **Empty error block** — `if err != nil { }` or `if err != nil { return }`
   ```go
   if err != nil {
       return  // ← no log, no wrap, just bail
   }
   ```

   Pattern 3: **Error checked but not logged** — `if err != nil { return err }` without logging
   ```go
   if err != nil {
       return err  // ← no structured log for debugging
   }
   ```
   Note: This is NOT always a violation — library code that wraps errors with `fmt.Errorf` is fine. Only flag if the error is returned WITHOUT context wrapping AND no log.

   Pattern 4: **Error in goroutine not surfaced** — error occurs inside a goroutine but is never logged, sent to a channel, or returned
   ```go
   go func() {
       result, err := doWork()
       if err != nil {
           return  // ← swallowed silently in goroutine
       }
   }()
   ```

3. **Exclude known-safe patterns**:
   - `_ = w.Write(...)` in HTTP handlers — non-actionable (client disconnected), should have comment
   - `_ = conn.Close()` with `// best-effort` or similar comment — intentionally ignored
   - `_ = resp.Body.Close()` — standard Go pattern
   - `defer func() { _ = ... }()` with comment explaining why
   - Errors wrapped with `fmt.Errorf("context: %w", err)` — properly handled
   - `//nolint` with justification — intentionally suppressed

4. **Identify hot paths vs cold paths** before recommending fixes:

   **Hot paths** (per-message, per-connection — NEVER add logging here):
   - `proxy.go` — WebSocket message forwarding (proxyClientToBackend, proxyBackendToClient)
   - `proxy.go` — interceptSubscribe, interceptPublish (per-message dispatch)
   - Broadcast fan-out loops (NATS/Valkey publish, shard distribution)
   - Write pump / read pump per-connection loops
   - `channel_filter.go` — filterSubscribeChannels (called per SSE/push request)
   - Kafka consumer batch processing loops

   **Cold paths** (rare operations — logging is safe):
   - Startup/shutdown sequences
   - Auth validation (per-connection, not per-message)
   - License reload
   - gRPC stream connect/reconnect
   - Config loading
   - Provisioning API handlers
   - Tester API handlers

   For hot path silent errors, recommend:
   - Prometheus counter increment (zero-allocation metric) instead of structured log
   - Error wrapping with `fmt.Errorf` so the caller can log once at a higher level
   - Comment explaining why logging is intentionally skipped (performance)

   For cold path silent errors, recommend:
   - Structured log with `logger.Warn().Err(err).Str(...).Msg(...)`

5. **For each finding**:
   - Read surrounding code (10 lines before/after) for context
   - Determine if the code is on a hot path or cold path
   - Classify severity:
     - **Critical** — error silently dropped on ANY path with no metric, no log, no wrap, no comment
     - **Warning** — error on cold path without structured log (but may be wrapped)
     - **Info** — error ignored with partial justification (comment exists but incomplete)

6. **Present findings** as a table:

   ```markdown
   ## Silent Error Scan Report

   **Scope**: [path scanned]
   **Files scanned**: N
   **Findings**: X critical, Y warnings, Z info

   | # | File:Line | Severity | Path | Pattern | Code snippet | Recommended fix |
   |---|-----------|----------|------|---------|--------------|-----------------|
   ```

   `Path` column: `hot` or `cold` — determines the fix approach.

   For each finding, recommend the specific fix based on path type:

   **Cold path fixes** (logging is safe):
   - Add structured log: `logger.Warn().Err(err).Str("component", "...").Msg("description")`
   - Add error wrapping: `fmt.Errorf("context: %w", err)`

   **Hot path fixes** (logging is NOT safe):
   - Add Prometheus counter: `errorCounter.Inc()` (zero allocation)
   - Add error wrapping and let the caller log at a higher level
   - Add comment: `// Error non-actionable on hot path: [reason]. Counted via [metric].`
   - NEVER add `logger.Warn()` or `logger.Error()` on hot paths — zerolog still allocates

7. **Create specs** for critical findings only:
   - Do NOT create/switch branches
   - Create spec in `specs/backlog/fix/{short-name}/spec.md`
   - Group related silent errors into one spec per package
   - If a spec already exists, skip and note "already spec'd"

8. **Report completion**:
   - Total findings by severity
   - Specs created (with paths)
   - Files that are clean (no silent errors)

## Notes

- This skill is primarily **read-only** — only writes spec files for critical findings
- Do NOT create branches or modify source code
- Not every `_ =` is a violation — check for comments explaining why
- Constitution III says: "Ignored errors MUST have an explicit comment explaining why"
- Constitution V says: "Silent failures are forbidden"
- Focus on production code paths — test files are lower priority
