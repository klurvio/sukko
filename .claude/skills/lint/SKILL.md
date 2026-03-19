---
name: lint
description: Auto-fix lint and formatting issues locally
user-invocable: true
---

# Lint

Auto-fix lint and formatting issues locally. CI detects violations — this skill fixes them.

## Usage

```
/lint [path...]
```

Examples:
- `/lint` - Fix the entire codebase
- `/lint ws/internal/server/` - Fix a package
- `/lint ws/internal/shared/kafka/consumer.go` - Fix a specific file

## Target Resolution

1. **Determine the target** from `{{args}}`:
   - If arguments provided, use them as the target(s)
   - If no arguments, target the entire codebase (`./...` / `.`)
2. **Set `TARGET`** for commands below:
   - Entire codebase: `TARGET=./...` and `FMT_TARGET=.`
   - Directory (e.g., `server/`): `TARGET=./server/...` and `FMT_TARGET=server/`
   - File (e.g., `server/hub.go`): `TARGET=./server/hub.go` and `FMT_TARGET=server/hub.go`
   - Multiple paths: apply each command to all targets

## Workflow

### 1. Quick Fix

Run these in sequence:

1. `golangci-lint run --fix $TARGET` — auto-fix lint + modernization issues
2. `gofmt -s -w $FMT_TARGET` — simplify + format
3. `go fix $TARGET` — apply Go built-in modernizers (Go 1.26+)
4. `go mod tidy` — only when targeting the entire codebase

Report summary:

```
## Lint Summary

- golangci-lint --fix: X issues auto-fixed, Y remaining
- gofmt: formatted
- go fix: applied
- Remaining issues: [list any that need manual fixing]
```

### 2. Modernize

Run detection commands from "Go Modernization Patterns" below scoped to `$FMT_TARGET`. Review each detection hit and apply modern patterns where applicable.

## Go Modernization Patterns

Review all Go files and apply these modern patterns where applicable.

### Detection Commands

Run these to find modernization opportunities:

```bash
# All detection commands use $FMT_TARGET as the search path
# When linting entire codebase, $FMT_TARGET is "."

# Detect loops that could use slices.Contains/ContainsFunc
grep -rn "for.*range.*{" --include="*.go" $FMT_TARGET | grep -B2 -A2 "if.*==\|if.*\.Has"

# Detect manual min/max patterns
grep -rn "if.*<.*{" --include="*.go" $FMT_TARGET | grep -A1 "return\|="

# Detect old atomic patterns
grep -rn "atomic\.Load\|atomic\.Store\|atomic\.Add" --include="*.go" $FMT_TARGET

# Detect interface{} that could be any
grep -rn "interface{}" --include="*.go" $FMT_TARGET

# Detect HasPrefix + TrimPrefix patterns
grep -rn "HasPrefix\|HasSuffix" --include="*.go" $FMT_TARGET

# Detect SplitN with limit 2
grep -rn "SplitN.*2)" --include="*.go" $FMT_TARGET

# Detect old-style for loops
grep -rn "for.*:=.*0;.*<.*;.*++" --include="*.go" $FMT_TARGET

# Detect manual slice cloning
grep -rn "append(\[\].*nil)," --include="*.go" $FMT_TARGET

# Detect sort.Slice usage
grep -rn "sort\.Slice\|sort\.SliceStable" --include="*.go" $FMT_TARGET

# Detect strings.Split that could use iterator-based SplitSeq/Lines (1.24+)
grep -rn "strings\.Split\b\|bytes\.Split\b" --include="*.go" $FMT_TARGET
grep -rn "strings\.Fields\b\|bytes\.Fields\b" --include="*.go" $FMT_TARGET

# Detect old benchmark loops that could use b.Loop() (1.24+)
grep -rn "for.*:=.*0;.*<.*b\.N;.*++" --include="*.go" $FMT_TARGET

# Detect runtime.SetFinalizer that could use runtime.AddCleanup (1.24+)
grep -rn "runtime\.SetFinalizer" --include="*.go" $FMT_TARGET

# Detect json omitempty on non-string types that could use omitzero (1.24+)
grep -rn 'json:"[^"]*omitempty' --include="*.go" $FMT_TARGET

# Detect WaitGroup Add/Done that could use wg.Go() (1.25+)
grep -rn "\.Add(1)" --include="*.go" $FMT_TARGET | grep -i "wg\|waitgroup\|wait"

# Detect errors.As with pointer target that could use errors.AsType (1.26+)
grep -rn "errors\.As(" --include="*.go" $FMT_TARGET

# Detect new() followed by assignment that could use new(expr) (1.26+)
grep -rn "= new(" --include="*.go" $FMT_TARGET
```

### Go 1.18+ (Generics)

- `any` instead of `interface{}` for cleaner type declarations
- Generic helpers instead of `interface{}` functions with type assertions

### Go 1.19+ (Atomic Types)

- `atomic.Bool` instead of `atomic.LoadInt32`/`atomic.StoreInt32` with 0/1
- `atomic.Int64`/`atomic.Int32` instead of `atomic.LoadInt64`/`atomic.AddInt64`
- `atomic.Pointer[T]` instead of `atomic.Value` or `unsafe.Pointer` patterns

### Go 1.20+ (Errors & Strings)

- `errors.Join(err1, err2)` instead of custom multi-error types
- `context.WithCancelCause` for cancellation with reason tracking
- `strings.CutPrefix`/`strings.CutSuffix` instead of `HasPrefix` + `TrimPrefix`
- `strings.Cut` instead of `SplitN(s, sep, 2)` + length check

### Go 1.21+ (Builtins & Packages)

- `min(a, b)`/`max(a, b)` builtins instead of `if a < b { return a }`
- `clear(m)` instead of `m = make(map[K]V)` or loop deletion
- `slices.Contains` instead of manual loop searching
- `slices.ContainsFunc` instead of loop with predicate check
- `slices.Index`/`slices.IndexFunc` instead of manual index finding
- `slices.Clone` instead of `append([]T(nil), s...)`
- `slices.Equal` instead of manual loop comparison
- `slices.Sort` instead of `sort.Slice` for ordered types
- `slices.SortFunc` instead of `sort.Slice` with custom comparator
- `slices.Reverse` instead of manual swap loop
- `slices.Compact` to remove consecutive duplicates
- `slices.Delete`/`slices.Insert`/`slices.Replace` for slice mutation
- `maps.Clone` instead of manual map copy loop
- `maps.Keys`/`maps.Values` instead of loop to collect keys/values
- `maps.DeleteFunc` instead of loop with delete
- `maps.Equal`/`maps.EqualFunc` instead of manual map comparison

### Go 1.22+ (Range & HTTP)

- `for i := range n` instead of `for i := 0; i < n; i++`
- `for range slice` instead of `for _ = range slice` when value unused
- `slices.Concat(s1, s2, s3)` instead of multiple `append` calls
- `cmp.Or(a, b, c)` for first non-zero value (default value chains)
- `cmp.Compare` for three-way comparison
- `http.ServeMux` patterns like `"GET /api/users/{id}"` for method routing

### Go 1.23+ (Iterators)

- `slices.All`/`slices.Values`/`slices.Backward` for iterator-based range
- `maps.All`/`maps.Keys`/`maps.Values` returning iterators
- Range over functions for custom iterators

### Go 1.24+ (Iterator Strings, Benchmarks & Cleanup)

- **Iterator-based string/bytes splitting** — avoid allocating intermediate slices:
  - `strings.Lines(s)` instead of `strings.Split(s, "\n")` for line iteration
  - `strings.SplitSeq(s, sep)` instead of `strings.Split(s, sep)` when iterating
  - `strings.SplitAfterSeq(s, sep)` instead of `strings.SplitAfter(s, sep)` when iterating
  - `strings.FieldsSeq(s)` instead of `strings.Fields(s)` when iterating
  - `strings.FieldsFuncSeq(s, f)` instead of `strings.FieldsFunc(s, f)` when iterating
  - Same functions available in `bytes` package: `bytes.Lines`, `bytes.SplitSeq`, `bytes.SplitAfterSeq`, `bytes.FieldsSeq`, `bytes.FieldsFuncSeq`
- **Benchmark loops** — `for b.Loop() { ... }` instead of `for i := 0; i < b.N; i++ { ... }`
- **Cleanup over finalizers** — `runtime.AddCleanup(obj, cleanupFunc)` instead of `runtime.SetFinalizer(obj, f)`
- **Filesystem sandboxing** — `os.OpenRoot(dir)` to restrict filesystem operations within a directory
- **JSON `omitzero` tag** — `json:"field,omitzero"` instead of `json:"field,omitempty"` for clearer zero-value omission
- **Generic type aliases** — `type Container[T any] = []T` now fully supported
- **`testing.T.Context()`** / **`testing.B.Context()`** — returns a context canceled when the test completes
- **`testing.T.Chdir(dir)`** — temporarily changes working directory for the duration of a test
- **`crypto/rand.Text()`** — generate cryptographically secure random text strings

### Go 1.25+ (WaitGroup.Go, synctest & FlightRecorder)

- **`sync.WaitGroup.Go(func())`** instead of the `wg.Add(1); go func() { defer wg.Done(); ... }()` pattern
- **`testing/synctest.Test(t, func(t *testing.T))`** — test concurrent code with virtualized time
- **`net/http.CrossOriginProtection(handler)`** — CSRF protection using Fetch metadata
- **`hash.Cloner` interface** — all standard hash functions implement `Clone()`
- **`runtime/trace.FlightRecorder`** — lightweight in-memory ring buffer for runtime traces
- **`testing.T.Attr(key, value)`** — emit structured test attributes
- **`testing.T.Output()`** — returns an `io.Writer` for writing to test output
- **Container-aware GOMAXPROCS** — runtime respects cgroup CPU limits

### Go 1.26+ (new(expr), errors.AsType & go fix modernizers)

- **`new(expr)` with initializer** — `new(calculateAge(birth))` instead of `p := new(int); *p = calculateAge(birth)`
- **`errors.AsType[T](err)`** — generic, type-safe alternative to `errors.As(err, &target)`
- **`slog.NewMultiHandler(h1, h2, ...)`** — broadcast log records to multiple handlers
- **`bytes.Buffer.Peek(n)`** — inspect next n bytes without advancing
- **`reflect.Type.Fields()`** / **`reflect.Type.Methods()`** — iterator-based enumeration
- **`reflect.Value.Fields()`** / **`reflect.Value.Methods()`** — same for values
- **`testing.T.ArtifactDir()`** — dedicated directory for test output files
- **`go fix ./...`** — rewritten modernizer tool that applies dozens of safe code modernizations

## Notes

- All lint rules and modernization checks are defined in `.golangci.yaml`
- Linters include: `modernize`, `intrange`, `copyloopvar`, `perfsprint`, `usestdlibvars`, `gocritic`, and more
- Do not modify generated code (`*.pb.go`)
- If issues remain after auto-fix, list them for the user to decide on manual fixes
